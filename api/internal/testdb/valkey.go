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
//  3. A run that is KILLED before its t.Cleanup cannot delete its keys at all —
//     and no test key carries a TTL, so nothing else would ever remove them.
//     pruneDeadValkeyKeys (valkey_prune.go) reclaims them on the next run: every
//     key under jftest: whose OWNING pid is gone, and only those.
//
// Nothing here ever calls FlushDB. Deleting a key this process did not write is
// what made the old fixtures destructive, and it is never needed once the keys
// are owned: isolation comes from the name, not from wiping the database.
package testdb

import (
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// ValkeyEnv is the environment variable holding the integration-test Valkey URL.
const ValkeyEnv = "JOMFORM_TEST_VALKEY_URL"

// keyNamespaceRoot is the prefix EVERY test key lives under — production keys
// are `jf:...`, so this root is the whole of what the suite can have written.
// The reclaim below scans it, and the guard in internal/httpapi asserts that a
// test prefix starts with it.
const keyNamespaceRoot = "jftest:"

// keyNamespacePrefix is the fixed leading component of Prefix(): the pid (and
// nothing else) follows it, e.g. "jftest:p48213:". It is ONE expression because
// two places depend on its exact shape — Prefix builds a key namespace with it,
// and keyOwnerPID (below) reads the owning process back out of a key name. A
// second copy of the rule is how the two would drift.
const keyNamespacePrefix = keyNamespaceRoot + "p"

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
	return fmt.Sprintf("%s%d:", keyNamespacePrefix, os.Getpid())
}

// keyOwnerPID reads the pid a test key namespace was built for out of the key
// itself, e.g. jftest:p48213:TestFoo-1a2b3c4d:delayed → 48213. ok is false when
// the name carries no usable jftest:p<pid>: namespace at all, which includes a
// key of the production shape (jf:emailq:jobs) and a jftest: key that predates
// the pid scheme: neither can be attributed to a process, and an unattributable
// key is never deleted (see reclaimableValkeyKey).
func keyOwnerPID(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, keyNamespacePrefix)
	if !ok {
		return 0, false
	}
	i := strings.IndexByte(rest, ':')
	if i <= 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(rest[:i])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// PrefixFor returns Prefix plus the key part this test owns, e.g.
// "jftest:p48213:TestFoo-1a2b3c4d:". Two tests in one binary therefore do not
// share it — see KeyPart for why the test's name on its own is not enough.
//
// This is what per-test isolation is built on, and it replaces the two
// destructive habits that used to provide it: FlushDB of a whole DB, and a
// sweep-delete of a production key prefix (e.g. `jf:bnc:*`). Both deleted keys
// the test had not written, so each one also cleared a live process's or a peer
// run's state — a fixture cannot be allowed to reach outside its own keys to
// make its assertions deterministic. A name of its own is the deterministic
// thing, and it costs nothing: the keys are deleted at test cleanup, and a run
// KILLED before that cleanup cannot leak them either — the next run reclaims
// them (pruneDeadValkeyKeys, valkey_prune.go), keyed on the pid in this very
// prefix.
//
// Nothing in the suite relies on a test key EXPIRING: no code sets a TTL on
// one, so an unreclaimed key lives forever. Any statement that they expire has
// to come with the TTL that makes it true.
func PrefixFor(t *testing.T) string {
	t.Helper()
	return PrefixForName(t.Name())
}

// PrefixForName is PrefixFor for a name the CALLER supplies instead of the one
// the test is running under. It exists because injectivity of KeyPart is a
// property of two names: the test running under one of them cannot check it
// with a single *testing.T.
func PrefixForName(name string) string {
	return Prefix() + KeyPart(name) + ":"
}

// KeyPart is the component of a key namespace that belongs to ONE test name:
// the readable sanitised form (SanitizeKeyPart) plus a short hash of the RAW
// name, e.g. "TestFoo-1a2b3c4d".
//
// The hash is not decoration. Sanitising alone maps every character outside
// [A-Za-z0-9_-] to '_', so it is NOT injective: "TestX/a" and "TestX_a" both
// become "TestX_a". Under the plain sanitised form those two tests share one
// namespace — the isolation failure this package exists to prevent (two tests
// in one binary emptying each other's queue), arriving through the NAME instead
// of through the pid, with no diagnostic anywhere. The RAW name is hashed, not
// the sanitised one, because the sanitised forms are precisely what collide.
//
// A guard comparing a key against a name must compare against this, not against
// SanitizeKeyPart: the key carries the hash.
func KeyPart(name string) string {
	return SanitizeKeyPart(name) + "-" + fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(name)))
}

// SanitizeKeyPart reduces a name to the readable half of a key-space component:
// everything outside [A-Za-z0-9_-] becomes '_', and an empty name becomes
// "test".
//
// It is deliberately NOT injective and is not on its own a namespace (see
// KeyPart, which is what prefixes and comparisons are built from). Exported so
// a guard can quote the readable half in its message and name the collision it
// is pinning — not so that a guard can decide ownership with it, which is the
// false reading card t_e26669bd was filed about.
func SanitizeKeyPart(s string) string {
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
//
// Before the caller writes anything, the FIRST call in this process also runs
// pruneDeadValkeyKeys once: the keys a KILLED run left behind are reclaimed
// here, because that is the only hook every Valkey-backed suite passes through.
func Valkey(t *testing.T) (*goredis.Client, string) {
	t.Helper()
	raw := os.Getenv(ValkeyEnv)
	if raw == "" {
		t.Skipf("%s not set; skipping Valkey integration test", ValkeyEnv)
	}
	valkeyPruneOnce.Do(func() { pruneDeadValkeyKeys(context.Background()) })
	rdb, err := newValkeyClient(Keyspace())
	if err != nil {
		t.Fatalf("%v", err)
	}
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

// valkeyPruneOnce makes the reclaim of a killed run's keys run at most once per
// test process: the first Valkey-backed test pays for the scan, and every test
// after it does not.
var valkeyPruneOnce sync.Once

// valkeyAddr returns the configured Valkey endpoint with any DB index stripped —
// what go-redis needs before this run's own index is appended. The deployment
// uses valkey://, which go-redis does not understand, so the scheme is
// normalised here rather than at every call site.
func valkeyAddr() (string, error) {
	u := strings.TrimPrefix(os.Getenv(ValkeyEnv), "valkey://")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[:i]
	}
	if u == "" {
		return "", fmt.Errorf("%s names no host", ValkeyEnv)
	}
	return u, nil
}

// newValkeyClient returns a client on the given DB number of the configured
// endpoint. It is the ONE place a test client is built, so the test namespace
// and the reclaim of a killed run's keys always address the same server — the
// reclaim needs the OTHER reserved indexes too, and it gets them from here.
func newValkeyClient(db int) (*goredis.Client, error) {
	addr, err := valkeyAddr()
	if err != nil {
		return nil, err
	}
	opts, err := goredis.ParseURL(fmt.Sprintf("redis://%s/%d", addr, db))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", ValkeyEnv, err)
	}
	return goredis.NewClient(opts), nil
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
