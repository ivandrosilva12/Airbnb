package splitpaymentapp_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	splitpaymentapp "github.com/airhost/backend/internal/application/splitpayment"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// recordingGateway is a programmable PaymentGateway for the split-payment
// tests. It records every Authorize / Refund call and lets a test reject a
// specific share's authorize so the partial-failure path can be exercised.
//
// failOn keys are the idempotency keys the splitpayment service builds —
// "split:<splitID>:<shareID>" — so a test can refuse one specific share
// without disturbing the others.
type recordingGateway struct {
	mu               sync.Mutex
	authorizeCalls   []gatewayCall
	refundCalls      []gatewayCall
	failOn           map[string]bool // idempotency keys to refuse on Authorize
	refundFailOnRefs map[string]bool // gateway refs to refuse on Refund
	nextRef          int
}

type gatewayCall struct {
	IdempotencyKey string
	Ref            string
	AmountCents    int64
	Currency       string
}

func newRecordingGateway() *recordingGateway {
	return &recordingGateway{
		failOn:           map[string]bool{},
		refundFailOnRefs: map[string]bool{},
	}
}

func (g *recordingGateway) Authorize(_ context.Context, amount shared.Money, idem string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.authorizeCalls = append(g.authorizeCalls, gatewayCall{
		IdempotencyKey: idem, AmountCents: amount.AmountCents(), Currency: amount.Currency(),
	})
	if g.failOn[idem] {
		return "", fmt.Errorf("gateway: card declined for %s", idem)
	}
	g.nextRef++
	ref := fmt.Sprintf("gw_%d", g.nextRef)
	// Keep the ref discoverable from the idempotency key for assertions.
	if strings.Contains(idem, "split:") {
		ref = "gw_" + idem
	}
	return ref, nil
}

func (g *recordingGateway) Capture(_ context.Context, _ string) error { return nil }

func (g *recordingGateway) Refund(_ context.Context, ref string, amt int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refundCalls = append(g.refundCalls, gatewayCall{Ref: ref, AmountCents: amt})
	if g.refundFailOnRefs[ref] {
		return fmt.Errorf("gateway: refund refused for %s", ref)
	}
	return nil
}

func (g *recordingGateway) authorizedKeys() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.authorizeCalls))
	for _, c := range g.authorizeCalls {
		out = append(out, c.IdempotencyKey)
	}
	return out
}

func (g *recordingGateway) refundedRefs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.refundCalls))
	for _, c := range g.refundCalls {
		out = append(out, c.Ref)
	}
	return out
}

var _ port.PaymentGateway = (*recordingGateway)(nil)

// capturingPublisher records every event that flows through the in-process
// dispatcher (after the outbox commits). The S31 service publishes via the
// UnitOfWork's outbox; the relay then dispatches to handlers, which is where
// we hook this capturer in.
type capturingPublisher struct {
	mu     sync.Mutex
	events []event.Event
}

func (p *capturingPublisher) capture(_ context.Context, ev event.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *capturingPublisher) names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.events))
	for _, ev := range p.events {
		out = append(out, ev.EventName())
	}
	return out
}

// splitFixture is the common harness: organizer + invitee user accounts,
// in-memory repos, a capturing handler subscribed to the dispatcher, and the
// service wired through a real UnitOfWork so the completion event flows
// outbox → relay → dispatcher → handler (matching production semantics).
type splitFixture struct {
	svc        *splitpaymentapp.Service
	splits     *memory.SplitPaymentRepository
	users      *memory.UserRepository
	publisher  *capturingPublisher
	gateway    *recordingGateway
	dispatcher *event.Dispatcher
	organizer  *user.User
	invitee    *user.User
	stranger   *user.User
	bookingID  uuid.UUID
}

func newSplitFixture(t *testing.T) *splitFixture {
	return newSplitFixtureWithGateway(t, nil)
}

// newSplitFixtureWithGateway builds the fixture with a programmable gateway
// wired into the service (S88 path). Tests that don't exercise the gateway
// path use newSplitFixture and get the legacy trust-mode service.
func newSplitFixtureWithGateway(t *testing.T, gw *recordingGateway) *splitFixture {
	t.Helper()
	ctx := context.Background()
	users := memory.NewUserRepository()
	splits := memory.NewSplitPaymentRepository()
	pub := &capturingPublisher{}

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(pub.capture)
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, dispatcher)
	uow := memory.NewUnitOfWork(nil, nil, nil, splits, nil, outbox, relay)
	svc := splitpaymentapp.NewService(splits, users, uow)
	if gw != nil {
		svc = svc.WithGateway(gw)
	}

	mustUser := func(email string) *user.User {
		u, err := user.NewUser("kc-"+email, email, "Test "+email, user.RoleGuest)
		if err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("save user: %v", err)
		}
		return u
	}
	organizer := mustUser("alice@test.dev")
	invitee := mustUser("bob@test.dev")
	stranger := mustUser("eve@test.dev")

	return &splitFixture{
		svc: svc, splits: splits, users: users, publisher: pub,
		gateway: gw, dispatcher: dispatcher,
		organizer: organizer, invitee: invitee, stranger: stranger,
		bookingID: uuid.New(),
	}
}

func (f *splitFixture) seedSplit(t *testing.T) *splitpayment.SplitPayment {
	t.Helper()
	sp, err := f.svc.Create(context.Background(), splitpaymentapp.CreateInput{
		BookingID: f.bookingID, OrganizerID: f.organizer.ID, OrganizerEmail: f.organizer.Email,
		Currency: "EUR", TotalCents: 10000,
		Shares: []splitpayment.ShareInput{
			{Email: f.organizer.Email, AmountCents: 5000},
			{Email: f.invitee.Email, AmountCents: 5000},
		},
	})
	if err != nil {
		t.Fatalf("create split: %v", err)
	}
	return sp
}

// TestCreatePersistsSplit — happy path.
func TestCreatePersistsSplit(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	if sp.Status != splitpayment.StatusPending {
		t.Fatalf("status = %v, want pending", sp.Status)
	}
	if len(sp.Shares) != 2 {
		t.Fatalf("shares = %d, want 2", len(sp.Shares))
	}
}

// TestAuthorizeMyShareNotCompletedYet — first payer authorises; the split
// stays pending (one share still unpaid); no completion event published.
func TestAuthorizeMyShareNotCompletedYet(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	myShare := sp.Shares[0] // organizer's share
	after, err := f.svc.AuthorizeShare(context.Background(), f.organizer.ID, sp.ID, myShare.ID)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if after.Status != splitpayment.StatusPending {
		t.Fatalf("status = %v, want pending after 1 of 2", after.Status)
	}
	if len(f.publisher.names()) != 0 {
		t.Fatalf("publisher events = %v, want none yet", f.publisher.names())
	}
}

// TestLastShareCompletesAndPublishes — once every share is paid the split
// transitions to completed and SplitPaymentCompleted is fanned out so the
// booking context can confirm the reservation.
func TestLastShareCompletesAndPublishes(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	ctx := context.Background()
	// Organizer pays their share.
	if _, err := f.svc.AuthorizeShare(ctx, f.organizer.ID, sp.ID, sp.Shares[0].ID); err != nil {
		t.Fatalf("authorize organizer: %v", err)
	}
	// Invitee pays the second (last) share.
	after, err := f.svc.AuthorizeShare(ctx, f.invitee.ID, sp.ID, sp.Shares[1].ID)
	if err != nil {
		t.Fatalf("authorize invitee: %v", err)
	}
	if after.Status != splitpayment.StatusCompleted {
		t.Fatalf("status = %v, want completed", after.Status)
	}
	names := f.publisher.names()
	if len(names) != 1 || names[0] != "splitpayment.completed" {
		t.Fatalf("publisher events = %v, want exactly [splitpayment.completed]", names)
	}
}

// TestAuthorizeShareRejectsStranger — a third party whose email doesn't
// appear in the shares cannot mark anyone's share as paid.
func TestAuthorizeShareRejectsStranger(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	_, err := f.svc.AuthorizeShare(context.Background(), f.stranger.ID, sp.ID, sp.Shares[1].ID)
	if err == nil {
		t.Fatalf("authorize by stranger: expected error, got nil")
	}
}

// TestCancelOnlyOrganizer — only the organizer can cancel the split.
func TestCancelOnlyOrganizer(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	ctx := context.Background()
	if _, err := f.svc.Cancel(ctx, f.invitee.ID, sp.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("cancel by invitee: err = %v, want ErrForbidden", err)
	}
	after, err := f.svc.Cancel(ctx, f.organizer.ID, sp.ID)
	if err != nil {
		t.Fatalf("cancel by organizer: %v", err)
	}
	if after.Status != splitpayment.StatusCancelled {
		t.Fatalf("status = %v, want cancelled", after.Status)
	}
}

// TestGetByIDGatedByEmailOrOrganizer — only the organizer or a payer
// (matched by email on their user record) can read the split.
func TestGetByIDGatedByEmailOrOrganizer(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	ctx := context.Background()
	if _, err := f.svc.GetByID(ctx, f.organizer.ID, sp.ID); err != nil {
		t.Fatalf("organizer get: %v", err)
	}
	if _, err := f.svc.GetByID(ctx, f.invitee.ID, sp.ID); err != nil {
		t.Fatalf("invitee get: %v", err)
	}
	if _, err := f.svc.GetByID(ctx, f.stranger.ID, sp.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("stranger get: err = %v, want ErrForbidden", err)
	}
}

// TestListMineSeesOrganizerAndPayerSplits — invitee's ListMine surfaces a
// split where they're only a payer (matched by lower-case email).
func TestListMineSeesOrganizerAndPayerSplits(t *testing.T) {
	f := newSplitFixture(t)
	f.seedSplit(t)
	items, err := f.svc.ListMine(context.Background(), f.invitee.ID)
	if err != nil {
		t.Fatalf("list invitee: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("invitee ListMine len = %d, want 1", len(items))
	}
	// Stranger sees nothing.
	items, err = f.svc.ListMine(context.Background(), f.stranger.ID)
	if err != nil {
		t.Fatalf("list stranger: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("stranger ListMine len = %d, want 0", len(items))
	}
}

// --- S88 — gateway-driven per-share authorize / refund ---------------------

// TestAuthorizeShare_GatewayHitOnAuthorize — when a gateway is wired,
// AuthorizeShare calls Authorize per share, stores the returned reference on
// the share, and emits a SplitShareAuthorized event. (WF-GAP-001.)
func TestAuthorizeShare_GatewayHitOnAuthorize(t *testing.T) {
	gw := newRecordingGateway()
	f := newSplitFixtureWithGateway(t, gw)
	sp := f.seedSplit(t)
	ctx := context.Background()

	if _, err := f.svc.AuthorizeShare(ctx, f.organizer.ID, sp.ID, sp.Shares[0].ID); err != nil {
		t.Fatalf("authorize organizer: %v", err)
	}

	keys := gw.authorizedKeys()
	wantKey := "split:" + sp.ID.String() + ":" + sp.Shares[0].ID.String()
	if len(keys) != 1 || keys[0] != wantKey {
		t.Fatalf("gateway authorize calls = %v, want exactly [%s]", keys, wantKey)
	}
	// The share now has a GatewayRef and a SplitShareAuthorized event was
	// published (the in-memory dispatcher fans it out synchronously after
	// the outbox commits).
	got, err := f.splits.FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	if got.Shares[0].Status != splitpayment.SharePaid {
		t.Fatalf("share status = %v, want paid", got.Shares[0].Status)
	}
	if got.Shares[0].GatewayRef == nil || *got.Shares[0].GatewayRef == "" {
		t.Fatalf("share gateway ref = %v, want a stored ref", got.Shares[0].GatewayRef)
	}
	names := f.publisher.names()
	wantAuthorized := false
	for _, n := range names {
		if n == "splitpayment.share.authorized" {
			wantAuthorized = true
		}
	}
	if !wantAuthorized {
		t.Fatalf("publisher events = %v, want at least one splitpayment.share.authorized", names)
	}
}

// TestAuthorizeShare_PartialGatewayFailure — when the gateway refuses one
// share but accepts the other, the failed share is marked failed (audit
// trail; payer can retry), the split stays pending, and the other payer can
// still authorise their slice. (WF-GAP-001 partial-failure invariant.)
func TestAuthorizeShare_PartialGatewayFailure(t *testing.T) {
	gw := newRecordingGateway()
	f := newSplitFixtureWithGateway(t, gw)
	sp := f.seedSplit(t)
	ctx := context.Background()

	// Programme the gateway to refuse the invitee's share.
	failKey := "split:" + sp.ID.String() + ":" + sp.Shares[1].ID.String()
	gw.failOn[failKey] = true

	// The invitee tries first and is rejected — but the split is NOT
	// blown up; the failed share moves to ShareFailed.
	if _, err := f.svc.AuthorizeShare(ctx, f.invitee.ID, sp.ID, sp.Shares[1].ID); err == nil {
		t.Fatalf("expected an error from the rejected gateway authorize")
	}
	got, err := f.splits.FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	if got.Shares[1].Status != splitpayment.ShareFailed {
		t.Fatalf("invitee share status = %v, want failed", got.Shares[1].Status)
	}
	if got.Status != splitpayment.StatusPending {
		t.Fatalf("split status = %v, want pending (failed share must not collapse the split)", got.Status)
	}

	// The organizer can still authorise their own share through the same
	// gateway — the failure was per-share, not per-split.
	if _, err := f.svc.AuthorizeShare(ctx, f.organizer.ID, sp.ID, sp.Shares[0].ID); err != nil {
		t.Fatalf("authorize organizer after invitee failure: %v", err)
	}
	got, _ = f.splits.FindByID(ctx, sp.ID)
	if got.Shares[0].Status != splitpayment.SharePaid {
		t.Fatalf("organizer share status = %v, want paid", got.Shares[0].Status)
	}
	if got.Shares[1].Status != splitpayment.ShareFailed {
		t.Fatalf("invitee share status after organizer success = %v, want still failed", got.Shares[1].Status)
	}
	// The invitee retries on a now-healthy gateway and goes through.
	delete(gw.failOn, failKey)
	if _, err := f.svc.AuthorizeShare(ctx, f.invitee.ID, sp.ID, sp.Shares[1].ID); err != nil {
		t.Fatalf("invitee retry: %v", err)
	}
	got, _ = f.splits.FindByID(ctx, sp.ID)
	if got.Status != splitpayment.StatusCompleted {
		t.Fatalf("split status after retry = %v, want completed", got.Status)
	}
}

// TestCancel_RefundsAuthorizedShares — Cancel releases each previously-paid
// share via gateway.Refund, marks each share refunded, and the publisher
// sees one SplitShareRefunded event per refunded share. (WF-GAP-005 via
// the organizer-initiated cancel path.)
func TestCancel_RefundsAuthorizedShares(t *testing.T) {
	gw := newRecordingGateway()
	f := newSplitFixtureWithGateway(t, gw)
	sp := f.seedSplit(t)
	ctx := context.Background()

	// Both payers authorise; the split is now completed and has two
	// shares with gateway refs.
	if _, err := f.svc.AuthorizeShare(ctx, f.organizer.ID, sp.ID, sp.Shares[0].ID); err != nil {
		t.Fatalf("authorize organizer: %v", err)
	}
	// Half-way through: cancel after only one share is paid so we hit
	// the mixed-state branch (one paid, one pending). The refund path
	// must touch ONLY the paid share.
	if _, err := f.svc.Cancel(ctx, f.organizer.ID, sp.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Exactly one refund call — the organizer's authorized share.
	refunded := gw.refundedRefs()
	if len(refunded) != 1 {
		t.Fatalf("refund calls = %v, want exactly 1 (only the paid share)", refunded)
	}
	got, err := f.splits.FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	if got.Shares[0].Status != splitpayment.ShareRefunded {
		t.Fatalf("paid share after cancel = %v, want refunded", got.Shares[0].Status)
	}
	if got.Shares[1].Status != splitpayment.SharePending {
		t.Fatalf("pending share after cancel = %v, want still pending (nothing to refund)", got.Shares[1].Status)
	}
	if got.Status != splitpayment.StatusCancelled {
		t.Fatalf("split status = %v, want cancelled", got.Status)
	}
}

// TestEventHandler_BookingCancelledRefundsShares — the BookingCancelled
// subscriber on the splitpayment service mirrors the organizer-initiated
// refund path for cancellations that originate outside the splitpayment
// surface (e.g. host/guest cancels via bookingapp.Service.Cancel).
// Each paid share gets a gateway refund and one SplitShareRefunded event.
// (WF-GAP-005, the asynchronous subscriber path.)
func TestEventHandler_BookingCancelledRefundsShares(t *testing.T) {
	gw := newRecordingGateway()
	f := newSplitFixtureWithGateway(t, gw)
	sp := f.seedSplit(t)
	ctx := context.Background()

	// Both payers authorise so each share has a gateway ref to refund.
	if _, err := f.svc.AuthorizeShare(ctx, f.organizer.ID, sp.ID, sp.Shares[0].ID); err != nil {
		t.Fatalf("authorize organizer: %v", err)
	}
	if _, err := f.svc.AuthorizeShare(ctx, f.invitee.ID, sp.ID, sp.Shares[1].ID); err != nil {
		t.Fatalf("authorize invitee: %v", err)
	}

	// Re-use the dispatcher the fixture wired (the publisher is already
	// subscribed). Subscribe the EventHandler too so we exercise the
	// subscriber path end-to-end.
	f.dispatcher.Subscribe(f.svc.EventHandler())
	f.dispatcher.Publish(ctx, event.BookingCancelled{
		BookingID: sp.BookingID,
		GuestID:   f.organizer.ID,
	})

	if got := len(gw.refundedRefs()); got != 2 {
		t.Fatalf("refund calls = %d, want 2 (one per paid share)", got)
	}
	got, err := f.splits.FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	for i, sh := range got.Shares {
		if sh.Status != splitpayment.ShareRefunded {
			t.Fatalf("share[%d] status = %v, want refunded", i, sh.Status)
		}
	}
	// Two SplitShareRefunded events were published (one per share).
	refundedEvents := 0
	for _, n := range f.publisher.names() {
		if n == "splitpayment.share.refunded" {
			refundedEvents++
		}
	}
	if refundedEvents != 2 {
		t.Fatalf("share.refunded events = %d, want 2 (got: %v)", refundedEvents, f.publisher.names())
	}
}

// TestEventHandler_BookingWithoutSplitIsNoOp — a BookingCancelled for a
// booking that has no split must not blow up the subscriber (the splitpayment
// BC is only responsible for split-paid bookings).
func TestEventHandler_BookingWithoutSplitIsNoOp(t *testing.T) {
	gw := newRecordingGateway()
	f := newSplitFixtureWithGateway(t, gw)
	f.dispatcher.Subscribe(f.svc.EventHandler())
	f.dispatcher.Publish(context.Background(), event.BookingCancelled{BookingID: uuid.New()})
	if got := len(gw.refundedRefs()); got != 0 {
		t.Fatalf("refund calls for non-split booking = %d, want 0", got)
	}
}
