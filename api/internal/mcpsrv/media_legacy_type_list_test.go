package mcpsrv

// t_5f6a5d37: the serve path corrects a legacy code asset's recorded type from
// the object's bytes (a comment-led stylesheet uploaded before t_8881000c was
// stored `.js` / `text/javascript`). The agent-facing surface must agree with
// that, or an agent reading list_media keeps telling the merchant their
// stylesheet is a script — and the merchant keeps chasing the wrong file.
//
// MCP parity (AGENTS.md): list_media and list_deleted_media report media.mime,
// the same record the dashboard library reads, so pinning that the tool output
// follows the corrected row is what keeps the two surfaces in step.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// TestListMediaReportsTheCorrectedType: a legacy row recorded text/javascript
// is listed as text/javascript, and once the serve path has corrected the row
// (MediaStore.CorrectMimeByKey) the tool reports the type the object is served
// with — text/css.
func TestListMediaReportsTheCorrectedType(t *testing.T) {
	if strings.TrimSpace(os.Getenv("JOMFORM_TEST_DATABASE_URL")) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping list_media type parity test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('lmt-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'lmt-acc-' || md5(random()::text), 'lmt-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	media := dbstore.NewMediaStore(pool)
	key := "u/" + accountID + "/legacy-stylesheet.js"
	row := &dbstore.Media{
		AccountID: accountID, Key: key, URL: "https://jomform.com/media/" + key,
		Mime: "text/javascript; charset=utf-8", Size: 62, Source: dbstore.MediaSourceMerchant,
	}
	if err := media.Create(ctx, row); err != nil {
		t.Fatalf("seed media row: %v", err)
	}

	srv := &Server{deps: &Deps{Media: media}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"read"}})

	listed := func() string {
		t.Helper()
		_, out, err := srv.listMedia(callCtx, nil, listMediaIn{})
		if err != nil {
			t.Fatalf("list_media: %v", err)
		}
		items, ok := out["items"].([]mediaOut)
		if !ok {
			t.Fatalf("list_media items have unexpected type %T", out["items"])
		}
		for _, it := range items {
			if it.Key == key {
				return it.Mime
			}
		}
		t.Fatalf("list_media does not include %q (items %+v)", key, items)
		return ""
	}

	// The stale record, before anything served the object.
	if got := listed(); got != "text/javascript; charset=utf-8" {
		t.Fatalf("list_media reported %q for the legacy row, want the recorded text/javascript", got)
	}

	// What GET /media and GET /assets write when they see the bytes.
	if rewritten, err := media.CorrectMimeByKey(ctx, accountID, key, "text/css; charset=utf-8"); err != nil || !rewritten {
		t.Fatalf("correct media.mime: rewritten=%v err=%v", rewritten, err)
	}

	if got := listed(); got != "text/css; charset=utf-8" {
		t.Fatalf("list_media still reports %q after the correction; the agent surface is out of step with the served type", got)
	}
}
