package mcpsrv

// Card t_93844723 — issue #185 residue on the MCP surface.
//
// get_form_analytics and get_product_analytics used to return money as
// `revenue_rm` / `refunded_rm` / `paid_revenue_rm`: a float in major units,
// under a field NAME that asserted ringgit, with no currency anywhere in the
// payload. In a USD workspace that made a US$24.90 sale arrive as
// `revenue_rm: 24.9` — the number right, the label a lie, and no way for the
// agent to tell. v1.22.73 (#185 / PR #1519) fixed the dashboard and the CSV;
// this is the MCP half, fixed the same way:
//
//   - money in integer MINOR units (`revenue_sen`, `refunded_sen`,
//     `paid_revenue_sen`) — the repo's one money rule, and it does not assume
//     two decimal places the way /100 does;
//   - the split per currency each sale was RECORDED in, keyed by ISO code, so
//     an agent can label each figure in the money it was actually paid in;
//   - no `*_rm` field at all — keeping one "for compatibility" would leave the
//     USD payload asserting ringgit, which is the defect this card is about.
//
// The tests are two layers on purpose: the mapping test needs no DB so it
// always runs, and the store-backed test proves the currency actually travels
// from the SALE row through dbstore into the tool's payload (a mapping-only
// test would stay green if the store stopped populating the maps).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// jsonKeys returns the top-level keys of a marshaled value.
func jsonKeys(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return wire
}

// assertNoRinggitFieldNames fails if any key asserts RM — the field name is the
// claim an agent reads first, and a value can be in any currency.
func assertNoRinggitFieldNames(t *testing.T, what string, wire map[string]json.RawMessage) {
	t.Helper()
	for k := range wire {
		if strings.HasSuffix(k, "_rm") {
			t.Errorf("%s payload still has a field named %q — its value is in the sale's own currency, not necessarily ringgit", what, k)
		}
	}
}

func TestFormStatsPayloadCarriesCurrencyNotRinggitNames(t *testing.T) {
	out := formStatsOutFrom(dbstore.FormStats{
		Title: "Tee", Slug: "tee", Mode: "payment",
		Submissions: 1, PaidCount: 1, ConfirmedCnt: 1, RefundedCount: 1,
		RevenueSen: 1990, RefundedSen: 500,
		RevenueByCurrency:  map[string]int64{"USD": 1990},
		RefundedByCurrency: map[string]int64{"USD": 500},
	})
	wire := jsonKeys(t, out)
	assertNoRinggitFieldNames(t, "get_form_analytics", wire)

	var revenue, refunded int64
	var byCur, refundedByCur map[string]int64
	if err := json.Unmarshal(wire["revenue_sen"], &revenue); err != nil {
		t.Fatalf("revenue_sen: %v (payload %v)", err, keysOf(wire))
	}
	if err := json.Unmarshal(wire["refunded_sen"], &refunded); err != nil {
		t.Fatalf("refunded_sen: %v (payload %v)", err, keysOf(wire))
	}
	if err := json.Unmarshal(wire["revenue_by_currency"], &byCur); err != nil {
		t.Fatalf("revenue_by_currency: %v (payload %v)", err, keysOf(wire))
	}
	if err := json.Unmarshal(wire["refunded_by_currency"], &refundedByCur); err != nil {
		t.Fatalf("refunded_by_currency: %v (payload %v)", err, keysOf(wire))
	}
	if revenue != 1990 || refunded != 500 {
		t.Errorf("revenue_sen/refunded_sen = %d/%d, want 1990/500 (minor units, not 19.9/5.0)", revenue, refunded)
	}
	if byCur["USD"] != 1990 {
		t.Errorf("revenue_by_currency[USD] = %d, want 1990 — a USD sale must not be labelled as ringgit", byCur["USD"])
	}
	if refundedByCur["USD"] != 500 {
		t.Errorf("refunded_by_currency[USD] = %d, want 500", refundedByCur["USD"])
	}
}

func TestProductStatsPayloadCarriesCurrencyNotRinggitNames(t *testing.T) {
	out := productStatsOutFrom(dbstore.ProductStats{
		Name: "Tee", UnitsSold: 1, PaidUnits: 1,
		RevenueSen: 2490, PaidRevenue: 1990,
		RevenueByCurrency:     map[string]int64{"USD": 2490},
		PaidRevenueByCurrency: map[string]int64{"USD": 1990},
	})
	wire := jsonKeys(t, out)
	assertNoRinggitFieldNames(t, "get_product_analytics", wire)

	if got := rawInt(t, wire, "revenue_sen"); got != 2490 {
		t.Errorf("revenue_sen = %d, want 2490", got)
	}
	if got := rawInt(t, wire, "paid_revenue_sen"); got != 1990 {
		t.Errorf("paid_revenue_sen = %d, want 1990 (net of the 500 refund)", got)
	}
	if wire["paid_revenue_by_currency"] == nil {
		t.Fatalf("payload has no paid_revenue_by_currency: %v", keysOf(wire))
	}
	var paidByCur map[string]int64
	if err := json.Unmarshal(wire["paid_revenue_by_currency"], &paidByCur); err != nil {
		t.Fatalf("paid_revenue_by_currency: %v", err)
	}
	if paidByCur["USD"] != 1990 {
		t.Errorf("paid_revenue_by_currency[USD] = %d, want 1990", paidByCur["USD"])
	}
}

func rawInt(t *testing.T, wire map[string]json.RawMessage, key string) int64 {
	t.Helper()
	raw, ok := wire[key]
	if !ok {
		t.Fatalf("payload has no %q: %v", key, keysOf(wire))
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("%s is not an integer: %v (%s)", key, err, raw)
	}
	return n
}

func keysOf(wire map[string]json.RawMessage) []string {
	out := make([]string, 0, len(wire))
	for k := range wire {
		out = append(out, k)
	}
	return out
}

// TestAnalyticsToolsAdvertiseTheNewMoneyFields closes the "advertised contract"
// gap: the payload tests above prove what the handler RETURNS, this one proves
// what a schema-strict MCP client is TOLD it will get. A tool that still
// advertises a `*_rm` property (or drops the per-currency maps) would fail
// validation at call time — or worse, tell an agent to read a ringgit field.
// It enumerates over the real protocol (tools/list), so a renamed or
// re-registered handler cannot pass by matching a source literal.
func TestAnalyticsToolsAdvertiseTheNewMoneyFields(t *testing.T) {
	want := map[string][]string{
		"get_form_analytics": {
			"revenue_sen", "refunded_sen", "revenue_by_currency", "refunded_by_currency",
		},
		"get_product_analytics": {
			"revenue_sen", "paid_revenue_sen", "revenue_by_currency", "paid_revenue_by_currency",
		},
	}
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()
	client := mcp.NewClient(&mcp.Implementation{Name: "analytics-schema-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	seen := map[string]bool{}
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			fields, ok := want[tl.Name]
			if !ok || seen[tl.Name] {
				continue
			}
			seen[tl.Name] = true
			raw, err := json.Marshal(tl.OutputSchema)
			if err != nil {
				t.Fatalf("marshal %s outputSchema: %v", tl.Name, err)
			}
			schema := string(raw)
			for _, f := range fields {
				if !strings.Contains(schema, `"`+f+`"`) {
					t.Errorf("%s outputSchema does not advertise %q — an agent validating the response cannot read the currency (schema: %s)", tl.Name, f, schema)
				}
			}
			if strings.Contains(schema, `_rm"`) {
				t.Errorf("%s outputSchema still advertises a *_rm money field — its values are in the sale's own currency (schema: %s)", tl.Name, schema)
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is not registered — the payload guard above tested a handler no client can call", name)
		}
	}
}

// TestAnalyticsToolPayloadCarriesTheSalesRecordedCurrency is the end-to-end
// half: a real USD sale with a partial refund (plus an MYR sale on the same
// form) must come back from the TOOL as minor units split per currency, with no
// `*_rm` name anywhere.
func TestAnalyticsToolPayloadCarriesTheSalesRecordedCurrency(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('anacur-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'anacur-' || md5(random()::text), 'anacur-' || md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	form := &dbstore.Form{
		AccountID: accountID, Slug: "anacur-" + accountID[:8], Title: "Analytics currency",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	sales := dbstore.NewSaleStore(pool)
	usd := &dbstore.Sale{
		AccountID: accountID, FormID: form.ID, RefCode: "ANACUR-USD-" + accountID[:8],
		Client:      dbstore.SaleClient{FullName: "US buyer", Email: "us@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "prod-usd", Name: "Tee", PriceSen: 2490, Qty: 1}},
		SubtotalSen: 2490,
		TotalSen:    2490, Currency: "USD", SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	if err := sales.Create(ctx, usd); err != nil {
		t.Fatalf("create USD sale: %v", err)
	}
	// A partial refund leaves the sale paid; only refunded_sen moves.
	if _, err := pool.Exec(ctx, `UPDATE sales SET refunded_sen = 500 WHERE id = $1`, usd.ID); err != nil {
		t.Fatalf("refund USD sale: %v", err)
	}
	myr := &dbstore.Sale{
		AccountID: accountID, FormID: form.ID, RefCode: "ANACUR-MYR-" + accountID[:8],
		Client:      dbstore.SaleClient{FullName: "MY buyer", Email: "my@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "prod-myr", Name: "Tee MY", PriceSen: 1000, Qty: 1}},
		SubtotalSen: 1000,
		TotalSen:    1000, Currency: "MYR", SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	if err := sales.Create(ctx, myr); err != nil {
		t.Fatalf("create MYR sale: %v", err)
	}

	srv := New(&Deps{Accounts: dbstore.NewAccountStore(pool), Sales: sales, Forms: forms}, nil, nil, nil)
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	_, formOut, err := srv.formAnalytics(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_form_analytics: %v", err)
	}
	if len(formOut.Items) != 1 {
		t.Fatalf("get_form_analytics returned %d forms, want 1", len(formOut.Items))
	}
	fWire := jsonKeys(t, formOut.Items[0])
	assertNoRinggitFieldNames(t, "get_form_analytics", fWire)
	// (2490 - 500) USD + 1000 MYR, in minor units. The scalar is a sum ACROSS
	// currencies; the maps are the label a reader must use.
	if got := rawInt(t, fWire, "revenue_sen"); got != 2990 {
		t.Errorf("revenue_sen = %d, want 2990", got)
	}
	if got := rawInt(t, fWire, "refunded_sen"); got != 500 {
		t.Errorf("refunded_sen = %d, want 500", got)
	}
	var revByCur, refByCur map[string]int64
	if err := json.Unmarshal(fWire["revenue_by_currency"], &revByCur); err != nil {
		t.Fatalf("revenue_by_currency: %v (payload %v)", err, keysOf(fWire))
	}
	if err := json.Unmarshal(fWire["refunded_by_currency"], &refByCur); err != nil {
		t.Fatalf("refunded_by_currency: %v (payload %v)", err, keysOf(fWire))
	}
	if revByCur["USD"] != 1990 || revByCur["MYR"] != 1000 {
		t.Errorf("revenue_by_currency = %v, want USD 1990 + MYR 1000 (a USD sale comes back as USD, never as RM)", revByCur)
	}
	if refByCur["USD"] != 500 {
		t.Errorf("refunded_by_currency = %v, want USD 500", refByCur)
	}

	_, prodOut, err := srv.productAnalytics(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_product_analytics: %v", err)
	}
	if len(prodOut.Items) != 2 {
		t.Fatalf("get_product_analytics returned %d products, want 2", len(prodOut.Items))
	}
	for _, p := range prodOut.Items {
		pWire := jsonKeys(t, p)
		assertNoRinggitFieldNames(t, "get_product_analytics", pWire)
		switch p.Name {
		case "Tee":
			if got := rawInt(t, pWire, "revenue_sen"); got != 2490 {
				t.Errorf("product revenue_sen = %d, want 2490", got)
			}
			if got := rawInt(t, pWire, "paid_revenue_sen"); got != 1990 {
				t.Errorf("product paid_revenue_sen = %d, want 1990 (net of refund)", got)
			}
			var paidByCur, grossByCur map[string]int64
			if err := json.Unmarshal(pWire["paid_revenue_by_currency"], &paidByCur); err != nil {
				t.Fatalf("paid_revenue_by_currency: %v", err)
			}
			if err := json.Unmarshal(pWire["revenue_by_currency"], &grossByCur); err != nil {
				t.Fatalf("revenue_by_currency: %v", err)
			}
			if paidByCur["USD"] != 1990 || grossByCur["USD"] != 2490 {
				t.Errorf("product USD maps = paid %v / gross %v, want paid 1990 / gross 2490", paidByCur, grossByCur)
			}
		case "Tee MY":
			if gross, paid := rawInt(t, pWire, "revenue_sen"), rawInt(t, pWire, "paid_revenue_sen"); gross != 1000 || paid != 1000 {
				t.Errorf("MYR product revenue/paid = %d/%d, want 1000/1000", gross, paid)
			}
		default:
			t.Errorf("unexpected product %q in the rollup", p.Name)
		}
	}
}
