package notificationapp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func TestEventHandler_CreatesNotificationsAndUnreadFlow(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()

	// A booking request notifies the host.
	dispatcher.Publish(ctx, event.BookingRequested{
		BookingID: uuid.New(), PropertyID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID,
	})
	// A message notifies its recipient (the host here).
	dispatcher.Publish(ctx, event.MessageSent{
		ConversationID: uuid.New(), SenderID: guestID, RecipientID: hostID,
	})

	unread, err := svc.UnreadCount(ctx, hostID)
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if unread != 2 {
		t.Fatalf("host unread = %d, want 2", unread)
	}

	// The guest received nothing.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 0 {
		t.Fatalf("guest unread = %d, want 0", c)
	}

	page, err := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("listed %d notifications, want 2", len(page.Items))
	}

	// Marking one read drops the unread count to 1.
	if err := svc.MarkRead(ctx, hostID, page.Items[0].ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Fatalf("unread after mark = %d, want 1", c)
	}

	// Another user cannot mark this notification read.
	if err := svc.MarkRead(ctx, guestID, page.Items[1].ID); err != shared.ErrNotFound {
		t.Fatalf("cross-user mark read err = %v, want ErrNotFound", err)
	}

	// Mark-all clears the rest.
	if err := svc.MarkAllRead(ctx, hostID); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", c)
	}
}

func TestEventHandler_BookingCompletedPromptsBothParties(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingCompleted{
		BookingID: uuid.New(), PropertyID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID,
	})

	// Both the guest and the host are prompted to review.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Fatalf("guest review prompts = %d, want 1", c)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Fatalf("host review prompts = %d, want 1", c)
	}
}

// S86 — ExperienceBooking lifecycle fans out the same way as property
// booking events: host learns about a new booking, guest about the
// confirmation, the OTHER party about a cancel.
func TestEventHandler_ExperienceBookingLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()
	bookingID := uuid.New()
	expID := uuid.New()

	// Created → host notified
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: bookingID, ExperienceID: expID, ExperienceTitle: "Pasta workshop",
		HostID: hostID, GuestID: guestID, TotalCents: 6600, Currency: "EUR",
	})
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Errorf("host unread after Created = %d, want 1", c)
	}
	if c, _ := svc.UnreadCount(ctx, guestID); c != 0 {
		t.Errorf("guest unread after Created = %d, want 0", c)
	}

	// Confirmed → guest notified
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingConfirmed{
		BookingID: bookingID, ExperienceID: expID, ExperienceTitle: "Pasta workshop",
		HostID: hostID, GuestID: guestID,
	})
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Errorf("guest unread after Confirmed = %d, want 1", c)
	}

	// Cancelled by GUEST → HOST notified (the other party)
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID: bookingID, ExperienceID: expID, ExperienceTitle: "Pasta workshop",
		HostID: hostID, GuestID: guestID, CancelledBy: guestID,
	})
	if c, _ := svc.UnreadCount(ctx, hostID); c != 2 {
		t.Errorf("host unread after guest Cancelled = %d, want 2", c)
	}
	// Guest stayed at 1 — canceller doesn't get notified of own action.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Errorf("guest unread should stay at 1, got %d", c)
	}

	// Verify the notification types are the ExperienceBooking-specific ones.
	page, err := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := map[notification.Type]bool{}
	for _, n := range page.Items {
		found[n.Type] = true
	}
	if !found[notification.TypeExperienceBookingRequested] {
		t.Error("missing TypeExperienceBookingRequested")
	}
	if !found[notification.TypeExperienceBookingCancelled] {
		t.Error("missing TypeExperienceBookingCancelled")
	}
}

// Title fallback: when the event arrives with an empty ExperienceTitle
// the subscriber still creates a notification (using a generic phrase)
// rather than rendering "" in user-facing copy.
func TestEventHandler_ExperienceBookingTitleFallback(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: uuid.New(), ExperienceID: uuid.New(), ExperienceTitle: "",
		HostID: hostID, GuestID: uuid.New(),
	})
	page, _ := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(page.Items))
	}
	if page.Items[0].Body == "" {
		t.Error("notification body should not be empty when title is missing")
	}
}

// TestEventHandler_SplitPaymentCompletedNotifiesOrganizerAndPayers covers
// WF-GAP-011: when every share of a split-payment plan is authorised, the
// organizer (booking.GuestID) is told their trip is booked, and every other
// payer is told the group plan they contributed to is now fully funded.
func TestEventHandler_SplitPaymentCompletedNotifiesOrganizerAndPayers(t *testing.T) {
	ctx := context.Background()
	notifRepo := memory.NewNotificationRepository()
	bookingRepo := memory.NewBookingRepository()
	splitRepo := memory.NewSplitPaymentRepository()
	svc := notificationapp.NewService(notifRepo).
		WithBookings(bookingRepo).
		WithSplitPayments(splitRepo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	organizerID := uuid.New()
	payerID := uuid.New()
	bookingID := uuid.New()

	// Seed a confirmed booking owned by the organizer.
	b := &booking.Booking{
		ID:         bookingID,
		PropertyID: uuid.New(),
		GuestID:    organizerID,
		Status:     booking.StatusConfirmed,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := bookingRepo.Create(ctx, b); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	// Seed a completed split with both shares paid: organizer + one payer.
	splitID := uuid.New()
	now := time.Now().UTC()
	sp := &splitpayment.SplitPayment{
		ID:          splitID,
		BookingID:   bookingID,
		OrganizerID: organizerID,
		Currency:    "EUR",
		TotalCents:  10000,
		Status:      splitpayment.StatusCompleted,
		Shares: []splitpayment.Share{
			{
				ID:             uuid.New(),
				SplitPaymentID: splitID,
				PayerEmail:     "alice@test.dev",
				PayerUserID:    &organizerID,
				AmountCents:    5000,
				Status:         splitpayment.SharePaid,
				PaidAt:         &now,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			{
				ID:             uuid.New(),
				SplitPaymentID: splitID,
				PayerEmail:     "bob@test.dev",
				PayerUserID:    &payerID,
				AmountCents:    5000,
				Status:         splitpayment.SharePaid,
				PaidAt:         &now,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		},
		CreatedAt:   now,
		UpdatedAt:   now,
		CompletedAt: &now,
	}
	if err := splitRepo.Create(ctx, sp); err != nil {
		t.Fatalf("seed split: %v", err)
	}

	dispatcher.Publish(ctx, event.SplitPaymentCompleted{
		SplitPaymentID: splitID,
		BookingID:      bookingID,
	})

	// Organizer gets exactly one TypeSplitPaymentCompleted notification.
	orgPage, err := svc.List(ctx, organizerID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list organizer: %v", err)
	}
	if len(orgPage.Items) != 1 {
		t.Fatalf("organizer notifications = %d, want 1", len(orgPage.Items))
	}
	if orgPage.Items[0].Type != notification.TypeSplitPaymentCompleted {
		t.Fatalf("organizer notification type = %q, want %q", orgPage.Items[0].Type, notification.TypeSplitPaymentCompleted)
	}
	if orgPage.Items[0].RelatedID != bookingID {
		t.Fatalf("organizer notification related = %s, want booking %s", orgPage.Items[0].RelatedID, bookingID)
	}

	// Other payer also gets a TypeSplitPaymentCompleted notification.
	payerPage, err := svc.List(ctx, payerID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list payer: %v", err)
	}
	if len(payerPage.Items) != 1 {
		t.Fatalf("payer notifications = %d, want 1", len(payerPage.Items))
	}
	if payerPage.Items[0].Type != notification.TypeSplitPaymentCompleted {
		t.Fatalf("payer notification type = %q, want %q", payerPage.Items[0].Type, notification.TypeSplitPaymentCompleted)
	}
}

// TestEventHandler_SplitPaymentCompletedSkipsWhenReposMissing guards the
// fluent-setter contract: the subscriber must not panic and must not create
// notifications when the optional repos haven't been wired. Older call sites
// (and most existing tests) depend on this.
func TestEventHandler_SplitPaymentCompletedSkipsWhenReposMissing(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo) // no WithBookings / WithSplitPayments

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	dispatcher.Publish(ctx, event.SplitPaymentCompleted{
		SplitPaymentID: uuid.New(),
		BookingID:      uuid.New(),
	})
	// No panic and no notifications created.
	if c, _ := svc.UnreadCount(ctx, uuid.New()); c != 0 {
		t.Fatalf("notifications created without repos: count=%d", c)
	}
}

// TestEventHandler_OfferEventsNotifyTheRightParty — S99 / WF-GAP-008.
// OfferCreated → guest; OfferDeclined → host; OfferWithdrawn → guest.
func TestEventHandler_OfferEventsNotifyTheRightParty(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()

	dispatcher.Publish(ctx, event.OfferCreated{
		OfferID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID, Kind: "special_offer",
	})
	if c, _ := svc.UnreadCount(ctx, guestID); c != 1 {
		t.Errorf("guest unread after Created = %d, want 1", c)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 0 {
		t.Errorf("host unread after Created = %d, want 0", c)
	}

	dispatcher.Publish(ctx, event.OfferDeclined{
		OfferID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID,
	})
	if c, _ := svc.UnreadCount(ctx, hostID); c != 1 {
		t.Errorf("host unread after Declined = %d, want 1", c)
	}

	dispatcher.Publish(ctx, event.OfferWithdrawn{
		OfferID: uuid.New(), PropertyTitle: "Loft", HostID: hostID, GuestID: guestID,
	})
	if c, _ := svc.UnreadCount(ctx, guestID); c != 2 {
		t.Errorf("guest unread after Withdrawn = %d, want 2", c)
	}
}

// TestEventHandler_KYCStepUpRequired_CreatesNotificationForGuest — S140 /
// WF-GAP-019 in-app arm. When the booking service emits the high-value
// gate event, exactly one notification row is created for the guest with
// TypeKYCStepUpRequired and a body that names the listing so the guest
// can deep-link back. RelatedID points at the listing's PropertyID (the
// resource the user taps to retry the booking flow), NOT at a booking
// (no booking exists — the step-up rejected the create).
func TestEventHandler_KYCStepUpRequired_CreatesNotificationForGuest(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	guestID := uuid.New()
	propID := uuid.New()

	dispatcher.Publish(ctx, event.KYCStepUpRequired{
		GuestID:        guestID,
		PropertyID:     propID,
		PropertyTitle:  "Sunny Flat",
		ThresholdCents: 50000,
		TotalCents:     75000,
		Currency:       "EUR",
	})

	page, err := svc.List(ctx, guestID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list guest: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("guest notifications = %d, want 1 (%+v)", len(page.Items), page.Items)
	}
	got := page.Items[0]
	if got.Type != notification.TypeKYCStepUpRequired {
		t.Fatalf("notification type = %q, want %q", got.Type, notification.TypeKYCStepUpRequired)
	}
	if got.RelatedID != propID {
		t.Fatalf("notification related = %s, want PropertyID %s", got.RelatedID, propID)
	}
	if !strings.Contains(got.Body, "Sunny Flat") {
		t.Fatalf("notification body should reference the property title, got %q", got.Body)
	}
}

// TestEventHandler_CohostInvitedNotifiesInvitee — S99 / WF-GAP-016.
func TestEventHandler_CohostInvitedNotifiesInvitee(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()
	svc := notificationapp.NewService(repo)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	inviteeID := uuid.New()
	propID := uuid.New()

	dispatcher.Publish(ctx, event.CohostInvited{
		CohostID: uuid.New(), PropertyID: propID, PropertyTitle: "Beach House",
		HostID: hostID, UserID: inviteeID, Permissions: []string{"reply_messages"},
	})
	if c, _ := svc.UnreadCount(ctx, inviteeID); c != 1 {
		t.Errorf("invitee unread = %d, want 1", c)
	}
	if c, _ := svc.UnreadCount(ctx, hostID); c != 0 {
		t.Errorf("host unread = %d, want 0 (the host is the one who invited)", c)
	}
}

// TestEventHandler_ReviewSubmitted_GuestToProperty_NotifiesHost — S148.
// A guest_to_property review notifies the listing's host. RelatedID is
// the ReviewID (so the in-app deep link lands on the review itself, not
// the listing) and the host is resolved via the properties repo so the
// event payload stays lean.
func TestEventHandler_ReviewSubmitted_GuestToProperty_NotifiesHost(t *testing.T) {
	ctx := context.Background()
	notifRepo := memory.NewNotificationRepository()
	propRepo := memory.NewPropertyRepository()
	svc := notificationapp.NewService(notifRepo).WithProperties(propRepo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()
	propID := uuid.New()
	reviewID := uuid.New()

	// Seed the listing so the subscriber can resolve the host.
	p := &property.Property{
		ID:     propID,
		HostID: hostID,
		Title:  "Sunny Flat",
	}
	if err := propRepo.Create(ctx, p); err != nil {
		t.Fatalf("seed property: %v", err)
	}

	dispatcher.Publish(ctx, event.ReviewSubmitted{
		ReviewID:   reviewID,
		BookingID:  uuid.New(),
		PropertyID: propID,
		AuthorID:   guestID,
		GuestID:    guestID,
		Direction:  "guest_to_property",
		Rating:     5,
	})

	// Host has exactly one notification.
	hostPage, err := svc.List(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list host: %v", err)
	}
	if len(hostPage.Items) != 1 {
		t.Fatalf("host notifications = %d, want 1 (%+v)", len(hostPage.Items), hostPage.Items)
	}
	got := hostPage.Items[0]
	if got.Type != notification.TypeReviewSubmitted {
		t.Fatalf("notification type = %q, want %q", got.Type, notification.TypeReviewSubmitted)
	}
	if got.RelatedID != reviewID {
		t.Fatalf("notification related = %s, want ReviewID %s", got.RelatedID, reviewID)
	}
	if !strings.Contains(got.Body, "Sunny Flat") {
		t.Fatalf("notification body should reference the property title, got %q", got.Body)
	}

	// Guest got nothing — they wrote the review.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 0 {
		t.Fatalf("guest unread = %d, want 0 (author shouldn't be notified)", c)
	}
}

// TestEventHandler_ReviewSubmitted_HostToGuest_NotifiesGuest — S148.
// A host_to_guest review notifies the guest. No property lookup is
// needed because GuestID is on the event payload.
func TestEventHandler_ReviewSubmitted_HostToGuest_NotifiesGuest(t *testing.T) {
	ctx := context.Background()
	notifRepo := memory.NewNotificationRepository()
	// Deliberately wire no properties repo — the host_to_guest branch
	// must not depend on it.
	svc := notificationapp.NewService(notifRepo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	hostID := uuid.New()
	guestID := uuid.New()
	reviewID := uuid.New()

	dispatcher.Publish(ctx, event.ReviewSubmitted{
		ReviewID:   reviewID,
		BookingID:  uuid.New(),
		PropertyID: uuid.New(),
		AuthorID:   hostID,
		GuestID:    guestID,
		Direction:  "host_to_guest",
		Rating:     4,
	})

	guestPage, err := svc.List(ctx, guestID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list guest: %v", err)
	}
	if len(guestPage.Items) != 1 {
		t.Fatalf("guest notifications = %d, want 1 (%+v)", len(guestPage.Items), guestPage.Items)
	}
	got := guestPage.Items[0]
	if got.Type != notification.TypeReviewSubmitted {
		t.Fatalf("notification type = %q, want %q", got.Type, notification.TypeReviewSubmitted)
	}
	if got.RelatedID != reviewID {
		t.Fatalf("notification related = %s, want ReviewID %s", got.RelatedID, reviewID)
	}

	// Host got nothing — they wrote the review.
	if c, _ := svc.UnreadCount(ctx, hostID); c != 0 {
		t.Fatalf("host unread = %d, want 0 (author shouldn't be notified)", c)
	}
}

// TestEventHandler_ReviewSubmitted_PropertyMissingShortCircuits — S148.
// Belt-and-braces: if the listing has been deleted between the review
// being emitted and the relay consuming the event, the subscriber must
// log and short-circuit rather than panic or fabricate a notification
// with no real recipient.
func TestEventHandler_ReviewSubmitted_PropertyMissingShortCircuits(t *testing.T) {
	ctx := context.Background()
	notifRepo := memory.NewNotificationRepository()
	propRepo := memory.NewPropertyRepository() // empty — FindByID returns ErrNotFound
	svc := notificationapp.NewService(notifRepo).WithProperties(propRepo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	guestID := uuid.New()
	dispatcher.Publish(ctx, event.ReviewSubmitted{
		ReviewID:   uuid.New(),
		BookingID:  uuid.New(),
		PropertyID: uuid.New(), // never seeded
		AuthorID:   guestID,
		GuestID:    guestID,
		Direction:  "guest_to_property",
		Rating:     5,
	})

	// No notification anywhere — the lookup failed and we logged.
	if c, _ := svc.UnreadCount(ctx, guestID); c != 0 {
		t.Fatalf("guest unread = %d, want 0", c)
	}
}
