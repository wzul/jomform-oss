package mcpsrv

// The `purge_media` DESCRIPTION must not promise a reply field the reply does not
// carry.
//
// WHY THIS FILE EXISTS (card t_918897e1). The registered description read, on
// this card's base (main 5e0e3786):
//
//	Jawapan membawa 'edge' (purged | not_configured | no_urls | out_of_zone |
//	failed) dan 'edge_urls'; bila tiada kredensial CDN ia menyebut
//	'edge_max_age_s' …
//
// i.e. `edge_urls` is announced for EVERY reply. It is not.
// `cdnpurge.Result.Fields()` (api/internal/cdnpurge/purge.go) publishes it only
// when at least one URL actually reached the CDN:
//
//	f := map[string]any{"edge": r.Status}
//	if len(r.URLs) > 0 { f["edge_urls"] = r.URLs }
//
// so `not_configured`, `no_urls` and `out_of_zone` — the "no CDN credential /
// nothing inside the zone" modes this deployment runs in by DEFAULT, not as an
// edge case — come back with NO `edge_urls` key at all (measured on prod
// v1.21.228: `{"edge":"not_configured","edge_max_age_s":300,"status":"purged"}`).
// A reader (human or model) told to expect the field then reads its absence as a
// dropped field or a broken purge.
//
// That omission is deliberate: `Fields()` publishes what was sent, and a status
// that sent nothing has nothing to list. So the fix is PROSE, and the prose is
// the half this guard is for.
//
// WHAT IT CHECKS (no database, no network — it always runs):
//
//  1. The behaviour, so the prose cannot drift away from it silently: a Result
//     that holds no URLs renders NO `edge_urls` key for every status that can
//     produce one (not_configured / no_urls / out_of_zone / failed), with a
//     positive control that a purge which did send URLs still lists them.
//  2. The prose: each mention of `edge_urls` in the registered description must
//     carry a CONDITIONAL or NEGATED qualifier next to it ("hanya", "apabila",
//     "bila", "jika", "tiada", "tidak"), searched within its own semicolon clause
//     only. The pre-fix mention was a bare `dan 'edge_urls'` whose clause ended
//     at the next `;` — unconditional, which is exactly the defect. The search is
//     bounded to the clause on purpose: the neighbouring `bila tiada kredensial
//     CDN` qualifies `edge_max_age_s`, and reading it as a qualifier for
//     `edge_urls` is how an unbounded check would pass the wrong prose.
//  3. The description still names the three no-URL statuses, so a reader is told
//     which replies omit the field instead of only that some might.
//
// WHAT IT DOES NOT COVER: it reads the registered description over the real MCP
// protocol, not the rendered /docs/mcp card. That page is kept byte-identical to
// this string by docs_mcp_page_description_test.go, and that it renders at all is
// `npm run build` plus a browser read of the page.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/cdnpurge"
)

// TestPurgeReplyOmitsEdgeURLsWhenNoURLWasSent pins the behaviour the prose has to
// describe: `edge_urls` appears only when a URL was actually sent to the CDN.
func TestPurgeReplyOmitsEdgeURLsWhenNoURLWasSent(t *testing.T) {
	// Every status PurgeObjectURLs can return with an EMPTY URL list: no
	// credential (nothing is sent at all), no URLs to send, nothing inside the
	// zone, and a failed purge whose request carried nothing.
	for _, status := range []string{
		cdnpurge.StatusNotConfigured,
		cdnpurge.StatusNoURLs,
		cdnpurge.StatusOutOfZone,
		cdnpurge.StatusFailed,
	} {
		t.Run(status, func(t *testing.T) {
			res := cdnpurge.Result{Status: status}
			f := res.Fields()
			if _, has := f["edge_urls"]; has {
				t.Errorf("status %q with no URL sent carries edge_urls (%v). Fields() publishes that key "+
					"only when len(URLs) > 0; a reply that names no URL must not advertise a list of "+
					"them, and the tool description must say so.", status, f["edge_urls"])
			}
			if got := f["edge"]; got != status {
				t.Errorf("Fields()[edge] = %v, want %q — every reply names its edge outcome", got, status)
			}
		})
	}

	// Positive control: the same rendering DOES carry the list when URLs were
	// sent, so the check above cannot pass by Fields() never publishing it.
	urls := []string{"https://jomform.com/media/u/acct/x.png", "https://jomform.com/media/u/acct/x-thumbnail.png"}
	f := cdnpurge.Result{Status: cdnpurge.StatusPurged, URLs: urls}.Fields()
	got, ok := f["edge_urls"].([]string)
	if !ok || len(got) != len(urls) || got[0] != urls[0] || got[1] != urls[1] {
		t.Fatalf("a purge that sent URLs renders edge_urls = %v, want %v — without this arm the "+
			"omission check above would pass even if Fields() dropped the field for every status", f["edge_urls"], urls)
	}
}

// TestPurgeMediaDescriptionQualifiesEdgeURLs pins the prose: the description may
// announce `edge_urls` only conditionally, and must name the statuses that omit
// it.
func TestPurgeMediaDescriptionQualifiesEdgeURLs(t *testing.T) {
	desc, ok := registeredDescriptions(t)["purge_media"]
	if !ok {
		t.Fatal("purge_media is not registered over tools/list — this guard has nothing to judge, " +
			"so it must fail rather than pass by absence")
	}
	if strings.TrimSpace(desc) == "" {
		t.Fatal("purge_media is registered with an EMPTY description")
	}

	mentions, unqualified := edgeURLsClaimClauses(desc)
	if mentions == 0 {
		t.Fatalf("the purge_media description never names 'edge_urls': it must state WHEN the reply "+
			"carries the list (only when a URL actually reached the CDN) and when it does not.\n"+
			"    description: %s", desc)
	}
	for _, clause := range unqualified {
		t.Errorf("this part of the purge_media description promises 'edge_urls' without qualifying it:\n"+
			"    %q\n"+
			"A purge that sends no URL (not_configured, no_urls, out_of_zone) returns NO 'edge_urls' "+
			"key at all — cdnpurge.Result.Fields() publishes it only when len(URLs) > 0 (see "+
			"TestPurgeReplyOmitsEdgeURLsWhenNoURLWasSent). Say so in the clause that names the field, "+
			"e.g. \"'edge_urls' only when the CDN accepted at least one purge request\". "+
			"web/src/app/docs/mcp/page.tsx must carry the same corrected sentence — "+
			"docs_mcp_page_description_test.go compares the two.", clause)
	}

	// The reader must be told WHICH replies omit it, not merely that some do.
	for _, status := range []string{"not_configured", "no_urls", "out_of_zone"} {
		if !strings.Contains(desc, status) {
			t.Errorf("the purge_media description does not name the %q status, so a reader cannot tell "+
				"which replies omit 'edge_urls'.\n    description: %s", status, desc)
		}
	}
}

// edgeURLsClaimClauses returns how many times the description mentions
// 'edge_urls', plus the context of every mention that carries no conditional
// qualifier next to it.
//
// The search for a qualifier is bounded to the mention's own semicolon clause and
// to a short window on either side of it, because the description's neighbouring
// clause ("bila tiada kredensial CDN ia menyebut 'edge_max_age_s'") qualifies a
// DIFFERENT field: letting it satisfy the check would pass exactly the sentence
// this guard exists to reject.
func edgeURLsClaimClauses(desc string) (mentions int, unqualified []string) {
	const token = "'edge_urls'"
	const window = 70 // characters either side of the mention

	r := []rune(desc)
	tok := []rune(token)
	for i := 0; i < len(r); {
		at := indexRunes(r, tok, i)
		if at < 0 {
			break
		}
		mentions++

		// Semicolon clause boundaries around this mention.
		left := 0
		for k := at - 1; k >= 0; k-- {
			if r[k] == ';' {
				left = k + 1
				break
			}
		}
		right := len(r)
		for k := at + len(tok); k < len(r); k++ {
			if r[k] == ';' {
				right = k
				break
			}
		}
		// A short window on each side, kept inside that clause.
		from := at - window
		if from < left {
			from = left
		}
		to := at + len(tok) + window
		if to > right {
			to = right
		}
		context := string(r[from:at]) + token + string(r[at+len(tok):to])
		if !hasConditionalMarker(context) {
			unqualified = append(unqualified, strings.TrimSpace(context))
		}
		i = at + len(tok)
	}
	return mentions, unqualified
}

// indexRunes returns the rune index of sub in r at or after from, or -1.
func indexRunes(r, sub []rune, from int) int {
	for i := from; i+len(sub) <= len(r); i++ {
		match := true
		for k := range sub {
			if r[i+k] != sub[k] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// hasConditionalMarker reports whether a clause qualifies what it says, rather
// than promising it flatly. Deliberately language-level: the point is that SOME
// qualifier is present next to the mention, not which wording was chosen.
func hasConditionalMarker(clause string) bool {
	lower := strings.ToLower(clause)
	for _, marker := range []string{"only", "when", "if ", "unless", "not ", "no ", "empty", "absent", "omit"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
