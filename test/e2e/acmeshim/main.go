// Command acmeshim sits between an ACME client and Pebble for tests. Pebble
// omits the Location header on finalize responses, which golang.org/x/crypto/acme
// needs to poll a "processing" order (Let's Encrypt sends it); the shim adds it.
// It also rewrites Pebble's own URLs so every request goes through the shim.
//
// Usage: acmeshim <listen addr> <pebble base URL> <cert> <key> <pebble CA>
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
)

func main() {
	listen, upstream, certFile, keyFile, caFile := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	target, _ := url.Parse(upstream)
	self := "https://" + listen
	pem, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // JWS "url" must match the URL the client signed
		},
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
		ModifyResponse: func(res *http.Response) error {
			body, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			res.Body.Close()
			body = bytes.ReplaceAll(body, []byte(upstream), []byte(self))
			res.Body = io.NopCloser(bytes.NewReader(body))
			res.ContentLength = int64(len(body))
			res.Header.Set("Content-Length", strconv.Itoa(len(body)))
			for _, h := range []string{"Location", "Link"} {
				if v := res.Header.Values(h); len(v) > 0 {
					res.Header.Del(h)
					for _, x := range v {
						res.Header.Add(h, strings.ReplaceAll(x, upstream, self))
					}
				}
			}
			if id, ok := strings.CutPrefix(res.Request.URL.Path, "/finalize-order/"); ok && res.Header.Get("Location") == "" {
				res.Header.Set("Location", self+"/my-order/"+id)
			}
			return nil
		},
	}
	log.Fatal(http.ListenAndServeTLS(listen, certFile, keyFile, proxy))
}
