package httpapi

// Card t_fc5cecbd — the CLASS guard for the unlock limiter.
//
// The behavioural guards in unlock_ratelimit_test.go prove the three CURRENT
// unlock routes are limited, but they enumerate those routes from a hand-written
// list, so a FOURTH `POST .../unlock` route added later would not be checked by
// them at all. That is the same shape of gap the board already had to fix once
// for the gate routes (the hand-written case list in
// TestEveryGateRouteHasItsOwnUnlockRoute): a guard that is only as wide as a
// human remembering to extend it is not the mechanical guard it claims to be.
//
// This file closes it from the other direction. It reads the package's own
// route registrations and asserts that EVERY `.../unlock` registration goes
// through middleware.RateLimit — no case list to maintain, so a new unlock route
// cannot be added unwrapped without failing here.
//
// It is deliberately a SOURCE scan rather than a live-request probe: a probe has
// to know the route's path, and knowing the path is exactly what a new route
// would not be in the list of. Reading the registrations makes the guard follow
// the code instead of a copy of it. The scan is asserted non-vacuous (it must
// find the existing unlock routes) so a broken regex cannot quietly turn it into
// a no-op that always passes.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// unlockRegistrationRe matches one mux registration whose pattern string ends in
// "/unlock", capturing the whole statement:
//
//	mux.Handle("POST /{slug}/unlock", middleware.RateLimit(http.HandlerFunc(h.handleFormUnlock)))
//	mux.HandleFunc("POST /embed/f/{wsslug}/{slug}/unlock", h.embedUnlock)
var unlockRegistrationRe = regexp.MustCompile(
	`mux\.(HandleFunc|Handle)\(\s*"[A-Z]+ +[^"]*/unlock"\s*,.*`)

// TestEveryUnlockRouteIsWrappedAtRegistration is the mechanical guard: no
// production registration of a `.../unlock` route may bypass middleware.RateLimit.
func TestEveryUnlockRouteIsWrappedAtRegistration(t *testing.T) {
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve package dir: %v", err)
	}
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", pkgDir, err)
	}

	var found, unwrapped []string
	for _, e := range entries {
		name := e.Name()
		// Production sources only. A TEST may legitimately register an
		// UNWRAPPED route as a control (to prove the limiter is what throttles),
		// so scanning _test.go files would make this guard fire on its own
		// fixtures rather than on production.
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(pkgDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if !unlockRegistrationRe.MatchString(line) {
				continue
			}
			entry := fmt.Sprintf("%s: %s", name, line)
			found = append(found, entry)
			if !strings.Contains(line, "middleware.RateLimit") {
				unwrapped = append(unwrapped, entry)
			}
		}
	}

	// Non-vacuity: the routes this card is about must be in the scan, or the
	// regex stopped matching and this guard proves nothing.
	if len(found) < 3 {
		t.Fatalf("found only %d `.../unlock` registration(s) in the package's production sources — "+
			"the scan is broken, so this guard would pass vacuously. Found:\n  %s",
			len(found), strings.Join(found, "\n  "))
	}

	if len(unwrapped) > 0 {
		t.Fatalf("%d `.../unlock` route(s) are registered WITHOUT middleware.RateLimit.\n"+
			"An unlock is a password oracle — it runs an argon2id check against the form's real\n"+
			"access hash and answers 403/200 — so an unwrapped one is an unlimited online\n"+
			"guessing surface. Register it the way /order is:\n"+
			"  mux.Handle(\"POST <pattern>\", middleware.RateLimit(http.HandlerFunc(h.<handler>)))\n"+
			"Offending registration(s):\n  %s", len(unwrapped), strings.Join(unwrapped, "\n  "))
	}

	t.Logf("%d `.../unlock` registration(s) scanned, all RateLimit-wrapped:\n  %s",
		len(found), strings.Join(found, "\n  "))
}
