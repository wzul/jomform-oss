package httpapi

import (
	"regexp"
	"strings"
	"testing"
)

// Guards on the licence every published surface states.
//
// WHY THEY LIVE HERE
//   The repo-wide guards in this package already read files outside api/ (the
//   docs snippet census, the CTA contrast guard) because those surfaces are
//   served BY this API or alongside it. The licence is the same shape: it is
//   stated once in code (openapi_routes.go) and copied into three generated
//   artifacts, and a web-side equivalent cannot read README.md at all — the root
//   .dockerignore keeps repo docs out of the image, and web/src/lib/
//   buildContext.test.ts reds any web test that reads one.
//
// WHY THEY EXIST
//   The project went private in 2026-09 and every public surface was rewritten to
//   claim it was proprietary, the OpenAPI contract included (info.license.name =
//   "Proprietary"). The project is MIT again. Generated code cannot notice a
//   wrong value stated once and copied correctly: TestGeneratedArtifactsAreInSync
//   compares the artifacts with a FRESH GENERATION of openapi_routes.go, so
//   editing the value regenerates consistently and every parity check stays
//   green while the contract keeps telling third-party callers a licence they do
//   not have.

// TestPublishedContractDeclaresTheMITLicence pins all four contract surfaces
// plus the licence file they point at.
func TestPublishedContractDeclaresTheMITLicence(t *testing.T) {
	sources := []struct{ name, body string }{
		{"the document the generator produces (in memory)", generatedArtifact(t, "api/openapi/openapi.json")},
		{"api/openapi/openapi.json", readRootFile(t, "api/openapi/openapi.json")},
		{"web/public/openapi.json", readRootFile(t, "web/public/openapi.json")},
		{"web/src/lib/openapiSpec.gen.ts", readRootFile(t, "web/src/lib/openapiSpec.gen.ts")},
	}
	// The license object, as both JSON copies and the TS module render it.
	mit := regexp.MustCompile(`"license":\s*\{[^}]*"name":\s*"MIT"`)
	proprietary := regexp.MustCompile(`"license":\s*\{[^}]*[Pp]roprietary`)
	checked := 0
	for _, src := range sources {
		if len(src.body) < 10_000 {
			t.Errorf("%s is %d bytes — that is not the API contract, so the match below would "+
				"check nothing", src.name, len(src.body))
			continue
		}
		if !mit.MatchString(src.body) {
			t.Errorf("%s does not advertise the MIT licence: its license object carries no "+
				"\"name\": \"MIT\".\nThe value is generated from openapi_routes.go — if you changed "+
				"it, run: cd api && go run ./cmd/openapi-gen", src.name)
		}
		if proprietary.MatchString(src.body) {
			t.Errorf("%s still advertises a proprietary licence in its license object", src.name)
		}
		checked++
	}
	if checked != len(sources) {
		t.Errorf("checked %d of %d contract surfaces", checked, len(sources))
	}

	// The licence the contract names has to be the one the repository grants —
	// a contract reading "MIT" over a LICENSE file that says otherwise would be
	// the same defect one layer down.
	licence := readRootFile(t, "LICENSE")
	for _, want := range []string{"MIT License", "Copyright (c) 2026 Wan Zulkarnain",
		"Permission is hereby granted", `THE SOFTWARE IS PROVIDED "AS IS"`} {
		if !strings.Contains(licence, want) {
			t.Errorf("LICENSE does not contain %q — the contract advertises MIT, so the file it "+
				"points at must grant it", want)
		}
	}
}

// TestReadmeLicenceSectionGrantsMIT pins the README, which is the surface a
// developer reads before the contract and which the 2026-09 rewrite turned into
// "a proprietary, hosted service. All rights reserved."
func TestReadmeLicenceSectionGrantsMIT(t *testing.T) {
	readme := readRootFile(t, "README.md")
	// ANCHOR: without a License section there is nothing below to check, and a
	// substring search over the whole file would pass on a stray mention.
	start := strings.Index(readme, "## License")
	if start < 0 {
		t.Fatalf("README.md has no '## License' section — the licence is then stated nowhere a " +
			"reader looks first")
	}
	section := readme[start:]
	if next := strings.Index(section[1:], "\n## "); next >= 0 {
		section = section[:next+1]
	}
	if len(section) < 80 {
		t.Fatalf("the README License section is %d bytes — that is not a licence statement, so the "+
			"checks below would pass vacuously", len(section))
	}
	lower := strings.ToLower(section)
	for _, bad := range []string{"proprietary", "all rights reserved"} {
		if strings.Contains(lower, bad) {
			t.Errorf("the README License section still claims %q:\n%s", bad, section)
		}
	}
	if !strings.Contains(section, "MIT") {
		t.Errorf("the README License section does not name MIT:\n%s", section)
	}
	if !strings.Contains(section, "LICENSE") {
		t.Errorf("the README License section does not link the LICENSE file:\n%s", section)
	}
}
