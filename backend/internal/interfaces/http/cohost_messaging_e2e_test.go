package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// inviteCohostWith is a thin helper that issues a co-host grant from the
// primary host with the given permission set.
func inviteCohostWith(t *testing.T, h *harness, hostTok, propID, email string, perms []string) {
	t.Helper()
	body := map[string]any{"email": email, "permissions": perms}
	rec := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, body)
	mustStatus(t, rec, http.StatusCreated, "invite cohost")
}

// startThreadAsGuest creates a conversation between the guest and the listing
// (and returns its id) by going through POST /conversations.
func startThreadAsGuest(t *testing.T, h *harness, guestTok, propID string) string {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/conversations", guestTok, map[string]any{"propertyId": propID})
	mustStatus(t, rec, http.StatusCreated, "start conversation")
	return h.decode(rec)["id"].(string)
}

// TestEndToEnd_CohostReplyMessages confirms a co-host with reply_messages
// can read and reply to a thread on the host's listing; the reply is
// recorded with the co-host's id as sender, and the host's unread counter
// does not tick on the co-host's reply.
func TestEndToEnd_CohostReplyMessages(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "msg-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "msg-guest@test.dev")
	cohost := h.seedUser(domainuser.RoleGuest, "msg-cohost@test.dev")
	hostTok, guestTok, cohostTok := host.ID.String(), guest.ID.String(), cohost.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)
	inviteCohostWith(t, h, hostTok, propID, cohost.Email, []string{"reply_messages"})

	convID := startThreadAsGuest(t, h, guestTok, propID)
	// Guest sends the first message — the host's unread bumps to 1.
	rec := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", guestTok, map[string]any{"body": "Hi, is the date free?"})
	mustStatus(t, rec, http.StatusCreated, "guest sends initial")

	// Sanity: the host has 1 unread.
	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host unread before reply")
	if unread := h.decode(rec)["unread"].(float64); unread != 1 {
		t.Fatalf("host unread before reply = %v, want 1", unread)
	}

	// Co-host can list the thread's messages.
	rec = h.do(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", cohostTok, nil)
	mustStatus(t, rec, http.StatusOK, "cohost reads")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("cohost sees %d messages, want 1", len(items))
	}

	// Co-host replies on the host's behalf. The reply records the cohost's id.
	rec = h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", cohostTok, map[string]any{"body": "Yes, those dates work — happy to confirm."})
	mustStatus(t, rec, http.StatusCreated, "cohost reply")
	replyView := h.decode(rec)
	if replyView["senderId"] != cohost.ID.String() {
		t.Fatalf("reply senderId = %v, want cohost %s", replyView["senderId"], cohost.ID)
	}

	// The host's unread should NOT have ticked — their team handled it.
	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host unread after reply")
	if unread := h.decode(rec)["unread"].(float64); unread != 0 {
		t.Fatalf("host unread after cohost reply = %v, want 0", unread)
	}

	// The guest, however, now has 1 unread (the cohost's outgoing reply
	// counts as a fresh inbound message for them).
	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "guest unread after cohost reply")
	if unread := h.decode(rec)["unread"].(float64); unread != 1 {
		t.Fatalf("guest unread after cohost reply = %v, want 1", unread)
	}
}

// TestEndToEnd_CohostMailboxListsThreads confirms the team-mailbox endpoint
// surfaces threads on listings the actor is a reply-enabled co-host of, with
// the host-side unread count attached.
func TestEndToEnd_CohostMailboxListsThreads(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "mbox-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "mbox-guest@test.dev")
	cohost := h.seedUser(domainuser.RoleGuest, "mbox-cohost@test.dev")
	hostTok, guestTok, cohostTok := host.ID.String(), guest.ID.String(), cohost.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)
	inviteCohostWith(t, h, hostTok, propID, cohost.Email, []string{"reply_messages"})

	convID := startThreadAsGuest(t, h, guestTok, propID)
	// Guest sends a fresh message so the host (and thus the cohost's mailbox)
	// has 1 outstanding item.
	rec := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", guestTok, map[string]any{"body": "hi"})
	mustStatus(t, rec, http.StatusCreated, "guest sends")

	rec = h.do(http.MethodGet, "/api/v1/me/cohost-mailbox", cohostTok, nil)
	mustStatus(t, rec, http.StatusOK, "cohost mailbox")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("cohost mailbox = %d, want 1", len(items))
	}
	first := items[0].(map[string]any)
	if first["id"] != convID {
		t.Fatalf("mailbox first id = %v, want %s", first["id"], convID)
	}
	if first["unreadCount"].(float64) != 1 {
		t.Fatalf("host-side unread = %v, want 1", first["unreadCount"])
	}
}

// TestEndToEnd_CohostMailboxOnlyWithReplyPerm confirms a calendar-only co-host
// does NOT see the team mailbox (no reply_messages perm).
func TestEndToEnd_CohostMailboxOnlyWithReplyPerm(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "perm-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "perm-guest@test.dev")
	calOnly := h.seedUser(domainuser.RoleGuest, "perm-cal@test.dev")
	hostTok, guestTok, calTok := host.ID.String(), guest.ID.String(), calOnly.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)
	inviteCohostWith(t, h, hostTok, propID, calOnly.Email, []string{"manage_calendar"})

	convID := startThreadAsGuest(t, h, guestTok, propID)
	rec := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", guestTok, map[string]any{"body": "hi"})
	mustStatus(t, rec, http.StatusCreated, "guest sends")

	// Calendar-only cohost cannot read the thread.
	if r := h.do(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", calTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("calendar cohost reads: status = %d, want 403", r.Code)
	}
	// Nor reply.
	if r := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", calTok, map[string]any{"body": "x"}); r.Code != http.StatusForbidden {
		t.Fatalf("calendar cohost reply: status = %d, want 403", r.Code)
	}
	// Mailbox should be empty.
	rec = h.do(http.MethodGet, "/api/v1/me/cohost-mailbox", calTok, nil)
	mustStatus(t, rec, http.StatusOK, "calendar cohost mailbox")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("calendar cohost mailbox = %d, want 0", len(items))
	}
}

// TestEndToEnd_CohostMarkReadAdvancesHost confirms that a co-host's MarkRead
// drops the host's unread counter (the team has handled the thread).
func TestEndToEnd_CohostMarkReadAdvancesHost(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "mr-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "mr-guest@test.dev")
	cohost := h.seedUser(domainuser.RoleGuest, "mr-cohost@test.dev")
	hostTok, guestTok, cohostTok := host.ID.String(), guest.ID.String(), cohost.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)
	inviteCohostWith(t, h, hostTok, propID, cohost.Email, []string{"reply_messages"})

	convID := startThreadAsGuest(t, h, guestTok, propID)
	rec := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", guestTok, map[string]any{"body": "ping"})
	mustStatus(t, rec, http.StatusCreated, "guest sends")

	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", hostTok, nil)
	if h.decode(rec)["unread"].(float64) != 1 {
		t.Fatalf("host unread before mark = %v, want 1", h.decode(rec)["unread"])
	}

	// Co-host marks the thread read on the host's behalf.
	if r := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/read", cohostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("cohost mark read: status = %d, want 204", r.Code)
	}

	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", hostTok, nil)
	if h.decode(rec)["unread"].(float64) != 0 {
		t.Fatalf("host unread after cohost mark read = %v, want 0", h.decode(rec)["unread"])
	}
}

// TestEndToEnd_NonCohostCannotReply confirms a stranger (neither owner nor
// co-host) cannot read or reply to a thread.
func TestEndToEnd_NonCohostCannotReply(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "nc-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "nc-guest@test.dev")
	stranger := h.seedUser(domainuser.RoleGuest, "nc-stranger@test.dev")
	hostTok, guestTok, strangerTok := host.ID.String(), guest.ID.String(), stranger.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)
	convID := startThreadAsGuest(t, h, guestTok, propID)
	rec := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", guestTok, map[string]any{"body": "hi"})
	mustStatus(t, rec, http.StatusCreated, "guest sends")

	if r := h.do(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", strangerTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("stranger reads: status = %d, want 403", r.Code)
	}
	if r := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", strangerTok, map[string]any{"body": "x"}); r.Code != http.StatusForbidden {
		t.Fatalf("stranger reply: status = %d, want 403", r.Code)
	}
}
