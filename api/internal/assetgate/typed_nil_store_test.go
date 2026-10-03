// Regression test for the typed-nil object store in the asset-scan worker.
//
// THE SAME DEFECT CLASS AS THE MEDIA HANDLER, ON A WORSE GOROUTINE
//
//	cmd/jomform-api assigns `var store *httpapi.ObjectStore` (nil whenever S3
//	creds are unset) into assetgate.Handler.Store, an ObjectFetcher INTERFACE.
//	A nil *T in a non-nil interface makes `h.Store != nil` TRUE, so both guards
//	were skipped and Send called h.Store.GetBytes on a nil receiver.
//
//	The media route survived its version of this because the HTTP recoverer
//	caught the panic (a 500 + a panic_recovered log line). This one runs on the
//	email queue WORKER goroutine, which recovers nothing — the same shape that
//	killed the whole jomform-api process for the nil *email.Sender. So the
//	assertion here is "no panic at all", not "not a 500".
package assetgate

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
)

// nilStore models *httpapi.ObjectStore: a concrete type whose methods dereference
// the receiver. A nil *nilStore therefore SIGSEGVs exactly like a nil
// *objstore.ObjectStore does, which is what makes this test able to reproduce the
// real defect. (assetgate cannot reference httpapi — that import cycle is why
// ObjectFetcher exists in the first place.)
type nilStore struct {
	marker string
}

func (n *nilStore) GetBytes(context.Context, string) ([]byte, string, error) {
	// Reading a field off a nil receiver panics with a nil pointer dereference —
	// the production failure, modelled faithfully and not by hand.
	return []byte(n.marker), "text/javascript", nil
}

// typedNilFetcher is the production shape when S3 is unconfigured.
func typedNilFetcher() ObjectFetcher {
	var s *nilStore
	return s
}

// countingClassifier records whether the AI step ran. It must not: with no way to
// re-fetch the asset's bytes the gate cannot scan, and the fail-closed posture is
// to leave the asset unserved.
type countingClassifier struct{ called bool }

func (c *countingClassifier) ClassifyAsset(context.Context, string) (string, error) {
	c.called = true
	return "benign", nil
}

// TestAssetScanTypedNilStoreDoesNotPanic is the reported shape, driven through
// the real Send. Pre-fix this panicked inside the fetch — an unrecoverable panic
// on the worker goroutine.
//
// Media is a non-nil store with a nil pool ON PURPOSE: it gets the handler PAST
// its `h.Media == nil` guard so the fetch branch is actually reached, and no
// database call happens before the panic site. A nil Media would return at the
// first guard and hide the bug — that is the "lazy probe looks clean" trap.
func TestAssetScanTypedNilStoreDoesNotPanic(t *testing.T) {
	store := typedNilFetcher()
	if store == nil {
		t.Fatal("test fixture is wrong: a nil *T in the interface must NOT be a nil interface " +
			"(that is the whole defect — if this is nil the test is vacuous)")
	}

	llm := &countingClassifier{}
	h := &Handler{Store: store, Media: dbstore.NewMediaStore(nil), LLM: llm}

	body, err := json.Marshal(ScanJob{MediaID: "m1", Key: "u/a/b.js", AccountID: "acct"})
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}

	// The job must be dropped, not retried, and the classifier must never be
	// asked to judge bytes that were never fetched.
	if err := h.Send(context.Background(), emailqueue.Job{Type: JobType, Body: string(body)}); err != nil {
		t.Fatalf("Send with an unconfigured store err = %v, want nil (drop the job, no retry ladder)", err)
	}
	if llm.called {
		t.Error("the classifier judged an asset whose bytes could not be fetched — fail-open")
	}
}

// TestHandlerFetcherUnwrapsTypedNil pins the guard itself, for both shapes. This
// is the assertion that was wrong in production: `h.Store != nil` said "there is a
// store" about a nil pointer.
func TestHandlerFetcherUnwrapsTypedNil(t *testing.T) {
	typedNil := &Handler{Store: typedNilFetcher()}
	if typedNil.fetcher() != nil {
		t.Error("fetcher() on a typed-nil store returned non-nil — the fetch would run on a nil receiver")
	}

	plainNil := &Handler{}
	if plainNil.fetcher() != nil {
		t.Error("fetcher() on a nil interface returned non-nil")
	}

	live := &Handler{Store: &nilStore{marker: "ok"}}
	if live.fetcher() == nil {
		t.Error("fetcher() on a configured store returned nil — the guard is too broad and would " +
			"block scanning on a working deployment")
	}
	if _, _, err := live.fetcher().GetBytes(context.Background(), "u/a/b.js"); err != nil {
		t.Errorf("configured store GetBytes err = %v, want nil", err)
	}
}

// TestAssetScanNoStoreLogsFailClosed names the branch that replaces the panic, so
// a future change that deletes it fails here rather than silently serving an
// unscanned asset (or crashing the worker again).
func TestAssetScanNoStoreLogsFailClosed(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := &Handler{Store: typedNilFetcher(), Media: dbstore.NewMediaStore(nil), LLM: &countingClassifier{}}
	body, _ := json.Marshal(ScanJob{MediaID: "m1", Key: "u/a/b.js", AccountID: "acct"})

	if err := h.Send(context.Background(), emailqueue.Job{Type: JobType, Body: string(body)}); err != nil {
		t.Fatalf("Send err = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "assetgate_no_store") {
		t.Fatalf("the unconfigured-store branch did not log assetgate_no_store; log:\n%s", buf.String())
	}
}
