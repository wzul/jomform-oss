package payments

// Issue #606: a notification rule whose `to_email` is `{admin_email}` resolved to
// an EMPTY string, so the rule was dropped (`notify_no_recipient`) and no email
// was ever enqueued.
//
// Root cause: resolveAdminEmail passed the ACCOUNT id to Users.ByID, but
// `users.id` and `accounts.id` are independent UUIDs — the FK is
// `accounts.owner_user_id` (000001_init.up.sql:26-27). So the lookup always
// missed and the recipient resolved to "".
//
// `{admin_email}` is the recipient of the DEFAULT rule EnsureDefault creates for
// every new form (dbstore/form_notification.go:164-179), so a merchant who never
// touched the settings got no email for a sale at all — silently.
//
// The correct owner-based lookup already existed in this package for the receipt
// path: receipt.go:257-271 (resolveMerchantEmail) and :300-313 (notifyMerchant)
// resolve Workspaces.ByID(ref.WorkspaceID) -> owner.Owner -> Users.ByID. These
// tests pin the same rule down for the per-form notification engine.
//
// Integration against a real Postgres + Valkey; skipped when either env is unset.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/testdb"
)

// emailQueueReadyKey is emailqueue's ready-list key (emailqueue.queueKey,
// unexported there). Pinned as a literal so a test can read the RAW queue
// without consuming it — Dequeue blocks, which makes it useless for asserting
// "exactly one job, with this To".
const emailQueueReadyKey = "jf:emailq:jobs"

// issue606Keyspace is a dedicated Valkey DB for this package's queue
// assertions, so FlushDB here never wipes a keyspace another package owns
// (emailqueue uses /3, the email breaker /2, the shared test env /1).
const issue606Keyspace = 11

// issue606Queue returns an email queue on a dedicated keyspace, flushed at the
// start of every test so a previous test's (or run's) ready jobs can never be
// mistaken for this test's. No test in this package runs in parallel, so the
// flush cannot race a sibling.
func issue606Queue(t *testing.T) (*emailqueue.Queue, *goredis.Client) {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping notification integration test")
	}
	u := strings.TrimPrefix(valkeyURL, "valkey://")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[:i]
	}
	opts, err := goredis.ParseURL(fmt.Sprintf("redis://%s/%d", u, issue606Keyspace))
	if err != nil {
		t.Fatalf("parse valkey url: %v", err)
	}
	rdb := goredis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	if ferr := rdb.FlushDB(context.Background()).Err(); ferr != nil {
		t.Fatalf("flush test keyspace: %v", ferr)
	}
	return emailqueue.NewQueue(rdb), rdb
}

// issue606Jobs reads every job on the ready list without claiming it.
func issue606Jobs(t *testing.T, rdb *goredis.Client) []emailqueue.Job {
	t.Helper()
	raw, err := rdb.LRange(context.Background(), emailQueueReadyKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("read email queue: %v", err)
	}
	out := make([]emailqueue.Job, 0, len(raw))
	for _, r := range raw {
		var j emailqueue.Job
		if err := json.Unmarshal([]byte(r), &j); err != nil {
			t.Fatalf("unmarshal queued job %q: %v", r, err)
		}
		out = append(out, j)
	}
	return out
}

// issue606Fixture seeds an owner user + workspace + account (linked to the
// workspace, as WorkspaceStore.Create does) + a published payment-mode form,
// and returns the ids the notification ref needs, plus the form itself (issue
// #597: EnsureDefault takes the form, because which default rule a form gets
// depends on its mode). The owner's email is unique per call so a job's
// recipient can be asserted exactly.
func issue606Fixture(t *testing.T, ctx context.Context) (ownerEmail, userID, workspaceID, accountID string, form *dbstore.Form) {
	t.Helper()
	pool := testdb.Open(t)

	// Derive a unique local part from a random uuid so repeated -count runs and
	// parallel fixtures never collide.
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-owner-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id, email`).Scan(&userID, &ownerEmail); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	form = &dbstore.Form{
		AccountID: accountID, Slug: "it-606-" + accountID[:8], Title: "Order form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{}},
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return ownerEmail, userID, workspaceID, accountID, form
}

// TestIssue606AdminEmailResolvesToTheOwnerUser is the regression: a
// `{admin_email}` recipient must enqueue a form_notification job addressed to
// the workspace owner's account email.
//
// Bite-proof: on the pre-fix code resolveAdminEmail passes the ACCOUNT id to
// Users.ByID, that lookup misses (asserted below as a fixture precondition), no
// recipient is resolved and NOTHING is enqueued — so this test fails.
func TestIssue606AdminEmailResolvesToTheOwnerUser(t *testing.T) {
	ctx := context.Background()
	ownerEmail, userID, workspaceID, accountID, form := issue606Fixture(t, ctx)
	pool := testdb.Open(t)

	// Fixture precondition, asserted rather than assumed: the account id names
	// no user row, so the OLD lookup (Users.ByID(ref.AccountID)) cannot match.
	if _, err := dbstore.NewUserStore(pool).ByID(ctx, accountID); err == nil {
		t.Fatalf("fixture is not discriminating: a user row exists with the ACCOUNT id %s", accountID)
	}
	if u, err := dbstore.NewUserStore(pool).ByID(ctx, userID); err != nil || u.Email != ownerEmail {
		t.Fatalf("fixture owner lookup = %v/%v, want %s", u, err, ownerEmail)
	}

	notif := dbstore.NewFormNotificationStore(pool)
	// The production writer for the default rule every new form is created with
	// (rest_handlers.go:682 calls exactly this).
	if err := notif.EnsureDefault(ctx, form); err != nil {
		t.Fatalf("ensure default notification: %v", err)
	}

	q, rdb := issue606Queue(t)
	svc := &Service{
		Accounts:      dbstore.NewAccountStore(pool),
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Users:         dbstore.NewUserStore(pool),
		Notifications: notif,
		EmailQueue:    q,
	}

	// sale.paid is the event the default rule listens for.
	svc.FireFormNotifications(ctx, NotificationRef{
		FormID: form.ID, WorkspaceID: workspaceID, AccountID: accountID,
		RefCode: "IT606REF", AmountSen: 1500, ItemsSummary: "Tee x1", BuyerName: "Ali",
	}, dbstore.NotifyEventSalePaid)

	jobs := issue606Jobs(t, rdb)
	if len(jobs) != 1 {
		t.Errorf("the default {admin_email} rule enqueued %d job(s), want exactly 1.\n"+
			"Zero means the recipient resolved to \"\" (notify_no_recipient) and the "+
			"merchant's default new-sale email is silently never sent.", len(jobs))
	} else {
		if jobs[0].Type != "form_notification" {
			t.Errorf("job type = %q, want \"form_notification\"", jobs[0].Type)
		}
		if jobs[0].To != ownerEmail {
			t.Errorf("{admin_email} recipient = %q, want the workspace owner's email %q.\n"+
				"users.id and accounts.id are independent UUIDs — the lookup must key on "+
				"accounts.owner_user_id, never on the account id itself.", jobs[0].To, ownerEmail)
		}
		if jobs[0].Subject == "" {
			t.Errorf("job subject is empty; the default rule's subject did not render")
		}
		if jobs[0].AccountID != accountID {
			t.Errorf("job account_id = %q, want %q", jobs[0].AccountID, accountID)
		}
	}

	// The literal-recipient control: the engine, the rendering and the enqueue
	// path are all fine, so a failure above can only be recipient resolution.
	// Asserted against the literal job itself, not the total queue length, so
	// this check is independent of whether the {admin_email} job above fired.
	if err := notif.Create(ctx, &dbstore.FormNotification{
		FormID: form.ID, Name: "literal control", Event: dbstore.NotifyEventResponseSubmitted,
		ToEmail: "organizer@example.test", Subject: "New response", Body: "body", Enabled: true,
	}); err != nil {
		t.Fatalf("create literal rule: %v", err)
	}
	svc.FireFormNotifications(ctx, NotificationRef{
		FormID: form.ID, WorkspaceID: workspaceID, AccountID: accountID,
		RefCode: "IT606CTL", AmountSen: 100, BuyerName: "Siti",
	}, dbstore.NotifyEventResponseSubmitted)

	var literal *emailqueue.Job
	jobs = issue606Jobs(t, rdb)
	for i := range jobs {
		if jobs[i].To == "organizer@example.test" {
			literal = &jobs[i]
		}
	}
	if literal == nil {
		t.Fatalf("the literal recipient was not honoured (queue holds %d job(s): %+v): "+
			"the defect is NOT recipient resolution after all", len(jobs), jobs)
	}
	if literal.Type != "form_notification" {
		t.Errorf("literal job type = %q, want \"form_notification\"", literal.Type)
	}
}

// TestIssue606FieldIDRecipientStillResolves guards the OTHER two recipient
// forms against a regression from the owner-id fix.
func TestIssue606FieldIDRecipientStillResolves(t *testing.T) {
	ctx := context.Background()
	_, _, _, accountID, form := issue606Fixture(t, ctx)
	pool := testdb.Open(t)

	notif := dbstore.NewFormNotificationStore(pool)
	if err := notif.Create(ctx, &dbstore.FormNotification{
		FormID: form.ID, Name: "respondent", Event: dbstore.NotifyEventResponseSubmitted,
		ToEmail: "{email}", Subject: "Thanks {{full_name}}", Body: "{{full_name}} replied", Enabled: true,
	}); err != nil {
		t.Fatalf("create {field_id} rule: %v", err)
	}
	q, rdb := issue606Queue(t)
	svc := &Service{
		Accounts: dbstore.NewAccountStore(pool), Users: dbstore.NewUserStore(pool),
		Notifications: notif, EmailQueue: q,
	}
	svc.FireFormNotifications(ctx, NotificationRef{
		FormID: form.ID, AccountID: accountID,
		Fields: map[string]string{"email": "buyer@example.test", "full_name": "Nurul Huda"},
	}, dbstore.NotifyEventResponseSubmitted)

	jobs := issue606Jobs(t, rdb)
	found := false
	for _, j := range jobs {
		if j.To == "buyer@example.test" {
			found = true
			if j.Subject != "Thanks Nurul Huda" {
				t.Errorf("{{field_id}} placeholder subject = %q, want \"Thanks Nurul Huda\"", j.Subject)
			}
		}
	}
	if !found {
		t.Errorf("a {field_id} recipient no longer resolves to the submitted value; queue holds %+v", jobs)
	}
}

// TestIssue606NewWiresAccounts is the wiring guard. `{admin_email}` needs
// Accounts to map the account id to its owner_user_id, so a Service built
// through the production constructor must carry it — a nil Accounts degrades to
// "no recipient" and the default rule silently delivers nothing again.
//
// The failure mode is a MISSING field in a struct literal, which no type checker
// catches because every field is optional; main.go builds the service with
// exactly this constructor.
func TestIssue606NewWiresAccounts(t *testing.T) {
	svc := New(nil, dbstore.NewAccountStore(testdb.Open(t)), nil, nil, nil, "")
	if svc.Accounts == nil {
		t.Fatalf("payments.New() does not wire Accounts.\n" +
			"resolveAdminEmail resolves the recipient through " +
			"accounts.owner_user_id; without the store every {admin_email} rule — " +
			"including the default one every new form is created with — resolves to " +
			"\"\" and delivers nothing.")
	}
}

// TestIssue606AdminEmailWithoutAccountsDoesNotInventARecipient locks the
// degradation contract: whenever the owner cannot be RESOLVED, the recipient
// must be "" — it must not panic, it must not treat the account id as a user id,
// and it must not fall back to a workspace owner it cannot tie to the account.
func TestIssue606AdminEmailWithoutAccountsDoesNotInventARecipient(t *testing.T) {
	ctx := context.Background()
	// A REAL workspace whose owner exists, so "fell back to the workspace owner"
	// would be observable if the code did it.
	ownerEmail, ownerUserID, workspaceID, accountID, _ := issue606Fixture(t, ctx)
	pool := testdb.Open(t)
	users := dbstore.NewUserStore(pool)
	workspaces := dbstore.NewWorkspaceStore(pool)

	ref := NotificationRef{AccountID: accountID, WorkspaceID: workspaceID}
	for _, tc := range []struct {
		name string
		svc  *Service
		ref  NotificationRef
	}{
		{"Accounts nil, Users + Workspaces wired",
			&Service{Users: users, Workspaces: workspaces}, ref},
		{"Accounts nil, Users nil, Workspaces wired",
			&Service{Workspaces: workspaces}, ref},
		// The account read must FAIL CLOSED too, not just be absent: an account
		// id that names no row (deleted/foreign/typo'd) and a non-UUID id both
		// return an error from AccountStore.ByID. A resolver that swallowed the
		// error and carried on with a zero owner id would look identical on the
		// happy path — these two cases are what make that observable.
		{"Accounts wired, account id names no row",
			&Service{Accounts: dbstore.NewAccountStore(pool), Users: users, Workspaces: workspaces},
			NotificationRef{AccountID: "00000000-0000-0000-0000-000000000000", WorkspaceID: workspaceID}},
		{"Accounts wired, account id is not a UUID",
			&Service{Accounts: dbstore.NewAccountStore(pool), Users: users, Workspaces: workspaces},
			NotificationRef{AccountID: "not-a-uuid", WorkspaceID: workspaceID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("resolveAdminEmail panicked: %v", r)
				}
			}()
			got := tc.svc.resolveNotificationRecipient(ctx, tc.ref, "{admin_email}")
			if got != "" {
				t.Errorf("resolveAdminEmail = %q, want \"\" (the owner is %q and is "+
					"not resolvable from this ref; a missing/unreadable account must "+
					"degrade to no recipient, never invent one)", got, ownerEmail)
			}
		})
	}
	_ = ownerUserID
}
