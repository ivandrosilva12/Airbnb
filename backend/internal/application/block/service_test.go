package blockapp_test

import (
	"context"
	"testing"
	"time"

	blockapp "github.com/airhost/backend/internal/application/block"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func storeProperty(t *testing.T, props *memory.PropertyRepository, hostID uuid.UUID) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	_ = props.Create(context.Background(), p)
	return p
}

func days(n int) time.Time { return time.Now().UTC().AddDate(0, 0, n) }

func TestBlock_CreateListDeleteWithOwnership(t *testing.T) {
	ctx := context.Background()
	blocks := memory.NewBlockRepository()
	props := memory.NewPropertyRepository()
	svc := blockapp.NewService(blocks, props)

	hostID := uuid.New()
	prop := storeProperty(t, props, hostID)

	// A non-owner cannot block.
	if _, err := svc.Create(ctx, uuid.New(), prop.ID, days(10), days(12), ""); err != shared.ErrForbidden {
		t.Fatalf("non-owner block err = %v, want ErrForbidden", err)
	}

	// The host blocks a range.
	b, err := svc.Create(ctx, hostID, prop.ID, days(10), days(12), "Maintenance")
	if err != nil {
		t.Fatalf("create block: %v", err)
	}

	// Overlapping block is rejected.
	if _, err := svc.Create(ctx, hostID, prop.ID, days(11), days(14), ""); err == nil {
		t.Fatal("expected overlapping block to be rejected")
	}

	// Listed for the host.
	res, err := svc.ListForHost(ctx, hostID, prop.ID, shared.NewPage(10, 0))
	if err != nil || res.Total != 1 {
		t.Fatalf("list = %d (err %v), want 1", res.Total, err)
	}

	// A stranger cannot delete it.
	if err := svc.Delete(ctx, uuid.New(), b.ID); err != shared.ErrForbidden {
		t.Fatalf("non-owner delete err = %v, want ErrForbidden", err)
	}
	// The host can.
	if err := svc.Delete(ctx, hostID, b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
