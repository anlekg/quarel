package community

import (
	"embed"
	"io/fs"
	"net/http"
)

// The voice test page is a throwaway tool for testing voice before the real
// client exists. It is served at /voice-test/ and authenticates with a
// session token passed in the URL fragment (quarelctl voice-test prints it).
//
//go:embed voicetest
var voiceTestFiles embed.FS

var voiceTestPage = func() http.Handler {
	sub, err := fs.Sub(voiceTestFiles, "voicetest")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/voice-test/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		files.ServeHTTP(w, r)
	})
}()
