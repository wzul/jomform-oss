// Package menufixture seeds and serves a NON-PROD restaurant-menu checkout that
// really renders a radio option group AND an image-choice option group.
//
// Why this exists (card t_2244109a). Two consecutive QA cards could not observe
// an option group on a live deployment: no published form in production carries
// a `radio` or `image-choice` block at all (read-only SQL on 2026-10-31: 69 live
// forms, 2 menu-enabled, 0 with either block), and manufacturing one on a live
// workspace is forbidden — prod data must not be written to verify a flow. Each
// of those cards therefore improvised its own probe and deleted it afterwards,
// so the next one started from zero: one injected the template markup into the
// live page client-side (which proves what a browser does with markup the server
// never sent), the other built a throwaway Go probe over a scratch schema and
// threw it away with the card.
//
// This package is that probe, kept:
//
//   - it owns a SCRATCH SCHEMA of the shared test database (never a new
//     database), named `test_menufixture_fx_p<pid>` so the existing
//     internal/testdb sweeper reclaims it if a run dies without closing;
//   - it applies the real migration set and seeds rows through the real stores,
//     so the page under test is served from a real Postgres, not a map;
//   - it renders through the REAL public handler (httpapi.PublicHandlers) over a
//     real net/http listener, so what a browser loads is what the deployment
//     serves — no client-side injection anywhere in the loop;
//   - it pins the request Host to the fixture's workspace subdomain, because
//     that is how the handler resolves the tenant, and a QA browser then needs
//     no /etc/hosts entry, no DNS and no proxy: the URL it gets is
//     http://127.0.0.1:PORT/<menu-slug>.
//
// The fixture is deliberately CHIP-less. `payment_method:"bank_direct"` on the
// order route answers 201 and stores the option answers; `"chip"` reaches the
// payment service and fails at a DEAD LOOPBACK port (the "payfail" path), never
// at production CHIP — the client is constructed with an explicit
// http://127.0.0.1:1 base URL, because chip.New("") silently defaults to the
// PRODUCTION base URL.
//
// Use it from a test (see fixture_test.go) or from the command
// `api/cmd/menu-fixture`, which serves it and prints its public URL as JSON. The
// browser half — the geometry assertions a human can see — lives in
// e2e/specs/menu-checkout-option-group.spec.ts and is driven by
// scripts/menu-fixture-e2e.sh.
package menufixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi"
	"github.com/wzul/jomform/api/internal/payments"
)

const (
	// DSNEnv is the environment variable the fixture reads when Options.DSN is
	// empty. It is the SAME variable every integration test uses, so a lane that
	// can run the api tests can run this.
	DSNEnv = "JOMFORM_TEST_DATABASE_URL"

	// DefaultMenuSlug / DefaultOrderSlug are the slugs of the two seeded forms.
	DefaultMenuSlug  = "menu-fixture"
	DefaultOrderSlug = "menu-fixture-order"

	// RadioBlockID / ImageChoiceBlockID are the block ids of the two option
	// groups. They are also the field keys the checkout submits, so a caller
	// asserting on the stored answers uses the same names.
	RadioBlockID       = "sd1"
	ImageChoiceBlockID = "ic1"

	// radioLabel / imageChoiceLabel are the merchant-visible question labels
	// ("Choose a size" / "Temperature").
	radioLabel       = "Pilih saiz"
	imageChoiceLabel = "Pilihan suhu"
)

// Choice is one option of a seeded group.
type Choice struct {
	Label string
	Value string
}

// RadioChoices / ImageChoices are the seeded options, in page order.
var (
	RadioChoices = []Choice{
		{Label: "Kecil", Value: "k"},
		{Label: "Besar", Value: "b"},
	}
	ImageChoices = []Choice{
		{Label: "Panas", Value: "panas"},
		{Label: "Sejuk", Value: "sejuk"},
	}
)

// ErrNoDSN is returned by Open when no test-database URL is configured. It is an
// error, not a skip: the fixture cannot serve anything without a database, and a
// caller that quietly carried on would prove nothing about the page.
var ErrNoDSN = fmt.Errorf("%s is unset: the fixture needs the shared test database", DSNEnv)

// Options configures Open.
type Options struct {
	// DSN is the test database URL. Empty falls back to DSNEnv.
	DSN string
	// Schema is the scratch schema to own. Empty uses
	// test_menufixture_fx_p<pid> — the pid keeps concurrent lanes apart and lets
	// internal/testdb's sweeper reclaim a schema left by a killed run.
	Schema string
	// BaseURL is the absolute origin the handler advertises (receipt links,
	// absolute URLs in emails). Empty uses http://127.0.0.1:8080; a server
	// should call SetBaseURL with the address it really bound.
	BaseURL string
	// Log receives the migration log lines. Nil discards them.
	Log *slog.Logger
}

// Fixture is a seeded menu checkout plus the handlers that render it.
type Fixture struct {
	// Schema is the scratch schema the rows live in. The fixture drops it on
	// Close, so nothing is left behind for the next lane to trip over.
	Schema string
	// Workspace is the workspace slug, i.e. the host label of Host.
	Workspace string
	// Host is the Host header every request is pinned to: <workspace>.jomform.com.
	Host string
	// AccountID / ProductIDs identify the seeded rows.
	AccountID  string
	ProductIDs []string
	// MenuSlug / OrderSlug are the slugs of the menu form and of the published
	// form whose field blocks are the checkout's extra option groups.
	MenuSlug  string
	OrderSlug string
	// RadioBlock / ImageChoiceBlock carry the ids the checkout submits under.
	RadioBlock       string
	ImageChoiceBlock string

	fx   *httpapi.PublicHandlers
	pool *pgxpool.Pool
	dsn  string
}

// schemaNameRE is the identifier contract for a scratch schema. It is narrow on
// purpose: the name is interpolated into DROP SCHEMA / CREATE SCHEMA (pgx cannot
// bind an identifier), so it must not be able to carry anything but a plain
// lower-case identifier.
var schemaNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,50}$`)

// DefaultSchema is the schema Open uses when Options.Schema is empty.
func DefaultSchema() string {
	return "test_menufixture_fx_p" + strconv.Itoa(os.Getpid())
}

// Open creates the scratch schema, migrates it, seeds the workspace + products +
// menu form + attached order form, and wires the real public handlers.
//
// The caller owns the returned Fixture and must Close it: Close drops the
// schema. Nothing here touches production, and nothing here is written outside
// the scratch schema of the shared test database.
func Open(ctx context.Context, opt Options) (*Fixture, error) {
	dsn := strings.TrimSpace(opt.DSN)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv(DSNEnv))
	}
	if dsn == "" {
		return nil, ErrNoDSN
	}
	schema := strings.TrimSpace(opt.Schema)
	if schema == "" {
		schema = DefaultSchema()
	}
	if !schemaNameRE.MatchString(schema) {
		return nil, fmt.Errorf("scratch schema %q is not a plain lower-case identifier", schema)
	}
	log := opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	// Admin connection: (re)create the schema. DROP ... CASCADE first so a
	// crashed run's leftovers cannot leak a half-migrated schema into this one.
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect test database: %w", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		return nil, fmt.Errorf("drop scratch schema: %w", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		return nil, fmt.Errorf("create scratch schema: %w", err)
	}

	// Pool whose connections resolve unqualified names to the scratch schema
	// first, then `public` for the shared extensions (citext, pgcrypto).
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	f := &Fixture{Schema: schema, pool: pool, dsn: dsn}
	if err := db.Migrate(ctx, pool, log); err != nil {
		pool.Close()
		_ = dropSchema(ctx, dsn, schema)
		return nil, fmt.Errorf("migrate scratch schema: %w", err)
	}
	if err := f.seed(ctx, opt.BaseURL); err != nil {
		_ = f.Close(ctx)
		return nil, err
	}
	return f, nil
}

// Handler returns the real public routes behind a Host-pinning wrapper. Mutating
// r.Host is exactly what a reverse proxy does for a workspace subdomain, and it
// is what lets a browser reach the fixture on 127.0.0.1 with no hosts entry.
func (f *Fixture) Handler() http.Handler {
	mux := http.NewServeMux()
	f.fx.RegisterRoutes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = f.Host
		mux.ServeHTTP(w, r)
	})
}

// MenuPath is the URL path of the seeded menu page ("/" + MenuSlug).
func (f *Fixture) MenuPath() string { return "/" + f.MenuSlug }

// SetBaseURL sets the absolute origin the handler advertises.
func (f *Fixture) SetBaseURL(u string) { f.fx.BaseURL = strings.TrimRight(u, "/") }

// SaleFieldsByRef returns the stored extra-answer map of the sale with refCode,
// so a caller can assert that the option answers really landed.
func (f *Fixture) SaleFieldsByRef(ctx context.Context, refCode string) (map[string]any, error) {
	sale, err := f.SaleByRef(ctx, refCode)
	if err != nil {
		return nil, err
	}
	return f.fx.Sales.SaleFields(ctx, sale.ID)
}

// SaleByRef returns the sale row for refCode (its TotalSen is integer sen).
func (f *Fixture) SaleByRef(ctx context.Context, refCode string) (*dbstore.Sale, error) {
	return f.fx.Sales.ByRefCode(ctx, refCode)
}

// Close drops the scratch schema and closes the pool. It is safe to call twice.
func (f *Fixture) Close(ctx context.Context) error {
	if f.pool != nil {
		f.pool.Close()
		f.pool = nil
	}
	if f.Schema == "" || f.dsn == "" {
		return nil
	}
	schema := f.Schema
	f.Schema = ""
	return dropSchema(ctx, f.dsn, schema)
}

func dropSchema(ctx context.Context, dsn, schema string) error {
	if !schemaNameRE.MatchString(schema) {
		return fmt.Errorf("refusing to drop %q", schema)
	}
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to drop scratch schema: %w", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		return fmt.Errorf("drop scratch schema %s: %w", schema, err)
	}
	return nil
}

// seed writes the rows. Everything goes through the real stores and real SQL, so
// the page under test is the page a merchant's own data would produce.
func (f *Fixture) seed(ctx context.Context, baseURL string) error {
	tag, err := randomTag()
	if err != nil {
		return err
	}
	f.Workspace = "it-menu-" + tag
	f.Host = f.Workspace + ".jomform.com"
	f.MenuSlug = DefaultMenuSlug
	f.OrderSlug = DefaultOrderSlug
	f.RadioBlock = RadioBlockID
	f.ImageChoiceBlock = ImageChoiceBlockID

	var userID string
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, 'menufixture') RETURNING id`,
		"menufixture-"+tag+"@example.test").Scan(&userID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	var workspaceID string
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1, $2, $3) RETURNING id`,
		userID, "Menu Fixture "+tag, f.Workspace).Scan(&workspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, "Menu Fixture "+tag, "it-menu-acc-"+tag, workspaceID).Scan(&f.AccountID); err != nil {
		return fmt.Errorf("create account: %w", err)
	}

	products := dbstore.NewProductStore(f.pool)
	variations := dbstore.NewProductVariationStore(f.pool)
	seeded := []struct {
		name, desc string
		priceSen   int64
	}{
		{"Nasi Lemak Ayam", "Sambal, telur, kacang", 850},
		{"Teh Tarik", "Panas, kurang manis", 350},
	}
	names := make([]string, 0, len(seeded))
	for _, s := range seeded {
		p := &dbstore.Product{
			AccountID: f.AccountID, Name: s.name, Description: s.desc,
			PriceSen: s.priceSen, Type: dbstore.SaleTypeOneTime, Active: true,
		}
		if err := products.Create(ctx, p); err != nil {
			return fmt.Errorf("create product %q: %w", s.name, err)
		}
		f.ProductIDs = append(f.ProductIDs, p.ID)
		names = append(names, s.name)
	}

	forms := dbstore.NewFormStore(f.pool)

	// The attached order form: its field blocks ARE the checkout's option
	// groups (issue #493 — menu.form_membership is a slug on the menu config,
	// resolved to a published form in the same account).
	orderForm := &dbstore.Form{
		AccountID: f.AccountID,
		Slug:      f.OrderSlug,
		Title:     "Menu fixture — extra order info",
		Status:    dbstore.FormStatusPublished,
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{
					"id": f.RadioBlock, "type": "radio", "value": radioLabel, "required": true,
					"choices": choicesJSON(RadioChoices),
				},
				map[string]any{
					"id": f.ImageChoiceBlock, "type": "image-choice", "value": imageChoiceLabel, "required": true,
					"choices": choicesJSON(ImageChoices),
				},
			},
		},
	}
	if err := forms.Create(ctx, orderForm); err != nil {
		return fmt.Errorf("create attached order form: %w", err)
	}

	// The menu form: one product block per seeded product + the per-form menu
	// config that turns the block list into a menu page, with the order form
	// attached for the extra checkout fields.
	blocks := make([]any, 0, len(f.ProductIDs))
	for i, id := range f.ProductIDs {
		blocks = append(blocks, map[string]any{
			"id": "p" + strconv.Itoa(i+1), "type": "product", "product_id": id, "value": names[i],
		})
	}
	menuForm := &dbstore.Form{
		AccountID: f.AccountID,
		Slug:      f.MenuSlug,
		Title:     "Warung Fixture",
		Status:    dbstore.FormStatusPublished,
		Config: map[string]any{
			"blocks": blocks,
			"menu": map[string]any{
				"enabled":         true,
				"categories":      []any{},
				"order_form_slug": f.OrderSlug,
			},
		},
	}
	if err := forms.Create(ctx, menuForm); err != nil {
		return fmt.Errorf("create menu form: %w", err)
	}

	sales := dbstore.NewSaleStore(f.pool)
	accounts := dbstore.NewAccountStore(f.pool)
	workspaces := dbstore.NewWorkspaceStore(f.pool)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	// A DEAD LOOPBACK port for the payment client, never chip.New(""): the empty
	// string falls back to CHIP's PRODUCTION base URL, and a fixture must not be
	// able to address production payments at all.
	pay := payments.New(chip.New("http://127.0.0.1:1"), accounts, sales,
		dbstore.NewSubscriptionStore(f.pool), []byte("menufixture-decrypt-key-32-bytes"), strings.TrimRight(baseURL, "/"))
	pay.Workspaces = workspaces

	f.fx = &httpapi.PublicHandlers{
		Forms:             forms,
		Sales:             sales,
		Accounts:          accounts,
		Products:          products,
		ProductVariations: variations,
		Workspaces:        workspaces,
		Pay:               pay,
		BaseURL:           strings.TrimRight(baseURL, "/"),
	}
	return nil
}

func choicesJSON(cs []Choice) []any {
	out := make([]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"label": c.Label, "value": c.Value})
	}
	return out
}

func randomTag() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random tag: %w", err)
	}
	return hex.EncodeToString(b), nil
}
