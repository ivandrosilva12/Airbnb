package property

import (
	"math"
	"time"
)

// ColdStartWindow is how recent a listing must be for the "new listing"
// bonus to apply — without a cold-start signal a brand-new listing with no
// reviews would never surface above seasoned ones and hosts would never
// onboard. 60 days is roughly long enough for a host to attract the first
// handful of reviews under typical demand.
const ColdStartWindow = 60 * 24 * time.Hour

// Ranking weights. Hand-tuned defaults; expressed as constants so a future
// experiment can split them into a config-driven knob without touching the
// formula. The absolute magnitudes don't matter — only the ratios do, since
// the score is purely an ordering key.
const (
	rankWeightRating     = 1.0  // rating × log10(1+reviewCount)
	rankWeightSuperhost  = 0.5  // applied when HostIsSuperhost
	rankWeightPhotos     = 0.05 // per photo, capped at 10 photos
	rankWeightColdStart  = 0.3  // for listings within ColdStartWindow
	rankMaxPhotosCounted = 10   // beyond this, more photos don't help
)

// RankScore is the composite ranking score (S63) used by SortRanked. Higher
// = better. Always non-negative.
//
// Components:
//   - rating × log10(1+reviewCount): quality scaled by review volume so a
//     5-star with one review can't dominate a 4.5 with hundreds.
//   - superhost boost: a flat bump for verified high-performers.
//   - photo richness: small bump scaling with photo count up to 10; signals
//     a host who put effort into the listing.
//   - cold-start bonus: a temporary bump for listings created in the last
//     ColdStartWindow so brand-new inventory surfaces above 0-review obscurity.
//
// `now` is injected (not time.Now()) so unit tests can pin a deterministic
// score and the cold-start bonus is reproducible.
func RankScore(p *Property, now time.Time) float64 {
	if p == nil {
		return 0
	}
	score := 0.0

	// Rating × log10(1 + reviewCount). log10(1) = 0 for zero-review
	// listings — they earn the cold-start bonus instead.
	if p.ReviewCount > 0 {
		score += rankWeightRating * p.AverageRating * math.Log10(1+float64(p.ReviewCount))
	}

	if p.HostIsSuperhost {
		score += rankWeightSuperhost
	}

	photos := len(p.Photos)
	if photos > rankMaxPhotosCounted {
		photos = rankMaxPhotosCounted
	}
	score += rankWeightPhotos * float64(photos)

	if !p.CreatedAt.IsZero() && now.Sub(p.CreatedAt) < ColdStartWindow {
		score += rankWeightColdStart
	}

	return score
}
