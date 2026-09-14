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
	for _, v := range in.Variations {
		if v.Name == "" {
			return nil, nil, fmt.Errorf("variation name wajib")
		}
		if v.PriceSen < 0 {
			return nil, nil, fmt.Errorf("variation price_sen mesti >= 0")
		}
		if v.Stock != nil && *v.Stock < 0 {
			return nil, nil, fmt.Errorf("variation stok mesti >= 0")
		}
		if !urlguard.ValidList(v.Images) {
			return nil, nil, fmt.Errorf("%s", urlguard.InvalidMessage("variations[].images"))
		}
	}
	if err := s.deps.Products.Update(ctx, acc, in.ProductID, namePtr, descPtr,
		in.PriceSen, imagesPtr, stockPtr, in.Active, typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges); err != nil {
		return nil, nil, err
	}
	// Wholesale-replace variations when the caller sent them (nil = keep).
	if in.Variations != nil && s.deps.ProductVariations != nil {
		vs := make([]*dbstore.ProductVariation, 0, len(in.Variations))
		for i, v := range in.Variations {
			order := v.SortOrder
			if order == 0 {
				order = i
			}
			vs = append(vs, &dbstore.ProductVariation{
				Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
				Images: v.Images, SortOrder: order,
			})
		}
		if err := s.deps.ProductVariations.ReplaceAll(ctx, in.ProductID, vs); err != nil {
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
) (*mcp.CallToolResult, []variationOut, error) {
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
		return nil, []variationOut{}, nil
	}
	vs, err := s.deps.ProductVariations.ListByProduct(ctx, in.ProductID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, toVariationOuts(vs), nil
}

// variationOut is the MCP output shape for one product variation.
type variationOut struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	PriceSen  int64    `json:"price_sen"`
	Stock     *int     `json:"stock,omitempty"`
	Images    []string `json:"images,omitempty"`
	SortOrder int      `json:"sort_order"`
}

// toVariationOuts maps store variations to the MCP output shape.
func toVariationOuts(vs []*dbstore.ProductVariation) []variationOut {
	if len(vs) == 0 {
		return nil
	}
	out := make([]variationOut, 0, len(vs))
	for _, v := range vs {
		out = append(out, variationOut{
			ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
			Images: v.Images, SortOrder: v.SortOrder,
		})
	}
	return out
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
		// Issue #193: pre-create the default notification (notify merchant on
		// sale.paid) so a new form sends the merchant a new-sale email with zero
		// configuration. Best-effort.
		if s.deps.Notifications != nil {
			_ = s.deps.Notifications.EnsureDefault(ctx, f.ID)
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
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, in.Title, in.Config, in.Theme); err != nil {
		return nil, nil, err
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
	return nil, map[string]string{"status": "updated", "form_id": in.FormID}, nil
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

// publishFormIn extends publish_form for the private status (issue #176):
// status=private requires a password; status=published/draft/unlisted clears it.
type publishFormIn struct {
	FormID   string `json:"form_id" jsonschema:"ID borang"`
	Status   string `json:"status,omitempty" jsonschema:"published (lalai) | private | draft | unlisted"`
	Password string `json:"password,omitempty" jsonschema:"Kata laluan akses (wajib bila status=private)"`
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
			_, e = s.deps.Forms.SetStatusMany(ctx, acc, []string{id}, status)
		default:
			_, e = s.deps.Forms.SoftDeleteMany(ctx, acc, []string{id})
		}
		if e == nil {
			updated++
		}
	}
	return nil, map[string]any{"status": "ok", "action": in.Action, "updated": updated}, nil
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
) (*mcp.CallToolResult, []formStatsOut, error) {
	return s.formAnalytics(ctx, req, in)
}

func (s *Server) productAnalyticsAlias(
	ctx context.Context, req *mcp.CallToolRequest, in emptyIn,
) (*mcp.CallToolResult, []productStatsOut, error) {
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
