package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// fakeDLQ implements event.DeadLetterStore in-memory for the handler test.
// Only the methods the handler actually calls are non-trivial; the rest are
// no-ops because the handler doesn't drive them.
type fakeDLQ struct {
	items    []event.DeadLetteredRecord
	pending  int
	requeued []uuid.UUID
}

func (f *fakeDLQ) IncrementAttempt(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeDLQ) DeadLetter(_ context.Context, _ uuid.UUID, _ string) error { return nil }
func (f *fakeDLQ) Requeue(_ context.Context, id uuid.UUID) error {
	f.requeued = append(f.requeued, id)
	return nil
}
func (f *fakeDLQ) ListDeadLettered(_ context.Context, limit, offset int) ([]event.DeadLetteredRecord, error) {
	if offset >= len(f.items) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.items) {
		end = len(f.items)
	}
	return f.items[offset:end], nil
}
func (f *fakeDLQ) PendingCount(_ context.Context) (int, error) { return f.pending, nil }

func TestOutboxAdminHandler_ListReturnsRecordsAndPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dlq := &fakeDLQ{
		pending: 7,
		items: []event.DeadLetteredRecord{
			{
				Record: event.Record{
					ID:        uuid.New(),
					Name:      "booking.requested",
					Payload:   []byte(`{"bookingId":"x"}`),
					CreatedAt: time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
					Attempts:  5,
				},
				DeadLetteredAt: time.Date(2026, 6, 4, 10, 5, 0, 0, time.UTC),
				LastError:      "exceeded max attempts (5)",
			},
		},
	}
	h := handler.NewOutboxAdminHandler(dlq)
	r := gin.New()
	r.GET("/admin/outbox/dlq", h.List)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/outbox/dlq", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var body struct {
		Items []struct {
			EventName string `json:"eventName"`
			Attempts  int    `json:"attempts"`
			LastError string `json:"lastError"`
		} `json:"items"`
		Pending int `json:"pending"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, w.Body.String())
	}
	if body.Pending != 7 {
		t.Fatalf("pending = %d, want 7", body.Pending)
	}
	if len(body.Items) != 1 || body.Items[0].EventName != "booking.requested" || body.Items[0].Attempts != 5 {
		t.Fatalf("items = %+v, want one booking.requested with 5 attempts", body.Items)
	}
	if !strings.Contains(body.Items[0].LastError, "max attempts") {
		t.Fatalf("lastError = %q, want it to mention 'max attempts'", body.Items[0].LastError)
	}
}

func TestOutboxAdminHandler_RequeueReturns204AndCallsStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dlq := &fakeDLQ{}
	h := handler.NewOutboxAdminHandler(dlq)
	r := gin.New()
	r.POST("/admin/outbox/dlq/:id/requeue", h.Requeue)

	id := uuid.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/outbox/dlq/"+id.String()+"/requeue", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", w.Code, w.Body.String())
	}
	if len(dlq.requeued) != 1 || dlq.requeued[0] != id {
		t.Fatalf("requeued = %v, want [%s]", dlq.requeued, id)
	}
}

func TestOutboxAdminHandler_RequeueRejectsBadID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dlq := &fakeDLQ{}
	h := handler.NewOutboxAdminHandler(dlq)
	r := gin.New()
	r.POST("/admin/outbox/dlq/:id/requeue", h.Requeue)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/outbox/dlq/not-a-uuid/requeue", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
	if len(dlq.requeued) != 0 {
		t.Fatalf("requeued = %v, want none on bad id", dlq.requeued)
	}
}
