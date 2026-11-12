package mcpsrv

// THE FEE TOOLS' REFUSALS ARE ENGLISH (card t_7011b9b8).
//
// get_payment_fees and set_payment_fees are the MCP face of the dashboard's
// Payment fees tab. Their refusals answered in Malay
// ("tetapan caj pemprosesan tidak tersedia", "tick pengakuan pendedahan
// sebelum mengaktifkan caj pemprosesan"), and an MCP client is an agent: it
// reads the error text verbatim and has no locale of its own, so the tool
// surface must speak the one language the rest of the registry prose is in.
//
// WHY IT SURVIVED: the English guards on main scan other packages
// (dbstore/formfields/payments), so no test in this package looked at a runtime
// refusal. The nil-store path is driven here through the real handlers rather
// than by reading the vars, so a handler that stopped returning the sentinel
// fails too.
//
// The ack refusal sits behind a live store + write-scope resolution, so its text
// is pinned on the value the handler returns (the `return nil, nil,
// errPaymentFeeAckRequired` arm). Driving set_payment_fees far enough to reach it
// needs a workspace, an owner and a scope, which is the DB-backed tool harness —
// out of proportion for a wording pin.

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	feeStoreUnavailableEnglish = "processing fee settings not available"
	feeAckRequiredEnglish      = "tick the disclosure acknowledgement before enabling a fee"
)

func TestPaymentFeeToolRefusalsAreEnglish(t *testing.T) {
	ctx := context.Background()
	// deps with no PaymentFees store: the bare-mode deployment the first refusal
	// exists for. Every other dep is nil too, which is fine — the store check is
	// the first thing both handlers do.
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}

	_, _, err := srv.getPaymentFees(ctx, nil, getPaymentFeesIn{})
	if err == nil {
		t.Fatal("get_payment_fees with no fee store returned nil error; a missing store must " +
			"refuse loudly rather than read as \"the fee is off\"")
	}
	if !errors.Is(err, errPaymentFeesUnavailable) {
		t.Fatalf("get_payment_fees with no fee store returned %v, want errPaymentFeesUnavailable", err)
	}
	if got := err.Error(); got != feeStoreUnavailableEnglish {
		t.Fatalf("get_payment_fees store refusal is %q\nwant %q\n"+
			"an MCP client reads this text verbatim — the tool surface is English", got,
			feeStoreUnavailableEnglish)
	}

	_, _, err = srv.setPaymentFees(ctx, nil, setPaymentFeesIn{
		Rates: []feeRateIn{{Method: "fpx", Enabled: true, FixedSen: 100}},
	})
	if err == nil {
		t.Fatal("set_payment_fees with no fee store returned nil error; a missing store must " +
			"refuse loudly rather than drop the write")
	}
	if !errors.Is(err, errPaymentFeesUnavailable) {
		t.Fatalf("set_payment_fees with no fee store returned %v, want errPaymentFeesUnavailable", err)
	}
	if got := err.Error(); got != feeStoreUnavailableEnglish {
		t.Fatalf("set_payment_fees store refusal is %q\nwant %q",
			got, feeStoreUnavailableEnglish)
	}

	// The acknowledgement refusal, the one set_payment_fees returns when a method
	// is enabled without `acknowledged`.
	if got := errPaymentFeeAckRequired.Error(); got != feeAckRequiredEnglish {
		t.Fatalf("set_payment_fees acknowledgement refusal is %q\nwant %q\n"+
			"the REST PUT carries the same sentence; a Malay one here reaches an agent that "+
			"has no locale of its own", got, feeAckRequiredEnglish)
	}
}
