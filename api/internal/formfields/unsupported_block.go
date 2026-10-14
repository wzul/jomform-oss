package formfields

// The UNSUPPORTED-BLOCK rule — the write-side half of the "a placed block that
// no renderer can honour must not be storable" class (card t_4086b8f0).
//
// THE DEFECT. The builder palette offers 30 block types; the public page is
// painted by two renderers (render_blocks.go: blockTmpl for a payment form,
// responseTmpl for a response form). `file` was placed by the merchant, saved by
// the server, and then silently dropped: on a response form by
// renderResponseForm's switch AND by the template (the block reached neither),
// so the merchant saw the block on the builder canvas and the buyer saw nothing
// at all. On a payment form it was worse than absent — blockTmpl DOES paint a
// `<input type="file">`, and the submit JS drops the picked file on the floor
// ("files not sent as text"), so the buyer believed an attachment had been sent
// and the merchant received a line item with the bytes missing.
//
// WHY `file` CANNOT BE RENDERED HERE. Every upload endpoint in this product
// (uploads/image, uploads/asset, uploads/avatar) sits behind an authenticated
// account with a write role and charges that workspace's media quota; a public
// form is filled by an ANONYMOUS buyer, and no public upload route exists
// (public.go's mux has none). The two other field-type authorities already say
// the same thing: this package's ValidTypes (the allowlist of "input field types
// JomForm renders and records", issue #254) and customform's validType (#255)
// both omit `file`. So a `file` block can neither be answered on the page nor
// recorded on submit, and the only honest outcome is to refuse the config that
// carries it rather than store a block whose promise is false.
//
// WHERE THE RULE IS APPLIED. At every write path that stores a caller-supplied
// form/template config — REST forms create/update, REST own-template
// create/update, MCP upsert_form create/update, MCP create_template/
// update_template — beside the package-urlguard checks those paths already
// share, so REST and MCP cannot disagree.
//
// WHY A DENYLIST AND NOT "every type not in ValidTypes": a config's blocks also
// carry decorative and pricing types (section, html, image, divider, product,
// quantity, option, shipping, total, coupon) which are NOT field types and are
// perfectly servable. The rule is about the (currently one) type that claims to
// be an input and cannot be one.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// unsupportedBlockTypes maps a builder block type JomForm cannot serve to the
// reason, so the rejection can say WHY and not only what.
var unsupportedBlockTypes = map[string]string{
	"file": "borang tidak boleh mengumpul lampiran: tiada laluan muat naik untuk pembeli",
}

// UnsupportedBlock returns the first block type in config["blocks"] that no
// renderer can honour, or "" when every placed block is servable. A config with
// no blocks key, a non-array blocks (that is the config-shape rule's refusal,
// urlguard.CheckConfigShape) or a non-block entry yields "" — this rule reports
// only what it recognises, so it can never refuse a shape it does not own.
func UnsupportedBlock(config map[string]any) string {
	if config == nil {
		return ""
	}
	raw, ok := config["blocks"]
	if !ok || raw == nil {
		return ""
	}
	// Decode through JSON rather than asserting []any directly: the write paths
	// hand over a map straight from the request body, but the same rule is
	// asked about a config read back from the database, where the array holds
	// json.RawMessage-ish values. One decode, one answer.
	body, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		t := strings.TrimSpace(b.Type)
		if _, bad := unsupportedBlockTypes[t]; bad {
			return t
		}
	}
	return ""
}

// UnsupportedBlockMessage is the user-facing rejection for a config carrying a
// block type no renderer can honour: it NAMES the type (so the merchant knows
// which block to delete) and says why it cannot be stored. Malay, like every
// other API validation message in this repo, and the ONE wording used by every
// write path — REST returns it as the 400 body, MCP as the tool error.
func UnsupportedBlockMessage(blockType string) string {
	if blockType == "" {
		return ""
	}
	reason := unsupportedBlockTypes[blockType]
	if reason == "" {
		reason = "tiada renderer menyokongnya"
	}
	return fmt.Sprintf("blok %s tidak disokong: %s — padam blok itu sebelum menyimpan", blockType, reason)
}
