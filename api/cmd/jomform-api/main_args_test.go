package main

// argv handling for the server binary (card t_85cddf3e).
//
// WHY THIS EXISTS. `func main` parsed no arguments at all — no flag.Parse, no
// read of os.Args anywhere in the binary — so `jomform-api --version`, the probe
// every operator reaches for, silently booted the FULL server instead: config,
// Postgres plus the migration pass, valkey, the email worker and the HTTP
// listener, in that order, with the port bind LAST and therefore the only step a
// second instance can fail on. Measured inside the live prod container on
// 2026-10-12: the probe logged `shared_extensions_ready` and
// `email_worker_started`. The impact was nil that time only because the email
// queue happened to be empty; two workers on a non-empty queue race for the
// same job, and the probe holds a second set of DB/valkey connections.
//
// WHAT THIS PINS.
//
//  1. handleArgs answers --version/-v and --help/-h with exit 0, answers
//     nothing else: empty argv means "boot the server", and an unknown argument
//     is a usage error (exit 2) rather than a server start.
//
//  2. The REAL binary, pointed at a Postgres DSN that cannot possibly work,
//     still prints the version and exits 0 — and a listener standing in for
//     Postgres counts ZERO connection attempts across every informational argv
//     path, so "startup did not run" is measured rather than inferred from a
//     missing log line.
//
//  3. That zero is shown to be a measurement and not a blind instrument: the
//     SAME unreachable env with no argv boots, reaches the database, fails
//     there (non-zero), and the same counter reads >= 1.
//
// NOT MEASURED HERE, stated because the card names the valkey URL too. The
// valkey half of this path cannot be instrumented the same way: auth.NewSessions
// only PARSES the URL and calls goredis.NewClient, whose connection is lazy, so
// a full boot dials no valkey at all — a valkey stand-in would read 0 on a boot
// AND on a probe and discriminate nothing. The Postgres dial is the instrument
// with a driven control; valkey gets no claim from this file.
//
// SCOPE. This pins argv handling and the fact that no startup work happens on
// the informational paths. It does not run the server and does not re-prove what
// startup does with an empty argv beyond the control arm below.

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/version"
)

// TestHandleArgsAnswersOnlyTheInformationalFlags is the fast, in-process half:
// what each argv shape decides, and where it writes.
func TestHandleArgsAnswersOnlyTheInformationalFlags(t *testing.T) {
	wantVersion := "version=" + version.Version + " commit=" + version.Commit + "\n"

	cases := []struct {
		name    string
		args    []string
		handled bool
		code    int
		stdout  string // exact; "" means "nothing on stdout"
		onErr   string // substring required on stderr
	}{
		{name: "no arguments boot the server", args: nil, handled: false, code: 0},
		{name: "--version", args: []string{"--version"}, handled: true, code: 0, stdout: wantVersion},
		{name: "-v", args: []string{"-v"}, handled: true, code: 0, stdout: wantVersion},
		{name: "--help", args: []string{"--help"}, handled: true, code: 0, stdout: usageText},
		{name: "-h", args: []string{"-h"}, handled: true, code: 0, stdout: usageText},
		{
			name: "an unknown flag is a usage error, not a server",
			args: []string{"--bogus"}, handled: true, code: 2,
			onErr: `unknown argument "--bogus"`,
		},
		{
			name: "a positional argument is a usage error too",
			args: []string{"serve"}, handled: true, code: 2,
			onErr: `unknown argument "serve"`,
		},
		{
			name: "the first argument decides: a handled flag is not overruled",
			args: []string{"--version", "--bogus"}, handled: true, code: 0, stdout: wantVersion,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			handled, code := handleArgs(tc.args, &out, &errOut)

			if handled != tc.handled {
				t.Fatalf("handled = %v, want %v (argv %q)", handled, tc.handled, tc.args)
			}
			if code != tc.code {
				t.Fatalf("exit code = %d, want %d (argv %q)", code, tc.code, tc.args)
			}
			if got := out.String(); got != tc.stdout {
				t.Fatalf("stdout = %q, want %q (argv %q)", got, tc.stdout, tc.args)
			}
			if tc.onErr == "" {
				if got := errOut.String(); got != "" {
					t.Fatalf("stderr = %q, want empty (argv %q)", got, tc.args)
				}
				return
			}
			if got := errOut.String(); !strings.Contains(got, tc.onErr) {
				t.Fatalf("stderr = %q, want it to contain %q (argv %q)", got, tc.onErr, tc.args)
			}
			// A usage error must print the usage, not only the complaint.
			if !strings.Contains(errOut.String(), usageText) {
				t.Fatalf("stderr does not carry the usage text (argv %q): %q", tc.args, errOut.String())
			}
		})
	}
}

// TestTheVersionProbeNeverStartsTheServer runs the real binary against a
// database that cannot work and measures whether startup ran.
func TestTheVersionProbeNeverStartsTheServer(t *testing.T) {
	bin := buildCommandBinary(t)

	// The stand-in for Postgres. It ACCEPTS and immediately CLOSES, so a client
	// that dials fails fast instead of hanging on a server that never answers —
	// and the attempt is counted, which is the measurement.
	db := newDialCounter(t)

	prodEnv := []string{
		"APP_ENV=prod",
		"DATABASE_URL=postgres://jomform:jomform@" + db.addr() + "/jomform?sslmode=disable&connect_timeout=5",
		"ENCRYPTION_KEY=" + strings.Repeat("ab", 32),
		"PATH=" + os.Getenv("PATH"),
	}
	wantVersion := "version=" + version.Version + " commit=" + version.Commit + "\n"

	// Every informational path, with the same env the control arm below boots
	// with. None of them may move the counter: a probe that dials the database
	// IS the defect this card is about, whatever it then prints.
	t.Run("informational argv touches no dependency", func(t *testing.T) {
		informational := []struct {
			name       string
			args       []string
			wantCode   int
			wantStdout string
			wantOnErr  string
		}{
			{name: "--version", args: []string{"--version"}, wantCode: 0, wantStdout: wantVersion},
			{name: "--help", args: []string{"--help"}, wantCode: 0, wantStdout: usageText},
			{name: "an unknown flag", args: []string{"--bogus"}, wantCode: 2, wantOnErr: `unknown argument "--bogus"`},
		}
		for _, tc := range informational {
			before := db.count()
			out, errOut, code := runBinary(t, bin, prodEnv, tc.args...)

			if code != tc.wantCode {
				t.Fatalf("%v: exit code = %d, want %d\nstdout: %s\nstderr: %s",
					tc.args, code, tc.wantCode, out, errOut)
			}
			if tc.wantStdout != "" && out != tc.wantStdout {
				t.Fatalf("%v: stdout = %q, want %q", tc.args, out, tc.wantStdout)
			}
			if tc.wantOnErr != "" && !strings.Contains(errOut, tc.wantOnErr) {
				t.Fatalf("%v: stderr = %q, want it to contain %q", tc.args, errOut, tc.wantOnErr)
			}
			// The probe writes the version (or the usage) and nothing else: any
			// slog line at all means startup ran.
			if tc.wantCode == 0 && errOut != "" {
				t.Fatalf("%v: stderr is not empty — startup ran: %q", tc.args, errOut)
			}
			if got := db.count() - before; got != 0 {
				t.Fatalf("%v: %d Postgres connection attempt(s), want 0", tc.args, got)
			}
		}
	})

	// The control: the SAME unreachable env with no argv. It must boot, reach
	// the database and fail there — and the counter must SEE the attempt, which
	// is what makes the zeros above a measurement rather than a dead instrument.
	t.Run("control: the same env with no argv boots and dials", func(t *testing.T) {
		before := db.count()
		out, errOut, code := runBinary(t, bin, prodEnv)
		if code == 0 {
			t.Fatalf("a boot with an unreachable DATABASE_URL exited 0; stdout: %s stderr: %s", out, errOut)
		}
		if !strings.Contains(errOut, "database") {
			t.Fatalf("the boot did not reach the database path; stderr: %q", errOut)
		}
		if got := db.count() - before; got < 1 {
			t.Fatalf("the Postgres stand-in counted %d attempts for a boot that failed on the database — "+
				"the counter cannot see a connection, so the zeros above prove nothing", got)
		}
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// dialCounter is a TCP listener that accepts and immediately closes, counting
// connection ATTEMPTS. Closing at once matters: a stand-in that accepted and
// stayed silent would leave the client waiting on a handshake until its own
// deadline, which turns a fast assertion into a test timeout.
type dialCounter struct {
	ln net.Listener
	n  atomic.Int64
}

func newDialCounter(t *testing.T) *dialCounter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	c := &dialCounter{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.n.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return c
}

func (c *dialCounter) addr() string { return c.ln.Addr().String() }

func (c *dialCounter) count() int64 { return c.n.Load() }

// buildCommandBinary compiles the real command once for this test. The package
// under test IS this directory, so `go test`'s working directory is the package
// to build.
func buildCommandBinary(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "jomform-api-argv-")
	if err != nil {
		t.Fatalf("temp dir for the built binary: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	bin := filepath.Join(dir, "jomform-api")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -o %s .: %v\n%s", bin, err, out)
	}
	return bin
}

// runBinary runs the built binary with exactly this environment (not the
// ambient one — the gate's own DATABASE_URL must not leak in) and reports its
// exit code. A run that outlives the budget is a FAILURE, not a slow success:
// the whole defect is a probe that keeps running.
func runBinary(t *testing.T, bin string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("jomform-api %v was still running after 30s and was killed — a probe must print and exit\n"+
			"stdout: %s\nstderr: %s", args, out.String(), errOut.String())
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), code
}
