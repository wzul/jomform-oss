// Package mediareconcile reconciles the media OBJECT STORE against the `media`
// table: which stored objects have a row, which are referenced by application
// data without a row, and what an account's `media_used_bytes` counter should
// be if it were derived from the bytes actually occupying the bucket.
//
// It exists because the two representations of "storage used" can disagree:
//
//   - every MCP image upload before v1.21.231 stored base + thumbnail/medium/full
//     objects and reserved quota, but wrote NO media row, so the objects were
//     invisible to the library and impossible to trash or purge through the app;
//   - rows written before issue #545 recorded only the base object's size, so a
//     row can understate the bytes its own variant keys occupy;
//   - and `accounts.media_used_bytes` is maintained by increments, which cannot
//     be repaired by decrementing the same way (a purge subtracts the ROW's
//     size, not the group's bytes).
//
// The rule this package enforces is: the OBJECT STORE is the source of truth for
// bytes occupied, the `media` row is the source of truth for what the app can
// manage, and application data (avatar, form config/theme, product images,
// landing page, legal pages) is the source of truth for whether an object may be
// deleted at all. A group with no row is never deletable until every one of
// those reference surfaces has been checked: "no media row" does not mean
// "unreferenced" (a row-less object was measured as an account's live avatar).
package mediareconcile

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Decision values. DecisionRowed describes a group that ALREADY has a media row
// (live or trashed); the three others are the classifications a row-less group
// can carry.
const (
	// DecisionRowed: the base object key has a media row. Nothing to reconcile —
	// the app can list, trash and purge it.
	DecisionRowed = "rowed"
	// DecisionAdopt: row-less, unreferenced, and kept rather than deleted by
	// default — re-creating the row makes the bytes manageable again WITHOUT
	// charging them a second time (they are already in the bucket).
	DecisionAdopt = "adopt"
	// DecisionKeep: row-less but referenced by application data, so the bytes are
	// in use and must not be touched (e.g. the account's own avatar).
	DecisionKeep = "keep"
	// DecisionDeletable: row-less, unreferenced, and explicitly approved for
	// deletion by a decisions file. This is the ONLY classification that can
	// authorise a store delete, and it is refused if a reference appears.
	DecisionDeletable = "deletable"
)

// Variant names derived from the canonical object key.
const (
	VariantBase      = "base"
	VariantThumbnail = "thumbnail"
	VariantMedium    = "medium"
	VariantFull      = "full"
	VariantPreview   = "preview"
)

// Object is one object listed from the media bucket.
type Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag,omitempty"`
}

// Row is one `media` row, live or trashed.
type Row struct {
	ID        string     `json:"id"`
	AccountID string     `json:"account_id"`
	Key       string     `json:"key"`
	URL       string     `json:"url"`
	Mime      string     `json:"mime"`
	Size      int64      `json:"size"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// RefHit is one application record that names an object's base uuid.
type RefHit struct {
	Surface  string `json:"surface"`
	RecordID string `json:"record_id"`
}

// ParsedKey is the canonical shape of an uploaded object key:
// u/<account>/<uuid>[-thumbnail|-medium|-full|-preview].<ext>.
type ParsedKey struct {
	AccountID string
	Base      string
	Variant   string
	Ext       string
}

// keyPattern is the canonical upload key. The optional `u/` prefix keeps the
// parser usable on listings that strip the bucket prefix from the key.
var keyPattern = regexp.MustCompile(
	`^(?:u/)?([A-Za-z0-9_-]+)/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:-(thumbnail|medium|full|preview))?(\.[A-Za-z0-9]+)$`)

// ParseKey parses a canonical upload key. Anything else (a legacy nested key, a
// non-uuid filename, a path with extra segments) reports ok=false and is handled
// as an unparsed object: it still counts toward the account's stored bytes, but
// it cannot be grouped or reconciled.
func ParseKey(key string) (ParsedKey, bool) {
	m := keyPattern.FindStringSubmatch(key)
	if m == nil {
		return ParsedKey{}, false
	}
	variant := m[3]
	if variant == "" {
		variant = VariantBase
	}
	return ParsedKey{
		AccountID: m[1],
		Base:      m[2],
		Variant:   variant,
		Ext:       strings.ToLower(m[4]),
	}, true
}

// AccountFromKey returns the account segment of ANY key under the media prefix,
// including keys ParseKey rejects. It is used only for byte accounting, so the
// quota recompute counts every object in the account's namespace.
func AccountFromKey(key string) string {
	trimmed := strings.TrimPrefix(key, "u/")
	if i := strings.IndexByte(trimmed, '/'); i > 0 {
		return trimmed[:i]
	}
	return ""
}

// Group is every object that shares one account + base uuid, plus the media row
// (if any) and the application references that name the uuid.
type Group struct {
	AccountID string    `json:"account_id"`
	Base      string    `json:"base"`
	Objects   []Object  `json:"objects"`
	Bytes     int64     `json:"bytes"`
	Variants  []string  `json:"variants"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Row       *Row      `json:"row,omitempty"`
	RowDelta  int64     `json:"row_size_delta,omitempty"`
	Refs      []RefHit  `json:"references,omitempty"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason"`
	Warning   string    `json:"warning,omitempty"`
}

// BaseKey returns the base object's key ("" when the base object is absent — a
// group can be variant-only after a failed or interrupted upload).
func (g *Group) BaseKey() string {
	for _, o := range g.Objects {
		if pk, ok := ParseKey(o.Key); ok && pk.Variant == VariantBase {
			return o.Key
		}
	}
	return ""
}

func (g *Group) key() string { return g.AccountID + "/" + g.Base }

// Decision is one operator ruling from a decisions file. It only ever WIDENS the
// evidence in the safe direction: a deletable ruling is refused when references
// exist, because the reference check is the safety property, not the ruling.
type Decision struct {
	Account  string `json:"account"`
	Base     string `json:"base"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (d Decision) key() string { return d.Account + "/" + d.Base }

// DecisionFile is the on-disk shape of --decisions.
type DecisionFile struct {
	Decisions []Decision `json:"decisions"`
}

// BuildGroups groups objects by (account, base uuid) and attaches media rows by
// object key. Rows are matched on exact key, and live + trashed rows are both
// attached: a trashed row still owns its bytes (quota is released only on
// purge), so "has a trashed row" is a different case from "no row at all".
func BuildGroups(objects []Object, rows []Row) (groups []*Group, unparsed []Object) {
	byKey := make(map[string]*Group)
	rowByKey := make(map[string]*Row, len(rows))
	for i := range rows {
		rowByKey[rows[i].Key] = &rows[i]
	}

	for _, o := range objects {
		pk, ok := ParseKey(o.Key)
		if !ok {
			unparsed = append(unparsed, o)
			continue
		}
		gk := pk.AccountID + "/" + pk.Base
		g := byKey[gk]
		if g == nil {
			g = &Group{AccountID: pk.AccountID, Base: pk.Base}
			byKey[gk] = g
		}
		g.Objects = append(g.Objects, o)
		g.Bytes += o.Size
		if g.FirstSeen.IsZero() || o.LastModified.Before(g.FirstSeen) {
			g.FirstSeen = o.LastModified
		}
		if o.LastModified.After(g.LastSeen) {
			g.LastSeen = o.LastModified
		}
		if !contains(g.Variants, pk.Variant) {
			g.Variants = append(g.Variants, pk.Variant)
		}
	}

	for _, g := range byKey {
		sort.Slice(g.Objects, func(i, j int) bool { return g.Objects[i].Key < g.Objects[j].Key })
		sort.Strings(g.Variants)
		for _, o := range g.Objects {
			if row, ok := rowByKey[o.Key]; ok {
				g.Row = row
				break
			}
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].AccountID != groups[j].AccountID {
			return groups[i].AccountID < groups[j].AccountID
		}
		return groups[i].Base < groups[j].Base
	})
	sort.Slice(unparsed, func(i, j int) bool { return unparsed[i].Key < unparsed[j].Key })
	return groups, unparsed
}

// ParseDecisions decodes a decisions file and validates every ruling against the
// measured groups. An unrecognised (account, base) pair is an ERROR, not a
// no-op: a typo in a uuid would otherwise silently leave an object undeleted
// while the report claimed the decision was applied.
func ParseDecisions(data []byte, groups []*Group) (map[string]Decision, error) {
	var file DecisionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decisions: %w", err)
	}
	known := make(map[string]*Group, len(groups))
	for _, g := range groups {
		known[g.key()] = g
	}
	out := make(map[string]Decision, len(file.Decisions))
	for _, d := range file.Decisions {
		if _, ok := known[d.key()]; !ok {
			return nil, fmt.Errorf("decisions: %s/%s is not a measured object group", d.Account, d.Base)
		}
		switch d.Decision {
		case DecisionAdopt, DecisionKeep, DecisionDeletable:
		case DecisionRowed:
			return nil, fmt.Errorf("decisions: %s/%s cannot be ruled %q (that is derived from the media row)", d.Account, d.Base, d.Decision)
		default:
			return nil, fmt.Errorf("decisions: %s/%s has unknown decision %q", d.Account, d.Base, d.Decision)
		}
		if strings.TrimSpace(d.Reason) == "" {
			return nil, fmt.Errorf("decisions: %s/%s needs a reason", d.Account, d.Base)
		}
		if prev, ok := out[d.key()]; ok {
			return nil, fmt.Errorf("decisions: %s/%s ruled twice (%q and %q)", d.Account, d.Base, prev.Decision, d.Decision)
		}
		out[d.key()] = d
	}
	return out, nil
}

// Classify applies the evidence, in order:
//
//  1. a media row (live or trashed) wins — the group is manageable in the app;
//  2. any application reference wins next — the bytes are in use, so the group is
//     KEPT no matter what a decisions file says. This is the rule that stops a
//     blind "delete every object without a media row" from breaking an avatar or
//     a product image;
//  3. an explicit operator ruling, if one exists;
//  4. otherwise ADOPT: preserve the bytes and give them a row, never delete on a
//     default. Deletion requires an explicit ruling plus no references.
func Classify(g *Group, decisions map[string]Decision) {
	g.Reason, g.Warning = "", ""
	if g.Row != nil {
		state := "live"
		if g.Row.DeletedAt != nil {
			state = "trashed"
		}
		g.Decision = DecisionRowed
		g.Reason = fmt.Sprintf("media row %s (%s, row size %d B)", g.Row.ID, state, g.Row.Size)
		if d, ok := decisions[g.key()]; ok && d.Decision == DecisionDeletable {
			g.Warning = fmt.Sprintf("refused deletable ruling %q: group has a media row, purge it through the app instead", d.Reason)
		}
		return
	}
	if len(g.Refs) > 0 {
		g.Decision = DecisionKeep
		g.Reason = "referenced by " + describeRefs(g.Refs)
		if d, ok := decisions[g.key()]; ok && d.Decision == DecisionDeletable {
			g.Warning = fmt.Sprintf("refused deletable ruling %q: still referenced by %s", d.Reason, describeRefs(g.Refs))
		}
		return
	}
	if d, ok := decisions[g.key()]; ok {
		g.Decision = d.Decision
		g.Reason = d.Reason
		return
	}
	g.Decision = DecisionAdopt
	g.Reason = "no media row and no reference on any scanned surface; adopted so the bytes stay and become manageable (no quota change)"
}

func describeRefs(refs []RefHit) string {
	seen := map[string]bool{}
	var parts []string
	for _, r := range refs {
		if seen[r.Surface] {
			continue
		}
		seen[r.Surface] = true
		parts = append(parts, r.Surface)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// VerifyDeletable re-checks the delete plan against references measured AFTER
// the report. A group ruled deletable when the report was produced must still be
// unreferenced now: a product edited in between wins, and the whole apply aborts
// rather than deleting bytes a merchant has since put on a page.
func VerifyDeletable(groups []*Group, fresh map[string][]RefHit) error {
	for _, g := range groups {
		if g.Decision != DecisionDeletable {
			continue
		}
		if hits := fresh[g.Base]; len(hits) > 0 {
			return fmt.Errorf("%s/%s gained a reference (%s) between report and apply; nothing deleted",
				g.AccountID, g.Base, describeRefs(hits))
		}
	}
	return nil
}

// DeletableKeys returns the object keys an --apply run may delete: every object
// of every group classified deletable. It refuses when such a group carries a
// row or a reference, so the delete plan can never be built from a stale or
// hand-edited classification.
func DeletableKeys(groups []*Group) ([]string, error) {
	var keys []string
	for _, g := range groups {
		if g.Decision != DecisionDeletable {
			continue
		}
		if g.Row != nil {
			return nil, fmt.Errorf("refusing to delete %s/%s: it has media row %s", g.AccountID, g.Base, g.Row.ID)
		}
		if len(g.Refs) > 0 {
			return nil, fmt.Errorf("refusing to delete %s/%s: referenced by %s", g.AccountID, g.Base, describeRefs(g.Refs))
		}
		for _, o := range g.Objects {
			keys = append(keys, o.Key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// AdoptRows builds the media rows an --apply run would insert for adopted
// groups. The row records the GROUP's bytes (base + variants), which is what the
// quota counter should track — re-creating the row must not add bytes, so the
// counter is left alone for an adopt.
func AdoptRows(groups []*Group, publicBase string) []Row {
	var out []Row
	base := strings.TrimRight(publicBase, "/")
	for _, g := range groups {
		if g.Decision != DecisionAdopt {
			continue
		}
		key := g.BaseKey()
		if key == "" || g.Row != nil {
			continue
		}
		out = append(out, Row{
			AccountID: g.AccountID,
			Key:       key,
			URL:       base + "/" + key,
			Mime:      MimeForExt(key),
			Size:      g.Bytes,
		})
	}
	return out
}

// MimeForExt maps the stored extension to the mime the upload path records.
func MimeForExt(key string) string {
	i := strings.LastIndexByte(key, '.')
	if i < 0 {
		return "application/octet-stream"
	}
	switch strings.ToLower(key[i:]) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	}
	return "application/octet-stream"
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
