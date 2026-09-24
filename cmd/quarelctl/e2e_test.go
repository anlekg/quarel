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
