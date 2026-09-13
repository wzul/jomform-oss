package httpapi

// Issue #528: TOTP single-use enforcement, in ONE place.
//
// auth.ValidateTOTPSecret is stateless — it accepts any code within the current
// 30s step ±1, so the same code verified repeatedly for its whole ~90s window.
// Recording the consumed step (users.totp_last_step) makes each code single-use.
//
// All three call sites (login 2FA, 2FA verify, and the issue #458 magic-session
// step-up gate) go through checkAndConsumeTOTP so the rule cannot drift between
// them — a second hand-rolled copy is how one site silently loses the check.

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// checkAndConsumeTOTP verifies a TOTP code AND atomically consumes its
// time-step, returning true only when the code is valid and was not already
// used. A replayed code (same step, already consumed) returns false.
func checkAndConsumeTOTP(ctx context.Context, users *dbstore.UserStore, user *dbstore.User, code string) bool {
	if user == nil || users == nil || !user.TOTPEnabled {
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
