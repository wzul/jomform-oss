// Package dormancy — phase 1 of the approved three-phase dormancy policy
// (card t_94e5cca3).
//
// # What phase 1 is, and what it deliberately is NOT
//
// Phase 1 is a REPORT and a single reminder email. It deletes nothing, unpublishes
// nothing, and writes no flag, queue row or state that a later job could act on.
// The approval for phases 2 (unpublish after a long silence) and 3 (narrow, graced
// deletion) is for separate cards; nothing in this package may be read as
// authorising either.
//
// # Why a dormant merchant is not a dormant form
//
// Measured on production 2026-11-10: of 3,151 events, 761 (24%) carry a NULL
// user_id. An account that has not signed in for a month can still own a
// PUBLISHED form that buyers are submitting to; deleting or unpublishing it
// because the *merchant* is quiet would 404 a live checkout and destroy buyer
// records. So the activity measure is ACCOUNT-scoped (events by account_id,
// forms.updated_at, sales, submissions, subscriptions) and an account is only a
// CANDIDATE when it also has no sale, no subscription, and no submission on any
// of its forms in the last 90 days.
//
// # The exclusion list is the safety property, not a nicety
//
// Every account in the report lands either in a dormancy tier or in the EXCLUDED
// list carrying the reason(s). "Excluded" is not a hidden state: the admin table
// and the JSON/CSV export print the row and its reasons, so an operator can see
// that a form with a live buyer flow was considered and refused rather than never
// looked at.
package dormancy

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// Bucket is the dormancy tier of one account.
type Bucket string

const (
	// BucketActive is "less than 30 days quiet" — not dormant at all.
	BucketActive Bucket = "active"
	// Bucket30/60/90/365 are the card's tiers. Each is a LOWER bound: a 400-day
	// silence reports Bucket365, not Bucket90, so the tiers partition the range
	// instead of overlapping.
	Bucket30  Bucket = "30"
	Bucket60  Bucket = "60"
	Bucket90  Bucket = "90"
	Bucket365 Bucket = "365"
	// BucketExcluded is an account that HAS a dormancy age but may not be treated
	// as a candidate (see Excluded).
	BucketExcluded Bucket = "excluded"
)

// TierBuckets is the display order of the candidate tiers.
var TierBuckets = []Bucket{Bucket30, Bucket60, Bucket90, Bucket365}

// Exclusion reasons. They are stable machine codes (the /admin table and the CSV
// print them verbatim, like the suppression list's reason codes), and the MCP/JSON
// consumers match on them.
const (
	// ReasonSystemAccount — the template gallery owner
	// (dbstore.LegacyProvisionedEmails) or any users.is_admin account. The card
	// names the system account explicitly: it must never be in scope.
	ReasonSystemAccount = "system_account"
	// ReasonHasSales — the account has at least one sale, ever. A merchant who has
	// been paid is not dormant, and sales are never destroyed.
	ReasonHasSales = "has_sales"
	// ReasonHasSubscription — any subscription row, ever. A recurring customer is
	// still being charged.
	ReasonHasSubscription = "has_subscription"
	// ReasonRecentSubmissions — a submission on one of the account's forms inside
	// the window (90 days). This is THE guard the card asks for: a dormant merchant
	// with a live buyer flow.
	ReasonRecentSubmissions = "recent_submissions"
	// ReasonWorkspaceInTrash — the workspace is in the bin, so its forms are not
	// served; "your form is still live" would be false.
	ReasonWorkspaceInTrash = "workspace_in_trash"
	// ReasonNoOwnerEmail — the owner row has an empty address (production has one
	// such verified user), so a candidate tier would promise a nudge that cannot be
	// delivered.
	ReasonNoOwnerEmail = "no_owner_email"
)

// Reasons is the full, ordered set of exclusion codes, so a UI legend and a test
// cannot drift from what Build can emit.
var Reasons = []string{
	ReasonSystemAccount, ReasonHasSales, ReasonHasSubscription,
	ReasonRecentSubmissions, ReasonWorkspaceInTrash, ReasonNoOwnerEmail,
}

// Options bounds one report build.
type Options struct {
	// Now is the reference instant. Zero means time.Now().
	Now time.Time
	// Limit is the row cap the STORE applied; the report reports Truncated when it
	// received exactly this many rows.
	Limit int
}

// Row is one account as the report presents it: the store's facts plus the
// policy verdict.
type Row struct {
	AccountID        string    `json:"account_id"`
	WorkspaceSlug    string    `json:"workspace_slug"`
	OwnerEmail       string    `json:"owner_email"`
	CreatedAt        time.Time `json:"created_at"`
	LastActivity     time.Time `json:"last_activity"`
	DormantDays      int       `json:"dormant_days"`
	AgeBucket        Bucket    `json:"age_bucket"`
	Bucket           Bucket    `json:"bucket"`
	Excluded         bool      `json:"excluded"`
	ExclusionReasons []string  `json:"exclusion_reasons,omitempty"`
	FormsPublished   int64     `json:"forms_published"`
	Submissions90d   int64     `json:"submissions_90d"`
	SalesEver        int64     `json:"sales_ever"`
	LastNudgeAt      *string   `json:"last_nudge_at,omitempty"`
	// NudgeEligible says this row WILL be (or was) emailed by the daily run, and
	// NudgeSkipReason says why it will not. Both are computed here rather than in
	// the sender so the admin table can show the operator the same decision the
	// job makes.
	NudgeEligible   bool   `json:"nudge_eligible"`
	NudgeSkipReason string `json:"nudge_skip_reason,omitempty"`

	// OwnerLang is the language the nudge is rendered in. Not exported in JSON: it
	// is an input to rendering, not a report fact.
	OwnerLang string `json:"-"`
	// OwnerUserID is the users.id the nudge audit row is attributed to.
	OwnerUserID string `json:"-"`
}

// Report is one consistent snapshot.
type Report struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Now         time.Time      `json:"now"`
	Rows        []Row          `json:"rows"`
	Counts      map[Bucket]int `json:"counts"`
	Candidates  int            `json:"candidates"`
	Excluded    int            `json:"excluded"`
	Truncated   bool           `json:"truncated"`
	Limit       int            `json:"limit"`
	Nudgeable   int            `json:"nudgeable"`
	// Skipped is how many rows the caller asked to skip (store-side truncation
	// makes the two numbers differ only through the limit).
	Skipped int `json:"skipped"`
}

// Nudge rate limit: the card allows one nudge per account and never a repeat
// inside 90 days. Read from the events log by the store (DormancyRow.LastNudgeAt).
const NudgeRepeatWindow = 90 * 24 * time.Hour

// NudgeMinDormantDays is the first tier that is emailed. The card says "owners in
// the 60/90-day buckets"; 365 is included deliberately — an account dormant for two
// years has passed through the 60- and 90-day tiers and must not be permanently
// unreachable by the one reminder the policy allows. It is a LOWER-bound tier, so
// "60/90" reads as "at least 60 days".
const NudgeMinDormantDays = 60

// AgeDays is the whole-day dormancy age at `now`, floored at 0.
func AgeDays(last, now time.Time) int {
	d := now.Sub(last)
	if d <= 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}

// bucketFor maps an age onto its tier. Lower bounds, so the tiers partition.
func bucketFor(days int) Bucket {
	switch {
	case days >= 365:
		return Bucket365
	case days >= 90:
		return Bucket90
	case days >= 60:
		return Bucket60
	case days >= 30:
		return Bucket30
	default:
		return BucketActive
	}
}

// exclusionReasons reads the verdict from the facts. It is the single place the
// policy decides "may this account be called a dormancy candidate", so the test
// that removes one check removes it HERE and the change is visible.
func exclusionReasons(r dbstore.DormancyRow) []string {
	var out []string
	if isSystemAccount(r) {
		out = append(out, ReasonSystemAccount)
	}
	if r.SalesEver > 0 {
		out = append(out, ReasonHasSales)
	}
	if r.SubscriptionsEver > 0 {
		out = append(out, ReasonHasSubscription)
	}
	if r.Submissions90d > 0 {
		out = append(out, ReasonRecentSubmissions)
	}
	if r.WorkspaceTrashed {
		out = append(out, ReasonWorkspaceInTrash)
	}
	if strings.TrimSpace(r.OwnerEmail) == "" {
		out = append(out, ReasonNoOwnerEmail)
	}
	return out
}

func isSystemAccount(r dbstore.DormancyRow) bool {
	if r.OwnerIsAdmin {
		return true
	}
	email := strings.ToLower(strings.TrimSpace(r.OwnerEmail))
	for _, e := range dbstore.LegacyProvisionedEmails {
		if strings.ToLower(strings.TrimSpace(e)) == email {
			return true
		}
	}
	return false
}

// Build turns the store's facts into the report: the tier, the exclusion verdict
// and the nudge decision for every row. Pure — no clock, no DB, no email.
func Build(rows []dbstore.DormancyRow, opts Options) Report {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = dbstore.DormancyReportLimitDefault
	}
	rep := Report{
		GeneratedAt: now,
		Now:         now,
		Rows:        make([]Row, 0, len(rows)),
		Counts:      map[Bucket]int{},
		Limit:       limit,
	}
	for _, src := range rows {
		days := AgeDays(src.LastActivity, now)
		ageBucket := bucketFor(days)
		reasons := exclusionReasons(src)
		excluded := len(reasons) > 0

		row := Row{
			AccountID:        src.AccountID,
			WorkspaceSlug:    src.WorkspaceSlug,
			OwnerEmail:       src.OwnerEmail,
			CreatedAt:        src.AccountCreatedAt,
			LastActivity:     src.LastActivity,
			DormantDays:      days,
			AgeBucket:        ageBucket,
			Bucket:           ageBucket,
			Excluded:         excluded,
			ExclusionReasons: reasons,
			FormsPublished:   src.FormsPublished,
			Submissions90d:   src.Submissions90d,
			SalesEver:        src.SalesEver,
			OwnerLang:        src.OwnerLang,
			OwnerUserID:      src.OwnerUserID,
		}
		if src.LastNudgeAt != nil {
			s := src.LastNudgeAt.UTC().Format(time.RFC3339)
			row.LastNudgeAt = &s
		}
		if excluded {
			row.Bucket = BucketExcluded
			rep.Excluded++
		} else {
			rep.Candidates++
		}
		row.NudgeEligible, row.NudgeSkipReason = nudgeDecision(row, src, now)
		if row.NudgeEligible {
			rep.Nudgeable++
		}
		rep.Counts[row.Bucket]++
		rep.Rows = append(rep.Rows, row)
	}
	rep.Truncated = len(rows) >= limit
	return rep
}

// nudgeDecision is the single place the "who gets emailed" rule lives. The three
// guarantees the card names are all here: never the system account (it is
// excluded), never the 30-day tier (below NudgeMinDormantDays), never twice inside
// the repeat window.
func nudgeDecision(row Row, src dbstore.DormancyRow, now time.Time) (bool, string) {
	if row.Excluded {
		return false, "excluded"
	}
	if row.DormantDays < NudgeMinDormantDays {
		return false, "not_dormant_enough"
	}
	if src.LastNudgeAt != nil && now.Sub(*src.LastNudgeAt) < NudgeRepeatWindow {
		return false, "nudged_within_90_days"
	}
	return true, ""
}

// Mailer is the send seam — satisfied by *email.Sender, whose Send already
// consults the suppression list and returns email.ErrSuppressed for a blocked
// address.
type Mailer interface {
	Send(to, subject, body, lang string) error
}

// NudgeRecorder reserves the rate-limit slot for one account and returns the error
// of the reservation. It is satisfied by *dbstore.DormancyStore.
//
// RESERVATION, NOT CONFIRMATION. The runner records BEFORE it sends, and refuses to
// send when the record fails. A send whose audit row was lost would be repeated
// tomorrow, and "never twice inside 90 days" is the promise that must hold; the
// price is that a failed SES call burns that account's slot for 90 days, which is
// the safe direction (no mail, rather than mail twice).
type NudgeRecorder interface {
	RecordDormancyNudge(ctx context.Context, accountID, userID, bucket, email string) error
}

// ReportSource is satisfied by *dbstore.DormancyStore.
type ReportSource interface {
	DormancyReport(ctx context.Context, opts dbstore.DormancyReportOptions) ([]dbstore.DormancyRow, error)
}

// Runner runs the daily job: build the report, then send the nudge to the accounts
// the policy allows. Every collaborator is a seam so the tests never need a live
// SES or a live database.
type Runner struct {
	Source     ReportSource
	Recorder   NudgeRecorder
	Mailer     Mailer
	Log        *slog.Logger
	Limit      int
	WindowDays int
	Now        func() time.Time
	BaseURL    string
}

// RunResult is what one daily run did.
type RunResult struct {
	Report  Report
	Sent    []NudgeResult
	Skipped []NudgeResult
}

// NudgeResult names one account's nudge outcome. `Err` is set when the reservation
// or the send failed; the run continues with the next account.
type NudgeResult struct {
	AccountID     string `json:"account_id"`
	WorkspaceSlug string `json:"workspace_slug"`
	OwnerEmail    string `json:"owner_email"`
	Bucket        Bucket `json:"bucket"`
	DormantDays   int    `json:"dormant_days"`
	Lang          string `json:"lang"`
	Err           string `json:"error,omitempty"`
}

// EmailKind is the dictionary entry the nudge is rendered from. Declared as a
// value, not a literal subject/body, so the language guard sees this package
// sending dictionary copy rather than hand-written mail.
const EmailKind = emailtext.DormancyNudge

// Run executes one daily pass. It never deletes, unpublishes or flags anything: the
// only write is the nudge audit row in the existing events log.
func (r *Runner) Run(ctx context.Context) (RunResult, error) {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	if r.Source == nil {
		return RunResult{}, errors.New("dormancy: no report source configured")
	}
	limit := r.Limit
	if limit <= 0 {
		limit = dbstore.DormancyReportLimitDefault
	}
	rows, err := r.Source.DormancyReport(ctx, dbstore.DormancyReportOptions{
		Limit:                 limit,
		Now:                   now,
		SubmissionsWindowDays: r.WindowDays,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("dormancy report: %w", err)
	}
	rep := Build(rows, Options{Now: now, Limit: limit})
	out := RunResult{Report: rep}

	for _, row := range rep.Rows {
		if !row.NudgeEligible {
			continue
		}
		res := NudgeResult{
			AccountID:     row.AccountID,
			WorkspaceSlug: row.WorkspaceSlug,
			OwnerEmail:    row.OwnerEmail,
			Bucket:        row.Bucket,
			DormantDays:   row.DormantDays,
			Lang:          emailtext.NormalizeLang(row.OwnerLang),
		}
		if r.Recorder == nil {
			res.Err = "no nudge recorder configured"
			out.Skipped = append(out.Skipped, res)
			continue
		}
		// A deployment without SES (a dev box, a probe) must NOT burn the account's
		// 90-day slot: nothing can be sent, so nothing is reserved either. This
		// check sits BEFORE the reservation for exactly that reason.
		if r.Mailer == nil {
			res.Err = "no mailer configured"
			out.Skipped = append(out.Skipped, res)
			r.logf("dormancy_nudge_no_mailer", res)
			continue
		}
		// Reserve the slot first (see NudgeRecorder): no send without a recorded
		// reservation, so a lost write can never turn into a repeat email.
		if err := r.Recorder.RecordDormancyNudge(ctx, row.AccountID, row.OwnerUserID, string(row.Bucket), row.OwnerEmail); err != nil {
			res.Err = "reserve: " + err.Error()
			out.Skipped = append(out.Skipped, res)
			r.logf("dormancy_nudge_reserve_failed", res)
			continue
		}
		msg := emailtext.Text(row.OwnerLang, EmailKind, nudgeVars(row))
		if err := r.Mailer.Send(row.OwnerEmail, msg.Subject, msg.Body, msg.Lang); err != nil {
			res.Err = err.Error()
			out.Skipped = append(out.Skipped, res)
			r.logf("dormancy_nudge_send_failed", res)
			continue
		}
		out.Sent = append(out.Sent, res)
	}
	return out, nil
}

// nudgeVars fills the dictionary body's {workspace}/{days} tokens. A legacy
// workspace-less account names the product rather than an empty set of quotes.
func nudgeVars(row Row) map[string]string {
	ws := row.WorkspaceSlug
	if ws == "" {
		ws = "JomForm"
	}
	return map[string]string{
		"workspace": ws,
		"days":      strconv.Itoa(row.DormantDays),
	}
}

func (r *Runner) logf(event string, res NudgeResult) {
	if r.Log == nil {
		return
	}
	r.Log.Warn(event, "account", res.AccountID, "bucket", string(res.Bucket), "err", res.Err)
}

// CSV renders the report as RFC 4180 rows: a header plus one line per account, in
// the same order as the table. It exists so the operator export and the JSON view
// come from ONE model (the card asks for "a JSON/CSV line per account").
func CSV(rep Report) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write([]string{
		"account_id", "workspace_slug", "owner_email", "created_at", "last_activity",
		"dormant_days", "bucket", "excluded", "exclusion_reasons",
		"forms_published", "submissions_90d", "sales_ever", "last_nudge_at",
		"nudge_eligible", "nudge_skip_reason",
	}); err != nil {
		return "", err
	}
	for _, row := range rep.Rows {
		if err := w.Write([]string{
			row.AccountID,
			row.WorkspaceSlug,
			row.OwnerEmail,
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.LastActivity.UTC().Format(time.RFC3339),
			strconv.Itoa(row.DormantDays),
			string(row.Bucket),
			strconv.FormatBool(row.Excluded),
			strings.Join(row.ExclusionReasons, "|"),
			strconv.FormatInt(row.FormsPublished, 10),
			strconv.FormatInt(row.Submissions90d, 10),
			strconv.FormatInt(row.SalesEver, 10),
			derefString(row.LastNudgeAt),
			strconv.FormatBool(row.NudgeEligible),
			row.NudgeSkipReason,
		}); err != nil {
			return "", err
		}
	}
	w.Flush()
	return b.String(), w.Error()
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
