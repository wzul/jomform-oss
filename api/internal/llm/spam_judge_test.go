// The spam judge's decision seam (card t_0e9dc7fc).
//
// The judge used to share the chat client's model and route, so the stored
// spam_reason named a model that had not answered for weeks. It now asks the
// router's DECISION dialect — POST {LLM_BASE_URL}/systemone — with its own model
// (SPAM_JUDGE_MODEL, default `clef`) and reads the answer's `noul` confidence.
//
// These tests pin the wire contract, the threshold, the model constant, and the
// failure modes. They run against httptest servers: no network, no key, so they
// hold in the gate. The one live check (that /v1/models still publishes the id)
// is opt-in and skipped unless explicitly enabled.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// decisionServer is a stand-in for the router's /systemone route. It records
// every path it is asked for, so a test can also prove the CHAT route was never
// called (the router answers 400 there for a decision model, which would make
// every judge call fail while the unit suite stayed green).
type decisionServer struct {
	paths  []string
	bodies []systemoneRequest
	status int
	body   string
}

func newDecisionServer(t *testing.T, status int, body string) (*decisionServer, *Client) {
	t.Helper()
	ds := &decisionServer{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ds.paths = append(ds.paths, r.URL.Path)
		var req systemoneRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			ds.bodies = append(ds.bodies, req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ds.status)
		_, _ = w.Write([]byte(ds.body))
	}))
	t.Cleanup(srv.Close)
	// The chat model is deliberately a DIFFERENT id from the judge's: if the
	// seam leaked back to the chat client, the body would carry it.
	return ds, New(srv.URL, "chat-model")
}

// noulBody is a decision answer as the router shapes it.
func noulBody(noul float64) string {
	return fmt.Sprintf(`{"model":%q,"answers":{"is_spam":{"type":"noul","noul":%v}},`+
		`"usage":{"input_tokens":137,"output_tokens":0}}`, DefaultSpamJudgeModel, noul)
}

// TestSpamJudgePostsToTheDecisionRoute pins the request: the decision path (not
// /chat/completions), the judge's own model (not the chat model), the submission
// as `state`, and one `noul` question named is_spam with instructions and both
// criteria. A route or field mistake here is a 400 or an unreadable answer on
// every submission, while the classifier's own logic would still look correct.
func TestSpamJudgePostsToTheDecisionRoute(t *testing.T) {
	ds, client := newDecisionServer(t, http.StatusOK, noulBody(0.987))

	spam, err := client.ClassifySpam(context.Background(), "BUY CHEAP FOLLOWERS NOW")
	if err != nil {
		t.Fatalf("ClassifySpam: %v", err)
	}
	if !spam {
		t.Fatalf("noul 0.987 was not read as spam (threshold %v)", DefaultSpamJudgeThreshold)
	}
	if len(ds.paths) != 1 || ds.paths[0] != spamDecisionPath {
		t.Fatalf("decision call hit %v, want exactly [%q] — the chat route answers HTTP 400 for a "+
			"decision model, so posting there fails every submission silently", ds.paths, spamDecisionPath)
	}
	req := ds.bodies[0]
	if req.Model != DefaultSpamJudgeModel {
		t.Errorf("model = %q, want %q — the judge must ask its own decision model, not the chat model",
			req.Model, DefaultSpamJudgeModel)
	}
	if req.Model == "chat-model" {
		t.Errorf("the request carried the CHAT model %q: the per-job seam leaked", req.Model)
	}
	if !strings.Contains(req.State, "BUY CHEAP FOLLOWERS NOW") {
		t.Errorf("state does not carry the submission: %q", req.State)
	}
	q, ok := req.Questions[spamQuestionKey]
	if !ok {
		t.Fatalf("request has no %q question: %+v", spamQuestionKey, req.Questions)
	}
	if q.Type != "noul" {
		t.Errorf("question type = %q, want noul — any other type answers without a confidence", q.Type)
	}
	if strings.TrimSpace(q.Instructions) == "" {
		t.Error("question carries no instructions")
	}
	if q.Criteria["true"] == "" || q.Criteria["false"] == "" {
		t.Errorf("criteria must state both outcomes, got %v", q.Criteria)
	}
}

// TestSpamJudgeVerdictFollowsNoulAndThreshold pins the two measured samples and
// the >= boundary. A judge that inverted the comparison would flag every
// genuine submission and miss every spam one — and both directions cost a real
// entry, because the retention sweep deletes what is flagged.
func TestSpamJudgeVerdictFollowsNoulAndThreshold(t *testing.T) {
	cases := []struct {
		name      string
		noul      float64
		threshold float64
		want      bool
	}{
		{"measured spam sample", 0.987, DefaultSpamJudgeThreshold, true},
		{"measured genuine sample", 0.0141, DefaultSpamJudgeThreshold, false},
		{"exactly at the default threshold is spam", DefaultSpamJudgeThreshold, DefaultSpamJudgeThreshold, true},
		{"just below the default threshold is not", 0.4999, DefaultSpamJudgeThreshold, false},
		{"a stricter configured threshold keeps 0.987 legitimate", 0.987, 0.99, false},
		{"a looser configured threshold flags 0.4", 0.4, 0.3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newDecisionServer(t, http.StatusOK, noulBody(tc.noul))
			client.SetSpamJudge("", tc.threshold)
			got, err := client.ClassifySpam(context.Background(), "submission")
			if err != nil {
				t.Fatalf("ClassifySpam: %v", err)
			}
			if got != tc.want {
				t.Fatalf("noul %v at threshold %v = spam %v, want %v", tc.noul, tc.threshold, got, tc.want)
			}
		})
	}
}

// TestSpamJudgeModelOverrideReachesTheWire pins SPAM_JUDGE_MODEL: a deployment
// that names a different decision model must actually ask it, or the seam is a
// decoration and an operator fixes nothing by setting it.
func TestSpamJudgeModelOverrideReachesTheWire(t *testing.T) {
	ds, client := newDecisionServer(t, http.StatusOK, noulBody(0.9))
	client.SetSpamJudge("other-decision-model", 0.5)
	if _, err := client.ClassifySpam(context.Background(), "submission"); err != nil {
		t.Fatalf("ClassifySpam: %v", err)
	}
	if got := ds.bodies[0].Model; got != "other-decision-model" {
		t.Fatalf("model = %q, want the configured other-decision-model", got)
	}
	if got := client.SpamJudgeModel(); got != "other-decision-model" {
		t.Fatalf("SpamJudgeModel() = %q — the stored spam_reason would name a model that did not answer", got)
	}
}

// TestSpamJudgeReportsEveryUndecidableResponse is the CAUSE half of the
// fail-open contract: the judge package maps these errors to "not spam", so each
// one must arrive as an error carrying WHY, not as a quiet false verdict. A
// missing noul in particular must not be read as a confident 0 — that would hide
// a broken router behind "no spam".
func TestSpamJudgeReportsEveryUndecidableResponse(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"provider 500", http.StatusInternalServerError, `{"error":{"message":"boom"}}`, "status 500"},
		{"unparseable body", http.StatusOK, `not json`, "not valid JSON"},
		{"no answers", http.StatusOK, `{"model":"clef","answers":{}}`, "no answers.is_spam.noul"},
		{"answer without noul", http.StatusOK, `{"answers":{"is_spam":{"type":"noul"}}}`, "no answers.is_spam.noul"},
		{"noul is not a number", http.StatusOK, `{"answers":{"is_spam":{"noul":"high"}}}`, "not valid JSON"},
		{"chat route refusal", http.StatusBadRequest,
			`{"error":{"api":"systemone","message":"clef is a systemone model; call /v1/systemone instead"}}`,
			"call /v1/systemone instead"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newDecisionServer(t, tc.status, tc.body)
			spam, err := client.ClassifySpam(context.Background(), "submission")
			if spam {
				t.Error("a failed decision call returned SPAM — fail-open means never flag on an error")
			}
			if err == nil {
				t.Fatal("the failure was reported as a clean not-spam verdict: the judge could not log a cause")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to name %q so the single log line says what broke", err, tc.want)
			}
		})
	}
}

// TestSpamJudgeReportsATransportFailure covers the "no answer at all" half —
// a connection that is refused, and a call that runs out of time. Both must be
// errors; the timeout case pins that the caller's context is honoured, because
// a judge that ignores it would hold a worker for the client's whole timeout.
func TestSpamJudgeReportsATransportFailure(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		spam, err := New(url, "chat-model").ClassifySpam(context.Background(), "submission")
		if spam || err == nil {
			t.Fatalf("spam = %v, err = %v; want false and a transport error", spam, err)
		}
	})

	t.Run("context deadline", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Hold the response past the caller's deadline. A sleep, not
			// r.Context().Done(): a server does not reliably observe a client
			// disconnect while the request body is unread.
			time.Sleep(300 * time.Millisecond)
		}))
		t.Cleanup(srv.Close)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		spam, err := New(srv.URL, "chat-model").ClassifySpam(ctx, "submission")
		if spam || err == nil {
			t.Fatalf("spam = %v, err = %v; want false and a timeout error", spam, err)
		}
	})
}

// TestSpamJudgeModelIsThePublishedDecisionModel pins the constant. The value is
// the id `GET /v1/models` publishes with `"api":"systemone"` (router v1.13.1,
// probed 2026-10-26); it must NOT be the chat model, because the chat route
// answers HTTP 400 for it. If the provider renames the id, this fails by name
// instead of every submission 400-ing in production.
func TestSpamJudgeModelIsThePublishedDecisionModel(t *testing.T) {
	if DefaultSpamJudgeModel != "clef" {
		t.Fatalf("DefaultSpamJudgeModel = %q, want %q — the id GET /v1/models publishes with "+
			"\"api\":\"systemone\" (router v1.13.1, measured 2026-10-26). Re-probe the router's "+
			"/v1/models before changing this: a renamed id answers HTTP 400 on every judge call.",
			DefaultSpamJudgeModel, "clef")
	}
	if DefaultSpamJudgeModel == DefaultModel {
		t.Fatalf("the judge model equals the chat model (%q): the decision route refuses a chat id and "+
			"the chat route refuses a decision id, so one of the two surfaces would be dead",
			DefaultModel)
	}
	if DefaultSpamJudgeThreshold != 0.5 {
		t.Fatalf("DefaultSpamJudgeThreshold = %v, want 0.5 — the documented default; the measured "+
			"samples (spam 0.987, genuine 0.0141) only bracket it from a distance, and a drift here "+
			"changes who gets flagged", DefaultSpamJudgeThreshold)
	}
	if got := (&Client{}).SpamJudgeModel(); got != DefaultSpamJudgeModel {
		t.Fatalf("a client with no spam-judge setting reports %q, want the default %q", got, DefaultSpamJudgeModel)
	}
	if got := (&Client{}).spamJudgeThreshold(); got != DefaultSpamJudgeThreshold {
		t.Fatalf("a client with no spam-judge setting applies threshold %v, want the default %v",
			got, DefaultSpamJudgeThreshold)
	}
}

// TestSpamJudgeModelIsPublishedByTheRouter is the LIVE half of that guard, and
// it is opt-in so the gate stays hermetic: set JOMFORM_LIVE_ROUTER_PROBE=1 and
// point LLM_BASE_URL (and OLLAMA_API_KEY) at the router. It asks the provider
// what it publishes, so a router-side rename fails a test rather than silently
// 400-ing every submission. Run it when the router is upgraded, not per card.
func TestSpamJudgeModelIsPublishedByTheRouter(t *testing.T) {
	if os.Getenv("JOMFORM_LIVE_ROUTER_PROBE") != "1" {
		t.Skip("live provider probe: set JOMFORM_LIVE_ROUTER_PROBE=1 with LLM_BASE_URL/OLLAMA_API_KEY " +
			"to re-check that /v1/models still publishes the decision model")
	}
	base := strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	if base == "" {
		t.Fatal("LLM_BASE_URL is unset: the probe has no router to ask")
	}
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if key := os.Getenv("OLLAMA_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s/models: %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s/models answered %d", base, resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID  string `json:"id"`
			API string `json:"api"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode /models: %v", err)
	}
	for _, m := range out.Data {
		if m.ID != DefaultSpamJudgeModel {
			continue
		}
		if m.API != "systemone" {
			t.Fatalf("/v1/models publishes %q with api %q, want \"systemone\" — the judge posts to "+
				"/systemone and the chat route refuses a decision model", m.ID, m.API)
		}
		return
	}
	var ids []string
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	t.Fatalf("/v1/models no longer publishes %q (publishes %v). The judge would answer HTTP 400 on every "+
		"submission; update DefaultSpamJudgeModel and this test together.", DefaultSpamJudgeModel, ids)
}
