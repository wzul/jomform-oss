// Favicon endpoints for the MCP host (issue #142). Claude Desktop fetches the
// connector's favicon from the server origin; serve the JomForm brand icon so
// the connector shows a real icon instead of a blank/generic one.
package httpapi

import (
	"embed"
	"net/http"
	"strconv"
)

//go:embed favicon/icon.svg favicon/icon-192.png
var faviconFS embed.FS

// faviconCache is a sensible cache header for a static, immutable brand asset.
const faviconCache = "public, max-age=86400"

// handleFaviconICO serves the brand icon for GET /favicon.ico. Claude Desktop
// accepts SVG/PNG favicons, so we serve the SVG with the correct content-type.
func (s *Server) handleFaviconICO(w http.ResponseWriter, r *http.Request) {
	s.serveFavicon(w, r, "favicon/icon.svg", "image/svg+xml")
}

// handleIconSVG serves the brand SVG for GET /icon.svg.
func (s *Server) handleIconSVG(w http.ResponseWriter, r *http.Request) {
	s.serveFavicon(w, r, "favicon/icon.svg", "image/svg+xml")
}

// handleIconPNG serves the brand PNG for GET /icon-192.png.
func (s *Server) handleIconPNG(w http.ResponseWriter, r *http.Request) {
	s.serveFavicon(w, r, "favicon/icon-192.png", "image/png")
}

// serveFavicon writes an embedded brand asset with the correct content-type
// and a cache header. Assets are embedded into the binary, so there is no
// external file dependency at runtime.
func (s *Server) serveFavicon(w http.ResponseWriter, r *http.Request, name, contentType string) {
	data, err := faviconFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", faviconCache)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
