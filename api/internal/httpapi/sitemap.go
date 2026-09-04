// Issue #276: sitemap.xml support.
//
// The web app (Next.js) serves the primary sitemap.xml for the jomform.com
// apex. Custom-domain spaces that have indexing enabled are served here by the
// Go API (which owns the account_domains + space_settings tables), so a
// merchant's custom domain can expose its own sitemap.xml listing the space's
// published forms. The web app's sitemap.ts proxies this endpoint to merge
// the indexed custom-domain spaces into the apex sitemap.
package httpapi

import (
	"encoding/xml"
	"net/http"
	"strings"
)

// sitemapURL is one <url> entry in a sitemap.
type sitemapURL struct {
	Loc string `xml:"loc"`
}

// sitemapIndex is the XML sitemap document.
type sitemapIndex struct {
	XMLName xml.Name    `xml:"urlset"`
	Xmlns   string      `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// handleSitemap serves sitemap.xml for the current host. On a custom domain
// it lists the space's published forms (indexed spaces only). On jomform.com
// subdomains it returns an empty sitemap (those forms are noindexed). The
// jomform.com apex is handled by the web app; this endpoint is a fallback.
func (h *PublicHandlers) handleSitemap(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)

	// Custom domain: list published forms for the owning account.
	if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, host); cd != "" {
		// Only index when the space's indexing toggle is on.
		if h.Spaces != nil {
			acc, err := h.Accounts.BySlug(r.Context(), cd)
			if err == nil && acc != nil {
				idx, ierr := h.Spaces.IndexingByAccount(r.Context(), acc.ID)
				if ierr == nil && !idx {
					writeSitemap(w, nil)
					return
				}
			}
		}
		forms, err := h.Forms.ListPublishedByAccountSlug(r.Context(), cd)
		if err != nil {
			writeSitemap(w, nil)
			return
		}
		urls := make([]sitemapURL, 0, len(forms))
		for _, f := range forms {
			urls = append(urls, sitemapURL{Loc: "https://" + host + "/" + f.Slug})
		}
		writeSitemap(w, urls)
		return
	}

	// jomform.com subdomain or bare apex: no indexable forms here.
	writeSitemap(w, nil)
}

// handleSitemapSpaces serves the list of indexed custom-domain space URLs
// (issue #276). The web app's sitemap.ts fetches this to merge the indexed
// custom-domain spaces into the jomform.com apex sitemap.
func (h *PublicHandlers) handleSitemapSpaces(w http.ResponseWriter, r *http.Request) {
	if h.Spaces == nil {
		writeJSON(w, http.StatusOK, map[string]any{"spaces": []string{}})
		return
	}
	urls, err := h.Spaces.IndexedSpaceURLs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"spaces": []string{}})
		return
	}
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		out = append(out, "https://"+u)
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": out})
}

func writeSitemap(w http.ResponseWriter, urls []sitemapURL) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	doc := sitemapIndex{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: urls}
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(doc)
}
