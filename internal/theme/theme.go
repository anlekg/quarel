// Package theme validates the cosmetic themes people give their profiles and
// community servers: a few colours, a gradient, a font of the app and some
// CSS. The server stores and relays them; the apps that display them filter
// the CSS again (client/src/lib/themecss.ts), since a server is not trusted.
// This check only refuses early what no app would apply: oversized themes,
// unknown keys, and CSS that could load something from elsewhere.
package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxCSS is the size of a theme's CSS, in bytes.
const MaxCSS = 16 << 10

// Colors a theme may set: the app's colour variables (--bg, --accent…).
var Colors = map[string]bool{
	"bg_deep": true, "bg": true, "bg_1": true, "bg_2": true, "bg_3": true, "line": true,
	"text": true, "text_2": true, "text_3": true, "accent": true, "accent_ink": true, "accent_text": true,
}

// Fonts are those of the app (bundled, or the system's generic families).
var Fonts = map[string]bool{"manrope": true, "space-grotesk": true, "system": true, "serif": true, "mono": true}

// Theme is the JSON of a theme; every field is optional.
type Theme struct {
	Colors   map[string]string `json:"colors,omitempty"`
	Gradient *Gradient         `json:"gradient,omitempty"`
	Font     string            `json:"font,omitempty"`
	CSS      string            `json:"css,omitempty"`
}

// Gradient is a linear gradient behind the themed area.
type Gradient struct {
	Angle int      `json:"angle"`
	Stops []string `json:"stops"`
}

var (
	hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	// Anything that would fetch a resource or hide what is written.
	forbiddenCSS = regexp.MustCompile(`(?i)url\s*\(|image-set|image\s*\(|element\s*\(|src\s*\(|@import|@font-face|@namespace|expression\s*\(|javascript:|\\|<`)
)

// ErrInvalid wraps every refusal; its message says what is wrong (in English, for the API).
var ErrInvalid = errors.New("invalid theme")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Normalize checks a theme and returns its canonical JSON ("" for an empty
// theme, which removes it).
func Normalize(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var t Theme
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return "", invalid("%v", err)
	}
	for k, v := range t.Colors {
		if !Colors[k] {
			return "", invalid("unknown colour %q", k)
		}
		if !hexColor.MatchString(v) {
			return "", invalid("colour %s must be #RGB, #RRGGBB or #RRGGBBAA", k)
		}
	}
	if len(t.Colors) == 0 {
		t.Colors = nil
	}
	if g := t.Gradient; g != nil {
		if g.Angle < 0 || g.Angle > 360 || len(g.Stops) < 2 || len(g.Stops) > 4 {
			return "", invalid("a gradient has an angle of 0-360 and 2 to 4 colours")
		}
		for _, c := range g.Stops {
			if !hexColor.MatchString(c) {
				return "", invalid("gradient colours must be #RGB, #RRGGBB or #RRGGBBAA")
			}
		}
	}
	if t.Font != "" && !Fonts[t.Font] {
		return "", invalid("unknown font %q", t.Font)
	}
	t.CSS = strings.TrimSpace(t.CSS)
	if len(t.CSS) > MaxCSS {
		return "", invalid("CSS is limited to %d KB", MaxCSS>>10)
	}
	if m := forbiddenCSS.FindString(t.CSS); m != "" {
		return "", invalid("CSS cannot contain %q (no external resources, imports or escapes)", m)
	}
	if t.Colors == nil && t.Gradient == nil && t.Font == "" && t.CSS == "" {
		return "", nil
	}
	out, err := json.Marshal(t)
	return string(out), err
}
