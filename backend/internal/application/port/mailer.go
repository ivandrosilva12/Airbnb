package port

import "context"

// Email is a transactional message to deliver. HTML is the rich body; Text is
// the plaintext fallback for clients that cannot render HTML. A mailer sends
// both as a multipart/alternative message.
type Email struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Mailer is an outbound port for sending transactional emails. The application
// depends on it; infrastructure provides the implementation (SMTP, a dev logger,
// or a recording fake in tests).
type Mailer interface {
	Send(ctx context.Context, msg Email) error
}
