package logctx

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// PII redaction patterns. Kept package-private and pre-compiled so the
// handler hot-path doesn't pay a regexp.MustCompile cost per record.
//
// Goals (in priority order):
//   - Strip enough that a leaked log dump doesn't expose user identity
//     under GDPR scrutiny.
//   - Keep enough context that operators can still debug: the domain of
//     an email tells you what tenant/host; /24 of an IP tells you the
//     region; the last-4 of a card tells you which card on the user's
//     account; the prefix of a token tells you the issuer.
//   - Apply uniformly: any string value passing through slog gets
//     scanned, regardless of attribute key. We do NOT keylist because
//     services emit ad-hoc keys ("recipient", "to", "u.Email"…) that
//     would be missed.
//
// Trade-off: matching is regex-based, not parse-based. A literal that
// happens to look like an email inside a longer error string IS
// redacted; this errs on the side of over-redaction (correct for
// privacy).
var (
	// RFC 5321/5322 simplified: local + @ + domain with at least one dot.
	// Anchored with word boundaries so substrings inside other tokens
	// don't get partial matches.
	emailRE = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)

	// IPv4 dotted quad. We deliberately don't try IPv6 — its variability
	// makes regex-based detection unreliable and the privacy gain
	// marginal until v6 becomes the dominant log source.
	ipv4RE = regexp.MustCompile(`\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b`)

	// 13–19 contiguous digits, optionally separated by spaces or dashes.
	// Covers Visa, Mastercard, Amex, Discover, JCB, Diners, UnionPay.
	// We don't validate the Luhn checksum (overhead per log record)
	// and accept some false positives (long order numbers) — better
	// safe than sorry.
	cardRE = regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)

	// JWT-shape: three base64url-safe sections joined by dots. Catches
	// virtually every Bearer access token we deal with (Keycloak issues
	// these). Not anchored to "Bearer " because tokens leak into error
	// strings stripped of their header.
	jwtRE = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
)

// redactString scrubs every known PII pattern from s. Returns the
// original string when no pattern matches (cheap fast-path for the
// vast majority of log lines that carry no PII at all).
func redactString(s string) string {
	if s == "" {
		return s
	}
	// Order matters: JWT first (longest, most distinctive) so its dots
	// aren't mis-matched by the IPv4 regex.
	if jwtRE.MatchString(s) {
		s = jwtRE.ReplaceAllString(s, "***jwt***")
	}
	if cardRE.MatchString(s) {
		s = cardRE.ReplaceAllStringFunc(s, func(match string) string {
			// Keep only digits to compute the last-4 deterministically;
			// the separators are noise for the masked output.
			digits := strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, match)
			if len(digits) < 4 {
				return "****"
			}
			return "****" + digits[len(digits)-4:]
		})
	}
	if emailRE.MatchString(s) {
		s = emailRE.ReplaceAllStringFunc(s, func(match string) string {
			at := strings.LastIndex(match, "@")
			if at < 0 {
				return "***"
			}
			return "***@" + match[at+1:]
		})
	}
	if ipv4RE.MatchString(s) {
		s = ipv4RE.ReplaceAllString(s, "$1.$2.$3.***")
	}
	return s
}

// redactValue rewrites a slog.Value's PII in-place semantics: returns a
// fresh Value when redacted, the original Value otherwise (allowing the
// handler to skip allocating when nothing changed). Walks groups
// recursively so a structured `slog.Group("user", ...)` is still
// scrubbed.
func redactValue(v slog.Value) slog.Value {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		r := redactString(s)
		if r == s {
			return v
		}
		return slog.StringValue(r)
	case slog.KindGroup:
		attrs := v.Group()
		out := make([]slog.Attr, 0, len(attrs))
		changed := false
		for _, a := range attrs {
			nv := redactValue(a.Value)
			if nv.Equal(a.Value) {
				out = append(out, a)
				continue
			}
			changed = true
			out = append(out, slog.Attr{Key: a.Key, Value: nv})
		}
		if !changed {
			return v
		}
		return slog.GroupValue(out...)
	case slog.KindAny:
		// Inspect string-shaped Any values (errors, fmt.Stringers, raw
		// strings buried in interface{}). We only handle the common
		// case — anything fancier (slices of strings, custom structs)
		// would need reflective walking; out of scope for this slice
		// and rarely a PII source.
		if a, ok := v.Any().(string); ok {
			r := redactString(a)
			if r != a {
				return slog.StringValue(r)
			}
		}
		if err, ok := v.Any().(error); ok {
			s := err.Error()
			r := redactString(s)
			if r != s {
				return slog.StringValue(r)
			}
		}
		return v
	default:
		return v
	}
}

// redactingHandler wraps an inner slog.Handler and redacts PII from
// every record's attributes (including those attached via WithAttrs)
// and the message itself. Cheap for log lines without PII: the
// fast-path is two regex MatchString calls per string value.
type redactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler wraps h with PII redaction. Use this between
// slog.NewJSONHandler and slog.SetDefault so every log line in the
// process is scrubbed without per-call-site discipline.
func NewRedactingHandler(h slog.Handler) slog.Handler {
	return &redactingHandler{inner: h}
}

func (r *redactingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return r.inner.Enabled(ctx, lvl)
}

func (r *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	// The record's message is itself a string, scan it too. Errors
	// rendered into the message ("failed to send to alice@x.com") are
	// the most common leak.
	rec.Message = redactString(rec.Message)

	// Clone the record so we don't mutate caller-visible attrs; AddAttrs
	// is the only way to add to a Record without rebuilding it, so we
	// rebuild via NewRecord.
	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(slog.Attr{Key: a.Key, Value: redactValue(a.Value)})
		return true
	})
	return r.inner.Handle(ctx, out)
}

func (r *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Redact at the With() call site too — otherwise something like
	// `logger.With("email", u.Email)` would bake the raw email into
	// every subsequent record without re-scanning.
	cleaned := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		cleaned[i] = slog.Attr{Key: a.Key, Value: redactValue(a.Value)}
	}
	return &redactingHandler{inner: r.inner.WithAttrs(cleaned)}
}

func (r *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: r.inner.WithGroup(name)}
}
