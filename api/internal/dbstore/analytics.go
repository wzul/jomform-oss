// Analytics: per-form & per-product submission/sales stats.
package dbstore

import "context"

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
	ConfirmedCnt int64  `json:"confirmed_count"`
	FailedCount  int64  `json:"failed_count"`
	PendingCount int64  `json:"pending_count"`
	// RefundedCount is how many sales have any refunded amount (issue #518).
	RefundedCount int64 `json:"refunded_count"`
	// RevenueSen is the NET paid + confirmed total (total_sen - refunded_sen).
	RevenueSen int64 `json:"revenue_sen"`
	// RefundedSen is the total refunded across the form's sales.
	RefundedSen int64 `json:"refunded_sen"`
}

// ProductStats summarises how often a product appears in sales.
type ProductStats struct {
	ProductID   string `json:"product_id"`
	Name        string `json:"name"`
	UnitsSold   int64  `json:"units_sold"`
	PaidUnits   int64  `json:"paid_units"`
	RevenueSen  int64  `json:"revenue_sen"`
	// PaidRevenue is the NET paid revenue for this product (issue #518): the
	// refunded share of each line is subtracted proportionally to the sale
	// total, so a partial refund reduces the reported revenue.
	PaidRevenue int64 `json:"paid_revenue_sen"`
}

const paidSet = `('paid','confirmed')`

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
		       -- figure honestly. Same rule as dbstore.FormMode: only an
		       -- explicit JSON string "response" is a response form, so a
		       -- legacy form with no mode key (or mode: 1) reads as payment —
		       -- exactly what the Go rule returns.
		       CASE WHEN f.config->>'mode' = 'response'
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
		       CASE WHEN f.config->>'mode' = 'response'
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
			&fs.RevenueSen, &fs.RefundedSen); err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}

// ProductAnalytics rolls items up per product for an account.
func (s *SaleStore) ProductAnalytics(ctx context.Context, accountID string) ([]ProductStats, error) {
	return s.ProductAnalyticsAccounts(ctx, []string{accountID})
}

// ProductAnalyticsAccounts rolls items up per product across a set of accounts.
func (s *SaleStore) ProductAnalyticsAccounts(ctx context.Context, accountIDs []string) ([]ProductStats, error) {
	if len(accountIDs) == 0 {
		return []ProductStats{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT elem->>'product_id'                       AS pid,
		       COALESCE(MIN(elem->>'name'), '')          AS name,
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
		FROM sales s, jsonb_array_elements(s.items) AS elem
		WHERE s.account_id = ANY($1)
		  AND elem->>'product_id' IS NOT NULL
		GROUP BY pid
		ORDER BY paid_gross DESC`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProductStats{}
	for rows.Next() {
		var p ProductStats
		if err := rows.Scan(&p.ProductID, &p.Name, &p.UnitsSold,
			&p.PaidUnits, &p.RevenueSen, &p.PaidRevenue); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
