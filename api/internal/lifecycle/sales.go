package lifecycle

import (
	"errors"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// The sales machine.
//
// The states are the DB's own vocabulary, taken verbatim from
// internal/db/migrations/000002_skip_capture.up.sql:
//
//	CHECK (status IN ('pending', 'authorized', 'paid', 'confirmed',
//	                  'refunded', 'cancelled', 'failed'))
//
// A test pins the two lists equal, so a state cannot be added to one and
// silently missed in the other.
//
// The events are what the write paths actually do today — verified in code, not
// designed from above:
//
//	pay      payments.ProcessCallbackTx, a purchase.paid callback   → paid
//	capture  the merchant captures an authorization (REST + MCP)    → paid
//	fail     payment_failure callback / a purchase that never started → failed
//	confirm  the merchant confirms the sale (REST + MCP)            → confirmed
//	release  the merchant voids an uncaptured authorization         → cancelled
//	refund   the money went back (refund endpoint + refunded callback) → refunded
//
// Two edges exist because they are real today even though they are not the
// happy path: `pending → confirmed` is how a bank-transfer sale is confirmed
// (docs/CHIP-SANDBOX-E2E.md scenario 4), and `paid → confirmed` is the merchant
// confirming a CHIP payment (scenario 2: `pending → paid → confirmed`). `capture`
// is also defined from `paid` and `confirmed` because an authorization can read
// paid (CHIP reports the authorised purchase as paid) while still uncaptured —
// ListAwaitingCapture lists `paid` for exactly that — and the old bulk capture
// had no status check at all; the guard, not the edge, is what keeps it honest.

// `refunded`, `cancelled` and `failed` are terminal except for the repeat of the
// event that produced them: a redelivered gateway callback is a legal no-op
// (Changed=false), so the caller can tell "already done" from "never allowed"
// without comparing status strings.
var SalesMachine = &Machine{
	Name: "sales",
	States: map[string]State{
		dbstore.SalePending: {Events: map[string]Transition{
			SaleEventPay:     {To: dbstore.SalePaid},
			SaleEventCapture: {To: dbstore.SalePaid, Guard: awaitingCapture, LabelKey: "bld.capture"},
			SaleEventFail:    {To: dbstore.SaleFailed},
			SaleEventConfirm: {To: dbstore.SaleConfirmed, LabelKey: "sales.confirmSale"},
			SaleEventRelease: {To: dbstore.SaleCancelled, Guard: awaitingCapture, LabelKey: "bld.release"},
		}},

		// 'authorized' is DB vocabulary that no current write path produces
		// (skip_capture sales sit in 'pending' until captured), but legacy rows
		// can carry it and ListAwaitingCapture treats them as capturable, so
		// the machine accepts the two moves that queue offers. No `pay`/`fail`:
		// a gateway callback for such a sale is ignored today, and the table is
		// supposed to describe today.
		dbstore.SaleAuthorized: {Events: map[string]Transition{
			SaleEventCapture: {To: dbstore.SalePaid, Guard: awaitingCapture, LabelKey: "bld.capture"},
			SaleEventConfirm: {To: dbstore.SaleConfirmed, LabelKey: "sales.confirmSale"},
			SaleEventRelease: {To: dbstore.SaleCancelled, Guard: awaitingCapture, LabelKey: "bld.release"},
		}},

		dbstore.SalePaid: {Events: map[string]Transition{
			// A duplicate purchase.paid callback: the sale is already where the
			// event would put it, so it is a legal no-op rather than a
			// conflicting transition that would re-fire webhooks/receipts.
			SaleEventPay: {To: dbstore.SalePaid},
			// Capturing an authorization that already READS paid (CHIP reported
			// the authorised purchase as paid; ListAwaitingCapture includes
			// `paid` for exactly this) is a real action that leaves the status
			// where it is: the capture charges the card and stamps captured_at.
			SaleEventCapture: {To: dbstore.SalePaid, Guard: awaitingCapture, LabelKey: "bld.capture"},
			SaleEventConfirm: {To: dbstore.SaleConfirmed, LabelKey: "sales.confirmSale"},
			SaleEventRefund:  {To: dbstore.SaleRefunded, LabelKey: "sales.refund"},
			SaleEventRelease: {To: dbstore.SaleCancelled, Guard: awaitingCapture, LabelKey: "bld.release"},
		}},

		dbstore.SaleConfirmed: {Events: map[string]Transition{
			// Re-confirming a confirmed sale succeeds (the endpoint always has);
			// it just is not a change, so it fires no side effect.
			SaleEventConfirm: {To: dbstore.SaleConfirmed},
			// A merchant may confirm before capturing an authorization; the
			// capture still has to charge the card, and MarkCaptured writes the
			// capture target (`paid`) — the same move the old unguarded bulk
			// capture allowed.
			SaleEventCapture: {To: dbstore.SalePaid, Guard: awaitingCapture, LabelKey: "bld.capture"},
			SaleEventRefund:  {To: dbstore.SaleRefunded, LabelKey: "sales.refund"},
			SaleEventRelease: {To: dbstore.SaleCancelled, Guard: awaitingCapture, LabelKey: "bld.release"},
		}},

		dbstore.SaleFailed: {Events: map[string]Transition{
			// A redelivered payment_failure: no-op, never a second webhook.
			SaleEventFail: {To: dbstore.SaleFailed},
		}},
		dbstore.SaleCancelled: {Events: map[string]Transition{}},
		dbstore.SaleRefunded: {Events: map[string]Transition{
			// A redelivered purchase.refunded: no-op. A merchant refund never
			// reaches here (RefundSale refuses a !Changed result).
			SaleEventRefund: {To: dbstore.SaleRefunded},
		}},
	},
}

// The sale events. Named as the money layer talks about them, so a call site
// reads as the thing that happened rather than as the state it wrote.
const (
	// SaleEventPay: the gateway reported the purchase paid.
	SaleEventPay = "pay"
	// SaleEventCapture: the merchant captured an authorization (the hold
	// becomes a charge).
	SaleEventCapture = "capture"
	// SaleEventFail: the gateway reported the payment failed, or the purchase
	// could not be created at all.
	SaleEventFail = "fail"
	// SaleEventConfirm: the merchant confirmed the sale (a CHIP payment or a
	// bank transfer).
	SaleEventConfirm = "confirm"
	// SaleEventRelease: the merchant released an authorization, so the card is
	// never charged.
	SaleEventRelease = "release"
	// SaleEventRefund: the money went back to the buyer.
	SaleEventRefund = "refund"
)

// awaitingCapture is the precondition for the two money moves that only mean
// something on an UNCAPTURED authorization: capture turns the hold into a
// charge, release voids the hold. Anything else — an ordinary CHIP sale, or an
// authorization that was already captured — must never reach the gateway.
//
// The sentence is the one the bulk-capture results already carried
// ("not an authorization awaiting capture"), so a refusal reads the same
// whether it comes from the table or from a per-row result.
func awaitingCapture(e Entity) error {
	sale, ok := e.(*dbstore.Sale)
	if !ok || sale == nil {
		return errors.New("the sale could not be inspected")
	}
	if !sale.SkipCapture || sale.ChipPurchaseID == "" || sale.CapturedAt != nil {
		return errors.New("not an authorization awaiting capture")
	}
	return nil
}
