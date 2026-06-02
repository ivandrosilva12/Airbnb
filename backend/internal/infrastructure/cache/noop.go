// Package cache holds the port.Cache adapters: a noop default, a
// TTL-aware in-memory store for tests + single-node deployments, and
// a Redis adapter for multi-node production.
package cache

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/application/port"
)

// Noop is the default port.Cache implementation: every Get is a
// miss, Set + Delete are no-ops. Lets the read-cache code paths
// stay wired in every deployment without the operator deciding to
// run Redis on day one.
type Noop struct{}

// NewNoop builds a Noop cache.
func NewNoop() *Noop { return &Noop{} }

var _ port.Cache = (*Noop)(nil)

func (Noop) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}

func (Noop) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

func (Noop) Delete(_ context.Context, _ ...string) error {
	return nil
}
