package objstore

import "testing"

// TestExceedsWorkspaceQuota (issue #384): the per-workspace 1GB storage quota is
// enforced by summing the account's current stored bytes and rejecting any
// upload that would push the total over the cap. Exactly-at-the-limit is
// allowed; anything over is rejected.
func TestExceedsWorkspaceQuota(t *testing.T) {
	limit := int64(MaxWorkspaceBytes)
	cases := []struct {
		name       string
		current    int64
		additional int64
		want       bool
	}{
		{"empty-workspace-small-upload", 0, 2 << 20, false},
		{"exactly-at-limit", limit - (2 << 20), 2 << 20, false},
		{"one-byte-over", limit - 1, 2, true},
		{"already-over", limit + 1, 0, true},
		{"huge-upload-over", 0, limit + 1, true},
		{"zero-additional-at-limit", limit, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExceedsWorkspaceQuota(c.current, c.additional); got != c.want {
				t.Fatalf("ExceedsWorkspaceQuota(%d, %d) = %v, want %v", c.current, c.additional, got, c.want)
			}
		})
	}
}
