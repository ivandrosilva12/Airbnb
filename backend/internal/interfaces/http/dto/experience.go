package dto

import (
	"time"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/google/uuid"
)

// ExperienceAddressView is the wire shape for an experience address.
type ExperienceAddressView struct {
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ExperiencePhotoView is the wire shape for one photo reference.
type ExperiencePhotoView struct {
	ID       uuid.UUID `json:"id"`
	URL      string    `json:"url"`
	Position int       `json:"position"`
}

// ExperienceView is the wire shape for a single Experience.
type ExperienceView struct {
	ID              uuid.UUID             `json:"id"`
	HostID          uuid.UUID             `json:"hostId"`
	Title           string                `json:"title"`
	Description     string                `json:"description"`
	Category        string                `json:"category"`
	Status          string                `json:"status"`
	Address         ExperienceAddressView `json:"address"`
	DurationMinutes int                   `json:"durationMinutes"`
	MaxGuests       int                   `json:"maxGuests"`
	PricePerGuest   MoneyView             `json:"pricePerGuest"`
	Language        string                `json:"language"`
	Photos          []ExperiencePhotoView `json:"photos"`
	CreatedAt       time.Time             `json:"createdAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
}

// FromExperience maps a domain Experience to the wire shape.
func FromExperience(e *experience.Experience) ExperienceView {
	if e == nil {
		return ExperienceView{}
	}
	photos := make([]ExperiencePhotoView, 0, len(e.Photos))
	for _, p := range e.Photos {
		photos = append(photos, ExperiencePhotoView{ID: p.ID, URL: p.URL, Position: p.Position})
	}
	return ExperienceView{
		ID:              e.ID,
		HostID:          e.HostID,
		Title:           e.Title,
		Description:     e.Description,
		Category:        string(e.Category),
		Status:          string(e.Status),
		Address: ExperienceAddressView{
			City: e.Address.City, Country: e.Address.Country,
			Latitude: e.Address.Latitude, Longitude: e.Address.Longitude,
		},
		DurationMinutes: e.DurationMinutes,
		MaxGuests:       e.MaxGuests,
		PricePerGuest:   fromMoney(e.PricePerGuest),
		Language:        e.Language,
		Photos:          photos,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}
