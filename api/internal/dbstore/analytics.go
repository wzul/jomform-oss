// Analytics: per-form & per-product submission/sales stats.
package dbstore

import "context"

// FormStats summarises one form's performance.
type FormStats struct {
	FormID       string `json:"form_id"`
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Submissions  int64  `json:"submissions"` // all sales rows (any status)
	PaidCount    int64  `json:"paid_count"`  // paid + confirmed
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
		       COUNT(s.id)                                  AS submissions,
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
		if err := rows.Scan(&fs.FormID, &fs.Slug, &fs.Title,
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
