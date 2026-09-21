// Typed-nil guards for the handler dependencies that are stored in INTERFACE
// fields.
//
// WHY THIS FILE EXISTS
//   cmd/jomform-api builds concrete pointers and hands them into interface
//   fields (VerifyMailer, ObjectStoreIface, assetgate.ObjectFetcher). When the
//   pointer is nil — SES unconfigured, S3_* unset — the interface field is NOT
//   nil: it holds a nil *T. Every `if field == nil` guard downstream is then
//   false and the call runs on a nil receiver, which dereferences its own
//   fields and panics.
//
//   That was not theoretical here. It took the whole API process down once
//   (nil *email.Sender on the email queue worker goroutine, which recovers
//   nothing) and it produced a visitor-reachable 500 + recovered panic on
//   GET /media/u/<id>/<file> a second time (nil *objstore.ObjectStore behind
//   ObjectStoreIface).
//
//   The gates below are the EDGE layer of the two-layer shape: cmd/jomform-api
//   no longer stores a typed nil, and these gates make the guards' intent hold
//   for either shape, so a future wiring change cannot reintroduce the crash.
//   The BOTTOM layer is the nil-receiver sentinel in the leaf packages
//   (email.ErrNotConfigured, objstore.ErrNotConfigured).
//
//   The reflect rule itself lives in internal/niliface, one implementation
//   shared with the asset-scan worker (assetgate.Handler.Store, whose guard was
//   defeated by the same typed nil) — a second hand-rolled copy is how the two
//   guards drift apart.
package httpapi

import "github.com/wzul/jomform/api/internal/niliface"

// nilInterface reports whether v is a nil interface OR holds a typed nil (a nil
// *T stored in a non-nil interface).
func nilInterface(v any) bool { return niliface.IsNil(v) }

// storeUnavailable reports whether an object store cannot serve anything: a nil
// ObjectStoreIface, or a typed nil (a nil *ObjectStore held in the interface).
// It is the media equivalent of mailerUnavailable.
//
// cmd/jomform-api no longer stores a nil *ObjectStore in these fields, so the
// typed nil should not occur in production; this is the layer that makes the
// guard's intent ("no store → 503 media storage tidak dikonfigurasi") hold for
// EITHER shape.
func storeUnavailable(s ObjectStoreIface) bool { return niliface.IsNil(s) }

// mailerUnavailable reports whether a VerifyMailer cannot deliver anything: a
// nil interface, or a typed nil (a nil *email.Sender held in the interface).
func mailerUnavailable(m VerifyMailer) bool { return niliface.IsNil(m) }
