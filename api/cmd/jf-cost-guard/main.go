// Command jf-cost-guard refuses a load / performance run while the target
// workspace's AI spam judge is ON.
//
// # WHY THIS EXISTS
//
// A JomForm response-mode submission enqueues an ASYNC AI spam-judge job
// (api/internal/httpapi/public.go, `enqueueSpamJudge`) and the queue worker then
// POSTs to the REAL model endpoint (api/internal/llm). The async design is
// correct for the request path and is exactly why the cost is invisible in a
// load run: it never appears in a latency percentile, only on the bill. One
// measured run of 17,123 submissions spent thousands of paid external model
// calls. This command PROVES the per-workspace toggle
// (`workspace_settings.spam_judge_enabled`, migration 000062) is OFF for the
// workspace a load run is aimed at, through the same store accessors the API
// uses, and fails CLOSED: a state that cannot be read is a refusal, never a
// pass.
//
// USAGE
//
//	jf-cost-guard check   --space <subdomain> | --workspace <uuid> [--database-url <dsn>]
//	jf-cost-guard disable --space <subdomain> | --workspace <uuid> [--database-url <dsn>]
//	jf-cost-guard --help
//
//	check    READ ONLY. Exit 0 only when the toggle READS OFF.
//	disable  Write it OFF and then READ IT BACK, so "the save said OK" is never
//	         mistaken for "the judge is off". Exit 0 only when a FRESH read
//	         reports false.
//
//	--space is the workspace's CURRENT subdomain — the k6 scripts' SPACE knob, so
//	the workspace VERIFIED is the workspace being load-tested. --workspace names
//	the uuid directly. Supplying both is allowed and they must agree.
//
// ENVIRONMENT (a flag wins over its variable)
//
//	SPACE          default for --space
//	WORKSPACE_ID   default for --workspace
//	DATABASE_URL   default for --database-url
//
// EXIT CODES
//
//	0  VERIFIED OFF — the only verdict a load sweep may start on
//	3  REFUSED: the judge is ON (or a disable did not take effect)
//	4  REFUSED: the state could not be VERIFIED (fail closed)
//	1  usage / configuration error (nothing was read or written)
//
// NOTE: this command can turn a PRODUCT FEATURE off. Scope it to a test/staging
// database — never run `disable` against production. See loadtest/BASELINE.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/loadcost"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run returns the process exit code instead of exiting, so every verdict path is
// one function an operator (and the tests) can follow.
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return loadcost.ExitUsage
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:])
	case "disable":
		return runDisable(args[1:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return loadcost.ExitOK
	default:
		fmt.Fprintf(os.Stderr, "jf-cost-guard: unknown command %q (want check|disable)\n", args[0])
		usage(os.Stderr)
		return loadcost.ExitUsage
	}
}

// usage prints the documented contract. It is a literal rather than a grep of
// the source file's header, because the binary is what an operator runs — a
// built binary has no header comment to read.
func usage(w *os.File) {
	fmt.Fprint(w, usageText)
}

func runCheck(args []string) int {
	o, err := parseOpts("check", args)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, tgt, err := resolve(ctx, o)
	if err != nil {
		return fail(err)
	}
	defer pool.Close()

	state, err := loadcost.Check(ctx, dbstore.NewWorkspaceStore(pool), tgt)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("cost-guard: OK — %s has spam_judge_enabled=false on %s.\n", state.Target, loadcost.RedactDSN(o.dsn))
	fmt.Printf("cost-guard: a load run from here enqueues no AI spam-judge job, so it spends no model calls.\n")
	fmt.Printf("cost-guard: this is a READ: nothing was changed.\n")
	return loadcost.ExitOK
}

func runDisable(args []string) int {
	o, err := parseOpts("disable", args)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, tgt, err := resolve(ctx, o)
	if err != nil {
		return fail(err)
	}
	defer pool.Close()

	state, err := loadcost.Disable(ctx, dbstore.NewWorkspaceStore(pool), tgt)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("cost-guard: DONE — %s now reads spam_judge_enabled=false on %s (verified by a FRESH read, "+
		"not by the write's own status).\n", state.Target, loadcost.RedactDSN(o.dsn))
	fmt.Printf("cost-guard: the rest of that workspace's settings were carried through unchanged " +
		"(read-modify-write, not a fresh literal).\n")
	fmt.Printf("cost-guard: remember to turn it back ON for a workspace you do not own / when you are done.\n")
	return loadcost.ExitOK
}

// options is the parsed invocation. Every field can also come from the
// environment (parseOpts seeds the flag defaults), so a load runner can export
// SPACE/DATABASE_URL once instead of threading flags through.
type options struct {
	workspace string
	space     string
	dsn       string
}

func parseOpts(cmd string, args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("jf-cost-guard "+cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&o.workspace, "workspace", os.Getenv("WORKSPACE_ID"),
		"workspace uuid (env WORKSPACE_ID)")
	fs.StringVar(&o.space, "space", os.Getenv("SPACE"),
		"workspace subdomain, i.e. the k6 SPACE knob (env SPACE)")
	fs.StringVar(&o.dsn, "database-url", os.Getenv("DATABASE_URL"),
		"Postgres URL of the TEST/STAGING database to check (env DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("%w: %v", loadcost.ErrUsage, err)
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("%w: unexpected argument %q", loadcost.ErrUsage, fs.Arg(0))
	}
	if strings.TrimSpace(o.space) == "" && strings.TrimSpace(o.workspace) == "" {
		o.space = os.Getenv("SPACE")
	}
	if strings.TrimSpace(o.dsn) == "" {
		return o, fmt.Errorf("%w: --database-url (or DATABASE_URL) is required: the toggle lives in "+
			"workspace_settings, and the REST settings route is session-only, so a load harness has to read it "+
			"from the database", loadcost.ErrUsage)
	}
	return o, nil
}

// resolve connects and resolves the named workspace, in that order: a target
// resolved from a slug needs the connection, and a target named by id is still
// worth resolving so the printed line carries the slug an operator recognises.
func resolve(ctx context.Context, o options) (*pgxpool.Pool, *loadcost.Target, error) {
	pool, err := db.Connect(ctx, o.dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: connect to %s: %v", loadcost.ErrUnverified, loadcost.RedactDSN(o.dsn), err)
	}
	tgt, err := loadcost.Resolve(ctx, dbstore.NewWorkspaceStore(pool), o.workspace, o.space)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return pool, tgt, nil
}

// fail prints the verdict and the next step, and returns the exit code the
// verdict maps to. The "what to do" line is here rather than in the library
// because only the command knows how it was invoked.
func fail(err error) int {
	fmt.Fprintf(os.Stderr, "cost-guard: %v\n", err)
	switch loadcost.ExitCode(err) {
	case loadcost.ExitJudgeOn:
		fmt.Fprintln(os.Stderr, "cost-guard: next — run `jf-cost-guard disable` with the same flags (or turn the "+
			"judge off in the dashboard: Settings -> AI spam judge), then re-run this check. See loadtest/BASELINE.md.")
	case loadcost.ExitUnverified:
		fmt.Fprintln(os.Stderr, "cost-guard: this is a fail-closed refusal: a load run whose cost cannot be "+
			"checked must not start. Fix the database URL / the workspace name above and re-run.")
	}
	return loadcost.ExitCode(err)
}

// usageText is the header comment, kept as a literal so --help works even when
// the source file is not the file that was run (a built binary).
const usageText = `jf-cost-guard — refuse a load/perf run while the AI spam judge is ON.

Every response-mode submission enqueues an async AI spam-judge job that POSTs to
the real model endpoint, so a load run spends paid model calls that never appear
in its latency numbers. This command verifies the per-workspace toggle
(workspace_settings.spam_judge_enabled) is OFF, and fails closed when it cannot.

USAGE
  jf-cost-guard check   --space <subdomain> | --workspace <uuid> [--database-url <dsn>]
  jf-cost-guard disable --space <subdomain> | --workspace <uuid> [--database-url <dsn>]
  jf-cost-guard --help

  check    READ ONLY. Exit 0 only when the toggle READS OFF.
  disable  Write it OFF, then READ IT BACK. Exit 0 only when a FRESH read says false.

ENVIRONMENT (a flag wins)
  SPACE, WORKSPACE_ID, DATABASE_URL

EXIT CODES
  0  VERIFIED OFF — the only verdict a load sweep may start on
  3  REFUSED: the judge is ON (or a disable did not take effect)
  4  REFUSED: the state could not be VERIFIED (fail closed)
  1  usage / configuration error

Never run 'disable' against production: the judge is a product feature.
See loadtest/BASELINE.md.
`
