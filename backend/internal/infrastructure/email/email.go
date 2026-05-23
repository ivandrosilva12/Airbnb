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
func (m *LogMailer) Send(_ context.Context, to, subject, _ string) error {
	slog.Info("email (log mailer; not delivered)", "from", m.from, "to", to, "subject", subject)
	return nil
}

// SMTPMailer sends email via an SMTP server using the standard library.
type SMTPMailer struct{ cfg config.EmailConfig }

// Send delivers a plain-text email over SMTP. When no SMTP user is configured
// (e.g. a local MailHog), it connects without authentication.
func (m *SMTPMailer) Send(_ context.Context, to, subject, body string) error {
	addr := fmt.Sprintf("%s:%s", m.cfg.SMTPHost, m.cfg.SMTPPort)
	msg := buildMessage(m.cfg.FromAddress, to, subject, body)

	var auth smtp.Auth
	if m.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPassword, m.cfg.SMTPHost)
	}
	if err := smtp.SendMail(addr, auth, m.cfg.FromAddress, []string{to}, msg); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// Sent is a captured email (used by RecordingMailer).
type Sent struct {
	To      string
	Subject string
	Body    string
}

// RecordingMailer captures emails in memory for tests and local previews.
type RecordingMailer struct {
	mu   sync.Mutex
	sent []Sent
}

// NewRecordingMailer builds an empty RecordingMailer.
func NewRecordingMailer() *RecordingMailer { return &RecordingMailer{} }

// Send records the email.
func (m *RecordingMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, Sent{To: to, Subject: subject, Body: body})
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
