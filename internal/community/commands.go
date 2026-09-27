package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/secret"
)

// Slash commands (P2). A bot declares its commands (PUT /v1/bots/@me/commands);
// members see them (GET /v1/commands, COMMANDS_UPDATE) and run one in a
// channel where they may post (POST /v1/channels/{id}/commands): the server
// checks the options against the declaration and sends INTERACTION_CREATE to
// that bot's connections only. The bot answers within interactionTTL with
// POST /v1/interactions/{id}/reply {content, ephemeral}: a message in the
// channel (marked with the command and who ran it; the bot needs
// send_messages there), or, ephemeral, INTERACTION_REPLY to the member's own
// connections, never stored.

const (
	maxCommandsPerBot = 50
	maxCommandOptions = 10
	interactionTTL    = 15 * time.Minute
	maxReplies        = 5 // answers (and follow-ups) to one interaction
)

var commandNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

var optionTypes = map[string]bool{"string": true, "integer": true, "boolean": true, "member": true, "channel": true}

type commandOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // string, integer, boolean, member (ID), channel (ID)
	Required    bool   `json:"required"`
}

type botCommand struct {
	BotID       string          `json:"bot_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Options     []commandOption `json:"options"`
}

type interactionRef struct {
	Name     string `json:"name"`      // the command
	MemberID string `json:"member_id"` // who ran it
}

type interaction struct {
	bot, member, name string
	channel           int64
	expires           time.Time
	replies           int
}

type interactions struct {
	mu   sync.Mutex
	byID map[string]interaction
}

func (c *botCommand) validate() error {
	bad := func(format string, a ...any) error {
		return errf(http.StatusBadRequest, "invalid_command", format, a...)
	}
	if !commandNameRe.MatchString(c.Name) {
		return bad("command names are 1-32 characters: a-z, 0-9, - and _")
	}
	if n := len([]rune(c.Description)); n > 100 {
		return bad("descriptions are at most 100 characters")
	}
	if len(c.Options) > maxCommandOptions {
		return bad("at most %d options", maxCommandOptions)
	}
	seen := map[string]bool{}
	optional := false
	for _, o := range c.Options {
		if !commandNameRe.MatchString(o.Name) || seen[o.Name] || !optionTypes[o.Type] || len([]rune(o.Description)) > 100 {
			return bad("invalid option %q", o.Name)
		}
		if o.Required && optional {
			return bad("required options come first")
		}
		optional = optional || !o.Required
		seen[o.Name] = true
	}
	if c.Options == nil {
		c.Options = []commandOption{}
	}
	return nil
}

// handleSetCommands: PUT /v1/bots/@me/commands [{name, description, options}] (bots only) replaces the list.
func (s *Server) handleSetCommands(w http.ResponseWriter, r *http.Request) {
	me := memberFrom(r)
	if !me.Bot {
		writeErr(w, r, errf(http.StatusForbidden, "bots_only", "only bots declare commands"))
		return
	}
	var list []botCommand
	if err := decode(r, &list); err != nil {
		writeErr(w, r, err)
		return
	}
	if len(list) > maxCommandsPerBot {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_command", "at most %d commands", maxCommandsPerBot))
		return
	}
	names := map[string]bool{}
	for i := range list {
		if err := list[i].validate(); err != nil {
			writeErr(w, r, err)
			return
		}
		if names[list[i].Name] {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_command", "command %q declared twice", list[i].Name))
			return
		}
		names[list[i].Name] = true
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_commands WHERE bot_id = ?`, me.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	for _, c := range list {
		opts, _ := json.Marshal(c.Options)
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_commands (bot_id, name, description, options) VALUES (?, ?, ?, ?)`,
			me.ID, c.Name, c.Description, string(opts)); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	all, err := s.commands(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("COMMANDS_UPDATE", all)
	writeJSON(w, http.StatusOK, all)
}

// commands lists the commands of the bots still members, by bot then name.
func (s *Server) commands(ctx context.Context) ([]botCommand, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.bot_id, c.name, c.description, c.options FROM bot_commands c
		JOIN members m ON m.id = c.bot_id WHERE m.left_at IS NULL ORDER BY c.bot_id, c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []botCommand{}
	for rows.Next() {
		var c botCommand
		var opts string
		if err := rows.Scan(&c.BotID, &c.Name, &c.Description, &opts); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(opts), &c.Options)
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	list, err := s.commands(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleRunCommand: POST /v1/channels/{id}/commands {bot_id, name, options: {name: value}}.
func (s *Server) handleRunCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotID   string                     `json:"bot_id"`
		Name    string                     `json:"name"`
		Options map[string]json.RawMessage `json:"options"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	c, ps, err := s.textChannel(r, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if err := requirePost(ps, me, c, 0); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.messages.Check(me); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	var cmd botCommand
	var opts string
	err = s.db.QueryRowContext(ctx, `SELECT c.bot_id, c.name, c.description, c.options FROM bot_commands c
		JOIN members m ON m.id = c.bot_id WHERE c.bot_id = ? AND c.name = ? AND m.left_at IS NULL`, req.BotID, req.Name).
		Scan(&cmd.BotID, &cmd.Name, &cmd.Description, &opts)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "unknown_command", "no such command"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	json.Unmarshal([]byte(opts), &cmd.Options)
	values, err := s.commandValues(ctx, ps, me, cmd, req.Options)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	// The command must reach the bot now: it answers live.
	if !s.hub.KeyOnline(cmd.BotID) {
		writeErr(w, r, errf(http.StatusConflict, "bot_offline", "this bot is not connected"))
		return
	}
	id := secret.NewID()
	s.cmds.mu.Lock()
	if s.cmds.byID == nil {
		s.cmds.byID = map[string]interaction{}
	}
	now := s.now()
	for k, v := range s.cmds.byID {
		if now.After(v.expires) {
			delete(s.cmds.byID, k)
		}
	}
	s.cmds.byID[id] = interaction{bot: cmd.BotID, member: me, name: cmd.Name, channel: c.ID, expires: now.Add(interactionTTL)}
	s.cmds.mu.Unlock()
	s.hub.BroadcastTo("INTERACTION_CREATE", map[string]any{
		"id": id, "name": cmd.Name, "options": values, "channel_id": c.ID, "member_id": me,
	}, func(key, _ string) bool { return key == cmd.BotID })
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

// commandValues checks the options against the declaration: required ones
// present, types, members still here, channels the member can see.
func (s *Server) commandValues(ctx context.Context, ps *permSnapshot, me string, cmd botCommand, in map[string]json.RawMessage) (map[string]any, error) {
	bad := func(o string) error {
		return errf(http.StatusBadRequest, "invalid_option", "invalid value for option %q", o)
	}
	out := map[string]any{}
	known := map[string]bool{}
	for _, o := range cmd.Options {
		known[o.Name] = true
		raw, ok := in[o.Name]
		if !ok || string(raw) == "null" {
			if o.Required {
				return nil, errf(http.StatusBadRequest, "missing_option", "option %q is required", o.Name)
			}
			continue
		}
		switch o.Type {
		case "string":
			var v string
			if json.Unmarshal(raw, &v) != nil || len([]rune(v)) > 2000 {
				return nil, bad(o.Name)
			}
			out[o.Name] = v
		case "integer":
			var v float64
			if json.Unmarshal(raw, &v) != nil || v != math.Trunc(v) || math.Abs(v) > 1<<53 {
				return nil, bad(o.Name)
			}
			out[o.Name] = int64(v)
		case "boolean":
			var v bool
			if json.Unmarshal(raw, &v) != nil {
				return nil, bad(o.Name)
			}
			out[o.Name] = v
		case "member":
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, bad(o.Name)
			}
			if m, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, v); err != nil || m == nil {
				return nil, bad(o.Name)
			}
			out[o.Name] = v
		case "channel":
			var v json.Number
			if json.Unmarshal(raw, &v) != nil {
				return nil, bad(o.Name)
			}
			id, err := strconv.ParseInt(v.String(), 10, 64)
			if err != nil || ps.inChannel(me, id)&permViewChannel == 0 {
				return nil, bad(o.Name)
			}
			out[o.Name] = id
		}
	}
	for k := range in {
		if !known[k] {
			return nil, errf(http.StatusBadRequest, "unknown_option", "unknown option %q", k)
		}
	}
	return out, nil
}

// handleInteractionReply: POST /v1/interactions/{id}/reply {content, ephemeral} (the bot that got it).
func (s *Server) handleInteractionReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content   string `json:"content"`
		Ephemeral bool   `json:"ephemeral"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	id := r.PathValue("id")
	bot := memberFrom(r).ID
	s.cmds.mu.Lock()
	it, ok := s.cmds.byID[id]
	ok = ok && it.bot == bot && !s.now().After(it.expires)
	tooMany := ok && it.replies >= maxReplies
	if ok && !tooMany {
		it.replies++
		s.cmds.byID[id] = it
	}
	s.cmds.mu.Unlock()
	if !ok {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such interaction (or answered too late)"))
		return
	}
	if tooMany {
		writeErr(w, r, errf(http.StatusTooManyRequests, "too_many_replies", "at most %d replies to one interaction", maxReplies))
		return
	}
	// Ephemeral or not, replies count as the bot's messages (no flooding a member).
	if err := s.limit.messages.Check(bot); err != nil {
		writeErr(w, r, err)
		return
	}
	content, err := validContent(req.Content, false)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if req.Ephemeral {
		s.hub.BroadcastTo("INTERACTION_REPLY", map[string]any{
			"interaction_id": id, "channel_id": it.channel, "bot_id": bot, "name": it.name, "content": content,
		}, func(key, _ string) bool { return key == it.member })
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := channelByID(ctx, s.db, it.channel)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := requirePost(ps, bot, c, 0); err != nil {
		writeErr(w, r, err)
		return
	}
	msg, err := s.storeMessage(ctx, ps, c, bot, content, ps.inChannel(bot, c.ID), func(tx *sql.Tx, mid int64) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO message_interactions (message_id, name, member_id) VALUES (?, ?, ?)`, mid, it.name, it.member)
		return err
	})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

// loadInteractions marks command replies with the command and who ran it.
func (s *Server) loadInteractions(ctx context.Context, in string, args []any, byID map[int64]*message) error {
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, name, member_id FROM message_interactions WHERE message_id IN `+in, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ref interactionRef
		if err := rows.Scan(&id, &ref.Name, &ref.MemberID); err != nil {
			return err
		}
		if m := byID[id]; m != nil {
			m.Interaction = &ref
		}
	}
	return rows.Err()
}
