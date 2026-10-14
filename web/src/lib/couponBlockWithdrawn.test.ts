// Card t_4975927e — the `coupon` builder block is withdrawn, and no web surface
// may promise it again.
//
// THE DEFECT. The builder offered a "Coupon" block (Pricing group, hint "Discount
// code"), the Go renderer painted a text input for it, and nothing on the submit
// or order path ever applied a discount — order.go charges
// `payments.SumItems(subtotal, 0, 0)`, there is no coupon table and no server
// validation. A buyer typed a code, got no error, and paid full price.
//
// WHY A SOURCE SCAN AND NOT A RENDER. The palette, the i18n dictionary and the
// docs prose are all plain arrays/strings inside .tsx modules; this repo has no
// DOM/JSX test runner, and the Go side of the withdrawal (the renderer arm, the
// formgen allowlist) is pinned by Go tests beside the code. What is left and
// unguarded by any compiler is the COPY — the palette item, its label/hint pair
// and the two marketing sentences that named coupons. A re-added palette entry
// or a restored sentence is exactly how this defect would come back, so the
// guard reads those files.
//
// WHAT IT DOES NOT COVER (stated honestly). It cannot tell that a palette entry
// renders correctly, and it says nothing about the dashboard's rendered HTML —
// only that the literal is absent from the source a reader would be served.
//
// BOTH DIRECTIONS. `findCouponSurfaces` is asserted against a fixture that DOES
// carry the markers, so a scan that silently matches nothing (a wrong path, a
// broken regex) fails instead of reporting a clean tree.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// Every web surface that used to name the block, and that must not name it now.
const SURFACES: { file: string; why: string }[] = [
  {
    file: "../app/builder/page.tsx",
    why: "the palette item, the BlockType union and the field/pricing type lists",
  },
  {
    file: "../components/i18nDict.ts",
    why: "blk.coupon / blk.couponHint, and the landing feature line",
  },
  {
    file: "../../public/llms-full.txt",
    why: "the agent-facing capability list",
  },
  {
    file: "../app/docs/page.tsx",
    why: "the /docs index entry that described the builder",
  },
];

// findCouponSurfaces returns the offending lines of a source text. Matching is
// on `coupon` OR the Malay `kupon`, because the shipped copy named the block in
// both languages and a fix that only removed the English word would leave the
// BM landing line promising it.
function findCouponSurfaces(src: string): string[] {
  return src
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => /coupon|kupon/i.test(line));
}

test("the scan finds the markers in a fixture that carries them (both directions)", () => {
  const fixture = [
    'const PALETTE = [{ type: "coupon", icon: "🏷" }];',
    '"blk.couponHint": { en: "Discount code", ms: "Kod diskaun" },',
    'body: "fields, products, images, coupons — and publish."',
    '"Borang siap 5 minit — tarik produk, medan, imej, kupon. Tanpa kod."',
  ].join("\n");

  assert.equal(findCouponSurfaces(fixture).length, 4, "the scan matched nothing in a fixture full of coupons");

  // …and it is not a blanket matcher: an ordinary source has no findings.
  assert.deepEqual(findCouponSurfaces('const PALETTE = [{ type: "total", icon: "Σ" }];'), []);
});

test("no web surface offers a coupon block any more", () => {
  for (const { file, why } of SURFACES) {
    const src = read(file);
    assert.ok(src.length > 500, `${file} read back suspiciously short (${src.length} chars) — check the path`);
    assert.deepEqual(
      findCouponSurfaces(src),
      [],
      `${file} still names a coupon (${why}). The block applies no discount: withdraw the surface, do not ` +
        `re-add the promise.`,
    );
  }
});

test("the pricing palette is otherwise intact", () => {
  // Without this, deleting the whole Pricing group would satisfy the check
  // above. The block that was withdrawn is the coupon one; the rest of the
  // group — and the docs sentence that lists what the builder does — stay.
  const builder = read("../app/builder/page.tsx");
  for (const t of ["product", "quantity", "option", "shipping", "total"]) {
    assert.ok(
      builder.includes(`{ type: "${t}"`),
      `the builder palette lost its ${t} block — the withdrawal must be scoped to coupon`,
    );
  }
  assert.ok(
    read("../app/docs/page.tsx").includes("drag-and-drop builder"),
    "the /docs entry stopped describing the builder (docsFacts pins 'drag-and-drop builder')",
  );
});
