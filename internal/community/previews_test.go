package community

import "testing"

// A link hidden in a spoiler gets no preview (its title would give it away).
func TestSpoilerLinksNotPreviewed(t *testing.T) {
	got := extractURLs("voir https://a.example/x et ||la fin : https://b.example/y|| puis https://c.example/z")
	if len(got) != 2 || got[0] != "https://a.example/x" || got[1] != "https://c.example/z" {
		t.Fatalf("extractURLs = %v", got)
	}
}
