package dbstore

// Issue #102 — migration / scan-drift guard.
//
// AGENTS.md warns that a migration which adds columns without updating the
// matching `*Select` constant AND every Scan call site (ByID, ByOwner, ByEmail,
// ...) causes silent column-aligned scan regressions (e.g. 401 storms on
// accounts). This file is the automated guard:
//
//  1. TestSelectScanColumnAlignment — parses the dbstore package source and
//     asserts that every `*Select` constant's column count equals the number
//     of exported fields on the struct it scans into, and that at least one
//     Scan call site actually scans that many destinations. A migration that
//     adds a column without updating the select, or a select updated without
//     updating a scan site, fails the build.
//
//  2. TestMigrationColumnsAppearInSelect — parses every migration *.up.sql and
//     asserts that any `ALTER TABLE ... ADD COLUMN` on a table that has a
//     `*Select` constant is reflected in that constant (unless the column is
//     deliberately read via a dedicated accessor query — see the allowlist).
//     This is the convention-enforcement check: a migration that adds a column
//     without updating the corresponding select constant is caught in the same
//     PR.
//
// Both are pure static checks — they run in CI with no database required.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// selectConsts maps a *Select constant name to the struct it scans into.
// Keep this in sync with the store files; the column/field counts themselves
// are derived from source so drift is still caught.
var selectConsts = map[string]string{
	"accountSelect":   "Account",
	"formSelect":      "Form",
	"productSelect":   "Product",
	"saleSelect":      "Sale",
	"workspaceSelect": "Workspace",
	"subSelect":       "Subscription",
	// The trash read is a SEPARATE constant from formSelect (its filter is the
	// opposite one), and it scans into the same Form struct, so it must satisfy
	// the same column/field alignment. Registering it means the trash read
	// cannot drift away from Form either — the pair moves together.
	"trashedFormSelect": "Form",
	// The workspace trash read is the same shape of pair: a SEPARATE constant
	// with the opposite filter, scanning into the same Workspace struct, so it
	// must satisfy the same column/field alignment. Registering it means
	// Workspace.DeletedAt cannot drift away from the trash query either.
	"trashedWorkspaceSelect": "Workspace",
	// Same reasoning for the product bin: trashedProductSelect is a separate
	// constant (deleted_at IS NOT NULL) scanning into Product, so registering it
	// keeps the bin's read aligned with the struct too.
	"trashedProductSelect": "Product",
	// account_domains had no registered select, so a migration adding a column
	// to it could land without the matching struct field or Scan destination —
	// exactly how the nullable auto_cname/tls_* columns came to be scanned into
	// plain strings and broke every domain read. Registering it puts the table
	// under the same column-alignment guard as the rest.
	"domainSelect": "AccountDomain", // Registered by card t_dd606310: these column-list constants were live but
	// UNREGISTERED, so nothing checked their column list against the struct they
	// scan into. The count check could never see them (it only kept names already
	// in this map), so adding one landed GREEN.
	"affiliateSelect":      "Affiliate",
	"feedbackThreadSelect": "FeedbackThread",
	"submissionSelect":     "Submission",
	"suppressionSelect":    "SuppressedEmail",
	"variationSelect":      "ProductVariation",
	// Shipping zones + rates (the managed rate source). Each constant is a FLAT
	// row read — the guard aligns a constant's column count with the EXPORTED
	// FIELD count of the struct it scans into, so the parent/child assembly (a
	// set holding its zones, a zone holding its rates) happens in Go rather than
	// in nested structs.
	"zoneSetSelect": "shippingZoneSetRow",
	"zoneSelect":    "shippingZoneRow",
	"rateSelect":    "shippingRateRow",
	// Courier connections (phase 2). Registered for the same reason as the zone
	// constants: a migration that adds a column to shipping_courier_accounts
	// cannot land without the select and its scan moving with it.
	"courierAccountSelect": "shippingCourierRow",
}

// selectTables maps a *Select constant name to the table it selects from.
var selectTables = map[string]string{
	"accountSelect":          "accounts",
	"formSelect":             "forms",
	"productSelect":          "products",
	"saleSelect":             "sales",
	"workspaceSelect":        "workspaces",
	"subSelect":              "subscriptions",
	"domainSelect":           "account_domains",
	"trashedFormSelect":      "forms",
	"trashedWorkspaceSelect": "workspaces",
	"trashedProductSelect":   "products",
	// Registered so BOTH registries agree about every column-list constant
	// (card t_3951b4d1): a constant that is in `selectConsts` but not here is
	// invisible to the migration guard, and nothing used to notice the missing row.
	// `suppressed_emails` matters most — migration 000061 adds `note` and
	// `created_by` to it, so its constants have to be checked.
	"affiliateSelect":       "affiliates",
	"variationSelect":       "product_variations",
	"submissionSelect":      "submissions",
	"suppressionSelect":     "suppressed_emails",
	"feedbackThreadSelect":  "feedback_threads",
	"feedbackMessageSelect": "feedback_messages",
	// Shipping zones + rates (registered so BOTH registries agree about every
	// column-list constant).
	"zoneSetSelect": "shipping_zone_sets",
	"zoneSelect":    "shipping_zones",
	"rateSelect":    "shipping_rates",
	// Courier connections (phase 2).
	"courierAccountSelect": "shipping_courier_accounts",
}

// selectScanFuncs maps a *Select constant to the receiver-qualified
// function(s) that perform the full-struct Scan of that select's columns.
// These are the call sites that MUST stay column-aligned with the select
// constant. A migration that adds a column to the select (and struct) without
// updating every one of these scan functions is caught by
// TestSelectScanColumnAlignment. Format: "ReceiverType.Method".
var selectScanFuncs = map[string][]string{
	"accountSelect":     {"AccountStore.ByID", "AccountStore.ByOwner"},
	"formSelect":        {"FormStore.scanOne"},
	"productSelect":     {"ProductStore.scanProductFull"},
	"saleSelect":        {"scanSale"},
	"workspaceSelect":   {"scanWorkspace"},
	"subSelect":         {"scanSub"},
	"domainSelect":      {"DomainStore.GetDomain", "DomainStore.ListDomains"},
	"trashedFormSelect": {"FormStore.scanTrashed"},
	// The workspace trash read is the same shape of pair (a separate constant,
	// opposite filter, same struct), so it gets its own scan function.
	"trashedWorkspaceSelect": {"scanTrashedWorkspace"},
	// The product bin selects the SAME column list as productSelect (only the
	// WHERE differs), so it shares that scan function. Registering it here is
	// what keeps the two constants from drifting apart on the column list.
	"trashedProductSelect": {"ProductStore.scanProductFull"},
	// Registered by card t_dd606310 — the scan sites these constants feed.
	"affiliateSelect":      {"scanAffiliate"},
	"feedbackThreadSelect": {"scanFeedbackThread"},
	"submissionSelect":     {"scanSubmission"},
	"suppressionSelect":    {"scanSuppressedEmail"},
	"variationSelect":      {"scanVariation"},
	// Shipping zones + rates: three flat row scans.
	"zoneSetSelect": {"scanShippingZoneSetRow"},
	"zoneSelect":    {"scanShippingZoneRow"},
	"rateSelect":    {"scanShippingRateRow"},
	// Courier connections (phase 2).
	"courierAccountSelect": {"scanShippingCourierRow"},
}

// bareColumnConsts maps a constant holding a BARE column list — no `SELECT ...
// FROM`, so selectColumnCount cannot parse it — to the struct it must stay
// aligned with. These are registered separately because nothing else covers
// them: an unregistered list drifts silently, and the drift only shows up as a
// runtime "number of field descriptions must equal number of destinations" the
// first time the code path that reads it runs.
//
// redirectFormColumns is the case that motivated this: it is shared by the two
// rename-301 lookups (RedirectedFormByAccountSlug / ...WorkspaceSlug), each of
// which scans it with FormStore.scanOne — the same full-struct scan every other
// form read uses. It was written before DeletedAt was selectable and fell one
// column behind Form, so both redirect lookups failed to scan at all.
var bareColumnConsts = map[string]string{
	"redirectFormColumns": "Form", // Registered by card t_dd606310. Both are column lists WITHOUT `SELECT ... FROM`
	// in the constant itself (mediaSelect is the list only; feedbackMessageSelect
	// begins mid-statement with `m.id`), so selectColumnCount cannot parse them — the
	// bare path is the one that can. mediaSelect's list is qualified (`m.id`, `m.key`),
	// which commaListCount counts exactly.
	"mediaSelect":           "Media",
	"feedbackMessageSelect": "FeedbackMessage", "chatProposalSelect": "ChatWriteProposal",
}

// bareColumnScanFuncs maps a bare-columns constant to the function that scans
// it, so the same alignment check applies.
var bareColumnScanFuncs = map[string][]string{
	"redirectFormColumns":   {"FormStore.scanOne"},
	"chatProposalSelect":    {"scanChatProposal"},
	"mediaSelect":           {"scanMedia"},
	"feedbackMessageSelect": {"FeedbackThreadStore.Messages"},
}

// commaListCount counts the TOP-LEVEL items in a comma-separated column list.
//
// It used to be a plain comma count, justified by "no parens, so a plain comma count
// is exact". That assumption is false for a bare list that contains a function call:
// feedbackMessageSelect carries `COALESCE(m.author_user_id::text, ”)`, whose inner
// comma made the count 9 for a 7-column list — a FALSE DRIFT report against a list
// that is correct. Depth-aware counting is exact for both shapes, which is why this
// now mirrors selectColumnCount's loop instead of trusting the assumption (card
// t_dd606310: the check that misreads is the same class as the check that cannot see).
func commaListCount(list string) int {
	depth := 0
	count := 1
	for _, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

// columnRenames maps historical column names (from older migrations) to their
// current name after the Space→Workspace rename (issue #431, migration 000045).
// Older migrations that ADD COLUMN space_id are checked against the renamed
// workspace_id in the current select constant.
var columnRenames = map[string]string{
	"space_id":         "workspace_id",
	"default_space_id": "default_workspace_id",
}

// intentionallyUnselected lists columns that are deliberately NOT part of the
// main *Select constant because they are read via a dedicated accessor query
// (e.g. Tracking, SigningKeys, CFToken, ChipOverrideFor, WorkspaceIDByAccount).
// A migration adding one of these must NOT be flagged. Keyed by table name.
var intentionallyUnselected = map[string]map[string]bool{
	"accounts": {
		"tracking_head":     true, // AccountStore.Tracking
		"wh_sign_pub":       true, // AccountStore.SigningKeys
		"wh_sign_priv":      true, // AccountStore.SigningKeys
		"cf_token_enc":      true, // AccountStore.CFToken
		"workspace_id":      true, // WorkspaceStore.WorkspaceIDByAccount
		"slug_changed_from": true, // legacy 301 source, not scanned
		// Issue #545: the storage-quota counter is read and maintained by
		// MediaStore.UsedBytes / ReserveQuota / SettleQuotaBytes / ReleaseQuota —
		// dedicated accessors, deliberately not part of accountSelect.
		"media_used_bytes": true,
	},
	"forms": {
		"workspace_id":          true, // denormalised, WorkspaceStore.WorkspaceIDByAccount
		"tracking_head":         true, // FormStore.SetTracking / read via dedicated query
		"chip_brand_id":         true, // FormStore.ChipOverrideFor
		"chip_secret_key_enc":   true, // FormStore.ChipOverrideFor
		"chip_email_fallback":   true, // FormStore.ChipOverrideFor
		"chip_method_whitelist": true, // FormStore.ChipOverrideFor
		"access_password":       true, // FormStore.AccessPassword (dedicated accessor, issue #176)
		// Card t_135c3c72: the per-form processing-fee override is read by
		// FormStore.FeeOverrideFor and resolved with the workspace's rates by
		// PaymentFeeStore.ForForm — never as part of the form row, because the
		// checkout asks "is the fee active for THIS form?" and not "what is the
		// form's raw override".
		"fee_override": true,
	},
	"products": {
		"workspace_id": true, // denormalised
	},
	"sales": {
		"workspace_id": true, // denormalised
		"fields":       true, // FormStore.SaleFields (dedicated accessor, issue #179)
	},
	"workspaces": {
		"created_at": true, // not scanned by workspaceSelect
	},
	"users": {
		"default_workspace_id": true, // UserStore.DefaultWorkspace (dedicated accessor, issue #164)
	},
}

// packageDir returns the directory containing this test file.
func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// selectRegionKeywordRe finds the FROM that closes a `SELECT ... FROM` constant,
// case-insensitively, as a WORD. This is the ONE definition of where the SELECT
// list ends — selectColumnCount (the column/field alignment check) and
// migrationColumnsAppearInSelect (the migration check) both go through
// selectRegion, so the two cannot end up cutting the constant in different
// places.
//
// The word boundary is load-bearing and was MEASURED: an unbounded
// case-insensitive match finds a `from` INSIDE productSelect's last-but-one
// column, `COALESCE(schedule_mode, 'from_payment')`, so selectRegion returns a
// region that stops there — `productSelect` counts 16 columns instead of 17 and
// the alignment check then fails on a HEALTHY tree ("productSelect selects 16
// columns but Product has 17 exported fields"). `_` is a word character, so there
// is no boundary between `from` and `_payment`; that is what makes the bounded
// form correct.
var selectRegionKeywordRe = regexp.MustCompile(`(?i)\bfrom\b`)

// selectRegion returns the SELECT list of a `SELECT ... FROM` constant: the text
// after the word SELECT up to (but not including) the FROM that terminates it,
// and whether the constant HAS that shape at all. A bare column list (no SELECT,
// no FROM) returns ok=false rather than an empty region — the distinction the
// migration guard turns on, because an empty region would fail every column of a
// bare list for the wrong reason.
func selectRegion(sel string) (string, bool) {
	m := regexp.MustCompile(`(?i)\bselect\b`).FindStringIndex(sel)
	if m == nil {
		return "", false
	}
	rest := sel[m[1]:]
	f := selectRegionKeywordRe.FindStringIndex(rest)
	if f == nil {
		return "", false
	}
	return rest[:f[0]], true
}

// selectColumnCount counts the top-level columns in a `SELECT ... FROM`
// constant body. It is robust to nested parens (COALESCE(...), casts) and
// multi-line constants.
func selectColumnCount(sel string) int {
	body, ok := selectRegion(sel)
	if !ok {
		return -1
	}
	depth := 0
	count := 1
	for _, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

// structFieldCount counts the exported fields on a struct type in the package.
func structFieldCount(t *testing.T, dir, typeName string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != typeName {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				n := 0
				for _, fld := range st.Fields.List {
					if len(fld.Names) > 0 && fld.Names[0].IsExported() {
						n++
					}
				}
				return n
			}
		}
	}
	t.Fatalf("struct %s not found in package", typeName)
	return -1
}

// scalarSelectConsts are SELECT statements that scan into a SCALAR, not a struct, so
// there is no field count to align against. They still look like column-list constants
// (they begin with SELECT), which is why they are named here rather than left to be
// rediscovered — and why the leg below fails if one of them stops being a scalar read.
var scalarSelectConsts = map[string]string{
	// SELECT a.id only, scanned into `var id string` (WorkspaceStore.LegacyUnscopedAccount).
	"legacyUnscopedAccountSQL": "WorkspaceStore.LegacyUnscopedAccount scans a single id",
}

// collectAllConsts parses the package source and returns EVERY string constant by
// name — registered or not. This is what makes an UNREGISTERED column-list constant
// visible (card t_dd606310): collectSelectConsts below only keeps a constant whose
// name is already a key in selectConsts/bareColumnConsts, so a brand-new
// `const fooSelect = \`SELECT ...\“ never reached the count and landed GREEN with
// the message at the end of TestSelectScanColumnAlignment promising a failure that
// could not come.
func collectAllConsts(t driftReporter, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					str, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					out[name.Name] = str
				}
			}
		}
	}
	return out
}

// looksLikeColumnList reports whether a constant VALUE is a column list — a SELECT
// statement, or a bare comma-separated list of names/qualified names. A string that
// is neither (a status set like `('paid','confirmed')`, a SQL fragment, a message)
// is not one, so it does not need registering.
//
// The bare-list test requires EVERY token to be a name or `alias.name`; a token like
// `('paid'` fails it, which is what keeps the check from flagging unrelated constants.
func looksLikeColumnList(v string) bool {
	trimmed := strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToUpper(trimmed), "SELECT ") {
		return true
	}
	parts := strings.Split(trimmed, ",")
	if len(parts) < 2 {
		return false
	}
	nameRe := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	for _, part := range parts {
		if !nameRe.MatchString(strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

// collectSelectConsts parses the package source and returns the value of each
// *Select constant (the raw string literal).
func collectSelectConsts(t driftReporter, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					inSelect := false
					if _, isSel := selectConsts[name.Name]; isSel {
						inSelect = true
					} else if _, isBare := bareColumnConsts[name.Name]; isBare {
						inSelect = true
					}
					if !inSelect {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconvUnquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", name.Name, err)
					}
					out[name.Name] = val
				}
			}
		}
	}
	return out
}

// scanDestinationsInFunc returns the destination counts of every `.Scan(...)`
// call in the named function. `qualified` is either "ReceiverType.Method" for a
// method or a bare name for a package-level function.
func scanDestinationsInFunc(t *testing.T, dir, qualified string) []int {
	t.Helper()
	parts := strings.SplitN(qualified, ".", 2)
	recvType, methodName := "", qualified
	if len(parts) == 2 {
		recvType, methodName = parts[0], parts[1]
	}
	var counts []int
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != methodName || fn.Body == nil {
				continue
			}
			// If a receiver type is expected, match it; otherwise the function
			// must be package-level (no receiver).
			if recvType != "" {
				if fn.Recv == nil {
					continue
				}
				recv := fn.Recv.List[0].Type
				var recvName string
				switch rt := recv.(type) {
				case *ast.StarExpr:
					if id, ok := rt.X.(*ast.Ident); ok {
						recvName = id.Name
					}
				case *ast.Ident:
					recvName = rt.Name
				}
				if recvName != recvType {
					continue
				}
			} else if fn.Recv != nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Scan" {
					counts = append(counts, len(call.Args))
				}
				return true
			})
		}
	}
	return counts
}

// TestSelectScanColumnAlignment asserts every *Select constant's column count
// equals the exported-field count of the struct it scans into, and that every
// registered scan function scans exactly that many destinations.
func TestSelectScanColumnAlignment(t *testing.T) {
	dir := packageDir()
	consts := collectSelectConsts(t, dir)
	if want := len(selectConsts) + len(bareColumnConsts); len(consts) != want {
		t.Fatalf("found %d REGISTERED column-list constants, want %d — a name in selectConsts or bareColumnConsts is no longer a parseable string const", len(consts), want)
	}

	// An UNREGISTERED column-list constant is the case the message above used to
	// promise and could never deliver: collectSelectConsts only keeps names already
	// in the registries, so a brand-new `fooSelect` was invisible to the count and
	// landed GREEN. Read EVERY const and require each column-list one to be
	// registered — this is what makes "register your new select constant" true.
	all := collectAllConsts(t, dir)
	var unregistered []string
	for name, val := range all {
		if _, isSel := selectConsts[name]; isSel {
			continue
		}
		if _, isBare := bareColumnConsts[name]; isBare {
			continue
		}
		if _, isScalar := scalarSelectConsts[name]; isScalar {
			continue
		}
		if looksLikeColumnList(val) {
			unregistered = append(unregistered, name)
		}
	}
	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Fatalf("%d column-list constant(s) are NOT registered, so nothing checks their column list against the struct they scan into: %v — add each to selectConsts (with its struct) or bareColumnConsts", len(unregistered), unregistered)
	}

	// The scalar exemption must not rot: a name listed there that no longer exists, or
	// that no longer looks like a column list, would silently widen the exemption.
	var staleScalar []string
	for name := range scalarSelectConsts {
		val, ok := all[name]
		if !ok || !looksLikeColumnList(val) {
			staleScalar = append(staleScalar, name)
		}
	}
	if len(staleScalar) > 0 {
		sort.Strings(staleScalar)
		t.Fatalf("scalarSelectConsts names %v, but they are gone or no longer look like a column list — remove the stale exemption so the constant is registered properly", staleScalar)
	}

	for name, structName := range selectConsts {
		sel, ok := consts[name]
		if !ok {
			t.Errorf("%s: select constant not found in source", name)
			continue
		}
		cols := selectColumnCount(sel)
		if cols < 0 {
			t.Errorf("%s: could not parse SELECT column list", name)
			continue
		}
		fields := structFieldCount(t, dir, structName)
		if cols != fields {
			t.Errorf("%s selects %d columns but %s has %d exported fields — column/struct drift (update the select AND all Scan call sites together)", name, cols, structName, fields)
		}
		// Every registered scan function must scan exactly `cols` destinations.
		funcs, ok := selectScanFuncs[name]
		if !ok {
			t.Errorf("%s: no scan functions registered in selectScanFuncs", name)
			continue
		}
		for _, fn := range funcs {
			counts := scanDestinationsInFunc(t, dir, fn)
			if len(counts) == 0 {
				t.Errorf("%s: scan function %s not found or has no Scan call — a migration likely added a column without updating the scan sites", name, fn)
				continue
			}
			for _, c := range counts {
				if c != cols {
					t.Errorf("%s used by %s: Scan has %d destinations but select has %d columns — column-aligned scan regression (update the select AND all scan sites together)", name, fn, c, cols)
				}
			}
		}
	}

	// Bare column lists (no SELECT ... FROM) get the same treatment. They are
	// invisible to selectColumnCount, so without this they drift unnoticed until
	// the read path runs and pgx refuses the scan.
	for name, structName := range bareColumnConsts {
		list, ok := consts[name]
		if !ok {
			t.Errorf("%s: constant not found in source", name)
			continue
		}
		cols := commaListCount(list)
		fields := structFieldCount(t, dir, structName)
		if cols != fields {
			t.Errorf("%s lists %d columns but %s has %d exported fields — column/struct drift (update the list AND all Scan call sites together)", name, cols, structName, fields)
		}
		funcs, ok := bareColumnScanFuncs[name]
		if !ok || len(funcs) == 0 {
			t.Errorf("%s: no scan functions registered in bareColumnScanFuncs", name)
			continue
		}
		for _, fn := range funcs {
			counts := scanDestinationsInFunc(t, dir, fn)
			if len(counts) == 0 {
				t.Errorf("%s: scan function %s not found or has no Scan call", name, fn)
				continue
			}
			for _, c := range counts {
				if c != cols {
					t.Errorf("%s used by %s: Scan has %d destinations but the list has %d columns — column-aligned scan regression (update the list AND all scan sites together)", name, fn, c, cols)
				}
			}
		}
	}
}

// TestMigrationColumnsAppearInSelect parses every migration *.up.sql and asserts
// that any ALTER TABLE ... ADD COLUMN on a table with a *Select constant is
// reflected in EVERY constant that reads that table (unless deliberately
// unselected).
//
// The body is in migrationColumnsAppearInSelect, parameterised on the source and
// migration directories, so the bite test below can run this same rule against a
// throwaway copy with one constant sabotaged. A second, hand-written copy of the
// check would be free to drift from the guard it claims to prove.
func TestMigrationColumnsAppearInSelect(t *testing.T) {
	dir := packageDir()
	migrationColumnsAppearInSelect(t, dir, filepath.Join(dir, "..", "db", "migrations"))
}

// migrationColumnsAppearInSelect is the guard's body: it reads `migDir`'s
// *.up.sql files and checks each added column against the SELECT LIST of every
// column-list constant registered for the table it belongs to, parsing those
// constants out of the Go source in `dir`.
func migrationColumnsAppearInSelect(t driftReporter, dir, migDir string) {
	t.Helper()
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	consts := collectSelectConsts(t, dir)

	var migFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			migFiles = append(migFiles, e.Name())
		}
	}
	sort.Strings(migFiles)

	for _, name := range migFiles {
		raw, err := os.ReadFile(filepath.Join(migDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		content := string(raw)
		lower := strings.ToLower(content)
		idx := 0
		for {
			pos := strings.Index(lower[idx:], "add column")
			if pos < 0 {
				break
			}
			pos += idx
			table := tableForAlter(content, pos)
			col := columnName(content, pos)
			idx = pos + len("add column")
			if table == "" || col == "" {
				continue
			}
			// Only enforce for tables that have a *Select constant.
			selNames, ok := selectConstsForTable(table)
			if !ok {
				continue
			}
			// Issue #431: the Space→Workspace rename (migration 000045) renamed
			// columns like space_id → workspace_id. Older migrations that ADD
			// COLUMN space_id must be checked against the current (renamed)
			// select constant, so translate the historical column name.
			if renamed, ok := columnRenames[col]; ok {
				col = renamed
			}
			// Skip columns deliberately read via dedicated accessor queries.
			if intentionallyUnselected[table][col] {
				continue
			}
			// EVERY constant on the table is checked, not one representative.
			// A table with two constants (a live read and a trash read) is the
			// case that used to be a coin flip — see selectConstsForTable.
			for _, selName := range selNames {
				sel, ok := consts[selName]
				if !ok {
					t.Errorf("%s: table %q has select constant %s but it was not parsed", name, table, selName)
					continue
				}
				// The check is scoped to the SELECT LIST, not the whole
				// constant. Matching the whole constant let a column satisfy
				// the guard by surviving anywhere in it — both workspace
				// constants carry `deleted_at` in their WHERE clause, so a
				// migration-added column removed from the column list was
				// invisible (measured: PASS 30/30 on the unfixed guard).
				list, ok := selectRegion(sel)
				if !ok {
					t.Errorf("%s: %s is not a `SELECT ... FROM` constant this guard can bound (table %q)", name, selName, table)
					continue
				}
				if !containsColumn(list, col) {
					t.Errorf("%s: adds column %q to table %q but %s does not select it — update the select constant AND all Scan call sites in the same PR", name, col, table, selName)
				}
			}
		}
	}
}

// selectConstsForTable returns EVERY *Select constant that selects from the
// named table, SORTED, and whether the table has any at all.
//
// A table can have more than one column-list constant: `workspaces` has
// workspaceSelect (live) and trashedWorkspaceSelect (the bin), `forms` has
// formSelect and trashedFormSelect, `products` has productSelect and
// trashedProductSelect. Returning ONE of them — which the map-range version of
// this did — made the guard a coin flip: Go randomises map iteration, so which
// constant got checked changed per run, and a real drift in the other one was
// invisible on most runs (measured on the merged tree: the `workspaces` drift
// failed 20 of 30 runs; the card measured 21 of 60).
//
// The caller must check ALL of them: a migration adds the column to the TABLE,
// so every constant reading that table is obliged to select it. This is also why
// the result is sorted rather than taken in map order — a failure message that
// names one constant must not depend on the run.
func selectConstsForTable(table string) ([]string, bool) {
	var names []string
	for name, tbl := range selectTables {
		if tbl == table {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, len(names) > 0
}

// containsColumn reports whether a SELECT list names `col` as a COLUMN, matched
// case-insensitively on word boundaries.
//
// Word boundaries, not `strings.Contains`, because a column name is a WORD: the
// bare-substring test reported a match for `deleted_at` inside a longer
// identifier such as `not_deleted_at`, and for `id` inside every `account_id`,
// `product_id` and `workspace_id` in the list. Those are false GREENs — the
// guard would pass while the column it exists to pin is absent. The boundary
// also makes the check independent of how a caller quotes the name: with or
// without backticks the `_`-surrounded form still matches, so `\b` (which treats
// `_` as a word character) is the right rule and `\bdeleted_at\b` does NOT match
// `deleted_at_archive`.
//
// The SELECT-REGION scoping happens in the caller (selectRegion), so this only
// has to be correct about what "names this column" means.
func containsColumn(list, col string) bool {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(col) + `\b`)
	return re.MatchString(list)
}

// tableForAlter finds the table name of the ALTER TABLE statement that
// contains the byte offset `pos`.
func tableForAlter(content string, pos int) string {
	head := content[:pos]
	last := strings.LastIndex(strings.ToLower(head), "alter table")
	if last < 0 {
		return ""
	}
	rest := strings.TrimSpace(head[last+len("alter table"):])
	end := strings.IndexAny(rest, " \t\n")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// columnName extracts the column name from an "ADD COLUMN <col> ..." at pos,
// skipping an optional "IF NOT EXISTS" clause.
func columnName(content string, pos int) string {
	rest := strings.TrimSpace(content[pos+len("add column"):])
	// Skip "IF NOT EXISTS" (case-insensitive).
	lower := strings.ToLower(rest)
	if strings.HasPrefix(lower, "if not exists") {
		rest = strings.TrimSpace(rest[len("if not exists"):])
	}
	end := strings.IndexAny(rest, " \t\n,")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// strconvUnquote unquotes a Go string literal (handles backticks and quotes).
func strconvUnquote(s string) (string, error) {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1], nil
	}
	return strconv.Unquote(s)
}

// ── The bite proof for TestMigrationColumnsAppearInSelect ─────────────────────
//
// A guard nobody has seen fail is indistinguishable from a guard that works, and
// this one HAD two ways of not failing. Both were reproduced by mutation on the
// merged tree (main at ca110813, workspace.go restored byte-identically after
// each probe, `go build ./...` asserted OK so the mutation is a compiling one —
// that is the whole reason the guard exists):
//
//  1. It matched the column name anywhere in the WHOLE constant, not in its
//     SELECT list. Both workspace constants carry `deleted_at` in their WHERE
//     clause, so removing it from the column LIST while it survived in the WHERE
//     still satisfied the check: 30 runs, 30 PASSES, no failure at all.
//  2. It checked ONE constant per table, chosen by ranging a Go map. `workspaces`
//     has two (workspaceSelect and trashedWorkspaceSelect), `forms` and
//     `products` likewise, and Go randomises map iteration, so a drift in the
//     constant it did not pick was invisible on most runs. Measured: dropping
//     `deleted_at` from trashedWorkspaceSelect — name gone from the constant
//     entirely, so defect 1 could not mask it — passed 20 of 30 runs (the card
//     measured 21 of 60). After the fix the same tree fails 30 of 30.
//
// This test pins both properties by running the guard's own body
// (migrationColumnsAppearInSelect) against a throwaway copy of the package with
// one constant sabotaged. It never touches the tree under test, and it does not
// re-implement the rule: if the shipped check is weakened again, these cases go
// RED. That was verified by reverting each half of the fix in turn — with the
// check unscoped again BOTH cases passed (proof stopped biting); with the lookup
// narrowed to one candidate again BOTH cases passed; with the fix in place each
// case fails on the FIRST run and names the constant that was actually broken.

// workspaceSelectPristine / trashedWorkspaceSelectPristine are the exact SELECT
// lines the sabotage rewrites, taken from the tree's own source so a rename of
// the constants fails this proof loudly instead of silently proving nothing.
const (
	workspaceSelectPristine        = "const workspaceSelect = `SELECT id, owner_user_id, name, slug, deleted_at FROM workspaces WHERE deleted_at IS NULL`"
	trashedWorkspaceSelectPristine = "const trashedWorkspaceSelect = `SELECT id, owner_user_id, name, slug, deleted_at\n	FROM workspaces WHERE deleted_at IS NOT NULL`"
)

// TestMigrationColumnsAppearInSelectBitesEveryConstantOnTheTable is the bite
// proof. It is deterministic by construction: it asserts the guard's FIRST run
// on the sabotaged copy fails, and repeats the run so that a map-order-dependent
// guard cannot pass by luck.
// TestSelectTablesCoversEveryColumnListConstant pins the guard's OWN registry
// against the OTHER one (card t_3951b4d1).
//
// `selectConsts` must name every column-list constant (the alignment test asserts
// the count), but nothing asserted that the same constant also has a row in
// `selectTables` — and `selectTables` is the registry the migration guard
// consults. So a constant registered in `selectConsts` yet missing from
// `selectTables` was invisible to the migration guard, and nothing went red for
// the missing row itself. Measured before this test: dropping `"formSelect":
// "forms",` left the whole package GREEN, and a same-count wrong name in
// `formSelect` then shipped green too.
//
// Both directions are asserted, so the two registries cannot drift apart.
func TestSelectTablesCoversEveryColumnListConstant(t *testing.T) {
	var sc, st []string
	for name := range selectConsts {
		sc = append(sc, name)
	}
	for name := range selectTables {
		st = append(st, name)
	}
	sort.Strings(sc)
	sort.Strings(st)

	var missing []string
	for _, name := range sc {
		if _, ok := selectTables[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these column-list constants are registered in selectConsts but have NO row in "+
			"selectTables: %v. The migration/select drift guard consults selectTables, so a "+
			"constant missing from it is INVISIBLE — a migration that adds a column to its table "+
			"can no longer be caught, and nothing goes red for the missing row. Add the row (the "+
			"table the constant selects FROM, read from its own SQL).", missing)
	}
	// The reverse direction: a selectTables row must name a constant that is
	// registered SOMEWHERE — `selectConsts` (full column lists) or
	// `bareColumnConsts` (the fragment constants). An unregistered name is a stale
	// row, and a stale row makes the migration guard check a constant nobody uses.
	var extra []string
	for _, name := range st {
		if _, ok := selectConsts[name]; ok {
			continue
		}
		if _, ok := bareColumnConsts[name]; ok {
			continue
		}
		extra = append(extra, name)
	}
	if len(extra) > 0 {
		t.Errorf("these names have a selectTables row but are registered in NEITHER selectConsts "+
			"nor bareColumnConsts: %v — either the constant was renamed or it was dropped, and a "+
			"stale row lets the migration guard check a constant nobody uses.", extra)
	}
	// The premise, so this cannot pass by the registries both being empty.
	if len(sc) < 10 {
		t.Fatalf("only %d constants in selectConsts — the registry has shrunk, so this check "+
			"is no longer proving what it exists for", len(sc))
	}
}

// TestContainsColumnBoundaryIsLoadBearing pins the WORD BOUNDARY in `containsColumn`
// (card t_3951b4d1).
//
// `containsColumn` matches `(?i)\b<col>\b`. That boundary is the whole rule for a
// column whose name is a proper prefix of another column on the same table — and
// this tree has a LIVE pair: migration 000004 adds `custom_domain_verified` to
// `accounts`, and `accountSelect` carries BOTH `custom_domain_verified` and
// `custom_domain_verified_at`. Weakening the boundary to a bare substring is
// silent: measured on a same-count mutation of `accountSelect` (the exact column
// replaced by a second `..._at`, so the alignment test cannot fire), the shipped
// bounded form FAILS correctly while an unbounded form stays GREEN.
//
// The bite harness runs the guard's own body against throwaway copies; this case
// lives here so the rule is pinned by a test that runs in the `test` gate.
func TestContainsColumnBoundaryIsLoadBearing(t *testing.T) {
	list := "SELECT id, custom_domain_verified, custom_domain_verified_at FROM accounts"
	if !containsColumn(list, "custom_domain_verified") {
		t.Fatalf("containsColumn no longer finds an exactly-selected column")
	}
	// The column is present only as a PREFIX of a longer identifier: that must NOT
	// count as selecting it.
	prefixOnly := "SELECT id, custom_domain_verified_at FROM accounts"
	if containsColumn(prefixOnly, "custom_domain_verified") {
		t.Errorf("containsColumn matched `custom_domain_verified` inside " +
			"`custom_domain_verified_at` — the word boundary is gone, so a migration-added " +
			"column can satisfy the guard while not being selected at all (the live pair on " +
			"accounts: custom_domain_verified vs custom_domain_verified_at)")
	}
	// The inverse: a real, underscore-surrounded match still passes, so the rule is
	// not merely strict.
	if !containsColumn("SELECT a.custom_domain_verified FROM accounts a", "custom_domain_verified") {
		t.Errorf("containsColumn missed a qualified column reference — the boundary is too " +
			"strict to be useful")
	}
	if !containsColumn("SELECT `custom_domain_verified` FROM accounts", "custom_domain_verified") {
		t.Errorf("containsColumn missed a backticked column name")
	}
}

func TestMigrationColumnsAppearInSelectBitesEveryConstantOnTheTable(t *testing.T) {
	dir := packageDir()
	migDir := filepath.Join(dir, "..", "db", "migrations")

	// The control: the UNMUTATED copy must pass, or every red below is
	// meaningless — a guard that fails on a healthy tree proves nothing about
	// its mutations.
	control := copyPackageForBite(t, dir, migDir)
	runBiteGuard(t, control.dir, control.migDir, false, "")

	// Repeat count: enough repetitions that a guard which still ranged a map
	// would be caught with overwhelming probability (the unfixed lookup picked
	// the wrong constant ~50% of the time, so 24 runs leave a ~6e-8 chance of
	// silent luck).
	const runs = 24

	cases := []struct {
		name     string
		pristine string
		mutated  string
		// names is the constant the failure message must name. It is the one
		// that was BROKEN, not merely one of the table's constants — the old
		// message named whichever constant the map happened to return.
		names string
	}{
		{
			// Defect 2: the name survives in the WHERE clause, so only a check
			// scoped to the SELECT list can see this.
			name:     "column removed from a constant's SELECT list but still in its WHERE",
			pristine: workspaceSelectPristine,
			mutated:  "const workspaceSelect = `SELECT id, owner_user_id, name, slug FROM workspaces WHERE deleted_at IS NULL`",
			names:    "workspaceSelect",
		},
		{
			// Defect 1: the column is gone from a SECOND constant on a table that
			// has two, which a single-representative lookup misses about half the
			// time.
			name:     "column dropped entirely from the trash constant of a two-constant table",
			pristine: trashedWorkspaceSelectPristine,
			mutated:  "const trashedWorkspaceSelect = `SELECT id, owner_user_id, name, slug\n	FROM workspaces WHERE deleted_at IS NOT NULL`",
			names:    "trashedWorkspaceSelect",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tree := copyPackageForBite(t, dir, migDir)
			sabotage(t, tree.dir, tc.pristine, tc.mutated)
			for i := 0; i < runs; i++ {
				runBiteGuard(t, tree.dir, tree.migDir, true, tc.names)
			}
			t.Logf("%d/%d runs RED, each naming %s", runs, runs, tc.names)
		})
	}
}

// biteTree is a throwaway package copy: the Go sources in dir and the migrations
// in migDir.
type biteTree struct {
	dir    string
	migDir string
}

// copyPackageForBite copies the package's non-test Go sources and the migration
// files into a t.TempDir(). The copy exists so the sabotage is applied to a
// throwaway tree: the guard under test can then be run in-process without the
// worktree ever being mutated, which is what makes this proof repeatable and
// safe to run on every gate.
func copyPackageForBite(t *testing.T, dir, migDir string) biteTree {
	t.Helper()
	root := t.TempDir()
	goDir := filepath.Join(root, "pkg")
	sqlDir := filepath.Join(root, "migrations")
	for _, d := range []string{goDir, sqlDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	copyFiles := func(from, to, suffix string, skipTests bool) int {
		entries, err := os.ReadDir(from)
		if err != nil {
			t.Fatalf("read %s: %v", from, err)
		}
		n := 0
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
				continue
			}
			if skipTests && strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(from, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			if err := os.WriteFile(filepath.Join(to, e.Name()), data, 0o644); err != nil {
				t.Fatalf("write %s: %v", e.Name(), err)
			}
			n++
		}
		return n
	}

	// A copy that silently came up short would make the guard pass vacuously,
	// so both counts are asserted rather than assumed.
	if n := copyFiles(dir, goDir, ".go", true); n < 20 {
		t.Fatalf("copied only %d non-test Go files out of %s — the copy is incomplete, so this proof would run the guard against a package that is not the tree's", n, dir)
	}
	if n := copyFiles(migDir, sqlDir, ".up.sql", false); n < 20 {
		t.Fatalf("copied only %d *.up.sql files out of %s — an empty migrations dir makes the guard pass while checking nothing", n, migDir)
	}
	return biteTree{dir: goDir, migDir: sqlDir}
}

// sabotage rewrites the single pristine line in the copied tree. A missing or
// ambiguous anchor is a FAILURE, not a skip: it means the source moved and this
// proof stopped testing what it claims to. It therefore has no "not found"
// return — a caller cannot accidentally treat an unanchored proof as a pass.
func sabotage(t *testing.T, dir, pristine, mutated string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(data)
		if !strings.Contains(src, pristine) {
			continue
		}
		if strings.Count(src, pristine) != 1 {
			t.Fatalf("%s: anchor %q matched %d times, want exactly 1", e.Name(), pristine, strings.Count(src, pristine))
		}
		if err := os.WriteFile(path, []byte(strings.Replace(src, pristine, mutated, 1)), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	t.Fatalf("could not find the constant to sabotage: no copied file contains\n  %s\n"+
		"the constant was renamed or reshaped, so this proof is measuring nothing — re-anchor it", pristine)
}

// driftReporter is the slice of *testing.T that the guard body uses. The bite
// proof has to read the guard's verdict and its messages WITHOUT failing the
// test that is running the proof, so the body is written against this interface:
// a real *testing.T satisfies it in production, and biteReporter satisfies it
// below. No hand-built *testing.T, and no sub-test whose failure would leak into
// the parent.
type driftReporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// biteReporter records what the guard body reports instead of failing a test.
// It is the whole instrumentation of the bite proof: nothing else about the
// guard is stubbed.
type biteReporter struct {
	msgs   []string
	failed bool
}

// biteFatal distinguishes the guard body's Fatalf from any other panic so a
// genuinely unexpected panic is re-raised rather than swallowed as a verdict.
type biteFatal struct{}

func (b *biteReporter) Helper() {}

func (b *biteReporter) Errorf(format string, args ...any) {
	b.failed = true
	b.msgs = append(b.msgs, fmt.Sprintf(format, args...))
}

func (b *biteReporter) Fatalf(format string, args ...any) {
	b.failed = true
	b.msgs = append(b.msgs, fmt.Sprintf(format, args...))
	panic(biteFatal{})
}

// run applies the guard body to a copy of the package and returns whether it
// failed, with every message it reported. The guard body is a pure read of the
// copy, so this is safe to repeat.
func (b *biteReporter) run(dir, migDir string) (bool, []string) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(biteFatal); !ok {
				panic(r)
			}
		}
	}()
	migrationColumnsAppearInSelect(b, dir, migDir)
	return b.failed, b.msgs
}

// runBiteGuard runs the guard's own body against a copy and asserts the verdict.
// wantRed distinguishes the control (must pass) from a mutation (must fail, and
// must name the constant that was broken).
func runBiteGuard(t *testing.T, dir, migDir string, wantRed bool, names string) {
	t.Helper()
	rec := &biteReporter{}
	failed, msgs := rec.run(dir, migDir)
	joined := "(no messages)"
	if len(msgs) > 0 {
		joined = "  " + strings.Join(msgs, "\n  ")
	}

	if !wantRed {
		if failed {
			t.Fatalf("guard FAILED on an unmutated copy of this package — the control must be green "+
				"or every red below is meaningless.\nmessages:\n%s", joined)
		}
		return
	}

	if !failed {
		t.Fatalf("guard PASSED on the sabotaged copy — it does not bite: "+
			"TestMigrationColumnsAppearInSelect still passes with the column removed from %s", names)
	}
	found := false
	for _, m := range msgs {
		if strings.Contains(m, names) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("guard failed but its message does not name %s — a red that blames another "+
			"constant is a red nobody can act on.\nmessages:\n%s", names, joined)
	}
}
