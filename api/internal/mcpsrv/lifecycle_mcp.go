package mcpsrv

// The lifecycle table on the MCP surface (card t_52429d6f).
//
// WHY A SURFACE AT ALL. The transition rules already live in ONE place
// (internal/lifecycle) and every write path applies them, so an agent that
// guesses wrong is refused. Refused is still late: the model has already told
// the merchant what it was going to do. Serving the SAME table — not a copy —
// lets an agent ask "what may I do with sale X" before it proposes anything,
// which is the AX half of the one-source rule.
//
// TWO SURFACES, ONE SOURCE. `describe_lifecycle` is the tool an agent calls with
// arguments; `jomform://lifecycle/{aggregate}` is the resource a client can
// list and read without a tool call. Both render `Machine.Describe()`, so the
// table an agent reads and the table Apply enforces cannot drift: a test reads
// the surface's bytes and compares them with the Go table field for field.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
)

// lifecycleResourceURIPrefix is the template's fixed part. The variable is the
// aggregate name, so a client lists ONE template and reads one URI per
// aggregate.
const lifecycleResourceURIPrefix = "jomform://lifecycle/"

// registerLifecycleResource adds the resource template. It is registered from
// New() next to the tools, so a server that has the tool also has the resource.
func (s *Server) registerLifecycleResource() {
	s.mcp.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: lifecycleResourceURIPrefix + "{aggregate}",
		Name:        "lifecycle-table",
		Title:       "Aggregate lifecycle table",
		Description: "The declarative state machine for one aggregate: every state, every event it accepts, the state the event lands in, whether a guard can refuse it, and the dashboard label key of a person's action. Read this before proposing a state change — the same table decides what the write endpoints accept.",
		MIMEType:    "application/json",
	}, s.readLifecycleResource)
}

// readLifecycleResource serves the table as JSON. Auth is required even though
// the table itself is not account data: the transport authenticates the request,
// and a resource that answered without an identity would be the one hole in that
// rule.
func (s *Server) readLifecycleResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if IdentityFromContext(ctx) == nil {
		return nil, errors.New("token missing or invalid")
	}
	uri := ""
	if req != nil && req.Params != nil {
		uri = req.Params.URI
	}
	name := strings.TrimPrefix(uri, lifecycleResourceURIPrefix)
	machine := lifecycle.MachineByName(name)
	if machine == nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	body, err := json.Marshal(machine.Describe())
	if err != nil {
		return nil, fmt.Errorf("encode lifecycle table: %w", err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      uri,
		MIMEType: "application/json",
		Text:     string(body),
	}}}, nil
}

// availableEventsOf is the MCP-side single derivation of a sale row's
// available_events. Non-nil for every sale, so the JSON field is an array even
// when the state accepts nothing.
func availableEventsOf(sl *dbstore.Sale) []lifecycle.AvailableEvent {
	if sl == nil {
		return []lifecycle.AvailableEvent{}
	}
	out := lifecycle.SalesMachine.Available(sl)
	if out == nil {
		return []lifecycle.AvailableEvent{}
	}
	return out
}

// describeLifecycleIn is the tool input. `id` is optional: without it the answer
// is the whole table; with it the answer is that RECORD's current state and the
// actions it accepts (guards evaluated, so a disabled action carries its reason).
type describeLifecycleIn struct {
	Aggregate string `json:"aggregate" jsonschema:"the aggregate to describe (sales, forms)"`
	ID        string `json:"id,omitempty" jsonschema:"optional record id: a sale ref_code, or a form id (a trashed form answers too). With id the answer is that record's current state and available_events; without it the whole transition table is returned"`
}

// lifecycleAnswer is the union of the two answers. The table fields are present
// without id; the record fields are present with one. Every field is derived
// from the machine on the call, never stored.
type lifecycleAnswer struct {
	Aggregate string `json:"aggregate"`
	// State / AvailableEvents are set when the caller named a record.
	State           string                     `json:"state,omitempty"`
	AvailableEvents []lifecycle.AvailableEvent `json:"available_events,omitempty"`
	// AcceptingOrders/Reason/Code are the FORM aggregate's derived buyer state
	// (card t_3d5a8d32) — the same answer the dashboard list and the public page
	// give, so an agent asking "can this form take an order" is not guessing
	// from a 200. Absent for aggregates that have no buyer page.
	AcceptingOrders *bool  `json:"accepting_orders,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Code            string `json:"code,omitempty"`
	// States / Events / Codes are set when the caller asked for the table.
	States []lifecycle.StateDoc `json:"states,omitempty"`
	Events []string             `json:"events,omitempty"`
	Codes  []string             `json:"codes,omitempty"`
}

// describeLifecycle answers "what can I do with this aggregate (or this record)".
func (s *Server) describeLifecycle(ctx context.Context, _ *mcp.CallToolRequest, in describeLifecycleIn) (*mcp.CallToolResult, lifecycleAnswer, error) {
	name := strings.TrimSpace(in.Aggregate)
	machine := lifecycle.MachineByName(name)
	if machine == nil {
		known := make([]string, 0, len(lifecycle.Machines()))
		for _, m := range lifecycle.Machines() {
			known = append(known, m.Name)
		}
		return nil, lifecycleAnswer{}, fmt.Errorf("unknown aggregate %q — known aggregates: %s", in.Aggregate, strings.Join(known, ", "))
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		table := machine.Describe()
		return nil, lifecycleAnswer{
			Aggregate: table.Aggregate,
			States:    table.States,
			Events:    table.Events,
			Codes:     table.Codes,
		}, nil
	}

	// A record id is a sale ref_code or a form id — the two aggregates with a
	// machine. Both lookups are scoped exactly like the write tools: a record
	// in an account this call may not touch is "not found", never another
	// merchant's row.
	switch machine.Name {
	case lifecycle.FormsMachine.Name:
		return s.describeFormLifecycle(ctx, id)
	case lifecycle.SalesMachine.Name:
	default:
		return nil, lifecycleAnswer{}, fmt.Errorf("aggregate %q has no record lookup yet", machine.Name)
	}
	if s.deps.Sales == nil {
		return nil, lifecycleAnswer{}, errors.New("sales not available")
	}
	accs, err := s.ownedOrScopedAccounts(ctx)
	if err != nil {
		return nil, lifecycleAnswer{}, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, id)
	if err != nil {
		return nil, lifecycleAnswer{}, fmt.Errorf("sale not found")
	}
	member := false
	for _, acc := range accs {
		if sale.AccountID == acc {
			member = true
			break
		}
	}
	if !member {
		return nil, lifecycleAnswer{}, fmt.Errorf("sale not found")
	}
	available := machine.Available(sale)
	if available == nil {
		available = []lifecycle.AvailableEvent{}
	}
	return nil, lifecycleAnswer{
		Aggregate:       machine.Name,
		State:           sale.LifecycleState(),
		AvailableEvents: available,
	}, nil
}

// describeFormLifecycle answers "what can I do with THIS form" (card t_3d5a8d32).
//
// It is the same call as the sale branch — one tool, one table — because the AX
// half of the one-source rule is that an agent plans from the machine the write
// paths enforce instead of guessing from a 200.
//
// A TRASHED form is a legitimate subject: GetByID filters it out, so the lookup
// falls back to GetTrashed. Restore and purge are then the only events left, and
// they are exactly what a caller asking this question needs to learn before it
// proposes a publish. The account scope is the caller's own (ownedOrScopedAccounts),
// so another merchant's form id answers "form not found".
func (s *Server) describeFormLifecycle(ctx context.Context, formID string) (*mcp.CallToolResult, lifecycleAnswer, error) {
	if s.deps.Forms == nil {
		return nil, lifecycleAnswer{}, errors.New("forms not available")
	}
	accs, err := s.ownedOrScopedAccounts(ctx)
	if err != nil {
		return nil, lifecycleAnswer{}, err
	}
	var f *dbstore.Form
	for _, acc := range accs {
		if got, gerr := s.deps.Forms.GetByID(ctx, acc, formID); gerr == nil {
			f = got
			break
		}
		if got, gerr := s.deps.Forms.GetTrashed(ctx, acc, formID); gerr == nil {
			f = got
			break
		}
	}
	if f == nil {
		return nil, lifecycleAnswer{}, errors.New("form not found")
	}
	order := s.formOrder(ctx, f)
	accepting := order.Accepting
	return nil, lifecycleAnswer{
		Aggregate:       lifecycle.FormsMachine.Name,
		State:           f.LifecycleState(),
		AvailableEvents: availableFormEvents(f),
		AcceptingOrders: &accepting,
		Reason:          order.Reason,
		Code:            order.Code,
	}, nil
}
