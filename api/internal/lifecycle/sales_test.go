package lifecycle

// The sales table's own tests. The point of these is that the table, not a
// handler, is the thing under test: every (state, event) pair is generated and
// asserted, so a state or an event added without its rule cannot slip through.

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// saleStatesFromTheDatabase is the status vocabulary the DB itself accepts,
// copied from internal/db/migrations/000002_skip_capture.up.sql:
//
//	CHECK (status IN ('pending', 'authorized', 'paid', 'confirmed',
//	                  'refunded', 'cancelled', 'failed'))
//
// The machine does not invent states: it reads this list. Pinning the two
// together is the drift guard — a new DB state must be added to the machine in
// the same change, or this test fails.
var saleStatesFromTheDatabase = []string{
	dbstore.SalePending,
	dbstore.SaleAuthorized,
	dbstore.SalePaid,
	dbstore.SaleConfirmed,
	dbstore.SaleRefunded,
	dbstore.SaleCancelled,
	dbstore.SaleFailed,
}

// authorizedSale is a sale every guard accepts: an uncaptured skip_capture
// authorization. Tests that are not about the guard use it so the table's edges
// are exercised rather than its preconditions.
func authorizedSale(state string) *dbstore.Sale {
	return &dbstore.Sale{
		Status:         state,
		SkipCapture:    true,
		ChipPurchaseID: "pur_lifecycle_test",
	}
}

func TestSalesMachineIsWellFormed(t *testing.T) {
	if err := SalesMachine.Validate(); err != nil {
		t.Fatalf("the sales table is invalid: %v", err)
	}
}

func TestSalesMachineStatesAreTheDatabaseVocabulary(t *testing.T) {
	want := append([]string(nil), saleStatesFromTheDatabase...)
	sort.Strings(want)
	got := SalesMachine.StateNames()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("machine states = %v, database states = %v\n"+
			"The machine must read the column's own vocabulary (migration 000002); "+
			"adding a state to one without the other is how a write path ends up "+
			"unable to express a real row.", got, want)
	}
}

// TestSalesTableCoversEveryStateEventPair generates the full cross product and
// asserts allowed/rejected for each pair — the coverage rule from the card: a
// transition cannot be added, removed or retargeted without a test saying so.
func TestSalesTableCoversEveryStateEventPair(t *testing.T) {
	states := SalesMachine.StateNames()
	events := SalesMachine.Events()
	if len(states) == 0 || len(events) == 0 {
		t.Fatal("no states or no events — the walk would pass vacuously")
	}

	for _, state := range states {
		for _, event := range events {
			state, event := state, event
			t.Run(state+"__"+event, func(t *testing.T) {
				entity := authorizedSale(state)
				res, err := SalesMachine.Apply(entity, event)

				if !SalesMachine.Allows(state, event) {
					if err == nil {
						t.Fatalf("%s accepts %q although the table has no such edge (landed in %q)",
							state, event, res.To)
					}
					if !errors.Is(err, ErrIllegalTransition) {
						t.Fatalf("missing edge %s/%s answered %v, want ErrIllegalTransition", state, event, err)
					}
					return
				}

				if err != nil {
					t.Fatalf("%s/%s is in the table but Apply refused it: %v", state, event, err)
				}
				wantTo := SalesMachine.States[state].Events[event].To
				if res.To != wantTo {
					t.Errorf("%s/%s target = %q, want %q", state, event, res.To, wantTo)
				}
				if res.Changed != (wantTo != state) {
					t.Errorf("%s/%s Changed = %v, want %v", state, event, res.Changed, wantTo != state)
				}
				if res.From != state {
					t.Errorf("%s/%s reported From = %q", state, event, res.From)
				}
			})
		}
	}
}

// TestSalesTerminalStatesOnlyAcceptTheirOwnRepeat pins the "no transition from
// failed/cancelled" rule, and the one exception that exists today: a redelivered
// callback for the event that produced the state is a legal no-op.
func TestSalesTerminalStatesOnlyAcceptTheirOwnRepeat(t *testing.T) {
	cases := []struct {
		state     string
		repeat    string
		forbidden []string
	}{
		{dbstore.SaleFailed, SaleEventFail,
			[]string{SaleEventPay, SaleEventCapture, SaleEventConfirm, SaleEventRelease, SaleEventRefund}},
		{dbstore.SaleCancelled, "",
			[]string{SaleEventPay, SaleEventCapture, SaleEventConfirm, SaleEventRelease, SaleEventRefund, SaleEventFail}},
		{dbstore.SaleRefunded, SaleEventRefund,
			[]string{SaleEventPay, SaleEventCapture, SaleEventConfirm, SaleEventRelease, SaleEventFail}},
	}
	for _, c := range cases {
		entity := authorizedSale(c.state)
		if c.repeat != "" {
			res, err := SalesMachine.Apply(entity, c.repeat)
			if err != nil {
				t.Errorf("%s: repeating %q = %v, want a legal no-op", c.state, c.repeat, err)
			} else if res.Changed {
				t.Errorf("%s: repeating %q reported a change to %q", c.state, c.repeat, res.To)
			}
		}
		for _, event := range c.forbidden {
			if _, err := SalesMachine.Apply(entity, event); !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("%s/%s = %v, want ErrIllegalTransition (the state is terminal)", c.state, event, err)
			}
		}
	}
}

// TestSalesEveryStateThatCanConfirmIsDeclared keeps the two real confirmation
// paths explicit: a bank transfer is confirmed from pending, a settled CHIP
// payment from paid (docs/CHIP-SANDBOX-E2E.md scenarios 2 and 4).
func TestSalesEveryStateThatCanConfirmIsDeclared(t *testing.T) {
	for _, state := range []string{dbstore.SalePending, dbstore.SalePaid} {
		res, err := SalesMachine.Apply(authorizedSale(state), SaleEventConfirm)
		if err != nil {
			t.Errorf("%s: confirm = %v, want a move to confirmed", state, err)
			continue
		}
		if res.To != dbstore.SaleConfirmed {
			t.Errorf("%s: confirm lands in %q, want confirmed", state, res.To)
		}
	}
}

func TestSalesCaptureAndReleaseRefuseANonAuthorization(t *testing.T) {
	cases := map[string]*dbstore.Sale{
		"an ordinary CHIP sale": {
			Status: dbstore.SalePending,
		},
		"an authorization already captured": {
			Status:         dbstore.SalePending,
			SkipCapture:    true,
			ChipPurchaseID: "pur_captured",
			CapturedAt:     func() *time.Time { now := time.Now(); return &now }(),
		},
		"an authorization with no purchase id": {
			Status:      dbstore.SalePending,
			SkipCapture: true,
		},
	}
	for name, sale := range cases {
		for _, event := range []string{SaleEventCapture, SaleEventRelease} {
			_, err := SalesMachine.Apply(sale, event)
			if !errors.Is(err, ErrTransitionRefused) {
				t.Errorf("%s: %s = %v, want ErrTransitionRefused", name, event, err)
				continue
			}
			if !strings.Contains(err.Error(), "not an authorization awaiting capture") {
				t.Errorf("%s: %s message = %q, want the awaiting-capture sentence", name, event, err.Error())
			}
		}
	}
}

func TestSalesAvailableEventsFromPending(t *testing.T) {
	got := SalesMachine.Available(authorizedSale(dbstore.SalePending))
	byEvent := map[string]AvailableEvent{}
	for _, av := range got {
		byEvent[av.Event] = av
	}

	// The four merchant actions, each with the dashboard key it renders from.
	want := map[string]string{
		SaleEventCapture: "bld.capture",
		SaleEventConfirm: "sales.confirmSale",
		SaleEventRelease: "bld.release",
	}
	for event, label := range want {
		av, ok := byEvent[event]
		if !ok {
			t.Errorf("%s missing from Available(pending)", event)
			continue
		}
		if !av.Enabled || av.Reason != "" {
			t.Errorf("%s = %+v, want enabled with no reason for an uncaptured authorization", av.Event, av)
		}
		if av.LabelKey != label {
			t.Errorf("%s label_key = %q, want %q", event, av.LabelKey, label)
		}
	}

	// Gateway-driven events are in the table but are not buttons: they carry no
	// label key, so the UI must not render them.
	for _, event := range []string{SaleEventPay, SaleEventFail} {
		av, ok := byEvent[event]
		if !ok {
			t.Errorf("%s missing from Available(pending)", event)
			continue
		}
		if av.LabelKey != "" {
			t.Errorf("%s label_key = %q, want empty (not a person's action)", event, av.LabelKey)
		}
	}
}

func TestSalesAvailableEventsExplainADisabledRefund(t *testing.T) {
	// An ordinary CHIP sale: capture and release are defined for pending but
	// the guard refuses them, and the refusal must be visible as a reason.
	sale := &dbstore.Sale{Status: dbstore.SalePending}
	got := SalesMachine.Available(sale)
	seen := false
	for _, av := range got {
		if av.Event != SaleEventRelease && av.Event != SaleEventCapture {
			continue
		}
		seen = true
		if av.Enabled {
			t.Errorf("%s is enabled for a sale that is not an authorization", av.Event)
		}
		if !strings.Contains(av.Reason, "not an authorization awaiting capture") {
			t.Errorf("%s reason = %q, want the awaiting-capture sentence", av.Event, av.Reason)
		}
	}
	if !seen {
		t.Fatal("capture/release missing from Available(pending)")
	}
}

func TestSalesAvailableEventsFromPaid(t *testing.T) {
	sale := authorizedSale(dbstore.SalePaid)
	byEvent := map[string]AvailableEvent{}
	for _, av := range SalesMachine.Available(sale) {
		byEvent[av.Event] = av
	}

	// `pay` is a legal repeat but not an action: a duplicate callback must not
	// show up as a "Pay" button.
	if _, ok := byEvent[SaleEventPay]; ok {
		t.Error("Available(paid) offers `pay`; a repeated callback is not an action")
	}
	// Capturing an authorization whose sale already reads paid IS an action
	// (the charge still has to happen), so it is offered even though the status
	// will not move.
	if av, ok := byEvent[SaleEventCapture]; !ok || !av.Enabled || av.LabelKey != "bld.capture" {
		t.Errorf("capture from paid = %+v (present=%v), want enabled with the bld.capture key", av, ok)
	}
	if av, ok := byEvent[SaleEventRefund]; !ok || !av.Enabled || av.LabelKey != "sales.refund" {
		t.Errorf("refund from paid = %+v (present=%v), want enabled with the sales.refund key", av, ok)
	}
	if av, ok := byEvent[SaleEventConfirm]; !ok || !av.Enabled {
		t.Errorf("confirm from paid = %+v (present=%v), want enabled", av, ok)
	}
}

// TestSalesEveryLabelKeyIsAnI18nKeyShape keeps the table honest about what it
// hands the dashboard: a label key is rendered with t(key), which prints the
// raw key when it does not exist — the exact defect class a server-driven
// button is meant to end.
func TestSalesEveryLabelKeyIsAnI18nKeyShape(t *testing.T) {
	for state, st := range SalesMachine.States {
		for event, tr := range st.Events {
			if tr.LabelKey == "" {
				continue
			}
			if !strings.Contains(tr.LabelKey, ".") {
				t.Errorf("%s/%s label_key = %q, want a namespaced i18n key like sales.confirmSale",
					state, event, tr.LabelKey)
			}
			if strings.ContainsAny(tr.LabelKey, " \t") {
				t.Errorf("%s/%s label_key = %q contains whitespace", state, event, tr.LabelKey)
			}
		}
	}
}
