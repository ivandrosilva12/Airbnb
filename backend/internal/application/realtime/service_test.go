package realtimeapp_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	realtimeapp "github.com/airhost/backend/internal/application/realtime"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type capture struct {
	UserID  uuid.UUID
	Payload string
}

type fakeBroadcaster struct {
	mu   sync.Mutex
	sent []capture
}

func (f *fakeBroadcaster) Publish(userID uuid.UUID, payload string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, capture{UserID: userID, Payload: payload})
}

func TestEventHandler_PushesToAffectedUser(t *testing.T) {
	ctx := context.Background()
	hub := &fakeBroadcaster{}
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(realtimeapp.NewService(hub).EventHandler())

	host := uuid.New()
	guest := uuid.New()
	conv := uuid.New()

	dispatcher.Publish(ctx, event.BookingRequested{HostID: host, GuestID: guest})
	dispatcher.Publish(ctx, event.BookingConfirmed{GuestID: guest})
	dispatcher.Publish(ctx, event.MessageSent{ConversationID: conv, SenderID: guest, RecipientID: host})

	want := []capture{
		{UserID: host, Payload: `{"type":"notification"}`},
		{UserID: guest, Payload: `{"type":"notification"}`},
		{UserID: host, Payload: `{"type":"message","conversationId":"` + conv.String() + `"}`},
	}
	if len(hub.sent) != len(want) {
		t.Fatalf("sent %d updates, want %d (%+v)", len(hub.sent), len(want), hub.sent)
	}
	for i, w := range want {
		if hub.sent[i] != w {
			t.Fatalf("update[%d] = %+v, want %+v", i, hub.sent[i], w)
		}
	}
}

func TestEventHandler_CancellationNotifiesOtherParty(t *testing.T) {
	ctx := context.Background()
	hub := &fakeBroadcaster{}
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(realtimeapp.NewService(hub).EventHandler())

	host := uuid.New()
	guest := uuid.New()
	// Guest cancels → the host is notified.
	dispatcher.Publish(ctx, event.BookingCancelled{HostID: host, GuestID: guest, CancelledBy: guest})

	if len(hub.sent) != 1 || hub.sent[0].UserID != host {
		t.Fatalf("cancellation push = %+v, want one to the host", hub.sent)
	}
}

// S86 — ExperienceBooking events fan out the same way:
// Created → host; Confirmed → guest; Cancelled → the other party.
func TestEventHandler_ExperienceBookingPushes(t *testing.T) {
	ctx := context.Background()
	hub := &fakeBroadcaster{}
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(realtimeapp.NewService(hub).EventHandler())

	host := uuid.New()
	guest := uuid.New()

	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: uuid.New(), ExperienceID: uuid.New(),
		HostID: host, GuestID: guest, TotalCents: 6600, Currency: "EUR",
	})
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingConfirmed{
		BookingID: uuid.New(), ExperienceID: uuid.New(), HostID: host, GuestID: guest,
	})
	// guest cancels → host is pushed
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID: uuid.New(), ExperienceID: uuid.New(),
		HostID: host, GuestID: guest, CancelledBy: guest,
	})

	want := []capture{
		{UserID: host, Payload: `{"type":"notification"}`},
		{UserID: guest, Payload: `{"type":"notification"}`},
		{UserID: host, Payload: `{"type":"notification"}`},
	}
	if len(hub.sent) != len(want) {
		t.Fatalf("sent %d updates, want %d (%+v)", len(hub.sent), len(want), hub.sent)
	}
	for i, w := range want {
		if hub.sent[i] != w {
			t.Fatalf("update[%d] = %+v, want %+v", i, hub.sent[i], w)
		}
	}
}

// TestEventHandler_SplitPaymentCompletedPushes covers WF-GAP-011 on the
// realtime side: every payer (organizer + others) gets a notification-refresh
// hint when their split fully funds, so their UI lights up without polling.
func TestEventHandler_SplitPaymentCompletedPushes(t *testing.T) {
	ctx := context.Background()
	bookingRepo := memory.NewBookingRepository()
	splitRepo := memory.NewSplitPaymentRepository()

	organizerID := uuid.New()
	payerID := uuid.New()
	bookingID := uuid.New()

	if err := bookingRepo.Create(ctx, &booking.Booking{
		ID: bookingID, PropertyID: uuid.New(), GuestID: organizerID,
		Status: booking.StatusConfirmed, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	splitID := uuid.New()
	now := time.Now().UTC()
	if err := splitRepo.Create(ctx, &splitpayment.SplitPayment{
		ID: splitID, BookingID: bookingID, OrganizerID: organizerID,
		Currency: "EUR", TotalCents: 10000, Status: splitpayment.StatusCompleted,
		Shares: []splitpayment.Share{
			{ID: uuid.New(), SplitPaymentID: splitID, PayerEmail: "a@x", PayerUserID: &organizerID, AmountCents: 5000, Status: splitpayment.SharePaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now},
			{ID: uuid.New(), SplitPaymentID: splitID, PayerEmail: "b@x", PayerUserID: &payerID, AmountCents: 5000, Status: splitpayment.SharePaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now},
		},
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}); err != nil {
		t.Fatalf("seed split: %v", err)
	}

	hub := &fakeBroadcaster{}
	svc := realtimeapp.NewService(hub).
		WithBookings(bookingRepo).
		WithSplitPayments(splitRepo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	dispatcher.Publish(ctx, event.SplitPaymentCompleted{SplitPaymentID: splitID, BookingID: bookingID})

	if len(hub.sent) != 2 {
		t.Fatalf("split-completed pushes = %d, want 2 (%+v)", len(hub.sent), hub.sent)
	}
	got := map[uuid.UUID]string{}
	for _, c := range hub.sent {
		got[c.UserID] = c.Payload
	}
	if got[organizerID] != `{"type":"notification"}` {
		t.Fatalf("organizer push = %q", got[organizerID])
	}
	if got[payerID] != `{"type":"notification"}` {
		t.Fatalf("payer push = %q", got[payerID])
	}
}

// TestEventHandler_SplitPaymentCompletedSkipsWhenReposMissing — the optional
// dep contract must not panic and must not push when repos are absent.
func TestEventHandler_SplitPaymentCompletedSkipsWhenReposMissing(t *testing.T) {
	ctx := context.Background()
	hub := &fakeBroadcaster{}
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(realtimeapp.NewService(hub).EventHandler())

	dispatcher.Publish(ctx, event.SplitPaymentCompleted{SplitPaymentID: uuid.New(), BookingID: uuid.New()})

	if len(hub.sent) != 0 {
		t.Fatalf("pushes without repos = %+v, want none", hub.sent)
	}
}

// TestEventHandler_DisputeEventsPushHints — S99 / WF-GAP-002. When a guest or
// host opens a Resolution Center case, the OTHER party gets the badge; when
// a moderator decides it, BOTH parties do.
func TestEventHandler_DisputeEventsPushHints(t *testing.T) {
	ctx := context.Background()
	hub := &fakeBroadcaster{}
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(realtimeapp.NewService(hub).EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()

	// Guest opens → host (respondent) is pushed.
	dispatcher.Publish(ctx, event.DisputeOpened{
		DisputeID: uuid.New(), BookingID: uuid.New(),
		HostID: hostID, GuestID: guestID, OpenerID: guestID,
	})
	if len(hub.sent) != 1 || hub.sent[0].UserID != hostID {
		t.Fatalf("guest-opens push = %+v, want one to host", hub.sent)
	}

	// Resolved → both parties pushed.
	hub.sent = nil
	dispatcher.Publish(ctx, event.DisputeResolved{
		DisputeID: uuid.New(), BookingID: uuid.New(),
		HostID: hostID, GuestID: guestID,
	})
	if len(hub.sent) != 2 {
		t.Fatalf("resolved pushes = %d, want 2 (%+v)", len(hub.sent), hub.sent)
	}
	got := map[uuid.UUID]bool{hub.sent[0].UserID: true, hub.sent[1].UserID: true}
	if !got[hostID] || !got[guestID] {
		t.Fatalf("resolved pushes targets = %v, want both host and guest", got)
	}
}
