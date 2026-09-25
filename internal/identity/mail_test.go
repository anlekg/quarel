package identity

import (
	"bufio"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
)

// fakeSMTP accepts one message on the loopback address (no TLS: allowed there only).
func fakeSMTP(t *testing.T) (port string, got chan string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	got = make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { c.Write([]byte(s + "\r\n")) }
		w("220 test ESMTP")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"):
				w("250 test")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				w("250 ok")
			case cmd == "DATA":
				w("354 go")
				for {
					l, _ := r.ReadString('\n')
					if l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				w("250 queued")
			case cmd == "QUIT":
				w("221 bye")
				got <- data.String()
				return
			}
		}
	}()
	_, port, _ = net.SplitHostPort(l.Addr().String())
	return port, got
}

func TestSMTPMailer(t *testing.T) {
	port, got := fakeSMTP(t)
	m := SMTPMailer{Host: "127.0.0.1", Port: port, From: "Quarel <no-reply@quarel.app>"}
	if err := m.Send("alice@example.com", "Vérifiez votre adresse", "Votre code : 123456\nÀ bientôt."); err != nil {
		t.Fatal(err)
	}
	raw := <-got
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subject, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Vérifiez votre adresse" || msg.Header.Get("Date") == "" || !strings.HasSuffix(msg.Header.Get("Message-Id"), "@quarel.app>") {
		t.Fatalf("headers: %v", msg.Header)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if !strings.Contains(string(body), "À bientôt.") || !strings.Contains(string(body), "123456") {
		t.Fatalf("body: %q", body)
	}
	if err := (SMTPMailer{Host: "127.0.0.1", Port: port, From: "Quarel <no-reply@quarel.app>"}).Send("a@b.c", "x\r\nBcc: evil@x", "b"); err == nil {
		t.Fatal("header injection accepted")
	}
}
