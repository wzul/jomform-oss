package dbstore

// The device approval's scope rule at the STORAGE layer (card t_cf45b84c).
//
// The handler validates the scope a person picked before it calls the store, so
// this is the second check — the same reason the old "narrow only" rule lived
// here: ApproveDeviceCode is the last write before a token exists, and a caller
// validating first is not a reason for the store to trust it.
//
// WHAT CHANGED, AND WHY IT NEEDS A TEST. The rule used to be "the approval may
// only NARROW the client's request" (scopeSubset vs. the row's requested scope).
// At the consent card the PERSON decides the scope, so the request is only the
// default the card opens on and widening at approval is the point — the old rule
// would refuse exactly the case Wan asked for ("bagi semua supaya user tak perlu
// grant banyak kali"). The replacement keeps the part that still matters: a
// scope this deployment cannot ISSUE must fail before anything is written, and it
// must fail with its own error rather than as "this code is not valid".
//
// Real Postgres (skipped when JOMFORM_TEST_DATABASE_URL is unset), because both
// properties are about the ROW that ends up in the table.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDeviceApprovalStoresTheChosenScopeNotTheRequest(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	// The harness asked for READ — the request that used to get stuck read-only.
	deviceHash, userCodeHash, _ := seedDevice(t, ctx, store, clientID, userID, accountID,
		[]string{"read"}, time.Hour)

	// The person approves READWRITE. This is the widening the old scopeSubset
	// check refused; the row must now carry what the person chose.
	d, err := store.ApproveDeviceCode(ctx, userCodeHash, userID, accountID, []string{"readwrite"})
	if err != nil {
		t.Fatalf("ApproveDeviceCode widening a read request: %v", err)
	}
	if strings.Join(d.Scope, " ") != "readwrite" {
		t.Fatalf("returned scope = %v, want readwrite", d.Scope)
	}
	var stored []string
	if err := p.QueryRow(ctx,
		`SELECT scope FROM oauth_device_codes WHERE user_code_hash = $1`, userCodeHash).Scan(&stored); err != nil {
		t.Fatalf("read stored scope: %v", err)
	}
	if strings.Join(stored, " ") != "readwrite" {
		t.Fatalf("stored scope = %v, want readwrite — the token minted from this row would be read-only", stored)
	}
	// The token endpoint reads the row, so the row is the credential's scope.
	backdatePoll(t, ctx, p, deviceHash, 10*time.Second)
	polled, err := store.PollDeviceCode(ctx, deviceHash, clientID)
	if err != nil {
		t.Fatalf("poll after approval: %v", err)
	}
	if strings.Join(polled.Scope, " ") != "readwrite" {
		t.Fatalf("polled scope = %v, want readwrite", polled.Scope)
	}

	// The other direction, on a fresh row: the person hands out LESS than asked.
	_, narrowCode, _ := seedDevice(t, ctx, store, clientID, userID, accountID,
		[]string{"readwrite"}, time.Hour)
	if _, err := store.ApproveDeviceCode(ctx, narrowCode, userID, accountID, []string{"read"}); err != nil {
		t.Fatalf("ApproveDeviceCode narrowing a readwrite request: %v", err)
	}
	var narrowed []string
	if err := p.QueryRow(ctx,
		`SELECT scope FROM oauth_device_codes WHERE user_code_hash = $1`, narrowCode).Scan(&narrowed); err != nil {
		t.Fatalf("read narrowed scope: %v", err)
	}
	if strings.Join(narrowed, " ") != "read" {
		t.Fatalf("narrowed scope = %v, want read", narrowed)
	}
}

func TestDeviceApprovalRefusesAScopeThisServerCannotIssue(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	_, userCodeHash, _ := seedDevice(t, ctx, store, clientID, userID, accountID,
		[]string{"read"}, time.Hour)

	// A value outside oauthmeta.ScopesSupported(), and the empty set. Both must
	// fail with the scope error — NOT as "invalid code", which would tell the
	// person to start over for a one-click mistake — and must write NOTHING.
	for _, bad := range [][]string{{"admin"}, {"read", "admin"}, {}, {"READWRITE"}} {
		if _, err := store.ApproveDeviceCode(ctx, userCodeHash, userID, accountID, bad); !errors.Is(err, ErrDeviceScopeUnsupported) {
			t.Fatalf("ApproveDeviceCode(%v) = %v, want ErrDeviceScopeUnsupported", bad, err)
		}
	}
	var approved *time.Time
	var stored []string
	if err := p.QueryRow(ctx,
		`SELECT approved_at, scope FROM oauth_device_codes WHERE user_code_hash = $1`, userCodeHash).
		Scan(&approved, &stored); err != nil {
		t.Fatalf("read row after refusals: %v", err)
	}
	if approved != nil {
		t.Fatal("a refused approval marked the row approved")
	}
	if strings.Join(stored, " ") != "read" {
		t.Fatalf("refused approvals rewrote the requested scope to %v", stored)
	}
}
