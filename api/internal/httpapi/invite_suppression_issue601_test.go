// Invite-path suppression (issue #601).
//
// The workspace invite email was one of four send paths that reached SES with
// no suppression check at all. It now checks before enqueueing/sending, and the
// invite ROW is still recorded — a suppressed address returns the same
// 201 {"status":"invited"} as any other, because the invitation exists and is
// claimable if the address is later un-suppressed. What must NOT happen is mail
// being queued to a hard-bounced address.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL are set
// (the session path needs Valkey), like the neighbouring RBAC tests.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestCreateInviteSuppressedAddressStillInvitedButNotEmailed: a suppressed
// address gets the normal 201 response and an invite row, but no email is
// queued or sent.
func TestCreateInviteSuppressedAddressStillInvitedButNotEmailed(t *testing.T) {
	pool := bundleTestPool(t)
	ctx := context.Background()
	h := rbacHandlers(t, pool)
	supps := dbstore.NewSuppressionStore(pool)
	h.Suppressions = supps
	h.EmailQueue = nil // synchronous path: Mailer is the send path
	mailer := &recorderMailer{}
	h.Mailer = mailer

	wsID, wsSlug, memberID, _ := rbacFixture(t, ctx, pool, dbstore.RoleAdmin)
	token := rbacSessionCookie(t, h, memberID)

	// Suppress the invitee.
	invitee := "invite-suppressed-" + randSuffix() + "@example.com"
	if _, err := supps.Add(ctx, invitee, dbstore.SuppressionReasonHardBounce,
		dbstore.SuppressionSourceBounce, "Permanent/NoEmail", ""); err != nil {
		t.Fatalf("seed suppression: %v", err)
	}

	// Silence the handler's slog noise but keep it out of the assertion.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	rec := rbacDo(t, h, http.MethodPost,
		"/v1/workspaces/"+wsID+"/invites?workspace="+wsSlug, token,
		map[string]any{"email": invitee, "role": dbstore.RoleReadOnly})

	if rec.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201 (a suppressed address must still be invited); body=%s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"invited"`) {
		t.Fatalf("invite body = %s, want status invited", rec.Body.String())
	}
	if mailer.wasCalled() {
		t.Fatal("a suppressed address must not receive an invite email")
	}
}

// TestCreateInviteUnsuppressedAddressIsEmailed is the regression guard: the new
// check must not stop ordinary invitations.
func TestCreateInviteUnsuppressedAddressIsEmailed(t *testing.T) {
	pool := bundleTestPool(t)
	ctx := context.Background()
	h := rbacHandlers(t, pool)
	h.Suppressions = dbstore.NewSuppressionStore(pool)
	h.EmailQueue = nil
	mailer := &recorderMailer{}
	h.Mailer = mailer

	wsID, wsSlug, memberID, _ := rbacFixture(t, ctx, pool, dbstore.RoleAdmin)
	token := rbacSessionCookie(t, h, memberID)

	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	invitee := "invite-ok-" + randSuffix() + "@example.com"
	rec := rbacDo(t, h, http.MethodPost,
		"/v1/workspaces/"+wsID+"/invites?workspace="+wsSlug, token,
		map[string]any{"email": invitee, "role": dbstore.RoleReadOnly})

	if rec.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if !mailer.wasCalled() {
		t.Fatal("an unsuppressed invitee must still be emailed")
	}
}
