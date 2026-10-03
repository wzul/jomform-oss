// Regression test for the typed-nil ObjectStoreIface in the media path.
//
// WHAT WAS BROKEN
//
//	cmd/jomform-api assigned a nil *httpapi.ObjectStore into the
//	ObjectStoreIface fields RESTHandlers.Store / PublicHandlers.Store whenever
//	S3_ACCESS_KEY/S3_SECRET_KEY were unset. A nil *T in a non-nil interface
//	makes `h.Store == nil` FALSE, so the guards at handleMedia (uploads.go),
//	handleAsset (public.go), the three upload handlers, listMedia and deleteMedia
//	were all skipped and the call reached (*objstore.ObjectStore).GetObject on a
//	nil receiver → SIGSEGV.
//
//	Reproduced on the built binary at this head, S3 unset, with a key shape that
//	gets PAST the key validator (a bad key 400s first and hides the bug):
//	  GET /media/u/00000000-0000-0000-0000-000000000000/file.jpg
//	    → 500 {"error":"internal error"}
//	    + ERROR panic_recovered err="runtime error: invalid memory address or nil
//	      pointer dereference" path=/media/u/... error_class=panic
//	The route's own guard (whose text is "media storage tidak dikonfigurasi")
//	was written to answer 503 and never ran.
//
// These tests drive the REAL handler through the production recoverer, so a
// regression answers the same 500 + panic_recovered the deployment did — not
// just a wrong status a unit check would tolerate.
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

// typedNilStore returns the EXACT production shape when S3 is unconfigured: a
// nil *ObjectStore stored in the ObjectStoreIface interface. A test that assigns
// a bare `nil` would pass against the broken code and prove nothing.
func typedNilStore() ObjectStoreIface {
	var store *ObjectStore
	return store
}

// TestTypedNilStoreIsTreatedAsUnconfigured pins the guard itself for both
// shapes: a typed nil must be reported unavailable exactly like a nil interface.
func TestTypedNilStoreIsTreatedAsUnconfigured(t *testing.T) {
	var plainNil ObjectStoreIface
	typedNil := typedNilStore()

	if typedNil == nil {
		t.Fatal("test fixture is wrong: a nil *ObjectStore in the interface must NOT be a nil interface " +
			"(that is the whole bug — if this is nil the test is vacuous)")
	}
	if !storeUnavailable(typedNil) {
		t.Error("storeUnavailable(nil *ObjectStore in the interface) = false, want true — " +
			"the guard must unwrap a typed nil or the handler calls into a nil receiver")
	}
	if !storeUnavailable(plainNil) {
		t.Error("storeUnavailable(nil interface) = false, want true")
	}
	if storeUnavailable(newFakeObjectStore()) {
		t.Error("storeUnavailable(configured store) = true — the guard is too broad and would 503 a working deployment")
	}
}

// mediaServeWithStore routes GET /media/{key} through the production middleware
// chain (the recoverer is what turned the panic into the reported 500) and
// returns the recorder plus whatever panic was recovered.
func mediaServeWithStore(t *testing.T, h *RESTHandlers, path string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	panicked := false
	// Same order as httpapi.Server.Handler(): recoverer sits inside Logger.
	handler := recoverer(mux)
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	}()
	return rec, panicked
}

// TestHandleMediaTypedNilStoreAnswers503NotPanic is the reported defect, driven
// as a real request. Pre-fix this produced a recovered panic and a 500; the key
// is a VALID 3-segment `u/...` key because a malformed one 400s before the nil
// receiver is reached and would hide the bug.
func TestHandleMediaTypedNilStoreAnswers503NotPanic(t *testing.T) {
	h := &RESTHandlers{Store: typedNilStore()}

	rec, panicked := mediaServeWithStore(t, h, "/media/u/00000000-0000-0000-0000-000000000000/file.jpg")

	if panicked {
		t.Fatal("GET /media/<valid key> with an unconfigured store PANICKED (recovered) — " +
			"that is the reported defect: a nil receiver reached because `h.Store == nil` was false")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /media/<valid key> with an unconfigured store = %d (%s), want 503 %q",
			rec.Code, strings.TrimSpace(rec.Body.String()), "media storage tidak dikonfigurasi")
	}
	if body := rec.Body.String(); !strings.Contains(body, "media storage tidak dikonfigurasi") {
		t.Fatalf("503 body = %q, want the guard's own message %q", body, "media storage tidak dikonfigurasi")
	}
}

// TestHandleMediaUnconfiguredMessageIsTheGuard pins the one-meaning rule: an
// unconfigured deployment answers the SAME 503 body whether the store reaches the
// handler as a typed nil, a nil interface, or (via the leaf sentinel) a store
// that reports ErrNotConfigured. Three shapes, one status.
func TestHandleMediaUnconfiguredMessageIsTheGuard(t *testing.T) {
	cases := []struct {
		name  string
		store ObjectStoreIface
	}{
		{"typed nil", typedNilStore()},
		{"nil interface", nil},
		{"sentinel", notConfiguredStore{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &RESTHandlers{Store: c.store}
			rec, panicked := mediaServeWithStore(t, h, "/media/u/00000000-0000-0000-0000-000000000000/file.jpg")
			if panicked {
				t.Fatal("recovered panic; want a plain 503")
			}
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d (%s), want 503", rec.Code, strings.TrimSpace(rec.Body.String()))
			}
			if !strings.Contains(rec.Body.String(), "media storage tidak dikonfigurasi") {
				t.Fatalf("body = %q, want the guard's message", rec.Body.String())
			}
		})
	}
}

// TestHandleAssetTypedNilStoreAnswers503NotPanic covers the second route that
// holds the same interface field (the workspace/custom-domain code-asset route).
// Its guard runs before tenant resolution, so it needs no database.
func TestHandleAssetTypedNilStoreAnswers503NotPanic(t *testing.T) {
	h := &PublicHandlers{Store: typedNilStore()}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/assets/u/00000000-0000-0000-0000-000000000000/a.css", nil)
	req.Host = "any-workspace.jomform.com"
	rec := httptest.NewRecorder()

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		recoverer(mux).ServeHTTP(rec, req)
	}()

	if panicked {
		t.Fatal("GET /assets/<key> with an unconfigured store PANICKED (recovered); want a plain 503")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /assets/<key> with an unconfigured store = %d (%s), want 503",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}

// notConfiguredStore is a store that exists but reports itself unconfigured, the
// shape the leaf sentinel produces. It is the third way the same 503 must be
// reached; without the errors.Is mapping in the handlers it would 500.
type notConfiguredStore struct{}

func (notConfiguredStore) GetObject(context.Context, string) ([]byte, string, error) {
	return nil, "", objstore.ErrNotConfigured
}
func (notConfiguredStore) PutObject(context.Context, string, string, []byte) error {
	return objstore.ErrNotConfigured
}
func (notConfiguredStore) PublicURL(string) string { return "" }
func (notConfiguredStore) DeleteObject(context.Context, string) error {
	return objstore.ErrNotConfigured
}

// ObjectExists is part of the interface, so the "exists but unconfigured" third
// shape has to answer it too and keep answering ErrNotConfigured — a restore
// must treat "cannot tell" the same way it treats this store everywhere else.
func (notConfiguredStore) ObjectExists(context.Context, string) (bool, error) {
	return false, objstore.ErrNotConfigured
}
