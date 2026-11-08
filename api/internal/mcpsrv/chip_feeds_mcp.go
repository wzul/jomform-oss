// MCP tools: per-form CHIP feeds (issue #208). Multiple feeds with conditional
// logic, Gravity-Forms-style. Secrets are encrypted at rest and never returned.
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

type chipFeedIn struct {
	ID              string   `json:"id" jsonschema:"Unique feed ID (e.g. feed_1)"`
	Name            string   `json:"name" jsonschema:"Feed name"`
	BrandID         string   `json:"brand_id" jsonschema:"CHIP Brand ID for this feed"`
	SecretKey       string   `json:"secret_key" jsonschema:"CHIP Secret Key (will be encrypted). Empty on an existing feed = keep the old key"`
	EmailFallback   string   `json:"email_fallback,omitempty" jsonschema:"Fallback email when the buyer provides no email (one-time only)"`
	MethodWhitelist []string `json:"payment_method_whitelist,omitempty" jsonschema:"List of payment methods, empty = all"`
	Condition       *struct {
		FieldID string `json:"field_id" jsonschema:"ID of the form block being evaluated"`
		Op      string `json:"op" jsonschema:"equals | not_equals | contains | gt | lt | not_empty"`
		Value   string `json:"value" jsonschema:"Comparison value"`
	} `json:"condition,omitempty" jsonschema:"Conditional logic. Empty = default feed (always matches)"`
}

type chipFeedsIn struct {
	FormID string       `json:"form_id" jsonschema:"Form ID"`
	Feeds  []chipFeedIn `json:"feeds" jsonschema:"List of the form's CHIP feeds. A feed with no condition = default (always matches). The first matching feed is used."`
}

type chipFeedOut struct {
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
}

// getFormChipFeedsMCP returns a form's CHIP feeds (secrets masked).
func (s *Server) getFormChipFeedsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, *listOutput[chipFeedOut], error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	// Issue #307: the form may live in any of the user's workspaces.
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	feeds, err := s.deps.Forms.ChipFeedsFor(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]chipFeedOut, 0, len(feeds))
	for _, f := range feeds {
		item := chipFeedOut{
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
		out = append(out, item)
	}
	return nil, newListOut(out), nil
}

// toChipFeeds converts MCP input feeds to dbstore feeds, preserving the
// stored encrypted secret for existing feeds when a blank secret_key is given.
func (s *Server) toChipFeeds(ctx context.Context, acc, formID string, in []chipFeedIn) ([]dbstore.ChipFeed, error) {
	existing := map[string]string{}
	if cur, err := s.deps.Forms.ChipFeedsFor(ctx, acc, formID); err == nil {
		for _, f := range cur {
			existing[f.ID] = f.SecretKeyEnc
		}
	}
	feeds := make([]dbstore.ChipFeed, 0, len(in))
	for _, f := range in {
		secretEnc := existing[f.ID]
		if f.SecretKey != "" {
			enc, err := cryptoutil.Encrypt(s.decKey, []byte(f.SecretKey))
			if err != nil {
				return nil, err
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
	return feeds, nil
}

// setFormChipFeedsMCP replaces a form's CHIP feeds. A blank secret_key on an
// existing feed keeps the stored encrypted secret.
func (s *Server) setFormChipFeedsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in chipFeedsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil || s.decKey == nil {
		return nil, nil, errors.New("feeds not available")
	}
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	feeds, err := s.toChipFeeds(ctx, acc, in.FormID, in.Feeds)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SetChipFeeds(ctx, acc, in.FormID, feeds); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "feeds_set", "form_id": in.FormID}, nil
}
