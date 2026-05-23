// Package payment provides a fake PaymentGateway implementation for local
// development and tests. It simulates a provider that always succeeds and
// tracks references in memory, so the rest of the system can exercise the full
// authorize/capture/refund flow without a real payment processor.
package payment

import (
	"context"
	"fmt"
	"sync"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// FakeGateway is an in-memory PaymentGateway. It is goroutine-safe.
type FakeGateway struct {
	mu   sync.Mutex
	refs map[string]string // ref -> state
}

// NewFakeGateway builds a FakeGateway.
func NewFakeGateway() *FakeGateway {
	return &FakeGateway{refs: map[string]string{}}
}

// Authorize returns a synthetic gateway reference and records the hold.
func (g *FakeGateway) Authorize(_ context.Context, _ shared.Money, idempotencyKey string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ref := "fake_" + uuid.NewString()
	g.refs[ref] = "authorized:" + idempotencyKey
	return ref, nil
}

// Capture marks an authorized reference as captured.
func (g *FakeGateway) Capture(_ context.Context, ref string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.refs[ref]; !ok {
		return fmt.Errorf("unknown payment reference %q", ref)
	}
	g.refs[ref] = "captured"
	return nil
}

// Refund marks a reference as refunded.
func (g *FakeGateway) Refund(_ context.Context, ref string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.refs[ref]; !ok {
		return fmt.Errorf("unknown payment reference %q", ref)
	}
	g.refs[ref] = "refunded"
	return nil
}
