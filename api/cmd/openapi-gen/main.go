// Command openapi-gen writes the published REST API contract artifacts.
//
// THE ARTIFACTS, and why there is more than one
//   api/openapi/openapi.json   — the contract as the repository publishes it.
//   web/public/openapi.json    — the SAME bytes, served by the web app, so any
//                                visitor can fetch
//                                https://jomform.com/openapi.json. This copy
//                                exists because the repository is private: a
//                                github.com link answers 404 for exactly the
//                                third-party developer the /docs/api page is
//                                written for.
//   web/src/lib/openapiSpec.gen.ts
//                              — the same JSON, as a typed module, so the
//                                /docs/api page renders the endpoint reference
//                                from the contract instead of restating it in
//                                prose. A docs page that lists endpoints by hand
//                                is the defect this whole card is about.
//
//   All of them are generated from the single registry in
//   api/internal/httpapi/openapi_routes.go, and all of them are checked by
//   openapi_parity_test.go — which regenerates them in memory and fails on any
//   difference, so a hand-edited or forgotten artifact cannot be served.
//
// USAGE
//
//	cd api && go run ./cmd/openapi-gen          # write both files
//	cd api && go run ./cmd/openapi-gen -check   # fail if either is stale
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wzul/jomform/api/internal/httpapi"
)

func main() {
	check := flag.Bool("check", false, "verify the artifacts are up to date instead of writing them")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	artifacts := httpapi.GeneratedArtifactPaths()

	stale := false
	for _, rel := range artifacts {
		want := httpapi.GeneratedArtifact(rel)
		if want == "" {
			fail(fmt.Errorf("no generator is registered for %s", rel))
		}
		path := filepath.Join(root, rel)
		if *check {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				stale = true
				fmt.Fprintf(os.Stderr, "STALE: %s\n", rel)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", rel, len(want))
	}
	if *check && stale {
		fail(fmt.Errorf("run: cd api && go run ./cmd/openapi-gen"))
	}
}

// repoRoot walks up from the working directory to the module root (the directory
// holding go.mod), which is api/; the repository root is its parent.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s — run this from inside the repo", dir)
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "openapi-gen:", err)
	os.Exit(1)
}
