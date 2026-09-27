package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Automatic moderation (P2): rules set by manage_server, applied to the
// messages of members without manage_messages in the channel (owner and
// administrators included in that exemption). A message breaking a rule is
// refused with a code telling which rule (automod_word, automod_link,
// automod_mentions, automod_duplicate) and recorded in the audit log
// (automod_block: rule and channel, never the text). After automodStrikes
// refusals within automodWindow, the member can be timed out automatically.
// Word matching ignores case and accents and compares whole words ("mot*":
// words starting with "mot"; several words: that sequence). Invisible
// characters (zero-width spaces, soft hyphens…) are dropped and lookalike
// letters folded (full-width and mathematical letters, Cyrillic and Greek
// letters that look Latin): "cr\u200bétin" or "сrétin" (Cyrillic с) match
// "crétin". Thread names, forum post titles and nicknames are checked too
// (banned words and links). A member's refusals are logged once per rule and
// minute (automodLogEvery).

const (
	automodMaxWords    = 200
	automodMaxWordLen  = 60
	automodStrikes     = 3
	automodWindow      = 10 * time.Minute
	automodDupWindow   = 30 * time.Second
	automodDupMax      = 2 // the same message a third time within automodDupWindow is refused
	automodLogEvery    = time.Minute
	auditAutomodBlock  = "automod_block"
	automodSettingsKey = "automod"
)

type autoModConfig struct {
	Words       []string `json:"words"`        // banned words or phrases
	BlockLinks  bool     `json:"block_links"`  // refuse messages with links
	MaxMentions int      `json:"max_mentions"` // distinct mentions per message (0: no limit)
	Duplicates  bool     `json:"duplicates"`   // refuse the same message repeated quickly
	Timeout     int64    `json:"timeout"`      // seconds of automatic timeout after repeated refusals (0: none)
}

// autoModState remembers recent messages and refusals, in memory (a restart forgets them).
type autoModState struct {
	mu      sync.Mutex
	recent  map[string][]recentMsg
	strikes map[string][]time.Time
	logged  map[string]time.Time // member|rule → last audit entry
}

type recentMsg struct {
	text string
	at   time.Time
}

func (c *autoModConfig) validate() error {
	if len(c.Words) > automodMaxWords {
		return errf(http.StatusBadRequest, "invalid_automod", "at most %d words", automodMaxWords)
	}
	words := c.Words[:0]
	seen := map[string]bool{}
	for _, w := range c.Words {
		w = strings.Join(strings.Fields(w), " ")
		if w == "" || seen[w] {
			continue
		}
		if len([]rune(w)) > automodMaxWordLen || len(tokens(w, true)) == 0 {
			return errf(http.StatusBadRequest, "invalid_automod", "invalid word %q", w)
		}
		seen[w] = true
		words = append(words, w)
	}
	c.Words = words
	if c.MaxMentions < 0 || c.MaxMentions > 100 {
		return errf(http.StatusBadRequest, "invalid_automod", "max_mentions must be 0 to 100")
	}
	if c.Timeout < 0 || time.Duration(c.Timeout)*time.Second > maxTimeout {
		return errf(http.StatusBadRequest, "invalid_automod", "timeout must be 0 to 28 days")
	}
	return nil
}

func (s *Server) autoModConfig(ctx context.Context) (autoModConfig, error) {
	c := autoModConfig{Words: []string{}}
	v, err := s.setting(ctx, automodSettingsKey)
	if err != nil || v == "" {
		return c, err
	}
	err = json.Unmarshal([]byte(v), &c)
	if c.Words == nil {
		c.Words = []string{}
	}
	return c, err
}

// handleAutoMod: GET/PUT /v1/server/automod (manage_server).
func (s *Server) handleAutoMod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPut {
		var c autoModConfig
		if err := decode(r, &c); err != nil {
			writeErr(w, r, err)
			return
		}
		if c.Words == nil {
			c.Words = []string{}
		}
		if err := c.validate(); err != nil {
			writeErr(w, r, err)
			return
		}
		v, _ := json.Marshal(c)
		if err := setSetting(ctx, s.db, automodSettingsKey, string(v)); err != nil {
			writeErr(w, r, err)
			return
		}
		s.audit(ctx, s.db, memberFrom(r).ID, auditServerUpdate, "", "", map[string]any{"automod": true})
	}
	c, err := s.autoModConfig(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// wordTokens lowercases, strips accents and splits into words (patterns
// keep their "*").
func wordTokens(s string) []string { return tokens(s, false) }

// lookalikes are Cyrillic and Greek letters drawn like Latin ones (after lowercasing).
var lookalikes = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'к': 'k', 'м': 'm', 'н': 'h', 'о': 'o', 'р': 'p', 'с': 'c', 'т': 't', 'у': 'y', 'х': 'x',
	'і': 'i', 'ј': 'j', 'ѕ': 's', 'ԁ': 'd', 'һ': 'h', 'ӏ': 'l', 'ԛ': 'q', 'ԝ': 'w',
	'α': 'a', 'β': 'b', 'ε': 'e', 'η': 'n', 'ι': 'i', 'κ': 'k', 'ν': 'v', 'ο': 'o', 'ρ': 'p', 'τ': 't', 'υ': 'u', 'χ': 'x',
}

func tokens(s string, pattern bool) []string {
	var b strings.Builder
	// NFKD: compatibility forms (full-width, mathematical letters, ligatures) and accents apart.
	for _, r := range norm.NFKD.String(strings.ToLower(s)) {
		if l, ok := lookalikes[unicode.ToLower(r)]; ok {
			r = l
		}
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Cf, r): // accents; invisible characters
		case unicode.IsLetter(r) || unicode.IsDigit(r) || (pattern && r == '*'):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

// bannedWord says whether the message contains one of the banned words or phrases.
func bannedWord(words []string, text string) bool {
	toks := wordTokens(text)
	for _, w := range words {
		pat := tokens(w, true)
		for i := 0; i+len(pat) <= len(toks); i++ {
			ok := true
			for j, p := range pat {
				t := toks[i+j]
				if prefix, found := strings.CutSuffix(p, "*"); found {
					ok = strings.HasPrefix(t, prefix)
				} else {
					ok = t == strings.ReplaceAll(p, "*", "")
				}
				if !ok {
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

var (
	linkRe           = regexp.MustCompile(`(?i)\b(?:https?://|www\.|quarel://)\S`)
	automodMentionRe = regexp.MustCompile(`<@&?[A-Za-z0-9]+>|(?:^|\s)@(?:everyone|here)\b`)
)

func distinctMentions(text string) int {
	seen := map[string]bool{}
	for _, m := range automodMentionRe.FindAllString(text, -1) {
		seen[strings.TrimSpace(m)] = true
	}
	return len(seen)
}

// checkAutoMod applies the rules to a message (create: repeated messages
// too). It returns the refusal to send, if any, after recording it.
func (s *Server) checkAutoMod(ctx context.Context, ps *permSnapshot, member string, channelID int64, text string, create bool) error {
	if ps.inChannel(member, channelID)&permManageMessages != 0 {
		return nil
	}
	c, err := s.autoModConfig(ctx)
	if err != nil {
		return err
	}
	rule := ""
	switch {
	case len(c.Words) > 0 && bannedWord(c.Words, text):
		rule = "word"
	case c.BlockLinks && linkRe.MatchString(text):
		rule = "link"
	case c.MaxMentions > 0 && distinctMentions(text) > c.MaxMentions:
		rule = "mentions"
	case create && c.Duplicates && s.repeated(member, text):
		rule = "duplicate"
	}
	if rule == "" {
		return nil
	}
	return s.autoModRefuse(ctx, ps, member, channelID, rule, c.Timeout)
}

// autoModRefuse logs a refusal (once per member, rule and automodLogEvery),
// counts it towards the automatic timeout and returns the error to send.
func (s *Server) autoModRefuse(ctx context.Context, ps *permSnapshot, member string, channelID int64, rule string, timeout int64) error {
	now := s.now()
	s.automod.mu.Lock()
	if s.automod.logged == nil || len(s.automod.logged) > 10000 {
		s.automod.logged = map[string]time.Time{}
	}
	key := member + "|" + rule
	log := now.Sub(s.automod.logged[key]) >= automodLogEvery
	if log {
		s.automod.logged[key] = now
	}
	s.automod.mu.Unlock()
	if log {
		s.audit(ctx, s.db, "", auditAutomodBlock, member, "", map[string]any{"rule": rule, "channel_id": channelID})
	}
	s.strike(ctx, ps, member, timeout)
	msg := map[string]string{
		"word":      "this message contains a word banned on this server",
		"link":      "links are not allowed on this server",
		"mentions":  "too many mentions in one message",
		"duplicate": "the same message was sent too many times",
	}[rule]
	return errf(http.StatusForbidden, "automod_"+rule, "%s", msg)
}

// repeated records a sent message and says whether it repeats too much.
func (s *Server) repeated(member, text string) bool {
	now := s.now()
	key := strings.Join(wordTokens(text), " ")
	if key == "" {
		key = strings.TrimSpace(text)
	}
	s.automod.mu.Lock()
	defer s.automod.mu.Unlock()
	if s.automod.recent == nil {
		s.automod.recent = map[string][]recentMsg{}
	}
	var kept []recentMsg
	same := 0
	for _, m := range s.automod.recent[member] {
		if now.Sub(m.at) < automodDupWindow {
			kept = append(kept, m)
			if m.text == key {
				same++
			}
		}
	}
	if same >= automodDupMax {
		s.automod.recent[member] = kept
		return true
	}
	s.automod.recent[member] = append(kept, recentMsg{key, now})
	if len(s.automod.recent) > 10000 { // bounded memory: forget everyone's history
		s.automod.recent = map[string][]recentMsg{}
	}
	return false
}

// strike counts a refusal; enough of them in a short time: automatic timeout.
func (s *Server) strike(ctx context.Context, ps *permSnapshot, member string, timeout int64) {
	now := s.now()
	s.automod.mu.Lock()
	if s.automod.strikes == nil {
		s.automod.strikes = map[string][]time.Time{}
	}
	var kept []time.Time
	for _, t := range s.automod.strikes[member] {
		if now.Sub(t) < automodWindow {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	punish := timeout > 0 && len(kept) >= automodStrikes
	if punish {
		kept = nil
	}
	s.automod.strikes[member] = kept
	s.automod.mu.Unlock()
	if !punish || ps.base(member)&permAdministrator != 0 {
		return
	}
	d := time.Duration(timeout) * time.Second
	until := sql.NullInt64{Int64: now.Add(d).UnixMilli(), Valid: true}
	if _, err := s.db.ExecContext(ctx, `UPDATE members SET timeout_until = ? WHERE id = ?`, until, member); err != nil {
		s.logErr("automatic timeout", err)
		return
	}
	s.audit(ctx, s.db, "", auditMemberTimeout, member, "Modération automatique", map[string]any{"duration": timeout})
	if m, err := scanMember(s.db.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE id = ?`, member)); err == nil {
		if view, err := s.memberView(ctx, m); err == nil {
			s.hub.Broadcast("MEMBER_UPDATE", view)
		}
	}
	s.syncPermissions(ctx)
	time.AfterFunc(d+time.Second, func() { s.syncPermissions(context.Background()) })
}

// checkAutoModName applies the banned words and links to a name: a thread's,
// a nickname (channelID 0: exempt with manage_messages on the server).
func (s *Server) checkAutoModName(ctx context.Context, ps *permSnapshot, member string, channelID int64, name string) error {
	p := ps.base(member)
	if channelID != 0 {
		p = ps.inChannel(member, channelID)
	}
	if p&permManageMessages != 0 {
		return nil
	}
	c, err := s.autoModConfig(ctx)
	if err != nil {
		return err
	}
	switch {
	case len(c.Words) > 0 && bannedWord(c.Words, name):
		return s.autoModRefuse(ctx, ps, member, channelID, "word", c.Timeout)
	case c.BlockLinks && linkRe.MatchString(name):
		return s.autoModRefuse(ctx, ps, member, channelID, "link", c.Timeout)
	}
	return nil
}
