// Per-form CHIP feeds endpoints (issue #208). Feeds are stored in the form's
// config (chip_feeds key); secrets are encrypted at rest and never echoed back.
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// chipFeedsIn is the write payload for a form's CHIP feeds. Secret keys are
// write-only: a blank secret_key on an existing feed keeps the stored one.
type chipFeedsIn struct {
	Feeds []struct {
		ID              string   `json:"id"`
		Name            string   `json:"name"`
		BrandID         string   `json:"brand_id"`
		SecretKey       string   `json:"secret_key"`
		EmailFallback   string   `json:"email_fallback"`
		MethodWhitelist []string `json:"payment_method_whitelist"`
		Condition       *struct {
			FieldID string `json:"field_id"`
			Op      string `json:"op"`
			Value   string `json:"value"`
		} `json:"condition"`
	} `json:"feeds"`
}

// chipFeedsOut is the read payload: secrets are never returned, only a
// configured flag per feed.
type chipFeedsOut struct {
	Feeds []struct {
		ID              string   `json:"id"`
		Name            string   `json:"name"`
		BrandID         string   `json:"brand_id"`
		EmailFallback   string   `json:"email_fallback"`
		MethodWhitelist []string `json:"payment_method_whitelist"`
		Configured      bool     `json:"configured"`
		Condition       *struct {
			FieldID string `json:"field_id"`
			Op      string `json:"op"`
			Value   string `json:"value"`
		} `json:"condition"`
	} `json:"feeds"`
}

// getFormChipFeeds returns the form's CHIP feeds (secrets masked).
func (h *RESTHandlers) getFormChipFeeds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	feeds, err := h.Forms.ChipFeedsFor(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := chipFeedsOut{}
	for _, f := range feeds {
		item := struct {
			ID              string   `json:"id"`
			Name            string   `json:"name"`
			BrandID         string   `json:"brand_id"`
			EmailFallback   string   `json:"email_fallback"`
			MethodWhitelist []string `json:"payment_method_whitelist"`
			Configured      bool     `json:"configured"`
			Condition       *struct {
				FieldID string `json:"field_id"`
				Op      string `json:"op"`
				Value   string `json:"value"`
			} `json:"condition"`
		}{
			ID: f.ID, Name: f.Name, BrandID: f.BrandID,
			EmailFallback: f.EmailFallback, MethodWhitelist: f.MethodWhitelist,
			Configured: f.BrandID != "" && f.SecretKeyEnc != "",
		}
		if f.Condition != nil {
			item.Condition = &struct {
				FieldID string `json:"field_id"`
				Op      string `json:"op"`
				Value   string `json:"value"`
			}{FieldID: f.Condition.FieldID, Op: f.Condition.Op, Value: f.Condition.Value}
		}
		out.Feeds = append(out.Feeds, item)
	}
	respond(w, http.StatusOK, out)
}

// setFormChipFeeds replaces the form's CHIP feeds. A blank secret_key on an
// existing feed keeps the stored encrypted secret (so the UI can edit a feed
// without re-entering the key).
func (h *RESTHandlers) setFormChipFeeds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireAdminRole(w, r, actx); !ok {
		return
	}
	var in chipFeedsIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	// Load existing feeds so a blank secret_key preserves the stored secret.
	existing := map[string]string{}
	if cur, err := h.Forms.ChipFeedsFor(r.Context(), actx.Account.ID, r.PathValue("id")); err == nil {
		for _, f := range cur {
			existing[f.ID] = f.SecretKeyEnc
		}
	}
	feeds := make([]dbstore.ChipFeed, 0, len(in.Feeds))
	for _, f := range in.Feeds {
		secretEnc := existing[f.ID]
		if f.SecretKey != "" {
			enc, err := cryptoutil.Encrypt(h.SecretsKey, []byte(f.SecretKey))
			if err != nil {
				respondErr(w, r, err)
				return
			}
			secretEnc = enc
		}
		feed := dbstore.ChipFeed{
			ID: f.ID, Name: f.Name, BrandID: f.BrandID,
			SecretKeyEnc: secretEnc, EmailFallback: f.EmailFallback,
			MethodWhitelist: f.MethodWhitelist,
		}
		if f.Condition != nil {
			feed.Condition = &dbstore.ChipFeedCondition{
				FieldID: f.Condition.FieldID, Op: f.Condition.Op, Value: f.Condition.Value,
			}
		}
		feeds = append(feeds, feed)
	}
	if err := h.Forms.SetChipFeeds(r.Context(), actx.Account.ID, r.PathValue("id"), feeds); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "feeds_set"})
}
