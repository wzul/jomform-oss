package dbstore

import (
	"context"
	"encoding/json"
)

// ChipFeed models one per-form CHIP feed (issue #208), Gravity-Forms-style.
// A feed carries its own brand_id/secret (secret stored encrypted by the
// caller) plus an optional conditional rule. A feed with no condition is the
// default feed and always matches. Feeds are stored in the form's config
// under the "chip_feeds" key (same pattern as chip_mapping, issue #186), so
// no schema migration is required.
type ChipFeed struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	BrandID        string            `json:"brand_id"`
	SecretKeyEnc   string            `json:"secret_key_enc"`
	EmailFallback  string            `json:"email_fallback"`
	MethodWhitelist []string          `json:"method_whitelist"`
	Condition      *ChipFeedCondition `json:"condition"`
}

// ChipFeedCondition is the conditional rule that gates a feed: it matches
// when the submitted value of field_id satisfies op against value. Ops mirror
// the builder's block-visibility conditions (equals | not_equals | contains |
// gt | lt | not_empty).
type ChipFeedCondition struct {
	FieldID string `json:"field_id"`
	Op      string `json:"op"`
	Value   string `json:"value"`
}

// chipFeedsFromConfig extracts the chip_feeds array from a form's config.
func chipFeedsFromConfig(config map[string]any) []ChipFeed {
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		ChipFeeds []ChipFeed `json:"chip_feeds"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.ChipFeeds
}

// ChipFeedsFor returns the form's configured CHIP feeds (may be empty).
func (s *FormStore) ChipFeedsFor(ctx context.Context, accountID, formID string) ([]ChipFeed, error) {
	f, err := s.GetByID(ctx, accountID, formID)
	if err != nil {
		return nil, err
	}
	return chipFeedsFromConfig(f.Config), nil
}

// SetChipFeeds replaces the form's chip_feeds list in its config, leaving all
// other config keys untouched (jsonb_set). Pass nil to clear all feeds.
func (s *FormStore) SetChipFeeds(ctx context.Context, accountID, formID string, feeds []ChipFeed) error {
	var payload any
	if feeds == nil {
		payload = []any{}
	} else {
		payload = feeds
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET config = jsonb_set(config, '{chip_feeds}', $3::jsonb), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, formID, string(raw))
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}
