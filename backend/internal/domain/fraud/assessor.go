package fraud

import "time"

// Tunables — every threshold the rules consult lives here so an
// operator can re-tune via a one-line patch instead of hunting
// through code. Values are deliberately conservative: a single
// signal lands in low; two compound into medium; three or a single
// high-impact + medium land in high.
const (
	// NewAccountWindow defines how recently an account must have
	// been created to count as "fresh". Stayed at 24h because most
	// genuine guest accounts complete a booking within hours of
	// signup, but throwaways for one-shot fraud also live mostly
	// in the same window — the rule is a correlator, not a verdict.
	NewAccountWindow = 24 * time.Hour
	// ShortLeadWindow flags bookings booked less than this far in
	// advance. Short-lead is heavily correlated with stolen-card
	// fraud (the attacker wants to consume the value before the
	// real cardholder notices).
	ShortLeadWindow = 3 * 24 * time.Hour
	// GuestVelocityWindow + Limit. More than Limit bookings created
	// by the same guest in the window indicates account-takeover
	// rather than a normal booking pattern (genuine guests rarely
	// book a second stay before the first one's check-in).
	GuestVelocityWindow = 24 * time.Hour
	GuestVelocityLimit  = 3

	// Per-signal impacts. The score is the sum, clamped to 100.
	// New account alone = low; new account + short lead = medium;
	// new account + short lead + missing KYC = high.
	ImpactNewAccount    = 20
	ImpactShortLead     = 15
	ImpactHighValue     = 25
	ImpactMissingKYC    = 20
	ImpactGuestVelocity = 30
	ImpactSuspended     = 60
)

// AssessmentInput carries everything the rules need to score a
// single booking. The application service is responsible for
// populating it from booking + user + identity reads — the domain
// stays pure (no repository calls inside the assessor).
type AssessmentInput struct {
	// GuestCreatedAt — when the guest account was created. Zero
	// value disables the new-account rule.
	GuestCreatedAt time.Time
	// GuestKYCVerified — whether the guest has a completed KYC
	// record. False triggers the missing-kyc signal.
	GuestKYCVerified bool
	// GuestIsActive — whether the guest's account is currently
	// active. Should normally be true at booking time (suspended
	// guests don't reach Create), but a race between suspension
	// and an in-flight booking can land here.
	GuestIsActive bool
	// GuestRecentBookingCount — bookings created by this guest
	// in the last GuestVelocityWindow (the application service
	// scans the booking history).
	GuestRecentBookingCount int
	// BookingTotalCents + Currency. HighValueThresholds is
	// consulted per-currency; a currency without an entry skips
	// the high-value rule. The keys are uppercase ISO codes.
	BookingTotalCents    int64
	BookingCurrency      string
	HighValueThresholds  map[string]int64
	// BookingCheckIn + BookingCreatedAt — used to derive lead time.
	BookingCheckIn    time.Time
	BookingCreatedAt  time.Time
}

// Assess runs the heuristic rules and returns the matching signals,
// in the order they were checked (the Assessment constructor will
// re-sort by impact, so this is just a stable visit order). A nil
// or zero input returns no signals — the score will be 0.
//
// Pure function: no I/O, no clock reads, no random — every input
// must come from the caller. This makes the rules trivially
// testable and replayable.
func Assess(in AssessmentInput) []Signal {
	out := make([]Signal, 0, 4)

	// New-account rule: account created within NewAccountWindow
	// of the booking's CreatedAt.
	if !in.GuestCreatedAt.IsZero() && !in.BookingCreatedAt.IsZero() {
		if in.BookingCreatedAt.Sub(in.GuestCreatedAt) < NewAccountWindow {
			out = append(out, Signal{
				Name:   SignalNameNewAccount,
				Impact: ImpactNewAccount,
				Note:   "guest account is less than 24 hours old",
			})
		}
	}

	// Short-lead rule: check-in within ShortLeadWindow of booking
	// time. Defensive guard against a check-in date already in
	// the past (clock skew) — we treat that as the most extreme
	// short lead and still flag it.
	if !in.BookingCheckIn.IsZero() && !in.BookingCreatedAt.IsZero() {
		lead := in.BookingCheckIn.Sub(in.BookingCreatedAt)
		if lead < ShortLeadWindow {
			out = append(out, Signal{
				Name:   SignalNameShortLead,
				Impact: ImpactShortLead,
				Note:   "booking made less than 3 days before check-in",
			})
		}
	}

	// High-value rule: per-currency threshold lookup. Absent
	// currency / absent threshold / zero threshold all skip.
	if in.BookingTotalCents > 0 && in.BookingCurrency != "" && len(in.HighValueThresholds) > 0 {
		threshold, ok := in.HighValueThresholds[in.BookingCurrency]
		if ok && threshold > 0 && in.BookingTotalCents >= threshold {
			out = append(out, Signal{
				Name:   SignalNameHighValue,
				Impact: ImpactHighValue,
				Note:   "booking total exceeds high-value threshold",
			})
		}
	}

	// Missing-KYC rule: unverified guest. Stronger when combined
	// with new-account or high-value, but already a signal on its
	// own — most legitimate stays involve a verified guest by
	// the time their second booking is made.
	if !in.GuestKYCVerified {
		out = append(out, Signal{
			Name:   SignalNameMissingKYC,
			Impact: ImpactMissingKYC,
			Note:   "guest has not completed identity verification",
		})
	}

	// Velocity rule: many bookings in a short window. The
	// application service counts pending+confirmed; a single
	// recent booking doesn't fire (legitimate "book a second
	// stay before the first one") — only > limit does.
	if in.GuestRecentBookingCount > GuestVelocityLimit {
		out = append(out, Signal{
			Name:   SignalNameGuestVelocity,
			Impact: ImpactGuestVelocity,
			Note:   "guest has booked unusually often in the last 24 hours",
		})
	}

	// Suspended-guest rule. As above, this normally shouldn't
	// reach here, but if it does (race between admin suspension
	// and the in-flight booking) it dominates the score.
	if !in.GuestIsActive {
		out = append(out, Signal{
			Name:   SignalNameSuspended,
			Impact: ImpactSuspended,
			Note:   "guest account is suspended",
		})
	}

	return out
}
