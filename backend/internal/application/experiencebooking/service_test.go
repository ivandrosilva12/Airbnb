package experiencebookingapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type stubFinder struct {
	exp *experience.Experience
	err error
}

func (s *stubFinder) FindByID(_ context.Context, _ uuid.UUID) (*experience.Experience, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.exp, nil
}

func mkExperience(t *testing.T, status experience.Status) *experience.Experience {
	t.Helper()
	price, _ := shared.NewMoney(3000, "EUR")
	exp, err := experience.NewExperience(uuid.New(), "Pasta workshop", "Make fresh pasta from scratch",
		experience.CategoryCooking,
		experience.Address{City: "Rome", Country: "IT", Latitude: 41.9, Longitude: 12.5},
		120, 6, price, "en")
	if err != nil {
		t.Fatalf("new experience: %v", err)
	}
	exp.AddPhoto("k", "u")
	switch status {
	case experience.StatusPublished:
		if err := exp.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
	case experience.StatusSuspended:
		exp.Suspend()
	}
	return exp
}

func TestService_Create_HappyPath(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Status != experiencebooking.StatusPending {
		t.Errorf("status got %s want pending", b.Status)
	}
	if got := b.Pricing.Subtotal.AmountCents(); got != 6000 {
		t.Errorf("subtotal got %d want 6000", got)
	}
	if got := b.Pricing.Total.AmountCents(); got != 6600 {
		t.Errorf("total got %d want 6600", got)
	}
}

func TestService_Create_RejectsDraftExperience(t *testing.T) {
	exp := mkExperience(t, experience.StatusDraft)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	_, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	if err == nil {
		t.Fatal("expected error for draft experience")
	}
}

func TestService_Create_RejectsOverlappingSession(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	repo := memory.NewExperienceBookingRepository()
	svc := NewService(repo, &stubFinder{exp: exp}, 0.10)
	start := time.Now().UTC().Add(48 * time.Hour)
	if _, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(), StartAt: start, Guests: 1,
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Second booking same start → conflict.
	_, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(), StartAt: start, Guests: 1,
	})
	if !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestService_Cancel_GuestAllowed(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	guest := uuid.New()
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: guest,
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	got, err := svc.Cancel(context.Background(), guest, b.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got.Status != experiencebooking.StatusCancelled {
		t.Errorf("status got %s want cancelled", got.Status)
	}
}

func TestService_Cancel_HostAllowed(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	if _, err := svc.Cancel(context.Background(), exp.HostID, b.ID); err != nil {
		t.Fatalf("host cancel: %v", err)
	}
}

func TestService_Cancel_StrangerForbidden(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	_, err := svc.Cancel(context.Background(), uuid.New(), b.ID)
	if !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestService_Confirm_HostOnly(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	// Guest cannot confirm.
	if _, err := svc.Confirm(context.Background(), b.GuestID, b.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Errorf("guest confirm: expected ErrForbidden, got %v", err)
	}
	got, err := svc.Confirm(context.Background(), exp.HostID, b.ID)
	if err != nil {
		t.Fatalf("host confirm: %v", err)
	}
	if got.Status != experiencebooking.StatusConfirmed {
		t.Errorf("status got %s want confirmed", got.Status)
	}
}

func TestService_ListMine_ReturnsGuestsBookings(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	g1, g2 := uuid.New(), uuid.New()
	_, _ = svc.Create(context.Background(), CreateInput{ExperienceID: exp.ID, GuestID: g1, StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1})
	_, _ = svc.Create(context.Background(), CreateInput{ExperienceID: exp.ID, GuestID: g2, StartAt: time.Now().UTC().Add(72 * time.Hour), Guests: 1})
	res, err := svc.ListMine(context.Background(), g1, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list mine: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("total got %d want 1", res.Total)
	}
}
