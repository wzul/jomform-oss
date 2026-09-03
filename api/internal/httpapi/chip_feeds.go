// CHIP feed resolution (issue #208): pick the first matching per-form CHIP
// feed for a submission, Gravity-Forms-style. A feed with no condition is the
// default feed and always matches; feeds are evaluated in order and the first
// match wins. Falls back to nil (account-level creds) when no feed matches.
package httpapi

import (
	"github.com/wzul/jomform/api/internal/dbstore"
)

// resolveChipFeed evaluates a form's CHIP feeds against the submitted field
// values and returns the first matching feed. A feed with a nil condition
// (the default feed) always matches. Returns nil when no feed matches, in
// which case the caller falls back to the account-level CHIP credentials.
func resolveChipFeed(feeds []dbstore.ChipFeed, fields map[string]string) *dbstore.ChipFeed {
	for i := range feeds {
		f := &feeds[i]
		if f.Condition == nil {
			return f
		}
		if conditionMet(&Condition{
			FieldID: f.Condition.FieldID,
			Op:      f.Condition.Op,
			Value:   f.Condition.Value,
		}, fields, nil) {
			return f
		}
	}
	return nil
}
