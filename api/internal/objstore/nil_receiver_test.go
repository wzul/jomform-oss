// Regression test for the nil-receiver trap in the media path.
//
// WHAT WAS BROKEN, AND HOW
//   cmd/jomform-api holds `var store *ObjectStore`, nil whenever S3_ACCESS_KEY/
//   S3_SECRET_KEY are unset, and assigns it into INTERFACE fields
//   (httpapi.RESTHandlers.Store, httpapi.PublicHandlers.Store,
//   assetgate.Handler.Store). A nil *T stored in an interface is NOT a nil
//   interface: every `if store == nil` guard downstream was false and the call
//   ran on a nil receiver, dereferencing o.client → SIGSEGV at addr=0x0.
//
//   On the live binary (this head, S3 unset) a valid key shape reproduced it:
//     GET /media/u/00000000-0000-0000-0000-000000000000/file.jpg
//       → 500 {"error":"internal error"} + one ERROR panic_recovered
//                ... error_class=panic
//   The intended answer was the 503 "media storage tidak dikonfigurasi" the
//   handler's own guard writes. A bad key did NOT reproduce it (400 from the
//   key validator, before the nil receiver is reached) — which is why a lazy
//   probe of this bug looks clean.
//
// This file is the BOTTOM layer: whatever shape the wiring takes, a method on an
// unconfigured store must answer a sentinel rather than dereference itself.
// api/internal/httpapi/typed_nil_objectstore_test.go covers the edge layer
// (guards + HTTP status).
package objstore

import (
	"context"
	"errors"
	"testing"
)

// TestNilReceiverAnswersErrNotConfigured pins the contract for every ObjectStore
// method that the handlers and the asset-scan worker call. Before the fix each
// of these panicked with a nil pointer dereference when the receiver was nil.
func TestNilReceiverAnswersErrNotConfigured(t *testing.T) {
	var o *ObjectStore // the exact production shape when S3 is unconfigured
	ctx := context.Background()

	if err := o.PutObject(ctx, "u/a/b.jpg", "image/jpeg", []byte("x")); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("PutObject on a nil store err = %v, want ErrNotConfigured "+
			"(a nil receiver here dereferenced o.client and panicked)", err)
	}
	if _, _, err := o.GetObject(ctx, "u/a/b.jpg"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetObject on a nil store err = %v, want ErrNotConfigured", err)
	}
	if _, _, err := o.GetBytes(ctx, "u/a/b.jpg"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetBytes on a nil store err = %v, want ErrNotConfigured "+
			"(the asset-scan worker's ObjectFetcher path)", err)
	}
	if err := o.DeleteObject(ctx, "u/a/b.jpg"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("DeleteObject on a nil store err = %v, want ErrNotConfigured", err)
	}
	if err := o.EnsureBucket(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("EnsureBucket on a nil store err = %v, want ErrNotConfigured", err)
	}

	// PublicURL has no error to return, so its contract is "no base → empty",
	// not an empty-but-plausible URL built from a nil receiver.
	if got := o.PublicURL("u/a/b.jpg"); got != "" {
		t.Errorf("PublicURL on a nil store = %q, want %q", got, "")
	}
}

// TestConfiguredStoreIsUnaffected guards the other direction: the nil guards must
// not turn a WORKING store into ErrNotConfigured. A store built by
// NewObjectStore answers normally (here: a PublicURL, which needs no network).
func TestConfiguredStoreIsUnaffected(t *testing.T) {
	o, err := NewObjectStore("http://127.0.0.1:9000", "k", "s", "media-bucket", "us-east-1", "https://jomform.com/media")
	if err != nil {
		t.Fatalf("NewObjectStore: %v", err)
	}
	if got, want := o.PublicURL("u/a/b.jpg"), "https://jomform.com/media/u/a/b.jpg"; got != want {
		t.Errorf("PublicURL on a configured store = %q, want %q", got, want)
	}
	if errors.Is(o.DeleteObject(context.Background(), "u/a/b.jpg"), ErrNotConfigured) {
		t.Error("a configured store reported ErrNotConfigured — the nil guard is too broad")
	}
}
