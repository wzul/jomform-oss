package httpapi

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// chipPaymentUnavailableStatus is the status the buyer's /order call answers
// when the CHIP purchase could not be created — 200 ON PURPOSE. This is the fix
// (card t_e2024698), not an oversight:
//
//   - Cloudflare substitutes its OWN error page for an origin 5xx. Measured on
//     prod 2026-10-20: the api answered 502 with `{"error": …}` and the page's
//     fetch resolved with a `<title>jomform.com | 502: Bad gateway</title>` body,
//     while `curl` of the same route got `error code: 502` (content-length 16,
//     server: cloudflare). A 4xx from the SAME route arrives intact (400 +
//     `{"code":"request.badRequest",…}` measured), so the substitution is
//     specific to the 5xx class — and "Custom Errors" (which passes an origin
//     error page through) is an account-level edge setting we cannot rely on.
//   - The request itself was fine: the sale EXISTS and the buyer is being told
//     about a provider-side failure. The body names that state
//     (`status:"payment_error"`), so no caller can mistake it for a checkout.
//
// Nothing is hidden from operators by this: the ERROR line above still logs the
// provider error, and the sale is marked `failed` below so it is counted in the
// merchant's own records instead of sitting there as a phantom `pending`.
const chipPaymentUnavailableStatus = http.StatusOK

// salePaymentUnavailableMessage is the wire prose for a payment that could not
// be STARTED. Like every other message this handler writes it is BM (the api
// does not know the buyer's language); a buyer's page ignores it and paints its
// own localized copy through payFailJS, while a curl/MCP caller gets a sentence
// it can act on.
const salePaymentUnavailableMessage = "pembayaran tidak dapat dimulakan — sila hubungi penjual"

// answerChipPurchaseFailed answers a buyer whose CHIP purchase could not be
// created (rejected merchant key, provider outage, missing credentials).
// Two decisions live here:
//
//  1. The status. See chipPaymentUnavailableStatus: 200 + an explicit
//     `payment_error` state, because a 502 body never reaches the buyer through
//     the edge. The page then renders the failure in-app (payFailJS) with the
//     reference the buyer can quote to the merchant, instead of an opaque edge
//     error page — or, on the block renderer which had no `.catch`, nothing at
//     all.
//
//  2. The sale moves to `failed`. Before, it was left `pending` with
//     chip_purchase_id NULL (measured: JF-1ce524460b4f, 300 sen), so every
//     provider failure silently accumulated a phantom in the merchant's Sales
//     list for money that could never be collected. `failed` is the right state
//     and it already exists end to end — the CHIP callback sets it for a
//     declined payment, the Sales list renders it as its own badge, and the
//     thank-you page reads it for the merchant's failure message — so this needs
//     no new column, no new vocabulary and no new UI. Deleting the row was the
//     alternative, and it is wrong: the row is the buyer's only record of the
//     attempt, and an audit trail that vanishes when something breaks is exactly
//     the trail you need afterwards.
//
// The bookkeeping write must not cost the buyer their answer: a failed
// SetStatus is logged and the response still goes out (a phantom pending row is
// a smaller failure than a buyer staring at a dead form).
func (h *PublicHandlers) answerChipPurchaseFailed(w http.ResponseWriter, r *http.Request, sale *dbstore.Sale) {
	if err := h.Sales.SetStatus(r.Context(), sale.ID, dbstore.SaleFailed); err != nil {
		slog.Error("chip_failure_mark_sale_failed",
			"err", fmt.Sprintf("%+v", err), "sale", sale.RefCode)
	}
	sale.Status = dbstore.SaleFailed
	writeJSON(w, chipPaymentUnavailableStatus, map[string]string{
		"status":   "payment_error",
		"ref_code": sale.RefCode,
		"error":    salePaymentUnavailableMessage,
	})
}

// Card t_e2024698: the ONE buyer-facing "the payment could not be started"
// renderer, shared by the four public order surfaces (the block form, the
// response block form, the legacy product-checkbox form and the custom-HTML
// form). Each of them POSTs to the form's own /order route with fetch() and
// then decides what the buyer sees.
//
// Why it exists. When CHIP purchase creation failed, order.go answered
//
//	502 {"error":"gagal mencipta pembayaran"}
//
// and this origin sits behind Cloudflare, which substitutes ITS OWN body for an
// origin 5xx ("error code: 502", measured on prod 2026-10-20). The api's JSON
// never reached the page: the block form's handler had no .catch, so
// r.json() rejected on the HTML error page and the buyer's click did NOTHING;
// the surfaces that did have a .catch fell back to a bare "Error" alert. In
// both shapes the buyer got no reference, no instruction and no evidence the
// order existed. That is what this renderer fixes — the api now answers a
// status the edge passes through (order.go: chipPaymentUnavailable), and this
// function paints the failure INSIDE the page, next to the form the buyer filled
// in, with the reference they can quote to the merchant.
//
// Safety: every string is inserted with textContent, never innerHTML, so the
// copy (and the reference code the server generated) cannot become markup. The
// dictionary strings are ours, but the same rule keeps this sink safe if a
// caller-supplied value is ever added. The renderer is idempotent — a retry
// reuses the existing box instead of stacking a second one — and it does not
// disable or clear the form, so the buyer can submit again once the merchant's
// payment account is fixed.
//
// The copy is resolved from `ls`, the page's own language (per-form config.lang
// ?? workspace default), the same way every other buyer-facing string on these
// pages resolves. It is embedded as a JSON-encoded literal so a translation
// containing a quote or a backslash cannot terminate the JS string.
func payFailJS(ls langStrings) template.JS {
	msg, err := json.Marshal(ls.PaymentUnavailable)
	if err != nil {
		// Unreachable for a plain string; an empty message is still better than
		// a broken page, and the reference line below survives.
		msg = []byte(`""`)
	}
	refLabel, err := json.Marshal(ls.OrderLabel)
	if err != nil {
		refLabel = []byte(`""`)
	}
	return template.JS(`function jfPayFail(d){
  var box=document.getElementById('jf-payfail');
  if(!box){
    box=document.createElement('div');
    box.id='jf-payfail';
    box.setAttribute('role','alert');
    box.style.cssText='margin:0 0 14px;padding:12px 14px;border:1px solid #fecaca;background:#fef2f2;color:#7f1d1d;border-radius:10px;font-size:14px;line-height:1.5;text-align:left';
    var host=document.querySelector('form')||document.querySelector('.card');
    if(host){host.insertBefore(box,host.firstChild);}
    else{document.body.insertBefore(box,document.body.firstChild);}
  }
  while(box.firstChild){box.removeChild(box.firstChild);}
  var head=document.createElement('div');
  head.style.fontWeight='600';
  head.textContent=` + string(msg) + `;
  box.appendChild(head);
  if(d&&d.ref_code){
    var line=document.createElement('div');
    line.style.marginTop='4px';
    line.textContent=` + string(refLabel) + `+' '+d.ref_code;
    box.appendChild(line);
  }
  try{box.scrollIntoView({block:'center'});}catch(e){}
}`)
}
