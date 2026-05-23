package notificationapp_test

import (
	"context"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	notificationapp "github.com/airhost/backend/internal/application/notification"
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
