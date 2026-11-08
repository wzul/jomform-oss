package httpapi

// One refusal sentence, two doors, one machine code (card t_7546eda5).
//
// The MCP form-minting tools (upsert_form create, use_template clone) refuse an
// account with no live workspace, exactly as POST /v1/rest/forms does since
// t_6c0badbe. The sentence lives twice because httpapi imports mcpsrv (so
// mcpsrv cannot import workspaceRequiredMessage back), and a duplicated literal
// is exactly the thing that drifts.
//
// Two independent things are pinned, because passing one is not passing the
// other:
//
//  1. the MCP refusal is the SAME string the REST body carries — so a client
//     that reads either surface sees one answer, and
//  2. that string still resolves to the `workspace.required` MACHINE code
//     through this package's message→code table. That is the code the dashboard
//     and the chat lane branch on; without it the MCP refusal would arrive as a
//     bare sentence and the two doors would disagree about the machine answer
//     even while agreeing about the prose.
//
// No database, no network — it always runs.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/mcpsrv"
)

func TestWorkspaceRequiredIsOneSentenceOnBothDoors(t *testing.T) {
	if workspaceRequiredMessage != mcpsrv.WorkspaceRequiredMessage {
		t.Fatalf("the no-workspace refusal drifted between the REST and MCP doors:\n"+
			"  REST (workspaceRequiredMessage): %q\n"+
			"  MCP  (WorkspaceRequiredMessage): %q",
			workspaceRequiredMessage, mcpsrv.WorkspaceRequiredMessage)
	}

	if got := ErrCodeFor(mcpsrv.WorkspaceRequiredMessage); got != CodeWorkspaceRequired {
		t.Fatalf("the MCP door's refusal text resolves to code %q, want %q — an agent refused over "+
			"MCP must get the same machine code a REST client gets, otherwise the two write paths "+
			"disagree about the same account state", got, CodeWorkspaceRequired)
	}

	// The REST side through the same lookup, so the assertion above cannot be
	// satisfied by a table entry that only happens to match one of the two.
	if got := ErrCodeFor(workspaceRequiredMessage); got != CodeWorkspaceRequired {
		t.Fatalf("the REST door's refusal text resolves to code %q, want %q", got, CodeWorkspaceRequired)
	}
}
