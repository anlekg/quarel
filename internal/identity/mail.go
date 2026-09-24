package identity

import (
	"fmt"
	"log/slog"
	"net/mail"
	"net/smtp"
	"strings"
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

// SMTPMailer sends through an SMTP relay (STARTTLS when offered). From may
// carry a display name ("Quarel <quarel@example.org>").
type SMTPMailer struct {
	Host, Port, User, Password, From string
}

func (m SMTPMailer) Send(to, subject, body string) error {
	for _, s := range []string{to, subject} {
		if strings.ContainsAny(s, "\r\n") {
			return fmt.Errorf("mail: header injection attempt")
		}
	}
	msg := "From: " + m.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	var auth smtp.Auth
	if m.User != "" {
		auth = smtp.PlainAuth("", m.User, m.Password, m.Host)
	}
	sender, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("mail: QUAREL_SMTP_FROM: %w", err)
	}
	return smtp.SendMail(m.Host+":"+m.Port, auth, sender.Address, []string{to}, []byte(msg))
}
