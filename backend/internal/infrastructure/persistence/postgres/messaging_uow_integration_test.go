package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/message"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S132 — Postgres integration tests for the messaging SendMessage UoW. The
// application-layer / memory tests (see backend/internal/application/message/
// service_test.go) already prove that messageapp.Service.SendMessage routes
// the new message row + the MessageSent event through the UoW (see
// service.go `persist`); this file closes the gap by exercising the actual
// pgx.Tx-bound MessageRepository so the messages INSERT, the conversations
// UPDATE, AND the outbox INSERT really do commit (or roll back) together
// against a live Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / seedHostAndProperty / newTestBooking
//     helpers from integration_test.go (which truncates users + properties,
//     and the FK cascade chain (users/properties → conversations →
//     messages) drops any stale messaging rows along with them — so this
//     file does not need to truncate conversations/messages itself)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound MessageRepository the same way messageapp.persist does
//
// Event-count rationale: investigation of
// backend/internal/application/message/service.go (persist at L206-L228)
// shows the SendMessage UoW emits EXACTLY ONE event — MessageSent — and
// performs exactly ONE messages INSERT (tx.Messages.AddMessage) plus ONE
// conversations UPDATE (tx.Messages.UpdateConversation). So the atomicity
// contract this file protects is: one messages INSERT + one conversations
// UPDATE + one MessageSent outbox row, committed or rolled back together.

// newTestConversation builds an unsaved conversation aggregate for the
// host/guest/property triple. Callers persist it via the pool-bound
// MessageRepository so the row exists BEFORE the test UoW runs (the UoW
// we're exercising performs an UPDATE on it). Cascade cleanup is handled
// by withTestDB truncating users + properties (FK → conversations →
// messages), so this file does not need to truncate the messaging tables
// itself.
func newTestConversation(t *testing.T, hostID, guestID, propertyID uuid.UUID) *message.Conversation {
	t.Helper()
	conv, err := message.NewConversation(propertyID, hostID, guestID)
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	return conv
}

// TestMessagingUoW_SendWritesAtomicallyWithOutbox_Integration — the happy
// path. We seed a conversation row (the realistic pre-send state), then run
// the UoW exactly as messageapp.persist does: PostMessage on the aggregate,
// tx.Messages.AddMessage, tx.Messages.UpdateConversation, append a single
// MessageSent record to tx.Outbox. After commit the messages row must be
// present, the conversation's LastMessageAt must reflect the new message,
// AND the outbox must hold exactly one matching pending event.
func TestMessagingUoW_SendWritesAtomicallyWithOutbox_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	// Seed the FK chain: host + property + guest, then a conversation row
	// between host and guest about the property. We persist the conversation
	// OUTSIDE the test UoW so the UoW we're exercising performs an UPDATE
	// (messageapp.persist's actual shape) rather than an INSERT — the bug
	// class S132 protects against is "the messages INSERT landed but the
	// event didn't", which can't be reproduced if the conversation only
	// exists inside the same tx.
	b, propID := newTestBooking(t, pool)
	// newTestBooking seeds a guest user too; we reuse its GuestID + the
	// property's host id (looked up via the bookings/properties chain).
	// The host id is the property's host — query it back so we don't have
	// to reach into property internals.
	var hostID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT host_id FROM properties WHERE id = $1`, propID).Scan(&hostID); err != nil {
		t.Fatalf("lookup host id: %v", err)
	}
	guestID := b.GuestID

	conv := newTestConversation(t, hostID, guestID, propID)
	if err := pg.NewMessageRepository(pool).CreateConversation(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	// Capture baselines BEFORE the UoW so we assert only NEW rows are
	// counted. withTestDB truncates outbox/users/properties (cascading to
	// conversations + messages), so these baselines are 0 in practice —
	// pinning them explicitly keeps the test resilient against any future
	// helper that seeds rows during setup.
	var outboxBefore, messagesBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID,
	).Scan(&messagesBefore); err != nil {
		t.Fatalf("baseline messages count: %v", err)
	}

	// Run the SendMessage UoW exactly the way messageapp.persist does:
	// PostMessage on the aggregate, AddMessage + UpdateConversation, then
	// append one MessageSent record to the outbox. The senderID is the
	// guest (the typical first-message direction) and the recipient is the
	// host (mirrors messageapp.persist's recipient derivation).
	msg, err := conv.PostMessage(guestID, "hello, is the place still available?")
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Messages.AddMessage(ctx, msg); err != nil {
			return err
		}
		if err := tx.Messages.UpdateConversation(ctx, conv); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.MessageSent{
			ConversationID: conv.ID,
			SenderID:       guestID,
			RecipientID:    hostID,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: exactly ONE new messages row landed for this
	// conversation. We round-trip through the repo so any silent
	// column-shape regression in AddMessage also surfaces.
	var messagesAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID,
	).Scan(&messagesAfter); err != nil {
		t.Fatalf("messages count: %v", err)
	}
	if messagesAfter-messagesBefore != 1 {
		t.Fatalf("messages rows for conv %s grew by %d, want 1 (the UoW commit must have persisted the AddMessage write)",
			conv.ID, messagesAfter-messagesBefore)
	}

	// Belt-and-braces: the specific message id we built reloads with the
	// body intact. This guards against a case where SOME message row
	// landed (passing assertion 1) but not the one we wrote.
	var body string
	if err := pool.QueryRow(ctx,
		`SELECT body FROM messages WHERE id = $1`, msg.ID,
	).Scan(&body); err != nil {
		t.Fatalf("reload message body: %v", err)
	}
	if body != "hello, is the place still available?" {
		t.Errorf("reloaded message body = %q, want the body we posted", body)
	}

	// Assertion 2: exactly ONE new outbox row tied to this conversation,
	// in pending state. The persist path emits exactly one event
	// (MessageSent) — see service.go L218-L226. If a future change adds a
	// second event (e.g. ConversationBumped) this assertion will fail
	// loudly so the test stays honest about what the production path
	// actually emits.
	var pendingForConv int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'ConversationID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.MessageSent{}).EventName(), conv.ID.String(),
	).Scan(&pendingForConv); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pendingForConv != 1 {
		t.Fatalf("pending MessageSent rows for conv %s = %d, want 1 (the UoW commit must have appended exactly one MessageSent record)",
			conv.ID, pendingForConv)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. Guards against a
	// future change that adds an extra event to the send path but happens
	// to use a different event_name or payload key (so assertion 2 would
	// still see 1). Together the two assertions pin the contract: one and
	// only one new outbox row per send.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after send, want 1 (the SendMessage UoW currently emits exactly one event — update this test if messageapp.persist starts emitting more)",
			outboxAfter-outboxBefore)
	}

	// Assertion 4 (belt-and-braces): the conversation's last_message_at
	// has advanced — proves tx.Messages.UpdateConversation committed too.
	// Without this, a regression that drops the UpdateConversation call
	// would still pass assertions 1-3 silently.
	reloaded, err := pg.NewMessageRepository(pool).FindConversationByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("reload conversation: %v", err)
	}
	if !reloaded.LastMessageAt.Equal(conv.LastMessageAt) {
		t.Errorf("conversation last_message_at after commit = %v, want %v (UpdateConversation must have written it back)",
			reloaded.LastMessageAt, conv.LastMessageAt)
	}
}

// TestMessagingUoW_RollbackOnError_Integration — same setup, but the UoW
// closure returns an error after both writes. After rollback the messages
// table must hold ZERO rows for this conversation (since baseline is 0)
// AND no NEW outbox rows may carry this conversation's id. This is the
// contract the UoW exists to enforce — without it, a partial commit could
// ship a MessageSent event for a message that was never persisted (the
// notification fires for a message no participant can read), or persist a
// message while silently dropping the realtime fan-out event.
func TestMessagingUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	b, propID := newTestBooking(t, pool)
	var hostID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT host_id FROM properties WHERE id = $1`, propID).Scan(&hostID); err != nil {
		t.Fatalf("lookup host id: %v", err)
	}
	guestID := b.GuestID

	conv := newTestConversation(t, hostID, guestID, propID)
	if err := pg.NewMessageRepository(pool).CreateConversation(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	// Baselines: how many rows EXIST before the UoW runs. We'll assert
	// these counts are unchanged after the forced rollback.
	var outboxBefore, messagesBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID,
	).Scan(&messagesBefore); err != nil {
		t.Fatalf("baseline messages count: %v", err)
	}

	// Capture the pre-call LastMessageAt — after rollback the
	// conversation's last_message_at must still read this value (the
	// UpdateConversation inside the rolled-back tx must not have leaked).
	preCallLastMessageAt := conv.LastMessageAt

	// Build the message OUTSIDE the closure so PostMessage's side-effect
	// on conv.LastMessageAt happens once; we still rely on the DB to
	// reject the UPDATE that goes with it via the rollback.
	msg, err := conv.PostMessage(guestID, "this send will be rolled back")
	if err != nil {
		t.Fatalf("post message: %v", err)
	}

	wantErr := errors.New("simulated mid-tx failure after message + outbox write")
	uow := pg.NewUnitOfWork(pool, nil)
	err = uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Messages.AddMessage(ctx, msg); err != nil {
			return err
		}
		if err := tx.Messages.UpdateConversation(ctx, conv); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.MessageSent{
			ConversationID: conv.ID,
			SenderID:       guestID,
			RecipientID:    hostID,
		})
		if err != nil {
			return err
		}
		if err := tx.Outbox.Append(ctx, rec); err != nil {
			return err
		}
		return wantErr // force rollback AFTER all writes
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1: messages count for this conversation is unchanged
	// from the baseline. The AddMessage INSERT inside the failed UoW
	// must have rolled back with the transaction.
	var messagesAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, conv.ID,
	).Scan(&messagesAfter); err != nil {
		t.Fatalf("messages count: %v", err)
	}
	if messagesAfter != messagesBefore {
		t.Fatalf("messages rows for conv %s after rollback = %d, want %d (AddMessage must have rolled back)",
			conv.ID, messagesAfter, messagesBefore)
	}

	// Belt-and-braces: the specific message id is not findable. Catches
	// a hypothetical bug where some other message landed but ours did
	// not (or vice versa) — the count check alone could mask that.
	var ourMsgRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE id = $1`, msg.ID,
	).Scan(&ourMsgRows); err != nil {
		t.Fatalf("count by message id: %v", err)
	}
	if ourMsgRows != 0 {
		t.Fatalf("messages row for our msg id %s after rollback = %d, want 0 (the rolled-back AddMessage must leave no trace)",
			msg.ID, ourMsgRows)
	}

	// Assertion 2: NO new outbox row carries this conversation id. The
	// Append inside the rolled-back tx must not have left a trace,
	// otherwise the relay would later ship a MessageSent event for a
	// message that does not exist in the DB (the exact split-brain the
	// UoW exists to prevent).
	var leakedForConv int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'ConversationID' = $2`,
		(event.MessageSent{}).EventName(), conv.ID.String(),
	).Scan(&leakedForConv); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if leakedForConv != 0 {
		t.Fatalf("MessageSent rows for conv %s after rollback = %d, want 0 (Append must have rolled back)",
			conv.ID, leakedForConv)
	}

	// Assertion 3: the TOTAL outbox row count is unchanged. Belt-and-
	// braces against a future change to event payloads (so payload
	// filtering in assertion 2 misses) and a future helper that seeds
	// outbox state during setup.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter != outboxBefore {
		t.Fatalf("outbox count after rollback = %d, want %d (no NEW rows must have leaked through the rolled-back tx)",
			outboxAfter, outboxBefore)
	}

	// Assertion 4: the conversation row's last_message_at on disk still
	// matches the PRE-CALL value. The in-tx UpdateConversation must
	// have rolled back; reading the row back through the repo should
	// surface the timestamp from CreateConversation, NOT the one we
	// pushed via PostMessage in the closure.
	reloaded, err := pg.NewMessageRepository(pool).FindConversationByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("reload conversation: %v", err)
	}
	if !reloaded.LastMessageAt.Equal(preCallLastMessageAt) {
		t.Fatalf("conversation last_message_at after rollback = %v, want %v (the UpdateConversation inside the failed UoW must have rolled back)",
			reloaded.LastMessageAt, preCallLastMessageAt)
	}
}

// Compile-time guards: the test file references the message domain package
// and the MessageSent event constructor; keep explicit references so a
// future "unused import" reflow doesn't accidentally strip them.
var _ = message.NewConversation
var _ = (event.MessageSent{}).EventName
var _ uuid.UUID
