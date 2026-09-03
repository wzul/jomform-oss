// Package spamjudge — background AI spam detection (issue #245).
//
// On submit, a spam-judge job is enqueued to the background queue (#199).
// A worker sends the submission (field values, email, IP, form context) to
// Ollama Cloud gemma4 and asks it to judge legit vs spam. On a spam verdict
// the entry is flagged is_spam=true with a reason; a legit verdict leaves it
// normal. The judge never blocks the submit response.
//
// The job rides the same durable Valkey-backed queue as email (#199), so it
// survives a restart. The worker is the emailqueue.Worker with a dispatcher
// that routes the "spam_judge" job type here.
package spamjudge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/llm"
)

// JobType is the emailqueue.Job.Type used for spam-judge jobs.
const JobType = "spam_judge"

// Job carries everything the judge needs to classify one submission. It is
// serialized into the emailqueue.Job.Body as JSON.
type Job struct {
	// SubmissionID is set for response-mode submissions; SaleID for
	// payment-mode sales. Exactly one is non-empty.
	SubmissionID string `json:"submission_id,omitempty"`
	SaleID       string `json:"sale_id,omitempty"`
	AccountID    string `json:"account_id"`
	FormID       string `json:"form_id"`
	FormTitle    string `json:"form_title,omitempty"`
	// Data is the submitted field values (block_id → value).
	Data map[string]any `json:"data"`
	// Email is the submitter's email ("" when none given).
	Email string `json:"email,omitempty"`
	// IP is the submitter's client IP.
	IP string `json:"ip,omitempty"`
}

// Enqueue pushes a spam-judge job onto the background queue. Best-effort: a
// failure is logged by the caller and never fails the submit response.
func Enqueue(ctx context.Context, q *emailqueue.Queue, job Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.Enqueue(ctx, emailqueue.Job{
		Type:      JobType,
		AccountID: job.AccountID,
		Body:      string(payload),
	})
}

// buildPrompt assembles the classification prompt from the submission. It
// includes the field values, email, IP and form context so the model can
// judge legit vs spam.
func buildPrompt(job Job) string {
	var sb strings.Builder
	sb.WriteString("Classify the following form submission as SPAM or LEGIT.\n\n")
	if job.FormTitle != "" {
		sb.WriteString("Form: " + job.FormTitle + "\n")
	}
	if job.Email != "" {
		sb.WriteString("Submitter email: " + job.Email + "\n")
	}
	if job.IP != "" {
		sb.WriteString("Submitter IP: " + job.IP + "\n")
	}
	sb.WriteString("Submitted field values:\n")
	if len(job.Data) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for k, v := range job.Data {
			sb.WriteString("- " + k + ": " + fmt.Sprintf("%v", v) + "\n")
		}
	}
	sb.WriteString("\nReply with exactly one word: SPAM or LEGIT.")
	return sb.String()
}

// Judge classifies one job and applies the verdict to the store. It returns
// the spam verdict (true = spam) and the reason. A judge error is returned so
// the worker can retry with backoff.
func Judge(ctx context.Context, client *llm.Client, subs *dbstore.SubmissionStore, sales *dbstore.SaleStore, job Job) (bool, error) {
	spam, err := client.ClassifySpam(ctx, buildPrompt(job))
	if err != nil {
		return false, err
	}
	reason := ""
	if spam {
		reason = "AI judge (gemma4) flagged as spam"
	}
	if job.SubmissionID != "" && subs != nil {
		if spam {
			if err := subs.MarkSpam(ctx, job.SubmissionID, reason); err != nil {
				return false, err
			}
		}
	} else if job.SaleID != "" && sales != nil {
		if spam {
			if err := sales.MarkSpam(ctx, job.SaleID, reason); err != nil {
				return false, err
			}
		}
	}
	return spam, nil
}

// Handler implements emailqueue.Handler for spam-judge jobs. It is wired into
// the background worker's dispatcher so the same queue serves both email and
// spam-judge jobs.
type Handler struct {
	LLM   *llm.Client
	Subs  *dbstore.SubmissionStore
	Sales *dbstore.SaleStore
}

// Send classifies one spam-judge job. A returned error triggers the worker's
// retry-with-backoff ladder.
func (h *Handler) Send(ctx context.Context, job emailqueue.Job) error {
	if h.LLM == nil {
		slog.Warn("spamjudge_no_llm", "account", job.AccountID)
		return nil
	}
	var sj Job
	if err := json.Unmarshal([]byte(job.Body), &sj); err != nil {
		slog.Warn("spamjudge_bad_job", "err", err)
		return nil
	}
	spam, err := Judge(ctx, h.LLM, h.Subs, h.Sales, sj)
	if err != nil {
		slog.Warn("spamjudge_judge_failed", "account", sj.AccountID, "err", err)
		return err
	}
	slog.Info("spamjudge_done", "account", sj.AccountID, "submission", sj.SubmissionID, "sale", sj.SaleID, "spam", spam)
	return nil
}
