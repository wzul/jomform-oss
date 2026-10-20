package mediareconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/objstore"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func obj(key string, size int64, when string) Object {
	return Object{Key: key, Size: size, LastModified: ts(when)}
}

const (
	acct  = "d9ee2f12-c118-424a-940a-de3e3f1c81cc"
	base1 = "05ab4de5-24b2-4712-8443-42e384e58584"
	base2 = "197cf316-5bc9-4f8e-9e98-a6b285593651"
	base3 = "7911b2bf-049e-4496-8747-6477ea897b32"
)

func groupFixture() []Object {
	return []Object{
		obj("u/"+acct+"/"+base1+".jpg", 1000, "2026-09-20T16:26:10Z"),
		obj("u/"+acct+"/"+base1+"-thumbnail.jpg", 100, "2026-09-20T16:26:11Z"),
		obj("u/"+acct+"/"+base1+"-medium.jpg", 200, "2026-09-20T16:26:12Z"),
		obj("u/"+acct+"/"+base1+"-full.jpg", 1000, "2026-09-20T16:26:13Z"),
		obj("u/"+acct+"/"+base2+".jpg", 500, "2026-10-18T23:36:36Z"),
		obj("u/"+acct+"/"+base3+".jpg", 120, "2026-09-02T17:07:07Z"),
		obj("u/"+acct+"/legacy/weird-name.png", 70, "2026-09-01T00:00:00Z"),
	}
}

func TestParseKey(t *testing.T) {
	cases := []struct {
		key     string
		ok      bool
		account string
		base    string
		variant string
		ext     string
	}{
		{"u/" + acct + "/" + base1 + ".jpg", true, acct, base1, VariantBase, ".jpg"},
		{"u/" + acct + "/" + base1 + "-thumbnail.jpg", true, acct, base1, VariantThumbnail, ".jpg"},
		{"u/" + acct + "/" + base1 + "-medium.jpg", true, acct, base1, VariantMedium, ".jpg"},
		{"u/" + acct + "/" + base1 + "-full.jpg", true, acct, base1, VariantFull, ".jpg"},
		{"u/" + acct + "/" + base1 + "-preview.jpg", true, acct, base1, VariantPreview, ".jpg"},
		// A listing that strips the bucket prefix still parses.
		{acct + "/" + base1 + ".png", true, acct, base1, VariantBase, ".png"},
		// Not the canonical upload shape: must not be grouped, because a group
		// delete works by base key and these have no base key to reason about.
		{"u/" + acct + "/legacy/weird-name.png", false, "", "", "", ""},
		{"u/" + acct + "/not-a-uuid.jpg", false, "", "", "", ""},
		{"elsewhere/x.jpg", false, "", "", "", ""},
	}
	for _, c := range cases {
		pk, ok := ParseKey(c.key)
		if ok != c.ok {
			t.Fatalf("%s: ok=%v want %v", c.key, ok, c.ok)
		}
		if !ok {
			continue
		}
		if pk.AccountID != c.account || pk.Base != c.base || pk.Variant != c.variant || pk.Ext != c.ext {
			t.Fatalf("%s: got %+v", c.key, pk)
		}
	}
}

func TestBuildGroupsAttachesRowsAndKeepsUnparsed(t *testing.T) {
	rows := []Row{
		{ID: "row-live", AccountID: acct, Key: "u/" + acct + "/" + base1 + ".jpg", Size: 1000},
		// Trashed rows are still rows: quota is released only on purge, so this
		// group must NOT be treated as row-less.
		{ID: "row-trash", AccountID: acct, Key: "u/" + acct + "/" + base3 + ".jpg", Size: 120, DeletedAt: ptrTime(ts("2026-10-01T00:00:00Z"))},
	}
	groups, unparsed := BuildGroups(groupFixture(), rows)
	if len(unparsed) != 1 || unparsed[0].Key != "u/"+acct+"/legacy/weird-name.png" {
		t.Fatalf("unparsed = %+v", unparsed)
	}
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	byBase := map[string]*Group{}
	for _, g := range groups {
		byBase[g.Base] = g
	}
	g1 := byBase[base1]
	if g1.Row == nil || g1.Row.ID != "row-live" {
		t.Fatalf("base1 row = %+v", g1.Row)
	}
	if g1.Bytes != 2300 || len(g1.Objects) != 4 {
		t.Fatalf("base1 bytes=%d objects=%d", g1.Bytes, len(g1.Objects))
	}
	if strings.Join(g1.Variants, ",") != "base,full,medium,thumbnail" {
		t.Fatalf("base1 variants = %v", g1.Variants)
	}
	if g1.FirstSeen != ts("2026-09-20T16:26:10Z") || g1.LastSeen != ts("2026-09-20T16:26:13Z") {
		t.Fatalf("base1 window = %v..%v", g1.FirstSeen, g1.LastSeen)
	}
	if g3 := byBase[base3]; g3.Row == nil || g3.Row.DeletedAt == nil {
		t.Fatalf("trashed row not attached: %+v", g3.Row)
	}
	if g2 := byBase[base2]; g2.Row != nil {
		t.Fatalf("base2 should have no row, got %+v", g2.Row)
	}
}

func TestClassifyKeepsReferencedGroupEvenWhenRuledDeletable(t *testing.T) {
	groups, _ := BuildGroups(groupFixture(), nil)
	byBase := map[string]*Group{}
	for _, g := range groups {
		byBase[g.Base] = g
	}
	// A product references base1; a decisions file (wrongly, or with stale
	// evidence) rules it deletable.
	byBase[base1].Refs = []RefHit{{Surface: "products.images", RecordID: "p1"}}
	decisions := map[string]Decision{
		acct + "/" + base1: {Account: acct, Base: base1, Decision: DecisionDeletable, Reason: "looked orphaned"},
	}
	for _, gk := range []string{acct + "/" + base2, acct + "/" + base3} {
		decisions[gk] = Decision{Account: acct, Base: strings.Split(gk, "/")[1], Decision: DecisionDeletable, Reason: "probe"}
	}
	for _, g := range groups {
		Classify(g, decisions)
	}
	if got := byBase[base1].Decision; got != DecisionKeep {
		t.Fatalf("referenced group decision = %q, want keep", got)
	}
	if !strings.Contains(byBase[base1].Warning, "refused deletable") {
		t.Fatalf("refused ruling must be surfaced, warning = %q", byBase[base1].Warning)
	}
	if got := byBase[base2].Decision; got != DecisionDeletable {
		t.Fatalf("unreferenced ruled group decision = %q, want deletable", got)
	}
	// And the safety rule holds at the delete-plan layer too.
	keys, err := DeletableKeys(groups)
	if err != nil {
		t.Fatalf("DeletableKeys: %v", err)
	}
	for _, k := range keys {
		if strings.Contains(k, base1) {
			t.Fatalf("delete plan contains a referenced group key: %s", k)
		}
	}
}

func TestClassifyRowlessDefaultsToAdoptNotDelete(t *testing.T) {
	groups, _ := BuildGroups(groupFixture(), nil)
	for _, g := range groups {
		Classify(g, nil)
	}
	for _, g := range groups {
		if g.Decision != DecisionAdopt {
			t.Fatalf("%s decision = %q, want adopt (no silent delete on a default)", g.Base, g.Decision)
		}
	}
	// The default must produce NO delete plan at all.
	keys, err := DeletableKeys(groups)
	if err != nil {
		t.Fatalf("DeletableKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("default classification produced a delete plan: %v", keys)
	}
}

func TestClassifyRowedRefusesDeletableRuling(t *testing.T) {
	rows := []Row{{ID: "row-live", AccountID: acct, Key: "u/" + acct + "/" + base1 + ".jpg", Size: 1000}}
	groups, _ := BuildGroups(groupFixture(), rows)
	var g1 *Group
	for _, g := range groups {
		if g.Base == base1 {
			g1 = g
		}
	}
	Classify(g1, map[string]Decision{
		acct + "/" + base1: {Account: acct, Base: base1, Decision: DecisionDeletable, Reason: "tidy"},
	})
	if g1.Decision != DecisionRowed {
		t.Fatalf("decision = %q, want rowed", g1.Decision)
	}
	if !strings.Contains(g1.Warning, "purge it through the app") {
		t.Fatalf("warning = %q", g1.Warning)
	}
	// Force the classification the delete plan must never trust: a group that
	// carries a row cannot be deleted from the store, whatever the field says.
	g1.Decision = DecisionDeletable
	if _, err := DeletableKeys([]*Group{g1}); err == nil {
		t.Fatal("DeletableKeys accepted a group with a media row")
	}
}

func TestParseDecisionsRejectsTyposAndUnknownValues(t *testing.T) {
	groups, _ := BuildGroups(groupFixture(), nil)
	cases := map[string]string{
		"unknown group":    `{"decisions":[{"account":"` + acct + `","base":"11111111-1111-1111-1111-111111111111","decision":"deletable","reason":"x"}]}`,
		"row is derived":   `{"decisions":[{"account":"` + acct + `","base":"` + base2 + `","decision":"rowed","reason":"x"}]}`,
		"bad value":        `{"decisions":[{"account":"` + acct + `","base":"` + base2 + `","decision":"delete","reason":"x"}]}`,
		"missing reason":   `{"decisions":[{"account":"` + acct + `","base":"` + base2 + `","decision":"deletable","reason":""}]}`,
		"duplicate ruling": `{"decisions":[{"account":"` + acct + `","base":"` + base2 + `","decision":"keep","reason":"a"},{"account":"` + acct + `","base":"` + base2 + `","decision":"deletable","reason":"b"}]}`,
	}
	for name, body := range cases {
		if _, err := ParseDecisions([]byte(body), groups); err == nil {
			t.Fatalf("%s: expected an error, got none", name)
		}
	}
	// The valid shape is accepted.
	ok := `{"decisions":[{"account":"` + acct + `","base":"` + base2 + `","decision":"deletable","reason":"probe"}]}`
	if _, err := ParseDecisions([]byte(ok), groups); err != nil {
		t.Fatalf("valid decisions rejected: %v", err)
	}
}

func TestAdoptRowsUseGroupBytesAndBaseKey(t *testing.T) {
	groups, _ := BuildGroups(groupFixture(), nil)
	for _, g := range groups {
		Classify(g, nil)
	}
	rows := AdoptRows(groups, "https://jomform.com/media/")
	if len(rows) != 3 {
		t.Fatalf("adopted rows = %d, want 3", len(rows))
	}
	wantSize := map[string]int64{base1: 2300, base2: 500, base3: 120}
	for _, r := range rows {
		base := ""
		for _, g := range groups {
			if g.BaseKey() == r.Key {
				base = g.Base
			}
		}
		if base == "" {
			t.Fatalf("adopted row key is not any group's base key: %s", r.Key)
		}
		if r.Size != wantSize[base] {
			t.Fatalf("%s adopted size = %d, want the group total %d", base, r.Size, wantSize[base])
		}
		if strings.Contains(r.Key, "-thumbnail") {
			t.Fatalf("adopted key is a variant: %s", r.Key)
		}
		if !strings.HasPrefix(r.URL, "https://jomform.com/media/u/") {
			t.Fatalf("adopted url = %s", r.URL)
		}
	}
}

func TestBuildReportTotalsAndRecompute(t *testing.T) {
	objects := groupFixture()
	// Make base2 huge enough to exceed the cap: the recompute is clamped.
	objects[4].Size = MaxWorkspaceBytes + 10
	rows := []Row{{ID: "row-live", AccountID: acct, Key: "u/" + acct + "/" + base1 + ".jpg", Size: 1000}}
	groups, unparsed := BuildGroups(objects, rows)
	r := BuildReport("jomform-media", "u/", objects, rows, groups, unparsed,
		[]Account{{ID: acct, Name: "wan Store", Counter: 3074848}}, nil)

	if r.Totals.Objects != len(objects) {
		t.Fatalf("totals objects = %d, want %d", r.Totals.Objects, len(objects))
	}
	if r.Totals.Groups != 3 || r.Totals.RowedGroups != 1 || r.Totals.RowlessGroups != 2 {
		t.Fatalf("group totals = %+v", r.Totals)
	}
	if r.ByDecision[DecisionAdopt].Groups != 2 {
		t.Fatalf("adopt counts = %+v", r.ByDecision)
	}
	var sum int64
	for _, o := range objects {
		sum += o.Size
	}
	if r.Totals.Bytes != sum {
		t.Fatalf("totals bytes = %d, want %d", r.Totals.Bytes, sum)
	}
	if len(r.Accounts) != 1 {
		t.Fatalf("accounts = %+v", r.Accounts)
	}
	a := r.Accounts[0]
	if a.StoreBytes != sum {
		t.Fatalf("store bytes = %d, want %d", a.StoreBytes, sum)
	}
	if a.Recomputed != MaxWorkspaceBytes {
		t.Fatalf("recomputed = %d, want the 1GiB clamp", a.Recomputed)
	}
	if a.RowBytes != 1000 {
		t.Fatalf("row bytes = %d", a.RowBytes)
	}
	if r.Groups[0].RowDelta == 0 && r.Groups[0].Row != nil {
		t.Fatal("row delta must show how far the row is from the group's bytes")
	}
}

func TestMaxWorkspaceBytesMatchesObjstore(t *testing.T) {
	if MaxWorkspaceBytes != objstore.MaxWorkspaceBytes {
		t.Fatalf("mediareconcile cap %d != objstore cap %d", MaxWorkspaceBytes, objstore.MaxWorkspaceBytes)
	}
}

func TestVerifyDeletableAbortsWhenAReferenceAppears(t *testing.T) {
	groups, _ := BuildGroups(groupFixture(), nil)
	decisions := map[string]Decision{
		acct + "/" + base2: {Account: acct, Base: base2, Decision: DecisionDeletable, Reason: "probe"},
	}
	for _, g := range groups {
		Classify(g, decisions)
	}
	// The apply-time re-scan finds a NEW reference on a group ruled deletable.
	err := VerifyDeletable(groups, map[string][]RefHit{
		base2: {{Surface: "products.images", RecordID: "product-created-after-report"}},
	})
	if err == nil {
		t.Fatal("apply proceeded although a reference appeared after the report")
	}
	if !strings.Contains(err.Error(), "gained a reference") {
		t.Fatalf("error = %v", err)
	}
	// No new references: the plan stands.
	if err := VerifyDeletable(groups, map[string][]RefHit{}); err != nil {
		t.Fatalf("verify with no hits: %v", err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
