// Valkey test isolation: one shared rule for the integration suites.
//
// Every checkout points at the SAME Valkey (JOMFORM_TEST_VALKEY_URL), and the
// email queue's key names (`jf:emailq:jobs` and its siblings) are the SAME
// literals the production binary writes. A test that reads or flushes those
// literals is therefore competing with whatever else is on that database —
// including a developer's own running API, whose email worker claims the very
// job the assertion is about:
//
//	--- FAIL: TestIssue606AdminEmailResolvesToTheOwnerUser
//	    the default {admin_email} rule enqueued 0 job(s), want exactly 1.
//
// for an enqueue that succeeded (the job was found in `jf:emailq:delayed`,
// claimed and retried by the live worker). The old fixtures tried to avoid this
// with a dedicated DB number pinned per file (/4, /7, /11, /12, /13), but a fixed
// DB number is not owned by the test binary either: another run, another package
// or a live process configured on that number collides exactly the same way.
//
// Two mechanisms, and they cover different things:
//
//  1. Prefix() namespaces KEY NAMES per test process. This is the correctness
//     guarantee: a process's jobs live under keys no other process reads,
//     claims or flushes, so sharing a database with anything — a peer suite, a
//     live API, a stale run — cannot steal or delete them. It is the same fix
//     the scratch SCHEMAS got in testdb.Open (per process, not per package).
//  2. Valkey also picks its DB from a small set of numbers that no other test in
//     the repo flushes, derived from the pid. That is belt to the prefix's
//     braces: it keeps a run clear of a PEER still built from a pre-fix fixture,
//     which FlushDBs its hardcoded number (/3, /4, /7, /11, /12, /13) and would
//     delete the prefixed keys too if this run happened to be on that number.
//
// Nothing here ever calls FlushDB. Deleting a key this process did not write is
// what made the old fixtures destructive, and it is never needed once the keys
// are owned: isolation comes from the name, not from wiping the database.
package testdb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// ValkeyEnv is the environment variable holding the integration-test Valkey URL.
const ValkeyEnv = "JOMFORM_TEST_VALKEY_URL"

// Prefix returns the key namespace this test PROCESS owns, e.g.
// "jftest:p48213:". Pid-derived, so two concurrent runs of the same package write
// under different names and cannot see each other's keys.
//
// Use it for any Valkey key an assertion depends on: a literal production key
// name is a shared resource and will eventually be written by something else.
// emailqueue.NewQueueWithPrefix is how a queue is built on one.
//
// It is the right namespace when state must survive across the tests of one
// binary; use PrefixFor when each test needs its own.
func Prefix() string {
	return fmt.Sprintf("jftest:p%d:", os.Getpid())
}

// PrefixFor returns Prefix plus the test's own name, e.g.
// "jftest:p48213:TestFoo:". Two tests in one binary therefore do not share it.
//
// This is what per-test isolation is built on, and it replaces the two
// destructive habits that used to provide it: FlushDB of a whole DB, and a
// sweep-delete of a production key prefix (e.g. `jf:bnc:*`). Both deleted keys
// the test had not written, so each one also cleared a live process's or a peer
// run's state — a fixture cannot be allowed to reach outside its own keys to
// make its assertions deterministic. A name of its own is the deterministic
// thing, and it costs nothing: the keys expire or are cleaned up with the run.
func PrefixFor(t *testing.T) string {
	t.Helper()
	return Prefix() + sanitizeKeyPart(t.Name()) + ":"
}

// sanitizeKeyPart reduces a test name to characters that are safe and readable
// in a key: everything outside [A-Za-z0-9_-] becomes '_'.
func sanitizeKeyPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "test"
	}
	return b.String()
}

// reservedKeyspaces are the DB numbers Valkey may use. They exclude every number
// another test flushes or an env file in use points at, so a run also stays clear
// of a PEER still built from a pre-fix fixture (see the package note above).
var reservedKeyspaces = []int{8, 10, 15}

// Keyspace returns this run's Valkey DB number.
func Keyspace() int {
	return reservedKeyspaces[os.Getpid()%len(reservedKeyspaces)]
}

// Valkey returns a client on this test process's own DB, plus the key namespace
// this TEST owns (PrefixFor). It skips the test when ValkeyEnv is unset, and
// fails it when the database is unreachable rather than skipping silently.
//
// The client is closed, and every key under the returned namespace is deleted, at
// test cleanup — those keys are this test's own, so removing them touches nothing
// another run owns. It deliberately does NOT FlushDB: deleting keys this process
// did not write is exactly what produced the false results the package note
// describes.
//
// It returns a client and a prefix rather than an *emailqueue.Queue so that this
// package keeps no dependency on emailqueue — the emailqueue tests use it too,
// and an import there would be a cycle.
//
// The prefix is per TEST, because per-test isolation is what the old FlushDB was
// standing in for: a test that asserts an exact queue length, or that the next
// dequeued job is its own, must not inherit an earlier test's keys. A test that
// genuinely needs state shared with its siblings in the same binary can build
// its own prefix on Prefix() instead.
func Valkey(t *testing.T) (*goredis.Client, string) {
	t.Helper()
	raw := os.Getenv(ValkeyEnv)
	if raw == "" {
		t.Skipf("%s not set; skipping Valkey integration test", ValkeyEnv)
	}
	// go-redis only understands redis://; the deployment uses valkey://. Any DB
	// in the configured URL is replaced by this run's own.
	u := strings.TrimPrefix(raw, "valkey://")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[:i]
	}
	opts, err := goredis.ParseURL(fmt.Sprintf("redis://%s/%d", u, Keyspace()))
	if err != nil {
		t.Fatalf("parse %s: %v", ValkeyEnv, err)
	}
	rdb := goredis.NewClient(opts)
	prefix := PrefixFor(t)
	t.Cleanup(func() {
		deleteOwnKeys(t, rdb, prefix)
		_ = rdb.Close()
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("test valkey unavailable: %v", err)
	}
	return rdb, prefix
}

// deleteOwnKeys removes the keys under prefix by SCAN, not KEYS, and never a key
// outside it.
func deleteOwnKeys(t *testing.T, rdb *goredis.Client, prefix string) {
	t.Helper()
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			t.Errorf("cleanup: scan own keys: %v", err)
			return
		}
		if len(keys) > 0 {
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				t.Errorf("cleanup: delete own keys: %v", err)
				return
			}
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}
