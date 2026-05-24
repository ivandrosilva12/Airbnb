package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func newUoW() (*memory.UnitOfWork, *int) {
	calls := new(int)
	d := event.NewDispatcher()
	d.Subscribe(func(_ context.Context, _ event.Event) { *calls++ })
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, d)
	return memory.NewUnitOfWork(memory.NewBookingRepository(), nil, nil, outbox, relay), calls
}

func TestUnitOfWork_DispatchesOnSuccess(t *testing.T) {
	uow, calls := newUoW()
	err := uow.Run(context.Background(), func(tx port.Tx) error {
		rec, _ := event.NewRecord(event.MessageSent{ConversationID: uuid.New()})
		return tx.Outbox.Append(context.Background(), rec)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("dispatched = %d, want 1", *calls)
	}
}

func TestUnitOfWork_NoDispatchOnError(t *testing.T) {
	uow, calls := newUoW()
	want := errors.New("boom")
	err := uow.Run(context.Background(), func(tx port.Tx) error {
		rec, _ := event.NewRecord(event.MessageSent{ConversationID: uuid.New()})
		_ = tx.Outbox.Append(context.Background(), rec)
		return want // abort: nothing should be dispatched
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want boom", err)
	}
	if *calls != 0 {
		t.Fatalf("dispatched = %d, want 0 (aborted)", *calls)
	}
}
