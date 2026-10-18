package version

// This file guards the build-time wiring of the values above (card t_633befca).
//
// WHY IT EXISTS. `Date` shipped as the literal `unknown` on prod for a whole
// release train: `deploy/build-and-ship.sh` computed BUILD_DATE, logged it and
// passed `--build-arg BUILD_DATE=...` to `docker build`, the Dockerfile never
// declared that ARG and never added `-X ...version.Date`, so the build arg was
// accepted and silently dropped. https://api.jomform.com/version answered
// {"commit":"…","date":"unknown","version":"…"} for v1.21.139 while
// `docker image inspect` carried the real timestamp in the OCI label — the
// provenance chain looked complete everywhere except in the binary.
//
// WHAT IT PINS. For EVERY `Name = "default"` variable declared in version.go:
//
//  1. api/Dockerfile contains `-X <pkg>.Name=<arg-ref>` (so the variable is
//     actually stamped into the binary, not just defaulted), and
//  2. the ARG that value is read from is declared in the Dockerfile, and
//  3. its ARG default is byte-identical to the Go default, so a bare
//     `docker build` with no --build-arg still reports dev/none/unknown
//     instead of an empty string.
//
// The check is driven off version.go rather than a hand-written list of three
// fields: adding a fourth stamped value without wiring it fails the build the
// same way, which is the rot this file is meant to stop. The regexes read the
// two real files (no copies), so the test cannot pass against a stale copy.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const (
	// The package path the Dockerfile's -X flags must target.
	ldflagPkg = "github.com/wzul/jomform/api/internal/version"

	// Relative to this directory (api/internal/version) and to api/.
	versionGoPath  = "version.go"
	dockerfilePath = "../../Dockerfile"
)

// versionVarDecl matches `Name = "default"` inside version.go's var block.
var versionVarDecl = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)

// dockerfileArg matches `ARG NAME` / `ARG NAME=default` in the Dockerfile.
var dockerfileArg = regexp.MustCompile(`(?m)^\s*ARG\s+([A-Za-z_][A-Za-z0-9_]*)(?:=([^\s]*))?`)

// ldflagEntry matches one `-X <pkg>.<Field>=<value>` pair.
var ldflagEntry = regexp.MustCompile(`-X\s+([^\s=]+)\.([A-Za-z_][A-Za-z0-9_]*)=([^\s]+)`)

// argRef matches a Dockerfile ARG reference, `$NAME` or `${NAME}`.
var argRef = regexp.MustCompile(`^\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))$`)

// pkgDir resolves the directory this test file lives in, wherever `go test` was
// invoked from.
func pkgDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed: cannot locate version.go / Dockerfile to guard")
	}
	return filepath.Dir(file)
}

func readGuardedFile(t *testing.T, path, what string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		// A missing file is a FAILURE, never a skip: a guard that reads nothing
		// proves nothing.
		t.Fatalf("cannot read %s (%s): %v — this test guards the build-time wiring of the version stamps", what, path, err)
	}
	return string(b)
}

// TestVersionVarsAreStampedByTheDockerfile is the guard described at the top of
// this file.
func TestVersionVarsAreStampedByTheDockerfile(t *testing.T) {
	dir := pkgDir(t)
	versionGo := readGuardedFile(t, filepath.Join(dir, versionGoPath), "the version variables")
	dockerfile := readGuardedFile(t, filepath.Join(dir, dockerfilePath), "the api image build recipe")

	// 1. Every stamped value declared in version.go, with its default.
	goDefaults := map[string]string{}
	for _, m := range versionVarDecl.FindAllStringSubmatch(versionGo, -1) {
		goDefaults[m[1]] = m[2]
	}
	if len(goDefaults) == 0 {
		t.Fatalf("%s declares no `Name = \"default\"` variables — the guard would prove nothing", versionGoPath)
	}
	names := make([]string, 0, len(goDefaults))
	for name := range goDefaults {
		names = append(names, name)
	}
	sort.Strings(names)

	// 2. Every ARG the Dockerfile declares, with its default.
	argDefaults := map[string]string{}
	for _, m := range dockerfileArg.FindAllStringSubmatch(dockerfile, -1) {
		argDefaults[m[1]] = m[2]
	}

	// 3. Every -X for THIS package: field -> the ARG it reads from.
	stampedTo := map[string]string{}
	for _, m := range ldflagEntry.FindAllStringSubmatch(dockerfile, -1) {
		pkg, field, value := m[1], m[2], m[3]
		// The last -X in the quoted -ldflags value ends at the closing quote (and
		// possibly the Dockerfile's trailing backslash), which `[^\s]+` swallows.
		value = strings.TrimRight(value, `\"`)
		if pkg != ldflagPkg {
			continue
		}
		// A literal value is not provenance: `-X ...Date=unknown` would pin the
		// same string the package already defaults to and hide a dropped
		// --build-arg forever.
		ref := argRef.FindStringSubmatch(value)
		if ref == nil {
			t.Errorf("api/Dockerfile wires %s.%s to the literal %q, not to an ARG reference ($NAME or ${NAME}); "+
				"a literal cannot carry the value deploy/build-and-ship.sh passes as --build-arg", ldflagPkg, field, value)
			continue
		}
		argName := ref[1]
		if argName == "" {
			argName = ref[2]
		}
		if prev, dup := stampedTo[field]; dup && prev != argName {
			t.Errorf("api/Dockerfile wires %s.%s twice, from both $%s and $%s", ldflagPkg, field, prev, argName)
			continue
		}
		stampedTo[field] = argName
	}

	// 4. version.go must not declare a stamped value the binary cannot receive.
	for _, name := range names {
		argName, ok := stampedTo[name]
		if !ok {
			t.Errorf("api/Dockerfile never stamps version.%s: add `-X %s.%s=${SOME_ARG}` to the go build -ldflags "+
				"line, plus `ARG SOME_ARG=<default>` whose default equals the Go default (%q). "+
				"Without it, /version reports the package default on a real release (this is the date:\"unknown\" bug).",
				name, ldflagPkg, name, goDefaults[name])
			continue
		}

		argDefault, declared := argDefaults[argName]
		if !declared {
			t.Errorf("api/Dockerfile reads $%s for version.%s but never declares `ARG %s=<default>`: "+
				"`docker build --build-arg %s=...` accepts the value and silently drops it", argName, name, argName, argName)
			continue
		}

		// 5. Defaults must match, so a bare `docker build` (no --build-arg)
		// reports exactly what the package defaults to.
		if argDefault != goDefaults[name] {
			t.Errorf("version.%s defaults to %q in go, but api/Dockerfile's `ARG %s=%s` defaults to %q: "+
				"a bare `docker build` would now report a different value than the package default",
				name, goDefaults[name], argName, argDefault, argDefault)
		}
	}

	// 6. No -X may target a field version.go does not declare — that is how a
	// typo (version.Date vs version.DATE) stays green while stamping nothing.
	declaredGo := map[string]bool{}
	for _, name := range names {
		declaredGo[name] = true
	}
	stamped := make([]string, 0, len(stampedTo))
	for field := range stampedTo {
		stamped = append(stamped, field)
	}
	sort.Strings(stamped)
	for _, field := range stamped {
		if !declaredGo[field] {
			t.Errorf("api/Dockerfile stamps %s.%s, but version.go declares no such variable (declared: %s)",
				ldflagPkg, field, strings.Join(names, ", "))
		}
	}
}

// TestVersionDefaultsMatchTheDocumentedDefaults pins the three defaults a bare
// build reports. The Dockerfile guard above keeps the two files in step; this
// keeps the pair from silently becoming "", which is what an unwired -X looks
// like on the wire (an empty JSON field instead of "unknown").
func TestVersionDefaultsMatchTheDocumentedDefaults(t *testing.T) {
	cases := map[string]struct{ got, want string }{
		"Version": {Version, "dev"},
		"Commit":  {Commit, "none"},
		"Date":    {Date, "unknown"},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("version.%s = %q, want %q (the documented default a bare `docker build` reports)", name, c.got, c.want)
		}
	}
}
