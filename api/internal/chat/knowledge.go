// The product guide the in-app assistant is given about JomForm itself.
//
// WHAT THIS IS FOR, AND WHY IT IS A TABLE AND NOT PROSE IN THE PROMPT.
//
// Before this file the system prompt (prompt.go) told the model it was the
// assistant inside "a Malaysian form-builder and payment product" and then only
// described the merchant's OWN data: their forms, sales, products. Every fact
// about how the PRODUCT works — that a processing fee can be passed to the
// buyer, that shipping can be quoted by a courier, that a donation form is a
// pay-what-you-want product — was left to the model's own priors, so the answer
// depended on whether the model happened to guess JomForm's rules right. Wan's
// words: "patutnya agen umum yang tahu dan pandai pasal produk JomForm" (it
// should be a general agent that knows and understands the JomForm product).
//
// The knowledge is CURATED, not harvested: injecting the docs tree (4,602 lines
// of TSX) or the repository would cost more per message than the answer is
// worth, and would drown the operating instructions. This file is the small,
// hand-written subset a merchant actually asks about, and it is bounded by
// knowledge_size_test.go so the per-message cost cannot grow by accident.
//
// EVERY FACT CARRIES ITS PROVENANCE. Source names a file in THIS repository and
// Anchors name phrases that must still appear in it. knowledge_provenance_test.go
// resolves every one of them, so:
//
//   - a fact cannot be invented here (an entry with no source text behind it
//     fails the build);
//   - a fact cannot silently outlive the documentation it came from (a docs edit
//     that removes the anchor fails the build and names the entry, so the prose
//     is re-checked instead of drifting);
//   - the guide cannot claim a capability the code does not have, because the
//     capability-absence entries are checked against the code itself in
//     knowledge_absent_capability_test.go.
//
// WHAT BELONGS HERE, and what does not:
//
//   - HOW the product behaves (a rule, an amount, an order of resolution, where
//     a setting lives) — yes.
//   - The merchant's own data (a form name, a price, a customer) — never. That
//     is what the read tools are for, and the guide says so.
//   - Anything the tree cannot support — never. If a fact is worth telling a
//     merchant, the documentation in web/src/app/docs (or the code, for a
//     negative) has to say it first.
//
// TEXT IS ENGLISH, LIKE EVERY OTHER PROMPT IN THIS REPO. The reply language is
// decided by the rule in prompt.go (the user's own language), not by this file;
// this is the material, not the answer. Keep it free of dashboard-internal
// identifiers: the model is told not to read column names or tool names out to
// the merchant, and a fact written in those words invites exactly that.
package chat

import "strings"

// factSource is one file a fact rests on, plus the phrase that must still be
// readable there. Anchors are matched case-insensitively after the file's lines
// are joined, so a sentence wrapped across two `//` lines in a Go comment still
// matches (that is how the coupon fact's source is written).
type factSource struct {
	File   string
	Anchor string
}

// productFact is one thing the assistant is told about how JomForm works.
type productFact struct {
	// ID is stable and named by the question table in knowledge_questions_test.go,
	// which is how a question's coverage is asserted against a fact instead of
	// against a bare substring.
	ID string
	// Topic groups facts in the rendered guide. Topics must be contiguous in
	// productFacts (asserted by the test), so the guide reads as sections rather
	// than as a shuffled list.
	Topic string
	// Text is what the model reads. One or two sentences; keep it concrete.
	Text string
	// Sources are the files the fact is verified against, every Anchor included.
	Sources []factSource
}

// productFacts is the guide, in reading order.
//
// The order is deliberate: payments first (the question Wan measured), then what
// a merchant sells, then delivery, then the records, then the surfaces around
// them, and finally the two things the product does NOT do — which the model
// needs last and explicitly, because a negative is what it is most likely to
// paper over with a plausible guess.
var productFacts = []productFact{
	{
		ID:    "payments.fee-setup",
		Topic: "Payments and fees",
		Text: "JomForm can pass the CHIP processing fee to the buyer. It is off until the merchant sets it up in " +
			"Settings → Payment fees, where FPX B2C, FPX B2B1, Card, e-Wallet and DuitNow QR each has its own rate: " +
			"a fixed sen amount, basis points (100 = 1%), or both. FPX's two products are separate methods with " +
			"different acquirer fees. The merchant types their acquirer's rate; JomForm does not fill it in. " +
			"Payments run on the merchant's CHIP credentials.",
		Sources: []factSource{
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "FPX B2C, FPX B2B1, Card, e-Wallet and DuitNow QR"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "100 basis points is 1%"},
			{File: "web/src/app/docs/page.tsx", Anchor: "CHIP payment credentials"},
		},
	},
	{
		ID:    "payments.fee-acknowledgement",
		Topic: "Payments and fees",
		Text: "Passing a card surcharge on to the buyer is restricted by card-network and acquirer rules, so the " +
			"merchant must tick an acknowledgement sentence before any fee reaches a buyer, and the acceptance is " +
			"recorded with who accepted it and when. One form can opt out of a workspace-wide rate with its own fee " +
			"override.",
		Sources: []factSource{
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "I understand this processing fee is passed on to the buyer"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "no fee reaches a buyer"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "A single form can opt out"},
		},
	},
	{
		ID:    "payments.fee-grossup",
		Topic: "Payments and fees",
		Text: "The buyer's total is grossed up, not price plus rate: JomForm charges the smallest total that still " +
			"leaves the merchant the asking price after the fee is taken (RM1,000 at 1% is charged as RM1,010.10, " +
			"a fee of RM10.10). The buyer sees the method, the fee and the final total before paying, and the " +
			"payment is restricted to the method they chose.",
		Sources: []factSource{
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "smallest total where, after the fee is taken"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "before Proceed to Payment"},
		},
	},
	{
		ID:    "payments.fee-refunds",
		Topic: "Payments and fees",
		Text: "A refund never recalculates the processing fee. A full refund returns what the buyer actually paid, " +
			"fee included; a partial refund returns the amount the merchant chooses; and the sale keeps the fee the " +
			"merchant bore at the rate in force when the buyer paid.",
		Sources: []factSource{
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "a refund is never recalculated"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "does not return the processing fee"},
		},
	},
	{
		ID:    "payments.no-tax-line",
		Topic: "Payments and fees",
		Text: "JomForm does not calculate SST or any other tax on a sale: the total is items subtotal + shipping − " +
			"discount + processing fee, with no tax line, and SST is not calculated on the processing fee in the " +
			"current release. An e-Invoice (LHDN) line-item document is supported.",
		Sources: []factSource{
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "items subtotal + shipping − discount + processing fee"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "SST is not calculated for this fee in the current release"},
			{File: "web/src/app/docs/payment-fees/page.tsx", Anchor: "an LHDN line-item document can carry it"},
		},
	},

	{
		ID:    "products.types",
		Topic: "Products, donations and subscriptions",
		Text: "A product has a type: one_time (a fixed price), subscription (recurring) or custom_amount (the buyer " +
			"types the amount). Money is integer sen, so RM25.00 is 2500.",
		Sources: []factSource{
			{File: "api/internal/dbstore/product.go", Anchor: "one_time | subscription | custom_amount"},
			{File: "api/internal/mcpsrv/handlers.go", Anchor: "price in SEN (RM25.00 = 2500)"},
		},
	},
	{
		ID:    "products.custom-amount",
		Topic: "Products, donations and subscriptions",
		Text: "A custom_amount product is pay-what-you-want: it needs a minimum of at least RM1.00, may set a " +
			"maximum (no maximum means no upper limit) and an optional suggested amount pre-filled for the buyer, " +
			"and it cannot have variations.",
		Sources: []factSource{
			{File: "api/internal/db/migrations/000085_custom_amount_products.up.sql", Anchor: "Pay-what-you-want products"},
			{File: "api/internal/payments/custom_amount.go", Anchor: "min_price_sen must be at least"},
			{File: "api/internal/mcpsrv/parity.go", Anchor: "a custom_amount product cannot have variations"},
		},
	},
	{
		ID:    "products.donation",
		Topic: "Products, donations and subscriptions",
		Text: "A donation form is built from the donation template with a pay-what-you-want product, so the donor " +
			"chooses the amount. Fixed-price products are the alternative: one product per amount (RM10, RM50, " +
			"RM100). A paid donation is recorded as an ordinary sale, so it appears in Sales like any other payment.",
		Sources: []factSource{
			{File: "api/internal/db/migrations/000027_template_library.up.sql", Anchor: "tpl-donation"},
			{File: "web/src/app/landing/page.tsx", Anchor: "a paid donation is a sale row"},
			{File: "web/src/app/landing/page.tsx", Anchor: "Fixed-denomination products"},
		},
	},
	{
		ID:    "products.variations",
		Topic: "Products, donations and subscriptions",
		Text: "A product can have variations, each with its own name, price, stock and image; the buyer picks one " +
			"on the form.",
		Sources: []factSource{
			{File: "web/src/app/docs/variations/page.tsx", Anchor: "its own name, price, stock and image"},
		},
	},
	{
		ID:    "subscriptions.basics",
		Topic: "Products, donations and subscriptions",
		Text: "A subscription product bills monthly or yearly. By default it bills indefinitely; a product can set " +
			"a fixed number of charges instead, and the checkout payment counts as charge 1. Recurring billing is " +
			"card-based, so the card is saved at checkout.",
		Sources: []factSource{
			{File: "web/src/app/docs/subscriptions/page.tsx", Anchor: "The first payment is charge 1"},
			{File: "web/src/app/docs/subscriptions/page.tsx", Anchor: "The card is saved at checkout"},
		},
	},
	{
		ID:    "subscriptions.failed-charge",
		Topic: "Products, donations and subscriptions",
		Text: "A declined recurring charge is retried automatically three times, one a day. If all three fail the " +
			"subscription becomes past due and the customer is asked to update their card; with no update it is " +
			"cancelled after a seven-day grace period. Cancelling never revokes the customer's saved card token, so " +
			"a subscription can be reactivated without the card being entered again.",
		Sources: []factSource{
			{File: "web/src/app/docs/subscriptions/page.tsx", Anchor: "three attempts, one a day"},
			{File: "web/src/app/docs/subscriptions/page.tsx", Anchor: "seven-day grace period"},
			{File: "web/src/app/docs/subscriptions/page.tsx", Anchor: "never revokes the customer's saved card token"},
		},
	},

	{
		ID:    "shipping.zones",
		Topic: "Shipping and delivery",
		Text: "A form's shipping block can charge delivery from managed zones: one named set of delivery zones " +
			"shared by forms, each zone matching states, districts or a postcode range and carrying its own rate " +
			"rows, resolved from the buyer's address at checkout. An address that matches no zone is either refused " +
			"or priced by the zone marked as fallback — a zone fee is never silently zero.",
		Sources: []factSource{
			{File: "web/src/app/docs/shipping/page.tsx", Anchor: "The zone comes from the submitted address"},
			{File: "web/src/app/docs/shipping/page.tsx", Anchor: "never silently zero"},
		},
	},
	{
		ID:    "shipping.couriers",
		Topic: "Shipping and delivery",
		Text: "A shipping block can use a courier quote instead of a fixed zone rate (its rate source is Courier " +
			"quote): Lalamove for intra-city routes and EasyParcel nationwide. The fee is quoted at checkout; if the " +
			"courier is not connected, cannot cover the address or fails, the zone rate on the block is charged " +
			"instead and the sale records which of the two applied.",
		Sources: []factSource{
			{File: "web/src/app/docs/shipping/page.tsx", Anchor: "Courier quote"},
			{File: "web/src/app/docs/shipping/page.tsx", Anchor: "EasyParcel"},
			{File: "web/src/app/docs/shipping/page.tsx", Anchor: "never a silent zero"},
		},
	},

	{
		ID:    "sales.refunds",
		Topic: "Sales, responses and notifications",
		Text: "A paid or confirmed sale can be refunded in full or in part. A partial refund keeps the sale paid or " +
			"confirmed, marks it Partially refunded, and leaves the rest refundable until the balance reaches zero. " +
			"An amount that would push the refunds past the sale total is refused before the gateway is called.",
		Sources: []factSource{
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "Partially refunded"},
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "outstanding_sen"},
		},
	},
	{
		ID:    "forms.modes",
		Topic: "Sales, responses and notifications",
		Text: "A form's mode decides what its entries are: a payment form records sales, while a response-only form " +
			"records responses and takes no payment. Both appear on the Sales page; choosing a payment status hides " +
			"the responses, because a status is a payment question.",
		Sources: []factSource{
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "response-only form"},
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "the status filter does not appear"},
		},
	},
	{
		ID:    "sales.realtime",
		Topic: "Sales, responses and notifications",
		Text: "The Sales page subscribes to the workspace's realtime stream, so a new sale or response appears " +
			"without a refresh (the Live pill shows whether the stream is connected). A per-workspace sound switch " +
			"beeps on a new sale or response; it applies to the dashboard only, because buyers are never played a " +
			"sound.",
		Sources: []factSource{
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "Live pill"},
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "only the dashboard plays a sound"},
		},
	},

	{
		ID:    "storefront.domains",
		Topic: "Domains, templates and embedding",
		Text: "A workspace is served at its own address, {slug}.jomform.com. A form can also go on the merchant's " +
			"own domain: add the domain in Settings → Domains, point a CNAME to custom.jomform.com and verify it; " +
			"with Cloudflare linked, JomForm can create the CNAME for the merchant.",
		Sources: []factSource{
			{File: "web/src/app/docs/workspaces/page.tsx", Anchor: "{slug}.jomform.com"},
			{File: "web/src/app/docs/page.tsx", Anchor: "point a CNAME to custom.jomform.com"},
		},
	},
	{
		ID:    "storefront.templates",
		Topic: "Domains, templates and embedding",
		Text: "A form can start from a template instead of the drag-and-drop builder; the templates cover order " +
			"forms, booking, donation, contact, survey and more.",
		Sources: []factSource{
			{File: "web/src/app/docs/customize/page.tsx", Anchor: "order forms, booking, donation, contact, survey and more"},
		},
	},
	{
		ID:    "storefront.embed",
		Topic: "Domains, templates and embedding",
		Text: "A form can be embedded in another website with an iframe; the embedded form posts its height to the " +
			"page so it grows with the form instead of scrolling inside a fixed box.",
		Sources: []factSource{
			{File: "web/src/app/docs/embed/page.tsx", Anchor: "iframe"},
			{File: "web/src/app/docs/embed/page.tsx", Anchor: "jomform:height"},
		},
	},

	{
		ID:    "email.notifications",
		Topic: "Email, spam and security",
		Text: "Receipt and notification emails are configured per form and fall back to the workspace, then to the " +
			"built-in default. Placeholders such as {{ref_code}} and {{amount}} are replaced with the real values " +
			"when the email is sent.",
		Sources: []factSource{
			{File: "web/src/app/docs/email/page.tsx", Anchor: "resolution order is form → workspace → built-in default"},
			{File: "web/src/app/docs/email/page.tsx", Anchor: "{{ref_code}}"},
		},
	},
	{
		ID:    "spam.review",
		Topic: "Email, spam and security",
		Text: "Every submission is reviewed for spam by an AI judge, per workspace: a flagged entry waits in a " +
			"review queue, a false positive can be un-flagged, and an unreviewed flag is deleted after 30 days.",
		Sources: []factSource{
			{File: "web/src/app/docs/spam-detection/page.tsx", Anchor: "un-flag"},
			{File: "web/src/app/docs/spam-detection/page.tsx", Anchor: "30 days"},
			{File: "web/src/app/docs/spam-detection/page.tsx", Anchor: "per workspace"},
		},
	},
	{
		ID:    "security.2fa",
		Topic: "Email, spam and security",
		Text: "Two-factor authentication is set up in Profile → Security: scan a QR into an authenticator app and " +
			"confirm a 6-digit code. A trusted device skips the code for 30 days and can be revoked there.",
		Sources: []factSource{
			{File: "web/src/app/docs/security/page.tsx", Anchor: "6-digit"},
			{File: "web/src/app/docs/security/page.tsx", Anchor: "trusted device"},
		},
	},

	{
		ID:    "agents.mcp",
		Topic: "Agents and the API",
		Text: "JomForm exposes an MCP server, so an AI agent such as Claude or Cursor can build forms and manage " +
			"products, sales and more. The client points at https://mcp.jomform.com/mcp and registers itself through " +
			"OAuth 2.1, so no API key is needed.",
		Sources: []factSource{
			{File: "web/src/app/docs/page.tsx", Anchor: "mcp.jomform.com/mcp"},
			{File: "web/src/app/docs/page.tsx", Anchor: "OAuth 2.1"},
		},
	},
	{
		ID:    "agents.rest-api",
		Topic: "Agents and the API",
		Text: "A REST API drives JomForm from the merchant's own server: create forms, read sales and responses, " +
			"manage products, subscriptions and webhooks. Keys are created in Settings → API keys, and a key can be " +
			"readonly.",
		Sources: []factSource{
			{File: "web/src/app/docs/api/page.tsx", Anchor: "OpenAPI 3.1"},
			{File: "web/src/app/docs/api/page.tsx", Anchor: "Settings → API keys"},
			{File: "web/src/app/docs/api/page.tsx", Anchor: "readonly"},
		},
	},

	{
		ID:    "absent.coupons",
		Topic: "What JomForm does not have",
		Text: "There is no coupon or discount-code feature. The coupon block was withdrawn from the builder and " +
			"nothing applies a discount code at checkout, so there is no code field to offer a buyer. If a merchant " +
			"asks for discount codes, say that the product does not have them and offer to file it as a feature " +
			"request — do not describe a coupon block, a voucher setting or a checkout code box that does not exist.",
		Sources: []factSource{
			{File: "api/internal/formgen/coupon_withdrawn_test.go", Anchor: "no coupon table and no server-side validation"},
			{File: "api/internal/formgen/coupon_withdrawn_test.go", Anchor: "ValidBlockTypes"},
		},
	},
}

// productGuideOpen and productGuideClose delimit the guide inside the system
// prompt. They are fixed markers with no user-supplied part: the guide is
// written by JomForm, and the model is told (in prompt.go) that it is reference
// material rather than something a tool or a user said.
const (
	productGuideOpen  = "===BEGIN-JOMFORM-PRODUCT-GUIDE==="
	productGuideClose = "===END-JOMFORM-PRODUCT-GUIDE==="
)

// productGuideText renders productFacts as the block the prompt carries.
//
// The shape is deliberately flat: a topic line, then one "- " bullet per fact.
// No markdown headings and no nesting — the guide is read by a model, not
// rendered, and every extra token here is paid on every round-trip of every
// message (up to MaxSteps+1 of them).
func productGuideText() string {
	var sb strings.Builder
	sb.WriteString(productGuideOpen + "\n")
	topic := ""
	for _, f := range productFacts {
		if f.Topic != topic {
			if topic != "" {
				sb.WriteString("\n")
			}
			sb.WriteString(f.Topic + "\n")
			topic = f.Topic
		}
		sb.WriteString("- " + f.Text + "\n")
	}
	sb.WriteString(productGuideClose)
	return sb.String()
}
