package emailqueue

// The queue's key names are a PRODUCTION contract, not an implementation
// detail: a running deployment's ready list, delayed set, in-flight list and
// dead-letter list are keyed by them, and a job left in `jf:emailq:jobs` at the
// moment of a deploy is picked up by the next process only if that process
// computes the same names. They became computed values (the queue prefixes them)
// when the key set gained a per-process namespace for the tests — so this pins
// the production default against a rename, and pins that a custom prefix
// REPLACES it rather than sitting beside it.
//
// A rename here would not fail to compile and would not fail any behavioural
// test: the worker would simply read an empty list forever while the old keys
// piled up, which is a silent, total delivery outage.

import (
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestQueueKeysProductionDefaults(t *testing.T) {
	q := NewQueue((*goredis.Client)(nil))
	want := map[string]string{
		"ready":    "jf:emailq:jobs",
		"delayed":  "jf:emailq:delayed",
		"inflight": "jf:emailq:inflight",
		"dead":     "jf:emailq:dead",
	}
	got := map[string]string{
		"ready":    q.ReadyKey(),
		"delayed":  q.DelayedKey(),
		"inflight": q.InflightKey(),
		"dead":     q.DeadKey(),
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("NewQueue() %s key = %q, want %q — a running deployment's jobs live under "+
				"the documented names, and a rename strands every queued job without an error",
				name, got[name], w)
		}
	}
	if DefaultKeyPrefix != "jf:emailq:" {
		t.Errorf("DefaultKeyPrefix = %q, want %q", DefaultKeyPrefix, "jf:emailq:")
	}
}

// TestQueueWithPrefixReplacesTheDefault: a prefixed queue must use its prefix
// INSTEAD of the production one. If it merely added to it — or fell back to the
// default — a test process would still be writing the production keys, which is
// the defect this whole mechanism exists to remove.
func TestQueueWithPrefixReplacesTheDefault(t *testing.T) {
	const custom = "jftest:p1:TestFoo:"
	q := NewQueueWithPrefix((*goredis.Client)(nil), custom)
	for name, key := range map[string]string{
		"ready": q.ReadyKey(), "delayed": q.DelayedKey(),
		"inflight": q.InflightKey(), "dead": q.DeadKey(),
	} {
		if !strings.HasPrefix(key, custom) {
			t.Errorf("prefixed queue %s key = %q, want it to start with %q", name, key, custom)
		}
		if strings.Contains(key, DefaultKeyPrefix) {
			t.Errorf("prefixed queue %s key = %q still contains the production prefix %q — a test "+
				"process would write keys a live worker reads and claims",
				name, key, DefaultKeyPrefix)
		}
	}

	// An empty prefix must not produce bare "jobs"-style keys in the shared
	// keyspace; it falls back to the production default.
	if got := NewQueueWithPrefix((*goredis.Client)(nil), "").ReadyKey(); got != "jf:emailq:jobs" {
		t.Errorf("empty-prefix queue ready key = %q, want the production default %q",
			got, "jf:emailq:jobs")
	}
}
