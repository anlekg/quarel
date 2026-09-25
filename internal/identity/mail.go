package identity

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Mailer sends transactional emails.
type Mailer interface {
	Send(to, subject, body string) error
}

// LogMailer writes emails to the log instead of sending them (development only).
type LogMailer struct{}

func (LogMailer) Send(to, subject, body string) error {
	slog.Warn("email not sent (no SMTP configured)", "to", to, "subject", subject, "body", body)
	return nil
}

// SMTPMailer sends through an SMTP relay: implicit TLS on port 465,
// otherwise STARTTLS (required unless the relay is on the loopback address).
// From may carry a display name ("Quarel <quarel@example.org>").
type SMTPMailer struct {
	Host, Port, User, Password, From string
}

const smtpTimeout = 30 * time.Second

// message builds the email: encoded headers (accents), Date and Message-ID
// (spam filters expect them), quoted-printable UTF-8 body.
func (m SMTPMailer) message(sender *mail.Address, to, subject, body string) ([]byte, error) {
	var id [12]byte
	rand.Read(id[:])
	domain := sender.Address[strings.LastIndex(sender.Address, "@")+1:]
	var b bytes.Buffer
	b.WriteString("From: " + sender.String() + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + hex.EncodeToString(id[:]) + "@" + domain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m SMTPMailer) Send(to, subject, body string) error {
	for _, s := range []string{to, subject} {
		if strings.ContainsAny(s, "\r\n") {
			return fmt.Errorf("mail: header injection attempt")
		}
	}
	sender, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("mail: QUAREL_SMTP_FROM: %w", err)
	}
	msg, err := m.message(sender, to, subject, body)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(m.Host, m.Port)
	dialer := &net.Dialer{Timeout: smtpTimeout}
	var conn net.Conn
	if m.Port == "465" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	conn.SetDeadline(time.Now().Add(smtpTimeout))
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: %w", err)
	}
	defer c.Close()
	if m.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("mail: STARTTLS: %w", err)
			}
		} else if !isLoopbackHost(m.Host) {
			return fmt.Errorf("mail: %s does not offer STARTTLS: refusing to send in clear", m.Host)
		}
	}
	if m.User != "" {
		if err := c.Auth(smtp.PlainAuth("", m.User, m.Password, m.Host)); err != nil {
			return fmt.Errorf("mail: authentication: %w", err)
		}
	}
	if err := c.Mail(sender.Address); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	return c.Quit()
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
