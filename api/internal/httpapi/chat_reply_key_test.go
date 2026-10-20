package httpapi

// THE WIRE CARRIES THE ANSWER'S KEY WHEN THE MODEL WROTE NOTHING (t_7f1694cb).
//
// The runner half is pinned in api/internal/chat (TestRefusedProposalStillAnswersTheUser).
// This file pins the EDGE, which is the only place the page actually reads: a
// Result whose ReplyKey the handler forgot to copy is indistinguishable, on the
// browser side, from a run that had no key at all — `reply: ""`, an empty bubble,
// and a refusal block, which is the defect this card was filed on.
//
// Both directions are asserted, because either one alone is satisfied by a
// handler that always (or never) sends the field: an empty answer carries
// `chat.reply.noAnswer`, and a real answer carries NO reply_key at all (omitempty)
// so the page cannot prefer a canned sentence over the model's own words.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
)

func TestChatResponseCarriesTheAnswerKeyOnlyWhenThereIsNoProse(t *testing.T) {
	t.Run("the model said nothing", func(t *testing.T) {
		runner := &recordingRunner{ready: true, reply: &chat.Result{
			Reply:    "",
			ReplyKey: "chat.reply.noAnswer",
			Steps: []chat.Step{
				{Tool: "propose_write", Status: chat.StepRefused, DetailKey: "chat.refused.proposal"},
				{Tool: "propose_write", Status: chat.StepRefused, DetailKey: "chat.refused.proposal"},
			},
		}}
		e := newChatEnv(t, runner)
		_, _, token := e.newUser(t)

		rec := e.do(t, token, http.MethodPost, "/v1/rest/chat",
			`{"message":"Create a form called My New Form with one text field."}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var body struct {
			Reply    string `json:"reply"`
			ReplyKey string `json:"reply_key"`
			Steps    []struct {
				Tool      string `json:"tool"`
				Status    string `json:"status"`
				DetailKey string `json:"detail_key"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode chat response: %v (body %s)", err, rec.Body.String())
		}
		if body.Reply != "" {
			t.Fatalf("reply = %q, want empty — this arm is the silent model", body.Reply)
		}
		if body.ReplyKey != "chat.reply.noAnswer" {
			t.Fatalf("reply_key = %q on the wire, want chat.reply.noAnswer — the page renders an "+
				"EMPTY assistant bubble without it, which is the reported defect (t_7f1694cb)",
				body.ReplyKey)
		}
		if len(body.Steps) != 2 {
			t.Fatalf("steps = %+v, want the two refusals", body.Steps)
		}
	})

	t.Run("the model answered", func(t *testing.T) {
		runner := &recordingRunner{ready: true, reply: &chat.Result{
			Reply: "Borang itu tidak dapat disediakan. Beritahu saya namanya.",
		}}
		e := newChatEnv(t, runner)
		_, _, token := e.newUser(t)

		rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode chat response: %v (body %s)", err, rec.Body.String())
		}
		if _, present := body["reply_key"]; present {
			t.Fatalf("a real answer carried reply_key = %v — the page would resolve a canned "+
				"sentence over the model's own words", body["reply_key"])
		}
	})
}
