package lifecycle

// The DUPLICATED STATE, frozen.
//
// `sales` records "this authorization was captured" twice: as
// status = 'authorized' (a CHECK value no write path produces) and as the
// `authorized` BOOLEAN column (written only by dbstore's MarkCaptured, together
// with captured_at). The table resolves the duplication by fiat — status wins,
// the boolean is an annotation — and these tests pin that resolution so a later
// change cannot quietly make the boolean authoritative.
//
// A card may not fix the duplication (it touches a column the dashboard and the
// REST payload read); it may only prove what today's behaviour IS. That is what
// this file does. The queue-vs-table divergence it was written against is now
// closed: the bulk-capture queue's status set is dbstore.CaptureQueueStatuses,
// pinned to SalesMachine.StatesAllowing(SaleEventCapture) in
// sales_capture_queue_test.go.

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestSaleAuthorizedFieldNeverDecidesAState: for EVERY state in the table, two
// rows that differ only in the legacy boolean must get an identical answer from
// LifecycleState, from Available and from Apply over EVERY event.
//
// This is the property the resolution claims: the boolean can neither open nor
// close an edge, because the table never reads it. Flip the boolean and nothing
// about the machine's answer may move.
func TestSaleAuthorizedFieldNeverDecidesAState(t *testing.T) {
	states := SalesMachine.StateNames()
	events := SalesMachine.Events()
	if len(states) == 0 || len(events) == 0 {
		t.Fatal("no states or no events — the walk would pass vacuously")
	}

	for _, state := range states {
		state := state
		t.Run(state, func(t *testing.T) {
			flagOff := authorizedSale(state) // Authorized=false (the zero value)
			flagOn := authorizedSale(state)
			flagOn.Authorized = true

			if got, want := flagOff.LifecycleState(), flagOn.LifecycleState(); got != want {
				t.Fatalf("LifecycleState reads the legacy boolean: authorized=false → %q, authorized=true → %q", got, want)
			}
			if got := flagOn.LifecycleState(); got != state {
				t.Fatalf("LifecycleState = %q, want the Status %q (status is the authority)", got, state)
			}

			if off, on := SalesMachine.Available(flagOff), SalesMachine.Available(flagOn); !reflect.DeepEqual(off, on) {
				t.Fatalf("Available changes with the legacy boolean:\n  authorized=false → %+v\n  authorized=true  → %+v", off, on)
			}

			for _, event := range events {
				offRes, offErr := SalesMachine.Apply(flagOff, event)
				onRes, onErr := SalesMachine.Apply(flagOn, event)

				if (offErr == nil) != (onErr == nil) {
					t.Fatalf("%s/%s: the boolean decides legality — authorized=false → %v, authorized=true → %v",
						state, event, offErr, onErr)
				}
				if offErr != nil {
					// The KIND of refusal must match too: a missing edge and a
					// guard refusal are different answers to a caller.
					if got, want := errors.Is(onErr, ErrTransitionRefused), errors.Is(offErr, ErrTransitionRefused); got != want {
						t.Fatalf("%s/%s: refusal kind changes with the boolean — refused=%v vs %v (%v / %v)",
							state, event, want, got, offErr, onErr)
					}
					continue
				}
				if offRes != onRes {
					t.Fatalf("%s/%s: the boolean changes the outcome — authorized=false → %+v, authorized=true → %+v",
						state, event, offRes, onRes)
				}
			}
		})
	}
}

// TestSaleCaptureLegalityComesFromTheGuardNotTheBoolean: the two money moves
// that only mean something on an UNCAPTURED authorization are decided by
// skip_capture + chip_purchase_id + captured_at — the same three fields the
// capture queue queries — and never by `authorized`. So:
//
//   - capture/release are EDGES on pending/authorized/paid/confirmed and are
//     refused by the GUARD (ErrTransitionRefused, not ErrIllegalTransition) when
//     the sale is not an uncaptured authorization;
//   - flipping the legacy boolean changes neither the edge nor the guard.
func TestSaleCaptureLegalityComesFromTheGuardNotTheBoolean(t *testing.T) {
	// Every state whose table declares the capture edge (see sales.go).
	capturable := []string{
		dbstore.SalePending,
		dbstore.SaleAuthorized,
		dbstore.SalePaid,
		dbstore.SaleConfirmed,
	}

	for _, state := range capturable {
		for _, flag := range []bool{false, true} {
			sale := authorizedSale(state)
			sale.Authorized = flag

			if !SalesMachine.Allows(state, SaleEventCapture) {
				t.Fatalf("%s: the table declares no capture edge, so this case would prove nothing", state)
			}
			res, err := SalesMachine.Apply(sale, SaleEventCapture)
			if err != nil {
				t.Fatalf("%s (authorized=%v): capture on an uncaptured authorization failed — the guard read the boolean: %v",
					state, flag, err)
			}
			if want := SalesMachine.States[state].Events[SaleEventCapture].To; res.To != want {
				t.Errorf("%s (authorized=%v): capture target = %q, want %q", state, flag, res.To, want)
			}
		}
	}

	// A CAPTURED authorization: the row a real capture produces carries
	// authorized=true AND captured_at. It must be refused — by the guard, since
	// the edge exists — and the boolean must not rescue it.
	capturedAt := time.Now().UTC()
	captured := authorizedSale(dbstore.SalePaid)
	captured.Authorized = true
	captured.CapturedAt = &capturedAt
	_, err := SalesMachine.Apply(captured, SaleEventCapture)
	if err == nil {
		t.Fatal("capture on an already-captured authorization was allowed")
	}
	if !errors.Is(err, ErrTransitionRefused) {
		t.Errorf("capture on a captured authorization = %v, want ErrTransitionRefused (the edge exists, the guard said no)", err)
	}

	// An ordinary CHIP sale (no held authorization) must be refused for the same
	// reason, again regardless of the boolean.
	for _, flag := range []bool{false, true} {
		plain := &dbstore.Sale{Status: dbstore.SalePending, ChipPurchaseID: "pur_plain", Authorized: flag}
		if _, err := SalesMachine.Apply(plain, SaleEventCapture); !errors.Is(err, ErrTransitionRefused) {
			t.Errorf("capture on a non-skip-capture sale (authorized=%v) = %v, want ErrTransitionRefused", flag, err)
		}
		if _, err := SalesMachine.Apply(plain, SaleEventRelease); !errors.Is(err, ErrTransitionRefused) {
			t.Errorf("release on a non-skip-capture sale (authorized=%v) = %v, want ErrTransitionRefused", flag, err)
		}
	}
}
