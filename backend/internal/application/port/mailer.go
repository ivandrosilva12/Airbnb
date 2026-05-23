package port

import "context"

// Mailer is an outbound port for sending transactional emails. The application
// depends on it; infrastructure provides the implementation (SMTP, a dev logger,
// or a recording fake in tests).
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}
