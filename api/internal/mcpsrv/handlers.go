// Tool handlers — typed input/output via sdk inference.
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// ─── Products ────────────────────────────────────────────────────────────────

type productOut struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	PriceSen            int64    `json:"price_sen"`
	Images              []string `json:"images,omitempty"`
	Type                string   `json:"type"`
	BillingPeriod       string   `json:"billing_period,omitempty"`
	TrialDays           int      `json:"trial_days"`
	SubscriptionCharges *int     `json:"subscription_charges,omitempty"`                     // nil = infinite
	ScheduleMode        string   `json:"schedule_mode" jsonschema:"next-charge-date policy"` // round 13c
	Active              bool     `json:"active"`
	Stock               *int     `json:"stock,omitempty"`
	// Card t_06851d88: pay-what-you-want limits (type custom_amount), integer
	// sen. max_price_sen omitted = no cap; suggested_price_sen omitted = no
	// pre-filled amount. price_sen on such a product is only the display
	// fallback — the buyer's validated amount is what is charged.
	MinPriceSen       int64  `json:"min_price_sen,omitempty"`
	MaxPriceSen       *int64 `json:"max_price_sen,omitempty"`
	SuggestedPriceSen *int64 `json:"suggested_price_sen,omitempty"`
	// Issue #153: variations for variable products (empty for simple products).
	Variations []variationOut `json:"variations,omitempty"`
}

type emptyIn struct{}

func (s *Server) listProducts(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[productOut], error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	// Issue #307: a user-level agent lists active products across all of the
	// user's accessible workspaces (aggregated; ownership enforced per store).
	products, err := s.deps.Products.ListActiveByAccounts(ctx, accs)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]productOut, 0, len(products))
	for _, p := range products {
		o := productOut{
			ID: p.ID, Name: p.Name, Description: p.Description, PriceSen: p.PriceSen,
			Images: p.Images, Type: p.Type, BillingPeriod: p.BillingPeriod,
			TrialDays: p.TrialDays, Active: p.Active, Stock: p.Stock,
			SubscriptionCharges: p.SubscriptionCharges,
			ScheduleMode:        p.ScheduleMode,
			MinPriceSen:         p.MinPriceSen,
			MaxPriceSen:         p.MaxPriceSen,
			SuggestedPriceSen:   p.SuggestedPriceSen,
		}
		// Issue #153: include variations for variable products.
		if s.deps.ProductVariations != nil {
			if vs, verr := s.deps.ProductVariations.ListByProduct(ctx, p.ID); verr == nil {
				o.Variations = toVariationOuts(vs)
			}
		}
		out = append(out, o)
	}
	return nil, newListOut(out), nil
}

type createProductIn struct {
	Name                string        `json:"name" jsonschema:"product name"`
	Description         string        `json:"description,omitempty" jsonschema:"product description"`
	PriceSen            *int64        `json:"price_sen" jsonschema:"price in SEN (RM25.00 = 2500). Omit for type custom_amount, whose price is derived from min_price_sen/suggested_price_sen and the buyer's own amount."`
	Type                string        `json:"type" jsonschema:"one_time, subscription, or custom_amount (pay-what-you-want)"`
	BillingPeriod       string        `json:"billing_period,omitempty" jsonschema:"month or year (subscription only)"`
	TrialDays           int           `json:"trial_days,omitempty" jsonschema:"trial days (subscription)"`
	Stock               *int          `json:"stock,omitempty" jsonschema:"stock (omit = unlimited)"`
	Images              []string      `json:"images,omitempty" jsonschema:"image (URL must be https:// — issue #501)"`
	Variations          []variationIn `json:"variations,omitempty" jsonschema:"product variations (name, price_sen, stock, images, sort_order) — empty = simple product"`
	SubscriptionCharges *int          `json:"subscription_charges,omitempty" jsonschema:"number of charges (subscription; omit/null = infinite, N = charge N times)"`
	ScheduleMode        *string       `json:"schedule_mode,omitempty" jsonschema:"from_payment (default) | catch_up | skip_missed"`
	// Card t_06851d88: limits for a pay-what-you-want (custom_amount) product,
	// integer sen. The buyer types the amount at checkout and the server
	// validates it against these; nothing on this product is charged as-is.
	MinPriceSen       *int64 `json:"min_price_sen,omitempty" jsonschema:"custom_amount: smallest accepted amount in SEN (>= 100, RM1.00). Required for type custom_amount, ignored otherwise."`
	MaxPriceSen       *int64 `json:"max_price_sen,omitempty" jsonschema:"custom_amount: largest accepted amount in SEN. Omit or null = no upper limit."`
	SuggestedPriceSen *int64 `json:"suggested_price_sen,omitempty" jsonschema:"custom_amount: amount pre-filled in the buyer's field, in SEN. Omit = start empty. Must be within min/max."`
}

// variationIn is the MCP input shape for one product variation.
//
// Issue #623: the subscription cycle fields are POINTERS so an omitted field is
// distinguishable from a set one. Omitted/null = "not set → inherit the
// product's", which is a different state from "explicitly set to the product's
// value" and must stay so (pre-filling would silently make the product-level
// cycle inert for that variation).
type variationIn struct {
	Name      string   `json:"name" jsonschema:"variation name (e.g.: Size M, Red)"`
	PriceSen  *int64   `json:"price_sen" jsonschema:"variation price in SEN"`
	Stock     *int     `json:"stock,omitempty" jsonschema:"variation stock (omit = unlimited)"`
	Images    []string `json:"images,omitempty" jsonschema:"variation image (URL must be https:// — issue #501)"`
	SortOrder int      `json:"sort_order,omitempty" jsonschema:"sort order (0 = follow the list)"`
	// Issue #623: the variation's own subscription cycle. Omit = inherit the
	// product's for that field.
	BillingPeriod       *string `json:"billing_period,omitempty" jsonschema:"billing cycle for this variation: month | year. Omit = follow product."`
	TrialDays           *int    `json:"trial_days,omitempty" jsonschema:"trial days for the variation. Omit = follow product."`
	SubscriptionCharges *int    `json:"subscription_charges,omitempty" jsonschema:"number of charges for the variation (N = charge N times). Omit = follow product."`
}

// validateVariationIn validates one MCP variation (money stays integer sen;
// stock nil = unlimited; cycle fields optional and inherit the product when
// omitted, but validated when present).
func validateVariationIn(v variationIn) error {
	if v.Name == "" {
		return fmt.Errorf("variation name is required")
	}
	// Issue #551: *int64 so missing/null is a rejection, not a silent 0.
	if v.PriceSen == nil || *v.PriceSen < 0 {
		return fmt.Errorf("variation price_sen must be >= 0")
	}
	if v.Stock != nil && *v.Stock < 0 {
		return fmt.Errorf("variation stock must be >= 0")
	}
	if !urlguard.ValidList(v.Images) {
		return fmt.Errorf("%s", urlguard.InvalidMessage("variations[].images"))
	}
	// Issue #623: an unknown period is rejected, not silently dropped — an
	// ignored cycle is how a merchant ends up billing yearly where they chose
	// monthly.
	if v.BillingPeriod != nil && *v.BillingPeriod != "" &&
		*v.BillingPeriod != "month" && *v.BillingPeriod != "year" {
		return fmt.Errorf("variation billing_period must be month|year")
	}
	if v.TrialDays != nil && *v.TrialDays < 0 {
		return fmt.Errorf("variation trial_days must be >= 0")
	}
	if v.SubscriptionCharges != nil && *v.SubscriptionCharges < 1 {
		return fmt.Errorf("variation subscription_charges must be >= 1")
	}
	return nil
}

// toStoreVariations maps MCP variation inputs to store rows (fill a zero
// sort_order from the list position). An omitted cycle field stays nil in the
// store, which is the "inherit the product" state.
func toStoreVariations(vs []variationIn) []*dbstore.ProductVariation {
	out := make([]*dbstore.ProductVariation, 0, len(vs))
	for i, v := range vs {
		order := v.SortOrder
		if order == 0 {
			order = i
		}
		bp := ""
		if v.BillingPeriod != nil {
			bp = *v.BillingPeriod
		}
		out = append(out, &dbstore.ProductVariation{
			Name: v.Name, PriceSen: *v.PriceSen, Stock: v.Stock,
			Images: v.Images, SortOrder: order,
			BillingPeriod: bp, TrialDays: v.TrialDays, Charges: v.SubscriptionCharges,
		})
	}
	return out
}

func (s *Server) createProduct(ctx context.Context, _ *mcp.CallToolRequest, in createProductIn) (*mcp.CallToolResult, productOut, error) {
	accountID, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, productOut{}, err
	}
	// Issue #551: price_sen is a POINTER so missing or explicit `null` is rejected
	// rather than silently decoding to 0 (which created a RM 0.00 product).
	//
	// Card t_06851d88: a custom_amount product has no asking price — it is
	// derived from the limits below — so the requirement does not apply to it.
	if in.Name == "" {
		return nil, productOut{}, fmt.Errorf("name and price_sen are required")
	}
	if in.PriceSen != nil && *in.PriceSen < 0 {
		return nil, productOut{}, fmt.Errorf("price_sen must be >= 0")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges < 1 {
		return nil, productOut{}, fmt.Errorf("subscription_charges must be >= 1 (or null for infinite)")
	}
	// round 13c: schedule mode for the next-charge date.
	schedMode := payments.ScheduleFromPayment
	if in.ScheduleMode != nil && *in.ScheduleMode != "" {
		if !payments.ValidScheduleMode(*in.ScheduleMode) {
			return nil, productOut{}, fmt.Errorf("invalid schedule_mode: %q (from_payment|catch_up|skip_missed)", *in.ScheduleMode)
		}
		schedMode = *in.ScheduleMode
	}
	// Issue #501: product images (and variation images) render into
	// buyer-facing `<img src>` attributes (form renderer + restaurant menu),
	// so an unsafe scheme is rejected here with the ONE shared rule.
	if !urlguard.ValidList(in.Images) {
		return nil, productOut{}, fmt.Errorf("%s", urlguard.InvalidMessage("images"))
	}
	pType := in.Type
	if pType == "" {
		pType = dbstore.SaleTypeOneTime
	}
	if pType != dbstore.SaleTypeOneTime && pType != dbstore.SaleTypeSubscription &&
		pType != dbstore.SaleTypeCustomAmount {
		return nil, productOut{}, fmt.Errorf("type must be one_time|subscription|custom_amount")
	}
	if in.PriceSen == nil && pType != dbstore.SaleTypeCustomAmount {
		return nil, productOut{}, fmt.Errorf("name and price_sen are required")
	}
	period := ""
	if pType == dbstore.SaleTypeSubscription {
		period = in.BillingPeriod
		if period != "month" && period != "year" {
			return nil, productOut{}, fmt.Errorf("subscription requires billing_period month|year")
		}
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	// Issue #623: also validates each variation's own subscription cycle.
	for _, v := range in.Variations {
		if err := validateVariationIn(v); err != nil {
			return nil, productOut{}, err
		}
	}
	// Card t_06851d88: a custom_amount product's price is derived, and its
	// limits are the pay-what-you-want contract — a variation would carry a
	// fixed price that contradicts it.
	priceSen := int64(0)
	if in.PriceSen != nil {
		priceSen = *in.PriceSen
	}
	var minSen int64
	var maxSen, suggestSen *int64
	if pType == dbstore.SaleTypeCustomAmount {
		if in.MinPriceSen == nil {
			return nil, productOut{}, fmt.Errorf("custom_amount requires min_price_sen (at least 100 sen / RM1.00)")
		}
		if len(in.Variations) > 0 {
			return nil, productOut{}, fmt.Errorf("a custom_amount product cannot have variations")
		}
		if msg := payments.ValidateCustomAmountLimits(*in.MinPriceSen, in.MaxPriceSen, in.SuggestedPriceSen); msg != "" {
			return nil, productOut{}, fmt.Errorf("%s", msg)
		}
		minSen, maxSen, suggestSen = *in.MinPriceSen, in.MaxPriceSen, in.SuggestedPriceSen
		priceSen = payments.CustomAmountDisplayPrice(minSen, suggestSen)
	} else if in.MinPriceSen != nil || in.MaxPriceSen != nil || in.SuggestedPriceSen != nil {
		return nil, productOut{}, fmt.Errorf("min_price_sen, max_price_sen and suggested_price_sen apply only to type custom_amount")
	}
	p := &dbstore.Product{
		AccountID: accountID, Name: in.Name, Description: in.Description,
		PriceSen: priceSen, Images: in.Images, Type: pType,
		BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock, SubscriptionCharges: in.SubscriptionCharges, ScheduleMode: schedMode,
		MinPriceSen: minSen, MaxPriceSen: maxSen, SuggestedPriceSen: suggestSen,
	}
	if err := s.deps.Products.Create(ctx, p); err != nil {
		return nil, productOut{}, fmt.Errorf("db: %w", err)
	}
	// Persist variations wholesale when provided.
	if len(in.Variations) > 0 && s.deps.ProductVariations != nil {
		if err := s.deps.ProductVariations.ReplaceAll(ctx, p.ID,
			toStoreVariations(in.Variations)); err != nil {
			return nil, productOut{}, fmt.Errorf("db: %w", err)
		}
	}
	out := productOut{ID: p.ID, Name: p.Name, Description: p.Description,
		Images: p.Images, PriceSen: p.PriceSen, Type: p.Type,
		BillingPeriod: p.BillingPeriod, TrialDays: p.TrialDays, Active: true,
		Stock: p.Stock, SubscriptionCharges: p.SubscriptionCharges,
		MinPriceSen: p.MinPriceSen, MaxPriceSen: p.MaxPriceSen, SuggestedPriceSen: p.SuggestedPriceSen}
	// Issue #153: return the persisted variations so the caller sees them.
	if s.deps.ProductVariations != nil {
		if vs, verr := s.deps.ProductVariations.ListByProduct(ctx, p.ID); verr == nil {
			out.Variations = toVariationOuts(vs)
		}
	}
	return nil, out, nil
}

type archiveProductIn struct {
	ProductID string `json:"product_id" jsonschema:"Product ID"`
}

// purgeProductIn confirms a PERMANENT delete of a trashed product. The
// identifier is the product's NAME, not a slug: products have no slug, so the
// name is what the caller can name back.
type purgeProductIn struct {
	ProductID   string `json:"product_id" jsonschema:"Product ID (must be in trash)"`
	ConfirmName string `json:"confirm_name,omitempty" jsonschema:"The product's own name, to confirm permanent deletion. Wrong/absent = rejected."`
}

func (s *Server) archiveProduct(ctx context.Context, _ *mcp.CallToolRequest, in archiveProductIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: the product may live in any of the user's workspaces; resolve +
	// RBAC-gate the owning account.
	acc, err := s.requireProductWrite(ctx, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Products.SetActive(ctx, acc, in.ProductID, false); err != nil {
		return nil, nil, fmt.Errorf("%w", err)
	}
	return nil, map[string]string{"status": "archived", "product_id": in.ProductID}, nil
}

// ─── Sales ───────────────────────────────────────────────────────────────────

type saleItemOut struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	PriceSen  int64  `json:"price_sen"`
	Qty       int64  `json:"qty"`
	// Issue #153: which variation was bought (empty for simple products).
	VariationID   string `json:"variation_id,omitempty"`
	VariationName string `json:"variation_name,omitempty"`
}

type saleOut struct {
	RefCode  string `json:"ref_code"`
	Status   string `json:"status"`
	TotalSen int64  `json:"total_sen"`
	// Currency is the currency the sale was RECORDED in (issue #185), so an
	// agent reading total_sen can label it without asking which workspace
	// setting is current — a workspace that switches currency keeps its old
	// rows in the currency they were paid in. Always set on a sale row (MYR
	// for rows written before the column existed).
	Currency  string `json:"currency,omitempty"`
	SaleType  string `json:"sale_type"`
	Client    string `json:"client"`
	CreatedAt string `json:"created_at"`
	// Issue #153: line items (with variation) so agents see what was bought.
	Items []saleItemOut `json:"items,omitempty"`
	// Issue #518: partial-refund accounting, so an agent can tell a partial
	// refund from a full one and see the balance still refundable.
	RefundedSen    int64  `json:"refunded_sen"`
	OutstandingSen int64  `json:"outstanding_sen"`
	RefundStatus   string `json:"refund_status"`
	// Issue #604: for a response-mode form the row is a SUBMISSION, not a sale
	// (issue #171) — sale_type is "response", submission_id identifies it and
	// data carries the recorded answers. Without these an agent following the
	// dashboard would see an empty list for a form that has entries.
	SubmissionID string         `json:"submission_id,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
	// AvailableEvents is the lifecycle table's answer for this sale: what it
	// accepts from its current state, with `enabled` and — when a guard refuses
	// an action — the English reason. Same list the REST row carries, so an agent
	// reading list_sales does not have to guess from `status` (and the in-app
	// chat, which reads this tool, sees the server's own answer before it
	// suggests anything). Always an array on a sale row; a response row has no
	// lifecycle, so it carries an empty one.
	AvailableEvents []lifecycle.AvailableEvent `json:"available_events"`
}

type listSalesIn struct {
	FormID string `json:"form_id,omitempty" jsonschema:"Optional — limit to this form's sales (id from list_forms/get_form)"`
	Status string `json:"status,omitempty" jsonschema:"Optional — limit to status: pending|paid|confirmed|failed|cancelled"`
}

// recordScope resolves the ONE sales-read decision for this MCP call: which
// tables does the call cover, given the caller's account scope and its optional
// form_id. It is the same dbstore.ResolveRecordScope the REST list and export
// use, so an agent and the dashboard cannot be told different things about the
// same account.
//
// Read-only. A failure to read the forms is returned rather than degraded to
// "sales only": that silent fallback is what hid response entries from every
// caller without a form_id (the reported defect).
func (s *Server) recordScope(ctx context.Context, accs []string, formID string) (dbstore.RecordScope, error) {
	if s.deps.Forms == nil {
		return dbstore.ResolveRecordScope(nil, formID), nil
	}
	forms, err := s.deps.Forms.ListByAccounts(ctx, accs)
	if err != nil {
		return dbstore.RecordScope{}, fmt.Errorf("db: %w", err)
	}
	return dbstore.ResolveRecordScope(forms, formID), nil
}

func (s *Server) listSales(ctx context.Context, _ *mcp.CallToolRequest, in listSalesIn) (*mcp.CallToolResult, *listOutput[saleOut], error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Sales == nil {
		return nil, nil, fmt.Errorf("sales not available")
	}
	// Issue #604 + the unfiltered-read defect: a response-mode form records no
	// sales — its entries are submissions (issue #171). Reading `sales` for such
	// a form (or for no form at all, when the scope owns one) returns an empty
	// or partial list, which reads to an agent as "nothing was recorded".
	// Resolve the record set once, then read it from the merged query.
	sc, err := s.recordScope(ctx, accs, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if in.Status != "" {
		// The tool has no date/q inputs, so only the status filter can apply —
		// and a status is a payment-status question a submission cannot answer,
		// so the responses half is excluded rather than silently emptied.
		sc.IncludeSubs = false
		sc.ResponseFormIDs = nil
		sc.Mixed = false
		sc.OnlyResponses = false
	}
	filter := dbstore.RecordFilter{FormID: in.FormID, Status: in.Status}
	records, _, err := s.deps.Sales.MergedPage(ctx, accs, sc, filter, 1, 50)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]saleOut, 0, len(records))
	for _, rec := range records {
		if rec.Submission != nil {
			out = append(out, submissionSaleOut(rec.Submission))
			continue
		}
		if rec.Sale == nil {
			continue
		}
		out = append(out, saleOutFromSale(rec.Sale))
	}
	return nil, newListOut(out), nil
}

// submissionSaleOut renders a submission in the sales tool's row shape, marked
// sale_type "response" — the same contract the REST list emits, so an agent
// reading either surface gets one answer.
func submissionSaleOut(sub *dbstore.Submission) saleOut {
	client := ""
	if v, ok := sub.Data["full_name"].(string); ok && v != "" {
		client = v
	} else if v, ok := sub.Data["email"].(string); ok {
		client = v
	}
	return saleOut{
		Status:       "response",
		SaleType:     "response",
		Client:       client,
		CreatedAt:    sub.CreatedAt.UTC().Format(time.RFC3339),
		SubmissionID: sub.ID,
		Data:         sub.Data,
		// A response has no lifecycle: an explicit empty list, never a missing
		// field (a caller must be able to tell "nothing to do" from "the server
		// did not answer").
		AvailableEvents: []lifecycle.AvailableEvent{},
	}
}

// saleOutFromSale maps a sale to the tool's row shape, including the line items
// (issue #153) and the partial-refund accounting (issue #518).
func saleOutFromSale(sl *dbstore.Sale) saleOut {
	o := saleOut{
		RefCode: sl.RefCode, Status: sl.Status, TotalSen: sl.TotalSen,
		Currency: sl.CurrencyOrMYR(),
		SaleType: sl.SaleType, Client: sl.Client.FullName,
		CreatedAt:   sl.CreatedAt.UTC().Format(time.RFC3339),
		RefundedSen: sl.RefundedSen, OutstandingSen: sl.OutstandingSen(),
		RefundStatus: sl.RefundStatus(),
		// The server's lifecycle answer travels with the row (card t_52429d6f):
		// an agent planning an action reads it instead of guessing from status.
		AvailableEvents: availableEventsOf(sl),
	}
	if len(sl.Items) > 0 {
		o.Items = make([]saleItemOut, 0, len(sl.Items))
		for _, it := range sl.Items {
			o.Items = append(o.Items, saleItemOut{
				ProductID: it.ProductID, Name: it.Name, PriceSen: it.PriceSen, Qty: it.Qty,
				VariationID: it.VariationID, VariationName: it.VariationName,
			})
		}
	}
	return o
}

type summaryOut struct {
	Pending   int64 `json:"pending"`
	Paid      int64 `json:"paid"`
	Confirmed int64 `json:"confirmed"`
	Failed    int64 `json:"failed"`
	Cancelled int64 `json:"cancelled"`
	Refunded  int64 `json:"refunded"`
	// Issue #518: refunded_sen is the total refunded across the account's
	// sales, and revenue_sen the NET paid/confirmed value (total - refunded),
	// so a partial refund is visible in the summary instead of the sale
	// silently reading as fully paid.
	//
	// Card t_8064a965: revenue_sen includes the buyer-borne processing fee, so
	// it is money BUYERS PAID, not money the merchant keeps. fees_sen is the fee
	// inside it and net_revenue_sen is revenue_sen − fees_sen — the figure a
	// merchant or an agent should quote as income. Same rule and same names as
	// GET /v1/rest/summary and the dashboard.
	RefundedSen   int64 `json:"refunded_sen"`
	RevenueSen    int64 `json:"revenue_sen"`
	FeesSen       int64 `json:"fees_sen"`
	NetRevenueSen int64 `json:"net_revenue_sen"`
}

func (s *Server) salesSummary(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, summaryOut, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, summaryOut{}, err
	}
	// Issue #307: summary spans all of the user's workspaces.
	sales, err := s.deps.Sales.ListByAccounts(ctx, accs, 200)
	if err != nil {
		return nil, summaryOut{}, fmt.Errorf("db: %w", err)
	}
	sum := summaryOut{}
	for _, sl := range sales {
		switch sl.Status {
		case dbstore.SalePending:
			sum.Pending++
		case dbstore.SalePaid:
			sum.Paid++
		case dbstore.SaleConfirmed:
			sum.Confirmed++
		case dbstore.SaleFailed:
			sum.Failed++
		case dbstore.SaleCancelled:
			sum.Cancelled++
		case dbstore.SaleRefunded:
			sum.Refunded++
		}
		sum.RefundedSen += sl.RefundedSen
		if sl.Status == dbstore.SalePaid || sl.Status == dbstore.SaleConfirmed {
			sum.RevenueSen += sl.TotalSen - sl.RefundedSen
			// Card t_8064a965: the buyer-borne fee is inside the total, so it
			// is not income. Read it from the same rows as the revenue and
			// derive what the merchant keeps, in integer sen.
			sum.FeesSen += sl.FeeSen
		}
	}
	sum.NetRevenueSen = sum.RevenueSen - sum.FeesSen
	return nil, sum, nil
}

type confirmSaleIn struct {
	RefCode string `json:"ref_code" jsonschema:"sale reference code"`
}

func (s *Server) confirmSale(ctx context.Context, _ *mcp.CallToolRequest, in confirmSaleIn) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireSaleWrite(ctx, in.RefCode)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != acc {
		return nil, nil, fmt.Errorf("sale not found")
	}
	// Same table as the REST door: `confirm` is defined from pending (a bank
	// transfer) and paid (a settled CHIP payment), and re-confirming is a no-op.
	res, lerr := lifecycle.SalesMachine.Apply(sale, lifecycle.SaleEventConfirm)
	if lerr != nil {
		return nil, nil, lerr
	}
	if res.Changed {
		if err := s.deps.Sales.SetStatus(ctx, sale.ID, res.To); err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
	}
	return nil, map[string]string{"status": "confirmed", "ref_code": in.RefCode}, nil
}

// ─── Per-sale email status (issue #106) ────────────────────────────────────

type saleEmailStatusIn struct {
	RefCode string `json:"ref_code" jsonschema:"sale reference code (from list_sales)"`
}

type saleEmailEventOut struct {
	EventType string `json:"event_type"`
	At        string `json:"at"`
}

type saleEmailStatusOut struct {
	RefCode string              `json:"ref_code"`
	Sent    bool                `json:"sent"`
	Events  []saleEmailEventOut `json:"events"`
}

// saleEmailStatusMCP returns the per-sale receipt email delivery timeline
// (issue #106): Sent → Delivered → Opened → (Clicked), with timestamps. The
// "sent" step is implied by the sale having a receipt_message_id; the rest
// come from SES delivery events correlated to the sale's ref.
func (s *Server) saleEmailStatusMCP(ctx context.Context, _ *mcp.CallToolRequest, in saleEmailStatusIn) (*mcp.CallToolResult, saleEmailStatusOut, error) {
	// Issue #645: reading a sale's receipt-email timeline is a pure READ. It
	// used to demand a readwrite scope, which refused a readonly key on a read.
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, saleEmailStatusOut{}, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != accountID {
		return nil, saleEmailStatusOut{}, fmt.Errorf("sale not found")
	}
	out := saleEmailStatusOut{RefCode: sale.RefCode, Sent: sale.ReceiptMessageID != "", Events: []saleEmailEventOut{}}
	if s.deps.EmailDeliveryEvents != nil {
		events, err := s.deps.EmailDeliveryEvents.ListBySaleRef(ctx, sale.RefCode)
		if err != nil {
			return nil, saleEmailStatusOut{}, fmt.Errorf("db: %w", err)
		}
		for _, e := range events {
			out.Events = append(out.Events, saleEmailEventOut{
				EventType: e.EventType,
				At:        e.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	return nil, out, nil
}

// ─── Subscriptions ───────────────────────────────────────────────────────────

type subscriptionOut struct {
	ID            string `json:"id"`
	Customer      string `json:"customer"` // client email
	ProductID     string `json:"product_id"`
	ProductName   string `json:"product_name"`
	AmountSen     int64  `json:"amount_sen"`
	Period        string `json:"period"` // month | year
	Status        string `json:"status"` // active | past_due | cancelled
	NextChargeAt  string `json:"next_charge_at"`
	RetryAttempts int64  `json:"retry_attempts"`
	CreatedAt     string `json:"created_at"`
}

type listSubscriptionsIn struct {
	Status string `json:"status,omitempty" jsonschema:"Optional — filter by status: active|past_due|cancelled"`
	Page   int    `json:"page,omitempty" jsonschema:"Optional — page (starting at 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional — limit per page (1-100, default 20)"`
}

func (s *Server) listSubscriptions(ctx context.Context, _ *mcp.CallToolRequest, in listSubscriptionsIn) (*mcp.CallToolResult, *listOutput[subscriptionOut], error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	switch in.Status {
	case "", dbstore.SubActive, dbstore.SubPastDue, dbstore.SubCanceled, dbstore.SubCompleted:
	default:
		return nil, nil, fmt.Errorf("invalid status")
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	// Issue #307: subscriptions span all of the user's workspaces.
	rows, _, err := s.deps.Subs.ListByAccountsPage(ctx, accs, in.Status, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]subscriptionOut, 0, len(rows))
	for _, sw := range rows {
		o := subscriptionOut{
			ID: sw.ID, Customer: sw.ClientEmail, ProductID: sw.ProductID,
			ProductName: sw.ProductName, AmountSen: sw.AmountSen, Period: sw.Period,
			Status: sw.Status, RetryAttempts: sw.RetryAttempts,
			CreatedAt: sw.CreatedAt.UTC().Format(time.RFC3339),
		}
		if !sw.NextChargeAt.IsZero() {
			o.NextChargeAt = sw.NextChargeAt.UTC().Format(time.RFC3339)
		}
		out = append(out, o)
	}
	return nil, newListOut(out), nil
}

type cancelSubIn struct {
	Email string `json:"email" jsonschema:"email of a customer with a subscription"`
}

func (s *Server) cancelSubscription(ctx context.Context, _ *mcp.CallToolRequest, in cancelSubIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: the subscription may be in any of the user's workspaces. Find the
	// owning account within scope, then RBAC-gate it.
	sub, accountID, err := s.subscriptionByEmail(ctx, in.Email)
	if err != nil {
		return nil, nil, err
	}
	if err := s.requireWriteRoleIn(ctx, accountID); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Subs.Cancel(ctx, sub.ID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	// Best-effort: delete the card token at CHIP so the card is un-future-charged.
	return nil, map[string]string{"status": "cancelled", "email": in.Email}, nil
}

type accountOut struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Currency string `json:"currency"`
	ChipMode string `json:"chip_mode"`
	// Issue #181: the account owner's optional phone number ("" when not set).
	Phone string `json:"phone,omitempty"`
	// Issue #191: the account owner's preferred IANA timezone ("" = Auto).
	Timezone string `json:"timezone,omitempty"`
}

func (s *Server) getAccount(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, accountOut, error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return nil, accountOut{}, fmt.Errorf("token missing or invalid")
	}
	// Issue #307: for a user-level OAuth token the account is scoped per call
	// (workspace_id/account_id or the user's first accessible account).
	accountID := id.AccountID
	if cs := callScopeFrom(ctx); cs != nil {
		if cs.ExplicitAccountID != "" {
			accountID = cs.ExplicitAccountID
		} else if len(cs.Accounts) > 0 {
			accountID = cs.Accounts[0]
		}
	}
	if accountID == "" {
		return nil, accountOut{}, fmt.Errorf("no account in scope")
	}
	acc, err := s.deps.Accounts.ByID(ctx, accountID)
	if err != nil {
		return nil, accountOut{}, fmt.Errorf("db: %w", err)
	}
	phone := ""
	tz := ""
	if s.deps.Users != nil {
		if u, uerr := s.deps.Users.ByID(ctx, acc.OwnerUserID); uerr == nil && u != nil {
			phone = u.Phone
			tz = u.Timezone
		}
	}
	return nil, accountOut{Name: acc.Name, Slug: acc.Slug, Currency: acc.Currency, ChipMode: acc.ChipMode, Phone: phone, Timezone: tz}, nil
}
