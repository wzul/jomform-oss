// Package totpconsume is the ONE place JomForm's TOTP single-use rule lives.
//
// Issue #528: auth.ValidateTOTPSecret is stateless — it accepts any code within
// the current 30s step ±1, so the SAME code verified repeatedly for its whole
// ~90s window. Recording the consumed step (users.totp_last_step) makes each
// code single-use, and that is the difference between "a code proves a factor"
// and "a code can be replayed for 90 seconds by whoever saw it".
//
// # WHY THIS PACKAGE EXISTS
//
// The rule needs BOTH halves of the system: auth (to validate a code and learn
// which time-step it matched) and dbstore (to consume that step atomically).
// internal/auth deliberately does not import internal/dbstore and
// internal/dbstore deliberately does not import internal/auth, so neither could
// own it. The first copy therefore landed in internal/httpapi and, when the MCP
// surface needed the same rule (PR 686), a SECOND hand-rolled copy landed in
// internal/mcpsrv — kept in step by nothing but a comment asking the next reader
// to remember to edit both.
//
// A comment is not a guard. On the 2FA-disable path, drift between two copies
// means a code seen once can turn a second factor off twice. So the rule lives
// here once, both surfaces call it, and
// TestOnlyTheSharedRuleConsumesATOTPStep (only_one_copy_test.go) fails the build
// if a third copy — or any new direct consumer — appears.
//
// DEPENDENCY DIRECTION (deliberate)
//
//	totpconsume -> auth (code validation) + dbstore (step consumption)
//	httpapi      -> totpconsume
//	mcpsrv       -> totpconsume
//
// The caller supplies the user row it just loaded; it can never supply the
// secret. That is what stops one surface from validating against something other
// than the account's own stored secret.
//
// WHAT MUST NEVER CHANGE WITHOUT A TEST GOING RED
//
//   - the code is validated from the freshly loaded user's OWN secret
//     (user.TOTPSecret, with the store accessor as the fallback for a row loaded
//     without it), never from an argument;
//   - the matched step is consumed with the predicate inside the UPDATE
//     (dbstore.ConsumeTOTPStep), so two concurrent submissions of the SAME code
//     cannot both succeed — that is what makes a code single-use rather than
//     single-use-per-window;
//   - a database error is fail-CLOSED: a false return, never an accepted code,
//     because a blip must not silently reopen replay.
package totpconsume

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// UserStore is the slice of *dbstore.UserStore the rule needs: read the
// account's secret, and consume a time-step on the account's row. It is an
// interface so the rule can be tested against a real database, against a stub
// that fails, and against a stub that reports a replay — without which the
// fail-closed branch could only be exercised by breaking Postgres.
//
// *dbstore.UserStore satisfies it; no store code changed to make that true.
type UserStore interface {
	TOTPSecret(ctx context.Context, userID string) (string, error)
	ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error)
}

// ConsumeCode verifies code against the user's OWN TOTP secret AND atomically
// consumes the time-step it matched, returning true only when the code is valid
// and had not already been used. A replayed code (same step, already consumed)
// returns false.
//
// It returns false — never true — for: a nil user, a nil store, a user whose 2FA
// is not enabled, an unknown or empty secret, a wrong code, an already-consumed
// step, and a database error.
//
// A false return means REJECTED. It is never a reason to fall back to a password
// check: on the 2FA-disable path that fallback is exactly the defect this rule
// exists to prevent (a replayed code turning the second factor off anyway).
func ConsumeCode(ctx context.Context, users UserStore, user *dbstore.User, code string) bool {
	if user == nil || users == nil || !user.TOTPEnabled {
		return false
	}
	// A typed-nil *dbstore.UserStore stored in the interface is != nil, so the
	// check above would miss it and the first method call would panic. Fail
	// closed on that too: the rule is a security gate, and a panic is a 500
	// rather than the 401 a caller should get.
	if store, ok := users.(*dbstore.UserStore); ok && store == nil {
		return false
	}
	secret := user.TOTPSecret
	if secret == "" {
		// The struct may have been loaded without the secret (e.g. a query that
		// intentionally omits it); fall back to the accessor.
		if s, err := users.TOTPSecret(ctx, user.ID); err == nil {
			secret = s
		}
	}
	step, ok := auth.ValidateTOTPSecretStep(secret, code)
	if !ok {
		return false
	}
	consumed, err := users.ConsumeTOTPStep(ctx, user.ID, step)
	if err != nil {
		// Fail closed: a database error must not become an accepted code, or a
		// blip would silently reopen replay.
		slog.Error("totp_consume_step", "err", err, "user", user.ID)
		return false
	}
	if !consumed {
		slog.Warn("totp_replay_rejected", "user", user.ID, "step", step)
		return false
	}
	return true
}
