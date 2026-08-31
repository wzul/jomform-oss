package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Form models the forms table.
type Form struct {
	ID        string
	AccountID string
	Slug      string
	Title     string
	Config    map[string]any
	Theme     map[string]any
	Status    string // draft | published
	IsMain    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrFormNotFound indicates a missing form.
var ErrFormNotFound = errors.New("form not found")

// FormStore wraps pgx for forms.
type FormStore struct{ pool *pgxpool.Pool }

// NewFormStore builds a FormStore.
func NewFormStore(pool *pgxpool.Pool) *FormStore { return &FormStore{pool: pool} }

const formSelect = `SELECT id, account_id, slug, title, config, theme, status, is_main, created_at, updated_at
	FROM forms`

// Create inserts a form.
func (s *FormStore) Create(ctx context.Context, f *Form) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, theme, status)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id, created_at, updated_at`,
		f.AccountID, f.Slug, f.Title, f.Config, f.Theme, f.Status,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
}

// BySlug fetches a published form by its public slug (any account).
func (s *FormStore) PublishedBySlug(ctx context.Context, slug string) (*Form, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		formSelect+" WHERE slug=$1 AND status='published'", slug))
}

// ListByAccount lists all forms for an account.
func (s *FormStore) ListByAccount(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx, formSelect+" WHERE account_id=$1 ORDER BY created_at DESC", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Form
	for rows.Next() {
		f := &Form{}
		if err := rows.Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
			&f.Status, &f.IsMain, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetByID fetches one form scoped to account.
func (s *FormStore) GetByID(ctx context.Context, accountID, id string) (*Form, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		formSelect+" WHERE id=$2 AND account_id=$1", accountID, id))
}

// Update mutates title/config/theme.
func (s *FormStore) Update(ctx context.Context, accountID, id, title string, config, theme map[string]any) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET title=$3, config=$4, theme=$5, updated_at=now()
		 WHERE id=$2 AND account_id=$1`,
		accountID, id, title, config, theme)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// SetStatus publishes/unpublishes.
func (s *FormStore) SetStatus(ctx context.Context, accountID, id, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET status=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// rowScanner matches pgx.Row for form scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *FormStore) scanOne(row rowScanner) (*Form, error) {
	f := &Form{}
	err := row.Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}
