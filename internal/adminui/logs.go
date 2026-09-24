package adminui

import (
	"strings"
	"sync"
)

// Logs keeps the last lines written to it (the server log), for the journal page.
type Logs struct {
	mu    sync.Mutex
	lines []string
	part  string
	max   int
}

// NewLogs keeps up to max lines.
func NewLogs(max int) *Logs { return &Logs{max: max} }

func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := l.part + string(p)
	parts := strings.Split(text, "\n")
	l.part = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		l.lines = append(l.lines, line)
	}
	if over := len(l.lines) - l.max; over > 0 {
		l.lines = append([]string(nil), l.lines[over:]...)
	}
	return len(p), nil
}

// Lines returns a copy of the kept lines, oldest first.
func (l *Logs) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}
