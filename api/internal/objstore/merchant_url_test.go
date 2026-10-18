package objstore

// t_0fe14ba5 — MerchantURL is the one place the "which host does this media url
// use" decision lives, so REST (httpapi) and MCP (mcpsrv) cannot drift. These
// are the pure cases; the end-to-end proof (the emitted url fetched on its own
// host answering 200) lives in httpapi/media_url_tenant_host_test.go, because it
// needs the real tenant gate and a real workspace row.

import "testing"

func TestMerchantURLMovesCodeAssetsToTheTenantHost(t *testing.T) {
	const key = "u/11111111-1111-1111-1111-111111111111/x.css"
	canonical := "https://jomform.com/media/" + key

	cases := []struct {
		name      string
		canonical string
		mime      string
		host      string
		want      string
	}{
		{
			name:      "code asset on a workspace host",
			canonical: canonical,
			mime:      "text/css; charset=utf-8",
			host:      "wan.jomform.com",
			want:      "https://wan.jomform.com/media/" + key,
		},
		{
			name:      "code asset on a verified custom domain",
			canonical: canonical,
			mime:      "text/css; charset=utf-8",
			host:      "forms.example.com",
			want:      "https://forms.example.com/media/" + key,
		},
		{
			// Dev: S3_PUBLIC_BASE is a host-mapped local base, so the tenant host
			// must keep the port the canonical url carries or the url is dead.
			name:      "port is preserved",
			canonical: "http://localhost:8080/media/" + key,
			mime:      "text/css; charset=utf-8",
			host:      "wan.jomform.com",
			want:      "http://wan.jomform.com:8080/media/" + key,
		},
		{
			// Every code-asset type, not just CSS: a JS file and a font are the
			// same tenant-gated route.
			name:      "javascript",
			canonical: canonical,
			mime:      "text/javascript; charset=utf-8",
			host:      "wan.jomform.com",
			want:      "https://wan.jomform.com/media/" + key,
		},
		{
			name:      "svg",
			canonical: canonical,
			mime:      "image/svg+xml",
			host:      "wan.jomform.com",
			want:      "https://wan.jomform.com/media/" + key,
		},
		{
			// Images stay public on the canonical media host: forms embed them
			// cross-origin, and moving them would break every stored url.
			name:      "image keeps the canonical host",
			canonical: "https://jomform.com/media/u/acct/x.png",
			mime:      "image/png",
			host:      "wan.jomform.com",
			want:      "https://jomform.com/media/u/acct/x.png",
		},
		{
			name:      "pdf keeps the canonical host",
			canonical: "https://jomform.com/media/u/acct/x.pdf",
			mime:      "application/pdf",
			host:      "wan.jomform.com",
			want:      "https://jomform.com/media/u/acct/x.pdf",
		},
		{
			// No public host resolved (no workspace, no verified custom domain):
			// the canonical url, unchanged — never an invented host.
			name:      "no tenant host falls back to canonical",
			canonical: canonical,
			mime:      "text/css; charset=utf-8",
			host:      "",
			want:      canonical,
		},
		{
			name:      "empty canonical",
			canonical: "",
			mime:      "text/css; charset=utf-8",
			host:      "wan.jomform.com",
			want:      "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MerchantURL(tc.canonical, tc.mime, tc.host); got != tc.want {
				t.Fatalf("MerchantURL(%q, %q, %q) = %q, want %q", tc.canonical, tc.mime, tc.host, got, tc.want)
			}
		})
	}
}

// TestPublicURLOnHostKeepsEverythingButTheHost pins the mechanical half: the
// purge path rewrites the advertised url's host to invalidate the CDN entry a
// form actually fetched, so scheme, port and path must survive untouched.
func TestPublicURLOnHostKeepsEverythingButTheHost(t *testing.T) {
	cases := []struct {
		canonical, host, want string
	}{
		{"https://jomform.com/media/u/a/x.css", "wan.jomform.com", "https://wan.jomform.com/media/u/a/x.css"},
		{"https://jomform.com/media/u/a/x.css?v=1", "wan.jomform.com", "https://wan.jomform.com/media/u/a/x.css?v=1"},
		{"http://localhost:8080/media/u/a/x.css", "wan.jomform.com", "http://wan.jomform.com:8080/media/u/a/x.css"},
		{"https://jomform.com/media/u/a/x.css", "", "https://jomform.com/media/u/a/x.css"},
		{"", "wan.jomform.com", ""},
		{"not a url", "wan.jomform.com", "not a url"},
	}
	for _, tc := range cases {
		if got := PublicURLOnHost(tc.canonical, tc.host); got != tc.want {
			t.Fatalf("PublicURLOnHost(%q, %q) = %q, want %q", tc.canonical, tc.host, got, tc.want)
		}
	}
}
