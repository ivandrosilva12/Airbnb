package realtimeapp_test

import (
	"context"
	"sync"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	realtimeapp "github.com/airhost/backend/internal/application/realtime"
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
