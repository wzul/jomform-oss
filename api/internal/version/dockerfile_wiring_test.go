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
// real files (no copies), so the test cannot pass against a stale copy.
//
// THE THIRD FILE, AND WHY IT IS READ HERE (card t_7184eede). A Dockerfile that
// declares every ARG and wires every -X can still ship a binary reporting
// dev/none/unknown, because `docker build --build-arg X=…` accepts a value no
// step passes and no step requires. The release script is the only place those
// values are ever set, so this file reads it too and fails when a stamp the
// Dockerfile needs is not passed as --build-arg. That read is ALSO what makes
// `scripts/preflight.sh`'s diff classifier select the `test` gate for a change
// to that script: the classifier derives a gate's inputs from repo-relative
// literals in its sources, so the literal below is load-bearing, not decoration.

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

	// shipScriptPath is REPO-relative, exactly as a human names the file.
	// Two reasons it is written out rather than built from `..` segments: a
	// failure message should print the path you go and edit, and
	// scripts/preflight.sh derives a gate's inputs from repo-relative literals
	// in that gate's sources — this literal is what ties the release script to
	// the `test` gate. Built from `..` segments it would still resolve on disk,
	// but the derivation would see an opaque expression and claim nothing.
	shipScriptPath = "deploy/build-and-ship.sh"
)

// versionVarDecl matches `Name = "default"` inside version.go's var block.
var versionVarDecl = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)

// dockerfileArg matches `ARG NAME` / `ARG NAME=default` in the Dockerfile.
var dockerfileArg = regexp.MustCompile(`(?m)^\s*ARG\s+([A-Za-z_][A-Za-z0-9_]*)(?:=([^\s]*))?`)

// ldflagEntry matches one `-X <pkg>.<Field>=<value>` pair.
var ldflagEntry = regexp.MustCompile(`-X\s+([^\s=]+)\.([A-Za-z_][A-Za-z0-9_]*)=([^\s]+)`)

// argRef matches a Dockerfile ARG reference, `$NAME` or `${NAME}`.
var argRef = regexp.MustCompile(`^\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))$`)

// shipBuildArg matches one `--build-arg NAME=value` in the release script,
// including the shell-quoted form that script actually uses, `--build-arg
// "NAME=$VALUE"`: the value stops at the closing quote, so `$VERSION"` captures
// as `$VERSION`, and an empty `NAME=""` captures as "" (rejected below — an
// empty stamp is the no-value shape, not a provenance value).
var shipBuildArg = regexp.MustCompile(`--build-arg[ \t]+"?([A-Za-z_][A-Za-z0-9_]*)=([^"\s]*)`)

// shipBuildArgsArray is the array the release script puts those flags in. Both
// of its `docker build` call sites (local, and the command shipped over ssh)
// expand it, and that expansion is what carries every --build-arg into the
// build. Asserted as a PRESENCE, not as a call-site analysis: this file reads
// bash by regex, and following a `\`-continued invocation plus a command inside
// a quoted ssh argument would be a brittle reading of formatting rather than of
// behaviour.
const shipBuildArgsArray = "${BUILD_ARGS[@]}"

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

// TestReleaseScriptPassesTheStampsTheDockerfileRequires closes the last hop of
// the provenance chain (card t_7184eede). The test above keeps version.go and
// api/Dockerfile in step, but the value a release actually ships is the one
// `deploy/build-and-ship.sh` hands to `docker build`: an ARG nobody passes
// silently takes its default, and the binary then reports dev/none/unknown with
// every other link of the chain looking complete. That is exactly how `Date`
// shipped as `unknown` for a whole release train.
//
// WHAT IT PINS. For every ARG an `-X <pkg>.<Field>=$ARG` reads out of api/Dockerfile:
//
//  1. the release script passes it as `--build-arg ARG=...`,
//  2. the value is non-empty, and
//  3. the value references a computed value rather than being a literal (a
//     literal stamp would label every release with the same value),
//
// and the script must expand its build-args array, so a flag that is defined
// but never handed to `docker build` is caught too.
//
// THE REVERSE DIRECTION IS NOT ASSERTED, deliberately: DOCKERFILE is overridable
// (the same script builds web/Dockerfile), so a `--build-arg` api/Dockerfile does
// not consume can be legitimate. The forward direction is the one whose rot
// shipped a wrong stamp.
func TestReleaseScriptPassesTheStampsTheDockerfileRequires(t *testing.T) {
	dir := pkgDir(t)
	dockerfile := readGuardedFile(t, filepath.Join(dir, dockerfilePath), "the api image build recipe")
	ship := readGuardedFile(t, filepath.Join(dir, "..", "..", "..", shipScriptPath),
		"the release script that sets the stamps on a real build")

	// Comments are prose, not wiring. The script's own header explains what a
	// missing --build-arg looks like, and that sentence must never be able to
	// satisfy this check on a script that stopped passing one.
	var active strings.Builder
	for _, line := range strings.Split(ship, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		active.WriteString(line)
		active.WriteString("\n")
	}
	script := active.String()

	// 1. The ARGs the shipped binary reads its stamps from: ARG -> field.
	required := map[string]string{}
	for _, m := range ldflagEntry.FindAllStringSubmatch(dockerfile, -1) {
		if m[1] != ldflagPkg {
			continue
		}
		ref := argRef.FindStringSubmatch(strings.TrimRight(m[3], `\"`))
		if ref == nil {
			continue // a literal value is the other test's finding, and needs no ARG
		}
		arg := ref[1]
		if arg == "" {
			arg = ref[2]
		}
		required[arg] = m[2]
	}
	if len(required) == 0 {
		t.Fatalf("api/Dockerfile wires no `-X %s.<Field>=$ARG`, so this guard would prove nothing about the release script", ldflagPkg)
	}

	// 2. What the release script actually passes, with the value as written.
	passed := map[string]string{}
	for _, m := range shipBuildArg.FindAllStringSubmatch(script, -1) {
		passed[m[1]] = m[2]
	}
	if len(passed) == 0 {
		t.Fatalf("%s passes no `--build-arg` at all: every stamp falls back to its package default on a real release", shipScriptPath)
	}
	if !strings.Contains(script, shipBuildArgsArray) {
		t.Fatalf("%s never expands %s, so none of the --build-arg flags it defines can reach `docker build`", shipScriptPath, shipBuildArgsArray)
	}

	args := make([]string, 0, len(required))
	for arg := range required {
		args = append(args, arg)
	}
	sort.Strings(args)

	// 3. Every required ARG must arrive, non-empty and computed.
	for _, arg := range args {
		value, ok := passed[arg]
		switch {
		case !ok:
			t.Errorf("%s never passes `--build-arg %s=...`: api/Dockerfile reads $%s for version.%s, so a real release reports the package default instead of the stamp",
				shipScriptPath, arg, arg, required[arg])
		case value == "":
			t.Errorf("%s passes `--build-arg %s=` with an empty value: version.%s reaches the binary as an empty string, not as a provenance value",
				shipScriptPath, arg, required[arg])
		case !strings.Contains(value, "$"):
			t.Errorf("%s passes `--build-arg %s=%s`, a literal: every release would claim that one value, so the stamp cannot identify the build it is in",
				shipScriptPath, arg, value)
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
