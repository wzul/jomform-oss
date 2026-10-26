package httpapi

// Card t_28baedff — the judge JOB must carry only the sender's answers.
//
// buildPrompt drops the renderer's hidden anti-spam fields again as a last line
// of defence (prompt_bookkeeping_t_28baedff_test.go), but the enqueue site is a
// separate thing to guard: `enqueueSpamJudge` used to pass the submitted fields
// map straight into spamjudge.Job.Data, so the durable queue body — which also
// names what the judge is going to read — carried `website` and `jf_ts`. A test
// that only calls buildPrompt stays green while the job payload regresses, and
// the queue body is exactly what an operator reads when asking "what did we
// send?".
//
// This drives the REAL public submit path (a published response form, the real
// handler, a real queue) so the assertion is about the wiring, not the helper.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/spamjudge"
)

func TestSpamJudgeJobCarriesOnlyTheSendersAnswers(t *testing.T) {
	f := issue622New(t)
	f.setJudge(t, true)

	// A normal browser submit: the renderer's honeypot (empty) and its render
	// timestamp ride the fields map next to the answers. The timestamp is old
	// enough to clear anti-spam layer 3, exactly like a real page that has been
	// open for a moment.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"fields":{` +
		`"b1":"Ali","b2":"ali@example.test","website":"","jf_ts":"1700000000"}}`
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+f.formSlug+"/order", bytes.NewBufferString(body))
	req.Host = f.workspaceSl + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	job, _, err := f.q.DequeueRaw(context.Background())
	if err != nil {
		t.Fatalf("dequeue spam-judge job: %v", err)
	}
	if job.Type != spamjudge.JobType {
		t.Fatalf("job type = %q, want %q", job.Type, spamjudge.JobType)
	}
	var payload spamjudge.Job
	if err := json.Unmarshal([]byte(job.Body), &payload); err != nil {
		t.Fatalf("unmarshal job body: %v", err)
	}
	for _, leaked := range []string{"website", "jf_ts"} {
		if v, ok := payload.Data[leaked]; ok {
			t.Errorf("the queued spam-judge job carries the renderer's bookkeeping %q=%v — it is not "+
				"the sender's answer, and it moves the verdict (job data: %v)", leaked, v, payload.Data)
		}
	}
	if payload.Data["b1"] != "Ali" || payload.Data["b2"] != "ali@example.test" {
		t.Errorf("the queued job lost a real answer: %v", payload.Data)
	}
	// The queue body is what the worker reads, so assert the wire form too: a
	// JudgeableData that filtered only the in-memory map would leave the leak here.
	if bytes.Contains([]byte(job.Body), []byte(`"jf_ts"`)) || bytes.Contains([]byte(job.Body), []byte(`"website"`)) {
		t.Errorf("the serialized job body still carries an anti-spam control: %s", job.Body)
	}
}
