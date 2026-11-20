package objstore

// This file guards WHICH object store the two shipped compose files declare and
// HOW it is pinned (card t_fc236020).
//
// WHY IT EXISTS. Production was moved to RustFS on 2026-11-20, and both compose
// files in this repo still described a store that had never run there — the
// exact failure this test is for: a repo-driven deploy provisions the store the
// FILE names, so a stale file silently installs a fourth store next to the one
// holding every uploaded image. Nothing else in the repo reads these files for
// their object store, so a wrong image would ship green.
//
// WHAT IT PINS, per file:
//
//  1. The api's S3_ENDPOINT resolves to the store service in the same file —
//     the two cannot drift into "the api talks to a name nothing defines".
//  2. There is EXACTLY ONE object-store service, it is named `rustfs`, and its
//     image is the exact digest-pinned image below. A tag substituted for the
//     digest fails here: a tag can be re-pushed under the data, a digest cannot.
//  3. That service is configured the way the app was verified against:
//     RUSTFS_ADDRESS on 0.0.0.0:9000, RUSTFS_VOLUMES=/data, the console off, the
//     `rustfsdata` volume at /data, no published port, and a healthcheck that
//     proves the S3 HANDLER is serving.
//  4. The api's boot is gated on `rustfs` being service_healthy. This is not
//     decoration: the api provisions its bucket once at startup, so an api that
//     starts before the store is serving fails EnsureBucket and leaves uploads
//     disabled for the life of the process.
//  5. No service name and no image in either file names a store this product
//     does not run (the name and image families below).
//
// The digest is written once, here. A version bump is therefore a two-file plus
// one-constant change, which is the point: it cannot happen by editing one file.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// wantStoreImage is the RustFS release prod runs, pinned by digest. The tag
	// records the version a human verified; the digest is what ships.
	wantStoreImage = "rustfs/rustfs:1.0.6@sha256:ee717072290bec9b11abfc0365f8310b00df1cd9b3639943edae47bdef05044a"

	// wantStoreService is the compose service name, and therefore the host the
	// api reaches the store on (http://rustfs:9000).
	wantStoreService = "rustfs"

	// wantStoreEndpoint is the api-side endpoint, asserted both ways: the env
	// value, and the service it resolves to.
	wantStoreEndpoint = "http://rustfs:9000"

	// wantStoreVolume is the named volume holding the objects.
	wantStoreVolume = "rustfsdata"
)

// composeFiles are every compose file a deploy can start a stack from.
var composeFiles = []string{
	"../../../deploy/docker-compose.yml",   // the self-hosted one-command install
	"../../../deploy/vps/compose.prod.yml", // the production stack
}

// pinnedDigest matches a version tag followed by a digest — the only shape an
// object-store image is allowed to have here.
var pinnedDigest = regexp.MustCompile(`^rustfs/rustfs:[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}$`)

// strayStoreName matches anything that names a store family this product does
// not run. It is applied to SERVICE NAMES and IMAGE references only, never to
// the files' prose: a comment is allowed to explain why those images are not
// used, and the headers do.
var strayStoreName = regexp.MustCompile(`(?i)(minio|seaweed|chrislusf|garage|ceph)`)

func TestComposeObjectStoreIsRustFSPinnedByDigest(t *testing.T) {
	for _, path := range composeFiles {
		path := path
		t.Run(path, func(t *testing.T) {
			doc := parseComposeFile(t, path)

			// ── 1. the api points at the service this file defines ───────────
			api, ok := doc.services["api"]
			if !ok {
				t.Fatalf("%s defines no `api` service", path)
			}
			endpoint := envValue(api, "S3_ENDPOINT")
			if endpoint != wantStoreEndpoint {
				t.Errorf("api S3_ENDPOINT = %q, want %q (the store service's name is what makes it resolvable on the compose network)",
					endpoint, wantStoreEndpoint)
			}
			if got := endpointHost(endpoint); got != wantStoreService {
				t.Errorf("api S3_ENDPOINT host = %q, want %q", got, wantStoreService)
			}

			// ── 2. exactly one store, and it is the pinned one ───────────────
			stores := doc.objectStoreServices()
			if len(stores) != 1 {
				t.Fatalf("%s declares %d object-store services (%s), want exactly 1 — a second store is how a deploy provisions a store the data is not in",
					path, len(stores), strings.Join(stores, ", "))
			}
			if stores[0] != wantStoreService {
				t.Fatalf("%s declares the object store as %q, want %q", path, stores[0], wantStoreService)
			}
			store := doc.services[wantStoreService]

			image := doc.image(wantStoreService)
			if image != wantStoreImage {
				t.Errorf("%s: %s image = %q, want %q — the object store must be pinned by digest",
					path, wantStoreService, image, wantStoreImage)
			}
			if !pinnedDigest.MatchString(image) {
				t.Errorf("%s: %s image %q is not a version tag followed by a sha256 digest; a tag alone can be re-pushed under the data",
					path, wantStoreService, image)
			}

			// ── 3. configured the way the app was verified ──────────────────
			for key, want := range map[string]string{
				"RUSTFS_ACCESS_KEY":     "", // asserted present and non-empty below
				"RUSTFS_SECRET_KEY":     "",
				"RUSTFS_ADDRESS":        "0.0.0.0:9000",
				"RUSTFS_VOLUMES":        "/data",
				"RUSTFS_CONSOLE_ENABLE": "false",
			} {
				got, present := envValueOK(store, key)
				if !present {
					t.Errorf("%s: %s sets no %s", path, wantStoreService, key)
					continue
				}
				if want == "" {
					if strings.TrimSpace(got) == "" {
						t.Errorf("%s: %s leaves %s empty", path, wantStoreService, key)
					}
					continue
				}
				if got != want {
					t.Errorf("%s: %s %s = %q, want %q", path, wantStoreService, key, got, want)
				}
			}
			if !blockHas(store, "- "+wantStoreVolume+":/data") {
				t.Errorf("%s: %s does not mount the %s volume at /data", path, wantStoreService, wantStoreVolume)
			}
			if !doc.declaresVolume(wantStoreVolume) {
				t.Errorf("%s: %s mounts %s but the top-level volumes: section does not declare it (a fresh host gets an anonymous volume instead)",
					path, wantStoreService, wantStoreVolume)
			}
			if blockHasPrefix(store, "ports:") {
				t.Errorf("%s: %s publishes a host port; the store is reachable on the compose network only", path, wantStoreService)
			}
			testLine := healthcheckTest(store)
			if !strings.Contains(testLine, "http://127.0.0.1:9000/") || !strings.Contains(testLine, "AccessDenied") {
				t.Errorf("%s: %s healthcheck = %q — want an unsigned GET on 127.0.0.1:9000 answered with the S3 error document (a port check passes while the store is not serving, and the api then fails its one-shot bucket provisioning)",
					path, wantStoreService, testLine)
			}

			// ── 4. the api's boot is gated on the store being healthy ───────
			if cond := dependsOnCondition(api, wantStoreService); cond != "service_healthy" {
				t.Errorf("%s: api depends_on %s = %q, want \"service_healthy\" (a failed EnsureBucket disables uploads for the life of the process)",
					path, wantStoreService, cond)
			}

			// ── 5. no other store family appears as a service or image ──────
			for name := range doc.services {
				if strayStoreName.MatchString(name) {
					t.Errorf("%s defines a service named %q — this product runs RustFS only", path, name)
				}
			}
			for name, img := range doc.images {
				if strayStoreName.MatchString(img) {
					t.Errorf("%s: service %q names image %q — this product runs RustFS only", path, name, img)
				}
			}
		})
	}
}

// TestComposeObjectStoreImageIsTheSameInBothFiles keeps the two files on ONE
// version. They ship to different hosts but hold the same data model, and a
// self-host install that pins a different release than production is a bug
// report waiting to happen.
func TestComposeObjectStoreImageIsTheSameInBothFiles(t *testing.T) {
	seen := map[string][]string{}
	for _, path := range composeFiles {
		doc := parseComposeFile(t, path)
		img := doc.image(wantStoreService)
		seen[img] = append(seen[img], path)
	}
	if len(seen) != 1 {
		t.Fatalf("the compose files pin %d different object-store images: %v", len(seen), seen)
	}
}

// ── a deliberately small compose reader ─────────────────────────────────────
// Only the subset a compose service block uses is understood: 2-space service
// keys under `services:`, their 4-space directives, and 6-space environment /
// depends_on entries. It FAILS LOUDLY when the expectations are not met
// (missing file, no services, no api) rather than passing vacuously on an
// unparsed document.

type composeDoc struct {
	path     string
	services map[string][]string // service name → its non-comment block lines
	images   map[string]string   // service name → its image
	volumes  map[string]bool     // top-level declared volume names
}

func parseComposeFile(t *testing.T, path string) composeDoc {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	doc := composeDoc{
		path:     path,
		services: map[string][]string{},
		images:   map[string]string{},
		volumes:  map[string]bool{},
	}

	section := "" // "services", "volumes", or ""
	service := "" // current service under services:
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		// Comment-only lines carry no behaviour: the headers explain which
		// images are NOT used, and reading that as configuration would be the
		// reverse of a guard.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if indent == 0 {
			service = ""
			switch {
			case strings.HasSuffix(trimmed, ":"):
				section = strings.TrimSuffix(trimmed, ":")
			default:
				section = ""
			}
			continue
		}
		switch section {
		case "services":
			if indent == 2 && strings.HasSuffix(trimmed, ":") {
				service = strings.TrimSuffix(trimmed, ":")
				if _, ok := doc.services[service]; !ok {
					doc.services[service] = nil
				}
				continue
			}
			if indent > 2 && service != "" {
				doc.services[service] = append(doc.services[service], line)
			}
		case "volumes":
			if indent == 2 && strings.HasSuffix(trimmed, ":") {
				doc.volumes[strings.TrimSuffix(trimmed, ":")] = true
			}
		}
	}
	if len(doc.services) == 0 {
		t.Fatalf("%s: no services parsed — the reader and the file disagree, so nothing below could be trusted", path)
	}
	for name, block := range doc.services {
		if img := firstMatch(block, "image:"); img != "" {
			doc.images[name] = img
		}
	}
	return doc
}

// image returns the service's image, or "".
func (d composeDoc) image(service string) string { return d.images[service] }

func (d composeDoc) declaresVolume(name string) bool { return d.volumes[name] }

// objectStoreServices finds every service that IS an object store, by name,
// image family or the env only a store reads — so a store added under a name
// nobody thought of is still found.
func (d composeDoc) objectStoreServices() []string {
	var out []string
	for name, block := range d.services {
		lower := strings.ToLower(name)
		img := strings.ToLower(d.images[name])
		is := strayStoreName.MatchString(name) ||
			strayStoreName.MatchString(img) ||
			strings.Contains(lower, "rustfs") ||
			strings.Contains(lower, "s3store") ||
			name == "s3" ||
			name == "objectstorage" ||
			name == "object-storage" ||
			blockHasPrefix(block, "RUSTFS_") ||
			blockHasPrefix(block, "MINIO_") ||
			blockHasPrefix(block, "AWS_ACCESS_KEY_ID:")
		if is {
			out = append(out, name)
		}
	}
	return out
}

// envValue returns the value of KEY in the service's environment: block, or "".
func envValue(block []string, key string) string {
	v, _ := envValueOK(block, key)
	return v
}

func envValueOK(block []string, key string) (string, bool) {
	inEnv := false
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed == "environment:" {
			inEnv = true
			continue
		}
		if !inEnv {
			continue
		}
		if indent <= 4 {
			inEnv = false
			continue
		}
		if !strings.HasPrefix(trimmed, key+":") {
			continue
		}
		return unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))), true
	}
	return "", false
}

// dependsOnCondition returns the condition: value for dependency dep.
func dependsOnCondition(block []string, dep string) string {
	inDep := false
	depIndent := -1
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed == "depends_on:" {
			inDep = true
			continue
		}
		if !inDep {
			continue
		}
		if indent <= 4 {
			return ""
		}
		if trimmed == dep+":" {
			depIndent = indent
			continue
		}
		if depIndent > 0 && indent > depIndent && strings.HasPrefix(trimmed, "condition:") {
			return unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "condition:")))
		}
		if depIndent > 0 && indent <= depIndent {
			depIndent = -1
		}
	}
	return ""
}

// healthcheckTest returns the first `test:` line of the service's healthcheck.
func healthcheckTest(block []string) string {
	inHC := false
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed == "healthcheck:" {
			inHC = true
			continue
		}
		if !inHC {
			continue
		}
		if indent <= 4 {
			return ""
		}
		if strings.HasPrefix(trimmed, "test:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "test:"))
		}
	}
	return ""
}

func blockHas(block []string, want string) bool {
	for _, line := range block {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// blockHasPrefix reports whether any line in the block (at any depth) starts
// with prefix, after trimming. Used for directive-level questions like "is
// there a ports: key".
func blockHasPrefix(block []string, prefix string) bool {
	for _, line := range block {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

func firstMatch(block []string, prefix string) string {
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)))
		}
	}
	return ""
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// endpointHost extracts the host[:port] part of an http(s) URL without pulling
// in net/url for a two-file assertion (and without accepting a path).
func endpointHost(endpoint string) string {
	rest := endpoint
	for _, scheme := range []string{"http://", "https://"} {
		if strings.HasPrefix(rest, scheme) {
			rest = strings.TrimPrefix(rest, scheme)
			break
		}
	}
	if i := strings.IndexAny(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}
