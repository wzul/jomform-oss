// XML well-formedness for merchant-supplied SVG.
//
// WHY THIS EXISTS (t_20af3915). v1.21.233 correctly classifies a `/* ... */`-led
// SVG as `.svg` / `image/svg+xml` instead of `.js`, and the bytes are then
// served as-is with `X-Content-Type-Options: nosniff`. That type is truthful,
// but the document is NOT well-formed XML: text before the root element is a
// fatal parse error, so the browser renders a `parsererror` where the merchant
// expects their logo (measured on prod v1.21.233: `document.documentElement`
// is `html`, zero `<svg>` elements, "error on line 1 at column 1: Start tag
// expected, '<' not found"). An XML-legal `<!-- ... -->` comment on the same
// serve path renders, so the type and the serve path were never the problem —
// the payload is, and nothing validated it.
package objstore

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// ErrNoRootElement is the message a body with no XML root element answers.
var (
	ErrNoRootElement      = errors.New("no root element")
	ErrTextBeforeRoot     = errors.New("text before the root element (a /* */ banner is not XML)")
	ErrTextAfterRoot      = errors.New("text after the root element")
	ErrMultipleRoots      = errors.New("more than one root element")
	ErrDeclarationNotTop  = errors.New("the XML declaration must be at the start of the document")
	ErrDirectiveAfterRoot = errors.New("DOCTYPE directive after the root element")
)

// SVGNotXMLMessageLeadIn is the fixed lead-in of the message a non-well-formed
// SVG answers. It is exported because THREE call sites build that message from
// it — the two REST upload handlers and the MCP upload_media asset path — and
// because errcode.go keys the machine code on it (prefix table), so the wire
// wording, the tool error and the dashboard's code mapping cannot drift apart.
const SVGNotXMLMessageLeadIn = "the SVG file is not valid XML"

// SVGNotXMLError returns the refusal message for merchant bytes that are not a
// well-formed XML document — the bar a browser's XML parser holds an
// `image/svg+xml` asset to — or "" when the document is fine.
//
// The decoder's own diagnostic follows the lead-in because it names the line the
// merchant has to fix ("XML syntax error on line 1: …").
func SVGNotXMLError(data []byte) string {
	if err := ValidateXMLDocument(data); err != nil {
		return SVGNotXMLMessageLeadIn + ": " + err.Error()
	}
	return ""
}

// ValidateXMLDocument reports whether data is a well-formed XML document — the
// bar a browser's XML parser holds an `image/svg+xml` asset to before it draws
// anything.
//
// A bare `xml.Decoder` drain to EOF is NOT that bar, and this is the point of
// measuring rather than assuming: encoding/xml returns character data before
// the root, more than one root element, and a document with no root at all
// without complaint (`"/* x */<svg/>"` decodes cleanly as CharData + one
// element). A browser treats each of those as fatal, which is exactly the
// parsererror this check exists to keep off a merchant's page. So the token
// stream is drained AND the document shape is enforced:
//
//   - a legal prolog: comments, processing instructions and at most one
//     DOCTYPE, with an XML declaration only as the very first token;
//   - exactly one root element;
//   - after it, only whitespace, comments and processing instructions.
//
// Tag balance, entity validity and illegal characters are left to the decoder,
// which does enforce those. Namespaces are not checked: well-formedness is the
// contract here, not SVG validity.
//
// A UTF-8 BOM is legal XML (Windows editors write it) but the decoder surfaces
// it as character data, so it is stripped first — otherwise a perfectly
// renderable file would be rejected as text-before-root.
func ValidateXMLDocument(data []byte) error {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	dec := xml.NewDecoder(bytes.NewReader(data))

	depth := 0
	seenRoot := false
	rootClosed := false
	tokens := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.ProcInst:
			// The XML declaration is legal only before everything else
			// (XML 1.0 §2.8); the decoder accepts it anywhere, a browser does
			// not.
			if strings.EqualFold(t.Target, "xml") && tokens > 0 {
				return ErrDeclarationNotTop
			}
		case xml.Directive:
			if seenRoot || rootClosed {
				return ErrDirectiveAfterRoot
			}
		case xml.Comment:
			// Legal anywhere outside a start/end tag.
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) > 0 {
				if seenRoot {
					return ErrTextAfterRoot
				}
				return ErrTextBeforeRoot
			}
		case xml.StartElement:
			if rootClosed {
				return ErrMultipleRoots
			}
			seenRoot = true
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				rootClosed = true
			}
		}
		tokens++
	}
	if !seenRoot {
		return ErrNoRootElement
	}
	return nil
}
