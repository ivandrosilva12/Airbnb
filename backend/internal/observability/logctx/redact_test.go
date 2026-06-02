package logctx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// runLog returns the JSON object emitted by a single log call through
// the redacting handler wrapping a JSON handler — the same composition
// production uses. We assert against the parsed map so attribute order
// doesn't matter.
func runLog(t *testing.T, fn func(*slog.Logger)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	h := NewRedactingHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	fn(slog.New(h))
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode log line: %v\n%s", err, buf.String())
	}
	return out
}

func TestRedact_EmailInAttribute_KeepsDomain(t *testing.T) {
	got := runLog(t, func(l *slog.Logger) {
		l.Info("send failed", "to", "alice@airhost.dev")
	})
	if got["to"] != "***@airhost.dev" {
		t.Fatalf("to = %q, want ***@airhost.dev", got["to"])
	}
}

func TestRedact_EmailInMessage(t *testing.T) {
	got := runLog(t, func(l *slog.Logger) {
		l.Warn("delivery bounced for bob@example.com after 3 retries")
	})
	msg := got["msg"].(string)
	if !strings.Contains(msg, "***@example.com") || strings.Contains(msg, "bob@example.com") {
		t.Fatalf("msg = %q, expected raw email scrubbed", msg)
	}
}

func TestRedact_IPv4_MasksLastOctet(t *testing.T) {
	got := runLog(t, func(l *slog.Logger) {
		l.Info("request from", "client_ip", "203.0.113.42")
	})
	if got["client_ip"] != "203.0.113.***" {
		t.Fatalf("client_ip = %q, want 203.0.113.***", got["client_ip"])
	}
}

func TestRedact_CardNumber_KeepsLast4(t *testing.T) {
	cases := map[string]string{
		"4111111111111111":      "****1111",
		"4111-1111-1111-1111":   "****1111",
		"3782 822463 10005":     "****0005", // Amex 15 digits with separators
	}
	for in, want := range cases {
		got := runLog(t, func(l *slog.Logger) {
			l.Info("charge", "pan", in)
		})
		if got["pan"] != want {
			t.Fatalf("pan(%q) = %q, want %q", in, got["pan"], want)
		}
	}
}

func TestRedact_JWT_Token_Replaced(t *testing.T) {
	// Realistic JWT shape: header.payload.signature, all base64url.
	token := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIn0.AbCdEfGhIjK_LMnoPQrsTUVwXyZ"
	got := runLog(t, func(l *slog.Logger) {
		l.Warn("auth failed", "token", token)
	})
	if got["token"] != "***jwt***" {
		t.Fatalf("token = %q, want ***jwt***", got["token"])
	}
}

func TestRedact_ErrorValue_AlsoScanned(t *testing.T) {
	// Errors often carry the offending email in their message — e.g.
	// "user alice@x.com not found". slog stores them under KindAny,
	// not KindString, so the redactor has to unwrap.
	got := runLog(t, func(l *slog.Logger) {
		l.Error("lookup", "error", errors.New("user alice@x.com not found"))
	})
	s, _ := got["error"].(string)
	if !strings.Contains(s, "***@x.com") || strings.Contains(s, "alice@x.com") {
		t.Fatalf("error attr = %q, expected raw email scrubbed", s)
	}
}

func TestRedact_WithAttrs_RedactsAtConfigureTime(t *testing.T) {
	// logger.With("email", x) stores the value once; subsequent records
	// emit that stored value. Without redacting at WithAttrs the bound
	// email would leak into every record.
	var buf bytes.Buffer
	h := NewRedactingHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	scoped := slog.New(h).With("user_email", "bound@airhost.dev")
	scoped.Info("hello")
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["user_email"] != "***@airhost.dev" {
		t.Fatalf("user_email = %q, want ***@airhost.dev", out["user_email"])
	}
}

func TestRedact_NoMatch_PassesThrough(t *testing.T) {
	// Fast-path: a line with no PII patterns should serialize identically
	// to one written by the inner handler directly. (We don't compare
	// bytes, just the visible payload.)
	got := runLog(t, func(l *slog.Logger) {
		l.Info("booking confirmed", "booking_id", "abc-123", "nights", 4)
	})
	if got["booking_id"] != "abc-123" {
		t.Fatalf("booking_id mangled: %v", got["booking_id"])
	}
	if got["nights"] != float64(4) {
		t.Fatalf("nights mangled: %v", got["nights"])
	}
	if got["msg"] != "booking confirmed" {
		t.Fatalf("msg mangled: %v", got["msg"])
	}
}

func TestRedact_GroupedAttrs_Walked(t *testing.T) {
	// slog.Group("user", "email", x, "id", y) — the redactor needs to
	// recurse into groups, otherwise structured logging is a back door.
	var buf bytes.Buffer
	h := NewRedactingHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.New(h).Info("evt", slog.Group("user", "email", "g@airhost.dev", "id", "u1"))
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	u, _ := out["user"].(map[string]any)
	if u["email"] != "***@airhost.dev" {
		t.Fatalf("nested email leaked: %v", u["email"])
	}
	if u["id"] != "u1" {
		t.Fatalf("non-PII attr mangled inside group: %v", u["id"])
	}
}

// Sanity: the handler still respects the level filter. Otherwise wrapping
// it could accidentally bypass `Level: slog.LevelInfo` and start logging
// debug noise.
func TestRedact_DelegatesEnabledToInner(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewRedactingHandler(inner)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatalf("Info enabled despite inner level=Warn")
	}
	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatalf("Warn disabled despite inner level=Warn")
	}
}
