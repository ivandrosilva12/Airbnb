package pushtokenapp_test

import (
	"context"
	"sync"
	"testing"

	"github.com/airhost/backend/internal/application/port"
	pushtokenapp "github.com/airhost/backend/internal/application/pushtoken"
	"github.com/airhost/backend/internal/domain/pushtoken"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// fakeSender records every Send call and lets each test stage which tokens to
// report as invalid.
type fakeSender struct {
	mu      sync.Mutex
	calls   []port.PushPayload
	tokens  [][]pushtoken.Token
	invalid map[string]bool // token string -> Invalid
}

func newFakeSender() *fakeSender { return &fakeSender{invalid: map[string]bool{}} }

func (f *fakeSender) Send(_ context.Context, devices []pushtoken.Token, payload port.PushPayload) []port.PushSendResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, payload)
	cp := make([]pushtoken.Token, len(devices))
	copy(cp, devices)
	f.tokens = append(f.tokens, cp)
	out := make([]port.PushSendResult, 0, len(devices))
	for _, d := range devices {
		out = append(out, port.PushSendResult{Token: d, Invalid: f.invalid[d.Token]})
	}
	return out
}

func TestRegister_StoresTokenAndUpsertsOnReRegister(t *testing.T) {
	users := memory.NewUserRepository()
	tokens := memory.NewPushTokenRepository()
	svc := pushtokenapp.NewService(tokens, users, nil)

	u := mustUser(t, users, "guest@example.com", domainuser.RoleGuest)

	if _, err := svc.Register(context.Background(), u.ID, pushtoken.PlatformAndroid, "abc", ""); err != nil {
		t.Fatalf("register #1: %v", err)
	}
	if _, err := svc.Register(context.Background(), u.ID, pushtoken.PlatformAndroid, "abc", ""); err != nil {
		t.Fatalf("register #2: %v", err)
	}

	list, err := svc.ListForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected upsert to keep a single row, got %d", len(list))
	}
}

func TestPush_RespectsCategoryOptOuts(t *testing.T) {
	users := memory.NewUserRepository()
	tokens := memory.NewPushTokenRepository()
	sender := newFakeSender()
	svc := pushtokenapp.NewService(tokens, users, sender)

	u := mustUser(t, users, "g2@example.com", domainuser.RoleGuest)
	u.SetPushPreferences(domainuser.PushPreferences{Bookings: false, Messages: true})
	if err := users.Update(context.Background(), u); err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	if _, err := svc.Register(context.Background(), u.ID, pushtoken.PlatformIOS, "ios-1", ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Bookings is opted out -> no push.
	if err := svc.Push(context.Background(), u.ID, pushtokenapp.CatBookings, "t", "b", nil); err != nil {
		t.Fatalf("push bookings: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected push suppressed for opted-out category, got %d calls", len(sender.calls))
	}

	// Messages is opted in -> push goes through.
	if err := svc.Push(context.Background(), u.ID, pushtokenapp.CatMessages, "t", "b", nil); err != nil {
		t.Fatalf("push messages: %v", err)
	}
	if len(sender.calls) != 1 || sender.calls[0].Title != "t" {
		t.Fatalf("expected one push delivered, got %#v", sender.calls)
	}

	// Account is never gated.
	u.SetPushPreferences(domainuser.PushPreferences{Bookings: false, Messages: false})
	_ = users.Update(context.Background(), u)
	if err := svc.Push(context.Background(), u.ID, pushtokenapp.CatAccount, "x", "y", nil); err != nil {
		t.Fatalf("push account: %v", err)
	}
	if len(sender.calls) != 2 {
		t.Fatalf("expected account push to bypass opt-outs, got %d calls", len(sender.calls))
	}
}

func TestPush_PrunesInvalidTokensReportedByProvider(t *testing.T) {
	users := memory.NewUserRepository()
	tokens := memory.NewPushTokenRepository()
	sender := newFakeSender()
	svc := pushtokenapp.NewService(tokens, users, sender)

	u := mustUser(t, users, "g3@example.com", domainuser.RoleGuest)
	if _, err := svc.Register(context.Background(), u.ID, pushtoken.PlatformAndroid, "dead", ""); err != nil {
		t.Fatalf("register dead: %v", err)
	}
	if _, err := svc.Register(context.Background(), u.ID, pushtoken.PlatformAndroid, "live", ""); err != nil {
		t.Fatalf("register live: %v", err)
	}
	sender.invalid["dead"] = true

	if err := svc.Push(context.Background(), u.ID, pushtokenapp.CatBookings, "t", "b", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	list, _ := svc.ListForUser(context.Background(), u.ID)
	if len(list) != 1 || list[0].Token != "live" {
		t.Fatalf("expected dead token pruned, got %#v", list)
	}
}

func mustUser(t *testing.T, repo *memory.UserRepository, email string, role domainuser.Role) *domainuser.User {
	t.Helper()
	u, err := domainuser.NewUser("sub-"+email, email, "Test "+string(role), role)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("store user: %v", err)
	}
	return u
}

// Compile-time check that the NotifierAdapter exposed by the package satisfies
// the notificationapp.PushNotifier port shape (loose string category).
var _ interface {
	Push(ctx context.Context, userID uuid.UUID, cat string, title, body string, data map[string]string) error
} = (*pushtokenapp.NotifierAdapter)(nil)
