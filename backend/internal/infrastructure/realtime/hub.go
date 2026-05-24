// Package realtime provides an in-process fan-out hub that pushes per-user
// payloads to live connections (used by the SSE endpoint). It is deliberately
// transport-agnostic: it deals in opaque string payloads and user IDs, so the
// application layer decides what to send.
package realtime

import (
	"sync"

	"github.com/google/uuid"
)

// Hub fans out string payloads to subscribers grouped by user ID. It is
// goroutine-safe. A single process holds one Hub; horizontal scaling would
// require an external bus (e.g. Redis pub/sub) behind the same interface.
type Hub struct {
	mu     sync.Mutex
	nextID int
	subs   map[uuid.UUID]map[int]chan string
}

// NewHub builds an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: map[uuid.UUID]map[int]chan string{}}
}

// Subscribe registers a new connection for a user and returns its channel and a
// cancel func that removes the subscription and closes the channel. The channel
// is buffered; if a consumer falls behind, messages are dropped rather than
// blocking publishers.
func (h *Hub) Subscribe(userID uuid.UUID) (<-chan string, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	id := h.nextID
	h.nextID++
	ch := make(chan string, 16)
	if h.subs[userID] == nil {
		h.subs[userID] = map[int]chan string{}
	}
	h.subs[userID][id] = ch

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		conns := h.subs[userID]
		if conns == nil {
			return
		}
		if c, ok := conns[id]; ok {
			delete(conns, id)
			close(c)
		}
		if len(conns) == 0 {
			delete(h.subs, userID)
		}
	}
	return ch, cancel
}

// Publish delivers payload to all of a user's live connections. Delivery is
// best-effort: a full buffer drops the message instead of blocking.
func (h *Hub) Publish(userID uuid.UUID, payload string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[userID] {
		select {
		case ch <- payload:
		default:
		}
	}
}

// Connections reports the number of live connections for a user (used in tests).
func (h *Hub) Connections(userID uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[userID])
}
