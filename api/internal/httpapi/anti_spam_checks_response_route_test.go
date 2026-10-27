package httpapi

// The response route must keep RUNNING its anti-spam layers even though it no
// longer STORES the two hidden controls they read.
//
// `website` (honeypot) and `jf_ts` (render timestamp) ride every public form's
// fields map back to the server. anti_spam_storage_t_a00d633f_test.go pins the
// storage rule — both are dropped from the map that becomes submissions.data —
// and the layers that consume them are what make that storage rule safe: a
// "fix" that stopped storing the controls by no longer CHECKING them (deleting
// the branch, returning early, or pruning the fields map before the anti-spam
// block) would leave every storage guard green while turning a response form
// into an open door. Every submission a bot sent would be stored as an answer.
//
// Each test below drives the real public submit route on a published
// response-mode block form and asserts BOTH halves on the same request: the
// submit is rejected with 400 AND no submission row is written. The positive
// control is TestBlockFormSubmissionStoresOnlyTheSendersAnswers — the same body
// with an empty honeypot and an old timestamp gets 201 and is stored — so a 400
// here cannot be a malformed body or a missing field contract.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// responseSubmitBody is the envelope the response page's own script posts:
// client identity plus the fields map, with the renderer's two hidden controls
// riding along under the names the renderer emits.
func responseSubmitBody(honeypot string, renderTimestamp int64) string {
	b, err := json.Marshal(map[string]any{
		"client": map[string]any{"full_name": "Ali", "email": "ali@example.test"},
		"fields": map[string]any{
			"b1":                  "Ali",
			"b2":                  "ali@example.test",
			antiSpamHoneypotField: honeypot,
			antiSpamRenderField:   strconv.FormatInt(renderTimestamp, 10),
		},
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// postResponseSubmit sends body to the form's public submit route. The Host
// names the fixture's workspace, which is how a published form is resolved.
func postResponseSubmit(t *testing.T, f issue622Fixture, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+f.formSlug+"/order", bytes.NewBufferString(body))
	req.Host = f.workspaceSl + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// storedSubmissionsForAccount counts the submission rows the fixture's account
// owns. The fixture mints a fresh account per test, so this counts exactly the
// rows the requests under test wrote.
func storedSubmissionsForAccount(t *testing.T, f issue622Fixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM submissions WHERE account_id = $1`, f.accountID).Scan(&n); err != nil {
		t.Fatalf("count stored submissions: %v", err)
	}
	return n
}

// TestResponseSubmitRejectsFilledHoneypot pins layer 1 on the response route: a
// filled honeypot is a bot, the submit is refused, and nothing is stored.
func TestResponseSubmitRejectsFilledHoneypot(t *testing.T) {
	f := issue622New(t)
	// An old timestamp, so the time layer cannot be the reason for the refusal:
	// this case must be decided by the honeypot alone.
	rec := postResponseSubmit(t, f, responseSubmitBody("http://spam.example", 1700000000))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("filled honeypot status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if n := storedSubmissionsForAccount(t, f); n != 0 {
		t.Fatalf("a honeypot-rejected submit stored %d submission row(s); nothing a bot sends may be persisted", n)
	}
}

// TestResponseSubmitRejectsRenderStampThatCannotBeOldEnough pins layer 3 on the
// response route: the honeypot is empty here, so only the render-time comparison
// can refuse this submit, and nothing may be stored.
//
// The stamp is in the FUTURE rather than a fresh `time.Now()`. A bot, or a page
// nobody read, is exactly what "the submit did not arrive at least
// antiSpamMinRenderTime after the render" describes, and a future stamp makes
// that true by construction — a `now` stamp would silently stop meaning it if
// this process were descheduled for the whole three-second window, which is a
// shared gate host's failure mode, not the route's. The comparison under test is
// the same one either way.
func TestResponseSubmitRejectsRenderStampThatCannotBeOldEnough(t *testing.T) {
	f := issue622New(t)
	stamp := time.Now().Add(time.Minute).Unix()
	rec := postResponseSubmit(t, f, responseSubmitBody("", stamp))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("render stamp %d (a minute in the future) status = %d, want 400 (body %s)",
			stamp, rec.Code, rec.Body.String())
	}
	if n := storedSubmissionsForAccount(t, f); n != 0 {
		t.Fatalf("a submit the time layer refuses stored %d submission row(s); nothing it refuses may be persisted", n)
	}
}
