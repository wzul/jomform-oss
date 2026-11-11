package version

// This file guards the Go TOOLCHAIN the api is built with (card t_b068e74f).
//
// WHY IT EXISTS. Go 1.27.3 (and 1.26.10) change crypto/x509 so the verifier
// REJECTS any certificate chain containing eight distrusted eMudhra
// intermediate CA keys that CCADB revoked for cACompromise on 2026-10-18/19
// (CVE-2026-111943, go.dev/issue/82345). A binary built with 1.27.2 or earlier
// still ACCEPTS those chains, so every outbound TLS call it makes — CHIP, SES,
// S3/MinIO, Cloudflare, MCP OAuth providers, and MERCHANT-CONFIGURED WEBHOOK
// URLs, which can be any host in the world — can be MITM'd through one of them.
// Inbound TLS is not the exposure: Traefik terminates it and the api is not the
// TLS server.
//
// FLOOR 1.27.4. Go 1.27.4 is also the first release that fixes
// GO-2026-6859 / CVE-2026-97054 (published 2026-11-11): the HTTP/1 MaxBytesReader
// path sets `Connection: close` unsynchronised, so a handler that reads the
// request body while writing a response header can panic. govulncheck reds this
// module on a 1.27.3 toolchain — "Found in: net/http@go1.27.3 / Fixed in:
// net/http@go1.27.4", 87 affected net/http symbols, i.e. the whole server surface
// this api is — so the 1.27.3 floor no longer clears the gate. The floor moves
// with the pins: go.mod's `toolchain` line, api/Dockerfile's base image and this
// constant are raised in one commit.
//
// WHY A TEST AND NOT JUST THE `govuln` GATE. Measured 2026-10-21 with 1.27.2
// pinned: govulncheck printed "No vulnerabilities found" — the advisory DB had
// not caught up with the fix — so the gate whose job is to force a stdlib bump
// would have let 1.27.2 ship green. Nothing else in the repo reads the toolchain
// pin, so the pin has to be checked here or it regresses silently.
//
// WHAT IT PINS, both the shipped and the host toolchain:
//
//  1. api/Dockerfile's `FROM golang:<tag>` — the toolchain the SHIPPED binary is
//     built against (the image runs GOTOOLCHAIN=local, so go.mod's `toolchain`
//     line does not apply inside the build stage). Every golang base image line
//     must be at or above the floor.
//  2. api/go.mod's `toolchain` line — and, if that line is absent, its `go`
//     directive — the toolchain the host `build`/`vet`/`test`/`govuln` gates
//     compile and run under. A 1.27.2 host toolchain means anything that depends
//     on the patched stdlib is exercised on the unpatched one.
//
// ACCEPTED SHAPE: any release at or above the floor, so 1.28.x and later pass
// without touching this file — this pins a FLOOR, not one exact release. 1.26.10
// carries the same crypto/x509 fix but is NOT accepted: api/go.mod declares
// `go 1.27.0`, so a 1.26 toolchain cannot build this module at all.
//
// A VERSION IT CANNOT READ IS A FAILURE, NEVER A SKIP. A floating tag (`1.27`,
// no patch), a tag behind a build arg (`golang:${GO_VERSION}-alpine`) and a
// prerelease tag (`1.27.3rc1`) are all refused WITH THE REASON NAMED, because a
// guard that silently passes what it cannot compare proves nothing about it —
// and the compare itself is numeric per component, so 1.27.10 > 1.27.9 (a
// string compare gets that backwards and would red a legitimate bump).

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// minPatchedGo is the first Go release that carries BOTH fixes this repo
	// depends on: crypto/x509 refusing chains through the eight distrusted
	// eMudhra intermediates (CVE-2026-111943) and the net/http MaxBytesReader
	// panic fix (GO-2026-6859 / CVE-2026-97054). Raise it in the same commit
	// that raises the pins.
	minPatchedGo = "1.27.4"

	// Relative to this directory (api/internal/version).
	goModPath = "../../go.mod"
)

var (
	// dockerfileFromGolang matches every `FROM golang:<tag>` line (any case, any
	// stage name). The tag is read up to whitespace so `AS build` is not part of
	// it.
	dockerfileFromGolang = regexp.MustCompile(`(?mi)^\s*FROM\s+golang:([^\s]+)`)

	// goModToolchain matches `toolchain go1.27.3`.
	goModToolchain = regexp.MustCompile(`(?m)^toolchain[ \t]+go(\S+)`)

	// goModGoDirective matches the `go 1.27.0` language-version line.
	goModGoDirective = regexp.MustCompile(`(?m)^go[ \t]+(\S+)`)

	// pinnedReleaseRe is a release with an explicit patch component: what a docker
	// tag has to carry to be a pin rather than a moving minor.
	pinnedReleaseRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

	// looseReleaseRe tolerates a missing patch (go.mod's `go 1.27` spelling).
	looseReleaseRe = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?$`)
)

// goRelease is a parsed Go release, compared per component — never as a string.
type goRelease struct {
	major, minor, patch int
}

func (v goRelease) String() string {
	return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) + "." + strconv.Itoa(v.patch)
}

// atLeast reports whether v is at or above o.
func (v goRelease) atLeast(o goRelease) bool {
	if v.major != o.major {
		return v.major > o.major
	}
	if v.minor != o.minor {
		return v.minor > o.minor
	}
	return v.patch >= o.patch
}

func parseRelease(s string, re *regexp.Regexp) (goRelease, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return goRelease{}, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return goRelease{}, false
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return goRelease{}, false
	}
	patch := 0
	if len(m) > 3 && m[3] != "" {
		if patch, err = strconv.Atoi(m[3]); err != nil {
			return goRelease{}, false
		}
	}
	return goRelease{major: major, minor: minor, patch: patch}, true
}

// parsePinnedRelease accepts only an exact X.Y.Z release.
func parsePinnedRelease(s string) (goRelease, bool) { return parseRelease(s, pinnedReleaseRe) }

// parseLooseRelease accepts X.Y as well, for the `go` directive.
func parseLooseRelease(s string) (goRelease, bool) { return parseRelease(s, looseReleaseRe) }

// dockerTagRelease strips what a docker tag carries around the version — a
// digest (`@sha256:…`) and a variant suffix (`-alpine`, `-alpine3.20`,
// `-bookworm`) — and parses what is left as an exact release.
func dockerTagRelease(tag string) (goRelease, string, bool) {
	if i := strings.IndexByte(tag, '@'); i >= 0 {
		tag = tag[:i]
	}
	version := tag
	if i := strings.IndexByte(version, '-'); i >= 0 {
		version = version[:i]
	}
	rel, ok := parsePinnedRelease(version)
	if !ok {
		return goRelease{}, version, false
	}
	return rel, version, true
}

// TestApiImageToolchainIsPatched is the shipped-binary half of the guard: the
// golang base image in api/Dockerfile must be at or above minPatchedGo.
func TestApiImageToolchainIsPatched(t *testing.T) {
	floor, ok := parsePinnedRelease(minPatchedGo)
	if !ok {
		t.Fatalf("minPatchedGo = %q is not an X.Y.Z release — the guard cannot compare anything against it", minPatchedGo)
	}

	dockerfile := readGuardedFile(t, filepath.Join(pkgDir(t), dockerfilePath), "the api image build recipe")

	matches := dockerfileFromGolang.FindAllStringSubmatch(dockerfile, -1)
	if len(matches) == 0 {
		t.Fatalf("api/Dockerfile has no `FROM golang:<tag>` line — either the image no longer builds from a golang base "+
			"(then this guard must be re-pointed at whatever now decides the toolchain) or the line was renamed and the "+
			"pin is now checked by nobody. The shipped binary's crypto/x509 behaviour depends on it: %s is the first "+
			"release that refuses the eight distrusted eMudhra intermediates (CVE-2026-111943).", minPatchedGo)
	}

	for _, m := range matches {
		tag := m[1]
		rel, version, pinned := dockerTagRelease(tag)
		if !pinned {
			t.Errorf("api/Dockerfile builds from `golang:%s`, whose version %q is not a literal X.Y.Z release, so the "+
				"toolchain the shipped binary is built with cannot be checked. Pin an exact patched tag "+
				"(e.g. `golang:%s-alpine`); a floating tag, a prerelease tag or a build-arg reference all defeat this guard.",
				tag, version, minPatchedGo)
			continue
		}
		if !rel.atLeast(floor) {
			t.Errorf("api/Dockerfile builds from `golang:%s` (go%s), below the patched floor go%s. A binary built with an "+
				"older toolchain still ACCEPTS certificate chains through the eight eMudhra intermediates revoked for "+
				"cACompromise (CVE-2026-111943, go.dev/issue/82345) on every outbound TLS call — CHIP, SES, S3/MinIO, "+
				"Cloudflare, MCP OAuth and merchant-configured webhook URLs — and still carries the unsynchronised "+
				"HTTP/1 MaxBytesReader `Connection: close` panic (GO-2026-6859 / CVE-2026-97054). Bump the tag to golang:%s-alpine.",
				tag, rel, minPatchedGo, minPatchedGo)
		}
	}
}

// TestHostToolchainIsPatched is the other half: the toolchain the host gates
// compile and run under (api/go.mod) must also be at or above the floor, so a
// test that depends on the patched stdlib is not exercised on the unpatched one.
func TestHostToolchainIsPatched(t *testing.T) {
	floor, ok := parsePinnedRelease(minPatchedGo)
	if !ok {
		t.Fatalf("minPatchedGo = %q is not an X.Y.Z release — the guard cannot compare anything against it", minPatchedGo)
	}

	gomod := readGuardedFile(t, filepath.Join(pkgDir(t), goModPath), "the module's toolchain declaration")

	// The `toolchain` line is what the go command selects; it wins when present.
	if m := goModToolchain.FindStringSubmatch(gomod); m != nil {
		rel, parsed := parsePinnedRelease(m[1])
		if !parsed {
			t.Errorf("api/go.mod declares `toolchain go%s`, which is not an X.Y.Z release, so the host gate toolchain "+
				"cannot be checked. Pin an exact patched toolchain (`toolchain go%s`).", m[1], minPatchedGo)
			return
		}
		if !rel.atLeast(floor) {
			t.Errorf("api/go.mod declares `toolchain go%s`, below the patched floor go%s: the api's `build`/`vet`/`test`/"+
				"`govuln` gates compile and run on that stdlib, so anything depending on the 1.27.3 crypto/x509 fix "+
				"(CVE-2026-111943) or the 1.27.4 net/http MaxBytesReader fix (GO-2026-6859 / CVE-2026-97054) is "+
				"exercised on the unpatched one. Raise it to `toolchain go%s` — and keep the "+
				"golang image tag in api/Dockerfile in step, which is what ships.", rel, minPatchedGo, minPatchedGo)
		}
		return
	}

	// No toolchain line: the `go` directive alone decides the minimum the go
	// command will accept, so it has to clear the floor itself.
	m := goModGoDirective.FindStringSubmatch(gomod)
	if m == nil {
		t.Fatalf("api/go.mod declares neither a `toolchain` line nor a `go` directive — the host gate toolchain is "+
			"unpinned and unchecked. Add `toolchain go%s`.", minPatchedGo)
	}
	rel, parsed := parseLooseRelease(m[1])
	if !parsed {
		t.Errorf("api/go.mod's `go %s` directive is not a parseable release, so the host gate toolchain cannot be "+
			"checked. Add `toolchain go%s`.", m[1], minPatchedGo)
		return
	}
	if !rel.atLeast(floor) {
		t.Errorf("api/go.mod has no `toolchain` line and its `go %s` directive is below the patched floor go%s: the go "+
			"command would run the host gates on an unpatched stdlib (CVE-2026-111943 is fixed in go%s). Add "+
			"`toolchain go%s`.", m[1], minPatchedGo, minPatchedGo, minPatchedGo)
	}
}

// TestGoReleaseComparisonBites pins the guard's own machinery, because a
// comparison bug is invisible in a green run: a lexicographic compare would
// reject 1.27.10 while accepting 1.27.2, i.e. exactly backwards. Every case here
// is a shape the two guards above feed it.
func TestGoReleaseComparisonBites(t *testing.T) {
	floor, _ := parsePinnedRelease(minPatchedGo)

	accepted := []string{"1.27.4", "1.27.10", "1.28.0", "1.30.1", "2.0.0"}
	for _, s := range accepted {
		rel, ok := parsePinnedRelease(s)
		if !ok {
			t.Errorf("parsePinnedRelease(%q) refused an exact X.Y.Z release", s)
			continue
		}
		if !rel.atLeast(floor) {
			t.Errorf("go%s is at or above the go%s floor but atLeast() said no — a numeric compare cannot fail here", s, minPatchedGo)
		}
	}

	rejected := []string{"1.27.3", "1.27.2", "1.27.0", "1.26.10", "1.26.99", "1.9.0", "0.27.3"}
	for _, s := range rejected {
		rel, ok := parsePinnedRelease(s)
		if !ok {
			t.Errorf("parsePinnedRelease(%q) refused an exact X.Y.Z release", s)
			continue
		}
		if rel.atLeast(floor) {
			t.Errorf("go%s is BELOW the go%s floor but atLeast() said yes — a binary built with it still carries the "+
				"stdlib defects the floor clears (the revoked eMudhra intermediates, CVE-2026-111943, and the net/http "+
				"MaxBytesReader panic, GO-2026-6859)", s, minPatchedGo)
		}
	}

	// Shapes that must be REFUSED rather than silently compared: a missing patch,
	// a prerelease, and a non-version.
	unparseable := []string{"1.27", "1.27.3rc1", "1.27.3beta2", "latest", "${GO_VERSION}", ""}
	for _, s := range unparseable {
		if rel, ok := parsePinnedRelease(s); ok {
			t.Errorf("parsePinnedRelease(%q) accepted %v as an exact release — a guard that compares a floating or "+
				"prerelease tag reports on a pin it did not verify", s, rel)
		}
	}

	// The docker-tag normaliser: variant suffixes and digests are stripped, the
	// version itself must survive exactly.
	cases := map[string]string{
		"1.27.3-alpine":               "1.27.3",
		"1.27.3-alpine3.20":           "1.27.3",
		"1.27.3-bookworm":             "1.27.3",
		"1.27.3":                      "1.27.3",
		"1.27.3-alpine@sha256:abc123": "1.27.3",
		"1.27-alpine":                 "",
		"1.27.2-alpine":               "1.27.2",
	}
	for tag, wantVersion := range cases {
		rel, version, ok := dockerTagRelease(tag)
		switch wantVersion {
		case "":
			if ok {
				t.Errorf("dockerTagRelease(%q) accepted %v — a tag with no patch component is not a pin", tag, rel)
			}
		default:
			if !ok {
				t.Errorf("dockerTagRelease(%q) refused a plain `version-variant` tag (version %q)", tag, version)
				continue
			}
			if rel.String() != wantVersion {
				t.Errorf("dockerTagRelease(%q) = %s, want %s", tag, rel, wantVersion)
			}
		}
	}
}
