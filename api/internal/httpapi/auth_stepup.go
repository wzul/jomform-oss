// Magic-link session hardening (issue #458).
//
// Option A (audit): a magic-link login marks its session magic=true (see
// auth/session.go v4 value + Sessions.Magic). Option C (step-up): on the three
// sensitive account actions — change password, disable 2FA, add passkey — a
// magic session belonging to a 2FA-enabled user must additionally prove a
// fresh TOTP code in the same request. Everyone else is untouched:
//   - non-2FA users keep the existing password confirmation (no new friction);
//   - 2FA users on a normal (password/social/passkey) session are not prompted
//     again — the login itself already proved a factor;
//   - a magic session for a non-2FA user is also unchanged.
//
// The rationale (issue #458): a magic-link session was started by clicking a
// link in an inbox, so it never proved a second factor. A stolen inbox/mailbox
// should not be able to silently rotate the account password, strip 2FA, or
// plant a passkey. A TOTP code the user already generates daily is near-zero
// friction for 2FA users, and it avoids the #303 edge case (a magic-link signup
// has a random-UUID password, so a password confirm is impossible for them).
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// stepUpNextStep is the machine-readable marker the frontend keys on to open a
// TOTP prompt and retry the request with totp_code.
const stepUpNextStep = "totp_stepup_required"

// sessionTokenFrom returns the raw session cookie value ("" when absent).
func sessionTokenFrom(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// magicStepUpNeeded reports whether the issue #458 step-up applies: the session
// was started by a magic link AND the user has 2FA enabled. Any legacy session
// value (no magic flag) or a non-2FA user reports false — the safe-by-default
// "no extra prompt" case.
func magicStepUpNeeded(r *http.Request, sessions *auth.Sessions, user *dbstore.User) bool {
	if sessions == nil || user == nil || !user.TOTPEnabled {
		return false
	}
	return sessions.Magic(r.Context(), sessionTokenFrom(r))
}

// stepUpGranted reports whether this session already holds a live step-up
// grant (a fresh second factor proved moments ago in the same flow, e.g.
// passkey register /start → /finish). It lets a handler ask "is the #458
// condition satisfied?" without going through the gate, which would consume a
// code or write a challenge of its own.
func (a *AuthHandlers) stepUpGranted(r *http.Request, user *dbstore.User) bool {
	if a.Sessions == nil || user == nil {
		return false
	}
	return a.Sessions.StepUp().Granted(r.Context(), sessionTokenFrom(r), user.ID)
}

// stepUpDecision is the outcome of the magic-session step-up gate.
type stepUpDecision int

const (
	// stepUpNotRequired: the action needs no step-up (the common case).
	stepUpNotRequired stepUpDecision = iota
	// stepUpSatisfied: a fresh TOTP code (or a live grant) was accepted.
	stepUpSatisfied
	// stepUpMissing: a magic + 2FA session supplied no usable TOTP code.
	stepUpMissing
)

// checkMagicStepUp evaluates the gate without writing a response, so callers
// can audit the decision before replying. code is the TOTP code supplied in the
// request ("" when the client did not send one).
func checkMagicStepUp(r *http.Request, sessions *auth.Sessions, users *dbstore.UserStore, user *dbstore.User, code string) stepUpDecision {
	if !magicStepUpNeeded(r, sessions, user) {
		return stepUpNotRequired
	}
	token := sessionTokenFrom(r)
	// A live grant (issued moments ago by an earlier leg of the same flow, e.g.
	// passkey register /start → /finish) satisfies the gate without asking
	// twice.
	if sessions.StepUp().Granted(r.Context(), token, user.ID) {
		return stepUpSatisfied
	}
	// Issue #528: the CODE must also be single-use, not just the grant. A code
	// that verifies is consumed here, so a replayed code (a leaked screenshot,
	// an over-shoulder read, a proxy log) cannot mint a second grant inside its
	// ~90s window.
	if code == "" || !checkAndConsumeTOTP(r.Context(), users, user, code) {
		return stepUpMissing
	}
	// Grant for the remainder of the flow. Single-use semantics are handled by
	// the short TTL + explicit Consume after a completed sensitive action.
	_ = sessions.StepUp().Grant(r.Context(), token, user.ID)
	return stepUpSatisfied
}

// writeStepUpRequired emits the uniform step-up challenge: 401 with a
// next_step marker so the client can prompt for a TOTP code and retry.
func writeStepUpRequired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":     "pengesahan 2FA diperlukan untuk tindakan ini",
		"next_step": stepUpNextStep,
	})
}

// gateMagicStepUp runs the gate and writes the challenge response when it
// fails. Returns true when the caller may proceed.
func (a *AuthHandlers) gateMagicStepUp(w http.ResponseWriter, r *http.Request, user *dbstore.User, code string) bool {
	switch checkMagicStepUp(r, a.Sessions, a.Users, user, code) {
	case stepUpNotRequired:
		return true
	case stepUpSatisfied:
		return true
	default:
		// Audit the step-up decision (issue #458) so a blocked sensitive action
		// on a magic session is observable.
		a.recordAuthEvent(r, a.firstAccountID(r, user.ID), user.ID, "auth.stepup_required", map[string]any{
			"magic": true, "totp_enabled": user.TOTPEnabled,
		})
		writeStepUpRequired(w)
		return false
	}
}

// gateMagicStepUpREST is gateMagicStepUp for the RESTHandlers surface (passkey
// registration). It audits through the RESTHandlers event helper.
func (h *RESTHandlers) gateMagicStepUpREST(w http.ResponseWriter, r *http.Request, actx *AccountCtx, user *dbstore.User, code string) bool {
	switch checkMagicStepUp(r, h.Sessions, h.Users, user, code) {
	case stepUpNotRequired, stepUpSatisfied:
		return true
	default:
		if actx != nil && actx.Account != nil {
			h.recordEvent(r, actx, user.ID, "auth.stepup_required", map[string]any{
				"magic": true, "totp_enabled": user.TOTPEnabled,
			})
		}
		writeStepUpRequired(w)
		return false
	}
}
