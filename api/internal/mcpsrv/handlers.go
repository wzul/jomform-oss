// Tool handlers — typed input/output via sdk inference.
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ─── Products ────────────────────────────────────────────────────────────────

type productOut struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	PriceSen      int64    `json:"price_sen"`
	Images        []string `json:"images,omitempty"`
	Type          string   `json:"type"`
	BillingPeriod string   `json:"billing_period,omitempty"`
	TrialDays     int      `json:"trial_days"`
	Active        bool     `json:"active"`
	Stock         *int     `json:"stock,omitempty"`
}

type emptyIn struct{}

func (s *Server) listProducts(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, []productOut, error) {
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	products, err := s.deps.Products.ListActiveByAccount(ctx, accountID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]productOut, 0, len(products))
	for _, p := range products {
		out = append(out, productOut{
			ID: p.ID, Name: p.Name, Description: p.Description, PriceSen: p.PriceSen,
			Images: p.Images, Type: p.Type, BillingPeriod: p.BillingPeriod,
			TrialDays: p.TrialDays, Active: p.Active, Stock: p.Stock,
		})
	}
	return nil, out, nil
}

type createProductIn struct {
	Name          string   `json:"name" jsonschema:"nama produk"`
	Description   string   `json:"description,omitempty" jsonschema:"penerangan produk"`
	PriceSen      int64    `json:"price_sen" jsonschema:"harga dalam SEN (RM25.00 = 2500)"`
	Type          string   `json:"type" jsonschema:"one_time atau subscription"`
	BillingPeriod string   `json:"billing_period,omitempty" jsonschema:"month atau year (subscription sahaja)"`
	TrialDays     int      `json:"trial_days,omitempty" jsonschema:"hari percubaan (subscription)"`
	Stock         *int     `json:"stock,omitempty" jsonschema:"stok (omit = unlimited)"`
	Images        []string `json:"images,omitempty" jsonschema:"gambar (URL)"`
}

func (s *Server) createProduct(ctx context.Context, _ *mcp.CallToolRequest, in createProductIn) (*mcp.CallToolResult, productOut, error) {
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, productOut{}, err
	}
	if in.Name == "" || in.PriceSen < 0 {
		return nil, productOut{}, fmt.Errorf("nama dan price_sen wajib")
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
	p := &dbstore.Product{
		AccountID: accountID, Name: in.Name, Description: in.Description,
		PriceSen: in.PriceSen, Images: in.Images, Type: pType,
		BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock,
	}
	if err := s.deps.Products.Create(ctx, p); err != nil {
		return nil, productOut{}, fmt.Errorf("db: %w", err)
	}
	return nil, productOut{ID: p.ID, Name: p.Name, Description: p.Description,
		Images: p.Images, PriceSen: p.PriceSen, Type: p.Type,
		BillingPeriod: p.BillingPeriod, TrialDays: p.TrialDays, Active: true,
		Stock: p.Stock}, nil
}

type archiveProductIn struct {
	ProductID string `json:"product_id" jsonschema:"ID produk"`
}

func (s *Server) archiveProduct(ctx context.Context, _ *mcp.CallToolRequest, in archiveProductIn) (*mcp.CallToolResult, map[string]string, error) {
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Products.SetActive(ctx, accountID, in.ProductID, false); err != nil {
		return nil, nil, fmt.Errorf("%w", err)
	}
	return nil, map[string]string{"status": "archived", "product_id": in.ProductID}, nil
}

// ─── Sales ───────────────────────────────────────────────────────────────────

type saleOut struct {
	RefCode   string `json:"ref_code"`
	Status    string `json:"status"`
	TotalSen  int64  `json:"total_sen"`
	SaleType  string `json:"sale_type"`
	Client    string `json:"client"`
	CreatedAt string `json:"created_at"`
}

type listSalesIn struct {
	FormID string `json:"form_id,omitempty" jsonschema:"Optional — hadkan kepada jualan borang ini (id dari list_forms/get_form)"`
	Status string `json:"status,omitempty" jsonschema:"Optional — hadkan kepada status: pending|paid|confirmed|failed|cancelled"`
}

func (s *Server) listSales(ctx context.Context, _ *mcp.CallToolRequest, in listSalesIn) (*mcp.CallToolResult, []saleOut, error) {
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	var sales []*dbstore.Sale
	switch {
	case in.Status != "":
		// Parity with the /sales page filter (issue #126): status-only filter,
		// account-scoped, newest first. Ownership is enforced inside the query.
		sales, err = s.deps.Sales.ListByAccountStatus(ctx, accountID, in.Status, 50)
	case in.FormID != "":
		// Scoped to one form; ownership enforced inside the store query.
		sales, err = s.deps.Sales.ListByAccountForm(ctx, accountID, in.FormID, 50)
	default:
		sales, err = s.deps.Sales.ListByAccount(ctx, accountID, 50)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]saleOut, 0, len(sales))
	for _, sl := range sales {
		out = append(out, saleOut{
			RefCode: sl.RefCode, Status: sl.Status, TotalSen: sl.TotalSen,
			SaleType: sl.SaleType, Client: sl.Client.FullName,
			CreatedAt: sl.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, out, nil
}

type summaryOut struct {
	Pending   int64 `json:"pending"`
	Paid      int64 `json:"paid"`
	Confirmed int64 `json:"confirmed"`
	Failed    int64 `json:"failed"`
	Cancelled int64 `json:"cancelled"`
}

func (s *Server) salesSummary(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, summaryOut, error) {
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, summaryOut{}, err
	}
	sales, err := s.deps.Sales.ListByAccount(ctx, accountID, 200)
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
		}
	}
	return nil, sum, nil
}

type confirmSaleIn struct {
	RefCode string `json:"ref_code" jsonschema:"kod rujukan jualan"`
}

func (s *Server) confirmSale(ctx context.Context, _ *mcp.CallToolRequest, in confirmSaleIn) (*mcp.CallToolResult, map[string]string, error) {
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != accountID {
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

type cancelSubIn struct {
	Email string `json:"email" jsonschema:"emel pelanggan yang ada langganan"`
}

func (s *Server) cancelSubscription(ctx context.Context, _ *mcp.CallToolRequest, in cancelSubIn) (*mcp.CallToolResult, map[string]string, error) {
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	// find active subs by email — list via DueActive would be wrong; use a direct lookup by email
	sub, err := s.deps.Subs.ByEmail(ctx, accountID, in.Email)
	if err != nil {
		return nil, nil, fmt.Errorf("langganan tidak dijumpai")
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
}

func (s *Server) getAccount(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, accountOut, error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return nil, accountOut{}, fmt.Errorf("token tiada atau tidak sah")
	}
	acc, err := s.deps.Accounts.ByID(ctx, id.AccountID)
	if err != nil {
		return nil, accountOut{}, fmt.Errorf("db: %w", err)
	}
	return nil, accountOut{Name: acc.Name, Slug: acc.Slug, Currency: acc.Currency, ChipMode: acc.ChipMode}, nil
}
