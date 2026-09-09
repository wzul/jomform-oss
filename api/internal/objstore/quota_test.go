package objstore

import "testing"

// TestExceedsSpaceQuota (issue #384): the per-space 1GB storage quota is
// enforced by summing the account's current stored bytes and rejecting any
// upload that would push the total over the cap. Exactly-at-the-limit is
// allowed; anything over is rejected.
func TestExceedsSpaceQuota(t *testing.T) {
	limit := int64(MaxSpaceBytes)
	cases := []struct {
		name       string
		current    int64
		additional int64
		want       bool
	}{
		{"empty-space-small-upload", 0, 2 << 20, false},
		{"exactly-at-limit", limit - (2 << 20), 2 << 20, false},
		{"one-byte-over", limit - 1, 2, true},
		{"already-over", limit + 1, 0, true},
		{"huge-upload-over", 0, limit + 1, true},
		{"zero-additional-at-limit", limit, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExceedsSpaceQuota(c.current, c.additional); got != c.want {
				t.Fatalf("ExceedsSpaceQuota(%d, %d) = %v, want %v", c.current, c.additional, got, c.want)
			}
		})
	}
}
