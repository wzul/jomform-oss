package dbstore

// Issue #578: webhook_events.account_id was never populated.
//
// The column has always existed (nullable, FK to accounts), but both claim
// inserts omitted it, so every row on production was NULL and a callback event
// could not be attributed to the workspace it belonged to. That is what blocks
// the dead-letter UI from showing whose callback failed.
//
// Two properties matter and both are covered here:
//   - a known owner is recorded on the claim
//   - a redelivery fills the owner in when the first attempt did not know it,
//     WITHOUT blanking an id already stored
//   - an unresolvable owner stores NULL rather than "" — "" is not a valid uuid
//     and would fail the insert instead of recording the event

import (
	"context"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/testdb"
)

func seedAccount(t *testing.T, ctx context.Context, s *WebhookEventStore) string {
	t.Helper()
	var userID string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('r578-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var id string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'r578-acc-' || md5(random()::text), 'r578-' || md5(random()::text)
		 RETURNING id::text`, userID).Scan(&id); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return id
}

// TestIssue578ClaimRecordsTheOwningAccount is the core regression: the owner
// must reach the row.
func TestIssue578ClaimRecordsTheOwningAccount(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)
	acct := seedAccount(t, ctx, s)

	dedup := "r578-owns-" + time.Now().Format("150405.000000000")
	claimed, err := s.ClaimStandalone(ctx, &WebhookEvent{
		Source: "chip", EventType: "purchase.paid", Payload: []byte(`{}`),
		DedupKey: dedup, AccountID: acct,
	}, 2*time.Minute)
	if err != nil {
		t.Fatalf("ClaimStandalone: %v", err)
	}
	if !claimed {
		t.Fatalf("event was not claimed")
	}

	var got *string
	if err := pool.QueryRow(ctx,
		`SELECT account_id::text FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil {
		t.Fatalf("issue #578: account_id is NULL — the owning workspace was not recorded")
	}
	if *got != acct {
		t.Errorf("account_id = %q, want %q", *got, acct)
	}
}

// TestIssue578UnknownOwnerStoresNull guards the insert itself: an unattributable
// event must still be recorded rather than failing on an invalid uuid.
func TestIssue578UnknownOwnerStoresNull(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	dedup := "r578-unknown-" + time.Now().Format("150405.000000000")
	claimed, err := s.ClaimStandalone(ctx, &WebhookEvent{
		Source: "chip", EventType: "purchase.paid", Payload: []byte(`{}`),
		DedupKey: dedup, AccountID: "",
	}, 2*time.Minute)
	if err != nil {
		t.Fatalf("issue #578: an unattributable event must still be claimed, got: %v", err)
	}
	if !claimed {
		t.Fatalf("event was not claimed")
	}

	var got *string
	if err := pool.QueryRow(ctx,
		`SELECT account_id::text FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != nil {
		t.Errorf("account_id = %q, want NULL for an unattributed event", *got)
	}
}

// TestIssue578RedeliveryFillsInWithoutBlanking covers the ON CONFLICT branch:
// a first attempt that could not resolve the owner must be repairable by a later
// delivery, and a known owner must never be overwritten with an empty one.
func TestIssue578RedeliveryFillsInWithoutBlanking(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)
	acct := seedAccount(t, ctx, s)

	dedup := "r578-redeliver-" + time.Now().Format("150405.000000000")

	// First delivery: owner unknown, and immediately re-claimable (failed) so
	// the redelivery is allowed to take the claim again.
	if _, err := s.ClaimStandalone(ctx, &WebhookEvent{
		Source: "chip", EventType: "purchase.paid", Payload: []byte(`{}`),
		DedupKey: dedup, AccountID: "",
	}, 2*time.Minute); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := s.MarkFailed(ctx, dedup); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	// Redelivery now knows the owner.
	claimed, err := s.ClaimStandalone(ctx, &WebhookEvent{
		Source: "chip", EventType: "purchase.paid", Payload: []byte(`{}`),
		DedupKey: dedup, AccountID: acct,
	}, 2*time.Minute)
	if err != nil {
		t.Fatalf("redelivery claim: %v", err)
	}
	if !claimed {
		t.Fatalf("redelivery was not claimed")
	}

	var got *string
	if err := pool.QueryRow(ctx,
		`SELECT account_id::text FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil || *got != acct {
		t.Errorf("issue #578: account_id = %v after a redelivery that knew the owner, want %q", got, acct)
	}

	// And an empty owner on a later attempt must NOT blank it.
	if err := s.MarkFailed(ctx, dedup); err != nil {
		t.Fatalf("MarkFailed 2: %v", err)
	}
	if _, err := s.ClaimStandalone(ctx, &WebhookEvent{
		Source: "chip", EventType: "purchase.paid", Payload: []byte(`{}`),
		DedupKey: dedup, AccountID: "",
	}, 2*time.Minute); err != nil {
		t.Fatalf("third claim: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT account_id::text FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&got); err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if got == nil || *got != acct {
		t.Errorf("issue #578: account_id = %v — a later attempt with no owner BLANKED a known owner", got)
	}
}

// TestIssue578DeadLetterCarriesTheOwner proves the fix reaches the surface the UI
// will read: the dead-letter list must expose the account.
func TestIssue578DeadLetterCarriesTheOwner(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)
	acct := seedAccount(t, ctx, s)

	dedup := "r578-dead-" + time.Now().Format("150405.000000000")
	if _, err := pool.Exec(ctx,
		`INSERT INTO webhook_events (source, event_type, payload, dedup_key, status, attempts, last_error, account_id)
		 VALUES ('chip','purchase.paid','{}'::jsonb,$1,$2,$3,'why',$4)`,
		dedup, WebhookEventDead, MaxReplayAttempts+1, acct); err != nil {
		t.Fatalf("seed dead: %v", err)
	}

	dead, err := s.ListDead(ctx, 100)
	if err != nil {
		t.Fatalf("ListDead: %v", err)
	}
	var found *DeadLetterEvent
	for _, e := range dead {
		if e.DedupKey == dedup {
			found = e
		}
	}
	if found == nil {
		t.Fatalf("dead event not listed")
	}
	if found.AccountID != acct {
		t.Errorf("issue #578: dead letter account_id = %q, want %q — the UI cannot show whose callback failed",
			found.AccountID, acct)
	}
}
