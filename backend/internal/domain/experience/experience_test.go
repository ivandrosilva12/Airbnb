package experience_test

import (
	"testing"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func okAddress() experience.Address {
	return experience.Address{City: "Porto", Country: "PT", Latitude: 41.15, Longitude: -8.61}
}

func okPrice() shared.Money {
	m, _ := shared.NewMoney(2500, "EUR")
	return m
}

func TestNewExperience_Valid(t *testing.T) {
	e, err := experience.NewExperience(uuid.New(), "Sunset Tasca Tour", "Wine + tapas",
		experience.CategoryTour, okAddress(), 120, 6, okPrice(), "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Status != experience.StatusDraft {
		t.Fatalf("status = %s, want draft", e.Status)
	}
	if e.Language != "en" {
		t.Fatalf("language = %s, want en", e.Language)
	}
}

func TestNewExperience_RejectsBadInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func() (*experience.Experience, error)
	}{
		{"empty title", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "  ", "x", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "en")
		}},
		{"bad category", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "T", "x", experience.Category("garbage"), okAddress(), 60, 4, okPrice(), "en")
		}},
		{"duration too short", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "T", "x", experience.CategoryTour, okAddress(), 10, 4, okPrice(), "en")
		}},
		{"duration too long", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "T", "x", experience.CategoryTour, okAddress(), 48*60, 4, okPrice(), "en")
		}},
		{"maxGuests = 0", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "T", "x", experience.CategoryTour, okAddress(), 60, 0, okPrice(), "en")
		}},
		{"language too long", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "T", "x", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "english")
		}},
		{"nil host", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.Nil, "T", "x", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "en")
		}},
		{"address bad coordinates", func() (*experience.Experience, error) {
			return experience.NewExperience(uuid.New(), "T", "x", experience.CategoryTour,
				experience.Address{City: "Porto", Country: "PT", Latitude: 100, Longitude: 0}, 60, 4, okPrice(), "en")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := c.mutate(); err == nil {
				t.Fatalf("expected validation error for %s", c.name)
			}
		})
	}
}

func TestPublish_RequiresDescriptionAndPhoto(t *testing.T) {
	e, _ := experience.NewExperience(uuid.New(), "T", "", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "en")
	if err := e.Publish(); err == nil {
		t.Fatal("expected Publish to refuse a description-less listing")
	}
	if err := e.UpdateBasics("T", "Great experience", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "en"); err != nil {
		t.Fatalf("UpdateBasics failed: %v", err)
	}
	if err := e.Publish(); err == nil {
		t.Fatal("expected Publish to refuse a photo-less listing")
	}
	e.AddPhoto("k1", "https://cdn/k1")
	if err := e.Publish(); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	if e.Status != experience.StatusPublished {
		t.Fatalf("status = %s, want published", e.Status)
	}
	// Re-publishing is a no-op.
	if err := e.Publish(); err != nil {
		t.Fatalf("idempotent Publish failed: %v", err)
	}
}

func TestSuspend_Idempotent(t *testing.T) {
	e, _ := experience.NewExperience(uuid.New(), "T", "x", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "en")
	e.Suspend()
	if e.Status != experience.StatusSuspended {
		t.Fatalf("status = %s, want suspended", e.Status)
	}
	e.Suspend() // no panic, no error
}

func TestUpdateBasics_PreservesIDAndHost(t *testing.T) {
	host := uuid.New()
	e, _ := experience.NewExperience(host, "T", "x", experience.CategoryTour, okAddress(), 60, 4, okPrice(), "en")
	origID := e.ID
	if err := e.UpdateBasics("T2", "y", experience.CategoryCooking, okAddress(), 90, 6, okPrice(), "pt"); err != nil {
		t.Fatalf("UpdateBasics failed: %v", err)
	}
	if e.ID != origID {
		t.Fatalf("ID changed after UpdateBasics")
	}
	if e.HostID != host {
		t.Fatalf("HostID changed after UpdateBasics")
	}
	if e.Title != "T2" || e.Category != experience.CategoryCooking || e.Language != "pt" {
		t.Fatalf("UpdateBasics didn't apply: %+v", e)
	}
}
