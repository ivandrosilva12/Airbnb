package notificationapp_test

import (
	"context"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func TestEventHandler_CreatesNotificationsAndUnreadFlow(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()

	// A booking request notifies the host.
	dispatcher.Publish(ctx, event.BookingRequested{
		BookingID: uuid.New(), PropertyID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID,
	})
	// A message notifies its recipient (the host here).
	dispatcher.Publish(ctx, event.MessageSent{
		ConversationID: uuid.New(), SenderID: guestID, RecipientID: hostID,
	})

	unread, err := svc.UnreadCount(ctx, hostID)
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if unread != 2 {
		t.Fatalf("host unread = %d, want 2", unread)
	}

	// The guest received nothing.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 0 {
		t.Fatalf("guest unread = %d, want 0", c)
	}

	page, err := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("listed %d notifications, want 2", len(page.Items))
	}

	// Marking one read drops the unread count to 1.
	if err := svc.MarkRead(ctx, hostID, page.Items[0].ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Fatalf("unread after mark = %d, want 1", c)
	}

	// Another user cannot mark this notification read.
	if err := svc.MarkRead(ctx, guestID, page.Items[1].ID); err != shared.ErrNotFound {
		t.Fatalf("cross-user mark read err = %v, want ErrNotFound", err)
	}

	// Mark-all clears the rest.
	if err := svc.MarkAllRead(ctx, hostID); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", c)
	}
}

func TestEventHandler_BookingCompletedPromptsBothParties(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingCompleted{
		BookingID: uuid.New(), PropertyID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID,
	})

	// Both the guest and the host are prompted to review.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Fatalf("guest review prompts = %d, want 1", c)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Fatalf("host review prompts = %d, want 1", c)
	}
}

// S86 — ExperienceBooking lifecycle fans out the same way as property
// booking events: host learns about a new booking, guest about the
// confirmation, the OTHER party about a cancel.
func TestEventHandler_ExperienceBookingLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()
	bookingID := uuid.New()
	expID := uuid.New()

	// Created → host notified
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: bookingID, ExperienceID: expID, ExperienceTitle: "Pasta workshop",
		HostID: hostID, GuestID: guestID, TotalCents: 6600, Currency: "EUR",
	})
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Errorf("host unread after Created = %d, want 1", c)
	}
	if c, _ := svc.UnreadCount(ctx, guestID); c != 0 {
		t.Errorf("guest unread after Created = %d, want 0", c)
	}

	// Confirmed → guest notified
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingConfirmed{
		BookingID: bookingID, ExperienceID: expID, ExperienceTitle: "Pasta workshop",
		HostID: hostID, GuestID: guestID,
	})
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Errorf("guest unread after Confirmed = %d, want 1", c)
	}

	// Cancelled by GUEST → HOST notified (the other party)
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID: bookingID, ExperienceID: expID, ExperienceTitle: "Pasta workshop",
		HostID: hostID, GuestID: guestID, CancelledBy: guestID,
	})
	if c, _ := svc.UnreadCount(ctx, hostID); c != 2 {
		t.Errorf("host unread after guest Cancelled = %d, want 2", c)
	}
	// Guest stayed at 1 — canceller doesn't get notified of own action.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Errorf("guest unread should stay at 1, got %d", c)
	}

	// Verify the notification types are the ExperienceBooking-specific ones.
	page, err := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := map[notification.Type]bool{}
	for _, n := range page.Items {
		found[n.Type] = true
	}
	if !found[notification.TypeExperienceBookingRequested] {
		t.Error("missing TypeExperienceBookingRequested")
	}
	if !found[notification.TypeExperienceBookingCancelled] {
		t.Error("missing TypeExperienceBookingCancelled")
	}
}

// Title fallback: when the event arrives with an empty ExperienceTitle
// the subscriber still creates a notification (using a generic phrase)
// rather than rendering "" in user-facing copy.
func TestEventHandler_ExperienceBookingTitleFallback(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: uuid.New(), ExperienceID: uuid.New(), ExperienceTitle: "",
		HostID: hostID, GuestID: uuid.New(),
	})
	page, _ := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(page.Items))
	}
	if page.Items[0].Body == "" {
		t.Error("notification body should not be empty when title is missing")
	}
}
