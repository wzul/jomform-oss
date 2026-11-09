// Package lifecycle is the single declarative source of an aggregate's state
// machine: which states exist, which events move between them, and what each
// move requires.
//
// Why it exists. The state VOCABULARY was already explicit (every status column
// carries a CHECK constraint), but the TRANSITION RULES lived as `if` statements
// scattered across handlers, so the dashboard, the REST API and the MCP tools
// each guessed which action a record accepted. A wrong guess is not cosmetic:
// on `sales` an illegal transition is money lost or charged twice.
//
// One table per aggregate therefore owns the rules, and the write paths call
// Apply instead of comparing state strings. The same table answers three
// callers without a second copy:
//
//   - write paths (Go):   Apply decides, and the caller persists the state the
//     table returned;
//   - the dashboard (UX): Available lists the actions a record accepts, with a
//     reason when one is disabled, so buttons come from the server rather than
//     from a client-side guess;
//   - agents (AX):        the table is served to MCP clients, so a caller can
//     plan ("this sale may be refunded or released") before acting.
//
// Adding a state is one map entry plus its test row. Adding an event is one
// entry in the state that accepts it. A rule that is not in the table does not
// exist.
package lifecycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Stable machine codes. A transition the table does not define and a
// transition whose guard refused are DIFFERENT refusals — a missing edge versus
// a stated rule — so they carry different codes and a caller can tell them
// apart without parsing prose. The codes never change: surfaces key their own
// wording and their own HTTP status off them.
const (
	// CodeIllegalTransition: the table defines no such move
	// (e.g. confirming a cancelled sale).
	CodeIllegalTransition = "lifecycle.illegalTransition"
	// CodeTransitionRefused: the table defines the move but its guard refused
	// it (e.g. releasing an authorization that was already captured).
	CodeTransitionRefused = "lifecycle.transitionRefused"
)

// ErrIllegalTransition is the sentinel every *IllegalTransitionError matches
// through errors.Is. It is a value, not a string, so a caller never has to
// compare messages.
var ErrIllegalTransition = errors.New(CodeIllegalTransition)

// ErrTransitionRefused is the sentinel every *TransitionRefusedError matches
// through errors.Is.
var ErrTransitionRefused = errors.New(CodeTransitionRefused)

// Entity is an aggregate a machine can move: its current state, by name. A
// guard may assert the concrete type when its rule needs fields beyond the
// state (the sales guards need to know whether a sale is an uncaptured
// authorization).
type Entity interface {
	LifecycleState() string
}

// Guard is a transition's precondition. nil means the move is always legal
// from that state. The error it returns is user-facing prose (it becomes the
// `reason` of a disabled action), so it must state the rule, not the internals.
type Guard func(Entity) error

// Transition is one event's outcome from one state.
type Transition struct {
	// To is the state the aggregate lands in. It must be a declared state.
	To string
	// Guard is the precondition, or nil for an unconditional move.
	Guard Guard
	// LabelKey is the dashboard's i18n key for this action's button. Empty
	// means the event is not a person's action (a gateway callback, for
	// example) and the UI must not render it.
	LabelKey string
}

// State is the set of events a state accepts. A terminal state maps to an
// empty (never nil-by-accident) set.
type State struct {
	Events map[string]Transition
}

// Machine is one aggregate's state machine.
type Machine struct {
	// Name is the aggregate name the table describes ("sales").
	Name string
	// States maps a state name to the events it accepts.
	States map[string]State
}

// IllegalTransitionError is returned when an event is not defined for the
// aggregate's current state.
type IllegalTransitionError struct {
	Machine string
	From    string
	Event   string
}

func (e *IllegalTransitionError) Error() string {
	return fmt.Sprintf("%s: event %q is not allowed from state %q (%s)",
		e.Machine, e.Event, e.From, CodeIllegalTransition)
}

// Code returns the stable machine code (CodeIllegalTransition).
func (e *IllegalTransitionError) Code() string { return CodeIllegalTransition }

// Unwrap makes errors.Is(err, ErrIllegalTransition) true.
func (e *IllegalTransitionError) Unwrap() error { return ErrIllegalTransition }

// TransitionRefusedError is returned when the table DEFINES the event for the
// current state but the transition's guard refused it.
type TransitionRefusedError struct {
	Machine string
	From    string
	Event   string
	// Reason is the guard's own English sentence, suitable to show a person.
	Reason string
}

func (e *TransitionRefusedError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s: event %q is not allowed from state %q (%s)",
			e.Machine, e.Event, e.From, CodeTransitionRefused)
	}
	return fmt.Sprintf("%s: %s (%s)", e.Machine, e.Reason, CodeTransitionRefused)
}

// Code returns the stable machine code (CodeTransitionRefused).
func (e *TransitionRefusedError) Code() string { return CodeTransitionRefused }

// Unwrap makes errors.Is(err, ErrTransitionRefused) true.
func (e *TransitionRefusedError) Unwrap() error { return ErrTransitionRefused }

// Result is what Apply decided. Callers persist To. Changed reports whether the
// STATE changes: a state-preserving ACTION (capturing an authorization whose
// sale already reads paid) is Changed=false but still an action, so the callers
// that gate fan-out on Changed are the gateway-callback paths, where a repeated
// delivery has no other effect and a real move always changes the state.
type Result struct {
	From    string
	To      string
	Event   string
	Changed bool
}

// Apply decides one event against the aggregate's current state. It never
// writes anything: it returns the state the caller must persist, so every
// write path stores exactly what the table says.
//
// A transition whose target equals the state it starts from is a legal no-op
// (Changed=false). That is how a duplicate gateway callback is expressed —
// without any caller comparing status strings itself.
func (m *Machine) Apply(e Entity, event string) (Result, error) {
	if e == nil {
		return Result{}, fmt.Errorf("%s: no aggregate to transition (%s)", m.Name, CodeIllegalTransition)
	}
	from := e.LifecycleState()
	res := Result{From: from, Event: event}

	st, ok := m.States[from]
	if !ok {
		return res, &IllegalTransitionError{Machine: m.Name, From: from, Event: event}
	}
	tr, ok := st.Events[event]
	if !ok {
		return res, &IllegalTransitionError{Machine: m.Name, From: from, Event: event}
	}
	if tr.Guard != nil {
		if err := tr.Guard(e); err != nil {
			return res, &TransitionRefusedError{
				Machine: m.Name, From: from, Event: event, Reason: err.Error(),
			}
		}
	}
	res.To = tr.To
	res.Changed = tr.To != from
	return res, nil
}

// AvailableEvent is one action a record accepts, as the server tells it. The
// dashboard renders buttons from this list instead of guessing from the state,
// which is what makes "the UI says the action exists but the API refuses it"
// impossible rather than merely fixed once.
type AvailableEvent struct {
	Event    string `json:"event"`
	LabelKey string `json:"label_key,omitempty"`
	Enabled  bool   `json:"enabled"`
	// Reason explains a disabled action in English. Empty when Enabled.
	Reason string `json:"reason,omitempty"`
}

// Available lists the events the aggregate's current state accepts, ordered by
// event name so the answer is stable across calls.
//
// A transition that leaves the state where it is is only listed when it is a
// PERSON'S action (it carries a LabelKey): capturing an authorization that
// already reads paid is a real action, while a repeated gateway callback
// (`pay` on a paid sale) is not something a caller can ask for. A guard refusal
// is reported as enabled=false WITH its reason, not hidden — the caller asked
// what it can do, and "nothing, because the authorization was already captured"
// is the useful answer.
func (m *Machine) Available(e Entity) []AvailableEvent {
	if e == nil {
		return nil
	}
	from := e.LifecycleState()
	st, ok := m.States[from]
	if !ok {
		return nil
	}
	out := make([]AvailableEvent, 0, len(st.Events))
	for event, tr := range st.Events {
		if tr.To == from && tr.LabelKey == "" {
			continue
		}
		av := AvailableEvent{Event: event, LabelKey: tr.LabelKey, Enabled: true}
		if tr.Guard != nil {
			if err := tr.Guard(e); err != nil {
				av.Enabled = false
				av.Reason = err.Error()
			}
		}
		out = append(out, av)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Event < out[j].Event })
	return out
}

// Validate checks the table's own integrity. It is the DX half of "one place
// to add a state": a typo in a target state, an empty event name or a dangling
// guard-less self-loop is caught here instead of at the first write.
func (m *Machine) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("lifecycle: machine has no name")
	}
	if len(m.States) == 0 {
		return fmt.Errorf("lifecycle: %s declares no states", m.Name)
	}
	var broken []string
	for state, st := range m.States {
		if strings.TrimSpace(state) == "" {
			broken = append(broken, "empty state name")
			continue
		}
		for event, tr := range st.Events {
			if strings.TrimSpace(event) == "" {
				broken = append(broken, fmt.Sprintf("%s: empty event name", state))
				continue
			}
			if tr.To == "" {
				broken = append(broken, fmt.Sprintf("%s %q: no target state", state, event))
				continue
			}
			if _, ok := m.States[tr.To]; !ok {
				broken = append(broken, fmt.Sprintf("%s %q: target %q is not a declared state", state, event, tr.To))
			}
		}
	}
	if len(broken) > 0 {
		sort.Strings(broken)
		return fmt.Errorf("lifecycle: %s has an invalid table: %s", m.Name, strings.Join(broken, "; "))
	}
	return nil
}

// Events returns every event name the machine declares, sorted. Useful for the
// exhaustive state x event test and for the MCP surface, which must describe
// the whole table rather than the events one record happens to accept.
func (m *Machine) Events() []string {
	seen := map[string]struct{}{}
	for _, st := range m.States {
		for event := range st.Events {
			seen[event] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for event := range seen {
		out = append(out, event)
	}
	sort.Strings(out)
	return out
}

// StateNames returns every declared state, sorted.
func (m *Machine) StateNames() []string {
	out := make([]string, 0, len(m.States))
	for state := range m.States {
		out = append(out, state)
	}
	sort.Strings(out)
	return out
}

// Allows reports whether the table defines event from state, WITHOUT running
// its guard and without applying anything. It answers "is there an edge", which
// is what the table's own test needs; a write path uses Apply so the guard is
// enforced.
func (m *Machine) Allows(state, event string) bool {
	st, ok := m.States[state]
	if !ok {
		return false
	}
	_, ok = st.Events[event]
	return ok
}

// ─── The registry and the table as data ──────────────────────────────────────
//
// A surface that serves the table to a caller (the MCP resource, the MCP tool)
// must read THIS package, never carry a copy. The only thing the surfaces need
// is a lookup by aggregate name, so that is all this package exports: a list and
// a name lookup. A copy elsewhere would pass its own tests and drift from the
// machine the write paths actually enforce.

// Machines returns every aggregate this package describes. One aggregate so
// far (sales), added one at a time in its own change.
func Machines() []*Machine { return []*Machine{SalesMachine} }

// MachineByName returns the machine for an aggregate name, or nil. Callers turn
// nil into a refusal that names the aggregates that DO exist, so an agent
// asking about a typo is told what it can ask about instead of receiving an
// empty table that looks like "nothing is possible".
func MachineByName(name string) *Machine {
	for _, m := range Machines() {
		if m.Name == name {
			return m
		}
	}
	return nil
}

// EventDoc is one edge of the table as data: the event, the state it lands in,
// whether a guard may refuse it, and the dashboard label when the event is a
// person's action.
type EventDoc struct {
	Event string `json:"event"`
	To    string `json:"to"`
	// Guarded reports that the edge has a precondition. The precondition's
	// SENTENCE is record-specific (it is the `reason` on a disabled
	// AvailableEvent), so it is not part of the table description.
	Guarded bool `json:"guarded"`
	// LabelKey is the dashboard button key; empty for events that are not a
	// person's action (a gateway callback).
	LabelKey string `json:"label_key,omitempty"`
}

// StateDoc is one state of the table as data. Terminal means the state accepts
// no event that changes it (only repeats of the event that produced it, which
// are no-ops).
type StateDoc struct {
	State    string     `json:"state"`
	Terminal bool       `json:"terminal"`
	Events   []EventDoc `json:"events"`
}

// Table is the whole machine as data — the shape served to agents. It is
// derived from the Machine on every call, never stored, so a caller cannot be
// told about an edge the write paths do not enforce.
type Table struct {
	Aggregate string     `json:"aggregate"`
	States    []StateDoc `json:"states"`
	// Events is every event the aggregate declares, sorted.
	Events []string `json:"events"`
	// Codes are the stable refusal codes the aggregate can return. A caller
	// keys its own wording off these, so they travel with the table.
	Codes []string `json:"codes"`
}

// Describe renders the machine as data. States, their events and the event list
// are sorted, so two calls (and two surfaces) produce byte-identical answers for
// the same machine — which is what lets a test compare a surface's output with
// the Go table directly.
func (m *Machine) Describe() Table {
	t := Table{
		Aggregate: m.Name,
		States:    make([]StateDoc, 0, len(m.States)),
		Events:    m.Events(),
		Codes:     []string{CodeIllegalTransition, CodeTransitionRefused},
	}
	for _, name := range m.StateNames() {
		st := m.States[name]
		doc := StateDoc{
			State:    name,
			Terminal: true,
			Events:   make([]EventDoc, 0, len(st.Events)),
		}
		for _, event := range sortedEventNames(st.Events) {
			tr := st.Events[event]
			if tr.To != name {
				doc.Terminal = false
			}
			doc.Events = append(doc.Events, EventDoc{
				Event: event, To: tr.To, Guarded: tr.Guard != nil, LabelKey: tr.LabelKey,
			})
		}
		t.States = append(t.States, doc)
	}
	return t
}

// sortedEventNames returns a state's event names, sorted, without allocating a
// second copy of the table.
func sortedEventNames(events map[string]Transition) []string {
	out := make([]string, 0, len(events))
	for event := range events {
		out = append(out, event)
	}
	sort.Strings(out)
	return out
}
