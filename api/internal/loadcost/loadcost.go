// Package loadcost is the COST SAFETY interlock for load / performance testing.
//
// THE DEFECT THIS EXISTS TO PREVENT (measured 2026-10-04/05, card t_bf70cb09)
//
// Every response-mode submission runs `PublicHandlers.enqueueSpamJudge`
// (api/internal/httpapi/public.go — called from `handleSubmit` and from the
// formless menu-order path), and the queue worker (`spamjudge.Handler.Send`)
// then POSTs to the REAL model endpoint through `llm.Client`
// (api/internal/llm — base URL `LLM_BASE_URL`, default https://ollama.com/v1,
// credential `OLLAMA_API_KEY`). The job is ASYNC, which is right for the
// request path and is exactly why this cost is invisible: it never appears in a
// latency percentile, only on the bill. One 17,123-submission load run spent
// thousands of paid external model calls, observed as
//
//	WARN spamjudge_judge_failed err="llm: Post \"https://ollama.com/v1/chat/completions\": ..."
//	INFO spamjudge_done ...            (hundreds of them, back to back)
//
// The lever is the per-workspace toggle `workspace_settings.spam_judge_enabled`
// (migration 000062, `NOT NULL DEFAULT TRUE`). It is read on the ENQUEUE —
// `WorkspaceStore.SpamJudgeEnabledByAccount`, `SELECT COALESCE(ss.spam_judge_enabled,
// TRUE)` — so with it OFF no judge job is enqueued at all and no AI work is spent.
//
// WHY THIS IS GO AND NOT A HAND-WRITTEN SQL SNIPPET IN THE RUNBOOK
//
//   - It reads and writes through the SAME store accessors the API uses
//     (`WorkspaceStore.Settings` / `SaveSettings`), so the "a workspace whose
//     settings row does not exist yet reads ON" default and the
//     read-modify-write that preserves every other column cannot drift from the
//     application. A hand-written INSERT/UPDATE in a doc can, and the card's own
//     thread already contains a wrong one (it claimed the other columns are
//     NOT NULL without defaults — they all have defaults;
//     api/internal/db/migrations/000012_spaces.up.sql, and the app itself relies
//     on them at workspace.go's `INSERT INTO workspace_settings (workspace_id)`).
//   - The REST settings route is NOT usable from a load harness: it is
//     session-only (`RESTHandlers.workspaceAccessByID` requires a session user,
//     so a Bearer API key gets 401). A DB-level check is the one a load runner
//     can actually run.
//
// WHAT IT DOES NOT DO: it never disables the judge in production on its own
// (that is a per-invocation decision), and it does not touch any workspace other
// than the one it is given.
package loadcost

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Exit codes. They are a CONTRACT, not decoration: `loadtest/baseline.sh`
// refuses the sweep on any non-zero code, so a run can never start on an
// unverified verdict, and an operator reads the code to tell "turn the judge
// off" (3) apart from "the state could not be read" (4).
const (
	// ExitOK — VERIFIED OFF. The only verdict a load sweep may start on.
	ExitOK = 0
	// ExitUsage — the command was misconfigured (nothing was read or written).
	ExitUsage = 1
	// ExitJudgeOn — the judge is ON: this run would spend real model calls. Also
	// returned when a `disable` reported success but a fresh read disagreed.
	ExitJudgeOn = 3
	// ExitUnverified — the state could not be established (unreachable DB,
	// unknown slug, missing table). Fail closed: never reported as safe.
	ExitUnverified = 4
)

// Sentinel verdicts. Each maps to an exit code (ExitCode) and each is wrapped
// with the detail an operator needs, so the command can print the error as-is.
var (
	// ErrUsage is a misconfigured invocation.
	ErrUsage = errors.New("jf-cost-guard: usage")
	// ErrJudgeOn means the toggle reads ON for the workspace under test.
	ErrJudgeOn = errors.New("REFUSING: the AI spam judge is ON")
	// ErrNotApplied means the update returned no error but a FRESH read still
	// reports the judge ON. "The save said OK" is not "the judge is off".
	ErrNotApplied = errors.New("REFUSING: the judge is STILL ON after the update")
	// ErrUnverified means the state could not be read at all. Fail closed.
	ErrUnverified = errors.New("REFUSING: could not verify the judge state")
)

// Target is the workspace a load run is aimed at.
//
// It is resolved from EITHER an id or a subdomain slug, because the two ends of
// a load run know different things: the k6 scripts know the subdomain (`SPACE`),
// an operator reading the DB knows the uuid. Slug is best-effort (empty when the
// target was named by id).
type Target struct {
	ID   string
	Slug string
}

// String renders the target the way an operator names it.
func (t *Target) String() string {
	if t == nil {
		return "(no target)"
	}
	if t.Slug == "" {
		return "workspace " + t.ID
	}
	return fmt.Sprintf("workspace %s (%s)", t.ID, t.Slug)
}

// State is one workspace's judge toggle, as READ BACK.
type State struct {
	Target  *Target
	Enabled bool
}

// slugRe is the subdomain shape a workspace slug may take. It is checked before
// the lookup so a typo fails as a usage error rather than as "no workspace found",
// and so nothing unexpected reaches the store.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Resolve turns the (id, slug) pair an operator supplied into a Target.
//
// EXACTLY ONE naming is required, and when both are supplied they must AGREE:
// "verify the workspace I named by id while the k6 run hammers the one named by
// slug" would be a green check that proves nothing, so a disagreement is a
// usage error rather than a silent preference.
func Resolve(ctx context.Context, ws *dbstore.WorkspaceStore, workspaceID, space string) (*Target, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	space = strings.TrimSpace(space)
	if workspaceID == "" && space == "" {
		return nil, fmt.Errorf("%w: name the workspace with --workspace <uuid> or --space <subdomain> "+
			"(the load scripts' SPACE knob is the subdomain)", ErrUsage)
	}
	if workspaceID != "" {
		if _, err := uuid.Parse(workspaceID); err != nil {
			return nil, fmt.Errorf("%w: --workspace %q is not a uuid", ErrUsage, workspaceID)
		}
	}
	if space != "" && !slugRe.MatchString(space) {
		return nil, fmt.Errorf("%w: --space %q is not a subdomain slug ([a-z0-9-], 1-63 chars, "+
			"lowercase — the k6 SPACE knob)", ErrUsage, space)
	}
	if space == "" {
		return &Target{ID: workspaceID}, nil
	}
	w, err := ws.BySlug(ctx, space)
	if err != nil {
		if isNoRows(err) {
			return nil, fmt.Errorf("%w: no workspace has the slug %q (the slug is the workspace's CURRENT "+
				"subdomain; a renamed workspace is named by its current slug, or by --workspace)", ErrUnverified, space)
		}
		return nil, fmt.Errorf("%w: read workspace by slug %q: %v", ErrUnverified, space, err)
	}
	if w == nil {
		return nil, fmt.Errorf("%w: no workspace has the slug %q", ErrUnverified, space)
	}
	if workspaceID != "" && w.ID != workspaceID {
		return nil, fmt.Errorf("%w: --workspace %s and --space %s name DIFFERENT workspaces (%s vs %s) — "+
			"refusing, because verifying one while load-testing the other proves nothing",
			ErrUsage, workspaceID, space, workspaceID, w.ID)
	}
	return &Target{ID: w.ID, Slug: w.Slug}, nil
}

// Check reads the toggle for one target through the store accessor the API's
// settings read uses (`WorkspaceStore.Settings`).
//
// It returns ErrJudgeOn when the judge is ON — including the case the migration
// and the store's fallback literal both define: a workspace whose settings row
// does not exist yet reads ON. Anything that prevents the read is ErrUnverified,
// never a pass.
func Check(ctx context.Context, ws *dbstore.WorkspaceStore, t *Target) (*State, error) {
	st, err := ws.Settings(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: read settings for %s: %v", ErrUnverified, t, err)
	}
	if st.SpamJudgeEnabled {
		return &State{Target: t, Enabled: true}, judgeOnError(t)
	}
	return &State{Target: t, Enabled: false}, nil
}

// Disable writes the toggle OFF for one target and then READS IT BACK.
//
// THE READ-BACK IS THE POINT. A write that returns no error is evidence that the
// statement was accepted, not that the row now says false — a trigger, a
// permission default, a stale connection pool or a wrong table would all leave
// the judge ON while a "saved" message was printed, and the load run would spend
// the money anyway. So the returned State is the verdict of a FRESH read, and a
// disagreement is ErrNotApplied (exit 3: the state is still ON).
//
// The write goes through the store's own read-modify-write: `SaveSettings`
// writes EVERY column from the struct it is handed, so a caller that built a
// fresh literal instead of carrying the current settings forward would wipe the
// merchant's storefront config (the hazard
// api/internal/httpapi/spam_judge_toggle_test.go pins for the REST path).
func Disable(ctx context.Context, ws *dbstore.WorkspaceStore, t *Target) (*State, error) {
	cur, err := ws.Settings(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: read settings for %s: %v", ErrUnverified, t, err)
	}
	cur.SpamJudgeEnabled = false
	if err := ws.SaveSettings(ctx, cur); err != nil {
		return nil, fmt.Errorf("%w: write spam_judge_enabled=false for %s: %v", ErrUnverified, t, err)
	}
	fresh, err := ws.Settings(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s BACK after the update: %v", ErrUnverified, t, err)
	}
	if fresh.SpamJudgeEnabled {
		return &State{Target: t, Enabled: true}, fmt.Errorf("%w: the update for %s was accepted but a FRESH "+
			"read still reports spam_judge_enabled = true — the write did not take effect, so a load run from "+
			"here would still enqueue paid AI judge jobs", ErrNotApplied, t)
	}
	return &State{Target: t, Enabled: false}, nil
}

// judgeOnError builds the refusal. It names the workspace and the cost, because
// the whole failure mode is that this cost is invisible in the numbers a load
// run reports.
func judgeOnError(t *Target) error {
	return fmt.Errorf("%w: %s has spam_judge_enabled = true, so EVERY submission the load run makes "+
		"enqueues an AI spam-judge job that POSTs to the real model endpoint (async — the cost never shows "+
		"up in the response time, only on the bill). A workspace whose settings row does not exist yet reads "+
		"ON too: that is the app's own default. Turn it off first with `jf-cost-guard disable` (same flags), "+
		"or in the dashboard (Settings -> AI spam judge). See loadtest/BASELINE.md", ErrJudgeOn, t)
}

// ExitCode maps a verdict onto the process exit code the load runner reads.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrUsage):
		return ExitUsage
	case errors.Is(err, ErrJudgeOn), errors.Is(err, ErrNotApplied):
		return ExitJudgeOn
	default:
		return ExitUnverified
	}
}

// RedactDSN renders a connection string as scheme://host:port/db, dropping the
// credentials. Output that names WHERE a check ran is useful; output carrying a
// database password into a load-run log is not.
func RedactDSN(dsn string) string {
	u, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil || u.Host == "" {
		return "(connection string with no host)"
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "postgres"
	}
	return scheme + "://" + u.Host + u.Path
}

// isNoRows reports the driver's "no such row" for a by-key read. The store maps
// a missing row to pgx.ErrNoRows (scanWorkspace), and a wrapper that keeps the
// text is still a not-found rather than a connection failure — the two must not
// be reported as the same thing, because one means "you named the wrong
// workspace" and the other means "the check could not run".
func isNoRows(err error) bool {
	return err != nil && (errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "no rows in result set"))
}
