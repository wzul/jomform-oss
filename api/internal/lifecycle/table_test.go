package lifecycle

// The table as DATA: the registry that surfaces look a machine up by, and the
// Describe() rendering they serve. These tests exist so a surface (MCP tool,
// MCP resource) cannot answer with a copy that drifts: Describe is derived from
// the same map Apply enforces, and the surfaces are compared against it.

import (
	"reflect"
	"testing"
)

func TestMachineRegistryFindTheSalesMachine(t *testing.T) {
	if got := MachineByName("sales"); got != SalesMachine {
		t.Fatalf("MachineByName(sales) = %p, want the package's own SalesMachine %p", got, SalesMachine)
	}
	if got := MachineByName("nope"); got != nil {
		t.Errorf("MachineByName(nope) = %v, want nil so a surface can refuse by name", got)
	}
	if len(Machines()) == 0 {
		t.Fatal("Machines() is empty — a surface would have nothing to serve")
	}
	for _, m := range Machines() {
		if m == nil {
			t.Fatal("Machines() contains a nil machine")
		}
		if err := m.Validate(); err != nil {
			t.Errorf("machine %q does not validate: %v", m.Name, err)
		}
	}
}

func TestDescribeCoversEveryStateAndEvent(t *testing.T) {
	table := SalesMachine.Describe()
	if table.Aggregate != "sales" {
		t.Errorf("Describe aggregate = %q, want sales", table.Aggregate)
	}
	if len(table.States) != len(SalesMachine.States) {
		t.Fatalf("Describe lists %d states, the machine declares %d", len(table.States), len(SalesMachine.States))
	}
	// Every edge of the machine must appear exactly once in the rendering — the
	// property that makes the surface's answer the same table as Apply's.
	seen := map[string]bool{}
	for _, st := range table.States {
		if _, ok := SalesMachine.States[st.State]; !ok {
			t.Errorf("Describe invented state %q", st.State)
		}
		for _, ev := range st.Events {
			key := st.State + "/" + ev.Event
			if seen[key] {
				t.Errorf("Describe lists %s twice", key)
			}
			seen[key] = true
			tr, ok := SalesMachine.States[st.State].Events[ev.Event]
			if !ok {
				t.Errorf("Describe lists %s, which the machine does not define", key)
				continue
			}
			if ev.To != tr.To {
				t.Errorf("%s → %q, machine says %q", key, ev.To, tr.To)
			}
			if ev.LabelKey != tr.LabelKey {
				t.Errorf("%s label_key = %q, machine says %q", key, ev.LabelKey, tr.LabelKey)
			}
			if ev.Guarded != (tr.Guard != nil) {
				t.Errorf("%s guarded = %v, machine guard present = %v", key, ev.Guarded, tr.Guard != nil)
			}
		}
	}
	for state, st := range SalesMachine.States {
		for event := range st.Events {
			if !seen[state+"/"+event] {
				t.Errorf("Describe omits the edge %s/%s", state, event)
			}
		}
	}
	wantEvents := SalesMachine.Events()
	if !reflect.DeepEqual(table.Events, wantEvents) {
		t.Errorf("Describe events = %v, want %v", table.Events, wantEvents)
	}
	if len(table.Codes) != 2 || table.Codes[0] != CodeIllegalTransition || table.Codes[1] != CodeTransitionRefused {
		t.Errorf("Describe codes = %v, want the two stable codes", table.Codes)
	}
}

func TestDescribeMarksTerminalStates(t *testing.T) {
	var cancelled, pending *StateDoc
	table := SalesMachine.Describe()
	for i := range table.States {
		switch table.States[i].State {
		case "cancelled":
			cancelled = &table.States[i]
		case "pending":
			pending = &table.States[i]
		}
	}
	if cancelled == nil || pending == nil {
		t.Fatal("Describe is missing cancelled or pending")
	}
	if !cancelled.Terminal {
		t.Error("cancelled is terminal (no event changes it), Describe says otherwise")
	}
	if pending.Terminal {
		t.Error("pending accepts moves, Describe calls it terminal")
	}
	// A state that only repeats its own event is still terminal.
	if len(cancelled.Events) != 0 {
		t.Errorf("cancelled should have no events, Describe lists %d", len(cancelled.Events))
	}
}

func TestDescribeIsStableAcrossCalls(t *testing.T) {
	first := SalesMachine.Describe()
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(first, SalesMachine.Describe()) {
			t.Fatalf("Describe is not deterministic (differs on call %d)", i+2)
		}
	}
}
