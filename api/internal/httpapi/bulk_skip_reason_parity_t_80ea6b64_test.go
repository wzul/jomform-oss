package httpapi

// One sentence, two doors (card t_80ea6b64).
//
// A bulk publish that skips a template for missing gallery metadata reports the
// skip on both surfaces: POST /v1/rest/forms/bulk returns skipped[] +
// skipped_reason, and the MCP bulk_forms tool now returns the same pair. The
// sentence lives twice because httpapi imports mcpsrv (so mcpsrv cannot import
// templateNeedMetadataWire back), and a duplicated literal is exactly the thing
// that drifts: this pins them to one string.
//
// No database, no network — it always runs.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/mcpsrv"
)

func TestBulkSkipReasonIsOneSentenceOnBothDoors(t *testing.T) {
	if templateNeedMetadataWire != mcpsrv.BulkGallerySkippedReason {
		t.Fatalf("the bulk skip sentence drifted between the REST and MCP doors:\n"+
			"  REST (templateNeedMetadataWire): %q\n"+
			"  MCP  (BulkGallerySkippedReason): %q",
			templateNeedMetadataWire, mcpsrv.BulkGallerySkippedReason)
	}
}
