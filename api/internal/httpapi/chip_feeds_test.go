package httpapi

import (
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestResolveChipFeed(t *testing.T) {
	cond := func(field, op, value string) *dbstore.ChipFeedCondition {
		return &dbstore.ChipFeedCondition{FieldID: field, Op: op, Value: value}
	}
	feed := func(id string, c *dbstore.ChipFeedCondition) dbstore.ChipFeed {
		return dbstore.ChipFeed{ID: id, BrandID: "br_" + id, SecretKeyEnc: "enc_" + id, Condition: c}
	}

	tests := []struct {
		name   string
		feeds  []dbstore.ChipFeed
		fields map[string]string
		want   string // expected feed id, "" = none
	}{
		{
			name:   "no feeds -> none",
			feeds:  nil,
			fields: map[string]string{},
			want:   "",
		},
		{
			name:   "default feed always matches",
			feeds:  []dbstore.ChipFeed{feed("default", nil)},
			fields: map[string]string{},
			want:   "default",
		},
		{
			name: "first matching conditional wins",
			feeds: []dbstore.ChipFeed{
				feed("a", cond("country", "equals", "MY")),
				feed("b", cond("country", "equals", "SG")),
				feed("default", nil),
			},
			fields: map[string]string{"country": "SG"},
			want:   "b",
		},
		{
			name: "no conditional matches -> default",
			feeds: []dbstore.ChipFeed{
				feed("a", cond("country", "equals", "MY")),
				feed("default", nil),
			},
			fields: map[string]string{"country": "US"},
			want:   "default",
		},
		{
			name: "conditional before default wins over default",
			feeds: []dbstore.ChipFeed{
				feed("a", cond("tier", "not_equals", "free")),
				feed("default", nil),
			},
			fields: map[string]string{"tier": "pro"},
			want:   "a",
		},
		{
			name: "not_empty op",
			feeds: []dbstore.ChipFeed{
				feed("a", cond("email", "not_empty", "")),
				feed("default", nil),
			},
			fields: map[string]string{"email": "x@y.z"},
			want:   "a",
		},
		{
			name: "no matching conditional and no default -> none",
			feeds: []dbstore.ChipFeed{
				feed("a", cond("country", "equals", "MY")),
			},
			fields: map[string]string{"country": "US"},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveChipFeed(tt.feeds, tt.fields)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("resolveChipFeed = %q, want none", got.ID)
				}
				return
			}
			if got == nil || got.ID != tt.want {
				if got == nil {
					t.Fatalf("resolveChipFeed = nil, want %q", tt.want)
				}
				t.Fatalf("resolveChipFeed = %q, want %q", got.ID, tt.want)
			}
		})
	}
}
