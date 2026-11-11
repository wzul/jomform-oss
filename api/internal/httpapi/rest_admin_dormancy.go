// Dormancy report — the read-only phase-1 surface (card t_94e5cca3).
//
// GET /v1/admin/dormancy?limit=200&format=json|csv
//
// Super-admin only, read-only, audited. It computes the report on demand from ONE
// database statement; it stores nothing and it changes nothing. The daily job
// (cmd/jomform-api) computes the same report to decide who gets the "your form is
// still alive" nudge, so the table an operator reads and the decision the job takes
// come from one model (dormancy.Build), not two.
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/dormancy"
)

// adminDormancyOut is the JSON body. `rows` is the whole report in dormancy order
// (most dormant first), each row carrying its bucket and, for an account that may
// NOT be treated as a candidate, the reason(s) why.
type adminDormancyOut struct {
	dormancy.Report
	// ReadOnly and Policy restate the phase-1 contract in the payload itself, so a
	// consumer (or an agent) cannot mistake this report for an instruction to
	// delete or unpublish anything.
	ReadOnly bool   `json:"read_only"`
	Policy   string `json:"policy"`
}

// dormancyPolicySentence is the contract phase 1 publishes. Phases 2 and 3 are
// separate decisions and are named as NOT IMPLEMENTED rather than promised.
const dormancyPolicySentence = "phase 1 of the dormancy policy: a read-only report and one nudge email. " +
	"No account is deleted, unpublished or flagged. Deletion (phase 3) and unpublishing (phase 2) are separate, unimplemented decisions."

// handleAdminDormancy: GET /v1/admin/dormancy — the per-account dormancy report.
func (h *RESTHandlers) handleAdminDormancy(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.dormancy")
	if h.Dormancy == nil {
		writeErr(w, http.StatusServiceUnavailable, "the dormancy store is not configured")
		return
	}

	limit := dbstore.DormancyReportLimitDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		} else {
			writeErr(w, http.StatusBadRequest, "limit must be a number")
			return
		}
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "csv" {
		writeErr(w, http.StatusBadRequest, "format must be json or csv")
		return
	}

	now := time.Now()
	rows, err := h.Dormancy.DormancyReport(r.Context(), dbstore.DormancyReportOptions{Limit: limit, Now: now})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to build the dormancy report")
		return
	}
	rep := dormancy.Build(rows, dormancy.Options{Now: now, Limit: limit})
	// Build clamps nothing; the STORE clamps, so report the cap the store really
	// applied rather than the number the caller asked for.
	if rep.Limit > dbstore.DormancyReportLimitMax {
		rep.Limit = dbstore.DormancyReportLimitMax
	}

	if format == "csv" {
		body, cerr := dormancy.CSV(rep)
		if cerr != nil {
			writeErr(w, http.StatusInternalServerError, "failed to render the report as CSV")
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="jomform-dormancy.csv"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
		return
	}
	respond(w, http.StatusOK, adminDormancyOut{
		Report:   rep,
		ReadOnly: true,
		Policy:   dormancyPolicySentence,
	})
}
