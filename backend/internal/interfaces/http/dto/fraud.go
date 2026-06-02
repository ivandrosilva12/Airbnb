package dto

import (
	"time"

	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/google/uuid"
)

// FraudSignalView is the wire shape for one rule outcome inside an
// Assessment. Mirrors the domain Signal — Impact is exposed so the
// admin UI can show "this rule contributed X points".
type FraudSignalView struct {
	Name   string `json:"name"`
	Impact int    `json:"impact"`
	Note   string `json:"note,omitempty"`
}

// FraudAssessmentView is the wire shape for a single Assessment. The
// signals slice arrives pre-sorted by impact (highest first), so the
// admin UI can render "top contributors" without re-sorting.
type FraudAssessmentView struct {
	ID        uuid.UUID         `json:"id"`
	BookingID uuid.UUID         `json:"bookingId"`
	GuestID   uuid.UUID         `json:"guestId"`
	Score     int               `json:"score"`
	Level     string            `json:"level"`
	Signals   []FraudSignalView `json:"signals"`
	CreatedAt time.Time         `json:"createdAt"`
}

// FromFraudAssessment maps a domain Assessment to the wire shape.
// Nil-safe so callers don't need to nil-check when handling a "no
// assessment yet" response.
func FromFraudAssessment(a *fraud.Assessment) FraudAssessmentView {
	if a == nil {
		return FraudAssessmentView{}
	}
	signals := make([]FraudSignalView, 0, len(a.Signals))
	for _, s := range a.Signals {
		signals = append(signals, FraudSignalView{
			Name: s.Name, Impact: s.Impact, Note: s.Note,
		})
	}
	return FraudAssessmentView{
		ID:        a.ID,
		BookingID: a.BookingID,
		GuestID:   a.GuestID,
		Score:     a.Score,
		Level:     string(a.Level),
		Signals:   signals,
		CreatedAt: a.CreatedAt,
	}
}
