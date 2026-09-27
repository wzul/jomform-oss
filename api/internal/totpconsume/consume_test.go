package totpconsume

// Issue #528 / the mirrored-rule defect — the BEHAVIOURAL half of the guard.
//
// only_one_copy_test.go proves there is exactly ONE copy of the rule. This file
// proves that the one copy still does its job, so a mutation that neuters it
// (deletes the ConsumeTOTPStep call, or turns the fail-closed branch into an
// accept) goes RED here WITHOUT the mutator touching any other package — which is
// the acceptance criterion on the card.
//
// It covers the three properties a future edit is most likely to break:
//
//  1. a valid current code is accepted exactly once, and the replay is refused;
//  2. a database error is fail-CLOSED (rejected, never accepted), because a blip
//     must not reopen replay;
//  3. a false return never means "fall back to something else" — every rejection
//     shape (nil user, 2FA off, empty secret, wrong code, typed-nil store) is
//     false.
//
// (2) and (3) run with no database at all: the fail-closed branch is reachable
// only through a store that ERRORS, which a real Postgres will not do on demand.
// A stub is not a weaker proof here — it is the only way to exercise the branch
// at all. (1) needs the real store, because atomic single-use is a property of
// the UPDATE's WHERE clause.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// testPeriod mirrors auth's 30s TOTP period.
const testPeriod = 30

// codeAtStep generates the code for secret at an absolute time-step.
func codeAtStep(t *testing.T, secret string, step int64) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, time.Unix(step*testPeriod, 0), totp.ValidateOpts{
		Period:    testPeriod,
		Skew:      0,
		Digits:    6,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// currentCode is the code an authenticator app would show right now.
func currentCode(t *testing.T, secret string) string {
	t.Helper()
	return codeAtStep(t, secret, time.Now().Unix()/testPeriod)
}

// requireDB skips the DB-backed case when the integration database is absent —
// the same rule the rest of the suite follows, so a unit-only run stays hermetic
// (but note the structural guard in only_one_copy_test.go still runs there).
func requireDB(t *testing.T) {
	t.Helper()
	if os.Getenv(testdb.DSNEnv) == "" {
		t.Skip(testdb.DSNEnv + " not set; skipping the DB-backed single-use case")
	}
}

// newUserWithTOTP creates a real user row with 2FA armed and returns the store,
// the user and its secret.
func newUserWithTOTP(t *testing.T, tag string) (*dbstore.UserStore, *dbstore.User, string) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)
	users := dbstore.NewUserStore(pool)

	email := "totpconsume-" + tag + "-" + time.Now().Format("150405.000000000") + "@example.test"
	u, err := users.Create(ctx, email, "x")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	secret, _, err := auth.GenerateTOTPSecret(email)
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if err := users.EnableTOTP(ctx, u.ID, secret); err != nil {
		t.Fatalf("enable TOTP: %v", err)
	}
	reloaded, err := users.ByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return users, reloaded, secret
}

// TestValidCodeIsAcceptedOnceAndReplayRefused is the property the whole rule
// exists for: a code works exactly once, and the second use is refused.
//
// BITE: remove the ConsumeTOTPStep call from ConsumeCode (accept on validation
// alone) and the replay assertion fails. Replace it with an unconditional `return
// true` and both assertions fail.
func TestValidCodeIsAcceptedOnceAndReplayRefused(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	users, u, secret := newUserWithTOTP(t, "single")
	code := currentCode(t, secret)

	if !ConsumeCode(ctx, users, u, code) {
		t.Fatalf("a valid current code was refused — the rule rejects the legitimate case")
	}
	if ConsumeCode(ctx, users, u, code) {
		t.Fatalf("the SAME code was accepted twice — a replayed TOTP code can turn 2FA off twice")
	}
	// And the failure really is the step being consumed, not some unrelated
	// rejection: a LATER step still works (normal use is not broken).
	later := codeAtStep(t, secret, time.Now().Unix()/testPeriod+1)
	if !ConsumeCode(ctx, users, u, later) {
		t.Fatalf("a code from the NEXT time-step was refused — the guard blocks normal use")
	}
}

// TestConsumedStepIsTheRowStateNotJustAnInProcessGuard confirms the refusal above
// is durable state: a fresh *dbstore.User loaded through the normal select (a
// different API instance, a restart) still sees the code as used.
func TestConsumedStepIsTheRowStateNotJustAnInProcessGuard(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	users, u, secret := newUserWithTOTP(t, "durable")
	code := currentCode(t, secret)

	if !ConsumeCode(ctx, users, u, code) {
		t.Fatalf("a valid current code was refused")
	}
	reloaded, err := users.ByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.TOTPLastStep == 0 {
		t.Fatalf("totp_last_step is still 0 after a consume — the step was not persisted")
	}
	if ConsumeCode(ctx, users, reloaded, code) {
		t.Fatalf("a FRESHLY LOADED user accepted the already-consumed code — the guard is in-process only")
	}
}

// TestRejectionsAreAlwaysFalse pins the "a false return is never a reason to fall
// back" contract from the shared rule's doc comment. On the 2FA-disable path a
// caller that read a rejection as "try the password instead" is the exact defect
// #528 exists to prevent, so every shape must be false.
//
// No database: none of these shapes may reach the store at all.
func TestRejectionsAreAlwaysFalse(t *testing.T) {
	ctx := context.Background()
	var nilStore *dbstore.UserStore

	users, _, secret := newUserWithTOTPNoDB(t, "shape") // real store, but never reached
	valid := currentCode(t, secret)
	// A code from a step far outside the ±1 drift window: a real authenticator
	// could not produce it, so it can never validate (unlike a guessed "000000",
	// which has a one-in-a-million chance of matching a given secret/window).
	wrong := codeAtStep(t, secret, time.Now().Unix()/testPeriod+100)

	cases := []struct {
		name  string
		users UserStore
		user  *dbstore.User
		code  string
	}{
		{"nil store", nil, &dbstore.User{ID: "u", TOTPEnabled: true, TOTPSecret: secret}, valid},
		{"typed-nil store", nilStore, &dbstore.User{ID: "u", TOTPEnabled: true, TOTPSecret: secret}, valid},
		{"nil user", users, nil, valid},
		{"2FA not enabled", users, &dbstore.User{ID: "u", TOTPSecret: secret}, valid},
		{"empty code", users, &dbstore.User{ID: "u", TOTPEnabled: true, TOTPSecret: secret}, ""},
		{"wrong code", users, &dbstore.User{ID: "u", TOTPEnabled: true, TOTPSecret: secret}, wrong},
	}
	for _, tc := range cases {
		if ConsumeCode(ctx, tc.users, tc.user, tc.code) {
			t.Errorf("%s: ConsumeCode returned TRUE — every rejection shape must be false", tc.name)
		}
	}
}

// newUserWithTOTPNoDB returns a store and a secret WITHOUT touching Postgres, so
// the rejection shapes above can be asserted in a DB-less run. The store is real
// but is never reached: every case in TestRejectionsAreAlwaysFalse is rejected
// before the first store call.
func newUserWithTOTPNoDB(t *testing.T, tag string) (*dbstore.UserStore, *dbstore.User, string) {
	t.Helper()
	email := "totpconsume-nodb-" + tag + "@example.test"
	secret, _, err := auth.GenerateTOTPSecret(email)
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	// A store with a nil pool: constructing it is safe, and reaching it would
	// panic — which is exactly what the rejection-shape assertions detect.
	return dbstore.NewUserStore(nil), &dbstore.User{ID: "nodb-" + tag, TOTPEnabled: true, TOTPSecret: secret}, secret
}

// failingStore fails TOTPSecret and ConsumeTOTPStep on demand — the only way to
// reach the fail-closed branch.
type failingStore struct {
	secret      string
	secretErr   error
	consumeErr  error
	consumed    bool
	consumeCall int
}

func (f *failingStore) TOTPSecret(ctx context.Context, userID string) (string, error) {
	if f.secretErr != nil {
		return "", f.secretErr
	}
	return f.secret, nil
}

func (f *failingStore) ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	f.consumeCall++
	if f.consumeErr != nil {
		return false, f.consumeErr
	}
	return f.consumed, nil
}

// TestDatabaseErrorIsFailClosed is the property that a blip must not reopen
// replay: a store error has to be a REJECTION, never an accepted code.
//
// BITE: change the `err != nil` branch in ConsumeCode to `return true` (the
// fail-open regression) and this test fails.
func TestDatabaseErrorIsFailClosed(t *testing.T) {
	ctx := context.Background()
	secret, _, err := auth.GenerateTOTPSecret("failclosed@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	code := currentCode(t, secret)

	// The secret comes from the user row, so the store's ConsumeTOTPStep is the
	// call that must fail here.
	st := &failingStore{consumeErr: errors.New("connection reset by peer")}
	u := &dbstore.User{ID: "failclosed", TOTPEnabled: true, TOTPSecret: secret}

	if ConsumeCode(ctx, st, u, code) {
		t.Fatalf("a DATABASE ERROR returned true — the rule failed OPEN; a blip reopens TOTP replay")
	}
	if st.consumeCall == 0 {
		t.Fatalf("ConsumeTOTPStep was never called — this test is not exercising the error branch")
	}

	// A store that reports the step as already consumed is a REPLAY, and must
	// also be false (a store returning err==nil, consumed==false).
	st2 := &failingStore{secret: secret, consumed: false}
	if ConsumeCode(ctx, st2, u, code) {
		t.Fatalf("a store reporting consumed=false returned true — a replayed code was accepted")
	}
}

// TestSecretFallbackUsesTheStoreAccessor covers the one branch that fetches a
// secret the row did not carry: a user loaded by a query that omits totp_secret
// must still be able to prove a code, and the secret must come from the STORE
// (never from the caller).
//
// BITE: make the fallback skip the store (or use the empty struct field) and the
// valid code is refused.
func TestSecretFallbackUsesTheStoreAccessor(t *testing.T) {
	ctx := context.Background()
	secret, _, err := auth.GenerateTOTPSecret("fallback@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	code := currentCode(t, secret)

	st := &failingStore{secret: secret, consumed: true}
	// The row carries NO secret — the only source is the accessor.
	u := &dbstore.User{ID: "fallback", TOTPEnabled: true, TOTPSecret: ""}

	if !ConsumeCode(ctx, st, u, code) {
		t.Fatalf("a valid code was refused when the row carried no secret — the accessor fallback is broken")
	}
	if st.consumeCall == 0 {
		t.Fatalf("the step was never consumed — this test did not reach the consume path")
	}
}
