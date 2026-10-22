// A builder LOAD must not rename a stored block (card t_cbeafc7c).
//
// THE DEFECT, measured on prod v1.21.279 (4b8fa0dc) by card t_bf407d24: a
// response form stored `blocks: [{id:"b1"},{id:"b2", ...required:"Nama"}]` and
// `registered_fields: [{field_id:"b2", required:true}]`. Pressing Publish in
// /builder?form=<id> wrote the config back with NEW block ids (`b1`/`b2` became
// `265kivw`/`db8x0qh`) because hydrateBlock assigned `id: uid()` on every load,
// while the carry-over kept `registered_fields` verbatim. formfields.Contract
// prefers that contract over the blocks, so the public page's own submit was
// refused for every visitor:
//
//   400 {"code":"request.badRequest","error":"medan wajib tidak diisi: Nama"}
//
// Removing registered_fields from the same row and resubmitting the same page
// answered 201 — the stale contract was the whole cause.
//
// The behavioural proof that the real page keeps the ids through a Publish is
// web/e2e/builder-publish-keeps-block-ids.spec.ts (it drives the built builder
// and reads the PATCH body it produced). The Go half — that the ids the page
// emits are the ones the stored contract names, and that such a submit is
// accepted — is api/internal/httpapi/builder_publish_block_ids_test.go.
//
// Run with the repo's Node test runner:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { hydrateBlockIds } from "./blockHydrate.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const builderPage = read("../app/builder/page.tsx");

/** A deterministic stand-in for uid(); the sequence is what the call under test consumes. */
const generator = (...ids: string[]) => {
  let i = 0;
  return () => ids[Math.min(i++, ids.length - 1)];
};

test("a stored block id survives a load — the id IS the field name the page emits", () => {
  // The card's fixture, verbatim. Before the fix this came back as two random
  // 7-char ids and the stored contract (field_id "b2") stopped referring to
  // anything on the page.
  const ids = hydrateBlockIds(["b1", "b2"], generator("SHOULD-NOT-BE-USED"));
  assert.deepEqual(ids, ["b1", "b2"], "a stored block id was regenerated on load");
});

test("a block with no usable id still gets one", () => {
  // A legacy/hand-written config can carry a block with no id, a blank one or a
  // non-string. The builder cannot render or submit a control without one, so
  // these are minted — and minted distinctly, or two controls would share a name.
  const ids = hydrateBlockIds([undefined, "", "   ", 42, null], generator("g1", "g2", "n1", "n2", "n3"));
  assert.deepEqual(ids, ["g1", "g2", "n1", "n2", "n3"]);
  assert.equal(new Set(ids).size, ids.length, "two blocks were handed the same generated id");
});

test("a DUPLICATE stored id is broken apart, first occurrence keeps it", () => {
  // Two controls with one name collapse into a single submitted value and the
  // canvas' React keys collide. The first occurrence keeps the stored id (the
  // one a stored contract can still name); the later one is minted, and the
  // minted id must not reuse a candidate that is already taken.
  const ids = hydrateBlockIds(["b1", "b1", "b2"], generator("b1", "fresh"));
  assert.deepEqual(ids, ["b1", "fresh", "b2"], "a duplicate id was not repaired, or the repair reused a taken id");
});

test("a generator that never yields a free id terminates instead of duplicating", () => {
  // The collision loop must not hang, and must not fall back to the id it kept
  // rejecting: a broken/looping `fresh` still has to yield a unique value.
  const ids = hydrateBlockIds(["b1", "b1"], generator("b1"));
  assert.equal(ids.length, 2);
  assert.notEqual(ids[1], "b1", "the repair handed back the id it was repairing");
  assert.equal(new Set(ids).size, 2);
});

// ── the page must actually call it (wiring, not just a correct helper) ─────
//
// A correct helper nothing calls is dead code, so the builder's hydration is
// pinned to it. The BEHAVIOURAL proof of this wiring is the e2e spec above;
// this is the cheap anchor that names the exact pre-fix shape.

test("the builder hydrates stored block ids through hydrateBlockIds", () => {
  assert.match(
    builderPage,
    /hydrateBlockIds\(/,
    "the builder load does not derive block ids from the stored config — every block is renamed again",
  );
  // The pre-fix shape, which shipped the bug: hydrateBlock minting the id itself.
  assert.doesNotMatch(
    builderPage,
    /return \{\s*id: uid\(\),\s*type: \(b\.type as BlockType\)/,
    "hydrateBlock assigns `id: uid()` again, so a load renames every stored block",
  );
});
