package main

import (
	"testing"
	"time"
)

// A forged edit or deletion (another member targeting someone else's
// message) must not change the history.
func TestApplyOnlyAuthorsEditOrDelete(t *testing.T) {
	e := &e2e{c: &cli{}, st: &e2eStore{UserID: "me", History: map[string][]histMsg{}, Names: map[string]string{}}}
	now := time.Now()
	e.apply(1, megolmPlain{DMID: "g", SenderUser: "bob", Text: "original", SentAt: now})
	if _, line := e.apply(2, megolmPlain{DMID: "g", SenderUser: "mallory", Type: "edit", Target: 1, Text: "falsifié"}); line == "" {
		t.Fatal("forged edit not reported")
	}
	e.apply(3, megolmPlain{DMID: "g", SenderUser: "mallory", Type: "delete", Target: 1})
	if h := e.st.History["g"]; len(h) != 1 || h[0].Text != "original" || h[0].Edited {
		t.Fatalf("history after forged events = %+v", h)
	}
	e.apply(4, megolmPlain{DMID: "g", SenderUser: "bob", Type: "edit", Target: 1, Text: "corrigé"})
	if h := e.st.History["g"]; h[0].Text != "corrigé" || !h[0].Edited {
		t.Fatalf("author's edit not applied: %+v", h)
	}
	e.apply(5, megolmPlain{DMID: "g", SenderUser: "bob", Type: "delete", Target: 1})
	if len(e.st.History["g"]) != 0 {
		t.Fatal("author's deletion not applied")
	}
}

// Ephemeral messages: the timer event sets the conversation's setting,
// messages carrying a ttl expire that long after they were sent (a date in
// the future counts as now), and an unknown duration is refused.
func TestEphemeralMessages(t *testing.T) {
	e := &e2e{c: &cli{}, st: &e2eStore{UserID: "me", History: map[string][]histMsg{}, Names: map[string]string{}}}
	now := time.Now()
	e.apply(1, megolmPlain{DMID: "g", SenderUser: "bob", Type: "timer", TTL: 300, SentAt: now.Add(-time.Hour)})
	if e.timerOf("g") != 300 {
		t.Fatalf("timer = %d", e.timerOf("g"))
	}
	if _, line := e.apply(2, megolmPlain{DMID: "g", SenderUser: "bob", Type: "timer", TTL: 42, SentAt: now}); line == "" || e.timerOf("g") != 300 {
		t.Fatal("unknown duration accepted")
	}
	e.apply(3, megolmPlain{DMID: "g", SenderUser: "bob", Text: "durable", SentAt: now.Add(-50 * time.Minute)})
	e.apply(4, megolmPlain{DMID: "g", SenderUser: "bob", Text: "déjà parti", TTL: 300, SentAt: now.Add(-10 * time.Minute)})
	e.apply(5, megolmPlain{DMID: "g", SenderUser: "bob", Text: "éphémère", TTL: 300, SentAt: now.Add(-time.Minute)})
	e.apply(6, megolmPlain{DMID: "g", SenderUser: "bob", Text: "du futur", TTL: 300, SentAt: now.Add(time.Hour)})
	if n := len(e.st.History["g"]); n != 4 { // timer, durable, éphémère, du futur
		t.Fatalf("history = %+v", e.st.History["g"])
	}
	// Never in the backup nor in the history sent to a new device.
	if d := durableHistory(e.st.History)["g"]; len(d) != 2 || d[1].Text != "durable" {
		t.Fatalf("durable history = %+v", d)
	}
	e.purgeExpired(now.Add(3 * time.Minute))
	if n := len(e.st.History["g"]); n != 4 {
		t.Fatalf("purged too early: %+v", e.st.History["g"])
	}
	e.purgeExpired(now.Add(6 * time.Minute))
	h := e.st.History["g"]
	if len(h) != 2 || h[0].Timer == nil || h[1].Text != "durable" {
		t.Fatalf("after expiry = %+v", h)
	}
}
