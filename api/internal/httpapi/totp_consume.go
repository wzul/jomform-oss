package httpapi

// Issue #528: TOTP single-use enforcement, called from ONE rule.
//
// auth.ValidateTOTPSecret is stateless — it accepts any code within the current
// 30s step ±1, so the same code verified repeatedly for its whole ~90s window.
// Recording the consumed step (users.totp_last_step) makes each code single-use.
//
// All three REST call sites (login 2FA, 2FA verify, and the issue #458
// magic-session step-up gate) go through checkAndConsumeTOTP, and the rule it
// applies lives in internal/totpconsume — called, not copied, so the REST and MCP
// surfaces cannot drift apart. A second hand-rolled copy is how one site silently
// loses the check.

import (
	"context"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/totpconsume"
)

// checkAndConsumeTOTP verifies a TOTP code AND atomically consumes its
// time-step, returning true only when the code is valid and was not already
// used. A replayed code (same step, already consumed) returns false.
//
// It is a thin alias for the shared rule (internal/totpconsume.ConsumeCode); the
// rule itself is NOT implemented here. The MCP surface calls the same function.
func checkAndConsumeTOTP(ctx context.Context, users *dbstore.UserStore, user *dbstore.User, code string) bool {
	return totpconsume.ConsumeCode(ctx, users, user, code)
}
