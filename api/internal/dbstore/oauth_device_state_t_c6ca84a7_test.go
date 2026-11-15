package dbstore

// The arms of the device state machine that a hand-written suite forgets, against
// the real store (card t_c6ca84a7).
//
// WHY THESE EXIST. statemachine/devicecode_test.go proves the TABLE. What it
// cannot prove is that the SQL store asks it: the store used to carry its own
// copy of the rules in three different shapes (a switch in ApproveDeviceCode, a
// WHERE clause in DenyDeviceCode, an if-ladder in PollDeviceCode), and those
// copies disagreed with the consent card on the arm this file pins —
// approved-but-never-exchanged, past its deadline.
//
// Every assertion below is about a ROW, not a return value: a refusal that wrote
// denied_at would turn "expired" into "denied" for every later reader, and a
// refusal that consumed a code would make the state machine's answer a lie about
// the row the next reader loads.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/statemachine"
)

// expireRow moves a seeded authorisation's deadline into the past, which is the
// only way to stand in for ten minutes without sleeping.
func expireRow(t *testing.T, ctx context.Context, p *pgxpool.Pool, userCodeHash string) {
	t.Helper()
	if _, err := p.Exec(ctx,
		`UPDATE oauth_device_codes SET expires_at = now() - interval '1 minute'
		 WHERE user_code_hash = $1`, userCodeHash); err != nil {
		t.Fatalf("expire the row: %v", err)
	}
}

// TestApprovedButNotExchangedIsExpiredEverywhere is the arm the consent card got
// wrong: a person approved, the harness never came back, the ten minutes ran out.
// The token endpoint has always answered expired_token here; the card said
// "approved" and drew Approve/Deny buttons.
func TestApprovedButNotExchangedIsExpiredEverywhere(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	deviceHash, userCodeHash, _ := seedDevice(t, ctx, store, clientID, userID, accountID, []string{"read"}, time.Hour)
	if _, err := store.ApproveDeviceCode(ctx, userCodeHash, userID, accountID, []string{"read"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	expireRow(t, ctx, p, userCodeHash)

	// THE DERIVATION every reader uses.
	row, err := store.DeviceCodeByUserCode(ctx, userCodeHash)
	if err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if got := row.State(); got != statemachine.DeviceExpired {
		t.Fatalf("derived state = %q, want %q (an approval cannot outrun the deadline)", got, statemachine.DeviceExpired)
	}
	if st := row.Status(time.Now().UTC()); st.Usable || st.RemainingSeconds != 0 || len(st.AvailableEvents) != 0 {
		t.Fatalf("status = {usable=%v remaining=%d events=%v}, want unusable / 0 / none", st.Usable, st.RemainingSeconds, st.AvailableEvents)
	}

	// THE TOKEN ENDPOINT still answers expired_token (frozen behaviour).
	if _, err := store.PollDeviceCode(ctx, deviceHash, clientID); !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("poll = %v, want ErrDeviceExpired", err)
	}
	// AND THE APPROVAL PATH keeps the answer it has always given for this arm:
	// "invalid", because the dashboard checks "already approved?" before it looks
	// at the clock. The STATE is expired (above and at the token endpoint); the
	// machine code of a re-approval is frozen by this card, so the dashboard still
	// answers 404 and not 410.
	if _, err := store.ApproveDeviceCode(ctx, userCodeHash, userID, accountID, []string{"read"}); !errors.Is(err, ErrDeviceCodeInvalid) {
		t.Fatalf("re-approve of an approved-but-expired row = %v, want ErrDeviceCodeInvalid (frozen)", err)
	}
	// The failed approval must not have moved the row.
	if after, err := store.DeviceCodeByUserCode(ctx, userCodeHash); err != nil {
		t.Fatalf("re-read the row: %v", err)
	} else if after.ConsumedAt != nil || after.DeniedAt != nil {
		t.Fatalf("a refused approval wrote consumed_at/denied_at on the row")
	}
}

// TestDenyRefusalsLeaveTheRowAlone: the deny path used to be one conditional
// UPDATE carrying its own copy of "may this row be denied". It now locks the row
// and asks the same table as approve and poll — so the assertion that matters is
// that the ROW is untouched when the table refuses.
func TestDenyRefusalsLeaveTheRowAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	// An approved row cannot be denied: the person already consented.
	_, approvedCode, _ := seedDevice(t, ctx, store, clientID, userID, accountID, []string{"read"}, time.Hour)
	if _, err := store.ApproveDeviceCode(ctx, approvedCode, userID, accountID, []string{"read"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := store.DenyDeviceCode(ctx, approvedCode, userID); !errors.Is(err, ErrDeviceCodeInvalid) {
		t.Fatalf("deny an approved row = %v, want ErrDeviceCodeInvalid (frozen)", err)
	}
	if row, err := store.DeviceCodeByUserCode(ctx, approvedCode); err != nil {
		t.Fatalf("read: %v", err)
	} else if row.DeniedAt != nil {
		t.Fatalf("a refused deny stamped denied_at on an approved row")
	}

	// An expired row cannot be denied either, and the refusal must not turn the
	// state "expired" into "denied" for every later reader.
	_, expiredCode, _ := seedDevice(t, ctx, store, clientID, userID, accountID, []string{"read"}, time.Hour)
	expireRow(t, ctx, p, expiredCode)
	if err := store.DenyDeviceCode(ctx, expiredCode, userID); err == nil {
		t.Fatalf("deny an expired row = nil, want a refusal")
	}
	if row, err := store.DeviceCodeByUserCode(ctx, expiredCode); err != nil {
		t.Fatalf("read: %v", err)
	} else if row.DeniedAt != nil {
		t.Fatalf("a refused deny stamped denied_at on an expired row")
	} else if row.State() != statemachine.DeviceExpired {
		t.Fatalf("state after a refused deny = %q, want expired", row.State())
	}

	// A pending row can still be denied — the rewrite must not have removed the
	// only path that works.
	_, pendingCode, _ := seedDevice(t, ctx, store, clientID, userID, accountID, []string{"read"}, time.Hour)
	if err := store.DenyDeviceCode(ctx, pendingCode, userID); err != nil {
		t.Fatalf("deny a pending row = %v, want nil", err)
	}
	if row, err := store.DeviceCodeByUserCode(ctx, pendingCode); err != nil {
		t.Fatalf("read: %v", err)
	} else if row.State() != statemachine.DeviceDenied {
		t.Fatalf("state after deny = %q, want denied", row.State())
	}
}

// TestPollReturnsTheRowWithItsRefusal: the token endpoint reports the derived
// state and the seconds left instead of a bare 400, which is only possible if the
// store hands the row back WITH a refusal about the code. invalid_grant (unknown,
// spent, wrong client_id) deliberately does not.
func TestPollReturnsTheRowWithItsRefusal(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	deviceHash, userCodeHash, _ := seedDevice(t, ctx, store, clientID, userID, accountID, []string{"read"}, time.Hour)

	// pending: the row comes back, so the endpoint can say "pending, N seconds".
	d, err := store.PollDeviceCode(ctx, deviceHash, clientID)
	if !errors.Is(err, ErrDevicePending) {
		t.Fatalf("first poll = %v, want ErrDevicePending", err)
	}
	if d == nil {
		t.Fatalf("a pending poll returned no row; the token endpoint cannot report the state")
	}
	if got := d.StateAt(time.Now().UTC()); got != statemachine.DevicePending {
		t.Fatalf("pending poll row state = %q, want pending", got)
	}

	// expired: same, and the remaining seconds are zero.
	expireRow(t, ctx, p, userCodeHash)
	d, err = store.PollDeviceCode(ctx, deviceHash, clientID)
	if !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("poll = %v, want ErrDeviceExpired", err)
	}
	if d == nil || d.State() != statemachine.DeviceExpired {
		t.Fatalf("expired poll returned %v, want the expired row", d)
	}

	// unknown: no row, so nothing is described.
	if d, err := store.PollDeviceCode(ctx, "no-such-device-hash", clientID); err == nil || d != nil {
		t.Fatalf("unknown device_code returned (%v, %v), want (nil, error)", d, err)
	}
}
