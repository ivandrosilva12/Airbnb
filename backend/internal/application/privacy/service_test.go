package privacyapp_test

import (
	"context"
	"testing"

	privacyapp "github.com/airhost/backend/internal/application/privacy"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func newPrivacyService(t *testing.T) (*privacyapp.Service, *memory.UserRepository, *memory.FavoriteRepository) {
	t.Helper()
	users := memory.NewUserRepository()
	favorites := memory.NewFavoriteRepository()
	svc := privacyapp.NewService(
		users,
		memory.NewBookingRepository(),
		memory.NewPaymentRepository(),
		favorites,
		memory.NewNotificationRepository(),
		memory.NewPayoutRepository(),
		memory.NewReviewRepository(),
	)
	return svc, users, favorites
}

func TestExport_IncludesProfileAndFavorites(t *testing.T) {
	ctx := context.Background()
	svc, users, favorites := newPrivacyService(t)

	u, err := user.NewUser("sub-1", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	prop := uuid.New()
	if err := favorites.Add(ctx, favorite.New(u.ID, prop)); err != nil {
		t.Fatalf("add favorite: %v", err)
	}

	exp, err := svc.Export(ctx, u.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.User.Email != "jane@test.dev" {
		t.Fatalf("export email = %q", exp.User.Email)
	}
	if len(exp.FavoriteIDs) != 1 || exp.FavoriteIDs[0] != prop {
		t.Fatalf("favorites = %v, want [%s]", exp.FavoriteIDs, prop)
	}
}

func TestErase_AnonymisesAndDropsFavorites(t *testing.T) {
	ctx := context.Background()
	svc, users, favorites := newPrivacyService(t)

	u, err := user.NewUser("sub-1", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := favorites.Add(ctx, favorite.New(u.ID, uuid.New())); err != nil {
		t.Fatalf("add favorite: %v", err)
	}

	if err := svc.Erase(ctx, u.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}

	reloaded, err := users.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.Email == "jane@test.dev" || reloaded.FullName == "Jane" || reloaded.IsActive {
		t.Fatalf("user not anonymised: %+v", reloaded)
	}
	favs, err := favorites.ListPropertyIDs(ctx, u.ID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	if len(favs.Items) != 0 {
		t.Fatalf("favorites after erase = %d, want 0", len(favs.Items))
	}
}
