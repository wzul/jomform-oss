// Guards for the reclaim of the Valkey keys a KILLED run leaves behind
// (valkey_prune.go, card t_b06c67af).
//
// Three properties have to hold, and each is pinned on its own because each can
// fail on its own:
//
//  1. the RULE — a key whose owning process is gone is reclaimable, a key a
//     LIVE process owns (this one, or a peer gating right now) never is, and a
//     key carrying no pid at all is never touched. Getting this wrong in the
//     eager direction deletes a peer's state mid-test, which is the false red
//     the per-process namespace exists to prevent;
//  2. the WIRING — pruneValkeyKeys consults that rule, deletes exactly what it
//     says, and reaches every reserved keyspace, because a killed run's index is
//     not necessarily this run's; and
//  3. the REAL path — pruneDeadValkeyKeys, against the real server, actually
//     removes a planted orphan and leaves a planted live peer's key where it is.
//     Only a live server answers that: the rule can be perfectly correct while
//     the live querier scans the wrong pattern, the wrong index, or a client
//     that is not connected at all.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// deadPID(t) (schema_name_test.go) supplies the "killed run" pid these guards
// plant: a process that has certainly exited. There is no other way to make one
// — a killed run IS the leak's shape.

// TestKeyOwnerPIDReadsTheNamespacePrefixWrites pins the PARSE against the
// namespace the suite actually writes. If the two drift, the reclaim either
// stops seeing a killed run's keys (the leak the card measured, back again) or
// starts attributing names to a process that never wrote them.
func TestKeyOwnerPIDReadsTheNamespacePrefixWrites(t *testing.T) {
	own := os.Getpid()
	for _, key := range []string{
		Prefix(),
		PrefixFor(t),
		PrefixForName(t.Name()) + "jobs",
		Prefix() + "TestFoo-1a2b3c4d:delayed",
	} {
		pid, ok := keyOwnerPID(key)
		if !ok || pid != own {
			t.Errorf("keyOwnerPID(%q) = (%d, %v); want (%d, true) — the reclaim cannot attribute this "+
				"process's own key, so the naming and the parse have drifted apart", key, pid, ok, own)
		}
	}

	// The other direction: a name the parse must REFUSE to attribute. Every one
	// of these is a name the reclaim would delete if the rule guessed, and the
	// first is the PRODUCTION queue's own key.
	for _, tc := range []struct{ key, why string }{
		{"jf:emailq:jobs", "a production key is not a test key at all"},
		{"jf:emailq:delayed", "a production key is not a test key at all"},
		{"jftest:", "the root alone carries no pid"},
		{"jftest:p", "nothing follows the p"},
		{"jftest:p:jobs", "the pid is empty"},
		{"jftest:px:jobs", "the pid is not a number"},
		{"jftest:p0:jobs", "0 is not a process"},
		{"jftest:p-5:jobs", "a negative pid is not a process"},
		{"jftest:p12", "no colon closes the pid, so the name is not a namespace"},
		{"jftest:legacy-thing", "a jftest: key with no pid predates the pid scheme"},
	} {
		if pid, ok := keyOwnerPID(tc.key); ok {
			t.Errorf("keyOwnerPID(%q) = (%d, true); want no pid — %s. An unattributable name must not be "+
				"attributed to a process, because the reclaim deletes what it attributes", tc.key, pid, tc.why)
		}
	}
}

// TestReclaimableValkeyKey pins both directions of the delete rule. The eager
// direction is the dangerous one: a key a live peer owns is state a test is
// asserting on.
func TestReclaimableValkeyKey(t *testing.T) {
	own := os.Getpid()
	dead := deadPID(t)

	// init is the one process every host has and this run never is; if it does
	// not probe as live here, the live-peer arm cannot be planted at all and the
	// guard says so instead of passing vacuously.
	const livePeer = 1
	if !valkeyOwnerAlive(livePeer) {
		t.Skipf("pid %d does not probe as live on this host, so the live-peer arm cannot be planted", livePeer)
	}

	// A probe with a planted answer, for the arm a single-user host cannot set
	// up: it proves the LIVENESS ANSWER is what decides, not the shape of the
	// name.
	probe := func(pid int) bool { return pid != 4242 }

	for _, tc := range []struct {
		name    string
		key     string
		alive   func(int) bool
		want    bool
		wantWhy string
	}{
		{
			name:  "an orphan whose process was killed is reclaimed",
			key:   keyNamespacePrefix + strconv.Itoa(dead) + ":TestKilledRun:delayed",
			alive: valkeyOwnerAlive,
			want:  true,
		},
		{
			name:    "this process's own key is never reclaimed",
			key:     Prefix() + "TestThisRun:delayed",
			alive:   valkeyOwnerAlive,
			want:    false,
			wantWhy: "belongs to this process",
		},
		{
			name:    "a live peer's key is never reclaimed",
			key:     keyNamespacePrefix + strconv.Itoa(livePeer) + ":TestPeerRun:delayed",
			alive:   valkeyOwnerAlive,
			want:    false,
			wantWhy: "still exists",
		},
		{
			name:    "a jftest: key with no pid is never reclaimed",
			key:     "jftest:legacy-thing",
			alive:   valkeyOwnerAlive,
			want:    false,
			wantWhy: "cannot be attributed",
		},
		{
			name:    "a production-shaped key is never reclaimed",
			key:     "jf:emailq:jobs",
			alive:   valkeyOwnerAlive,
			want:    false,
			wantWhy: "cannot be attributed",
		},
		{
			name:  "the liveness answer decides, not the name",
			key:   keyNamespacePrefix + "4242:TestProbeDecides:delayed",
			alive: probe,
			want:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := reclaimableValkeyKey(tc.key, own, tc.alive)
			if ok != tc.want {
				t.Fatalf("reclaimableValkeyKey(%q) = (%v, %q); want %v", tc.key, ok, why, tc.want)
			}
			if tc.want && why != "" {
				t.Fatalf("reclaimableValkeyKey(%q) reclaims the key but gives the reason %q", tc.key, why)
			}
			if !tc.want && !strings.Contains(why, tc.wantWhy) {
				t.Fatalf("reclaimableValkeyKey(%q) keeps the key for the reason %q, want it to name %q",
					tc.key, why, tc.wantWhy)
			}
		})
	}
}

// fakeValkey drives pruneValkeyKeys without a server, so a REFUSAL can be
// asserted on a key set the host would never hand us: a healthy host has
// orphans only when something was killed, and never a key next to a live peer's.
type fakeValkey struct {
	dbs     []int
	byDB    map[int][]string
	fail    map[int]error
	deleted []string
}

func (f *fakeValkey) keyspaces() []int { return f.dbs }

func (f *fakeValkey) scanKeyspace(_ context.Context, db int) ([]string, error) {
	if err, ok := f.fail[db]; ok {
		return nil, err
	}
	return f.byDB[db], nil
}

func (f *fakeValkey) deleteKeys(_ context.Context, db int, keys ...string) error {
	for _, k := range keys {
		f.deleted = append(f.deleted, fmt.Sprintf("%d/%s", db, k))
	}
	return nil
}

// TestPruneValkeyKeysReclaimsOnlyDeadOwners pins the WIRING: that the rule is
// consulted, that exactly the reclaimable keys are deleted, that every reserved
// keyspace is reached, and that a key the rule keeps is never passed to DEL.
// Without it the rule can be perfectly correct while the prune deletes nothing
// (a killed run's keys live on) or deletes what the rule refused.
func TestPruneValkeyKeysReclaimsOnlyDeadOwners(t *testing.T) {
	own := os.Getpid()
	dead := deadPID(t)
	const (
		runKeyspace   = 8
		otherKeyspace = 15
	)

	orphan := keyNamespacePrefix + strconv.Itoa(dead) + ":TestKilledRun:delayed"
	orphanElsewhere := keyNamespacePrefix + strconv.Itoa(dead) + ":TestKilledRunInTheOtherIndex:jobs"
	peer := keyNamespacePrefix + "1:TestLivePeer:jobs"
	mine := Prefix() + "TestThisRun:jobs"
	legacy := "jftest:legacy-thing"
	production := "jf:emailq:jobs"

	f := &fakeValkey{
		dbs: []int{runKeyspace, otherKeyspace},
		byDB: map[int][]string{
			runKeyspace:   {orphan, peer, mine, legacy, production},
			otherKeyspace: {orphanElsewhere, peer},
		},
	}

	res := pruneValkeyKeys(context.Background(), f, own)

	if res.note != "" {
		t.Errorf("pruneValkeyKeys recorded the degradation note %q on a querier that read everything", res.note)
	}
	if res.scanned != 7 {
		t.Errorf("pruneValkeyKeys scanned %d keys, want 7 — the prune must consider every key in every reserved keyspace", res.scanned)
	}
	want := []string{
		fmt.Sprintf("%d/%s", runKeyspace, orphan),
		fmt.Sprintf("%d/%s", otherKeyspace, orphanElsewhere),
	}
	if strings.Join(res.dropped, ",") != strings.Join(want, ",") {
		t.Errorf("pruneValkeyKeys dropped %v, want exactly %v — the keyspace this run happens to use is not "+
			"the only place a killed run's keys live", res.dropped, want)
	}

	// Every key the rule KEEPS must survive, and the reason must be the rule's,
	// not an accident of the fake. This is the arm that costs a peer its gate run
	// when it is wrong.
	for _, keep := range []struct {
		db      int
		key     string
		wantWhy string
	}{
		{runKeyspace, peer, "still exists"},
		{runKeyspace, mine, "belongs to this process"},
		{otherKeyspace, peer, "still exists"},
		{runKeyspace, legacy, "cannot be attributed"},
		{runKeyspace, production, "cannot be attributed"},
	} {
		why, ok := res.keptValkeyKey(keep.db, keep.key)
		if !ok {
			t.Errorf("pruneValkeyKeys did not record %d/%s as kept, so either it deleted it or it never "+
				"considered it", keep.db, keep.key)
		} else if !strings.Contains(why, keep.wantWhy) {
			t.Errorf("%d/%s was kept for the reason %q, want it to name %q", keep.db, keep.key, why, keep.wantWhy)
		}
		for _, d := range f.deleted {
			if d == fmt.Sprintf("%d/%s", keep.db, keep.key) {
				t.Errorf("pruneValkeyKeys issued DEL for %d/%s, which the rule says must be kept", keep.db, keep.key)
			}
		}
	}
}

// TestPruneValkeyKeysRecordsAKeyspaceItCouldNotRead: the reclaim is best-effort
// for the test about to run, but it must not be indistinguishable from one that
// found nothing to do — a keyspace that answered an error is exactly the case
// where a leak survives unnoticed.
func TestPruneValkeyKeysRecordsAKeyspaceItCouldNotRead(t *testing.T) {
	dead := deadPID(t)
	const (
		runKeyspace  = 8
		deadKeyspace = 10
	)
	orphan := keyNamespacePrefix + strconv.Itoa(dead) + ":TestKilledRun:delayed"

	f := &fakeValkey{
		dbs:  []int{runKeyspace, deadKeyspace},
		byDB: map[int][]string{runKeyspace: {orphan}},
		fail: map[int]error{deadKeyspace: errors.New("connection refused")},
	}

	res := pruneValkeyKeys(context.Background(), f, os.Getpid())

	if !strings.Contains(res.note, strconv.Itoa(deadKeyspace)) || !strings.Contains(res.note, "connection refused") {
		t.Errorf("pruneValkeyKeys recorded the note %q for a keyspace that failed to read, want it to name "+
			"keyspace %d and the error — a silent degradation leaves the leak invisible", res.note, deadKeyspace)
	}
	if !res.droppedValkeyKey(runKeyspace, orphan) {
		t.Errorf("pruneValkeyKeys dropped %v, want the orphan in the keyspace that DID read: one failing "+
			"keyspace must not stop the others being reclaimed", res.dropped)
	}
}

// valkeyKeyExists probes a key in one keyspace of the live server.
func valkeyKeyExists(t *testing.T, l *liveValkey, db int, key string) bool {
	t.Helper()
	rdb, err := l.client(db)
	if err != nil {
		t.Fatalf("client for keyspace %d: %v", db, err)
	}
	n, err := rdb.Exists(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("probe %d/%s: %v", db, key, err)
	}
	return n > 0
}

// TestPruneDeadValkeyKeysAgainstValkey drives the REAL reclaim against the real
// server: the acceptance the card states, in both directions.
//
// It plants the two properties that matter and then runs the live entry point,
// because the rule being right says nothing about whether the live querier scans
// the pattern and the indexes a killed run actually writes.
func TestPruneDeadValkeyKeysAgainstValkey(t *testing.T) {
	if os.Getenv(ValkeyEnv) == "" {
		t.Skipf("%s not set; skipping Valkey integration test", ValkeyEnv)
	}
	ctx := context.Background()
	dead := deadPID(t)

	// The card measured orphans in indexes this run was not using (DB 10 and DB
	// 15), so the fixture lives in this run's keyspace AND in another reserved
	// one: a prune that only reached Keyspace() would leave most of the leak.
	here := Keyspace()
	there := reservedKeyspaces[0]
	for _, db := range reservedKeyspaces {
		if db != here {
			there = db
			break
		}
	}

	live := newLiveValkey()
	// Registered FIRST so it runs LAST: t.Cleanup functions run in reverse
	// order, and the per-key cleanups registered below must still have a live
	// connection to delete through. (With `defer live.Close()` the close runs
	// before any t.Cleanup, the deletes fail on a closed client, and the fixture
	// leaks the very keys this card is about — measured on this host.)
	t.Cleanup(live.Close)

	plant := func(db int, key string, ttl time.Duration) {
		t.Helper()
		rdb, err := live.client(db)
		if err != nil {
			t.Fatalf("client for keyspace %d: %v", db, err)
		}
		// A zset member, the shape the card measured (the delayed queue key):
		// planted through the same client the reclaim scans with.
		if err := rdb.ZAdd(ctx, key, goredis.Z{Score: 1, Member: "job"}).Err(); err != nil {
			t.Fatalf("plant %d/%s: %v", db, key, err)
		}
		if ttl > 0 {
			if err := rdb.Expire(ctx, key, ttl).Err(); err != nil {
				t.Fatalf("bound %d/%s: %v", db, key, err)
			}
		}
		// The fixture cleans up after ITSELF, and only its own keys: this guard
		// must not leave behind the very leak it is testing for.
		t.Cleanup(func() { _ = rdb.Del(context.Background(), key).Err() })
	}

	const livePeer = 1
	if !valkeyOwnerAlive(livePeer) {
		t.Skipf("pid %d does not probe as live on this host, so the live-peer arm cannot be planted", livePeer)
	}

	own := os.Getpid()
	orphanHere := keyNamespacePrefix + strconv.Itoa(dead) + ":TestKilledRunProbe:delayed"
	orphanThere := keyNamespacePrefix + strconv.Itoa(dead) + ":TestKilledRunInTheOtherIndex:delayed"
	peerKey := keyNamespacePrefix + strconv.Itoa(livePeer) + ":TestLivePeerProbe:delayed"
	ownKey := Prefix() + "TestOwnProbe:delayed"
	legacyKey := "jftest:legacy-probe-" + strconv.Itoa(own)

	plant(here, orphanHere, 0)
	plant(there, orphanThere, 0)
	plant(there, peerKey, 0)
	plant(here, ownKey, 0)
	// The one fixture key with a TTL, and it needs one: a jftest: name carrying
	// no pid can never be attributed to a process, so nothing — not this prune,
	// not anything else in the tree — would ever remove it. A guard must not
	// plant a key that outlives it as a permanent leak, so this one is bounded
	// (and the arm it exercises is that the reclaim leaves it alone).
	plant(here, legacyKey, 15*time.Minute)

	pruneDeadValkeyKeys(ctx)

	res, ok := lastValkeyPrune()
	if !ok {
		t.Fatal("pruneDeadValkeyKeys recorded no result — the live entry point never ran")
	}
	if res.note != "" {
		t.Errorf("the live reclaim recorded the note %q; want none (a degraded read is how a leak survives "+
			"unnoticed)", res.note)
	}

	// Reclaimed: the orphan, in BOTH keyspaces.
	for _, orphan := range []struct {
		db  int
		key string
	}{{here, orphanHere}, {there, orphanThere}} {
		if !res.droppedValkeyKey(orphan.db, orphan.key) {
			t.Errorf("the reclaim did not report %d/%s as dropped (dropped: %v); a killed run's key survives",
				orphan.db, orphan.key, res.dropped)
		}
		if valkeyKeyExists(t, live, orphan.db, orphan.key) {
			t.Errorf("the killed run's key %d/%s SURVIVED the reclaim with TTL -1 — this is the leak the "+
				"card measured", orphan.db, orphan.key)
		}
	}

	// Never touched: a live peer's key, this run's own key, and a name carrying
	// no pid. Each is the arm that costs a live run its state when it is wrong.
	for _, keep := range []struct {
		db      int
		key     string
		wantWhy string
	}{
		{there, peerKey, "still exists"},
		{here, ownKey, "belongs to this process"},
		{here, legacyKey, "cannot be attributed"},
	} {
		why, recorded := res.keptValkeyKey(keep.db, keep.key)
		if !recorded {
			t.Errorf("the reclaim did not record %d/%s as kept (%v); either it deleted it or it never "+
				"considered it", keep.db, keep.key, res.kept)
		} else if !strings.Contains(why, keep.wantWhy) {
			t.Errorf("%d/%s was kept for the reason %q, want it to name %q", keep.db, keep.key, why, keep.wantWhy)
		}
		if !valkeyKeyExists(t, live, keep.db, keep.key) {
			t.Errorf("the reclaim DELETED %d/%s, which must never be deleted (owner: %s)", keep.db, keep.key, keep.wantWhy)
		}
	}

	// Nothing outside the test namespace: the scan pattern is `jftest:*`, and a
	// production key (`jf:emailq:*`) or any other tenant of these indexes must
	// never appear in what was deleted.
	for _, d := range res.dropped {
		key := d
		if i := strings.IndexByte(d, '/'); i >= 0 {
			key = d[i+1:]
		}
		if !strings.HasPrefix(key, keyNamespaceRoot) {
			t.Errorf("the reclaim deleted %q, which is not under the test namespace %q", d, keyNamespaceRoot)
		}
	}
}

// TestPruneDeadValkeyKeysIsWiredIntoValkey pins that the reclaim is actually
// CALLED from the one hook every Valkey-backed suite passes through. prune can
// be flawless and never run, and then a killed run's keys accumulate forever —
// which is exactly the card, since t.Cleanup cannot run on a killed process and
// nothing else ever removes them.
func TestPruneDeadValkeyKeysIsWiredIntoValkey(t *testing.T) {
	src, err := os.ReadFile("valkey.go")
	if err != nil {
		t.Fatalf("read valkey.go: %v", err)
	}
	if !strings.Contains(string(src), "pruneDeadValkeyKeys(context.Background())") {
		t.Error("Valkey() no longer runs pruneDeadValkeyKeys — the keys a killed run leaves behind would " +
			"never be reclaimed (card t_b06c67af)")
	}
}

// TestValkeyTestKeyStoryMatchesTheCode pins the two halves of the claim the card
// was filed about: PrefixFor's doc said the keys "expire or are cleaned up with
// the run", while nothing in the tree sets a TTL on a test key — so a run killed
// before its cleanup leaked them forever and the sentence said otherwise. The
// claim and the code are checked against each other, so the sentence can only
// come back alongside a TTL that makes it true.
func TestValkeyTestKeyStoryMatchesTheCode(t *testing.T) {
	src, err := os.ReadFile("valkey.go")
	if err != nil {
		t.Fatalf("read valkey.go: %v", err)
	}
	s := string(src)

	setsTTL := strings.Contains(s, ".Expire(") || strings.Contains(s, "ExpireAt(") || strings.Contains(s, "SetEX")
	if strings.Contains(s, "keys expire") && !setsTTL {
		t.Error("valkey.go claims test keys expire while it sets no TTL on any of them — nothing in the tree " +
			"does, so the claim is false (card t_b06c67af). Either state the reclaim that actually removes " +
			"them, or add the TTL that makes the claim true")
	}
	if !strings.Contains(s, "pruneDeadValkeyKeys") {
		t.Error("valkey.go no longer names what removes a killed run's test keys (pruneDeadValkeyKeys) — the " +
			"namespace's own documentation must state the reclaim that exists, not one that does not")
	}
}
