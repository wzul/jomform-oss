package mcpsrv

// The ONE write tool of the in-app chat (phase 1), exposed over MCP for parity
// with the dashboard chat.
//
// WHY IT LIVES HERE. AGENTS.md requires MCP parity for every dashboard
// capability, and the in-app chat's single write is "file this conversation as
// feedback". Without a matching tool the capability would exist on the dashboard
// only, which is exactly the drift the parity rule exists to prevent — and the
// #477 parity guard would fail on an unregistered capability.
//
// IT IS NOT A SECOND FEEDBACK PATH. It calls internal/feedbacksvc — the same
// service the /feedback page and the chat runner use — so the length bounds, the
// per-user daily limit and the admin notification apply identically. Filing here
// is the same act as filing on the page, by the same person, for the same
// reason.
//
// AUTHORIZATION. Two independent gates, both required:
//
//  1. a USER identity. A feedback thread belongs to the PERSON who filed it and
//     carries no account id (migration 000063), so an API-key caller — which has
//     no user — is refused rather than mapped to its account owner. That is the
//     same rule feedbackScopeFor applies to the other four feedback tools.
//  2. a WRITE scope. Filing is a write, so a readonly credential is refused by
//     requireWriteScope — the gate issue #645 restored. The chat runs its tool
//     calls WITH this scope deliberately absent, so its own filing goes through
//     the in-process service rather than this tool; an external agent needs the
//     scope.
//
// NO SPAM JUDGE. Wan's decision (see the in-app feedback card): every filer is
// authenticated, so the threat is one account flooding, and that is answered
// with the per-user daily limit instead of a classifier that can misclassify a
// genuine complaint.

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/feedbacksvc"
)

type submitFeedbackIn struct {
	Subject string `json:"subject,omitempty" jsonschema:"Tajuk ringkas (maksimum 200 aksara). Kosong dibenarkan — thread tanpa tajuk dipaparkan dengan petikan isinya."`
	Body    string `json:"body" jsonschema:"Isi maklum balas atau permintaan ciri (minimum 10 aksara, maksimum 5000). Tulis apa yang pengguna laporkan."`
}

type submitFeedbackOut struct {
	ThreadID  string `json:"thread_id"`
	Status    string `json:"status" jsonschema:"open"`
	Subject   string `json:"subject,omitempty"`
	CreatedAt string `json:"created_at"`
}

// submitFeedback files a feedback thread for the authenticated USER.
//
// The filer is the identity's user id — there is no argument naming a user, so
// an agent cannot file on someone else's behalf (the same shape the REST
// endpoint has: the filer is the session, never a parameter).
func (s *Server) submitFeedback(ctx context.Context, _ *mcp.CallToolRequest, in submitFeedbackIn) (*mcp.CallToolResult, submitFeedbackOut, error) {
	// Gate 1: a user identity must exist. Checked BEFORE the store so a
	// misconfiguration cannot mask the authorization decision (see listFeedback).
	id := IdentityFromContext(ctx)
	if id == nil {
		return nil, submitFeedbackOut{}, errors.New("token tiada atau tidak sah")
	}
	if id.UserID == "" {
		return nil, submitFeedbackOut{}, errors.New(
			"maklum balas ialah data peribadi pengguna — API key tidak boleh memfailkannya. " +
				"Guna token OAuth (terikat pada pengguna) atau hantar melalui papan pemuka /feedback.")
	}
	// Gate 2: the credential's write scope.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, submitFeedbackOut{}, err
	}

	svc, err := s.feedbackService()
	if err != nil {
		return nil, submitFeedbackOut{}, err
	}
	th, err := svc.CreateThread(ctx, id.UserID, strings.TrimSpace(in.Subject), in.Body)
	if err != nil {
		return nil, submitFeedbackOut{}, err
	}
	return nil, submitFeedbackOut{
		ThreadID:  th.ID,
		Status:    th.Status,
		Subject:   th.Subject,
		CreatedAt: th.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// feedbackService builds the shared filing service for this server, mirroring
// the REST handler's wiring: the same store, the same notification hook. The
// notification itself is injected (Deps.NotifyFeedback) so the MCP layer does
// not need the mailer — main.go wires the REST handler's notifier into it.
func (s *Server) feedbackService() (*feedbacksvc.Service, error) {
	if s.deps == nil || s.deps.Feedback == nil {
		return nil, feedbacksvc.ErrNotConfigured
	}
	return &feedbacksvc.Service{
		Store:  s.deps.Feedback,
		Users:  s.deps.Users,
		Notify: s.deps.NotifyFeedback,
	}, nil
}
