// Issue #589 regression test — the reported bug: the New-form template picker
// cards the same template twice.
//
// The picker merges two independently fetched sources:
//   - the system gallery   (GET /api/templates)       — published templates
//   - the merchant's own   (GET /api/rest/templates)  — is_template rows owned
//     by the caller's account (issue #457)
//
// A published template owned by the caller's account satisfies BOTH, so the same
// forms row came back from both endpoints and was drawn once per source: 17
// cards for 14 distinct templates on the live dashboard, with "Tuition Class
// Registration" (id 4283337b-…) shown twice carrying the SAME id.
//
// The fixture below is the real production shape: 14 gallery templates, three of
// which (tpl-fnb, tpl-booking, tpl-tuition) are also returned by the
// own-templates query because they live in a merchant account.
//
// Run with the repo's Node test runner (there is no JSX/component test setup):
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  mergePickerTemplates,
  splitPickerTemplates,
  type PickerTemplate,
} from "./templatePicker.ts";

const TUITION_ID = "4283337b-a0ec-464e-98d8-8dd301632de1";
const BOOKING_ID = "461bb028-d04d-4cb4-b4b6-65acbe39d51b";
const FNB_ID = "8142c8f6-3d99-40d7-a6c3-3e566aac4452";

/** The system gallery as production serves it (14 published templates). */
const GALLERY: PickerTemplate[] = [
  { id: FNB_ID, slug: "tpl-fnb", title: "F&B Order + Payment", category: "Food & Beverage", description: "Order from our menu." },
  { id: TUITION_ID, slug: "tpl-tuition", title: "Tuition Class Registration", category: "Education", description: "Register for tuition." },
  { id: BOOKING_ID, slug: "tpl-booking", title: "Service Booking Deposit", category: "Booking", description: "Book your slot." },
  { id: "tpl-ecommerce-id", slug: "tpl-ecommerce", title: "eCommerce Checkout" },
  { id: "tpl-contact-simple-id", slug: "tpl-contact-simple", title: "Simple Contact" },
  { id: "tpl-contact-advanced-id", slug: "tpl-contact-advanced", title: "Advanced Contact" },
  { id: "tpl-newsletter-id", slug: "tpl-newsletter", title: "Newsletter Signup" },
  { id: "tpl-survey-id", slug: "tpl-survey", title: "Survey" },
  { id: "tpl-quote-id", slug: "tpl-quote", title: "Request a Quote" },
  { id: "tpl-employment-id", slug: "tpl-employment", title: "Employment Application" },
  { id: "tpl-gift-id", slug: "tpl-gift", title: "Gift Certificate" },
  { id: "tpl-contest-id", slug: "tpl-contest", title: "Contest Entry" },
  { id: "tpl-event-id", slug: "tpl-event", title: "Event Registration" },
  { id: "tpl-donation-id", slug: "tpl-donation", title: "Donation" },
];

/** The own-templates response for that merchant: the same three row ids. */
const OWN_OVERLAPPING: PickerTemplate[] = [
  { id: BOOKING_ID, slug: "tpl-booking", title: "Service Booking Deposit" },
  { id: TUITION_ID, slug: "tpl-tuition", title: "Tuition Class Registration" },
  { id: FNB_ID, slug: "tpl-fnb", title: "F&B Order + Payment" },
];

function ids(list: PickerTemplate[]): string[] {
  return list.map((t) => t.id);
}

function duplicates(list: string[]): string[] {
  return [...new Set(list.filter((v, i) => list.indexOf(v) !== i))];
}

test("#589: a template returned by BOTH sources is carded exactly once", () => {
  const merged = mergePickerTemplates({ own: OWN_OVERLAPPING, gallery: GALLERY });

  // Before the fix this was 14 + 3 = 17 cards for 14 distinct templates.
  assert.equal(merged.length, 14, "one card per distinct template row");
  assert.deepEqual(duplicates(ids(merged)), [], "no template row may be carded twice");

  const tuition = merged.filter((t) => t.id === TUITION_ID);
  assert.equal(tuition.length, 1, "Tuition Class Registration must appear exactly once");
  assert.equal(tuition[0].title, "Tuition Class Registration");

  // Every other template survives untouched.
  assert.deepEqual(
    merged.map((t) => t.slug).sort(),
    GALLERY.map((t) => t.slug).sort(),
    "no template may be added, removed or renamed",
  );
});

test("#589: the merged card keeps the gallery fields (it is the richer card)", () => {
  const merged = mergePickerTemplates({ own: OWN_OVERLAPPING, gallery: GALLERY });
  const tuition = merged.find((t) => t.id === TUITION_ID);
  assert.ok(tuition);
  // The own-templates response carries no category/description; the gallery one
  // does, so the surviving card must be the gallery entry.
  assert.equal(tuition?.category, "Education");
  assert.equal(tuition?.description, "Register for tuition.");
});

test("#589: each row is rendered by exactly one section, so neither draws it twice", () => {
  const merged = mergePickerTemplates({ own: OWN_OVERLAPPING, gallery: GALLERY });
  const { ownCards, galleryCards } = splitPickerTemplates(merged, GALLERY);

  // The three overlapping rows belong to the gallery section only.
  assert.equal(ownCards.length, 0, "gallery rows must not also be listed as own templates");
  assert.equal(galleryCards.length, 14);
  assert.deepEqual(duplicates(ids([...ownCards, ...galleryCards])), []);
  assert.deepEqual(
    [...ownCards, ...galleryCards].map((t) => t.id).sort(),
    ids(merged).sort(),
    "every assembled row is rendered by exactly one section",
  );
});

test("#589: a genuine merchant template still appears once, in the own section", () => {
  const mine: PickerTemplate = { id: "own-1", slug: "tpl-my-invoice", title: "My Invoice Form" };
  const { ownCards, galleryCards } = splitPickerTemplates(
    mergePickerTemplates({ own: [...OWN_OVERLAPPING, mine], gallery: GALLERY }),
    GALLERY,
  );

  assert.deepEqual(ids(ownCards), ["own-1"], "an own-only template is listed once, as own");
  assert.equal(galleryCards.length, 14, "the gallery is unchanged by an own-only template");
  assert.equal(mergePickerTemplates({ own: [mine], gallery: [] }).length, 1);
});

test("#589: the gallery survives an own-templates fetch that fails (empty list)", () => {
  const merged = mergePickerTemplates({ own: [], gallery: GALLERY });
  assert.equal(merged.length, 14);
  assert.deepEqual(duplicates(ids(merged)), []);
});

test("#589: dedupe is on row id, so two distinct templates sharing a title both survive", () => {
  const a: PickerTemplate = { id: "id-a", slug: "tpl-a", title: "Booking" };
  const b: PickerTemplate = { id: "id-b", slug: "tpl-b", title: "Booking" };
  const merged = mergePickerTemplates({ own: [b], gallery: [a] });

  assert.equal(merged.length, 2, "deduping by title would wrongly hide a real template");
  assert.deepEqual(ids(merged), ["id-a", "id-b"]);
});

test("#589: a row with no id is dropped rather than rendered twice", () => {
  const broken = { slug: "tpl-broken", title: "Broken" } as unknown as PickerTemplate;
  const merged = mergePickerTemplates({ own: [broken, broken], gallery: [] });
  assert.equal(merged.length, 0);
});

test("#589: the whole gallery is never duplicated by a full own-templates echo", () => {
  // Worst case: the own-templates endpoint returns every gallery row too.
  const merged = mergePickerTemplates({ own: GALLERY, gallery: GALLERY });
  assert.equal(merged.length, GALLERY.length);
  assert.deepEqual(duplicates(ids(merged)), []);
});
