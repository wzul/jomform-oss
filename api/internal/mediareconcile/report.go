package mediareconcile

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// MaxWorkspaceBytes is the per-workspace storage cap the quota counter is
// enforced against. It mirrors objstore.MaxWorkspaceBytes; a test asserts the
// two stay equal so a cap change cannot leave this tool agreeing with a cap that
// no upload handler uses.
const MaxWorkspaceBytes = 1 << 30 // 1GB

// Account is one row of the `accounts` table that owns media.
type Account struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Counter int64  `json:"media_used_bytes"`
}

// Counts is a classified bucket of groups/objects/bytes.
type Counts struct {
	Groups  int   `json:"groups"`
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
}

// AccountSummary is one account's store-vs-row-vs-counter comparison.
//
// StoreBytes is the sum of EVERY object under the account's key prefix (parsed
// or not); RowBytes is the sum of the account's media rows of ANY state; Counter
// is what the quota actually holds today; Recomputed is what the counter would
// become if it were derived from the store (clamped to the cap).
type AccountSummary struct {
	AccountID  string `json:"account_id"`
	Name       string `json:"name"`
	Objects    int    `json:"objects"`
	StoreBytes int64  `json:"store_bytes"`
	RowCount   int    `json:"row_count"`
	RowBytes   int64  `json:"row_bytes"`
	Counter    int64  `json:"media_used_bytes"`
	Recomputed int64  `json:"recomputed_media_used_bytes"`
	Delta      int64  `json:"delta_bytes"`
}

// Report is the read-only reconciliation output: a JSON-able document plus a
// human table.
type Report struct {
	Bucket      string              `json:"bucket"`
	Prefix      string              `json:"prefix"`
	GeneratedAt time.Time           `json:"generated_at"`
	ReadOnly    bool                `json:"read_only"`
	Method      string              `json:"quota_method"`
	Accounts    []AccountSummary    `json:"accounts"`
	Totals      Totals              `json:"totals"`
	ByDecision  map[string]Counts   `json:"by_decision"`
	Groups      []*Group            `json:"groups"`
	Unparsed    []Object            `json:"unparsed_objects"`
	Warnings    []string            `json:"warnings,omitempty"`
	Decisions   map[string]Decision `json:"decisions,omitempty"`
}

// Totals are the bucket-wide figures the report leads with.
type Totals struct {
	Objects          int   `json:"objects"`
	Bytes            int64 `json:"bytes"`
	Groups           int   `json:"groups"`
	RowedGroups      int   `json:"rowed_groups"`
	RowlessGroups    int   `json:"rowless_groups"`
	RowlessObjects   int   `json:"rowless_objects"`
	RowlessBytes     int64 `json:"rowless_bytes"`
	DeletableObjects int   `json:"deletable_objects"`
	DeletableBytes   int64 `json:"deletable_bytes"`
}

// QuotaMethod is the sentence that must accompany any counter change: it names
// the source of truth and the arithmetic, so the number can be reproduced
// instead of trusted.
const QuotaMethod = "media_used_bytes := min(SUM(size) of every object under u/<account>/ listed from the media bucket at recompute time, 1 GiB cap). Never old-size per deleted object, and never SUM(media.size) — rows written before #545 record only the base object and omit the sized variants."

// BuildReport measures the store, the rows and the references into one document.
func BuildReport(bucket, prefix string, objects []Object, rows []Row, groups []*Group, unparsed []Object, accounts []Account, decisions map[string]Decision) *Report {
	r := &Report{
		Bucket:      bucket,
		Prefix:      prefix,
		GeneratedAt: time.Now().UTC(),
		ReadOnly:    true,
		Method:      QuotaMethod,
		ByDecision:  map[string]Counts{},
		Groups:      groups,
		Unparsed:    unparsed,
		Decisions:   decisions,
	}

	storeByAccount := map[string]int64{}
	objectsByAccount := map[string]int{}
	for _, o := range objects {
		a := AccountFromKey(o.Key)
		if a == "" {
			continue
		}
		storeByAccount[a] += o.Size
		objectsByAccount[a]++
	}
	rowBytes := map[string]int64{}
	rowCount := map[string]int{}
	for _, row := range rows {
		rowBytes[row.AccountID] += row.Size
		rowCount[row.AccountID]++
	}

	for _, g := range groups {
		Classify(g, decisions)
		if g.Row != nil {
			g.RowDelta = g.Bytes - g.Row.Size
		}
		c := r.ByDecision[g.Decision]
		c.Groups++
		c.Objects += len(g.Objects)
		c.Bytes += g.Bytes
		r.ByDecision[g.Decision] = c

		r.Totals.Groups++
		r.Totals.Objects += len(g.Objects)
		r.Totals.Bytes += g.Bytes
		if g.Row != nil {
			r.Totals.RowedGroups++
		} else {
			r.Totals.RowlessGroups++
			r.Totals.RowlessObjects += len(g.Objects)
			r.Totals.RowlessBytes += g.Bytes
		}
		if g.Decision == DecisionDeletable {
			r.Totals.DeletableObjects += len(g.Objects)
			r.Totals.DeletableBytes += g.Bytes
		}
		if g.Warning != "" {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s/%s: %s", g.AccountID, g.Base, g.Warning))
		}
	}
	// Unparsed objects are not groups, but they are still bytes in the bucket.
	for _, o := range unparsed {
		r.Totals.Objects++
		r.Totals.Bytes += o.Size
	}

	seen := map[string]bool{}
	for _, a := range accounts {
		seen[a.ID] = true
		store := storeByAccount[a.ID]
		rec := store
		if rec > MaxWorkspaceBytes {
			rec = MaxWorkspaceBytes
		}
		r.Accounts = append(r.Accounts, AccountSummary{
			AccountID:  a.ID,
			Name:       a.Name,
			Objects:    objectsByAccount[a.ID],
			StoreBytes: store,
			RowCount:   rowCount[a.ID],
			RowBytes:   rowBytes[a.ID],
			Counter:    a.Counter,
			Recomputed: rec,
			Delta:      rec - a.Counter,
		})
	}
	// An account segment in the bucket with no accounts row is a real finding:
	// those bytes cannot be attributed to a quota at all.
	for acc := range storeByAccount {
		if !seen[acc] {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s: objects in the bucket but no accounts row (cannot be charged to a quota)", acc))
			r.Accounts = append(r.Accounts, AccountSummary{
				AccountID:  acc,
				Name:       "(no accounts row)",
				Objects:    objectsByAccount[acc],
				StoreBytes: storeByAccount[acc],
			})
		}
	}
	sort.Slice(r.Accounts, func(i, j int) bool { return r.Accounts[i].AccountID < r.Accounts[j].AccountID })
	sort.Slice(r.Warnings, func(i, j int) bool { return r.Warnings[i] < r.Warnings[j] })
	return r
}

// Render writes the human report. The per-group lines always carry the reference
// verdict and the classification, so a group cannot be read without them.
func (r *Report) Render(w io.Writer) {
	mode := "READ-ONLY"
	if !r.ReadOnly {
		mode = "APPLY"
	}
	fmt.Fprintf(w, "media reconcile — bucket=%s prefix=%s mode=%s generated=%s\n", r.Bucket, r.Prefix, mode, r.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "objects=%d bytes=%d groups=%d rowed=%d rowless=%d (objects=%d bytes=%d)\n\n",
		r.Totals.Objects, r.Totals.Bytes, r.Totals.Groups, r.Totals.RowedGroups,
		r.Totals.RowlessGroups, r.Totals.RowlessObjects, r.Totals.RowlessBytes)

	fmt.Fprintln(w, "ACCOUNTS (store = every object under u/<account>/; rows = media rows live+trashed; counter = accounts.media_used_bytes)")
	for _, a := range r.Accounts {
		fmt.Fprintf(w, "  %s  %-24s objects=%d store=%d rows=%d row_bytes=%d counter=%d recomputed=%d delta=%+d\n",
			a.AccountID, a.Name, a.Objects, a.StoreBytes, a.RowCount, a.RowBytes, a.Counter, a.Recomputed, a.Delta)
	}
	fmt.Fprintf(w, "\n  quota method: %s\n\n", r.Method)

	fmt.Fprintln(w, "GROUPS")
	for _, g := range r.Groups {
		row := "no row"
		if g.Row != nil {
			state := "live"
			if g.Row.DeletedAt != nil {
				state = "trashed"
			}
			row = fmt.Sprintf("row %s (%s, size=%d, delta=%+d)", g.Row.ID, state, g.Row.Size, g.RowDelta)
		}
		refs := "refs: none"
		if len(g.Refs) > 0 {
			var parts []string
			for _, h := range g.Refs {
				parts = append(parts, h.Surface+":"+h.RecordID)
			}
			sort.Strings(parts)
			refs = "refs: " + strings.Join(parts, ", ")
		}
		fmt.Fprintf(w, "  %s/%s  %s  bytes=%d objects=%d variants=%s\n    %s; %s\n    => %s: %s\n",
			g.AccountID, g.Base, g.LastSeen.Format("2006-01-02T15:04:05Z"), g.Bytes, len(g.Objects),
			strings.Join(g.Variants, "+"), row, refs, g.Decision, g.Reason)
		if g.Warning != "" {
			fmt.Fprintf(w, "    !! %s\n", g.Warning)
		}
		for _, o := range g.Objects {
			pk, _ := ParseKey(o.Key)
			fmt.Fprintf(w, "      %-10s %10d B  %s\n", pk.Variant, o.Size, o.Key)
		}
	}
	if len(r.Unparsed) > 0 {
		fmt.Fprintln(w, "\nUNPARSED OBJECTS (counted in store bytes, not grouped)")
		for _, o := range r.Unparsed {
			fmt.Fprintf(w, "  %10d B  %s\n", o.Size, o.Key)
		}
	}
	fmt.Fprintln(w, "\nCLASSIFIED")
	var names []string
	for k := range r.ByDecision {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		c := r.ByDecision[n]
		fmt.Fprintf(w, "  %-10s groups=%d objects=%d bytes=%d\n", n, c.Groups, c.Objects, c.Bytes)
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, "\nWARNINGS")
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  - %s\n", warn)
		}
	}
}
