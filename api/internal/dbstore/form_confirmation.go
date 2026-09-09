// Per-form confirmations (issue #179, Gravity-Forms-style).
package dbstore

import (
	"context"
	"encoding/json"
)

// ConfirmationType enumerates the supported confirmation kinds.
const (
	ConfirmationTypeMessage  = "message"
	ConfirmationTypeRedirect = "redirect"
)

// ConfirmationCondition is the conditional-logic gate for a confirmation. It
// mirrors the builder's Condition shape (field_id/op/value) so the same
// evaluation logic applies. A nil condition means "always matches".
type ConfirmationCondition struct {
	FieldID string `json:"field_id"`
	Op      string `json:"op"` // equals | not_equals | contains | gt | lt | not_empty
	Value   string `json:"value"`
}

// Confirmation is one configurable post-submit confirmation for a form.
type Confirmation struct {
	ID          string                `json:"id"`
	FormID      string                `json:"form_id"`
	Name        string                `json:"name"`
	Type        string                `json:"type"` // message | redirect
	Message     string                `json:"message"`
	RedirectURL string                `json:"redirect_url"`
	Condition   *ConfirmationCondition `json:"condition,omitempty"`
	IsDefault   bool                  `json:"is_default"`
	Position    int                   `json:"position"`
}

// DefaultConfirmationName is the name of the always-present default.
const DefaultConfirmationName = "Default Confirmation"

// DefaultConfirmationMessage is the sensible default message shown when the
// merchant has not customised anything.
const DefaultConfirmationMessage = "Terima kasih! Maklum balas anda telah diterima."

// DefaultFailureMessage is the sensible default shown to a buyer when a
// payment fails and the merchant has not set a per-form failure message
// (issue #393).
const DefaultFailureMessage = "Pembayaran gagal. Sila cuba lagi."

// DefaultConfirmation returns the always-present default confirmation. It has
// no condition (matches always) and is the fallback when no other
// confirmation matches.
func DefaultConfirmation(formID string) *Confirmation {
	return &Confirmation{
		ID:        "default",
		FormID:    formID,
		Name:      DefaultConfirmationName,
		Type:      ConfirmationTypeMessage,
		Message:   DefaultConfirmationMessage,
		IsDefault: true,
	}
}

// Confirmations returns a form's confirmations ordered by position. When the
// form has none, a single default confirmation is returned (and lazily
// persisted) so every form always has one.
func (s *FormStore) Confirmations(ctx context.Context, formID string) ([]*Confirmation, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, form_id, name, type, message, redirect_url, condition, is_default, position
		   FROM form_confirmations WHERE form_id=$1 ORDER BY position, created_at`, formID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Confirmation
	for rows.Next() {
		c := &Confirmation{}
		var cond []byte
		if err := rows.Scan(&c.ID, &c.FormID, &c.Name, &c.Type, &c.Message,
			&c.RedirectURL, &cond, &c.IsDefault, &c.Position); err != nil {
			return nil, err
		}
		if len(cond) > 0 {
			cc := &ConfirmationCondition{}
			if json.Unmarshal(cond, cc) == nil {
				c.Condition = cc
			}
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		def := DefaultConfirmation(formID)
		if _, ierr := s.pool.Exec(ctx,
			`INSERT INTO form_confirmations (form_id, name, type, message, is_default, position)
			 VALUES ($1,$2,$3,$4,true,0) ON CONFLICT DO NOTHING`,
			formID, def.Name, def.Type, def.Message); ierr != nil {
			return nil, ierr
		}
		return []*Confirmation{def}, nil
	}
	return out, nil
}

// SaveConfirmations replaces a form's confirmations wholesale. The default
// confirmation is always preserved: if the payload omits it, it is re-created
// so the form never ends up without one.
func (s *FormStore) SaveConfirmations(ctx context.Context, formID string, confs []*Confirmation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM form_confirmations WHERE form_id=$1`, formID); err != nil {
		return err
	}

	hasDefault := false
	for i, c := range confs {
		if c.IsDefault {
			hasDefault = true
		}
		cond := []byte(nil)
		if c.Condition != nil {
			if b, merr := json.Marshal(c.Condition); merr == nil {
				cond = b
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO form_confirmations (form_id, name, type, message, redirect_url, condition, is_default, position)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			formID, c.Name, c.Type, c.Message, c.RedirectURL, cond, c.IsDefault, i); err != nil {
			return err
		}
	}
	if !hasDefault {
		def := DefaultConfirmation(formID)
		if _, err := tx.Exec(ctx,
			`INSERT INTO form_confirmations (form_id, name, type, message, is_default, position)
			 VALUES ($1,$2,$3,$4,true,$5)`,
			formID, def.Name, def.Type, def.Message, len(confs)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SaleFields returns the submitted field values persisted on a sale (issue
// #179). Returns an empty map when the sale has none stored.
func (s *SaleStore) SaleFields(ctx context.Context, saleID string) (map[string]any, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(fields,'{}'::jsonb) FROM sales WHERE id=$1`, saleID).Scan(&raw); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

// SetSaleFields persists the submitted field values on a sale (issue #179) so
// the thank-you page can evaluate confirmation conditional logic against them.
func (s *SaleStore) SetSaleFields(ctx context.Context, saleID string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET fields=$2, updated_at=now() WHERE id=$1`, saleID, fields)
	return err
}
