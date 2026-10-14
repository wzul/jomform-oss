package formgen

// Card t_4975927e — the AI form generator must not mint a coupon block.
//
// The block was withdrawn from the builder palette (web/src/app/builder/page.tsx)
// and from the public renderer because nothing on the submit/order path ever
// applied a discount: order.go charges payments.SumItems(subtotal, 0, 0), there
// is no coupon table and no server-side validation. The generator was the third
// surface still offering it — `ValidBlockTypes["coupon"]` sat in the allowlist
// and the prompt named `coupon` among the types the model may use, so an MCP
// agent could create a dead coupon that the merchant then saw in the builder.
//
// The assertions are deliberately three, because each is a different way the
// type can come back: the allowlist, the prompt the model reads, and the
// validation of a model reply that names it anyway.

import (
	"strings"
	"testing"
)

func TestCouponBlockIsNotAnAllowedGeneratorType(t *testing.T) {
	if ValidBlockTypes[BlockType("coupon")] {
		t.Errorf("ValidBlockTypes still allows `coupon` — the generator can mint a block nothing applies")
	}
	if p := Prompt("form dengan kod diskaun"); strings.Contains(p, "coupon") {
		t.Errorf("the generator prompt still offers `coupon` to the model:\n%s", p)
	}
}

func TestGeneratorRefusesAModelReplyCarryingCoupon(t *testing.T) {
	_, err := Parse(`{"title":"Diskaun","blocks":[{"id":"cp1","type":"coupon","value":"Kod diskaun"}]}`)
	if err == nil {
		t.Fatalf("Parse accepted a coupon block — the generator can still create one")
	}
	if !strings.Contains(err.Error(), "coupon") {
		t.Errorf("the refusal does not name the block type: %v", err)
	}

	// CONTROL, both directions: a servable block still parses, so the rule
	// refuses the withdrawn type and not the shape.
	if _, err := Parse(`{"title":"Biasa","blocks":[{"id":"tx1","type":"text","value":"Nama"}]}`); err != nil {
		t.Errorf("Parse refused an ordinary text block: %v", err)
	}
}
