package messageapp_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	messageapp "github.com/airhost/backend/internal/application/message"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// fakeStorage is a no-op port.Storage that echoes the object key back as a URL.
type fakeStorage struct{}

func (fakeStorage) Upload(_ context.Context, key string, _ io.Reader, _ int64, _ string) (string, error) {
	return "http://storage.test/" + key, nil
}
func (fakeStorage) PresignedPutURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "http://storage.test/" + key, nil
}
func (fakeStorage) PublicURL(key string) string { return "http://storage.test/" + key }

func newMessageService(t *testing.T) (*messageapp.Service, *memory.PropertyRepository) {
	t.Helper()
	messages := memory.NewMessageRepository()
	properties := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	identities := memory.NewIdentityRepository()
	dispatcher := event.NewDispatcher()
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, dispatcher)
	uow := memory.NewUnitOfWork(bookings, messages, identities, nil, nil, outbox, relay)
	return messageapp.NewService(messages, properties, memory.NewUserBlockRepository(), fakeStorage{}, uow), properties
}

// newMessageServiceWithBlocks is like newMessageService but also exposes the
// user-block repo so tests can place a block and assert messaging is gated.
func newMessageServiceWithBlocks(t *testing.T) (*messageapp.Service, *memory.PropertyRepository, *memory.UserBlockRepository) {
	t.Helper()
	messages := memory.NewMessageRepository()
	properties := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	identities := memory.NewIdentityRepository()
	blocks := memory.NewUserBlockRepository()
	dispatcher := event.NewDispatcher()
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, dispatcher)
	uow := memory.NewUnitOfWork(bookings, messages, identities, nil, nil, outbox, relay)
	return messageapp.NewService(messages, properties, blocks, fakeStorage{}, uow), properties, blocks
}

func seedProperty(t *testing.T, props *memory.PropertyRepository, hostID uuid.UUID) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	if err := props.Create(context.Background(), p); err != nil {
		t.Fatalf("store property: %v", err)
	}
	return p
}

func TestSendAttachment_UploadsAndPosts(t *testing.T) {
	ctx := context.Background()
	svc, props := newMessageService(t)
	hostID := uuid.New()
	guestID := uuid.New()
	prop := seedProperty(t, props, hostID)

	conv, err := svc.StartConversation(ctx, guestID, prop.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	msg, err := svc.SendAttachment(ctx, guestID, conv.ID, messageapp.AttachmentInput{
		Body:        "here is the floor plan",
		Reader:      strings.NewReader("PNGDATA"),
		Size:        7,
		ContentType: "image/png",
		Filename:    "plan.png",
	})
	if err != nil {
		t.Fatalf("send attachment: %v", err)
	}
	if msg.Attachment == nil {
		t.Fatal("returned message should carry an attachment")
	}
	if msg.Attachment.URL == "" || msg.Attachment.ContentType != "image/png" || msg.Attachment.Filename != "plan.png" {
		t.Fatalf("attachment metadata = %+v", msg.Attachment)
	}

	// The attachment survives a round-trip through the repository.
	page, err := svc.ListMessages(ctx, hostID, conv.ID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Attachment == nil {
		t.Fatalf("messages = %+v, want 1 with an attachment", page.Items)
	}
	if page.Items[0].Attachment.URL != msg.Attachment.URL {
		t.Errorf("persisted attachment URL = %q, want %q", page.Items[0].Attachment.URL, msg.Attachment.URL)
	}
}

func TestSendAttachment_NonParticipantForbidden(t *testing.T) {
	ctx := context.Background()
	svc, props := newMessageService(t)
	hostID := uuid.New()
	guestID := uuid.New()
	prop := seedProperty(t, props, hostID)

	conv, err := svc.StartConversation(ctx, guestID, prop.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	_, err = svc.SendAttachment(ctx, uuid.New(), conv.ID, messageapp.AttachmentInput{
		Reader: strings.NewReader("x"), Size: 1, ContentType: "image/png", Filename: "x.png",
	})
	if err != shared.ErrForbidden {
		t.Fatalf("non-participant attachment err = %v, want ErrForbidden", err)
	}
}

func TestStartConversation_BlockedIsRefused(t *testing.T) {
	svc, props, blocks := newMessageServiceWithBlocks(t)
	ctx := context.Background()
	hostID := uuid.New()
	guestID := uuid.New()
	prop := seedProperty(t, props, hostID)

	// The host blocks the guest; the guest can no longer start a conversation.
	if err := blocks.Add(ctx, hostID, guestID); err != nil {
		t.Fatalf("add block: %v", err)
	}
	if _, err := svc.StartConversation(ctx, guestID, prop.ID); err == nil {
		t.Fatal("expected a blocked guest to be refused")
	}
}

func TestSendMessage_BlockedIsRefusedBothWays(t *testing.T) {
	svc, props, blocks := newMessageServiceWithBlocks(t)
	ctx := context.Background()
	hostID := uuid.New()
	guestID := uuid.New()
	prop := seedProperty(t, props, hostID)

	conv, err := svc.StartConversation(ctx, guestID, prop.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.SendMessage(ctx, guestID, conv.ID, "hello"); err != nil {
		t.Fatalf("first message should succeed: %v", err)
	}
	// The guest blocks the host; neither party may post afterwards.
	if err := blocks.Add(ctx, guestID, hostID); err != nil {
		t.Fatalf("block: %v", err)
	}
	if _, err := svc.SendMessage(ctx, hostID, conv.ID, "reply"); err == nil {
		t.Fatal("expected the blocked host's reply to be refused")
	}
	if _, err := svc.SendMessage(ctx, guestID, conv.ID, "again"); err == nil {
		t.Fatal("expected the blocking guest's message to be refused too")
	}
}
