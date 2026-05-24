package userapp_test

import (
	"context"
	"testing"

	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
)

func TestSyncFromIdentity_DerivesRoleFromRealmRoles(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	// First login with the admin realm role provisions an admin.
	u, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-admin", Email: "admin@test.dev", FullName: "Admin", Roles: []string{"admin", "guest"},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if u.Role != user.RoleAdmin {
		t.Fatalf("role = %q, want admin", u.Role)
	}

	// A host-role token provisions a host.
	h, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-host", Email: "host@test.dev", FullName: "Host", Roles: []string{"host"},
	})
	if err != nil || h.Role != user.RoleHost {
		t.Fatalf("host sync: role=%v err=%v", h.Role, err)
	}

	// No recognised role defaults to guest.
	g, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-guest", Email: "guest@test.dev", FullName: "Guest", Roles: []string{"offline_access"},
	})
	if err != nil || g.Role != user.RoleGuest {
		t.Fatalf("guest sync: role=%v err=%v", g.Role, err)
	}
}

func TestSyncFromIdentity_ElevatesButNeverDemotes(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	// Start as a guest.
	if _, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	}); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// A later login asserting the admin role elevates the persisted user.
	u, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: []string{"admin"},
	})
	if err != nil || u.Role != user.RoleAdmin {
		t.Fatalf("elevate: role=%v err=%v", u.Role, err)
	}

	// A subsequent login WITHOUT the role must not demote the admin.
	u2, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil || u2.Role != user.RoleAdmin {
		t.Fatalf("must not demote: role=%v err=%v", u2.Role, err)
	}
}

func TestSyncFromIdentity_PreservesSelfServiceHost(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	u, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	// User self-promotes to host (no realm role involved).
	if _, err := svc.BecomeHost(ctx, u.ID); err != nil {
		t.Fatalf("become host: %v", err)
	}
	// A plain guest-token login must not demote them back to guest.
	again, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil || again.Role != user.RoleHost {
		t.Fatalf("self-service host preserved: role=%v err=%v", again.Role, err)
	}
}
