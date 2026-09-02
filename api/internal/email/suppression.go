// Suppression check (issue #56): the canonical "should this send be
// suppressed?" helper. Every outbound email path — verification sends and
// payment receipts alike — consults this before calling SES. The email package
// owns the helper so any code that wants to send can reuse it without knowing
// the underlying table.
package email

import (
	"context"
	"strings"
)

// Suppressor reports whether an address is on the suppression list.
//
// dbstore.SuppressionStore satisfies this directly (IsSuppressed has the same
// signature), keeping the email package free of a DB dependency while still
// giving a single reusable seam for every sender.
type Suppressor interface {
	IsSuppressed(ctx context.Context, email string) (bool, error)
}

// ShouldSuppress reports whether a message to `to` must be skipped because the
// address is suppressed. It returns suppressed=true with err==nil when the
// address is suppressed. If the underlying store errors, it returns
// (false, err) — senders must NOT silently drop revenue-critical mail on a
// store failure, so a suppression check failure is surfaced to the caller.
func ShouldSuppress(ctx context.Context, s Suppressor, to string) (bool, error) {
	return s.IsSuppressed(ctx, strings.ToLower(strings.TrimSpace(to)))
}
