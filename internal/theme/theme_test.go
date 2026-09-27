package theme

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		``:     ``,
		`null`: ``,
		`{}`:   ``,
		`{"colors":{"accent":"#fa0","bg":"#11131780"},"font":"serif"}`:                `{"colors":{"accent":"#fa0","bg":"#11131780"},"font":"serif"}`,
		`{"gradient":{"angle":135,"stops":["#000","#fff"]},"css":"  .x{color:red} "}`: `{"gradient":{"angle":135,"stops":["#000","#fff"]},"css":".x{color:red}"}`,
		`{"colors":{}}`: ``,
	}
	for in, want := range ok {
		got, err := Normalize([]byte(in))
		if err != nil || got != want {
			t.Errorf("Normalize(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		`{"colors":{"danger":"#fff"}}`,
		`{"colors":{"bg":"red"}}`,
		`{"colors":{"bg":"#12345"}}`,
		`{"gradient":{"angle":400,"stops":["#000","#fff"]}}`,
		`{"gradient":{"angle":0,"stops":["#000"]}}`,
		`{"font":"comic-sans"}`,
		`{"extra":1}`,
		`{"css":".x{background:url(https://example.org/a.png)}"}`,
		`{"css":".x{background:URL (x)}"}`,
		`{"css":"@import 'x.css';"}`,
		`{"css":".x{background:u\\72l(x)}"}`,
		`{"css":".x{background:image-set('a.png' 1x)}"}`,
		`{"css":"</style><script>"}`,
		`{"css":"@font-face{font-family:x}"}`,
		`{"css":"` + strings.Repeat("a", MaxCSS+1) + `"}`,
	}
	for _, in := range bad {
		if _, err := Normalize([]byte(in)); !errors.Is(err, ErrInvalid) {
			t.Errorf("Normalize(%.60s) accepted", in)
		}
	}
}
