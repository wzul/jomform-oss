// The judge's fail-open contract (card t_0e9dc7fc).
//
// Every decision call can fail — a provider 500, a body that is not JSON, a
// timeout — and none of those may cost a real submission its flag or push the
// job into the worker's retry ladder. Judge therefore maps a decision error to
// "not spam", logs ONE line naming the cause, and returns no error. An error
// still means a STORE failure, which is a different thing and still retries.
//
// These tests run the real llm client against httptest servers: no network.
package spamjudge

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/llm"
)

// captureLogs routes the default logger into a buffer for the duration of the
// test, so "logs once" can be counted rather than assumed.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// decisionStub is the router's /systemone route, canned per test.
func decisionStub(t *testing.T, handler http.HandlerFunc) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return llm.New(srv.URL, "chat-model")
}

func TestJudgeVerdictFollowsTheDecisionRoute(t *testing.T) {
	// The two measured samples, keyed on what the submission actually says so
	// the request path is exercised rather than a hardcoded answer.
	client := decisionStub(t, func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		noul := "0.0141"
		if strings.Contains(body.String(), "BUY CHEAP FOLLOWERS") {
			noul = "0.987"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"clef","answers":{"is_spam":{"type":"noul","noul":` + noul + `}}}`))
	})

	captureLogs(t)
	spam, err := Judge(context.Background(), client, nil, nil,
		Job{SubmissionID: "s-spam", AccountID: "a1", Data: map[string]any{"message": "BUY CHEAP FOLLOWERS"}})
	if err != nil {
		t.Fatalf("spam sample: Judge returned an error: %v", err)
	}
	if !spam {
		t.Fatal("the measured spam sample (noul 0.987) was judged not spam")
	}

	spam, err = Judge(context.Background(), client, nil, nil,
		Job{SubmissionID: "s-genuine", AccountID: "a1", Data: map[string]any{"message": "Bila boleh ambil?"}})
	if err != nil {
		t.Fatalf("genuine sample: Judge returned an error: %v", err)
	}
	if spam {
		t.Fatal("the measured genuine sample (noul 0.0141) was judged spam")
	}
}

// TestJudgeFailsOpenAndLogsOncePerFailure is the card's acceptance: a 500, a
// malformed body and a timeout each come back as "not spam" with NO error (so
// the worker does not retry), and each writes exactly one log line naming the
// cause. A judge that logged nothing would leave an operator blind to a router
// that stopped answering; one that logged twice per failure would be noise.
func TestJudgeFailsOpenAndLogsOncePerFailure(t *testing.T) {
	cases := []struct {
		name   string
		client func(t *testing.T) *llm.Client
		ctx    func() (context.Context, context.CancelFunc)
		want   string
	}{
		{
			name: "provider 500",
			client: func(t *testing.T) *llm.Client {
				return decisionStub(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
				})
			},
			ctx:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			want: "status 500",
		},
		{
			name: "malformed body",
			client: func(t *testing.T) *llm.Client {
				return decisionStub(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`<html>not an answer</html>`))
				})
			},
			ctx:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			want: "not valid JSON",
		},
		{
			name: "timeout",
			client: func(t *testing.T) *llm.Client {
				return decisionStub(t, func(w http.ResponseWriter, r *http.Request) {
					// Hold the response past the caller's deadline. A sleep, not
					// r.Context().Done(): a server does not reliably observe a
					// client disconnect while the request body is unread, and a
					// handler that waited on it would hang the suite.
					time.Sleep(300 * time.Millisecond)
				})
			},
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			want: "context deadline exceeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLogs(t)
			client := tc.client(t)
			ctx, cancel := tc.ctx()
			defer cancel()

			spam, err := Judge(ctx, client, nil, nil, Job{SubmissionID: "s1", AccountID: "acct"})
			if err != nil {
				t.Fatalf("Judge returned an error on a failed decision call: %v — that sends the job into the "+
					"worker's retry ladder instead of failing open", err)
			}
			if spam {
				t.Fatal("a failed decision call was judged SPAM — one flaky call would delete a real submission " +
					"after the 30-day retention sweep")
			}
			lines := 0
			for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				if strings.Contains(line, "spamjudge_decision_failed") {
					lines++
					if !strings.Contains(line, tc.want) {
						t.Errorf("the log line does not name the cause %q: %s", tc.want, line)
					}
					if !strings.Contains(line, "fail_open=true") {
						t.Errorf("the log line does not say the judge failed open: %s", line)
					}
				}
			}
			if lines != 1 {
				t.Fatalf("wrote %d decision-failure log lines, want exactly 1 (log: %s)", lines, buf.String())
			}
		})
	}
}

// TestJudgeStoresTheReasonItBuilds is a WIRING guard, and it reads the source on
// purpose: the store Judge writes through is a concrete
// *dbstore.SubmissionStore, so a DB-free test cannot observe the string that
// lands on the row. spamReason is asserted by value in the test above; this one
// asserts that Judge actually calls it with the client's model — without it the
// helper stays correct while the call site is edited back to a hardcoded label,
// which is exactly how the reason drifted for weeks (card t_0e9dc7fc).
func TestJudgeStoresTheReasonItBuilds(t *testing.T) {
	src, err := os.ReadFile("spamjudge.go")
	if err != nil {
		t.Fatalf("read spamjudge.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "reason = spamReason(client.SpamJudgeModel())") {
		t.Fatal("Judge no longer builds the stored reason from spamReason(client.SpamJudgeModel()): " +
			"the label would stop naming the model that answered")
	}
	if strings.Contains(body, `reason = "AI judge (`) {
		t.Fatal("Judge hardcodes the reason again instead of naming the model that answered")
	}
}

// TestSpamReasonNamesTheModelThatAnswered pins the stored label a merchant reads
// on /spam. It used to hardcode a model the judge had not asked for weeks; the
// reason now follows the client's configured model — and the shipped default is
// the decision id, not the chat model.
func TestSpamReasonNamesTheModelThatAnswered(t *testing.T) {
	if got := spamReason(llm.DefaultSpamJudgeModel); !strings.Contains(got, "clef") {
		t.Fatalf("spamReason(%q) = %q, want it to name clef", llm.DefaultSpamJudgeModel, got)
	}
	if got := spamReason("other-model"); !strings.Contains(got, "other-model") {
		t.Fatalf("spamReason(\"other-model\") = %q, want the configured model in the label", got)
	}
	if got := spamReason(""); !strings.Contains(got, llm.DefaultSpamJudgeModel) {
		t.Fatalf("spamReason(\"\") = %q, want the default decision model", got)
	}
	client := llm.New("", "").SetSpamJudge("chosen-model", 0.5)
	if got := client.SpamJudgeModel(); got != "chosen-model" {
		t.Fatalf("SpamJudgeModel() = %q, want the configured model — the reason would name the wrong one", got)
	}
}
