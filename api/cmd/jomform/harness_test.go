package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// harnessFixture is a config this command does NOT own: two foreign servers
// with the user's own spacing, and a top-level key beside mcpServers.
const harnessFixture = `{
  "mcpServers": {
    "github":   { "command": "npx", "args": [ "-y", "server-github" ] },
    "postgres":{"url":"https://pg.example/mcp","headers":{"X-Tenant":"a"}}
  },
  "theme": "dark"
}
`

func entryFor(t *testing.T, url, token string) json.RawMessage {
	t.Helper()
	entry, err := harnessEntryJSON(url, token)
	if err != nil {
		t.Fatalf("harnessEntryJSON: %v", err)
	}
	return entry
}

// TestMergeServerPreservesOtherServersByteForByte is the card's merge contract:
// the file is edited, not re-serialised. The expected output is built by placing
// the new member at the one position a merge may touch, so any other difference
// fails the byte comparison.
func TestMergeServerPreservesOtherServersByteForByte(t *testing.T) {
	entry := entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken())
	out, err := mergeServer([]byte(harnessFixture), "jomform", entry)
	if err != nil {
		t.Fatalf("mergeServer: %v", err)
	}

	tail := "\n  },\n  \"theme\": \"dark\"\n}\n"
	if !strings.HasSuffix(harnessFixture, tail) {
		t.Fatalf("fixture shape changed; this test builds its expectation from that tail")
	}
	want := strings.TrimSuffix(harnessFixture, tail) +
		",\n    \"jomform\": " + indentEntry(entry, "    ") + tail
	if string(out) != want {
		t.Fatalf("merged config is not the original plus one member:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}

	// Belt and braces: every foreign byte, including the odd spacing, is there.
	for _, raw := range []string{
		`"github":   { "command": "npx", "args": [ "-y", "server-github" ] }`,
		`"postgres":{"url":"https://pg.example/mcp","headers":{"X-Tenant":"a"}}`,
		`"theme": "dark"`,
	} {
		if !bytes.Contains(out, []byte(raw)) {
			t.Fatalf("merge rewrote %s\n%s", raw, out)
		}
	}
	if !json.Valid(out) {
		t.Fatalf("merge produced invalid JSON:\n%s", out)
	}
}

func TestMergeServerAddsTheContainerWhenTheFileHasNone(t *testing.T) {
	entry := entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken())

	out, err := mergeServer([]byte(`{"theme": "dark"}`), "jomform", entry)
	if err != nil {
		t.Fatalf("mergeServer: %v", err)
	}
	if !bytes.Contains(out, []byte(`"theme": "dark"`)) {
		t.Fatalf("merge dropped the top-level key:\n%s", out)
	}
	if !bytes.Contains(out, []byte(`"mcpServers"`)) || !bytes.Contains(out, []byte(`"jomform"`)) {
		t.Fatalf("merge did not add the server:\n%s", out)
	}
	if !json.Valid(out) {
		t.Fatalf("merge produced invalid JSON:\n%s", out)
	}
}

func TestMergeServerWritesAFreshFileWhenEmpty(t *testing.T) {
	entry := entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken())
	out, err := mergeServer(nil, "jomform", entry)
	if err != nil {
		t.Fatalf("mergeServer: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("fresh config is not JSON: %v\n%s", err, out)
	}
	if doc.MCPServers["jomform"].URL != "https://mcp.example.test/mcp" {
		t.Fatalf("fresh config does not point at the MCP endpoint:\n%s", out)
	}
}

func TestMergeServerReplacesOnlyOurOwnEntry(t *testing.T) {
	first := entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken())
	second := entryFor(t, "https://mcp2.example.test/mcp", fixtureRefreshToken())

	out, err := mergeServer([]byte(harnessFixture), "jomform", first)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	out, err = mergeServer(out, "jomform", second)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if n := bytes.Count(out, []byte(`"jomform"`)); n != 1 {
		t.Fatalf("config carries %d jomform entries after two logins, want 1:\n%s", n, out)
	}
	if !bytes.Contains(out, []byte(`mcp2.example.test/mcp`)) {
		t.Fatalf("the second login did not update the entry:\n%s", out)
	}
	for _, raw := range []string{
		`"github":   { "command": "npx", "args": [ "-y", "server-github" ] }`,
		`"postgres":{"url":"https://pg.example/mcp","headers":{"X-Tenant":"a"}}`,
	} {
		if !bytes.Contains(out, []byte(raw)) {
			t.Fatalf("the second merge rewrote %s\n%s", raw, out)
		}
	}
}

func TestMergeServerRefusesAFileItCannotParse(t *testing.T) {
	entry := entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken())
	broken := []byte("{\n  // a comment the harness tolerates\n  \"mcpServers\": {}\n}\n")
	if out, err := mergeServer(broken, "jomform", entry); err == nil {
		t.Fatalf("merge accepted invalid JSON and returned:\n%s", out)
	}
}

func TestWithoutServerRemovesOnlyOurEntry(t *testing.T) {
	entry := entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken())
	other := []string{
		`"alpha": {"url": "https://a.example/mcp"}`,
		`"beta": {"command": "run", "args": ["x"]}`,
	}
	ours := `"jomform": {"url": "https://mcp.example.test/mcp"}`

	// Ours first, ours in the middle, ours last (the last one is produced by a
	// real merge, so the removal is also exercised against our own output).
	merged, err := mergeServer([]byte(`{"mcpServers": {`+other[0]+`,`+other[1]+`}}`), "jomform", entry)
	if err != nil {
		t.Fatalf("seed merge: %v", err)
	}
	docs := map[string]string{
		"first":  `{"mcpServers": {` + ours + `,` + other[0] + `,` + other[1] + `}}`,
		"middle": `{"mcpServers": {` + other[0] + `,` + ours + `,` + other[1] + `}}`,
		"last":   string(merged),
	}
	for name, src := range docs {
		t.Run(name, func(t *testing.T) {
			out, removed, err := withoutServer([]byte(src), "jomform")
			if err != nil {
				t.Fatalf("withoutServer: %v", err)
			}
			if !removed {
				t.Fatal("withoutServer reported no removal")
			}
			if bytes.Contains(out, []byte(`"jomform"`)) {
				t.Fatalf("our entry survived:\n%s", out)
			}
			for _, raw := range other {
				if !bytes.Contains(out, []byte(raw)) {
					t.Fatalf("removal damaged %s:\n%s", raw, out)
				}
			}
			if !json.Valid(out) {
				t.Fatalf("removal produced invalid JSON:\n%s", out)
			}
		})
	}
}

func TestWithoutServerOnAFileWithoutItIsANoOp(t *testing.T) {
	out, removed, err := withoutServer([]byte(harnessFixture), "jomform")
	if err != nil || removed {
		t.Fatalf("removed=%v err=%v, want a no-op", removed, err)
	}
	if string(out) != harnessFixture {
		t.Fatalf("a no-op changed the bytes:\n%s", out)
	}
}

// TestPathAllowlist pins which files a login may touch without --force.
func TestPathAllowlist(t *testing.T) {
	home := t.TempDir()
	c := &cli{home: home}
	allowed := []string{
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"),
		filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"),
	}
	for _, p := range allowed {
		if !c.pathAllowed(p) {
			t.Fatalf("%s should be an allowed harness path", p)
		}
	}
	for _, p := range []string{
		filepath.Join(home, "notes.json"),
		filepath.Join(home, ".ssh", "config"),
		"/etc/passwd",
		filepath.Join(home, ".cursor", "settings.json"),
	} {
		if c.pathAllowed(p) {
			t.Fatalf("%s must not be an allowed harness path", p)
		}
	}
}

// TestPathAllowlistComparesRealLocations covers the two spellings that must not
// slip past the allowlist: a traversal that only looks like a cursor path, and a
// home directory reached through a symlink (which is the same file, so it must
// stay allowed rather than fail for the user who moved their home).
func TestPathAllowlistComparesRealLocations(t *testing.T) {
	home := t.TempDir()
	c := &cli{home: home}
	traversal := filepath.Join(home, ".cursor", "..", "..", "etc", "passwd")
	if c.pathAllowed(traversal) {
		t.Fatalf("%s was treated as the Cursor config", traversal)
	}

	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	linked := &cli{home: link}
	if err := os.MkdirAll(filepath.Join(real, ".cursor"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !linked.pathAllowed(filepath.Join(real, ".cursor", "mcp.json")) {
		t.Fatalf("the resolved spelling of a symlinked home was refused")
	}
}

func TestResolveHarnessPathDefaultsToCursor(t *testing.T) {
	home := t.TempDir()
	c := &cli{home: home}
	got, err := c.resolveHarnessPath("")
	if err != nil {
		t.Fatalf("resolveHarnessPath: %v", err)
	}
	if want := filepath.Join(home, ".cursor", "mcp.json"); got != want {
		t.Fatalf("default out = %q, want %q", got, want)
	}
	if got, err := c.resolveHarnessPath("~/Library/Application Support/Claude/claude_desktop_config.json"); err != nil {
		t.Fatalf("resolveHarnessPath(~): %v", err)
	} else if want := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"); got != want {
		t.Fatalf("~ was not expanded: got %q, want %q", got, want)
	}
	if _, err := c.resolveHarnessPath("relative/mcp.json"); err == nil {
		t.Fatal("a relative --out was accepted")
	}
}

// TestWriteFile0600TightensAnExistingMode pins the permission the card requires:
// an existing 0644 config is replaced with a 0600 file, not left world-readable.
func TestWriteFile0600TightensAnExistingMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := writeFile0600(p, []byte("{\"mcpServers\":{}}")); err != nil {
		t.Fatalf("writeFile0600: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}
