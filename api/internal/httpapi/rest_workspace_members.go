// Workspace membership + invite endpoints (issue #161, part A of #160).
//
// Admin-only (owner or an admin member). Inviting an existing user adds them
// as a member directly; inviting a new user creates a pending invite and
// emails a signup link that, on completion, adds them with the invited role.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/dbstore"
	emailpkg "github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
)

// ── admin gate ──────────────────────────────────────────────────────────────

// requireWorkspaceAdmin resolves the workspace and verifies the caller is the owner or
// an admin member. Returns the workspace id on success.
func (h *RESTHandlers) requireWorkspaceAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	sID := r.PathValue("id")
	if _, perr := uuid.Parse(sID); perr != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return "", false
	}
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return "", false
	}
	sp, err := h.Workspaces.ByID(r.Context(), sID)
	if err != nil || sp == nil {
		writeErr(w, http.StatusNotFound, "workspace tidak dijumpai")
		return "", false
	}
	// Owner is always an admin (stored as an admin member row). Check both
	// the owner flag and the membership role for robustness.
	if sp.Owner == userID {
		return sID, true
	}
	if h.WorkspaceMembers != nil {
		admin, aerr := h.WorkspaceMembers.IsAdmin(r.Context(), sID, userID)
		if aerr == nil && admin {
			return sID, true
		}
	}
	writeErr(w, http.StatusForbidden, "perlu admin workspace")
	return "", false
}

// ── invites ─────────────────────────────────────────────────────────────────

// inviteEmailBody returns a localized invite email body (5 locales) with the
// signup link. The link carries the invite token so registration redeems it.
func inviteEmailBody(lang, link string) string {
	switch lang {
	case "ms":
		return "Anda telah dijemput untuk menyertai workspace JomForm.\n\n" +
			"Klik pautan berikut untuk mendaftar akaun dan menyertai workspace:\n\n" +
			link + "\n\n" +
			"Jika anda tidak mengharapkan jemputan ini, anda boleh abaikan emel ini.\n"
	case "zh":
		return "您已被邀请加入 JomForm 空间。\n\n" +
			"点击以下链接注册账户并加入空间：\n\n" +
			link + "\n\n" +
			"如果您没有预期收到此邀请，可以忽略此邮件。\n"
	case "ta":
		return "JomForm இடத்தில் சேர உங்களை அழைத்துள்ளோம்.\n\n" +
			"கணக்கைப் பதிவுசெய்து இடத்தில் சேர பின்வரும் இணைப்பைக் கிளிக் செய்யவும்:\n\n" +
			link + "\n\n" +
			"இந்த அழைப்பை நீங்கள் எதிர்பார்க்கவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம்.\n"
	case "ar":
		return "تمت دعوتك للانضمام إلى مساحة JomForm.\n\n" +
			"انقر على الرابط التالي لإنشاء حساب والانضمام إلى المساحة:\n\n" +
			link + "\n\n" +
			"إذا لم تكن تتوقع هذه الدعوة، يمكنك تجاهل هذا البريد.\n"
	default: // en
		return "You've been invited to join a JomForm workspace.\n\n" +
			"Click the link below to create an account and join the workspace:\n\n" +
			link + "\n\n" +
			"If you weren't expecting this invitation, you can ignore this email.\n"
	}
}

// inviteEmailSubject returns a localized invite email subject.
func inviteEmailSubject(lang string) string {
	switch lang {
	case "ms":
		return "Jemputan menyertai workspace JomForm"
	case "zh":
		return "加入 JomForm 空间的邀请"
	case "ta":
		return "JomForm இடத்தில் சேர அழைப்பு"
	case "ar":
		return "دعوة للانضمام إلى مساحة JomForm"
	default:
		return "Invitation to join a JomForm workspace"
	}
}

// createInvite: POST /v1/workspaces/{id}/invites {email, role}
func (h *RESTHandlers) createInvite(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		writeErr(w, http.StatusBadRequest, "emel wajib")
		return
	}
	role := in.Role
	if role == "" {
		role = dbstore.RoleReadOnly
	}
	if !dbstore.ValidWorkspaceRole(role) {
		writeErr(w, http.StatusBadRequest, "role mesti read_only|manage|admin")
		return
	}
	if h.WorkspaceMembers == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Existing user → add as a member directly.
	if u, uerr := h.Users.ByEmail(r.Context(), email); uerr == nil && u != nil {
		if err := h.WorkspaceMembers.AddMember(r.Context(), sID, u.ID, role); err != nil {
			respondErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "added", "email": email, "role": role,
		})
		return
	}
	// New user → pending invite + email.
	token, terr := randomToken()
	if terr != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	inv, err := h.WorkspaceMembers.CreateInvite(r.Context(), sID, email, role, token)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Localize the invite email to the workspace's default language.
	lang := "en"
	if st, serr := h.Workspaces.Settings(r.Context(), sID); serr == nil {
		lang = st.WorkspaceLang()
	}
	link := strings.TrimRight(h.BaseURL, "/") + "/login?invite=" + token
	// Suppression is checked before enqueue/send: the invite ROW is already
	// recorded, so a suppressed address still returns 201 {"status":"invited"}
	// — the invitation exists and is claimable if the address is later
	// un-suppressed — but no mail is queued for an address that hard-bounced.
	// The pre-send choke point would also catch it; this keeps the skip visible
	// at the point an operator would look for it.
	if h.Suppressions != nil {
		sup, serr := emailpkg.ShouldSuppress(r.Context(), h.Suppressions, email)
		if serr != nil {
			slog.Error("email_suppression_check_error", "to", obfuscateEmail(email), "err", serr)
		} else if sup {
			slog.Warn("invite_email_suppressed_skipped", "email", obfuscateEmail(email))
			recordSESSuppression("invite")
			writeJSON(w, http.StatusCreated, map[string]string{
				"status": "invited", "id": inv.ID, "email": email, "role": role,
			})
			return
		}
	}
	// Issue #199: when an EmailQueue is wired, enqueue the invite email and
	// return immediately — the worker sends it. Otherwise send synchronously.
	if h.EmailQueue != nil {
		if err := h.EmailQueue.Enqueue(r.Context(), emailqueue.Job{
			Type: "invite", To: email,
			Subject: inviteEmailSubject(lang), Body: inviteEmailBody(lang, link),
		}); err != nil {
			// The invite is recorded; a failed enqueue is surfaced but not fatal.
			writeJSON(w, http.StatusCreated, map[string]string{
				"status": "invited", "id": inv.ID, "email": email, "role": role,
				"email_error": "failed to queue invite email",
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{
			"status": "invited", "id": inv.ID, "email": email, "role": role,
		})
		return
	}
	if !mailerUnavailable(h.Mailer) {
		if merr := h.Mailer.Send(email, inviteEmailSubject(lang), inviteEmailBody(lang, link)); merr != nil {
			// The invite is recorded; a failed email is surfaced but not fatal.
			writeJSON(w, http.StatusCreated, map[string]string{
				"status": "invited", "id": inv.ID, "email": email, "role": role,
				"email_error": "failed to send invite email",
			})
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "invited", "id": inv.ID, "email": email, "role": role,
	})
}

// listInvites: GET /v1/workspaces/{id}/invites
func (h *RESTHandlers) listInvites(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	if h.WorkspaceMembers == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	invites, err := h.WorkspaceMembers.ListInvites(r.Context(), sID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Role      string `json:"role"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]row, 0, len(invites))
	for _, inv := range invites {
		out = append(out, row{ID: inv.ID, Email: inv.Email, Role: inv.Role, CreatedAt: inv.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteInvite: DELETE /v1/workspaces/{id}/invites/{inviteId}
func (h *RESTHandlers) deleteInvite(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	inviteID := r.PathValue("inviteId")
	if _, perr := uuid.Parse(inviteID); perr != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return
	}
	if h.WorkspaceMembers == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.WorkspaceMembers.DeleteInvite(r.Context(), sID, inviteID); err != nil {
		if errors.Is(err, dbstore.ErrNotWorkspaceMember) {
			writeErr(w, http.StatusNotFound, "jemputan tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── members ────────────────────────────────────────────────────────────────

// listMembers: GET /v1/workspaces/{id}/members
func (h *RESTHandlers) listMembers(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	if h.WorkspaceMembers == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	members, err := h.WorkspaceMembers.ListMembers(r.Context(), sID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
		Name   string `json:"name"`
		Role   string `json:"role"`
	}
	out := make([]row, 0, len(members))
	for _, m := range members {
		out = append(out, row{UserID: m.UserID, Email: m.Email, Name: m.Name, Role: m.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

// updateMemberRole: PATCH /v1/workspaces/{id}/members/{userId} {role}
func (h *RESTHandlers) updateMemberRole(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if _, perr := uuid.Parse(userID); perr != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if !dbstore.ValidWorkspaceRole(in.Role) {
		writeErr(w, http.StatusBadRequest, "role mesti read_only|manage|admin")
		return
	}
	if h.WorkspaceMembers == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.WorkspaceMembers.UpdateRole(r.Context(), sID, userID, in.Role); err != nil {
		if errors.Is(err, dbstore.ErrNotWorkspaceMember) {
			writeErr(w, http.StatusNotFound, "ahli tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "user_id": userID, "role": in.Role})
}

// removeMember: DELETE /v1/workspaces/{id}/members/{userId}
func (h *RESTHandlers) removeMember(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if _, perr := uuid.Parse(userID); perr != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return
	}
	// Prevent removing the last admin / the owner.
	if sp, serr := h.Workspaces.ByID(r.Context(), sID); serr == nil && sp != nil && sp.Owner == userID {
		writeErr(w, http.StatusBadRequest, "pemilik workspace tidak boleh dibuang")
		return
	}
	if h.WorkspaceMembers == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.WorkspaceMembers.RemoveMember(r.Context(), sID, userID); err != nil {
		if errors.Is(err, dbstore.ErrNotWorkspaceMember) {
			writeErr(w, http.StatusNotFound, "ahli tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "user_id": userID})
}

// randomToken returns a 32-byte hex token for invite redemption links.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
