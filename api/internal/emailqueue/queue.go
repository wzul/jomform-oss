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
// and promotes due jobs back onto the main list.
//
// # Keys are namespaced per queue (NewQueueWithPrefix)
//
// Every one of those keys lives under a prefix (DefaultKeyPrefix for the
// production queue). The prefix is the queue's identity: a caller that builds
// its queue with a prefix of its own gets a key set no other queue writes,
// claims or flushes, even when it shares a Valkey database with a live API. That
// matters because the keys are otherwise identical on every host — a test that
// read the ready list while a live worker was running on the same database
// reported "0 jobs enqueued" for an enqueue that had in fact succeeded, the
// worker having claimed and retried the job. Reach the key set through
// ReadyKey/DelayedKey/InflightKey/DeadKey rather than a literal, so an
// assertion follows the same prefix the queue writes under.
//
// # Delivery guarantee (issue #547)
//
// Jobs are claimed, not popped. A worker atomically MOVEs a job from the ready
// list into an in-flight list (`jf:emailq:inflight`) and only removes it once the
// handler has succeeded. This closes the old failure window: the queue used a
// destructive BLPOP, so a job that had been taken but not yet completed was gone
// from Valkey entirely, and a process stop (deploy, OOM, reboot) lost it silently
// — retries only existed for a handler that RETURNED an error, and a crash returns
// nothing.
//
// A reaper promotes in-flight jobs older than the visibility timeout back onto the
// ready list, so a crashed job is retried rather than lost. A job that exhausts
// maxAttempts is pushed to a dead-letter list (`jf:emailq:dead`) instead of being
// discarded, so a persistently failing job can be inspected and replayed.
//
// Delivery is therefore at-least-once: a job whose handler succeeded but whose ack
// did not land (e.g. a crash in that instant) will run again. Handlers must be
// repeat-safe — email re-sends a duplicate, while asset_scan and spam_judge simply
// recompute a terminal status from the verdict and are naturally idempotent.
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
	// Type selects the dispatcher case. Values enqueued by this repo are:
	// receipt | merchant_notify | form_notification | verify | magic_link |
	// password_reset | email2fa | 2fa_active | invite | connect, plus
	// spamjudge.JobType and assetgate.JobType (those two carry their payload in
	// Body rather than an email). The dispatcher refuses and retries any value
	// it has no case for; email_dispatcher_jobtype_test.go keeps that set and
	// this list in step.
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
	// Lang is the language Subject/Body were RENDERED IN (language
	// consistency). The sender declares it on the branded HTML part, so a
	// queued email declares the same language as its content instead of the
	// Malay it used to claim unconditionally. Empty means "never resolved" and
	// the sender falls back to English.
	Lang string `json:"lang,omitempty"`
	// PDFFilename / PDFData carry an optional receipt PDF attachment.
	PDFFilename string `json:"pdf_filename,omitempty"`
	PDFData     []byte `json:"pdf_data,omitempty"`
	// Attempts counts how many times this job has been tried (for backoff).
	Attempts int `json:"attempts,omitempty"`
	// ClaimedAt records when a worker took the job (unix seconds, issue #547).
	// Zero when the job is not in flight. Used by the reaper to detect a job
	// whose worker died, and cleared on ack so a requeued job looks fresh.
	ClaimedAt int64 `json:"claimed_at,omitempty"`
}

// Queue is a durable Valkey-backed email job queue.
type Queue struct {
	rdb *goredis.Client
	// prefix namespaces every key this queue touches. It is what makes the
	// key set an OWNED resource: a process that builds its queue with its own
	// prefix writes jobs no other queue reads or claims, even when it shares a
	// Valkey database with a live API.
	prefix string
}

// NewQueue wires a queue over the given Valkey client (the same one that backs
// sessions). The client is shared — the queue does not own it.
func NewQueue(rdb *goredis.Client) *Queue {
	return &Queue{rdb: rdb, prefix: DefaultKeyPrefix}
}

// NewQueueWithPrefix wires a queue whose keys are namespaced by prefix instead
// of DefaultKeyPrefix.
//
// The keys were the only isolation axis this queue had, and the production
// literals (`jf:emailq:jobs` and its siblings) are the same on every host, so a
// test that read or flushed them was competing with any live worker on that
// Valkey database: the worker claims the job the assertion is about and the
// test reports 0 enqueued jobs for an enqueue that worked. A per-process prefix
// gives each such caller a key set of its own. prefix must be non-empty and end
// with a separator; an empty prefix falls back to DefaultKeyPrefix.
func NewQueueWithPrefix(rdb *goredis.Client, prefix string) *Queue {
	if prefix == "" {
		prefix = DefaultKeyPrefix
	}
	return &Queue{rdb: rdb, prefix: prefix}
}

const (
	// DefaultKeyPrefix namespaces every key of the production queue. It is the
	// value NewQueue uses; nothing else in the codebase may hardcode the full
	// key literals, since a queue may legitimately run under another prefix.
	DefaultKeyPrefix = "jf:emailq:"
	// keyJobs is the suffix of the main FIFO list of ready jobs.
	keyJobs = "jobs"
	// keyDelayed is the suffix of the sorted set of jobs waiting for a retry,
	// scored by the unix time at which they become due.
	keyDelayed = "delayed"
	// keyInflight is the suffix of the list holding jobs a worker has CLAIMED
	// but not yet acked (issue #547). A job here is still owned by the queue:
	// the reaper returns it to the ready list if its worker never acked.
	keyInflight = "inflight"
	// keyDead is the suffix of the list holding jobs that exhausted their
	// attempts (issue #547), so a permanently failing job can be inspected
	// instead of vanishing.
	keyDead = "dead"
	// dequeueTimeout bounds each blocking move so the worker can periodically
	// promote due delayed jobs and reap abandoned in-flight jobs.
	dequeueTimeout = 2 * time.Second
)

// key returns this queue's name for one of the suffixes above.
func (q *Queue) key(suffix string) string { return q.prefix + suffix }

// ReadyKey is this queue's ready-list key. Exported because a caller that must
// assert on jobs WITHOUT consuming them cannot use Dequeue (it MOVEs the job to
// the in-flight list and blocks for dequeueTimeout); reading the ready list is
// the only non-destructive way, and the key must come from the queue so it
// matches the prefix that queue writes under.
func (q *Queue) ReadyKey() string { return q.key(keyJobs) }

// DelayedKey is this queue's delayed sorted-set key.
func (q *Queue) DelayedKey() string { return q.key(keyDelayed) }

// InflightKey is this queue's in-flight list key.
func (q *Queue) InflightKey() string { return q.key(keyInflight) }

// DeadKey is this queue's dead-letter list key.
func (q *Queue) DeadKey() string { return q.key(keyDead) }

// VisibilityTimeout is how long a claimed job may sit in the in-flight list
// before the reaper assumes its worker died and returns it to the ready list
// (issue #547). It must comfortably exceed a realistic handler duration so a slow
// send is never re-delivered while still running.
const VisibilityTimeout = 5 * time.Minute

// reapInterval is how often the worker checks the in-flight list for abandoned
// claims (issue #547).
const reapInterval = 30 * time.Second

// ErrNoJob is returned by Dequeue when the queue is empty (move timed out).
var ErrNoJob = errors.New("email queue empty")

// Enqueue pushes a job onto the ready list. Durable — survives a restart.
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.rdb.RPush(ctx, q.key(keyJobs), data).Err()
}

// EnqueueDelayed schedules a job for a retry after delay. The job is stored in
// the delayed sorted set and promoted back to the ready list once due.
func (q *Queue) EnqueueDelayed(ctx context.Context, job Job, delay time.Duration) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	when := time.Now().Add(delay).Unix()
	return q.rdb.ZAdd(ctx, q.key(keyDelayed), goredis.Z{Score: float64(when), Member: data}).Err()
}

// Dequeue claims the next ready job, blocking up to dequeueTimeout.
//
// Issue #547: this MOVEs the job to the in-flight list and stamps ClaimedAt,
// rather than popping it off the queue. The job therefore remains recorded until
// Ack removes it, so a process stop mid-handler loses nothing — the reaper returns
// it to the ready list.
//
// Returns ErrNoJob when the queue is empty.
func (q *Queue) Dequeue(ctx context.Context) (Job, error) {
	job, _, err := q.DequeueRaw(ctx)
	return job, err
}

// DequeueRaw claims the next ready job and also returns the RAW stored blob, which
// the caller must hand back to Ack so the removal matches byte-for-byte
// (issue #547). The returned job has ClaimedAt populated.
func (q *Queue) DequeueRaw(ctx context.Context) (Job, string, error) {
	// BLMOVE <src> <dst> RIGHT LEFT <timeout>: atomic, and keeps FIFO order on
	// both lists. Valkey >= 6.2 (prod runs 9.1), the same requirement the rest
	// of the codebase assumes.
	res, err := q.rdb.BLMove(ctx, q.key(keyJobs), q.key(keyInflight), "RIGHT", "LEFT", dequeueTimeout).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return Job{}, "", ErrNoJob
		}
		return Job{}, "", err
	}
	var job Job
	if err := json.Unmarshal([]byte(res), &job); err != nil {
		// An unparseable job cannot be retried meaningfully: drop it from the
		// in-flight list so it does not pin a reaper slot forever, and surface
		// the error so the worker logs it.
		_ = q.rdb.LRem(ctx, q.key(keyInflight), 1, res).Err()
		return Job{}, "", err
	}
	// Stamp the claim so the reaper can tell a live claim from an abandoned one.
	// Replace the in-flight member with the stamped copy; the caller gets the
	// stamped blob back for its ack.
	job.ClaimedAt = time.Now().Unix()
	stamped, merr := json.Marshal(job)
	if merr != nil {
		// Cannot stamp: keep the original in flight so the reaper still finds it
		// (a zero ClaimedAt is treated as stale), and ack against the original.
		return job, res, nil
	}
	// Move atomically enough for a single-worker queue: remove the unstamped
	// member, then push the stamped one. If either step fails the job is still
	// in flight under one of the two forms, so nothing is lost.
	if lerr := q.rdb.LRem(ctx, q.key(keyInflight), 1, res).Err(); lerr != nil {
		slog.Warn("emailq_claim_stamp_failed", "err", lerr)
		return job, res, nil
	}
	if perr := q.rdb.LPush(ctx, q.key(keyInflight), stamped).Err(); perr != nil {
		slog.Warn("emailq_claim_stamp_failed", "err", perr)
		// Put the original back so the job is not stranded.
		_ = q.rdb.LPush(ctx, q.key(keyInflight), res).Err()
		return job, res, nil
	}
	return job, string(stamped), nil
}

// Ack removes a successfully-handled job from the in-flight list (issue #547).
// acked is the json blob exactly as it was claimed, so the removal matches the
// stored member byte-for-byte.
func (q *Queue) Ack(ctx context.Context, raw string) error {
	return q.rdb.LRem(ctx, q.key(keyInflight), 1, raw).Err()
}

// ReapInflight returns in-flight jobs whose worker has not acked within the
// visibility timeout to the ready list (issue #547), so a job claimed by a
// process that then stopped is retried instead of lost.
//
// Returns the number of jobs reaped.
func (q *Queue) ReapInflight(ctx context.Context, timeout time.Duration) (int, error) {
	res, err := q.rdb.LRange(ctx, q.key(keyInflight), 0, -1).Result()
	if err != nil {
		return 0, err
	}
	if len(res) == 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-timeout).Unix()
	reaped := 0
	for _, raw := range res {
		var job Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			// Unparseable in-flight entry: cannot retry it, so drop it rather
			// than reap it forever.
			if err := q.rdb.LRem(ctx, q.key(keyInflight), 1, raw).Err(); err != nil {
				return reaped, err
			}
			continue
		}
		// A zero ClaimedAt means the claim stamp failed; treat it as stale so
		// the job is not stranded in flight forever.
		if job.ClaimedAt != 0 && job.ClaimedAt > cutoff {
			continue // still within its visibility window
		}
		// Return it as a fresh ready job. Attempts is preserved so the backoff
		// ladder still bounds a job that keeps crashing its worker; ClaimedAt is
		// cleared so the next claim stamps it anew.
		job.ClaimedAt = 0
		next, merr := json.Marshal(job)
		if merr != nil {
			continue
		}
		// Claim-then-requeue: move it out of in-flight first so a concurrent
		// reaper cannot requeue the same member twice.
		if err := q.rdb.LRem(ctx, q.key(keyInflight), 1, raw).Err(); err != nil {
			return reaped, err
		}
		if err := q.rdb.RPush(ctx, q.key(keyJobs), next).Err(); err != nil {
			// Put it back in flight rather than dropping it.
			_ = q.rdb.RPush(ctx, q.key(keyInflight), raw).Err()
			return reaped, err
		}
		reaped++
	}
	return reaped, nil
}

// DeadLetter moves a job that exhausted its attempts to the dead list
// (issue #547), so a permanently failing job can be inspected and replayed
// instead of being discarded.
func (q *Queue) DeadLetter(ctx context.Context, job Job, reason string) error {
	rec := map[string]any{
		"job":      job,
		"reason":   reason,
		"died_at":  time.Now().Unix(),
		"attempts": job.Attempts,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return q.rdb.RPush(ctx, q.key(keyDead), data).Err()
}

// DeadLen reports the dead-letter list length (for tests/ops).
func (q *Queue) DeadLen(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.key(keyDead)).Result()
}

// InflightLen reports the in-flight list length (for tests/ops).
func (q *Queue) InflightLen(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.key(keyInflight)).Result()
}

// PromoteDue moves every delayed job whose due time has passed onto the ready
// list. Called by the worker each loop so retries are picked up.
//
// Issue #547: the ZREM happens BEFORE the RPush for each member, so with more than
// one worker a member can only be promoted once — previously the read-then-move
// pair could promote the same member from two workers concurrently.
func (q *Queue) PromoteDue(ctx context.Context) error {
	now := time.Now().Unix()
	res, err := q.rdb.ZRangeByScore(ctx, q.key(keyDelayed), &goredis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now, 10),
	}).Result()
	if err != nil {
		return err
	}
	for _, m := range res {
		// Claim the member first: ZRem returns 1 only for the worker that
		// actually removed it, so only that worker pushes it to the ready list.
		removed, rerr := q.rdb.ZRem(ctx, q.key(keyDelayed), m).Result()
		if rerr != nil {
			return rerr
		}
		if removed == 0 {
			continue // another worker got it
		}
		if perr := q.rdb.RPush(ctx, q.key(keyJobs), m).Err(); perr != nil {
			// Re-add to delayed so the retry is not lost.
			_ = q.rdb.ZAdd(ctx, q.key(keyDelayed), goredis.Z{Score: float64(time.Now().Unix()), Member: m}).Err()
			return perr
		}
	}
	return nil
}

// Len reports the number of ready jobs (for tests/ops).
func (q *Queue) Len(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.key(keyJobs)).Result()
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
	// Reap abandoned in-flight jobs once at start, so a job whose worker died
	// before this process came up is retried promptly rather than waiting for
	// the reaper interval (issue #547).
	if n, err := w.q.ReapInflight(ctx, VisibilityTimeout); err != nil {
		w.log.Warn("emailq_reap_inflight", "err", err)
	} else if n > 0 {
		w.log.Warn("emailq_reaped_inflight", "jobs", n)
	}
	lastReap := time.Now()
	for {
		// Promote any due retries before blocking on the ready list.
		if err := w.q.PromoteDue(ctx); err != nil {
			w.log.Warn("emailq_promote_due", "err", err)
		}
		// Periodically reap jobs whose worker never acked (issue #547).
		if time.Since(lastReap) >= reapInterval {
			if n, err := w.q.ReapInflight(ctx, VisibilityTimeout); err != nil {
				w.log.Warn("emailq_reap_inflight", "err", err)
			} else if n > 0 {
				w.log.Warn("emailq_reaped_inflight", "jobs", n)
			}
			lastReap = time.Now()
		}
		// Claim the next job. Dequeue returns both the parsed job and the raw
		// blob, because the ack must match the stored member byte-for-byte.
		job, raw, err := w.q.DequeueRaw(ctx)
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
			w.retry(ctx, job, raw, err)
			continue
		}
		// Success: ack the claimed job out of the in-flight list. If this fails
		// the job stays in flight and the reaper will retry it later — a
		// duplicate run, never a loss (issue #547).
		if err := w.q.Ack(ctx, raw); err != nil {
			w.log.Warn("emailq_ack_failed", "type", job.Type, "to", job.To, "err", err)
		}
	}
}

// retry re-enqueues a failed job with backoff, or dead-letters it after
// maxAttempts (issue #547: previously it was dropped and only logged, which made
// a permanently failing job disappear).
func (w *Worker) retry(ctx context.Context, job Job, raw string, sendErr error) {
	// Release the claim first: this job is being scheduled again (or retired), so
	// it must not also be reaped.
	if err := w.q.Ack(ctx, raw); err != nil {
		w.log.Warn("emailq_ack_failed", "type", job.Type, "to", job.To, "err", err)
	}
	job.Attempts++
	job.ClaimedAt = 0
	if job.Attempts >= w.maxAttempts {
		if err := w.q.DeadLetter(ctx, job, sendErr.Error()); err != nil {
			w.log.Error("emailq_deadletter_failed",
				"type", job.Type, "to", job.To, "attempts", job.Attempts, "err", err)
			return
		}
		w.log.Error("emailq_deadlettered",
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
