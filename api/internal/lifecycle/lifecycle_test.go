package lifecycle

// Engine tests for the lifecycle machine: the parts that are not about any one
// aggregate. The sales table's own rules live in sales_test.go.

import (
	"errors"
	"strings"
	"testing"
)

// stubEntity is a bare Entity for the engine tests, so they do not depend on
// any aggregate's struct.
type stubEntity struct{ state string }

func (s stubEntity) LifecycleState() string { return s.state }

func testMachine() *Machine {
	return &Machine{
		Name: "widget",
		States: map[string]State{
			"new": {Events: map[string]Transition{
				"start": {To: "running", LabelKey: "widget.start"},
				"skip":  {To: "done"},
			}},
			"running": {Events: map[string]Transition{
				"start": {To: "running"}, // duplicate event: legal no-op
				"stop": {To: "done", LabelKey: "widget.stop", Guard: func(e Entity) error {
					if e.LifecycleState() != "running" {
						return errors.New("the widget is not running")
					}
					return nil
				}},
			}},
			"done": {Events: map[string]Transition{}},
		},
	}
}

func TestApplyReturnsTheTablesTargetAndChangeFlag(t *testing.T) {
	m := testMachine()
	res, err := m.Apply(stubEntity{"new"}, "start")
	if err != nil {
		t.Fatalf("apply start from new: %v", err)
	}
	if res.From != "new" || res.To != "running" || res.Event != "start" || !res.Changed {
		t.Errorf("result = %+v, want from=new to=running changed=true", res)
	}

	// A transition whose target is the current state is a legal no-op — the
	// shape a duplicate gateway callback has.
	noop, err := m.Apply(stubEntity{"running"}, "start")
	if err != nil {
		t.Fatalf("apply start from running: %v", err)
	}
	if noop.Changed {
		t.Errorf("repeated start reported Changed=true, want a no-op")
	}
	if noop.To != "running" {
		t.Errorf("no-op target = %q, want running", noop.To)
	}
}

func TestApplyIllegalTransitionHasStableCode(t *testing.T) {
	m := testMachine()
	_, err := m.Apply(stubEntity{"done"}, "start")
	if err == nil {
		t.Fatal("apply start from a terminal state was allowed")
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("error %v does not match ErrIllegalTransition", err)
	}
	var it *IllegalTransitionError
	if !errors.As(err, &it) {
		t.Fatalf("error %v is not *IllegalTransitionError", err)
	}
	if it.Code() != CodeIllegalTransition {
		t.Errorf("code = %q, want %q", it.Code(), CodeIllegalTransition)
	}
	if !strings.Contains(err.Error(), CodeIllegalTransition) {
		t.Errorf("message %q does not carry the stable code", err.Error())
	}
	if it.From != "done" || it.Event != "start" {
		t.Errorf("error names from=%q event=%q, want done/start", it.From, it.Event)
	}
}

func TestApplyUnknownStateIsIllegalNotAPanic(t *testing.T) {
	m := testMachine()
	_, err := m.Apply(stubEntity{"nonsense"}, "start")
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("unknown state answered %v, want ErrIllegalTransition", err)
	}
	if _, err := m.Apply(nil, "start"); err == nil {
		t.Error("Apply(nil) was allowed")
	}
}

func TestApplyGuardRefusalHasItsOwnCode(t *testing.T) {
	m := testMachine()
	// The `stop` edge only exists from running; use a state that HAS the edge
	// but whose guard refuses. That needs a machine where the guard can fail
	// from the state it is defined on, so mutate the fixture's guard.
	m.States["new"].Events["stop"] = Transition{To: "done", Guard: func(Entity) error {
		return errors.New("the widget is not running")
	}}
	_, err := m.Apply(stubEntity{"new"}, "stop")
	if !errors.Is(err, ErrTransitionRefused) {
		t.Fatalf("guard refusal answered %v, want ErrTransitionRefused", err)
	}
	if errors.Is(err, ErrIllegalTransition) {
		t.Error("a guard refusal must not be reported as a missing edge")
	}
	var ref *TransitionRefusedError
	if !errors.As(err, &ref) {
		t.Fatalf("error %v is not *TransitionRefusedError", err)
	}
	if ref.Code() != CodeTransitionRefused {
		t.Errorf("code = %q, want %q", ref.Code(), CodeTransitionRefused)
	}
	if !strings.Contains(err.Error(), "the widget is not running") {
		t.Errorf("message %q does not carry the guard's reason", err.Error())
	}
}

func TestAvailableListsEnabledActionsWithoutNoops(t *testing.T) {
	m := testMachine()
	got := m.Available(stubEntity{"new"})
	if len(got) != 2 {
		t.Fatalf("Available(new) = %+v, want the two declared events", got)
	}
	// Sorted by event name: skip, start.
	if got[0].Event != "skip" || got[1].Event != "start" {
		t.Errorf("Available order = %q,%q, want skip,start", got[0].Event, got[1].Event)
	}
	for _, av := range got {
		if !av.Enabled || av.Reason != "" {
			t.Errorf("Available(new) %+v: want enabled with no reason", av)
		}
	}
	if got[0].LabelKey != "" || got[1].LabelKey != "widget.start" {
		t.Errorf("label keys = %q,%q", got[0].LabelKey, got[1].LabelKey)
	}

	// A repeated event is not an action: `start` is a no-op while running and
	// must not be offered, while `stop` is offered.
	running := m.Available(stubEntity{"running"})
	if len(running) != 1 || running[0].Event != "stop" {
		t.Fatalf("Available(running) = %+v, want only stop", running)
	}
	if !running[0].Enabled {
		t.Errorf("stop is disabled for a running widget: %+v", running[0])
	}

	// A terminal state offers nothing.
	if got := m.Available(stubEntity{"done"}); len(got) != 0 {
		t.Errorf("Available(done) = %+v, want none", got)
	}
}

func TestAvailableReportsDisabledActionsWithAReason(t *testing.T) {
	m := testMachine()
	m.States["new"].Events["stop"] = Transition{To: "done", LabelKey: "widget.stop", Guard: func(Entity) error {
		return errors.New("the widget is not running")
	}}
	got := m.Available(stubEntity{"new"})
	var stop *AvailableEvent
	for i := range got {
		if got[i].Event == "stop" {
			stop = &got[i]
		}
	}
	if stop == nil {
		t.Fatalf("stop missing from Available(new): %+v", got)
	}
	if stop.Enabled {
		t.Error("stop is enabled although its guard refuses")
	}
	if stop.Reason != "the widget is not running" {
		t.Errorf("reason = %q, want the guard's sentence", stop.Reason)
	}
}

func TestValidateCatchesATableTypo(t *testing.T) {
	good := testMachine()
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate on a sound table: %v", err)
	}

	broken := testMachine()
	broken.States["new"].Events["start"] = Transition{To: "runningg"}
	err := broken.Validate()
	if err == nil {
		t.Fatal("Validate accepted a transition to an undeclared state")
	}
	if !strings.Contains(err.Error(), "runningg") {
		t.Errorf("error %q does not name the offending target", err.Error())
	}

	blank := &Machine{Name: " "}
	if err := blank.Validate(); err == nil {
		t.Error("Validate accepted a machine with no name")
	}
}

func TestEventsAndStatesAreComplete(t *testing.T) {
	m := testMachine()
	if got, want := strings.Join(m.Events(), ","), "skip,start,stop"; got != want {
		t.Errorf("Events() = %q, want %q", got, want)
	}
	if got, want := strings.Join(m.StateNames(), ","), "done,new,running"; got != want {
		t.Errorf("StateNames() = %q, want %q", got, want)
	}
	if !m.Allows("new", "start") || m.Allows("done", "start") {
		t.Error("Allows disagrees with the table")
	}
}
