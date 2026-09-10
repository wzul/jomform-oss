// Package emailqueue — persistent background email queue + worker (issue #199).
//
// Emails (receipts, merchant notifications, verification, magic link, password
// reset, 2FA codes, workspace invites) are enqueued by the request handlers and
// sent by a background worker, so a slow SES call never blocks the
// payment/submit/auth response.
//
// The queue is backed by Valkey (the same client that backs sessions) and is
// durable: jobs survive a restart. A job is a JSON blob pushed onto a list
// (`jf:emailq:jobs`). On a send failure the worker re-enqueues the job onto a
// delayed sorted set (`jf:emailq:delayed`) with an exponential backoff ladder,
// and promotes due jobs back onto the main list. After the max attempt count a
// job is dropped and logged.
//
// The worker is generic: it takes a Handler that performs the actual SES send.
// The wiring in cmd/jomform-api provides a dispatcher that routes each job type
// to the appropriate sender (auth / payments / REST).
package emailqueue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Job is one queued email. Type selects the sender path; the remaining fields
// carry the fully-rendered message (rendering happens in the request handler,
// which is cheap local work — only the SES network call is deferred to the
// worker).
type Job struct {
	// Type is one of: receipt | merchant_notify | verify | magic_link |
	// password_reset | email2fa | 2fa_active | invite.
	Type string `json:"type"`
	To   string `json:"to"`
	// Subject and Body are the fully-rendered message.
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// AccountID is the owning account ("" for system mail). Used for logging.
	AccountID string `json:"account_id,omitempty"`
	// SaleRef / SaleID correlate a receipt to its sale so the worker can
	// persist the SES MessageID (issue #106) for open/click correlation.
	SaleRef string `json:"sale_ref,omitempty"`
	SaleID  string `json:"sale_id,omitempty"`
	// PDFFilename / PDFData carry an optional receipt PDF attachment.
	PDFFilename string `json:"pdf_filename,omitempty"`
	PDFData     []byte `json:"pdf_data,omitempty"`
	// Attempts counts how many times this job has been tried (for backoff).
	Attempts int `json:"attempts,omitempty"`
}

// Queue is a durable Valkey-backed email job queue.
type Queue struct {
	rdb *goredis.Client
}

// NewQueue wires a queue over the given Valkey client (the same one that backs
// sessions). The client is shared — the queue does not own it.
func NewQueue(rdb *goredis.Client) *Queue {
	return &Queue{rdb: rdb}
}

const (
	// queueKey is the main FIFO list of ready jobs.
	queueKey = "jf:emailq:jobs"
	// delayedKey is a sorted set of jobs waiting for a retry, scored by the
	// unix time at which they become due.
	delayedKey = "jf:emailq:delayed"
	// dequeueTimeout bounds each BLPOP so the worker can periodically promote
	// due delayed jobs.
	dequeueTimeout = 2 * time.Second
)

// ErrNoJob is returned by Dequeue when the queue is empty (BLPOP timed out).
var ErrNoJob = errors.New("email queue empty")

// Enqueue pushes a job onto the ready list. Durable — survives a restart.
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.rdb.RPush(ctx, queueKey, data).Err()
}

// EnqueueDelayed schedules a job for a retry after delay. The job is stored in
// the delayed sorted set and promoted back to the ready list once due.
func (q *Queue) EnqueueDelayed(ctx context.Context, job Job, delay time.Duration) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	when := time.Now().Add(delay).Unix()
	return q.rdb.ZAdd(ctx, delayedKey, goredis.Z{Score: float64(when), Member: data}).Err()
}

// Dequeue pops the next ready job, blocking up to dequeueTimeout. Returns
// ErrNoJob when the queue is empty.
func (q *Queue) Dequeue(ctx context.Context) (Job, error) {
	res, err := q.rdb.BLPop(ctx, dequeueTimeout, queueKey).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return Job{}, ErrNoJob
		}
		return Job{}, err
	}
	// res is [key, value].
	var job Job
	if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
		return Job{}, err
	}
	return job, nil
}

// PromoteDue moves every delayed job whose due time has passed onto the ready
// list. Called by the worker each loop so retries are picked up.
func (q *Queue) PromoteDue(ctx context.Context) error {
	now := time.Now().Unix()
	res, err := q.rdb.ZRangeByScore(ctx, delayedKey, &goredis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now, 10),
	}).Result()
	if err != nil {
		return err
	}
	if len(res) == 0 {
		return nil
	}
	pipe := q.rdb.TxPipeline()
	for _, m := range res {
		pipe.RPush(ctx, queueKey, m)
		pipe.ZRem(ctx, delayedKey, m)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// Len reports the number of ready jobs (for tests/ops).
func (q *Queue) Len(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, queueKey).Result()
}

// Handler performs the actual send for one job. Returning an error triggers a
// retry with backoff.
type Handler interface {
	Send(ctx context.Context, job Job) error
}

// Worker polls the queue and sends jobs via the Handler, retrying failures with
// an exponential backoff ladder. It runs until ctx is cancelled.
type Worker struct {
	q           *Queue
	handler     Handler
	log         *slog.Logger
	maxAttempts int
	// backoff[i] is the delay before retry attempt i+1 (0-indexed).
	backoff []time.Duration
}

// NewWorker wires a worker. maxAttempts<=0 defaults to 5; an empty backoff
// ladder defaults to 30s / 2m / 10m / 1h.
func NewWorker(q *Queue, h Handler, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	maxAttempts := 5
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	backoff := []time.Duration{
		30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour,
	}
	return &Worker{q: q, handler: h, log: log, maxAttempts: maxAttempts, backoff: backoff}
}

// Run processes jobs until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	for {
		// Promote any due retries before blocking on the ready list.
		if err := w.q.PromoteDue(ctx); err != nil {
			w.log.Warn("emailq_promote_due", "err", err)
		}
		job, err := w.q.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			if errors.Is(err, ErrNoJob) {
				continue // empty; loop promotes due jobs and blocks again
			}
			w.log.Warn("emailq_dequeue", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if err := w.handler.Send(ctx, job); err != nil {
			w.retry(ctx, job, err)
		}
	}
}

// retry re-enqueues a failed job with backoff, or drops it after maxAttempts.
func (w *Worker) retry(ctx context.Context, job Job, sendErr error) {
	job.Attempts++
	if job.Attempts >= w.maxAttempts {
		w.log.Error("emailq_dropped",
			"type", job.Type, "to", job.To, "attempts", job.Attempts, "err", sendErr)
		return
	}
	delay := w.backoff[job.Attempts-1]
	if err := w.q.EnqueueDelayed(ctx, job, delay); err != nil {
		w.log.Error("emailq_requeue_failed", "type", job.Type, "to", job.To, "err", err)
		return
	}
	w.log.Warn("emailq_retry",
		"type", job.Type, "to", job.To, "attempt", job.Attempts, "delay", delay, "err", sendErr)
}
