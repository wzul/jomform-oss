package mcpsrv

// disable_2fa: the trusted-device revocation disclosure must survive in BOTH
// places the tool documents itself — the Description an MCP client receives over
// tools/list, and the row for the tool on web/src/app/docs/mcp/page.tsx.
//
// WHY THIS FILE EXISTS. Issue #713 made `disable_2fa` revoke every trusted
// device of the account: a device that was skipping the factor being removed
// cannot stay exempt after it is gone. The tool discloses that in two places, and
// until this file the disclosure was pinned by NOTHING:
//
//	grep -rn "peranti dipercayai akaun" --include=*_test.go api/   -> no hits
//
// The two surfaces did not even have the same coverage, which is what makes the
// hole easy to miss. `TestDisable2FAInputSchemaMatchesDocs` (on the branch that
// carries the totp_code seam) asserts the docs row and the Description each name
// the two declared FIELD names (`password`, `totp_code`) — so it reds when a
// field name is dropped, and stays green when the revocation sentence is dropped.
// Measured, not assumed: replacing the Description's three concatenated
// revocation lines with prose that denies the behaviour entirely compiles and
// leaves `go test ./internal/mcpsrv/ -count=1` green.
//
// WHY THE TWO SURFACES, AND WHY BOTH LITERALS. The disclosure has two halves and
// an agent needs both: the STATEMENT that other devices are revoked (otherwise
// it cannot warn the user, and may believe a device it already trusted is still
// exempt) and the POINTER to `list_trusted_devices` (otherwise it has no way to
// check what is left). A guard that accepted either half alone would pass a
// Description that says "2FA dimatikan" and an agent would learn the revocation
// only from watching a device unexpectedly ask for a code again.
//
// The disclosure is the second half of a UNION conflict resolution: main's row
// and the refactor branch's row each carried one half of the tool's contract, and
// the merged row carries both. A union is only as durable as its weakest half —
// a re-cut that takes one side wholesale silently drops the other side's facts
// while every test stays green. That is the failure this file exists to make red.
//
// WHAT THIS FILE PINS (no database, no network — it always runs):
//
//	TestDisable2FAToolAndDocsRowDiscloseTrustedDeviceRevocation
//	  the disable_2fa Description as an MCP client receives it, and the
//	  disable_2fa row on the docs page, each contain the revocation statement
//	  AND the list_trusted_devices pointer.
//
// The expectations are literals written out here, not read from the code under
// test: a guard that derives its expectation from the thing it guards cannot
// catch that thing changing. That is the shape the surrounding guards already
// use for the field names.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// disable2faRevocationClause is the CLAIM half of the #713 disclosure: disabling
// 2FA cancels every trusted device of the account.
//
// It pins the claim ("membatalkan SEMUA peranti dipercayai akaun"), not a prefix
// such as "Mematikan 2FA juga …", so rewording the lead-in is not a false red —
// while a sentence that flips the meaning (a device kept exempt, or "tiada kesan
// sampingan") cannot pass by keeping the subject around.
const disable2faRevocationClause = "membatalkan SEMUA peranti dipercayai akaun"

// disable2faRevocationPointer is the second half of the disclosure: the tool an
// agent must call to see which devices, if any, are left.
const disable2faRevocationPointer = "list_trusted_devices"

// disable2faRevocationDocsRow returns the one docs entry for disable_2fa, and
// refuses to guess when the page does not have exactly one.
func disable2faRevocationDocsRow(t *testing.T) (string, string) {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the docs page")
	}
	// api/internal/mcpsrv → ../../../web/src/app/docs/mcp/page.tsx
	path := filepath.Clean(filepath.Join(filepath.Dir(file),
		"..", "..", "..", "web", "src", "app", "docs", "mcp", "page.tsx"))

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var rows []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, `name: "disable_2fa"`) {
			rows = append(rows, l)
		}
	}
	switch len(rows) {
	case 0:
		t.Fatalf("no docs entry for disable_2fa on %s — DOCUMENTATION PARITY requires one, "+
			"and without it the revocation disclosure is documented nowhere a user reads",
			path)
	case 1:
	default:
		t.Fatalf("found %d docs entries for disable_2fa on %s — the guard cannot tell which "+
			"one a reader sees, so a duplicate row could carry the disclosure while the rendered "+
			"one does not", len(rows), path)
	}
	return rows[0], path
}

// disable2faToolDescription returns the Description as an MCP client actually
// receives it — over tools/list, not read off the Go struct the server builds.
func disable2faToolDescription(t *testing.T) string {
	t.Helper()

	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "disable2fa-revocation-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	var found *mcp.Tool
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			if tl.Name == "disable_2fa" {
				found = tl
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if found == nil {
		t.Fatal("disable_2fa is not registered — every assertion below would be vacuous")
	}
	if strings.TrimSpace(found.Description) == "" {
		t.Fatal("disable_2fa has an empty Description — an agent is told nothing about the call")
	}
	// Log the Description as an MCP client receives it, so a failure report (and a
	// reviewer's own run) shows the real string rather than a paraphrase.
	t.Logf("disable_2fa Description over tools/list: %s", found.Description)
	return found.Description
}

// TestDisable2FAToolAndDocsRowDiscloseTrustedDeviceRevocation pins the #713
// revocation disclosure on both surfaces that document the tool.
func TestDisable2FAToolAndDocsRowDiscloseTrustedDeviceRevocation(t *testing.T) {
	row, path := disable2faRevocationDocsRow(t)
	desc := disable2faToolDescription(t)

	// Each surface must carry BOTH halves, and the failure names which half is
	// missing — a re-cut that takes the other side of the union drops exactly one.
	for _, want := range []string{disable2faRevocationClause, disable2faRevocationPointer} {
		if !strings.Contains(row, want) {
			t.Errorf("the disable_2fa row on %s does not state %q, but disabling 2FA DOES revoke "+
				"every trusted device of the account. An agent reading the docs would not know to "+
				"warn the user, and a device it already trusted would silently start asking for a "+
				"code again.\nrow: %s", path, want, strings.TrimSpace(row))
		}
		if !strings.Contains(desc, want) {
			t.Errorf("the disable_2fa Description does not state %q, but the revocation happens "+
				"anyway. This is the copy an MCP client is handed, so the behaviour would be "+
				"undocumented on the surface an agent actually reads.\ndescription: %s", want, desc)
		}
	}
}
