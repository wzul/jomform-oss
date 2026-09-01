package dbstore

// Regression tests for issue #16 (SQLSTATE 23502 on form save):
// nil config/theme maps must behave as "not provided", never as NULL.

import (
	"context"
	"testing"
)

func TestCreateWithNilThemeAndConfig(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := &Form{AccountID: acc, Slug: "it-nil-create-" + acc[:8], Title: "IT", Status: "draft"}
	if err := s.Create(ctx, f); err != nil {
		t.Fatalf("Create with nil config/theme: %v", err)
	}
	got, err := s.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Config == nil || got.Theme == nil {
		t.Fatalf("config/theme read back as nil: config=%v theme=%v", got.Config, got.Theme)
	}
}

func TestUpdateWithNilThemeKeepsStoredTheme(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := &Form{
		AccountID: acc, Slug: "it-nil-update-" + acc[:8], Title: "IT",
		Status: "draft",
		Config: map[string]any{"blocks": []any{}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := s.Create(ctx, f); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The bug in #16: a PATCH without a theme key sent nil → NULL → 23502.
	// After the fix, nil theme must keep the stored value.
	if err := s.Update(ctx, acc, f.ID, "IT v2", map[string]any{"blocks": []any{"x"}}, nil); err != nil {
		t.Fatalf("Update with nil theme: %v", err)
	}
	got, err := s.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Theme["primary"] != "#5e6ad2" {
		t.Fatalf("theme primary = %v, want #5e6ad2 (nil update must not wipe theme)", got.Theme["primary"])
	}
	if got.Title != "IT v2" {
		t.Fatalf("title = %q, want IT v2", got.Title)
	}
}

func TestUpdateWithEmptyMapClearsValue(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := &Form{
		AccountID: acc, Slug: "it-empty-update-" + acc[:8], Title: "IT",
		Status: "draft",
		Config: map[string]any{"blocks": []any{"a"}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := s.Create(ctx, f); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Explicit {} means "clear" — callers keep that ability post-fix.
	if err := s.Update(ctx, acc, f.ID, "IT v2", map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("Update with empty maps: %v", err)
	}
	got, err := s.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.Config) != 0 {
		t.Fatalf("config = %v, want {} (explicit clear)", got.Config)
	}
	if len(got.Theme) != 0 {
		t.Fatalf("theme = %v, want {} (explicit clear)", got.Theme)
	}
}

func TestUpdateNotFound(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	err := s.Update(ctx, acc, "00000000-0000-0000-0000-000000000000", "x", nil, nil)
	if err != ErrFormNotFound {
		t.Fatalf("err = %v, want ErrFormNotFound", err)
	}
}
