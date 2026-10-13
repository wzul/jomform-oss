// Feature-parity tools — dashboard operations exposed to merchant agents.
package mcpsrv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// slugUnsafe matches any character that is not URL-safe for a form slug.
var slugUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// ── Products update ──────────────────────────────────────────────────────

type updateProductIn struct {
	ProductID           string        `json:"product_id" jsonschema:"ID produk"`
	Name                string        `json:"name,omitempty" jsonschema:"Nama baharu (optional)"`
	Description         string        `json:"description,omitempty" jsonschema:"Penerangan baharu (optional)"`
	PriceSen            *int64        `json:"price_sen,omitempty" jsonschema:"Harga baharu dalam sen (optional)"`
	Images              []string      `json:"images,omitempty" jsonschema:"Gambar (URL) — set penuh; kosong = kosongkan" `
	Stock               *int          `json:"stock,omitempty" jsonschema:"Stok baharu (optional); omit = tak berubah" `
	Active              *bool         `json:"active,omitempty" jsonschema:"Aktifkan/Nyahaktif"`
	Type                string        `json:"type,omitempty" jsonschema:"one_time atau subscription (optional)"`
	BillingPeriod       string        `json:"billing_period,omitempty" jsonschema:"month atau year untuk subscription (optional)"`
	TrialDays           *int          `json:"trial_days,omitempty" jsonschema:"Hari percubaan (subscription; optional)"`
	Variations          []variationIn `json:"variations,omitempty" jsonschema:"Set penuh variasi (name, price_sen, stock, images, sort_order). Kosong = produk simple; omit = tak berubah."`
	SubscriptionCharges **int         `json:"subscription_charges,omitempty" jsonschema:"Bilangan caj (subscription; omit = tak berubah, null = infinite, N = caj N kali)"`
	// round 13c: next-charge date policy after a SUCCESSFUL charge.
	ScheduleMode *string `json:"schedule_mode,omitempty" jsonschema:"Polisi tarikh caj seterusnya: from_payment (dari saat berjaya, lalai) | catch_up (ikut tarikh asal; period tertinggal tetap dicaj) | skip_missed (ikut tarikh asal; period tertinggal dilangkau)"`
}

func (s *Server) updateProduct(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProductIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireProductWrite(ctx, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	var namePtr *string
	if in.Name != "" {
		namePtr = &in.Name
	}
	var descPtr *string
	if in.Description != "" {
		descPtr = &in.Description
	}
	stockPtr := in.Stock
	if stockPtr != nil && *stockPtr < 0 {
		*stockPtr = 0
	}
	var typPtr *string
	if in.Type != "" {
		if in.Type != dbstore.SaleTypeOneTime && in.Type != dbstore.SaleTypeSubscription {
			return nil, nil, fmt.Errorf("type mesti one_time|subscription")
		}
		typPtr = &in.Type
	}
	var periodPtr *string
	if in.BillingPeriod != "" {
		if in.BillingPeriod != "month" && in.BillingPeriod != "year" {
			return nil, nil, fmt.Errorf("billing_period mesti month|year")
		}
		periodPtr = &in.BillingPeriod
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		return nil, nil, fmt.Errorf("trial_days mesti >= 0")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges != nil && **in.SubscriptionCharges < 1 {
		return nil, nil, fmt.Errorf("subscription_charges mesti >= 1 (atau null untuk infinite)")
	}
	var imagesPtr *[]string
	if in.Images != nil {
		imagesPtr = &in.Images
	}
	// Issue #501: product images (and variation images) render into
	// buyer-facing `<img src>` attributes, so a non-https scheme is rejected
	// here with the ONE shared rule. nil = keep the stored images (allowed);
	// an explicit list is validated in full.
	if imagesPtr != nil && !urlguard.ValidList(*imagesPtr) {
		return nil, nil, fmt.Errorf("%s", urlguard.InvalidMessage("images"))
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	// Issue #623: also validates each variation's own subscription cycle.
	for _, v := range in.Variations {
		if err := validateVariationIn(v); err != nil {
			return nil, nil, err
		}
	}
	// round 13c: validate the schedule mode before it reaches the store.
	if in.ScheduleMode != nil && *in.ScheduleMode != "" && !payments.ValidScheduleMode(*in.ScheduleMode) {
		return nil, nil, fmt.Errorf("schedule_mode tidak sah: %q (from_payment|catch_up|skip_missed)", *in.ScheduleMode)
	}
	if err := s.deps.Products.Update(ctx, acc, in.ProductID, namePtr, descPtr,
		in.PriceSen, imagesPtr, stockPtr, in.Active, typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges,
		in.ScheduleMode); err != nil {
		return nil, nil, err
	}
	// Wholesale-replace variations when the caller sent them (nil = keep).
	if in.Variations != nil && s.deps.ProductVariations != nil {
		if err := s.deps.ProductVariations.ReplaceAll(ctx, in.ProductID,
			toStoreVariations(in.Variations)); err != nil {
			return nil, nil, err
		}
	}
	return nil, map[string]string{"status": "updated", "product_id": in.ProductID}, nil
}

// listProductVariationsIn is the input for the list_product_variations tool.
type listProductVariationsIn struct {
	ProductID string `json:"product_id" jsonschema:"ID produk"`
}

// listProductVariations returns a product's variations (per-variation
// image/price/stock) — MCP parity for the dashboard editor (issue #151).
func (s *Server) listProductVariations(
	ctx context.Context, _ *mcp.CallToolRequest, in listProductVariationsIn,
) (*mcp.CallToolResult, *listOutput[variationOut], error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if in.ProductID == "" {
		return nil, nil, fmt.Errorf("product_id wajib")
	}
	// Ownership check: the product must belong to the account.
	if _, err := s.deps.Products.Get(ctx, acc, in.ProductID); err != nil {
		return nil, nil, fmt.Errorf("produk tidak dijumpai")
	}
	if s.deps.ProductVariations == nil {
		return nil, newListOut([]variationOut{}), nil
	}
	vs, err := s.deps.ProductVariations.ListByProduct(ctx, in.ProductID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, newListOut(toVariationOuts(vs)), nil
}

// ── Product variation writes (issue #623) ─────────────────────────────────
//
// MCP FEATURE PARITY (AGENTS.md): the dashboard could always edit a product's
// variations — the editor saves them wholesale — but an agent could only LIST
// them. That made the MCP promise ("control everything from your agent") false
// for variations. These three tools close the gap:
//
//	create_product_variation  add one variation
//	update_product_variation  edit one (including clearing a cycle field)
//	delete_product_variation  remove one
//
// update_product's `variations[]` wholesale replace is untouched and still the
// right tool for a full re-list; these act on ONE row, so an agent never has to
// re-send a set it did not read.

// createVariationIn is the input for create_product_variation.
type createVariationIn struct {
	ProductID string `json:"product_id" jsonschema:"ID produk"`
	variationIn
}

// createProductVariation adds ONE variation to a product (issue #623).
func (s *Server) createProductVariation(
	ctx context.Context, _ *mcp.CallToolRequest, in createVariationIn,
) (*mcp.CallToolResult, variationOut, error) {
	if in.ProductID == "" {
		return nil, variationOut{}, fmt.Errorf("product_id wajib")
	}
	if s.deps.ProductVariations == nil {
		return nil, variationOut{}, fmt.Errorf("variasi produk tidak tersedia")
	}
	if _, err := s.requireProductWrite(ctx, in.ProductID); err != nil {
		return nil, variationOut{}, err
	}
	if err := validateVariationIn(in.variationIn); err != nil {
		return nil, variationOut{}, err
	}
	v := toStoreVariations([]variationIn{in.variationIn})[0]
	v.ProductID = in.ProductID
	if err := s.deps.ProductVariations.Create(ctx, v); err != nil {
		return nil, variationOut{}, fmt.Errorf("db: %w", err)
	}
	return nil, toVariationOut(v), nil
}

// updateVariationIn is the input for update_product_variation. Every field is
// optional (nil = leave unchanged); the clear_* flags reset a cycle field back
// to "not set", which is what makes the variation inherit the product again —
// without them a cleared field could never be told apart from a set one.
type updateVariationIn struct {
	ProductID   string    `json:"product_id" jsonschema:"ID produk"`
	VariationID string    `json:"variation_id" jsonschema:"ID variasi"`
	Name        *string   `json:"name,omitempty" jsonschema:"Nama baharu (optional)"`
	PriceSen    *int64    `json:"price_sen,omitempty" jsonschema:"Harga baharu dalam SEN (optional)"`
	Stock       *int      `json:"stock,omitempty" jsonschema:"Stok baharu (optional)"`
	Images      *[]string `json:"images,omitempty" jsonschema:"Gambar (URL mesti https://) — set penuh; kosong = kosongkan"`

	// Issue #623: the variation's own subscription cycle.
	BillingPeriod            *string `json:"billing_period,omitempty" jsonschema:"month | year. Kosongkan dengan clear_billing_period=true untuk warisi produk semula."`
	TrialDays                *int    `json:"trial_days,omitempty" jsonschema:"Hari percubaan. Kosongkan dengan clear_trial_days=true."`
	SubscriptionCharges      *int    `json:"subscription_charges,omitempty" jsonschema:"Bilangan caj. Kosongkan dengan clear_subscription_charges=true."`
	ClearBillingPeriod       bool    `json:"clear_billing_period,omitempty" jsonschema:"true = buang kitaran variasi ini supaya ia warisi produk semula"`
	ClearTrialDays           bool    `json:"clear_trial_days,omitempty" jsonschema:"true = buang hari percubaan variasi supaya ia warisi produk semula"`
	ClearSubscriptionCharges bool    `json:"clear_subscription_charges,omitempty" jsonschema:"true = buang bilangan caj variasi supaya ia warisi produk semula"`
}

// updateProductVariation edits ONE variation (issue #623). A cleared cycle field
// goes back to NULL, which is the "inherit the product" state.
func (s *Server) updateProductVariation(
	ctx context.Context, _ *mcp.CallToolRequest, in updateVariationIn,
) (*mcp.CallToolResult, variationOut, error) {
	if in.ProductID == "" || in.VariationID == "" {
		return nil, variationOut{}, fmt.Errorf("product_id dan variation_id wajib")
	}
	if s.deps.ProductVariations == nil {
		return nil, variationOut{}, fmt.Errorf("variasi produk tidak tersedia")
	}
	if _, err := s.requireProductWrite(ctx, in.ProductID); err != nil {
		return nil, variationOut{}, err
	}
	if in.Name != nil && *in.Name == "" {
		return nil, variationOut{}, fmt.Errorf("nama variasi tidak boleh kosong")
	}
	if in.PriceSen != nil && *in.PriceSen < 0 {
		return nil, variationOut{}, fmt.Errorf("variation price_sen mesti >= 0")
	}
	if in.Stock != nil && *in.Stock < 0 {
		return nil, variationOut{}, fmt.Errorf("variation stok mesti >= 0")
	}
	if in.Images != nil && !urlguard.ValidList(*in.Images) {
		return nil, variationOut{}, fmt.Errorf("%s", urlguard.InvalidMessage("variations[].images"))
	}
	// Issue #623: an unknown period is rejected, not silently dropped.
	if in.BillingPeriod != nil && !in.ClearBillingPeriod &&
		*in.BillingPeriod != "" && *in.BillingPeriod != "month" && *in.BillingPeriod != "year" {
		return nil, variationOut{}, fmt.Errorf("variation billing_period mesti month|year")
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		return nil, variationOut{}, fmt.Errorf("variation trial_days mesti >= 0")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges < 1 {
		return nil, variationOut{}, fmt.Errorf("variation subscription_charges mesti >= 1")
	}
	if err := s.deps.ProductVariations.Update(ctx, in.ProductID, in.VariationID,
		in.Name, in.PriceSen, &in.Stock, in.Images, nil,
		in.BillingPeriod, in.ClearBillingPeriod,
		in.TrialDays, in.ClearTrialDays,
		in.SubscriptionCharges, in.ClearSubscriptionCharges); err != nil {
		if err == dbstore.ErrVariationNotFound {
			return nil, variationOut{}, fmt.Errorf("variasi tidak dijumpai untuk produk ini")
		}
		return nil, variationOut{}, fmt.Errorf("db: %w", err)
	}
	v, err := s.deps.ProductVariations.GetByProduct(ctx, in.ProductID, in.VariationID)
	if err != nil {
		return nil, variationOut{}, fmt.Errorf("db: %w", err)
	}
	return nil, toVariationOut(v), nil
}

// deleteVariationIn is the input for delete_product_variation.
type deleteVariationIn struct {
	ProductID   string `json:"product_id" jsonschema:"ID produk"`
	VariationID string `json:"variation_id" jsonschema:"ID variasi"`
}

// deleteProductVariation removes ONE variation from a product (issue #623),
// scoped to that product so an agent cannot delete a variation out of another
// product's set. Deleting the LAST variation turns the product back into a
// simple product (the editor's own rule: >=1 variation = variable).
func (s *Server) deleteProductVariation(
	ctx context.Context, _ *mcp.CallToolRequest, in deleteVariationIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.ProductID == "" || in.VariationID == "" {
		return nil, nil, fmt.Errorf("product_id dan variation_id wajib")
	}
	if s.deps.ProductVariations == nil {
		return nil, nil, fmt.Errorf("variasi produk tidak tersedia")
	}
	if _, err := s.requireProductWrite(ctx, in.ProductID); err != nil {
		return nil, nil, err
	}
	if err := s.deps.ProductVariations.Delete(ctx, in.ProductID, in.VariationID); err != nil {
		if err == dbstore.ErrVariationNotFound {
			return nil, nil, fmt.Errorf("variasi tidak dijumpai untuk produk ini")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{
		"status": "deleted", "product_id": in.ProductID, "variation_id": in.VariationID,
	}, nil
}

// variationOut is the MCP output shape for one product variation.
//
// Issue #623: the subscription cycle fields are OMITTED when the variation has
// none, so an agent can tell "not set → inherits the product" from "set to the
// product's value". A zero-filled output would erase that distinction.
type variationOut struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	PriceSen  int64    `json:"price_sen"`
	Stock     *int     `json:"stock,omitempty"`
	Images    []string `json:"images,omitempty"`
	SortOrder int      `json:"sort_order"`
	// Issue #623: present only when the variation carries its own cycle.
	BillingPeriod       string `json:"billing_period,omitempty"`
	TrialDays           *int   `json:"trial_days,omitempty"`
	SubscriptionCharges *int   `json:"subscription_charges,omitempty"`
}

// toVariationOuts maps store variations to the MCP output shape.
func toVariationOuts(vs []*dbstore.ProductVariation) []variationOut {
	if len(vs) == 0 {
		return nil
	}
	out := make([]variationOut, 0, len(vs))
	for _, v := range vs {
		out = append(out, toVariationOut(v))
	}
	return out
}

// toVariationOut maps one store variation (see variationOut for the
// omitted-means-inherit rule).
func toVariationOut(v *dbstore.ProductVariation) variationOut {
	return variationOut{
		ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
		Images: v.Images, SortOrder: v.SortOrder,
		BillingPeriod: v.BillingPeriod, TrialDays: v.TrialDays,
		SubscriptionCharges: v.Charges,
	}
}

// ── Forms upsert + publish + delete ─────────────────────────────────────

// chipMappingFromConfig reads the form's merchant-configured CHIP field
// mapping (issue #186): a map of CHIP client field → form block id. Returns
// nil when the merchant hasn't configured one (implicit default applies).
func chipMappingFromConfig(f *dbstore.Form) map[string]string {
	if f == nil || f.Config == nil {
		return nil
	}
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return nil
	}
	var cfg struct {
		ChipMapping map[string]string `json:"chip_mapping"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.ChipMapping
}

// getFormChipMappingIn is the input for get_form_chip_mapping (issue #186).
type getFormChipMappingIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// getFormChipMapping returns the form's merchant-configured CHIP field
// mapping (CHIP client field → form block id). When the merchant hasn't set
// one, the implicit default mapping is returned (name→full_name, email→email,
// phone→phone), matching the dashboard's default behaviour.
func (s *Server) getFormChipMapping(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormChipMappingIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	mapping := chipMappingFromConfig(f)
	if mapping == nil {
		mapping = map[string]string{}
	}
	return nil, mapping, nil
}

// setFormChipMappingIn is the input for set_form_chip_mapping (issue #186).
type setFormChipMappingIn struct {
	FormID  string            `json:"form_id" jsonschema:"ID borang"`
	Mapping map[string]string `json:"mapping" jsonschema:"Peta medan CHIP → id blok borang, cth {\"full_name\":\"block_abc\",\"email\":\"block_def\"}. Kosong = guna lalai (name→full_name, email→email, phone→phone)."`
}

// setFormChipMapping stores the form's CHIP field mapping in its config
// (issue #186). An empty mapping clears it, reverting to the implicit default.
func (s *Server) setFormChipMapping(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormChipMappingIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	cfg := orEmptyMap(f.Config)
	if in.Mapping == nil || len(in.Mapping) == 0 {
		delete(cfg, "chip_mapping")
	} else {
		cfg["chip_mapping"] = in.Mapping
	}
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, f.Title, cfg, nil); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "ok", "form_id": in.FormID}, nil
}

type upsertFormIn struct {
	FormID string         `json:"form_id,omitempty" jsonschema:"Kosong = borang baru; isi = kemaskini"`
	Slug   string         `json:"slug" jsonschema:"Slug URL unik"`
	Title  string         `json:"title" jsonschema:"Tajuk borang"`
	Config map[string]any `json:"config,omitempty" jsonschema:"Block config — ambil contoh dari get_form"`
	// Issue #254: optional custom styling/theme (colors, fonts, logo, layout,
	// spacing, custom_css, custom_js). Omit to leave unchanged; pass {} to clear.
	Theme map[string]any `json:"theme,omitempty" jsonschema:"Gaya/branding tersuai (pilihan): primary, background, text, card_background, card_border_radius, font_family, logo_url, layout, spacing, custom_css, custom_js (JS tersuai — dikawal pemilik + diimbas)"`
	// Issue #207: form restrictions (schedule + entry limit). Omit to leave
	// unchanged; pass explicit nulls to clear.
	StartAt        *time.Time `json:"start_at,omitempty" jsonschema:"Masa mula jadual (kosong = tiada)"`
	EndAt          *time.Time `json:"end_at,omitempty" jsonschema:"Masa tamat jadual (kosong = tiada)"`
	EntryLimit     *int       `json:"entry_limit,omitempty" jsonschema:"Had penyertaan (kosong = tiada)"`
	PendingMessage string     `json:"pending_message,omitempty" jsonschema:"Mesej sebelum mula (kosong = lalai)"`
	ExpiredMessage string     `json:"expired_message,omitempty" jsonschema:"Mesej selepas tamat (kosong = lalai)"`
	LimitMessage   string     `json:"limit_message,omitempty" jsonschema:"Mesej had tercapai (kosong = lalai)"`
	// Issue #208: per-form CHIP feeds (multiple, with conditional logic).
	// Omit to leave unchanged; pass an empty array to clear all feeds.
	ChipFeeds []chipFeedIn `json:"chip_feeds,omitempty" jsonschema:"Senarai feed CHIP borang. Feed tanpa condition = lalai (sentiasa padan). Feed pertama yang padan dipakai."`
	// Issue #193: per-form notification rules. Omit to leave unchanged; pass
	// an explicit list to replace the form's notifications wholesale.
	Notifications []notificationIn `json:"notifications,omitempty" jsonschema:"Peraturan notifikasi emel (ganti semua bila diisi)"`
	// Issue #179: form confirmations (Gravity-Forms-style). Omit to leave
	// unchanged; pass an empty array to reset to just the default.
	Confirmations []confirmationIn `json:"confirmations,omitempty" jsonschema:"Pengesahan borang (ganti semua; lalai sentiasa dikekalkan)"`
	// Issue #255: fully-custom HTML response-only form. When set, the form
	// becomes a response-mode custom form: the agent-authored HTML body is
	// rendered in a sandboxed iframe (CSP + sanitized) and only the
	// registered custom_fields are recorded on submit. Omit to leave
	// unchanged; pass an empty string to clear the custom form.
	CustomHTML   string             `json:"custom_html,omitempty" jsonschema:"HTML badan borang tersuai (response-only, sandboxed iframe). Kosong = buang mod tersuai."`
	CustomFields []customform.Field `json:"custom_fields,omitempty" jsonschema:"Kontrak pendaftaran medan: [{field_id, type, label, required, validation}]. Input HTML mesti bawa field_id sebagai name."`
}

func (s *Server) upsertForm(
	ctx context.Context, _ *mcp.CallToolRequest, in upsertFormIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: creating a new form lands in the caller's currently-scoped
	// account (workspace_id/account_id or the default/first of the user's workspaces);
	// updating resolves + RBAC-gates the owning account.
	acc := ""
	var err error
	if in.FormID == "" {
		acc, err = s.requireWriteRole(ctx)
		if err != nil {
			return nil, nil, err
		}
	} else {
		acc, err = s.requireFormWrite(ctx, in.FormID)
		if err != nil {
			return nil, nil, err
		}
	}
	if in.Title == "" {
		return nil, nil, errors.New("title wajib")
	}
	if in.StartAt != nil && in.EndAt != nil && in.EndAt.Before(*in.StartAt) {
		return nil, nil, errors.New("tarikh tamat mesti selepas tarikh mula")
	}
	if in.EntryLimit != nil && *in.EntryLimit < 0 {
		return nil, nil, errors.New("had penyertaan tidak sah")
	}
	// Issue #501: upsert_form takes the config and theme maps verbatim from
	// the agent. The builder image-block src (config.blocks[].src), the
	// product snapshot images (config.products[].images) and the theme's
	// logo_url all render into buyer-facing `<img src>` attributes, so an
	// unsafe scheme is rejected here with the ONE shared rule.
	if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("theme." + field))
	}
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("config." + field))
	}
	// Card t_8fcb546d: the same present-but-not-array `config.blocks` refusal the
	// REST form routes apply — the MCP write path stores config verbatim too, and
	// a non-array blocks makes the gallery-metadata predicate raise SQLSTATE
	// 22023 for every id in a bulk publish.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidShapeMessage("config."+field, want))
	}
	if in.FormID == "" {
		// Issue #176: auto-generate a unique slug when the merchant leaves it
		// blank (slugified title + random suffix on collision).
		slug := strings.ToLower(strings.TrimSpace(in.Slug))
		if slug == "" {
			slug = s.uniqueFormSlug(ctx, acc, in.Title)
		}
		f := &dbstore.Form{
			AccountID: acc,
			Slug:      slug,
			Title:     in.Title,
			Config:    orEmptyMap(in.Config),
			Theme:     orEmptyMap(in.Theme), // never NULL: theme is NOT NULL
			Status:    dbstore.FormStatusDraft,
		}
		if err := s.deps.Forms.Create(ctx, f); err != nil {
			return nil, nil, err
		}
		// Issue #193: pre-create the default notification for the form's mode
		// — sale.paid for a payment form, response.submitted for a response
		// form (issue #597). Best-effort.
		if s.deps.Notifications != nil {
			_ = s.deps.Notifications.EnsureDefault(ctx, f)
		}
		// Apply restrictions on create when provided.
		if in.StartAt != nil || in.EndAt != nil || in.EntryLimit != nil ||
			in.PendingMessage != "" || in.ExpiredMessage != "" || in.LimitMessage != "" {
			if err := s.deps.Forms.UpdateRestrictions(ctx, acc, f.ID,
				in.StartAt, in.EndAt, in.EntryLimit, in.PendingMessage, in.ExpiredMessage, in.LimitMessage); err != nil {
				return nil, nil, err
			}
		}
		// Issue #208: apply CHIP feeds on create when provided.
		if in.ChipFeeds != nil {
			feeds, err := s.toChipFeeds(ctx, acc, f.ID, in.ChipFeeds)
			if err != nil {
				return nil, nil, err
			}
			if err := s.deps.Forms.SetChipFeeds(ctx, acc, f.ID, feeds); err != nil {
				return nil, nil, err
			}
		}
		// Issue #179: apply confirmations on create when provided.
		if in.Confirmations != nil {
			if err := s.applyConfirmations(ctx, f.ID, in.Confirmations); err != nil {
				return nil, nil, err
			}
		}
		// Issue #193: apply notifications when provided on create.
		if len(in.Notifications) > 0 {
			if err := s.applyFormNotifications(ctx, f.ID, in.Notifications); err != nil {
				return nil, nil, err
			}
		}
		// Issue #255: apply the custom-HTML response form when provided.
		if in.CustomHTML != "" || in.CustomFields != nil {
			if err := applyCustomFormConfig(f.Config, in.CustomHTML, in.CustomFields); err != nil {
				return nil, nil, err
			}
			if err := s.deps.Forms.Update(ctx, acc, f.ID, f.Title, f.Config, nil); err != nil {
				return nil, nil, err
			}
		}
		return nil, map[string]string{"status": "created", "form_id": f.ID, "slug": f.Slug}, nil
	}
	// Update store treats nil maps as "leave unchanged", so an omitted
	// config (and the nil theme below) keep the stored values intact.
	// Issue #255: when a custom form is provided on update, merge it into the
	// existing config (so blocks/other settings are preserved) and pass the
	// merged config to Update.
	if in.CustomHTML != "" || in.CustomFields != nil {
		existing, gerr := s.deps.Forms.GetByID(ctx, acc, in.FormID)
		if gerr != nil {
			return nil, nil, errors.New("borang tidak dijumpai")
		}
		merged := orEmptyMap(existing.Config)
		if err := applyCustomFormConfig(merged, in.CustomHTML, in.CustomFields); err != nil {
			return nil, nil, err
		}
		in.Config = merged
	}
	// Issue #388: power-user custom JS in the theme is owner-gated (requireFormWrite
	// above) and scanned with the deterministic code-asset scan (#364) so obviously
	// malicious JS is never served on the buyer's page.
	if in.Theme != nil {
		if js, ok := in.Theme["custom_js"].(string); ok {
			if reason := customform.ScanCustomJS(js); reason != "" {
				return nil, nil, errors.New("custom_js ditolak: " + reason)
			}
		}
	}
	// Issue #501: re-validate the update's theme/config with the shared rule
	// (the update path below is reached independently of the create-only
	// checks above). A nil map means "leave unchanged" and is skipped.
	if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("theme." + field))
	}
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("config." + field))
	}
	// Card t_8fcb546d: same present-but-not-array `config.blocks` refusal as the
	// create path above.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidShapeMessage("config."+field, want))
	}
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, in.Title, in.Config, in.Theme); err != nil {
		return nil, nil, err
	}
	// A slug supplied on the UPDATE path renames the form, exactly as the
	// dashboard's slug field does (same FormStore.RenameSlug, so the same
	// validation, the same per-workspace uniqueness and the same temporary 301
	// for the old URL). The tool's own description documents `slug` as "Slug URL
	// unik" and it was honoured only on create: an agent told to "set the slug"
	// got back {"status":"updated"} and no change, which is the carded defect.
	//
	// An OMITTED slug (empty) keeps the stored value, matching config/theme's
	// contract, and renaming to the slug the form already has is a no-op. A
	// collision is ErrSlugTaken, surfaced to the agent, never a silent no-op.
	if in.Slug != "" {
		if _, err := s.deps.Forms.RenameSlug(ctx, acc, in.FormID, in.Slug); err != nil {
			return nil, nil, err
		}
	}
	// Report the form's slug as it now STANDS, read back rather than echoed from
	// the request, so the value the agent is told it owns cannot disagree with
	// what was actually written.
	slug := in.Slug
	if f, gerr := s.deps.Forms.GetByID(ctx, acc, in.FormID); gerr == nil && f != nil {
		slug = f.Slug
	}
	// Apply restrictions on update when provided.
	if in.StartAt != nil || in.EndAt != nil || in.EntryLimit != nil ||
		in.PendingMessage != "" || in.ExpiredMessage != "" || in.LimitMessage != "" {
		if err := s.deps.Forms.UpdateRestrictions(ctx, acc, in.FormID,
			in.StartAt, in.EndAt, in.EntryLimit, in.PendingMessage, in.ExpiredMessage, in.LimitMessage); err != nil {
			return nil, nil, err
		}
	}
	// Issue #208: apply CHIP feeds on update when provided (nil = unchanged).
	if in.ChipFeeds != nil {
		feeds, err := s.toChipFeeds(ctx, acc, in.FormID, in.ChipFeeds)
		if err != nil {
			return nil, nil, err
		}
		if err := s.deps.Forms.SetChipFeeds(ctx, acc, in.FormID, feeds); err != nil {
			return nil, nil, err
		}
	}
	// Issue #193: apply notifications when provided on update.
	if len(in.Notifications) > 0 {
		if err := s.applyFormNotifications(ctx, in.FormID, in.Notifications); err != nil {
			return nil, nil, err
		}
	}
	// Issue #179: apply confirmations on update when provided.
	if in.Confirmations != nil {
		if err := s.applyConfirmations(ctx, in.FormID, in.Confirmations); err != nil {
			return nil, nil, err
		}
	}
	return nil, map[string]string{"status": "updated", "form_id": in.FormID, "slug": slug}, nil
}

// applyFormNotifications replaces a form's notification rules wholesale
// (issue #193). Rules with an id are updated, rules without an id are created,
// and existing rules not in the list are deleted.
func (s *Server) applyFormNotifications(ctx context.Context, formID string, in []notificationIn) error {
	if s.deps.Notifications == nil {
		return errors.New("notifications tidak tersedia")
	}
	for _, n := range in {
		if !validNotificationEvent(n.Event) {
			return errors.New("event tidak sah: " + n.Event)
		}
	}
	existing, err := s.deps.Notifications.ListByForm(ctx, formID)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, n := range in {
		enabled := true
		if n.Enabled != nil {
			enabled = *n.Enabled
		}
		rec := &dbstore.FormNotification{
			FormID: formID, Name: n.Name, Event: n.Event, ToEmail: n.ToEmail,
			Subject: n.Subject, Body: n.Body, Condition: n.Condition, Enabled: enabled,
		}
		if n.ID == "" {
			if err := s.deps.Notifications.Create(ctx, rec); err != nil {
				return err
			}
		} else {
			keep[n.ID] = true
			if err := s.deps.Notifications.Update(ctx, formID, n.ID, rec); err != nil {
				return err
			}
		}
	}
	for _, e := range existing {
		if !keep[e.ID] {
			if err := s.deps.Notifications.Delete(ctx, formID, e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyConfirmations validates and saves a form's confirmations (issue #179).
// The default confirmation is always preserved server-side.
func (s *Server) applyConfirmations(ctx context.Context, formID string, in []confirmationIn) error {
	confs := make([]*dbstore.Confirmation, 0, len(in))
	for _, c := range in {
		if strings.TrimSpace(c.Name) == "" {
			return errors.New("nama pengesahan wajib")
		}
		if c.Type != dbstore.ConfirmationTypeMessage && c.Type != dbstore.ConfirmationTypeRedirect {
			return errors.New("jenis pengesahan tidak sah")
		}
		if c.Type == dbstore.ConfirmationTypeRedirect {
			// Issue #549: the ONE shared URL rule (urlguard), not a hand-rolled
			// strings.HasPrefix copy — see the REST handler for why (the copy
			// rejected valid "HTTPS://…" and whitespace-padded values). A redirect
			// requires a target, and urlguard.Valid("") is true by design.
			if strings.TrimSpace(c.RedirectURL) == "" || !urlguard.Valid(c.RedirectURL) {
				return errors.New(urlguard.InvalidMessage("redirect_url"))
			}
		}
		cc := &dbstore.Confirmation{
			ID: c.ID, Name: c.Name, Type: c.Type, Message: c.Message,
			RedirectURL: c.RedirectURL, IsDefault: c.IsDefault,
		}
		if c.Condition != nil {
			cc.Condition = &dbstore.ConfirmationCondition{
				FieldID: c.Condition.FieldID, Op: c.Condition.Op, Value: c.Condition.Value,
			}
		}
		confs = append(confs, cc)
	}
	return s.deps.Forms.SaveConfirmations(ctx, formID, confs)
}

// applyCustomFormConfig merges a custom-HTML response form (issue #255) into
// a form's config map. It sets mode=response, stores the agent-authored HTML
// body and the field-registration contract under the custom_html key, and
// validates the contract. An empty html clears the custom form (falls back to
// the block renderer).
func applyCustomFormConfig(cfg map[string]any, html string, fields []customform.Field) error {
	cc := &customform.Config{HTML: html, Fields: fields}
	if err := customform.Validate(cc); err != nil {
		return err
	}
	cfg["mode"] = "response"
	if strings.TrimSpace(html) == "" {
		delete(cfg, customform.ConfigKey)
		return nil
	}
	cfg[customform.ConfigKey] = map[string]any{
		"html":   html,
		"fields": fields,
	}
	return nil
}

// uniqueFormSlug generates a URL-safe, per-workspace-unique slug for a new form
// (issue #176). Prefers a slugified title, falls back to a random token, and
// appends a random suffix until the slug is free.
func (s *Server) uniqueFormSlug(ctx context.Context, accountID, title string) string {
	base := strings.ToLower(strings.TrimSpace(title))
	base = slugUnsafe.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "f-" + randHex(4)
	}
	if len(base) > 40 {
		base = base[:40]
	}
	base = strings.Trim(base, "-")
	if base == "" {
		base = "f-" + randHex(4)
	}
	for i := 0; i < 5; i++ {
		taken, err := s.deps.Forms.SlugExists(ctx, accountID, base)
		if err != nil || !taken {
			return base
		}
		base = base + "-" + randHex(2)
	}
	return base + "-" + randHex(4)
}

// randHex returns n random bytes hex-encoded (lowercase).
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// orEmptyMap normalises a nil map to an empty one.
func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// jsonValid reports whether s parses as JSON (used for workspace menu_json).
func jsonValid(s string) bool {
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}

// errPublishNeedsAck is the single refusal message for "a payment form with
// nothing to sell cannot go live silently". The REST routes send it with a 409
// and a `code`; MCP returns the same sentence as the tool error, so an agent is
// told the same thing a merchant is.
const errPublishNeedsAck = "Borang bayaran ini tiada produk — ia tidak boleh menerima bayaran. " +
	"Tambah produk dahulu, atau hantar acknowledge=true untuk teruskan."

// publishFormIn extends publish_form for the private status (issue #176):
// status=private requires a password; status=published/draft/unlisted clears it.
type publishFormIn struct {
	FormID   string `json:"form_id" jsonschema:"ID borang"`
	Status   string `json:"status,omitempty" jsonschema:"published (lalai) | private | draft | unlisted"`
	Password string `json:"password,omitempty" jsonschema:"Kata laluan akses (wajib bila status=private)"`
	// Acknowledge accepts the "payment form with nothing to sell" warning. An
	// agent reading the tool error sees exactly the same refusal the dashboard
	// does, and re-calls with acknowledge=true to proceed.
	Acknowledge bool `json:"acknowledge,omitempty" jsonschema:"Sahkan amaran: borang bayaran tanpa produk tetap diterbitkan"`
}

func (s *Server) publishFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in publishFormIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	status := in.Status
	if status == "" {
		status = dbstore.FormStatusPublished
	}
	// Onboarding gap: a payment form with nothing to sell must not go live
	// silently through MCP either — this is the same rule the REST publish and
	// status routes apply (dbstore.PublishNeedsAck), not a second copy.
	if dbstore.IsLiveStatus(status) && !in.Acknowledge {
		f, ferr := s.deps.Forms.GetByID(ctx, acc, in.FormID)
		if ferr != nil {
			return nil, nil, errors.New("borang tidak dijumpai")
		}
		if dbstore.PublishNeedsAck(f, status) {
			return nil, nil, errors.New(errPublishNeedsAck)
		}
	}
	switch status {
	case dbstore.FormStatusPublished, dbstore.FormStatusDraft, dbstore.FormStatusUnlisted:
		// Leaving private clears the stored access password.
		if err := s.deps.Forms.SetStatusWithPassword(ctx, acc, in.FormID, status, ""); err != nil {
			return nil, nil, err
		}
	case dbstore.FormStatusPrivate:
		if len(in.Password) < 4 {
			return nil, nil, errors.New("password mesti sekurang-kurangnya 4 aksara")
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			return nil, nil, err
		}
		if err := s.deps.Forms.SetStatusWithPassword(ctx, acc, in.FormID, status, hash); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, errors.New("status mesti published|private|draft")
	}
	return nil, map[string]string{"status": status, "form_id": in.FormID}, nil
}

// setFormPasswordIn is the input for set_form_password (issue #176).
type setFormPasswordIn struct {
	FormID   string `json:"form_id" jsonschema:"ID borang"`
	Password string `json:"password" jsonschema:"Kata laluan akses baharu (kosong = buang kata laluan)"`
}

// setFormPassword stores (or clears) a private form's access password without
// changing its status. The password is hashed with argon2id before storage.
func (s *Server) setFormPassword(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormPasswordIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	hash := ""
	if in.Password != "" {
		if len(in.Password) < 4 {
			return nil, nil, errors.New("password mesti sekurang-kurangnya 4 aksara")
		}
		hash, err = auth.HashPassword(in.Password)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := s.deps.Forms.SetAccessPassword(ctx, acc, in.FormID, hash); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "ok", "form_id": in.FormID}, nil
}

func (s *Server) deleteFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SetStatus(ctx, acc, in.FormID, dbstore.FormStatusDraft); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "unpublished", "form_id": in.FormID}, nil
}

// deleteFormMCPSoft soft-deletes a form (parity with REST DELETE
// /v1/rest/forms/{id} and the dashboard trash). Registered as delete_form.
func (s *Server) deleteFormMCPSoft(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SoftDelete(ctx, acc, in.FormID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "deleted", "form_id": in.FormID}, nil
}

// bulkFormsMCP mirrors POST /v1/rest/forms/bulk: publish|draft|delete over
// many ids in one call (dedup, ≤500), returning how many actually changed.
type bulkFormsIn struct {
	Action string   `json:"action" jsonschema:"publish | draft | delete"`
	IDs    []string `json:"ids" jsonschema:"Senarai form id (maks 500)"`
	// Acknowledge accepts the "payment form with nothing to sell" warning for a
	// bulk publish (see publish_form / dbstore.PublishNeedsAck). Without it the
	// ids that cannot take money are withheld and reported instead.
	Acknowledge bool `json:"acknowledge,omitempty" jsonschema:"Sahkan amaran: borang bayaran tanpa produk tetap diterbitkan"`
}

func (s *Server) bulkFormsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkFormsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if in.Action != "publish" && in.Action != "draft" && in.Action != "delete" {
		return nil, nil, errors.New("action mesti publish|draft|delete")
	}
	if len(in.IDs) == 0 {
		return nil, nil, errors.New("ids wajib")
	}
	if len(in.IDs) > 500 {
		return nil, nil, errors.New("ids terlalu banyak (maks 500)")
	}
	// Issue #307: a user-level agent may bulk-operate across all of their
	// workspaces, but only up to their role in each. So we resolve each form's
	// owning account and apply the action per workspace when the caller has the
	// manage/admin role there.
	seen := make(map[string]struct{}, len(in.IDs))
	ids := make([]string, 0, len(in.IDs))
	for _, id := range in.IDs {
		if _, dup := seen[id]; dup || id == "" {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("ids mesti diisi")
	}
	var updated int64
	// Onboarding gap: ids the bulk publish would have flipped live with nothing
	// to sell. They are withheld and reported (a bulk publish must not be a way
	// around the single-publish guard).
	var needsProducts []string
	for _, id := range ids {
		acc, err := s.resolveFormAccount(ctx, id)
		if err != nil {
			continue // form not owned by an accessible account — skip
		}
		if err := s.requireWriteRoleIn(ctx, acc); err != nil {
			continue // read_only member here — skip
		}
		var e error
		switch in.Action {
		case "publish", "draft":
			status := "published"
			if in.Action == "draft" {
				status = "draft"
			}
			if status == dbstore.FormStatusPublished && !in.Acknowledge {
				f, ferr := s.deps.Forms.GetByID(ctx, acc, id)
				if ferr != nil {
					continue
				}
				if dbstore.PublishNeedsAck(f, status) {
					needsProducts = append(needsProducts, id)
					continue
				}
			}
			_, e = s.deps.Forms.SetStatusMany(ctx, acc, []string{id}, status)
		default:
			_, e = s.deps.Forms.SoftDeleteMany(ctx, acc, []string{id})
		}
		if e == nil {
			updated++
		}
	}
	out := map[string]any{"status": "ok", "action": in.Action, "updated": updated}
	if len(needsProducts) > 0 {
		out["needs_products"] = needsProducts
		out["needs_products_reason"] = "borang bayaran tanpa produk tidak diterbitkan — " +
			"tambah produk dahulu, atau hantar acknowledge=true"
	}
	return nil, out, nil
}

// delete product alias to archive
func (s *Server) archiveProductMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"product_id" jsonschema:"ID produk"`
	},
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireProductWrite(ctx, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Products.Archive(ctx, acc, in.ProductID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "archived", "product_id": in.ProductID}, nil
}

// deleteProductMCP moves a product to the BIN (soft delete), matching
// DELETE /v1/rest/products/{id}.
//
// It is deliberately NOT archiveProductMCP: archive is active=false and the
// product stays in the catalogue, while this takes it out of the live list
// entirely and puts it where it can be restored or purged. Conflating the two
// was the defect the trash work fixed.
func (s *Server) deleteProductMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in archiveProductIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireProductWrite(ctx, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Products.SoftDelete(ctx, acc, in.ProductID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "deleted", "product_id": in.ProductID}, nil
}

// trashedProductOut is one row of the product bin listing.
type trashedProductOut struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PriceSen int64  `json:"price_sen"`
	// DeletedAt is when the product entered the bin (RFC3339).
	DeletedAt string `json:"deleted_at"`
	// SalesCount / SubscriptionCount are what a purge would have to refuse on.
	SalesCount        int64 `json:"sales_count"`
	SubscriptionCount int64 `json:"subscription_count"`
	// CanPurge is the pre-computed answer, so a caller does not derive it.
	CanPurge bool `json:"can_purge"`
}

// listDeletedProducts: the product bin across the caller's accessible accounts.
func (s *Server) listDeletedProducts(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[trashedProductOut], error) {
	if s.deps.Products == nil {
		return nil, nil, errors.New("products tidak tersedia")
	}
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	products, err := s.deps.Products.ListTrashed(ctx, accs)
	if err != nil {
		return nil, nil, err
	}
	sales, err := s.deps.Products.SalesCountsByProduct(ctx, accs)
	if err != nil {
		return nil, nil, err
	}
	subs, err := s.deps.Products.SubscriptionCountsByProduct(ctx, accs)
	if err != nil {
		return nil, nil, err
	}
	out := make([]trashedProductOut, 0, len(products))
	for _, p := range products {
		sn, su := sales[p.ID], subs[p.ID]
		row := trashedProductOut{
			ID: p.ID, Name: p.Name, PriceSen: p.PriceSen,
			SalesCount: sn, SubscriptionCount: su,
			CanPurge: sn == 0 && su == 0,
		}
		if p.DeletedAt != nil {
			row.DeletedAt = p.DeletedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return nil, newListOut(out), nil
}

// requireTrashedProductWrite gates restore / purge / bin reads on a TRASHED
// product. It mirrors requireTrashedFormWrite: the SCOPE axis is checked before
// the entity lookup so the wording of the lookup's failure ("tidak dijumpai")
// cannot be used by a readonly key to tell a real product id from a fake one,
// and the lookup itself is the TRASH-aware GetTrashed (Products.Get filters
// `deleted_at IS NULL`, so it cannot see a trashed row at all).
func (s *Server) requireTrashedProductWrite(ctx context.Context, productID string) (string, error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	if s.deps.Products == nil {
		return "", errors.New("products tidak tersedia")
	}
	acc, err := s.resolveEntityAccount(ctx, func(acc string) error {
		_, err := s.deps.Products.GetTrashed(ctx, acc, productID)
		return err
	})
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// restoreProductMCP brings one trashed product back. No confirmation: it
// destroys nothing and trashing it again undoes it. There is no collision case
// (products have no slug), unlike restoreFormMCP.
func (s *Server) restoreProductMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in archiveProductIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireTrashedProductWrite(ctx, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Products.Restore(ctx, acc, in.ProductID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "restored", "product_id": in.ProductID}, nil
}

// purgeProductMCP permanently destroys a TRASHED product.
//
// Two gates, matching the REST handler and purgeFormMCP:
//
//	confirm_name — the caller must type the product's NAME (a product has no
//	  slug, so the name is the identifier it names back). An irreversible call
//	  should require the caller to name what it is destroying.
//	sales/subscriptions — refused, not purged. A sale is history and
//	  sales.subscription_product_id is ON DELETE SET NULL, so the delete would
//	  silently detach it; subscriptions.product_id is ON DELETE RESTRICT, so
//	  without the named refusal the caller would get a raw SQLSTATE 23503.
func (s *Server) purgeProductMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in purgeProductIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireTrashedProductWrite(ctx, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.deps.Products.GetTrashed(ctx, acc, in.ProductID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.ConfirmName) != p.Name {
		return nil, map[string]any{
			"status": "refused",
			"error": "taip nama produk untuk sahkan pemadaman kekal (produk: " + p.Name + "). " +
				"Pemadaman kekal tidak boleh dibatalkan.",
			"product_id": in.ProductID, "name": p.Name,
		}, nil
	}
	if err := s.deps.Products.Purge(ctx, acc, in.ProductID); err != nil {
		switch {
		case errors.Is(err, dbstore.ErrProductHasSales):
			n, _ := s.deps.Products.CountSalesByProduct(ctx, acc, in.ProductID)
			return nil, map[string]any{
				"status": "refused",
				"error": "produk ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan " +
					"dengan memadam produk. Ia kekal dalam tong sampah.",
				"product_id": in.ProductID, "sales_count": n,
			}, nil
		case errors.Is(err, dbstore.ErrProductHasSubscriptions):
			n, _ := s.deps.Products.CountSubscriptionsByProduct(ctx, acc, in.ProductID)
			return nil, map[string]any{
				"status": "refused",
				"error": "produk ini masih dirujuk subscription — batalkan atau pindahkan " +
					"langganan itu dahulu. Ia kekal dalam tong sampah.",
				"product_id": in.ProductID, "subscription_count": n,
			}, nil
		}
		return nil, nil, err
	}
	return nil, map[string]any{
		"status": "purged", "product_id": in.ProductID, "name": p.Name,
	}, nil
}

// bulkProductsMCP archives many products in one call (parity with
// POST /v1/rest/products/bulk). Issue #307: spans the user's workspaces (up to the
// caller's role per workspace).
type bulkProductsIn struct {
	IDs []string `json:"ids" jsonschema:"Senarai produk id (maks 500)"`
}

func (s *Server) bulkProductsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkProductsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if len(in.IDs) == 0 {
		return nil, nil, errors.New("ids wajib")
	}
	if len(in.IDs) > 500 {
		return nil, nil, errors.New("ids terlalu banyak (maks 500)")
	}
	seen := make(map[string]struct{}, len(in.IDs))
	ids := make([]string, 0, len(in.IDs))
	for _, id := range in.IDs {
		if _, dup := seen[id]; dup || id == "" {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("ids mesti diisi")
	}
	var updated int64
	for _, id := range ids {
		acc, err := s.resolveProductAccount(ctx, id)
		if err != nil {
			continue
		}
		if err := s.requireWriteRoleIn(ctx, acc); err != nil {
			continue
		}
		if n, e := s.deps.Products.ArchiveMany(ctx, acc, []string{id}); e == nil {
			updated += n
		}
	}
	return nil, map[string]any{"status": "ok", "action": "archive", "updated": updated}, nil
}

// ── Pass-through analytics aliases (agents get the same numbers as UI) ──

func (s *Server) formAnalyticsAlias(
	ctx context.Context, req *mcp.CallToolRequest, in emptyIn,
) (*mcp.CallToolResult, *listOutput[formStatsOut], error) {
	return s.formAnalytics(ctx, req, in)
}

func (s *Server) productAnalyticsAlias(
	ctx context.Context, req *mcp.CallToolRequest, in emptyIn,
) (*mcp.CallToolResult, *listOutput[productStatsOut], error) {
	return s.productAnalytics(ctx, req, in)
}

// ── Form restrictions (issue #207): schedule + entry limit ────────────────

// formRestrictionsOut is the read shape of a form's restrictions.
type formRestrictionsOut struct {
	FormID         string     `json:"form_id"`
	StartAt        *time.Time `json:"start_at,omitempty"`
	EndAt          *time.Time `json:"end_at,omitempty"`
	EntryLimit     *int       `json:"entry_limit,omitempty"`
	PendingMessage *string    `json:"pending_message,omitempty"`
	ExpiredMessage *string    `json:"expired_message,omitempty"`
	LimitMessage   *string    `json:"limit_message,omitempty"`
}

// getFormRestrictions reads a form's schedule + entry limit (issue #207).
func (s *Server) getFormRestrictions(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, formRestrictionsOut, error) {
	if s.deps.Forms == nil {
		return nil, formRestrictionsOut{}, errors.New("forms tidak tersedia")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, formRestrictionsOut{}, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, formRestrictionsOut{}, errors.New("borang tidak dijumpai")
	}
	return nil, formRestrictionsOut{
		FormID: f.ID, StartAt: f.StartAt, EndAt: f.EndAt, EntryLimit: f.EntryLimit,
		PendingMessage: f.PendingMessage, ExpiredMessage: f.ExpiredMessage, LimitMessage: f.LimitMessage,
	}, nil
}

// setFormRestrictionsIn is the input for set_form_restrictions.
type setFormRestrictionsIn struct {
	FormID         string     `json:"form_id" jsonschema:"ID borang"`
	StartAt        *time.Time `json:"start_at,omitempty" jsonschema:"Masa mula (kosong = tiada had mula)"`
	EndAt          *time.Time `json:"end_at,omitempty" jsonschema:"Masa tamat (kosong = tiada had tamat)"`
	EntryLimit     *int       `json:"entry_limit,omitempty" jsonschema:"Had penyertaan (kosong = tiada had)"`
	PendingMessage string     `json:"pending_message,omitempty" jsonschema:"Mesej sebelum mula (kosong = lalai)"`
	ExpiredMessage string     `json:"expired_message,omitempty" jsonschema:"Mesej selepas tamat (kosong = lalai)"`
	LimitMessage   string     `json:"limit_message,omitempty" jsonschema:"Mesej had tercapai (kosong = lalai)"`
}

// setFormRestrictions saves a form's schedule + entry limit (issue #207).
func (s *Server) setFormRestrictions(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormRestrictionsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if in.StartAt != nil && in.EndAt != nil && in.EndAt.Before(*in.StartAt) {
		return nil, nil, errors.New("tarikh tamat mesti selepas tarikh mula")
	}
	if in.EntryLimit != nil && *in.EntryLimit < 0 {
		return nil, nil, errors.New("had penyertaan tidak sah")
	}
	if err := s.deps.Forms.UpdateRestrictions(ctx, acc, in.FormID,
		in.StartAt, in.EndAt, in.EntryLimit, in.PendingMessage, in.ExpiredMessage, in.LimitMessage); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "ok", "form_id": in.FormID}, nil
}

var _ = json.Marshal
