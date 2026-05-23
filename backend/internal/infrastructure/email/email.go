// Package email provides Mailer implementations: an SMTP sender for production,
// a logging mailer for local development (so the app runs without SMTP), and a
// recording mailer for tests.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
	"github.com/google/uuid"
)

// NewMailer returns an SMTP mailer when a host is configured, otherwise a
// logging mailer that records the message without sending it.
func NewMailer(cfg config.EmailConfig) port.Mailer {
	if cfg.SMTPHost != "" {
		slog.Info("email: using SMTP mailer", "host", cfg.SMTPHost)
		return &SMTPMailer{cfg: cfg}
	}
	slog.Info("email: SMTP not configured, using log mailer")
	return &LogMailer{from: cfg.FromAddress}
}

// LogMailer logs emails instead of sending them.
type LogMailer struct{ from string }

// Send logs the email at info level.
func (m *LogMailer) Send(_ context.Context, msg port.Email) error {
	slog.Info("email (log mailer; not delivered)", "from", m.from, "to", msg.To, "subject", msg.Subject)
	return nil
}

// SMTPMailer sends email via an SMTP server using the standard library.
type SMTPMailer struct{ cfg config.EmailConfig }

// Send delivers a multipart/alternative email (text + HTML) over SMTP. When no
// SMTP user is configured (e.g. a local MailHog), it connects without auth.
func (m *SMTPMailer) Send(_ context.Context, msg port.Email) error {
	addr := fmt.Sprintf("%s:%s", m.cfg.SMTPHost, m.cfg.SMTPPort)
	raw := buildMessage(m.cfg.FromAddress, msg)

	var auth smtp.Auth
	if m.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPassword, m.cfg.SMTPHost)
	}
	if err := smtp.SendMail(addr, auth, m.cfg.FromAddress, []string{msg.To}, raw); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}

// buildMessage renders an RFC822 message. With an HTML body it produces a
// multipart/alternative so clients pick HTML or fall back to text; otherwise a
// plain text/plain message.
func buildMessage(from string, msg port.Email) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + msg.Subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(msg.Text)
		return []byte(b.String())
	}

	boundary := "airhost-" + uuid.NewString()
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(msg.Text + "\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(msg.HTML + "\r\n\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// Sent is a captured email (used by RecordingMailer).
type Sent struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// RecordingMailer captures emails in memory for tests and local previews.
type RecordingMailer struct {
	mu   sync.Mutex
	sent []Sent
}

// NewRecordingMailer builds an empty RecordingMailer.
func NewRecordingMailer() *RecordingMailer { return &RecordingMailer{} }

// Send records the email.
func (m *RecordingMailer) Send(_ context.Context, msg port.Email) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, Sent{To: msg.To, Subject: msg.Subject, HTML: msg.HTML, Text: msg.Text})
	return nil
}

// Sent returns a copy of the captured emails.
func (m *RecordingMailer) Sent() []Sent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Sent, len(m.sent))
	copy(out, m.sent)
	return out
}
