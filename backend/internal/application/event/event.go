// Package event provides a tiny in-process domain-event mechanism. Application
// services publish integration events (e.g. a booking was requested) through a
// Publisher; subscribers — such as the notification handler — react to them.
// Dispatch is synchronous and best-effort: a failing subscriber never breaks
// the publishing use case.
package event

import (
	"context"
	"log/slog"
	"sync"
)

// Event is a published domain/integration event.
type Event interface {
	// EventName returns a stable identifier, e.g. "booking.requested".
	EventName() string
}

// Publisher publishes events. Application services depend on this interface.
type Publisher interface {
	Publish(ctx context.Context, e Event)
}

// Handler reacts to an event. Handlers must not panic; the dispatcher recovers
// from panics so one bad subscriber cannot crash the request.
type Handler func(ctx context.Context, e Event)

// Dispatcher is a synchronous in-process Publisher with fan-out to handlers.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers []Handler
}

// NewDispatcher returns an empty Dispatcher.
func NewDispatcher() *Dispatcher { return &Dispatcher{} }

// Subscribe registers a handler invoked for every published event.
func (d *Dispatcher) Subscribe(h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, h)
}

// Publish invokes all handlers. Each handler runs in isolation; a panic or slow
// handler is contained and logged rather than propagated to the caller.
func (d *Dispatcher) Publish(ctx context.Context, e Event) {
	d.mu.RLock()
	handlers := make([]Handler, len(d.handlers))
	copy(handlers, d.handlers)
	d.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("event handler panicked", "event", e.EventName(), "recover", r)
				}
			}()
			h(ctx, e)
		}()
	}
}

// nopPublisher discards events. Used so services can be constructed without a
// dispatcher (e.g. in focused unit tests).
type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, Event) {}

// Nop returns a Publisher that ignores all events.
func Nop() Publisher { return nopPublisher{} }
