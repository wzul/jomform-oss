package mcpsrv

// t_0fe14ba5 — MCP parity for the media URL host (AGENTS.md: every dashboard
// behaviour has a matching tool, and the two lanes must answer the same thing).
//
// The defect was in the url the library HANDS OUT: for a code asset it was the
// canonical media host, which /media refuses unless the request Host resolves to
// the key's own account (issue #379) — so View/Copy URL were dead. list_media,
// get_media and upload_media had the same dead url for agents. This pins the MCP
// lane to the account's public host for a code asset, to the canonical host for
// an image, and pins the public-host resolver that both the form url and the
// media url now share.

import (
	"context"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPMediaURLUsesTheAccountPublicHost(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('mcpurl-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	ws, err := sp.Create(ctx, userID, "MCP media url", "mcpurl-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accountID); err != nil {
		t.Fatalf("read account for workspace %s: %v", ws.Slug, err)
	}

	srv := &Server{deps: &Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: sp,
	}}

	cssKey := "u/" + accountID + "/33333333-3333-3333-3333-333333333333.css"
	canonical := "https://jomform.com/media/" + cssKey
	wantCSS := "https://" + ws.Slug + ".jomform.com/media/" + cssKey

	// 1. The tool's url for a code asset is the account's public host.
	if got := srv.merchantMediaURL(ctx, accountID, canonical, "text/css; charset=utf-8"); got != wantCSS {
		t.Fatalf("merchantMediaURL(css) = %q, want %q", got, wantCSS)
	}
	// 2. The row builder list_media/get_media go through does the same — the
	//    stored row's url column stays canonical, so the host is applied on READ
	//    and existing rows are fixed without a data migration.
	row := &dbstore.Media{
		AccountID: accountID, Key: cssKey, URL: canonical,
		Mime: "text/css; charset=utf-8", Size: 18, Source: dbstore.MediaSourceMerchant,
	}
	if out := srv.mediaOutFromRow(ctx, row); out.URL != wantCSS {
		t.Fatalf("mediaOutFromRow(css).URL = %q, want %q (stored url was %q)", out.URL, wantCSS, canonical)
	}
	// 3. Images keep the canonical host — moving them would break every stored
	//    image url and cross-origin embedding.
	pngCanonical := "https://jomform.com/media/u/" + accountID + "/33333333-3333-3333-3333-333333333334.png"
	if got := srv.merchantMediaURL(ctx, accountID, pngCanonical, "image/png"); got != pngCanonical {
		t.Fatalf("merchantMediaURL(image) = %q, want the canonical %q", got, pngCanonical)
	}
	// 4. The form url still resolves the same host after the shared-resolver
	//    refactor (this is the function whose body was folded into
	//    publicHostForID, so it is the regression guard for that move).
	if got, want := srv.formPublicURLForAccount(ctx, accountID, "my-form"), "https://"+ws.Slug+".jomform.com/my-form"; got != want {
		t.Fatalf("formPublicURLForAccount = %q, want %q", got, want)
	}
}
