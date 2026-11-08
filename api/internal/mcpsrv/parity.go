// Feature-parity tools — dashboard operations exposed to merchant agents.
package mcpsrv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/textutil"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// slugUnsafe was the MCP copy of the slugify rule; the generator moved to
// dbstore.FormStore.UniqueSlug (card t_f76f81eb), which is the one place a
// candidate slug is derived now.

// ── Products update ──────────────────────────────────────────────────────

type updateProductIn struct {
	ProductID           string        `json:"product_id" jsonschema:"Product ID"`
	Name                string        `json:"name,omitempty" jsonschema:"New name (optional)"`
	Description         string        `json:"description,omitempty" jsonschema:"New description (optional)"`
	PriceSen            *int64        `json:"price_sen,omitempty" jsonschema:"New price in sen (optional)"`
	Images              []string      `json:"images,omitempty" jsonschema:"Images (URL) — full set; empty = clear" `
	Stock               *int          `json:"stock,omitempty" jsonschema:"New stock (optional); omit = unchanged" `
	Active              *bool         `json:"active,omitempty" jsonschema:"Enable/disable"`
	Type                string        `json:"type,omitempty" jsonschema:"one_time or subscription (optional)"`
	BillingPeriod       string        `json:"billing_period,omitempty" jsonschema:"month or year for subscription (optional)"`
	TrialDays           *int          `json:"trial_days,omitempty" jsonschema:"Trial days (subscription; optional)"`
	Variations          []variationIn `json:"variations,omitempty" jsonschema:"Full set of variations (name, price_sen, stock, images, sort_order). Empty = simple product; omit = unchanged."`
	SubscriptionCharges **int         `json:"subscription_charges,omitempty" jsonschema:"Number of charges (subscription; omit = unchanged, null = infinite, N = charge N times)"`
	// round 13c: next-charge date policy after a SUCCESSFUL charge.
	ScheduleMode *string `json:"schedule_mode,omitempty" jsonschema:"Next-charge-date policy: from_payment (from the moment it succeeds; default) | catch_up (follow the original date; missed periods are still charged) | skip_missed (follow the original date; missed periods are skipped)"`
	// Card t_06851d88: pay-what-you-want limits. nil = unchanged;
	// max/suggested are ** so an explicit null (clear the cap / the
	// suggestion) is distinguishable from "not sent".
	MinPriceSen       *int64  `json:"min_price_sen,omitempty" jsonschema:"custom_amount: new smallest accepted amount in SEN (>= 100). Only valid while the product's type is custom_amount."`
	MaxPriceSen       **int64 `json:"max_price_sen,omitempty" jsonschema:"custom_amount: new largest accepted amount in SEN; null = no upper limit; omit = unchanged."`
	SuggestedPriceSen **int64 `json:"suggested_price_sen,omitempty" jsonschema:"custom_amount: new pre-filled amount in SEN; null = start empty; omit = unchanged."`
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
		if in.Type != dbstore.SaleTypeOneTime && in.Type != dbstore.SaleTypeSubscription &&
			in.Type != dbstore.SaleTypeCustomAmount {
			return nil, nil, fmt.Errorf("type must be one_time|subscription|custom_amount")
		}
		typPtr = &in.Type
	}
	var periodPtr *string
	if in.BillingPeriod != "" {
		if in.BillingPeriod != "month" && in.BillingPeriod != "year" {
			return nil, nil, fmt.Errorf("billing_period must be month|year")
		}
		periodPtr = &in.BillingPeriod
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		return nil, nil, fmt.Errorf("trial_days must be >= 0")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges != nil && **in.SubscriptionCharges < 1 {
		return nil, nil, fmt.Errorf("subscription_charges must be >= 1 (or null for infinite)")
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
		return nil, nil, fmt.Errorf("invalid schedule_mode: %q (from_payment|catch_up|skip_missed)", *in.ScheduleMode)
	}
	// Card t_06851d88: pay-what-you-want limits. A partial write is judged as a
	// MERGED triple against the stored row (raising min past an old suggested
	// only fails as a triple), and only when the type or a limit is written.
	if typPtr != nil || in.MinPriceSen != nil || in.MaxPriceSen != nil || in.SuggestedPriceSen != nil {
		cur, gerr := s.deps.Products.Get(ctx, acc, in.ProductID)
		if gerr != nil {
			return nil, nil, gerr
		}
		effType := cur.Type
		if typPtr != nil {
			effType = *typPtr
		}
		if effType == dbstore.SaleTypeCustomAmount {
			mergedMin := cur.MinPriceSen
			if in.MinPriceSen != nil {
				mergedMin = *in.MinPriceSen
			}
			mergedMax := cur.MaxPriceSen
			if in.MaxPriceSen != nil {
				mergedMax = *in.MaxPriceSen
			}
			mergedSug := cur.SuggestedPriceSen
			if in.SuggestedPriceSen != nil {
				mergedSug = *in.SuggestedPriceSen
			}
			if msg := payments.ValidateCustomAmountLimits(mergedMin, mergedMax, mergedSug); msg != "" {
				return nil, nil, fmt.Errorf("%s", msg)
			}
			if in.Variations != nil && len(in.Variations) > 0 {
				return nil, nil, fmt.Errorf("a custom_amount product cannot have variations")
			}
			// price_sen follows the limits: the buyer chooses the charge, so the
			// display price is never editable on its own for this type.
			priceSen := payments.CustomAmountDisplayPrice(mergedMin, mergedSug)
			in.PriceSen = &priceSen
			in.MinPriceSen = &mergedMin
			in.MaxPriceSen = &mergedMax
			in.SuggestedPriceSen = &mergedSug
		} else if in.MinPriceSen != nil || in.MaxPriceSen != nil || in.SuggestedPriceSen != nil {
			return nil, nil, fmt.Errorf("min_price_sen, max_price_sen and suggested_price_sen apply only to type custom_amount")
		}
	}
	if err := s.deps.Products.Update(ctx, acc, in.ProductID, namePtr, descPtr,
		in.PriceSen, imagesPtr, stockPtr, in.Active, typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges,
		in.ScheduleMode, in.MinPriceSen, in.MaxPriceSen, in.SuggestedPriceSen); err != nil {
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
	ProductID string `json:"product_id" jsonschema:"Product ID"`
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
		return nil, nil, fmt.Errorf("product_id is required")
	}
	// Ownership check: the product must belong to the account.
	if _, err := s.deps.Products.Get(ctx, acc, in.ProductID); err != nil {
		return nil, nil, fmt.Errorf("product not found")
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
	ProductID string `json:"product_id" jsonschema:"Product ID"`
	variationIn
}

// createProductVariation adds ONE variation to a product (issue #623).
func (s *Server) createProductVariation(
	ctx context.Context, _ *mcp.CallToolRequest, in createVariationIn,
) (*mcp.CallToolResult, variationOut, error) {
	if in.ProductID == "" {
		return nil, variationOut{}, fmt.Errorf("product_id is required")
	}
	if s.deps.ProductVariations == nil {
		return nil, variationOut{}, fmt.Errorf("product variations not available")
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
	ProductID   string    `json:"product_id" jsonschema:"Product ID"`
	VariationID string    `json:"variation_id" jsonschema:"Variation ID"`
	Name        *string   `json:"name,omitempty" jsonschema:"New name (optional)"`
	PriceSen    *int64    `json:"price_sen,omitempty" jsonschema:"New price in SEN (optional)"`
	Stock       *int      `json:"stock,omitempty" jsonschema:"New stock (optional)"`
	Images      *[]string `json:"images,omitempty" jsonschema:"Images (URL must be https://) — full set; empty = clear"`

	// Issue #623: the variation's own subscription cycle.
	BillingPeriod            *string `json:"billing_period,omitempty" jsonschema:"month | year. Clear with clear_billing_period=true to inherit from the product again."`
	TrialDays                *int    `json:"trial_days,omitempty" jsonschema:"Trial days. Clear with clear_trial_days=true."`
	SubscriptionCharges      *int    `json:"subscription_charges,omitempty" jsonschema:"Number of charges. Clear with clear_subscription_charges=true."`
	ClearBillingPeriod       bool    `json:"clear_billing_period,omitempty" jsonschema:"true = remove this variation's billing cycle so it inherits from the product again"`
	ClearTrialDays           bool    `json:"clear_trial_days,omitempty" jsonschema:"true = remove the variation's trial days so it inherits from the product again"`
	ClearSubscriptionCharges bool    `json:"clear_subscription_charges,omitempty" jsonschema:"true = remove the variation's number of charges so it inherits from the product again"`
}

// updateProductVariation edits ONE variation (issue #623). A cleared cycle field
// goes back to NULL, which is the "inherit the product" state.
func (s *Server) updateProductVariation(
	ctx context.Context, _ *mcp.CallToolRequest, in updateVariationIn,
) (*mcp.CallToolResult, variationOut, error) {
	if in.ProductID == "" || in.VariationID == "" {
		return nil, variationOut{}, fmt.Errorf("product_id and variation_id are required")
	}
	if s.deps.ProductVariations == nil {
		return nil, variationOut{}, fmt.Errorf("product variations not available")
	}
	if _, err := s.requireProductWrite(ctx, in.ProductID); err != nil {
		return nil, variationOut{}, err
	}
	if in.Name != nil && *in.Name == "" {
		return nil, variationOut{}, fmt.Errorf("variation name cannot be empty")
	}
	if in.PriceSen != nil && *in.PriceSen < 0 {
		return nil, variationOut{}, fmt.Errorf("variation price_sen must be >= 0")
	}
	if in.Stock != nil && *in.Stock < 0 {
		return nil, variationOut{}, fmt.Errorf("variation stock must be >= 0")
	}
	if in.Images != nil && !urlguard.ValidList(*in.Images) {
		return nil, variationOut{}, fmt.Errorf("%s", urlguard.InvalidMessage("variations[].images"))
	}
	// Issue #623: an unknown period is rejected, not silently dropped.
	if in.BillingPeriod != nil && !in.ClearBillingPeriod &&
		*in.BillingPeriod != "" && *in.BillingPeriod != "month" && *in.BillingPeriod != "year" {
		return nil, variationOut{}, fmt.Errorf("variation billing_period must be month|year")
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		return nil, variationOut{}, fmt.Errorf("variation trial_days must be >= 0")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges < 1 {
		return nil, variationOut{}, fmt.Errorf("variation subscription_charges must be >= 1")
	}
	if err := s.deps.ProductVariations.Update(ctx, in.ProductID, in.VariationID,
		in.Name, in.PriceSen, &in.Stock, in.Images, nil,
		in.BillingPeriod, in.ClearBillingPeriod,
		in.TrialDays, in.ClearTrialDays,
		in.SubscriptionCharges, in.ClearSubscriptionCharges); err != nil {
		if err == dbstore.ErrVariationNotFound {
			return nil, variationOut{}, fmt.Errorf("variation not found for this product")
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
	ProductID   string `json:"product_id" jsonschema:"Product ID"`
	VariationID string `json:"variation_id" jsonschema:"Variation ID"`
}

// deleteProductVariation removes ONE variation from a product (issue #623),
// scoped to that product so an agent cannot delete a variation out of another
// product's set. Deleting the LAST variation turns the product back into a
// simple product (the editor's own rule: >=1 variation = variable).
func (s *Server) deleteProductVariation(
	ctx context.Context, _ *mcp.CallToolRequest, in deleteVariationIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.ProductID == "" || in.VariationID == "" {
		return nil, nil, fmt.Errorf("product_id and variation_id are required")
	}
	if s.deps.ProductVariations == nil {
		return nil, nil, fmt.Errorf("product variations not available")
	}
	if _, err := s.requireProductWrite(ctx, in.ProductID); err != nil {
		return nil, nil, err
	}
	if err := s.deps.ProductVariations.Delete(ctx, in.ProductID, in.VariationID); err != nil {
		if err == dbstore.ErrVariationNotFound {
			return nil, nil, fmt.Errorf("variation not found for this product")
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
	FormID string `json:"form_id" jsonschema:"Form ID"`
}

// getFormChipMapping returns the form's merchant-configured CHIP field
// mapping (CHIP client field → form block id). When the merchant hasn't set
// one, the implicit default mapping is returned (name→full_name, email→email,
// phone→phone), matching the dashboard's default behaviour.
func (s *Server) getFormChipMapping(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormChipMappingIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
	}
	mapping := chipMappingFromConfig(f)
	if mapping == nil {
		mapping = map[string]string{}
	}
	return nil, mapping, nil
}

// setFormChipMappingIn is the input for set_form_chip_mapping (issue #186).
type setFormChipMappingIn struct {
	FormID  string            `json:"form_id" jsonschema:"Form ID"`
	Mapping map[string]string `json:"mapping" jsonschema:"Map of CHIP fields → form block ids, e.g. {\"full_name\":\"block_abc\",\"email\":\"block_def\"}. Empty = use defaults (name→full_name, email→email, phone→phone)."`
}

// setFormChipMapping stores the form's CHIP field mapping in its config
// (issue #186). An empty mapping clears it, reverting to the implicit default.
func (s *Server) setFormChipMapping(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormChipMappingIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
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
	FormID string `json:"form_id,omitempty" jsonschema:"Empty = new form; filled = update"`
	// Card t_186b675c: slug MUST carry `omitempty`. The schema generator treats
	// every field without it as a REQUIRED property, and the schema is enforced
	// BEFORE the handler runs — so `json:"slug"` made the tool's own description
	// ("slug (pilihan) … kosongkan untuk kekalkan slug sedia ada", tools.go)
	// unreachable: a call that omitted slug was refused with
	//   validating "arguments": validating root: required: missing properties:
	//   ["slug"]
	// and never reached the handler. The handler implements exactly what the
	// description promises — an omitted slug auto-generates one on create
	// (issue #176) and keeps the stored one on update — so the three now agree.
	// Pinned in both directions by upsert_form_slug_optional_test.go.
	Slug   string         `json:"slug,omitempty" jsonschema:"Unique URL slug (optional): empty = generate automatically on create, keep the existing slug on update. a-z, 0-9, hyphens; 1-40 characters; not a name JomForm reserves for itself (login, api, docs, privacy, …)."`
	Title  string         `json:"title" jsonschema:"Form title"`
	Config map[string]any `json:"config,omitempty" jsonschema:"Block config — take an example from get_form"`
	// Issue #254: optional custom styling/theme (colors, fonts, logo, layout,
	// spacing, custom_css, custom_js). Omit to leave unchanged; pass {} to clear.
	Theme map[string]any `json:"theme,omitempty" jsonschema:"Custom style/branding (optional): primary, background, text, card_background, card_border_radius, font_family, logo_url, layout, spacing, custom_css, custom_js (custom JS — owner-controlled + scanned)"`
	// Issue #207: form restrictions (schedule + entry limit). Omit to leave
	// unchanged; pass explicit nulls to clear.
	StartAt        *time.Time `json:"start_at,omitempty" jsonschema:"Schedule start time (empty = none)"`
	EndAt          *time.Time `json:"end_at,omitempty" jsonschema:"Schedule end time (empty = none)"`
	EntryLimit     *int       `json:"entry_limit,omitempty" jsonschema:"Entry limit (empty = none)"`
	PendingMessage string     `json:"pending_message,omitempty" jsonschema:"Message before start (empty = default)"`
	ExpiredMessage string     `json:"expired_message,omitempty" jsonschema:"Message after end (empty = default)"`
	LimitMessage   string     `json:"limit_message,omitempty" jsonschema:"Limit-reached message (empty = default)"`
	// Issue #208: per-form CHIP feeds (multiple, with conditional logic).
	// Omit to leave unchanged; pass an empty array to clear all feeds.
	ChipFeeds []chipFeedIn `json:"chip_feeds,omitempty" jsonschema:"List of form CHIP feeds. A feed without a condition = default (always matches). The first matching feed is used."`
	// Issue #193: per-form notification rules. Omit to leave unchanged; pass
	// an explicit list to replace the form's notifications wholesale.
	Notifications []notificationIn `json:"notifications,omitempty" jsonschema:"Email notification rules (replace all when provided)"`
	// Issue #179: form confirmations (Gravity-Forms-style). Omit to leave
	// unchanged; pass an empty array to reset to just the default.
	Confirmations []confirmationIn `json:"confirmations,omitempty" jsonschema:"Form validation (replace all; defaults are always kept)"`
	// Issue #255: fully-custom HTML response-only form. When set, the form
	// becomes a response-mode custom form: the agent-authored HTML body is
	// rendered in a sandboxed iframe (CSP + sanitized) and only the
	// registered custom_fields are recorded on submit. Omit to leave
	// unchanged; pass an empty string to clear the custom form.
	CustomHTML   string             `json:"custom_html,omitempty" jsonschema:"Custom form body HTML (response-only, sandboxed iframe). Empty = remove custom mode."`
	CustomFields []customform.Field `json:"custom_fields,omitempty" jsonschema:"Field registration contract: [{field_id, type, label, required, validation}]. The HTML input must carry field_id as its name. type=url accepts any absolute URL the browser's input[type=url] accepts (https://, ftp://, mailto:), not http(s) only; script schemes (javascript:, data:, vbscript:) are rejected."`
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
		// Card t_7546eda5: an account with NO live workspace cannot mint a form.
		//
		// The tool used to store &dbstore.Form{AccountID: acc, …} with no
		// workspace check, and `accounts.workspace_id` is nullable (a signup
		// creates an account and no workspace, #718): the
		// forms_sync_workspace_id trigger (000045) copied the NULL onto the row,
		// the form could be PUBLISHED, and it was then 404 at every address
		// forever — the same orphan POST /v1/rest/forms refuses since
		// t_6c0badbe, reached through the other door into the same store.
		//
		// Only the CREATE half is gated: an update names an existing form, so it
		// mints nothing. The refusal is the REST door's sentence and therefore
		// its `workspace.required` machine code (httpapi's error-code table keys
		// that message; TestWorkspaceRequiredIsOneSentenceOnBothDoors pins both
		// halves), so the two write paths cannot disagree about this state.
		if err := s.requireLiveWorkspace(ctx, acc); err != nil {
			return nil, nil, err
		}
	} else {
		acc, err = s.requireFormWrite(ctx, in.FormID)
		if err != nil {
			return nil, nil, err
		}
	}
	if in.Title == "" {
		return nil, nil, errors.New("title is required")
	}
	if in.StartAt != nil && in.EndAt != nil && in.EndAt.Before(*in.StartAt) {
		return nil, nil, errors.New("end date must be after start date")
	}
	if in.EntryLimit != nil && *in.EntryLimit < 0 {
		return nil, nil, errors.New("invalid entry limit")
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
	// Card t_4086b8f0: a block type no renderer can honour (formfields
	// .UnsupportedBlock) is refused here as well, or the agent's write path
	// stores a block the buyer's page cannot serve.
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		return nil, nil, errors.New(formfields.UnsupportedBlockMessage(bt))
	}
	// Card t_3d11f98d: an option/shipping block with no resolvable price would
	// be stored as a free add-on and charged 0 sen — refused here too, so the
	// agent's write path cannot produce a form the merchant believes is priced.
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		return nil, nil, errors.New(msg)
	}
	// Card t_73a5db6e: a custom-form config whose registered `required` field
	// has no matching control in the sanitized HTML is unsubmittable at /order,
	// and an agent can send config.custom_html directly (without the
	// custom_html/custom_fields inputs), so the same refusal the REST form
	// routes apply belongs here too.
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		return nil, nil, err
	}
	// Issue #388: power-user custom JS in the theme is owner-gated (requireWriteRole
	// above) and scanned with the deterministic code-asset scan (#364) so obviously
	// malicious JS is never served on the buyer's page.
	//
	// Card t_8b3abbde: this arm MUST sit above the create/update split. It used to
	// live in the update half only, so the create branch returned at its own
	// `status: created` before ever reaching it and stored an unscanned value —
	// the same hole v1.21.293 closed on POST /v1/rest/forms. One rule, one place,
	// both halves of the tool; a nil/empty `custom_js` is not a string and falls
	// through unchanged, so an omitted value still creates.
	if in.Theme != nil {
		if js, ok := in.Theme["custom_js"].(string); ok {
			if reason := customform.ScanCustomJS(js); reason != "" {
				return nil, nil, errors.New(customform.RejectedMessage(reason))
			}
		}
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
			return nil, nil, errors.New("form not found")
		}
		merged := orEmptyMap(existing.Config)
		if err := applyCustomFormConfig(merged, in.CustomHTML, in.CustomFields); err != nil {
			return nil, nil, err
		}
		in.Config = merged
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
	// Card t_4086b8f0: same unsupported-block refusal as the create path above.
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		return nil, nil, errors.New(formfields.UnsupportedBlockMessage(bt))
	}
	// Card t_3d11f98d: same unresolved-pricing refusal as the create path.
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		return nil, nil, errors.New(msg)
	}
	// Card t_73a5db6e: same unsatisfiable-custom-form refusal as the create
	// path (the merged config that carries custom_html lands in in.Config).
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		return nil, nil, err
	}
	// A slug supplied on the UPDATE path renames the form, exactly as the
	// dashboard's slug field does (same FormStore.RenameSlug, so the same
	// validation, the same per-workspace uniqueness and the same temporary 301
	// for the old URL). The tool's own description documents `slug` as "Slug URL
	// unik" and it was honoured only on create: an agent told to "set the slug"
	// got back {"status":"updated"} and no change, which is the carded defect.
	//
	// The rename runs BEFORE the title/config write, and the order is the point
	// (card t_0b7980f5): RenameSlug validates the whole rename before writing
	// anything, so a refused one — a malformed slug, or ErrSlugTaken for a slug
	// another live form serves — leaves the form's data untouched. Issued second
	// it did not: the collision error came back after the title/config write had
	// already committed, so the agent was told the upsert failed while the row
	// had moved. The two writes are still two statements rather than one
	// transaction; what this removes is the only refusal that can happen
	// BETWEEN them.
	//
	// An OMITTED slug (empty) keeps the stored value, matching config/theme's
	// contract, and renaming to the slug the form already has is a no-op. A
	// collision is ErrSlugTaken, surfaced to the agent, never a silent no-op.
	if in.Slug != "" {
		if _, err := s.deps.Forms.RenameSlug(ctx, acc, in.FormID, in.Slug); err != nil {
			return nil, nil, err
		}
	}
	// The title is non-empty here (the "title is required" refusal above), and
	// Forms.Update keeps the stored value for an empty one, so an upsert can
	// never blank a form's title (card t_0b7980f5).
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, in.Title, in.Config, in.Theme); err != nil {
		return nil, nil, err
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
		return errors.New("notifications not available")
	}
	for _, n := range in {
		if !validNotificationEvent(n.Event) {
			return errors.New("invalid event: " + n.Event)
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
			return errors.New("confirmation name is required")
		}
		if c.Type != dbstore.ConfirmationTypeMessage && c.Type != dbstore.ConfirmationTypeRedirect {
			return errors.New("invalid confirmation type")
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
	cfg["mode"] = customform.ModeResponse
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
// (issue #176). The derivation and the uniqueness loop live in
// dbstore.FormStore.UniqueSlug — ONE generator for both create doors (card
// t_f76f81eb); this wrapper only keeps the call sites stable.
func (s *Server) uniqueFormSlug(ctx context.Context, accountID, title string) string {
	return s.deps.Forms.UniqueSlug(ctx, accountID, title)
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
//
// It states BOTH halves (t_39906c0a): the form cannot take money AND collects
// nothing — the legacy and block payment layouts both gate submit on a selected
// product, so `acknowledge=true` is not a way to collect answers.
const errPublishNeedsAck = "This payment form has no products — it cannot accept payment " +
	"and collects nothing. Add a product first, or send acknowledge=true to continue."

// publishFormIn extends publish_form for the private status (issue #176):
// status=private requires a password; status=published/draft/unlisted clears it.
type publishFormIn struct {
	FormID   string `json:"form_id" jsonschema:"Form ID"`
	Status   string `json:"status,omitempty" jsonschema:"published (default) | private | draft | unlisted"`
	Password string `json:"password,omitempty" jsonschema:"Access password (required when status=private)"`
	// Acknowledge accepts the "payment form with nothing to sell" warning. An
	// agent reading the tool error sees exactly the same refusal the dashboard
	// does, and re-calls with acknowledge=true to proceed.
	Acknowledge bool `json:"acknowledge,omitempty" jsonschema:"Confirm warning: a payment form without products is still published"`
}

func (s *Server) publishFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in publishFormIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	status := in.Status
	if status == "" {
		status = dbstore.FormStatusPublished
	}
	// ONE read of the CURRENT row, before the write: the acknowledgement below is
	// a TRANSITION rule, and whether the edge holds a publicly cached card for this
	// form depends on the status it is LEAVING (dbstore.OGImagePubliclyCached) —
	// reading after the write would lose that.
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
	}
	// Card t_fadfe9f7: a form cannot go LIVE on an account with no live
	// workspace. The published row would carry workspace_id NULL (the
	// forms_sync_workspace_id trigger copies accounts.workspace_id) and every
	// public serving path resolves a form through its workspace — so the tool
	// would answer status:published for a page that is 404 at every address
	// forever (apex /f/<slug> is refused by design, issue #77). Same sentence
	// and same nil-store tolerance as the CREATE doors (t_7546eda5); the UPDATE
	// half stays open, and moving the form back to draft stays open, because
	// that is the remedy for a row that already exists.
	// A gallery template entering `published` is exempt (t_8b185f34): publishing
	// it is what puts it in the gallery, which is its address — see
	// hasGalleryAddress.
	if dbstore.IsLiveStatus(status) && !hasGalleryAddress(f, status) {
		if err := s.requireLiveWorkspace(ctx, acc); err != nil {
			return nil, nil, err
		}
	}
	// Onboarding gap: a payment form with nothing to sell must not go live
	// silently through MCP either — this is the same rule the REST publish and
	// status routes apply (dbstore.PublishNeedsAck), not a second copy.
	if dbstore.IsLiveStatus(status) && !in.Acknowledge {
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
			return nil, nil, errors.New("password must be at least 4 characters")
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			return nil, nil, err
		}
		if err := s.deps.Forms.SetStatusWithPassword(ctx, acc, in.FormID, status, hash); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, errors.New("status must be published|private|draft")
	}
	// THE EDGE: the row is written above, so a card the CDN cached while the form
	// was published/unlisted is dropped here, in the same call (see
	// og_edge_purge.go). Same rule and same reported outcome as REST setFormStatus.
	var leaving []string
	if dbstore.OGImagePubliclyCached(f.Status) && !dbstore.OGImagePubliclyCached(status) {
		leaving = []string{f.Slug}
	}
	edge := s.purgeLeftPublicArtefacts(ctx, acc, leaving)
	return nil, withEdgeFields(map[string]any{"status": status, "form_id": in.FormID}, edge), nil
}

// setFormPasswordIn is the input for set_form_password (issue #176).
type setFormPasswordIn struct {
	FormID   string `json:"form_id" jsonschema:"Form ID"`
	Password string `json:"password" jsonschema:"New access password (empty = remove password)"`
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
			return nil, nil, errors.New("password must be at least 4 characters")
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
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	// The status it is LEAVING decides whether the CDN holds a public card for it.
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
	}
	if err := s.deps.Forms.SetStatus(ctx, acc, in.FormID, dbstore.FormStatusDraft); err != nil {
		return nil, nil, err
	}
	var leaving []string
	if dbstore.OGImagePubliclyCached(f.Status) {
		leaving = []string{f.Slug}
	}
	edge := s.purgeLeftPublicArtefacts(ctx, acc, leaving)
	return nil, withEdgeFields(map[string]any{"status": "unpublished", "form_id": in.FormID}, edge), nil
}

// deleteFormMCPSoft soft-deletes a form (parity with REST DELETE
// /v1/rest/forms/{id} and the dashboard trash). Registered as delete_form.
func (s *Server) deleteFormMCPSoft(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	// A TRASHED form stops resolving publicly (findPublicForm scopes on
	// deleted_at IS NULL), so its cached card has to leave the edge with it.
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
	}
	if err := s.deps.Forms.SoftDelete(ctx, acc, in.FormID); err != nil {
		return nil, nil, err
	}
	var leaving []string
	if dbstore.OGImagePubliclyCached(f.Status) {
		leaving = []string{f.Slug}
	}
	edge := s.purgeLeftPublicArtefacts(ctx, acc, leaving)
	return nil, withEdgeFields(map[string]any{"status": "deleted", "form_id": in.FormID}, edge), nil
}

// BulkGallerySkippedReason is the reply sentence for a bulk publish that skips a
// template for missing gallery metadata. It is the SAME sentence POST
// /v1/rest/forms/bulk sends as skipped_reason (httpapi's templateNeedMetadataWire),
// and it is duplicated as a literal here because httpapi imports this package, so
// mcpsrv cannot import that constant back — httpapi's
// TestBulkSkipReasonIsOneSentenceOnBothDoors pins the two equal instead.
const BulkGallerySkippedReason = "a template needs a category and one description block (Section or text) before it can be published to the gallery"

// bulkFormsMCP mirrors POST /v1/rest/forms/bulk: publish|draft|delete over
// many ids in one call (dedup, ≤500), returning how many actually changed.
type bulkFormsIn struct {
	Action string   `json:"action" jsonschema:"publish | draft | delete"`
	IDs    []string `json:"ids" jsonschema:"List of form ids (max 500)"`
	// Acknowledge accepts the "payment form with nothing to sell" warning for a
	// bulk publish (see publish_form / dbstore.PublishNeedsAck). Without it the
	// ids that cannot take money are withheld and reported instead.
	Acknowledge bool `json:"acknowledge,omitempty" jsonschema:"Confirm warning: a payment form without products is still published"`
}

func (s *Server) bulkFormsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkFormsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if in.Action != "publish" && in.Action != "draft" && in.Action != "delete" {
		return nil, nil, errors.New("action must be publish|draft|delete")
	}
	if len(in.IDs) == 0 {
		return nil, nil, errors.New("ids is required")
	}
	if len(in.IDs) > 500 {
		return nil, nil, errors.New("too many ids (max 500)")
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
		return nil, nil, errors.New("ids must not be empty")
	}
	var updated int64
	// Onboarding gap: ids the bulk publish would have flipped live with nothing
	// to sell. They are withheld and reported (a bulk publish must not be a way
	// around the single-publish guard).
	var needsProducts []string
	// Gallery-metadata skips (issue #594, card t_80ea6b64). A bulk publish
	// REFUSES a template that carries no category or no description block, and
	// SetStatusMany states that refusal INSIDE its UPDATE — the row matches the
	// account/deleted filters but not the gallery predicate, so it comes back as
	// 0 rows affected with no error. Counting "no error" as a write therefore
	// told the caller a template had gone live while the row stayed draft
	// (measured on prod: bulk_forms publish → {"updated":1} over a row still at
	// draft). Ask the SAME predicate which ids the write will skip, so the reply
	// names them exactly as POST /v1/rest/forms/bulk does.
	var skipped []string
	// Issue #307: a user-level agent may bulk-operate across all of their
	// workspaces, but only up to their role in each. Resolve each id's owning
	// account and the caller's role there ONCE, up front: the write loop needs
	// the same answer, and a publish additionally has to ask the store what it
	// will skip — one query per workspace rather than one per id.
	type bulkTarget struct{ id, acc string }
	targets := make([]bulkTarget, 0, len(ids))
	for _, id := range ids {
		acc, err := s.resolveFormAccount(ctx, id)
		if err != nil {
			continue // form not owned by an accessible account — skip
		}
		if err := s.requireWriteRoleIn(ctx, acc); err != nil {
			continue // read_only member here — skip
		}
		targets = append(targets, bulkTarget{id: id, acc: acc})
	}
	if in.Action == "publish" {
		byAccount := make(map[string][]string, len(targets))
		for _, t := range targets {
			byAccount[t.acc] = append(byAccount[t.acc], t.id)
		}
		// Card t_fadfe9f7: the workspace precondition of publish_form applies
		// to the bulk door too, or a bulk publish is the way around it. One
		// account without a live workspace refuses the WHOLE call before any
		// row is written: those ids would otherwise be reported `updated` and
		// be 404 at every address forever. A bulk call normally spans one
		// account (an API key is pinned to exactly that account), so this is
		// the same refusal the single tool gives, not a partial batch.
		//
		// Exception (card t_8b185f34): a batch made ONLY of gallery templates
		// has an address — the gallery — so that account is exempt, exactly
		// like publish_form. One ordinary form in the batch puts the account
		// back under the gate, because that row would go live with no address.
		for acc, accIDs := range byAccount {
			onlyTemplates, terr := s.deps.Forms.AllGalleryTemplates(ctx, acc, accIDs)
			if terr != nil {
				return nil, nil, terr
			}
			if onlyTemplates {
				continue
			}
			if werr := s.requireLiveWorkspace(ctx, acc); werr != nil {
				return nil, nil, werr
			}
		}
		for acc, accIDs := range byAccount {
			// BlockedGalleryTemplates is account-scoped, which is why the batch
			// is grouped rather than passed whole (a bulk action may span
			// several workspaces).
			blocked, berr := s.deps.Forms.BlockedGalleryTemplates(ctx, acc, accIDs)
			if berr != nil {
				return nil, nil, berr
			}
			skipped = append(skipped, blocked...)
		}
	}
	skippedSet := make(map[string]bool, len(skipped))
	for _, id := range skipped {
		skippedSet[id] = true
	}
	// Public artefacts this action invalidates, as absolute URLs, collected while
	// the loop already has each form's account and row: a bulk draft/delete can
	// span several workspaces (issue #307), so the hosts differ per id, and the
	// purge itself is ONE call at the end (see og_edge_purge.go). A form's cached
	// card and QR PNG only need dropping when it is LEAVING a publicly-cached
	// status, which is a property of the row BEFORE the write.
	var cardURLs []string
	hostCache := make(map[string][]string, len(ids))
	for _, t := range targets {
		id, acc := t.id, t.acc
		if in.Action != "publish" {
			f, ferr := s.deps.Forms.GetByID(ctx, acc, id)
			if ferr != nil {
				continue
			}
			if dbstore.OGImagePubliclyCached(f.Status) {
				hosts, ok := hostCache[acc]
				if !ok {
					hosts = s.publicArtefactHosts(ctx, acc)
					hostCache[acc] = hosts
				}
				cardURLs = append(cardURLs, artefactURLsForHosts(hosts, []string{f.Slug})...)
			}
		}
		var e error
		var n int64
		switch in.Action {
		case "publish", "draft":
			status := "published"
			if in.Action == "draft" {
				status = "draft"
			}
			if status == dbstore.FormStatusPublished && skippedSet[id] {
				// The gallery gate refuses this row outright, so neither the write
				// nor acknowledge=true can move it — report it as skipped, and NOT
				// as needs_products (whose advice is "send acknowledge=true", which
				// would be false here).
				continue
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
			n, e = s.deps.Forms.SetStatusMany(ctx, acc, []string{id}, status)
		default:
			n, e = s.deps.Forms.SoftDeleteMany(ctx, acc, []string{id})
		}
		// Count the rows the statement actually WROTE, never the absence of an
		// error: a predicate-refused row, or an id trashed between these checks
		// and the write, reports 0 rows affected and must not inflate `updated`.
		if e == nil {
			updated += n
		}
	}
	out := map[string]any{"status": "ok", "action": in.Action, "updated": updated}
	if len(skipped) > 0 {
		out["skipped"] = skipped
		out["skipped_reason"] = BulkGallerySkippedReason
	}
	if len(cardURLs) > 0 {
		// The rows are written above; the edge copies go here, in ONE request for
		// every affected artefact (cdnpurge chunks anything past its per-request cap).
		edge := cdnpurge.PurgeObjectURLs(ctx, cardURLs)
		if edge.Err != nil {
			slog.Warn("mcp_bulk_forms_edge_purge", "err", edge.Err, "status", edge.Status, "urls", len(cardURLs))
		}
		out = withEdgeFields(out, edge)
	}
	if len(needsProducts) > 0 {
		out["needs_products"] = needsProducts
		out["needs_products_reason"] = "a payment form without products is not published — " +
			"it collects nothing; add a product first, or send acknowledge=true"
	}
	return nil, out, nil
}

// delete product alias to archive
func (s *Server) archiveProductMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"product_id" jsonschema:"Product ID"`
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
		return nil, nil, errors.New("products not available")
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
		return "", errors.New("products not available")
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
	// textutil.SameTypedValue trims BOTH sides — the rule the REST handler applies
	// too. Trimming only the caller's side made a product whose stored name carries
	// padding unconfirmable over MCP as well: `{"name":"Kopi "}` is stored verbatim,
	// and TrimSpace(typed) can never equal "Kopi " (card t_1ee11d1f).
	if !textutil.SameTypedValue(in.ConfirmName, p.Name) {
		return nil, map[string]any{
			"status": "refused",
			"error": "type the product name to confirm the permanent deletion (product: " + p.Name + "). " +
				"Permanent deletion cannot be undone.",
			"product_id": in.ProductID, "name": p.Name,
		}, nil
	}
	if err := s.deps.Products.Purge(ctx, acc, in.ProductID); err != nil {
		switch {
		case errors.Is(err, dbstore.ErrProductHasSales):
			n, _ := s.deps.Products.CountSalesByProduct(ctx, acc, in.ProductID)
			return nil, map[string]any{
				"status": "refused",
				"error": "this product has sales — sales history cannot be destroyed " +
					"by deleting the product. It stays in the Trash.",
				"product_id": in.ProductID, "sales_count": n,
			}, nil
		case errors.Is(err, dbstore.ErrProductHasSubscriptions):
			n, _ := s.deps.Products.CountSubscriptionsByProduct(ctx, acc, in.ProductID)
			return nil, map[string]any{
				"status": "refused",
				"error": "this product is still referenced by a subscription — cancel or move " +
					"that subscription first. It stays in the Trash.",
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
	IDs []string `json:"ids" jsonschema:"List of product ids (max 500)"`
}

func (s *Server) bulkProductsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkProductsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if len(in.IDs) == 0 {
		return nil, nil, errors.New("ids is required")
	}
	if len(in.IDs) > 500 {
		return nil, nil, errors.New("too many ids (max 500)")
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
		return nil, nil, errors.New("ids must not be empty")
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
		return nil, formRestrictionsOut{}, errors.New("forms not available")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, formRestrictionsOut{}, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, formRestrictionsOut{}, errors.New("form not found")
	}
	return nil, formRestrictionsOut{
		FormID: f.ID, StartAt: f.StartAt, EndAt: f.EndAt, EntryLimit: f.EntryLimit,
		PendingMessage: f.PendingMessage, ExpiredMessage: f.ExpiredMessage, LimitMessage: f.LimitMessage,
	}, nil
}

// setFormRestrictionsIn is the input for set_form_restrictions.
type setFormRestrictionsIn struct {
	FormID         string     `json:"form_id" jsonschema:"Form ID"`
	StartAt        *time.Time `json:"start_at,omitempty" jsonschema:"Start time (empty = no start limit)"`
	EndAt          *time.Time `json:"end_at,omitempty" jsonschema:"End time (empty = no end limit)"`
	EntryLimit     *int       `json:"entry_limit,omitempty" jsonschema:"Entry limit (empty = no limit)"`
	PendingMessage string     `json:"pending_message,omitempty" jsonschema:"Message before start (empty = default)"`
	ExpiredMessage string     `json:"expired_message,omitempty" jsonschema:"Message after end (empty = default)"`
	LimitMessage   string     `json:"limit_message,omitempty" jsonschema:"Limit-reached message (empty = default)"`
}

// setFormRestrictions saves a form's schedule + entry limit (issue #207).
func (s *Server) setFormRestrictions(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormRestrictionsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if in.StartAt != nil && in.EndAt != nil && in.EndAt.Before(*in.StartAt) {
		return nil, nil, errors.New("end date must be after start date")
	}
	if in.EntryLimit != nil && *in.EntryLimit < 0 {
		return nil, nil, errors.New("invalid entry limit")
	}
	if err := s.deps.Forms.UpdateRestrictions(ctx, acc, in.FormID,
		in.StartAt, in.EndAt, in.EntryLimit, in.PendingMessage, in.ExpiredMessage, in.LimitMessage); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "ok", "form_id": in.FormID}, nil
}

var _ = json.Marshal
