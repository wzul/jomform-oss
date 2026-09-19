// Issue #615 acceptance gap: the delivered regression tests publish to the
// hub DIRECTLY, so they prove the wire stays open but not that production ever
// feeds it. The card is explicit — "Events actually ARRIVE: trigger a real sale
// or submission and confirm the event is pushed to a connected client. A 200
// that streams nothing is not a fix."
//
// This test closes that gap. It drives a REAL public form submission through
// the handler production serves (PublicHandlers.handleSubmit →
// handleResponseSubmit → the #447 publish inside public.go) over the full
// middleware chain, and requires the resulting response.submitted frame to
// reach a live SSE client. Nothing in this file publishes to the hub by hand:
// the only thing that reaches the stream is what the submit handler emits.
//
// Why an integration test: a bare httptest.ResponseRecorder satisfies
// http.Flusher by itself, so it cannot show whether the real chain streams.
//
// Needs JOMFORM_TEST_DATABASE_URL (scratch schema); skips otherwise.
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestEventsStreamDeliversARealFormSubmission is the end-to-end proof for
// issue #615: an authenticated SSE client stays connected through the logging
// middleware AND receives the event that a buyer's real form submission
// produced. Pre-fix the connect itself answered 500 (the wrapper hid
// http.Flusher), so the client never even opened.
func TestEventsStreamDeliversARealFormSubmission(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// A real owner + workspace + account, plus a published response-mode form,
	// exactly as the public submit path resolves them.
	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	form := &dbstore.Form{
		AccountID: accountID, Slug: "it-e2e-" + accountID[:8],
		Title: "Realtime response form", Status: "published",
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
				map[string]any{"id": "b3", "type": "textarea", "value": "Feedback"},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// ONE hub instance behind both the stream (REST) and the submit (Public) —
	// this is what main.go does, and it is what makes an event that a
	// submission produces reach a connected client.
	hub := NewRealtimeHub()

	rest := newBundleHandlers(pool)
	rest.Realtime = hub
	rawKey := bundleAPIKey(t, ctx, pool, accountID)

	pub := &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
		Realtime:    hub,
	}

	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{REST: rest, Public: pub})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// ── 1. open the authenticated stream through the real chain ──────────────
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/v1/rest/events/stream", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events/stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/rest/events/stream = %d, want 200 (a 500 here is issue #615: the "+
			"middleware chain still hides http.Flusher)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	br := bufio.NewReader(resp.Body)
	// The opening comment must reach us flushed, i.e. the stream is live.
	if line, err := br.ReadString('\n'); err != nil || strings.TrimSpace(line) != ": connected" {
		t.Fatalf("opening line = %q (err %v), want \": connected\"", strings.TrimSpace(line), err)
	}

	frames := make(chan string, 8)
	go func() {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				close(frames)
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				frames <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// ── 2. submit the form the way a buyer's browser does ───────────────────
	// Public route on the workspace subdomain: POST /f/{slug}/order. The
	// handler resolves the form by subdomain, stores the submission, and
	// publishes response.submitted to the hub — that publish is the only
	// thing that can put a frame on the stream below.
	body := `{"client":{"full_name":"Siti","email":"siti@example.test","phone":"0123456789"},
	          "fields":{"b1":"Siti","b2":"siti@example.test","b3":"Nasi lemak please"}}`
	subReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ts.URL+"/f/"+form.Slug+"/order", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build submit request: %v", err)
	}
	subReq.Host = workspaceSlug + ".jomform.com"
	subReq.Header.Set("Content-Type", "application/json")
	// The submit route is per-IP rate limited; a fresh IP keeps this test
	// independent of whatever else ran in the package.
	subReq.Header.Set("CF-Connecting-IP", uniqueTestIP())

	subResp, err := http.DefaultClient.Do(subReq)
	if err != nil {
		t.Fatalf("POST order: %v", err)
	}
	defer subResp.Body.Close()
	if subResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /f/%s/order = %d, want 201 (the submission must succeed before "+
			"anything can be published)", form.Slug, subResp.StatusCode)
	}

	// ── 3. the event must ARRIVE, and it must be THIS submission's ──────────
	// No manual publish anywhere: if the handler's publish is unwired or the
	// stream is dead, nothing shows up.
	select {
	case raw, ok := <-frames:
		if !ok {
			t.Fatal("the stream closed before the submission's event arrived")
		}
		var ev struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("bad event frame %q: %v", raw, err)
		}
		if ev.Event != "response.submitted" {
			t.Fatalf("streamed event = %q, want response.submitted", ev.Event)
		}
		if ev.Data["form_id"] != form.ID {
			t.Fatalf("event form_id = %v, want %s (the frame must belong to the form that "+
				"was just submitted)", ev.Data["form_id"], form.ID)
		}
		if ev.Data["submission_id"] == nil || ev.Data["submission_id"] == "" {
			t.Fatalf("event carries no submission_id: %v", ev.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a real form submission produced NO event on the connected stream within 5s — " +
			"the stream is open but nothing feeds it (a 200 that streams nothing is not a fix)")
	}
}

// TestEventsStreamStaysQuietForAnotherAccountsSubmission is the negative half:
// a submission on a DIFFERENT account must not reach this stream. Without it,
// the test above would still pass if the hub fanned every event out to every
// subscriber — which would be a cross-tenant leak, not a fix.
func TestEventsStreamStaysQuietForAnotherAccountsSubmission(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// Account A — the one that owns the stream.
	var userA, wsA, accA string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userA); err != nil {
		t.Fatalf("create user A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-a-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userA).Scan(&wsA); err != nil {
		t.Fatalf("create workspace A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-a-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userA, wsA).Scan(&accA); err != nil {
		t.Fatalf("create account A: %v", err)
	}

	// Account B — a different tenant with its own published response form.
	var userB, wsB, accB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userB); err != nil {
		t.Fatalf("create user B: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-b-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userB).Scan(&wsB); err != nil {
		t.Fatalf("create workspace B: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-b-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userB, wsB).Scan(&accB); err != nil {
		t.Fatalf("create account B: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formB := &dbstore.Form{
		AccountID: accB, Slug: "it-leak-" + accB[:8],
		Title: "Foreign response form", Status: "published",
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, formB); err != nil {
		t.Fatalf("create form B: %v", err)
	}

	hub := NewRealtimeHub()

	rest := newBundleHandlers(pool)
	rest.Realtime = hub
	rawA := bundleAPIKey(t, ctx, pool, accA)

	pub := &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
		Realtime:    hub,
	}

	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{REST: rest, Public: pub})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/v1/rest/events/stream", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawA)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events/stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET events/stream = %d, want 200", resp.StatusCode)
	}

	br := bufio.NewReader(resp.Body)
	if line, err := br.ReadString('\n'); err != nil || strings.TrimSpace(line) != ": connected" {
		t.Fatalf("opening line = %q (err %v), want \": connected\"", strings.TrimSpace(line), err)
	}

	frames := make(chan string, 4)
	go func() {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				close(frames)
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				frames <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// B submits its own form. A's stream must stay silent.
	body := `{"client":{"full_name":"Ben","email":"ben@example.test"},
	          "fields":{"b1":"Ben","b2":"ben@example.test"}}`
	subReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ts.URL+"/f/"+formB.Slug+"/order", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build submit request: %v", err)
	}
	subReq.Host = wsB + ".jomform.com"
	subReq.Header.Set("Content-Type", "application/json")
	subReq.Header.Set("CF-Connecting-IP", uniqueTestIP())

	subResp, err := http.DefaultClient.Do(subReq)
	if err != nil {
		t.Fatalf("POST order: %v", err)
	}
	defer subResp.Body.Close()
	if subResp.StatusCode != http.StatusCreated {
		t.Fatalf("foreign submit = %d, want 201 (body must succeed for the leak check to mean anything)",
			subResp.StatusCode)
	}

	select {
	case raw, ok := <-frames:
		if !ok {
			t.Fatalf("stream closed unexpectedly")
		}
		t.Fatalf("another account's submission leaked onto this stream: %s", raw)
	case <-time.After(2 * time.Second):
		// Silence is the pass condition here.
	}
}
