package httpapi

import "testing"

// Issue #231: the merchant's stored PrivacyURL/TermsURL/RefundURL may point at
// a stale/wrong workspace subdomain. When a link targets one of the workspace's own
// hosted legal pages ({workspace}.jomform.com/privacy|terms|refund), it must be
// rewritten to the CURRENT workspace's subdomain so the footer link always
// resolves. External URLs are left untouched.
func TestRewriteHostedLegalURL(t *testing.T) {
	cases := []struct {
		name, raw, slug, want string
	}{
		{
			name: "stale privacy slug rewritten to current workspace",
			raw:  "https://wanzulhosting.jomform.com/privacy",
			slug: "wan",
			want: "https://wan.jomform.com/privacy",
		},
		{
			name: "stale terms slug rewritten to current workspace",
			raw:  "https://wanzulhosting.jomform.com/terms",
			slug: "wan",
			want: "https://wan.jomform.com/terms",
		},
		{
			name: "stale refund slug rewritten to current workspace",
			raw:  "https://wanzulhosting.jomform.com/refund",
			slug: "wan",
			want: "https://wan.jomform.com/refund",
		},
		{
			name: "already-correct hosted URL unchanged",
			raw:  "https://wan.jomform.com/privacy",
			slug: "wan",
			want: "https://wan.jomform.com/privacy",
		},
		{
			name: "external URL unchanged",
			raw:  "https://acme.example/privacy",
			slug: "wan",
			want: "https://acme.example/privacy",
		},
		{
			name: "hosted URL with non-legal path unchanged",
			raw:  "https://wanzulhosting.jomform.com/contact",
			slug: "wan",
			want: "https://wanzulhosting.jomform.com/contact",
		},
		{
			name: "empty raw unchanged",
			raw:  "",
			slug: "wan",
			want: "",
		},
		{
			name: "empty slug leaves raw unchanged",
			raw:  "https://wanzulhosting.jomform.com/privacy",
			slug: "",
			want: "https://wanzulhosting.jomform.com/privacy",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rewriteHostedLegalURL(c.raw, c.slug); got != c.want {
				t.Errorf("rewriteHostedLegalURL(%q, %q) = %q, want %q", c.raw, c.slug, got, c.want)
			}
		})
	}
}
