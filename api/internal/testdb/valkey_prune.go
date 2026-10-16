// Package testdb — reclaiming the Valkey keys a KILLED test run left behind,
// the key-space counterpart of pruneDeadSchemas (testdb.go) and
// pruneDeadDatabases (scratchdb.go).
//
// WHY THIS FILE EXISTS (card t_b06c67af)
//
// Prefix() gives every test process a key namespace of its own (jftest:p<pid>:)
// and testdb.Valkey deletes that namespace at t.Cleanup. That is the right
// ownership rule — a process deletes only keys it wrote — but the cleanup runs
// ONLY if the process reaches t.Cleanup. A gate that hits its timeout, a
// `go test` that is SIGKILLed, a lane torn down mid-suite: each one leaves its
// keys in Valkey with no owner and nothing that removes them.
//
// Measured on this host: a test binary SIGKILLed while its key existed left
// `jftest:p4148088:TestDelayedPromotion:delayed` behind with `TTL = -1`, and
// keys in the same shape from earlier killed runs (owning pids long gone) were
// still present days later.
//
// Two properties make that a leak rather than a curiosity:
//
//   - nothing in the tree sets a TTL on a test key — api/internal/emailqueue
//     has no Expire/SetEX call at all, so `TTL = -1` is forever, not "soon"; and
//   - the DB index is one of three pid-derived numbers (Keyspace() = {8,10,15})
//     that every later run reuses, so the orphans accumulate in a keyspace that
//     IS shared.
//
// The scratch-SCHEMA half of testdb already had this answer for its own kind of
// leftover (pruneDeadSchemas → liveScratchSchema → ownerAliveFromErr), and the
// scratch-DATABASE half got one in scratchdb.go. This is the same rule, applied
// to key names: every key under jftest: whose pid is GONE is reclaimed; a key
// whose pid is still there — a peer gating right now — is never touched.
//
// Unlike the schema and database prunes this one does NOT yield to a live peer,
// and the difference is deliberate. Those two issue `DROP SCHEMA ... CASCADE` /
// `DROP DATABASE`, database-wide DDL that invalidates catalogue entries a peer
// may be reading, so they refuse to run at all while any other connection
// exists. This prune issues `DEL` of individually named keys, and the rule above
// already guarantees every one of those names belongs to a process that no
// longer exists — a live run cannot be reading its own keys under a dead pid,
// because a key namespace IS a pid here. There is nothing left to yield over,
// and yielding would mean a peer lane in every concurrent gate keeps the leak
// alive forever.
//
// Known limit, stated rather than assumed: a pid can be REUSED. If a killed run
// owned jftest:pX:… and a later run happens to be given pid X, that pid is alive
// again and its predecessor's keys are kept — the rule is conservative in the
// direction that never harms a live run, so those keys leak rather than being
// deleted under a run that now owns the name. No TTL is involved in the answer
// either way: the reclaim is what removes them, so the doc comment on PrefixFor
// must not claim the keys expire.
package testdb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"syscall"

	goredis "github.com/redis/go-redis/v9"
)

// valkeyScanPattern is what the reclaim enumerates: the whole test key
// namespace, `jftest:*`. It is the ROOT, not keyNamespacePrefix, so a jftest:
// key that carries no pid at all is SEEN by the scan and reported by the rule
// as unattributable instead of being invisible to it — a name this prune can
// read but will not delete is exactly the thing an operator needs told.
const valkeyScanPattern = keyNamespaceRoot + "*"

// valkeyOwnerAlive reports whether the process a key namespace names still
// exists. It is the SAME liveness rule the scratch-schema and scratch-database
// prunes use, deliberately not a second copy of it: `kill(pid, 0)`, with EPERM
// meaning "the process is there, owned by someone else" and therefore ALIVE
// (see ownerAliveFromErr — reading EPERM as dead is how a live peer's keys
// would be deleted under it).
func valkeyOwnerAlive(pid int) bool {
	return ownerAliveFromErr(syscall.Kill(pid, 0))
}

// reclaimableValkeyKey reports whether one key may be deleted, and why not when
// it may not. Both directions matter:
//
//   - a key a live process owns — this one or a peer gating right now — is NEVER
//     reclaimed: deleting it deletes state a test is asserting on, which is the
//     false red the per-process namespace exists to prevent.
//   - a key whose owner is gone IS reclaimed, or the leak the card measured
//     comes straight back, because t.Cleanup cannot run on a killed process.
//
// `alive` is injected so the rule can be driven with planted answers; the live
// path passes valkeyOwnerAlive. `ownPID` is this process: its own keys are never
// reclaimed, which a liveness probe would also conclude, but stating it makes
// the rule readable and keeps a pathological probe result (a stale pid inside a
// container, a pid-namespace oddity) from deleting this run's own state.
func reclaimableValkeyKey(key string, ownPID int, alive func(int) bool) (bool, string) {
	pid, ok := keyOwnerPID(key)
	if !ok {
		return false, "its name carries no jftest:p<pid>: namespace, so it cannot be attributed to a process"
	}
	if pid == ownPID {
		return false, "it belongs to this process"
	}
	if alive(pid) {
		return false, fmt.Sprintf("its owning process (pid %d) still exists", pid)
	}
	return true, ""
}

// valkeyQuerier is the slice of Valkey the reclaim needs: the keyspaces to
// consider, the keys in each, and the delete. It exists so the WIRING — that the
// rule is consulted at all, and that a refusal is honoured — can be asserted
// with planted key sets, which a live server cannot offer: it only ever reports
// the keys it happens to hold, and a key owned by a dead process is exactly what
// a healthy host has least of.
//
// The unit is a KEYSPACE, not a key, because a Valkey client is bound to one DB
// index at dial time and SCAN is per index: the reclaim has to reach the other
// reserved indexes too, and a killed run's index is not necessarily this run's.
type valkeyQuerier interface {
	keyspaces() []int
	scanKeyspace(ctx context.Context, db int) ([]string, error)
	deleteKeys(ctx context.Context, db int, keys ...string) error
}

// liveValkey is the live valkeyQuerier. It holds one client per reserved
// keyspace, opened lazily — the caller's client is on THIS run's index and
// cannot see the others.
type liveValkey struct {
	clients map[int]*goredis.Client
}

// newLiveValkey returns the live querier, with no connection open yet (nothing
// is dialled until a keyspace is actually scanned).
func newLiveValkey() *liveValkey {
	return &liveValkey{clients: map[int]*goredis.Client{}}
}

func (l *liveValkey) keyspaces() []int { return reservedKeyspaces }

func (l *liveValkey) client(db int) (*goredis.Client, error) {
	if c, ok := l.clients[db]; ok {
		return c, nil
	}
	c, err := newValkeyClient(db)
	if err != nil {
		return nil, err
	}
	l.clients[db] = c
	return c, nil
}

func (l *liveValkey) scanKeyspace(ctx context.Context, db int) ([]string, error) {
	rdb, err := l.client(db)
	if err != nil {
		return nil, err
	}
	var (
		cursor uint64
		keys   []string
	)
	for {
		batch, next, err := rdb.Scan(ctx, cursor, valkeyScanPattern, 200).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if next == 0 {
			return keys, nil
		}
		cursor = next
	}
}

func (l *liveValkey) deleteKeys(ctx context.Context, db int, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	rdb, err := l.client(db)
	if err != nil {
		return err
	}
	return rdb.Del(ctx, keys...).Err()
}

// Close releases every connection the querier opened. It is defer'd by the live
// prune, so no test binary leaves a Valkey connection behind.
func (l *liveValkey) Close() {
	for _, c := range l.clients {
		_ = c.Close()
	}
	l.clients = nil
}

// valkeyPruneResult is what the most recent live key prune did, for the guards
// (and for a human reading a gate log) to judge the reclaim against instead of
// guessing from whatever the host looks like afterwards.
type valkeyPruneResult struct {
	// dropped is what was reclaimed (each entry "<db>/<key>"), kept records
	// every key that stayed with the reason the rule gave, scanned is how many
	// keys were considered, and note is any degradation the operator should know
	// about ("" when the reclaim read everything it needed). The keyspaces
	// considered are the querier's, not a field here: a run that read them all
	// has nothing to say about them.
	dropped []string
	kept    []string
	scanned int
	note    string
}

// lastValkeyPruneResult holds the outcome of the most recent LIVE prune
// (pruneDeadValkeyKeys). It reads a process-global, which is sound because each
// test binary is its own process and the guards read it immediately after the
// prune that set it.
var lastValkeyPruneResult atomic.Pointer[valkeyPruneResult]

// lastValkeyPrune returns the most recent live key prune's outcome, and whether
// one ran in this process.
func lastValkeyPrune() (valkeyPruneResult, bool) {
	r := lastValkeyPruneResult.Load()
	if r == nil {
		return valkeyPruneResult{}, false
	}
	return *r, true
}

// pruneDeadValkeyKeys reclaims the keys a killed run left behind. Like the other
// two prunes it is best-effort — a failure here must not fail the test that is
// about to run — but a failure must not be indistinguishable from "nothing to
// do" either, so anything it could not read is recorded in the result's note
// rather than swallowed.
//
// It runs once per test process, from the first testdb.Valkey call: that is the
// hook every Valkey-backed suite passes through, and it is BEFORE the caller
// writes anything, so the reclaim can never race the test that triggered it.
func pruneDeadValkeyKeys(ctx context.Context) {
	var res valkeyPruneResult
	if os.Getenv(ValkeyEnv) == "" {
		res.note = ValkeyEnv + " is not set, so the key reclaim was skipped"
		lastValkeyPruneResult.Store(&res)
		return
	}
	live := newLiveValkey()
	defer live.Close()
	res = pruneValkeyKeys(ctx, live, os.Getpid())
	lastValkeyPruneResult.Store(&res)
}

// pruneValkeyKeys applies the rule to every key the querier offers and deletes
// the ones it reclaims, returning what it did. It is separated from the live
// querier so the wiring can be asserted with planted key sets.
func pruneValkeyKeys(ctx context.Context, q valkeyQuerier, ownPID int) valkeyPruneResult {
	var res valkeyPruneResult
	for _, db := range q.keyspaces() {
		keys, err := q.scanKeyspace(ctx, db)
		if err != nil {
			// Recorded, not swallowed: a keyspace that could not be read must
			// not look the same as one with nothing to reclaim.
			res.note = fmt.Sprintf("keyspace %d could not be read: %v", db, err)
			continue
		}
		res.scanned += len(keys)

		var drop []string
		for _, key := range keys {
			ok, why := reclaimableValkeyKey(key, ownPID, valkeyOwnerAlive)
			if !ok {
				res.kept = append(res.kept, fmt.Sprintf("%d/%s: %s", db, key, why))
				continue
			}
			drop = append(drop, key)
		}
		if len(drop) == 0 {
			continue
		}
		if err := q.deleteKeys(ctx, db, drop...); err != nil {
			// A DEL that failed is not a decision to keep the keys: record
			// them as unreclaimed, with the reason, so the leak is visible.
			res.note = fmt.Sprintf("keyspace %d: %d reclaimable key(s) could not be deleted: %v",
				db, len(drop), err)
			for _, key := range drop {
				res.kept = append(res.kept, fmt.Sprintf("%d/%s: the delete failed: %v", db, key, err))
			}
			continue
		}
		for _, key := range drop {
			res.dropped = append(res.dropped, fmt.Sprintf("%d/%s", db, key))
		}
	}
	return res
}

// droppedValkeyKey reports whether the result reclaimed key in keyspace db. The
// guards use it instead of comparing whole slices, because a live host's
// keyspace holds other runs' orphans too: what must hold is that OUR planted key
// is among what was reclaimed, not that it was the only thing.
func (r valkeyPruneResult) droppedValkeyKey(db int, key string) bool {
	want := fmt.Sprintf("%d/%s", db, key)
	for _, d := range r.dropped {
		if d == want {
			return true
		}
	}
	return false
}

// keptValkeyKey reports whether the result kept key in keyspace db, and the
// reason it recorded.
func (r valkeyPruneResult) keptValkeyKey(db int, key string) (string, bool) {
	prefix := fmt.Sprintf("%d/%s: ", db, key)
	for _, k := range r.kept {
		if strings.HasPrefix(k, prefix) {
			return strings.TrimPrefix(k, prefix), true
		}
	}
	return "", false
}
