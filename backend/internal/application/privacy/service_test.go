package privacyapp_test

import (
	"context"
	"testing"
	"time"

	privacyapp "github.com/airhost/backend/internal/application/privacy"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/review"
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
	if err := favorites.Add(ctx, favorite.New(u.ID, prop, nil)); err != nil {
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
	if err := favorites.Add(ctx, favorite.New(u.ID, uuid.New(), nil)); err != nil {
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

func TestErase_DeletesNotificationsAndScrubsReviewComments(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	notifications := memory.NewNotificationRepository()
	reviews := memory.NewReviewRepository()
	svc := privacyapp.NewService(
		users, memory.NewBookingRepository(), memory.NewPaymentRepository(),
		memory.NewFavoriteRepository(), notifications, memory.NewPayoutRepository(), reviews,
	)

	u, err := user.NewUser("sub-1", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := notifications.Create(ctx, &notification.Notification{
		ID: uuid.New(), UserID: u.ID, Type: notification.TypeBookingConfirmed,
		Title: "t", Body: "b", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	rv := &review.Review{
		ID: uuid.New(), BookingID: uuid.New(), PropertyID: uuid.New(),
		AuthorID: u.ID, GuestID: u.ID, Kind: review.KindGuestToProperty,
		Rating: 5, Comment: "Great place — reach me at jane@test.dev", CreatedAt: time.Now().UTC(),
	}
	if err := reviews.Create(ctx, rv); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	if err := svc.Erase(ctx, u.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}

	notes, err := notifications.ListByUser(ctx, u.ID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notes.Items) != 0 {
		t.Fatalf("notifications after erase = %d, want 0", len(notes.Items))
	}
	// The review survives (rating intact) but its free-text is scrubbed.
	page, err := reviews.ListByProperty(ctx, rv.PropertyID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Comment != "" || page.Items[0].Rating != 5 {
		t.Fatalf("review after erase = %+v, want comment scrubbed + rating kept", page.Items)
	}
}
