// Package niliface answers one question: is this interface value nil — including
// a TYPED nil (a nil *T stored inside a non-nil interface)?
//
// WHY THIS PACKAGE EXISTS
//
//	It is a leaf, imported by both httpapi and assetgate, because both hold the
//	SAME concrete pointer behind an interface and both were defeated by it:
//
//	  *objstore.ObjectStore  →  httpapi.RESTHandlers.Store / PublicHandlers.Store
//	                            (ObjectStoreIface)
//	                         →  assetgate.Handler.Store (ObjectFetcher)
//
//	cmd/jomform-api leaves that pointer nil when S3 creds are absent, and a nil
//	*T in an interface makes `field == nil` FALSE — so every downstream guard was
//	skipped and the call ran on a nil receiver. Two consequences were reproduced
//	on the built binary: GET /media/u/<id>/<file> answered 500 + a recovered
//	panic instead of the intended 503, and the asset-scan worker's unguarded
//	fetch would have killed the whole process (the worker goroutine recovers
//	nothing).
//
//	A second hand-rolled reflect switch in assetgate is exactly how those two
//	guards would drift apart, so the rule lives here once and callers name their
//	intent on top of it (httpapi.storeUnavailable, httpapi.mailerUnavailable).
package niliface

import "reflect"

// IsNil reports whether v is a nil interface, or holds a nil pointer-like value
// (pointer, interface, func, chan, map or slice).
//
// A non-pointer value (struct, string, int, …) is never nil, so IsNil is false
// for it.
func IsNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Chan, reflect.Map, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
