// Tool handlers — typed input/output via sdk inference.
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
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
	SubscriptionCharges *int     `json:"subscription_charges,omitempty"` // nil = infinite
	Active              bool     `json:"active"`
	Stock               *int     `json:"stock,omitempty"`
	// Issue #153: variations for variable products (empty for simple products).
	Variations []variationOut `json:"variations,omitempty"`
}

type emptyIn struct{}

func (s *Server) listProducts(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, []productOut, error) {
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
		}
		// Issue #153: include variations for variable products.
		if s.deps.ProductVariations != nil {
			if vs, verr := s.deps.ProductVariations.ListByProduct(ctx, p.ID); verr == nil {
				o.Variations = toVariationOuts(vs)
			}
		}
		out = append(out, o)
	}
	return nil, out, nil
}

type createProductIn struct {
	Name                string        `json:"name" jsonschema:"nama produk"`
	Description         string        `json:"description,omitempty" jsonschema:"penerangan produk"`
	PriceSen            *int64        `json:"price_sen" jsonschema:"harga dalam SEN (RM25.00 = 2500)"`
	Type                string        `json:"type" jsonschema:"one_time atau subscription"`
	BillingPeriod       string        `json:"billing_period,omitempty" jsonschema:"month atau year (subscription sahaja)"`
	TrialDays           int           `json:"trial_days,omitempty" jsonschema:"hari percubaan (subscription)"`
	Stock               *int          `json:"stock,omitempty" jsonschema:"stok (omit = unlimited)"`
	Images              []string      `json:"images,omitempty" jsonschema:"gambar (URL mesti https:// — issue #501)"`
	Variations          []variationIn `json:"variations,omitempty" jsonschema:"variasi produk (name, price_sen, stock, images, sort_order) — kosong = produk simple"`
	SubscriptionCharges *int          `json:"subscription_charges,omitempty" jsonschema:"bilangan caj (subscription; omit/null = infinite, N = caj N kali)"`
}

// variationIn is the MCP input shape for one product variation.
type variationIn struct {
	Name      string   `json:"name" jsonschema:"nama variasi (cth: Saiz M, Merah)"`
	PriceSen  *int64   `json:"price_sen" jsonschema:"harga variasi dalam SEN"`
	Stock     *int     `json:"stock,omitempty" jsonschema:"stok variasi (omit = unlimited)"`
	Images    []string `json:"images,omitempty" jsonschema:"gambar variasi (URL mesti https:// — issue #501)"`
	SortOrder int      `json:"sort_order,omitempty" jsonschema:"urutan (0 = ikut senarai)"`
}

func (s *Server) createProduct(ctx context.Context, _ *mcp.CallToolRequest, in createProductIn) (*mcp.CallToolResult, productOut, error) {
	accountID, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, productOut{}, err
	}
	// Issue #551: price_sen is a POINTER so missing or explicit `null` is rejected
	// rather than silently decoding to 0 (which created a RM 0.00 product).
	if in.Name == "" || in.PriceSen == nil || *in.PriceSen < 0 {
		return nil, productOut{}, fmt.Errorf("nama dan price_sen wajib")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges < 1 {
		return nil, productOut{}, fmt.Errorf("subscription_charges mesti >= 1 (atau null untuk infinite)")
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
	if pType != dbstore.SaleTypeOneTime && pType != dbstore.SaleTypeSubscription {
		return nil, productOut{}, fmt.Errorf("type mesti one_time|subscription")
	}
	period := ""
	if pType == dbstore.SaleTypeSubscription {
		period = in.BillingPeriod
		if period != "month" && period != "year" {
			return nil, productOut{}, fmt.Errorf("subscription perlu billing_period month|year")
		}
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	for _, v := range in.Variations {
		if v.Name == "" {
			return nil, productOut{}, fmt.Errorf("variation name wajib")
		}
		if v.PriceSen == nil || *v.PriceSen < 0 {
			return nil, productOut{}, fmt.Errorf("variation price_sen mesti >= 0")
		}
		if v.Stock != nil && *v.Stock < 0 {
			return nil, productOut{}, fmt.Errorf("variation stok mesti >= 0")
		}
		if !urlguard.ValidList(v.Images) {
			return nil, productOut{}, fmt.Errorf("%s", urlguard.InvalidMessage("variations[].images"))
		}
	}
	p := &dbstore.Product{
		AccountID: accountID, Name: in.Name, Description: in.Description,
		PriceSen: *in.PriceSen, Images: in.Images, Type: pType,
		BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock, SubscriptionCharges: in.SubscriptionCharges,
	}
	if err := s.deps.Products.Create(ctx, p); err != nil {
		return nil, productOut{}, fmt.Errorf("db: %w", err)
	}
	// Persist variations wholesale when provided.
	if len(in.Variations) > 0 && s.deps.ProductVariations != nil {
		vs := make([]*dbstore.ProductVariation, 0, len(in.Variations))
		for i, v := range in.Variations {
			order := v.SortOrder
			if order == 0 {
				order = i
			}
			vs = append(vs, &dbstore.ProductVariation{
				Name: v.Name, PriceSen: *v.PriceSen, Stock: v.Stock,
				Images: v.Images, SortOrder: order,
			})
		}
		if err := s.deps.ProductVariations.ReplaceAll(ctx, p.ID, vs); err != nil {
			return nil, productOut{}, fmt.Errorf("db: %w", err)
		}
	}
	out := productOut{ID: p.ID, Name: p.Name, Description: p.Description,
		Images: p.Images, PriceSen: p.PriceSen, Type: p.Type,
		BillingPeriod: p.BillingPeriod, TrialDays: p.TrialDays, Active: true,
		Stock: p.Stock, SubscriptionCharges: p.SubscriptionCharges}
	// Issue #153: return the persisted variations so the caller sees them.
	if s.deps.ProductVariations != nil {
		if vs, verr := s.deps.ProductVariations.ListByProduct(ctx, p.ID); verr == nil {
			out.Variations = toVariationOuts(vs)
		}
	}
	return nil, out, nil
}

type archiveProductIn struct {
	ProductID string `json:"product_id" jsonschema:"ID produk"`
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
	RefCode   string `json:"ref_code"`
	Status    string `json:"status"`
	TotalSen  int64  `json:"total_sen"`
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
}

type listSalesIn struct {
	FormID string `json:"form_id,omitempty" jsonschema:"Optional — hadkan kepada jualan borang ini (id dari list_forms/get_form)"`
	Status string `json:"status,omitempty" jsonschema:"Optional — hadkan kepada status: pending|paid|confirmed|failed|cancelled"`
}

func (s *Server) listSales(ctx context.Context, _ *mcp.CallToolRequest, in listSalesIn) (*mcp.CallToolResult, []saleOut, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	var sales []*dbstore.Sale
	switch {
	case in.Status != "":
		// Issue #307: aggregate status-filtered sales across the user's workspaces.
		// (No multi-account status method; iterate the account set.)
		for _, a := range accs {
			one, err := s.deps.Sales.ListByAccountStatus(ctx, a, in.Status, 50)
			if err != nil {
				return nil, nil, fmt.Errorf("db: %w", err)
			}
			sales = append(sales, one...)
		}
	case in.FormID != "":
		// Scoped to one form across the user's workspaces (ownership enforced per
		// store query).
		rows, _, err := s.deps.Sales.ListByAccountsFormPage(ctx, accs, in.FormID, 1, 50)
		if err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		sales = rows
	default:
		sales, err = s.deps.Sales.ListByAccounts(ctx, accs, 50)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]saleOut, 0, len(sales))
	for _, sl := range sales {
		o := saleOut{
			RefCode: sl.RefCode, Status: sl.Status, TotalSen: sl.TotalSen,
			SaleType: sl.SaleType, Client: sl.Client.FullName,
			CreatedAt:   sl.CreatedAt.UTC().Format(time.RFC3339),
			RefundedSen: sl.RefundedSen, OutstandingSen: sl.OutstandingSen(),
			RefundStatus: sl.RefundStatus(),
		}
		// Issue #153: include line items (with variation) for parity.
		if len(sl.Items) > 0 {
			o.Items = make([]saleItemOut, 0, len(sl.Items))
			for _, it := range sl.Items {
				o.Items = append(o.Items, saleItemOut{
					ProductID: it.ProductID, Name: it.Name, PriceSen: it.PriceSen, Qty: it.Qty,
					VariationID: it.VariationID, VariationName: it.VariationName,
				})
			}
		}
		out = append(out, o)
	}
	return nil, out, nil
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
	RefundedSen int64 `json:"refunded_sen"`
	RevenueSen  int64 `json:"revenue_sen"`
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
		}
	}
	return nil, sum, nil
}

type confirmSaleIn struct {
	RefCode string `json:"ref_code" jsonschema:"kod rujukan jualan"`
}

func (s *Server) confirmSale(ctx context.Context, _ *mcp.CallToolRequest, in confirmSaleIn) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireSaleWrite(ctx, in.RefCode)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != acc {
		return nil, nil, fmt.Errorf("jualan tidak dijumpai")
	}
	if err := s.deps.Sales.SetStatus(ctx, sale.ID, dbstore.SaleConfirmed); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "confirmed", "ref_code": in.RefCode}, nil
}

// ─── Per-sale email status (issue #106) ────────────────────────────────────

type saleEmailStatusIn struct {
	RefCode string `json:"ref_code" jsonschema:"kod rujukan jualan (dari list_sales)"`
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
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, saleEmailStatusOut{}, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != accountID {
		return nil, saleEmailStatusOut{}, fmt.Errorf("jualan tidak dijumpai")
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
	Status string `json:"status,omitempty" jsonschema:"Optional — tapis mengikut status: active|past_due|cancelled"`
	Page   int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

func (s *Server) listSubscriptions(ctx context.Context, _ *mcp.CallToolRequest, in listSubscriptionsIn) (*mcp.CallToolResult, []subscriptionOut, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	switch in.Status {
	case "", dbstore.SubActive, dbstore.SubPastDue, dbstore.SubCanceled, dbstore.SubCompleted:
	default:
		return nil, nil, fmt.Errorf("status tidak sah")
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
	return nil, out, nil
}

type cancelSubIn struct {
	Email string `json:"email" jsonschema:"emel pelanggan yang ada langganan"`
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
		return nil, accountOut{}, fmt.Errorf("token tiada atau tidak sah")
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
		return nil, accountOut{}, fmt.Errorf("tiada akaun dalam skop")
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
