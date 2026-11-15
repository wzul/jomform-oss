package httpapi

// One sentence, every door that can refuse a template publish for missing
// gallery metadata (card t_80ea6b64, extended by the forms state machine).
//
// A bulk publish that skips a template for missing gallery metadata reports the
// skip on both surfaces: POST /v1/rest/forms/bulk returns skipped[] +
// skipped_reason, and the MCP bulk_forms tool now returns the same pair.
//
// The sentence lives more than once ON PURPOSE, and only where it must:
//   - httpapi must spell it as a LITERAL, because errcode.go's message→code
//     table requires every key to appear as a string literal in a non-errcode.go
//     file of this package (see errors.go's own note);
//   - mcpsrv cannot import it back, because httpapi imports mcpsrv;
//   - internal/lifecycle cannot import httpapi either (httpapi imports it), so
//     the table's publish guard reads formfields.TemplateNeedsMetadata.
//
// A duplicated literal is exactly the thing that drifts, so this pins all three
// to one string. No database, no network — it always runs.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

func TestBulkSkipReasonIsOneSentenceOnBothDoors(t *testing.T) {
	doors := []struct{ name, sentence string }{
		{"REST (templateNeedMetadataWire)", templateNeedMetadataWire},
		{"MCP  (BulkGallerySkippedReason)", mcpsrv.BulkGallerySkippedReason},
		{"lifecycle (formfields.TemplateNeedsMetadata)", formfields.TemplateNeedsMetadata},
	}
	for _, d := range doors[1:] {
		if d.sentence != doors[0].sentence {
			t.Fatalf("the template-publish refusal sentence drifted:\n"+
				"  REST (templateNeedMetadataWire): %q\n"+
				"  %s: %q",
				doors[0].sentence, d.name, d.sentence)
		}
	}
}
