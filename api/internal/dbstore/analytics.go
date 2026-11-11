// Analytics: per-form & per-product submission/sales stats.
package dbstore

import (
	"context"
	"sort"
)

// FormStats summarises one form's performance.
type FormStats struct {
	FormID string `json:"form_id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	// Mode is the form's collection mode (dbstore.FormMode), carried in the
	// payload so the caller can LABEL Submissions honestly — see below.
	Mode string `json:"mode"`
	// Submissions means "entries recorded for this form": the number of entry
	// rows the form has produced, read from the table the form actually writes
	// to — a response form writes `submissions` (issue #171), a payment form
	// writes `sales`.
	//
	// Why the field is defined this way (issue #598):
	//   * The mode is exclusive. The public submit path branches on it and
	//     writes exactly ONE table (public.go handleSubmit → handleResponseSubmit
	//     or the shared order core), so this figure can never be the sum of two
	//     counts and a form that produces "both" cannot double count.
	//   * Reporting 0 for a payment form that has entries would reintroduce the
	//     exact defect #598 reported ("a working counter reading zero"), on the
	//     other mode.
	//   * The paid/confirmed/pending/failed/refunded/revenue figures stay their
	//     own separately named fields, so a sale's money is never conflated with
	//     the entries count.
	//
	// What was actually wrong was the LABEL, not the number: the dashboard
	// rendered this figure as "Submissions:" for every form while a payment
	// form's value is a count of sales rows (any status). The payload therefore
	// carries Mode (above) and the dashboard labels the figure per mode —
	// "Submission(s)" for a response form, "Entries" for a payment form.
	Submissions  int64 `json:"submissions"`
	PaidCount    int64 `json:"paid_count"` // paid + confirmed
	ConfirmedCnt int64 `json:"confirmed_count"`
	FailedCount  int64 `json:"failed_count"`
	PendingCount int64 `json:"pending_count"`
	// RefundedCount is how many sales have any refunded amount (issue #518).
	RefundedCount int64 `json:"refunded_count"`
	// RevenueSen is the NET paid + confirmed total (total_sen - refunded_sen),
	// summed across currencies. It stays for consumers that read one number;
	// a reader that has to LABEL the figure must use RevenueByCurrency below,
	// because a form's history can span currencies (issue #185).
	RevenueSen int64 `json:"revenue_sen"`
	// RevenueByCurrency is the same NET revenue grouped by the currency each
	// sale was recorded in, in integer minor units. The dashboard renders one
	// label per currency ("RM24.90 + US$1.00") instead of printing a single
	// hardcoded symbol over a sum of different monies. A currency with sales
	// but no paid money keeps an explicit 0 so the label still names the form's
	// own money ("US$0.00", never "RM0.00").
	RevenueByCurrency map[string]int64 `json:"revenue_by_currency,omitempty"`
	// RefundedSen is the total refunded across the form's sales.
	RefundedSen int64 `json:"refunded_sen"`
	// FeesSen is the buyer-borne processing fee contained in RevenueSen, summed
	// across currencies (card t_8064a965), in integer minor units. It is the
	// money the acquirer took on top of the merchant's price — RevenueSen
	// includes it because the buyer paid it, and the merchant never receives it.
	FeesSen int64 `json:"fees_sen"`
	// NetRevenueSen is what the merchant actually keeps: RevenueSen − FeesSen.
	// The two figures are named together everywhere so no surface has to choose
	// between "what buyers paid" and "what I earned" — see /docs/payment-fees.
	NetRevenueSen int64 `json:"net_revenue_sen"`
	// FeesByCurrency / NetRevenueByCurrency are the two figures above grouped by
	// the currency each sale was recorded in, for the same reason as
	// RevenueByCurrency: a reader that labels the amount must not put one symbol
	// over a sum of different monies.
	FeesByCurrency       map[string]int64 `json:"fees_by_currency,omitempty"`
	NetRevenueByCurrency map[string]int64 `json:"net_revenue_by_currency,omitempty"`
	// RefundedByCurrency is RefundedSen grouped by the currency each sale was
	// recorded in (issue #185), in integer minor units — the same reason as
	// RevenueByCurrency above: a reader that labels the figure must not put one
	// symbol over refunds taken in more than one currency.
	RefundedByCurrency map[string]int64 `json:"refunded_by_currency,omitempty"`
}

// ProductStats summarises how often a product appears in sales.
type ProductStats struct {
	ProductID  string `json:"product_id"`
	Name       string `json:"name"`
	UnitsSold  int64  `json:"units_sold"`
	PaidUnits  int64  `json:"paid_units"`
	RevenueSen int64  `json:"revenue_sen"`
	// PaidRevenue is the NET paid revenue for this product (issue #518): the
	// refunded share of each line is subtracted proportionally to the sale
	// total, so a partial refund reduces the reported revenue.
	PaidRevenue int64 `json:"paid_revenue_sen"`
	// RevenueByCurrency / PaidRevenueByCurrency are the two figures above
	// grouped by the currency each sale was recorded in (issue #185), in
	// integer minor units. Same rule as FormStats.RevenueByCurrency: one label
	// per currency, never a sum across them under one symbol.
	RevenueByCurrency     map[string]int64 `json:"revenue_by_currency,omitempty"`
	PaidRevenueByCurrency map[string]int64 `json:"paid_revenue_by_currency,omitempty"`
}

const paidSet = `('paid','confirmed')`

// responseFormSQL is the SQL half of dbstore.FormMode, so a dashboard count can
// never label a form differently from the rule the page and the submit path
// branch on. Card t_827b0a78: an ENABLED config.custom_html section is a
// response form even when `mode` is missing, exactly as the Go rule reports —
// without this the rollup called such a form a payment form and counted its
// sales table while its entries were landing in submissions.
const responseFormSQL = `(f.config->>'mode' = 'response'
	 OR btrim(coalesce(f.config->'custom_html'->>'html', '')) <> '')`

// FormAnalytics returns per-form rollups for an account.
func (s *SaleStore) FormAnalytics(ctx context.Context, accountID string) ([]FormStats, error) {
	return s.FormAnalyticsAccounts(ctx, []string{accountID})
}

// FormAnalyticsAccounts returns per-form rollups across a set of accounts.
func (s *SaleStore) FormAnalyticsAccounts(ctx context.Context, accountIDs []string) ([]FormStats, error) {
	if len(accountIDs) == 0 {
		return []FormStats{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.slug, f.title,
		       -- Issue #598: the mode rides along so a caller can label the
		       -- figure honestly. Same rule as dbstore.FormMode (one shared
		       -- fragment, responseFormSQL): an explicit JSON string
		       -- "response" is a response form, and so is a config that
		       -- carries an ENABLED custom_html section without one (card
		       -- t_827b0a78). A legacy form with no mode key (or mode: 1)
		       -- and no custom body reads as payment — exactly what the Go
		       -- rule returns.
		       CASE WHEN `+responseFormSQL+`
		            THEN 'response' ELSE 'payment' END       AS mode,
		       -- Issue #604: a response-mode form records NOTHING in the sales
		       -- table — its entries live in submissions (issue #171). Counting
		       -- sales alone reported 0 for such a form, which made "nothing was
		       -- recorded" look confirmed on the dashboard while submissions
		       -- existed. Count the table the form actually writes to.
		       --
		       -- Issue #598: ONE table, never a SUM of both. The mode is
		       -- exclusive — the public submit path writes the submissions row
		       -- OR the sale, never both (public.go handleSubmit) — so a
		       -- payment form that once recorded sales and was later switched to
		       -- response mode cannot have its two tables added together and
		       -- double counted.
		       CASE WHEN `+responseFormSQL+`
		            THEN (SELECT COUNT(*) FROM submissions sub WHERE sub.form_id = f.id)
		            ELSE COUNT(s.id) END                     AS submissions,
		       COUNT(s.id) FILTER (WHERE s.status IN `+paidSet+`) AS paid_count,
		       COUNT(s.id) FILTER (WHERE s.status = 'confirmed')  AS confirmed_cnt,
		       COUNT(s.id) FILTER (WHERE s.status = 'failed')     AS failed_cnt,
		       COUNT(s.id) FILTER (WHERE s.status = 'pending')    AS pending_cnt,
		       COUNT(s.id) FILTER (WHERE s.refunded_sen > 0)      AS refunded_cnt,
		       -- Issue #518: revenue is NET of refunds. A partially refunded sale
		       -- keeps its paid/confirmed status, so summing total_sen would
		       -- report money that has already gone back to the buyer.
		       COALESCE(SUM(s.total_sen - s.refunded_sen) FILTER (WHERE s.status IN `+paidSet+`), 0) AS revenue,
		       -- Card t_8064a965: the buyer-borne processing fee is INSIDE total_sen,
		       -- so the revenue above counts money the merchant never receives. Read
		       -- it on the same rows, so a reader can always name both figures.
		       COALESCE(SUM(s.fee_sen) FILTER (WHERE s.status IN `+paidSet+`), 0) AS fees,
		       COALESCE(SUM(s.refunded_sen), 0) AS refunded
		FROM forms f
		LEFT JOIN sales s ON s.form_id = f.id
		WHERE f.account_id = ANY($1)
		GROUP BY f.id, f.slug, f.title
		ORDER BY submissions DESC`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FormStats{}
	for rows.Next() {
		var fs FormStats
		if err := rows.Scan(&fs.FormID, &fs.Slug, &fs.Title, &fs.Mode,
			&fs.Submissions, &fs.PaidCount, &fs.ConfirmedCnt,
			&fs.FailedCount, &fs.PendingCount, &fs.RefundedCount,
			&fs.RevenueSen, &fs.FeesSen, &fs.RefundedSen); err != nil {
			return nil, err
		}
		// Card t_8064a965: what the merchant keeps is the collected revenue
		// minus the buyer-borne fee, in integer sen. Computed here, once, so
		// every consumer of the rollup divides it the same way.
		fs.NetRevenueSen = fs.RevenueSen - fs.FeesSen
		out = append(out, fs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Issue #185: the money above is one number because the payload's
	// consumers used to read it that way, but a form's sales can be recorded in
	// more than one currency (a workspace that switched). The label needs the
	// split, so read every money figure (NET revenue, the buyer-borne fee inside
	// it, and refunds) grouped by the sale's own currency.
	// A second query rather than a GROUP BY on the rollup: the `submissions`
	// subquery is per-form, so grouping the join by currency would repeat it
	// once per currency and summing would over-count it.
	byCurrency, err := s.formCurrencyTotals(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if t, ok := byCurrency[out[i].FormID]; ok {
			out[i].RevenueByCurrency = t.revenue
			out[i].FeesByCurrency = t.fees
			out[i].NetRevenueByCurrency = t.net
			out[i].RefundedByCurrency = t.refunded
		}
	}
	return out, nil
}

// formCurrencyTotals is form id → currency → the money figures FormStats
// reports, in integer minor units. Kept as one struct so the revenue, the fee
// inside it and the refunds of a form are read in the SAME query: they come off
// the same sale rows, and two queries could disagree if a sale changed between
// them.
type formCurrencyTotals struct {
	revenue  map[string]int64
	refunded map[string]int64
	// fees and net are card t_8064a965: the buyer-borne processing fee inside
	// this currency's revenue, and revenue − fees (what the merchant keeps).
	fees map[string]int64
	net  map[string]int64
}

// formCurrencyTotals returns form id → currency → NET paid+confirmed revenue,
// the buyer-borne fee inside it, and the refunded total (minor units), reading
// the currency off each SALE. Forms with no sales are absent; a currency that
// only holds unpaid sales is present with 0 revenue, so the caller can still
// label a zero in the form's own money.
func (s *SaleStore) formCurrencyTotals(ctx context.Context, accountIDs []string) (map[string]formCurrencyTotals, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id,
		       COALESCE(NULLIF(upper(btrim(s.currency)), ''), 'MYR') AS currency,
		       COALESCE(SUM(s.total_sen - s.refunded_sen) FILTER (WHERE s.status IN `+paidSet+`), 0) AS revenue,
		       COALESCE(SUM(s.fee_sen) FILTER (WHERE s.status IN `+paidSet+`), 0) AS fees,
		       COALESCE(SUM(s.refunded_sen), 0) AS refunded
		FROM forms f
		JOIN sales s ON s.form_id = f.id
		WHERE f.account_id = ANY($1)
		GROUP BY f.id, currency`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]formCurrencyTotals{}
	for rows.Next() {
		var formID, cur string
		var revenue, fees, refunded int64
		if err := rows.Scan(&formID, &cur, &revenue, &fees, &refunded); err != nil {
			return nil, err
		}
		t, ok := out[formID]
		if !ok {
			t = formCurrencyTotals{
				revenue: map[string]int64{}, fees: map[string]int64{},
				net: map[string]int64{}, refunded: map[string]int64{},
			}
		}
		t.revenue[cur] = revenue
		t.fees[cur] = fees
		// Card t_8064a965: the same subtraction as the scalar above, in the
		// sale's own money, so a per-currency label can name both figures.
		t.net[cur] = revenue - fees
		t.refunded[cur] = refunded
		out[formID] = t
	}
	return out, rows.Err()
}

// ProductAnalytics rolls items up per product for an account.
func (s *SaleStore) ProductAnalytics(ctx context.Context, accountID string) ([]ProductStats, error) {
	return s.ProductAnalyticsAccounts(ctx, []string{accountID})
}

// ProductAnalyticsAccounts rolls items up per product across a set of accounts.
//
// The rows are grouped by product AND by the currency each sale was recorded in
// (issue #185), then merged here: the scalar totals stay the same numbers
// they always were, and the per-currency maps give a reader the split it needs
// to label the amount in the sale's own money instead of a hardcoded symbol.
//
// Card t_8064a965: the buyer-borne processing fee is NOT part of these figures
// and must not be subtracted from them. The money here is `price_sen * qty`
// from the sale's ITEMS — the merchant's own listed price — while the fee is a
// separate component of sale.total_sen (see /docs/payment-fees). Product
// revenue was already what the merchant keeps; only the sale-total aggregates
// (form revenue, the summary and the CSV) had the fee folded in.
func (s *SaleStore) ProductAnalyticsAccounts(ctx context.Context, accountIDs []string) ([]ProductStats, error) {
	if len(accountIDs) == 0 {
		return []ProductStats{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT elem->>'product_id'                       AS pid,
		       COALESCE(MIN(elem->>'name'), '')          AS name,
		       COALESCE(NULLIF(upper(btrim(s.currency)), ''), 'MYR') AS currency,
		       SUM((elem->>'qty')::int)                  AS units,
		       COALESCE(SUM((elem->>'qty')::int) FILTER (WHERE s.status IN `+paidSet+`), 0) AS paid_units,
		       COALESCE(SUM((elem->>'price_sen')::bigint * (elem->>'qty')::int), 0)                        AS gross,
		       -- Issue #518: paid_gross is NET of refunds. A partial refund leaves
		       -- the sale paid/confirmed, so the refunded share of each line is
		       -- subtracted proportionally to the sale total.
		       COALESCE(SUM((elem->>'price_sen')::bigint * (elem->>'qty')::int
		                    - CASE WHEN s.total_sen > 0
		                           THEN (elem->>'price_sen')::bigint * (elem->>'qty')::int * s.refunded_sen / s.total_sen
		                           ELSE 0 END)
		                  FILTER (WHERE s.status IN `+paidSet+`), 0)                                       AS paid_gross
		FROM sales s, jsonb_array_elements(`+saleItemsArray("s.items")+`) AS elem
		WHERE s.account_id = ANY($1)
		  AND elem->>'product_id' IS NOT NULL
		GROUP BY pid, currency`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	merged := map[string]*ProductStats{}
	order := []string{}
	for rows.Next() {
		var (
			p        ProductStats
			cur      string
			units    int64
			paid     int64
			gross    int64
			paidGros int64
		)
		if err := rows.Scan(&p.ProductID, &p.Name, &cur, &units, &paid, &gross, &paidGros); err != nil {
			return nil, err
		}
		cur = NormaliseCurrency(cur)
		acc, ok := merged[p.ProductID]
		if !ok {
			p.RevenueByCurrency = map[string]int64{}
			p.PaidRevenueByCurrency = map[string]int64{}
			acc = &p
			merged[p.ProductID] = acc
			order = append(order, p.ProductID)
		}
		acc.UnitsSold += units
		acc.PaidUnits += paid
		acc.RevenueSen += gross
		acc.PaidRevenue += paidGros
		acc.RevenueByCurrency[cur] += gross
		acc.PaidRevenueByCurrency[cur] += paidGros
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ProductStats, 0, len(order))
	for _, id := range order {
		out = append(out, *merged[id])
	}
	// The SQL used to ORDER BY paid_gross DESC; grouping split that ordering per
	// currency, so the merged rows are ordered here on the same figure.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PaidRevenue != out[j].PaidRevenue {
			return out[i].PaidRevenue > out[j].PaidRevenue
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
