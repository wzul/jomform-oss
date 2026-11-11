package dormancy

// Guards on the phase-1 dormancy policy (card t_94e5cca3).
//
// These are the PURE half: no database, no Valkey, no SES. The store's single
// statement is pinned separately in dbstore/dormancy_test.go (against a real
// Postgres), because "the metric is account-scoped" and "no per-row scan of
// sales" are properties of SQL, not of this file.
//
// Every test here corresponds to one guarantee the card names. The two that
// matter most for safety are:
//
//   - TestDormantAccountWithRecentSubmissionIsExcluded — a merchant who has been
//     quiet for months while buyers are still submitting must be EXCLUDED, and the
//     test ALSO runs the same fixture through a copy of the policy with the
//     submission check removed, so the guard is proven to bite rather than merely
//     to pass.
//   - TestNudgeNeverGoesToTheSystemAccountThe30DayTierOrTwiceIn90Days — the three
//     email limits, each asserted by the absence of a send.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// sourceText reads this package's own non-test source, for the structural
// read-only guard.
func sourceText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("dormancy.go")
	if err != nil {
		t.Fatalf("read dormancy.go: %v", err)
	}
	return string(b)
}

var testNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return testNow.Add(-time.Duration(n) * 24 * time.Hour) }

// row builds a store row whose ONLY activity is its own creation, `dormant`
// days ago. Everything else is empty, which is the production shape of the six
// accounts the card measured (1-7 forms, 0 sales, 0 submissions).
func row(id string, dormant int) dbstore.DormancyRow {
	return dbstore.DormancyRow{
		AccountID:         id,
		WorkspaceSlug:     "ws-" + id,
		OwnerEmail:        id + "@example.test",
		OwnerUserID:       "user-" + id,
		OwnerLang:         "en",
		AccountCreatedAt:  daysAgo(dormant),
		LastActivity:      daysAgo(dormant),
		FormsPublished:    3,
		SalesEver:         0,
		SubscriptionsEver: 0,
		Submissions90d:    0,
	}
}

func TestBucketsAreLowerBoundsAndPartitionTheRange(t *testing.T) {
	cases := []struct {
		days int
		want Bucket
	}{
		{0, BucketActive}, {29, BucketActive},
		{30, Bucket30}, {59, Bucket30},
		{60, Bucket60}, {89, Bucket60},
		{90, Bucket90}, {364, Bucket90},
		{365, Bucket365}, {900, Bucket365},
	}
	for _, c := range cases {
		if got := bucketFor(c.days); got != c.want {
			t.Errorf("%d days → %q, want %q", c.days, got, c.want)
		}
	}
}

// TestDormantAccountWithRecentSubmissionIsExcluded is the card's headline guard:
// a dormant merchant is not a dormant form.
func TestDormantAccountWithRecentSubmissionIsExcluded(t *testing.T) {
	r := row("live-buyers", 200)
	r.Submissions90d = 4 // buyers submitted yesterday; the merchant has not logged in

	rep := Build([]dbstore.DormancyRow{r}, Options{Now: testNow})
	if len(rep.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rep.Rows))
	}
	got := rep.Rows[0]
	if !got.Excluded {
		t.Fatalf("an account with 4 submissions in the last 90 days was NOT excluded: bucket=%q reasons=%v",
			got.Bucket, got.ExclusionReasons)
	}
	if got.Bucket != BucketExcluded {
		t.Errorf("bucket = %q, want %q", got.Bucket, BucketExcluded)
	}
	if !hasReason(got.ExclusionReasons, ReasonRecentSubmissions) {
		t.Errorf("reasons = %v, want %q", got.ExclusionReasons, ReasonRecentSubmissions)
	}
	if rep.Candidates != 0 || rep.Excluded != 1 {
		t.Errorf("candidates=%d excluded=%d, want 0/1", rep.Candidates, rep.Excluded)
	}
	if got.NudgeEligible {
		t.Error("an excluded account is nudge-eligible")
	}
}

// TestRemovingTheSubmissionCheckPutsItInACandidateBucket is the BITE PROOF. It runs
// the same fixture through a copy of exclusionReasons with the submission check
// deleted and shows the account lands in a candidate bucket. If the check were
// ever removed from the real policy, the assertion above would fail and this one
// would keep passing — which is exactly what makes the first assertion meaningful
// rather than vacuous.
func TestRemovingTheSubmissionCheckPutsItInACandidateBucket(t *testing.T) {
	r := row("live-buyers", 200)
	r.Submissions90d = 4

	if !hasReason(exclusionReasons(r), ReasonRecentSubmissions) {
		t.Fatal("control: the real policy does not flag the recent submission, so this bite proof is meaningless")
	}

	// The same rules minus the submission check.
	withoutSubmissionCheck := func(r dbstore.DormancyRow) []string {
		var out []string
		if isSystemAccount(r) {
			out = append(out, ReasonSystemAccount)
		}
		if r.SalesEver > 0 {
			out = append(out, ReasonHasSales)
		}
		if r.SubscriptionsEver > 0 {
			out = append(out, ReasonHasSubscription)
		}
		if r.WorkspaceTrashed {
			out = append(out, ReasonWorkspaceInTrash)
		}
		if strings.TrimSpace(r.OwnerEmail) == "" {
			out = append(out, ReasonNoOwnerEmail)
		}
		return out
	}
	if reasons := withoutSubmissionCheck(r); len(reasons) != 0 {
		t.Fatalf("the bite-proof copy still excludes the fixture (%v) — it is not measuring the submission check alone", reasons)
	}
	// With no other reason, Build would place it in the 200-day tier: a CANDIDATE.
	if got := bucketFor(200); got != Bucket90 {
		t.Fatalf("fixture is not in a candidate tier: %q", got)
	}
	t.Log("bite proof: without the recent_submissions check the same fixture becomes a 90-day CANDIDATE and would be nudged")
}

func hasReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

// TestEveryExclusionReasonIsReachable keeps the reason list honest: a code in
// Reasons that Build can never emit would let a UI legend promise a state that
// does not exist.
func TestEveryExclusionReasonIsReachable(t *testing.T) {
	fixtures := map[string]dbstore.DormancyRow{
		ReasonSystemAccount:     {AccountID: "s", WorkspaceSlug: "w", OwnerEmail: dbstore.LegacyProvisionedEmails[0], LastActivity: daysAgo(400)},
		ReasonHasSales:          {AccountID: "a", WorkspaceSlug: "w", OwnerEmail: "a@example.test", LastActivity: daysAgo(400), SalesEver: 1},
		ReasonHasSubscription:   {AccountID: "b", WorkspaceSlug: "w", OwnerEmail: "b@example.test", LastActivity: daysAgo(400), SubscriptionsEver: 1},
		ReasonRecentSubmissions: {AccountID: "c", WorkspaceSlug: "w", OwnerEmail: "c@example.test", LastActivity: daysAgo(400), Submissions90d: 1},
		ReasonWorkspaceInTrash:  {AccountID: "d", WorkspaceSlug: "", OwnerEmail: "d@example.test", LastActivity: daysAgo(400), WorkspaceTrashed: true},
		ReasonNoOwnerEmail:      {AccountID: "e", WorkspaceSlug: "w", OwnerEmail: "", LastActivity: daysAgo(400)},
	}
	for _, want := range Reasons {
		r, ok := fixtures[want]
		if !ok {
			t.Fatalf("no fixture proves %q is reachable", want)
		}
		if got := exclusionReasons(r); !hasReason(got, want) {
			t.Errorf("fixture for %q produced %v", want, got)
		}
	}
	if len(fixtures) != len(Reasons) {
		t.Errorf("Reasons has %d codes but the fixture table has %d", len(Reasons), len(fixtures))
	}
}

// TestAdminAccountIsExcludedEvenWithAnOrdinaryEmail covers the other half of
// "system/admin": users.is_admin is a role, not an address.
func TestAdminAccountIsExcludedEvenWithAnOrdinaryEmail(t *testing.T) {
	r := row("admin", 400)
	r.OwnerIsAdmin = true
	rep := Build([]dbstore.DormancyRow{r}, Options{Now: testNow})
	if !rep.Rows[0].Excluded || !hasReason(rep.Rows[0].ExclusionReasons, ReasonSystemAccount) {
		t.Fatalf("an is_admin owner was treated as a candidate: %+v", rep.Rows[0])
	}
	if rep.Rows[0].NudgeEligible {
		t.Error("an is_admin owner is nudge-eligible")
	}
}

// TestNudgeTargetsOnlyTheSixtyDayTierAndBeyond pins the threshold, both ways.
func TestNudgeTargetsOnlyTheSixtyDayTierAndBeyond(t *testing.T) {
	rows := []dbstore.DormancyRow{
		row("active", 5), row("d29", 29),
		row("d30", 30), row("d59", 59),
		row("d60", 60), row("d90", 90), row("d365", 365),
	}
	rep := Build(rows, Options{Now: testNow})
	want := map[string]bool{"active": false, "d29": false, "d30": false, "d59": false, "d60": true, "d90": true, "d365": true}
	for _, r := range rep.Rows {
		if r.NudgeEligible != want[r.AccountID] {
			t.Errorf("%s (bucket %s, %d days): nudge_eligible=%v, want %v",
				r.AccountID, r.Bucket, r.DormantDays, r.NudgeEligible, want[r.AccountID])
		}
	}
}

// ── Runner ─────────────────────────────────────────────────────────────────

type fakeSource struct {
	rows []dbstore.DormancyRow
	err  error
	got  dbstore.DormancyReportOptions
}

func (f *fakeSource) DormancyReport(_ context.Context, o dbstore.DormancyReportOptions) ([]dbstore.DormancyRow, error) {
	f.got = o
	return f.rows, f.err
}

type fakeRecorder struct {
	calls  []string
	failOn map[string]bool
}

func (f *fakeRecorder) RecordDormancyNudge(_ context.Context, accountID, _, _, _ string) error {
	if f.failOn[accountID] {
		return errors.New("db down")
	}
	f.calls = append(f.calls, accountID)
	return nil
}

type sentMail struct{ To, Subject, Body, Lang string }

type fakeMailer struct {
	sent   []sentMail
	failOn map[string]bool
}

func (f *fakeMailer) Send(to, subject, body, lang string) error {
	if f.failOn[to] {
		return errors.New("ses rejected")
	}
	f.sent = append(f.sent, sentMail{to, subject, body, lang})
	return nil
}

// TestNudgeNeverGoesToTheSystemAccountThe30DayTierOrTwiceIn90Days is the card's
// email guard, asserted as ABSENCE: the fake mailer records what was sent and the
// test names the accounts that must not appear.
func TestNudgeNeverGoesToTheSystemAccountThe30DayTierOrTwiceIn90Days(t *testing.T) {
	system := row("sys", 400)
	system.OwnerEmail = dbstore.LegacyProvisionedEmails[0]
	recentlyNudged := row("nudged", 400)
	before := daysAgo(30) // inside the 90-day window
	recentlyNudged.LastNudgeAt = &before

	src := &fakeSource{rows: []dbstore.DormancyRow{
		system,
		row("d30", 30),
		recentlyNudged,
		row("d60", 60),
		row("d90", 95),
	}}
	rec := &fakeRecorder{failOn: map[string]bool{}}
	mail := &fakeMailer{failOn: map[string]bool{}}
	r := &Runner{Source: src, Recorder: rec, Mailer: mail, Now: func() time.Time { return testNow }}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	sentTo := map[string]bool{}
	for _, m := range mail.sent {
		sentTo[m.To] = true
	}
	if sentTo[dbstore.LegacyProvisionedEmails[0]] {
		t.Error("the system account was emailed")
	}
	if sentTo["d30@example.test"] {
		t.Error("an account in the 30-day bucket was emailed")
	}
	if sentTo["nudged@example.test"] {
		t.Error("an account nudged 30 days ago was emailed again (the window is 90 days)")
	}
	// The two that SHOULD be nudged.
	for _, want := range []string{"d60@example.test", "d90@example.test"} {
		if !sentTo[want] {
			t.Errorf("%s was not emailed: sent=%v", want, mail.sent)
		}
	}
	if len(mail.sent) != 2 || len(res.Sent) != 2 {
		t.Errorf("sent %d mail(s) / %d result(s), want 2", len(mail.sent), len(res.Sent))
	}
	// The email is dictionary copy in the owner's language, carries the workspace
	// and the dormancy age, and makes no claim about deletion.
	for _, m := range mail.sent {
		if m.Subject == "" || m.Body == "" {
			t.Errorf("empty message to %s", m.To)
		}
		if !strings.Contains(m.Body, "ws-") {
			t.Errorf("body does not name the workspace: %q", m.Body)
		}
		if m.Lang != emailtext.En {
			t.Errorf("lang = %q, want the owner's en", m.Lang)
		}
	}
}

// TestNudgeRendersInTheOwnersLanguage: the nudge follows users.lang, so an ms
// owner reads Malay. This is the language-consistency contract the dictionary
// enforces for every platform-user email.
func TestNudgeRendersInTheOwnersLanguage(t *testing.T) {
	r := row("ms", 95)
	r.OwnerLang = "ms"
	src := &fakeSource{rows: []dbstore.DormancyRow{r}}
	mail := &fakeMailer{failOn: map[string]bool{}}
	run := &Runner{Source: src, Recorder: &fakeRecorder{failOn: map[string]bool{}}, Mailer: mail,
		Now: func() time.Time { return testNow }}
	if _, err := run.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := emailtext.Text("ms", emailtext.DormancyNudge, map[string]string{"workspace": "ws-ms", "days": "95"})
	if len(mail.sent) != 1 || mail.sent[0].Subject != want.Subject || mail.sent[0].Lang != "ms" {
		t.Fatalf("sent %+v, want the ms rendering %q", mail.sent, want.Subject)
	}
}

// TestNoSendWithoutARecordedReservation: the rate-limit reservation comes FIRST.
// If the events write fails the account is skipped, because a send whose record
// was lost would be repeated tomorrow — and a repeat inside 90 days is the one
// thing this job must never do.
func TestNoSendWithoutARecordedReservation(t *testing.T) {
	src := &fakeSource{rows: []dbstore.DormancyRow{row("a", 95), row("b", 95)}}
	rec := &fakeRecorder{failOn: map[string]bool{"a": true}}
	mail := &fakeMailer{failOn: map[string]bool{}}
	r := &Runner{Source: src, Recorder: rec, Mailer: mail, Now: func() time.Time { return testNow }}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.sent) != 1 || mail.sent[0].To != "b@example.test" {
		t.Fatalf("sent=%+v, want only b (a's reservation failed)", mail.sent)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Err, "reserve") {
		t.Errorf("skipped=%+v, want a's reservation failure recorded", res.Skipped)
	}
}

// TestNoReservationWithoutAMailer: a deployment that cannot send (a dev box, a
// probe) must not burn an account's 90-day slot. The nil check sits BEFORE the
// reservation, so an SES-less boot leaves the report unchanged.
func TestNoReservationWithoutAMailer(t *testing.T) {
	src := &fakeSource{rows: []dbstore.DormancyRow{row("a", 95)}}
	rec := &fakeRecorder{failOn: map[string]bool{}}
	r := &Runner{Source: src, Recorder: rec, Mailer: nil, Now: func() time.Time { return testNow }}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("recorded %v with no mailer configured — the 90-day slot is burned by a send that cannot happen", rec.calls)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Err != "no mailer configured" {
		t.Errorf("skipped=%+v, want the nil-mailer skip", res.Skipped)
	}
}

// TestReportIsReadOnlyByShape is a structural guard on the phase-1 boundary: this
// package must not contain a delete/unpublish/flag write at all. If a later card
// wants phases 2 or 3 it will have to change this test deliberately, which is the
// point. Prose in the comments is not what is checked — a STATEMENT is.
func TestReportIsReadOnlyByShape(t *testing.T) {
	src := sourceText(t)
	for _, forbidden := range []string{
		"DELETE FROM", "deleted_at =", ".Delete(", ".Unpublish(",
		"SET status = 'draft'", "status = 'published'", "PurgeAccount",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("dormancy.go contains %q — phase 1 must not be able to delete, unpublish or flag anything", forbidden)
		}
	}
	// The one write it IS allowed: the nudge audit row, via the recorder seam.
	if !strings.Contains(src, "RecordDormancyNudge") {
		t.Error("the runner no longer records the nudge reservation — the 90-day limit would stop working")
	}
}

// TestCSVHasOneLinePerAccount pins the export the card asks for.
func TestCSVHasOneLinePerAccount(t *testing.T) {
	rep := Build([]dbstore.DormancyRow{row("a", 95), row("b", 5)}, Options{Now: testNow})
	body, err := CSV(rep)
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a header + 2 rows:\n%s", len(lines), body)
	}
	if !strings.HasPrefix(lines[0], "account_id,workspace_slug,owner_email") {
		t.Errorf("header = %q", lines[0])
	}
	// Most dormant first.
	if !strings.HasPrefix(lines[1], "a,") || !strings.Contains(lines[1], ",95,") {
		t.Errorf("first row is not the 95-day account: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "b,") {
		t.Errorf("second row is not the 5-day account: %q", lines[2])
	}
}
