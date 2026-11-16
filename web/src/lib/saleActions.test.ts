// THE BUTTONS COME FROM THE SERVER (card t_52429d6f).
//
// These tests pin the rule that replaced `disabled={s.status !== "paid"}`:
// the page renders an action only when the API's `available_events` says it
// exists, and renders NOTHING when the field is absent. The absent case is the
// one the card calls out — a missing answer must not fall back to a guess.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { capturable, enabledSaleActions, saleActions, type AvailableEvent } from "./saleActions.ts";

const paid: AvailableEvent[] = [
  // Not a person's action: a gateway callback. No label_key, so no button.
  { event: "pay", enabled: true },
  { event: "capture", label_key: "bld.capture", enabled: false, reason: "not an authorization awaiting capture" },
  { event: "confirm", label_key: "sales.confirmSale", enabled: true },
  { event: "refund", label_key: "sales.refund", enabled: true },
  { event: "release", label_key: "bld.release", enabled: false, reason: "not an authorization awaiting capture" },
];

test("no available_events means no buttons — the page never guesses from status", () => {
  assert.deepEqual(saleActions(undefined), []);
  assert.deepEqual(saleActions(null), []);
  assert.deepEqual(saleActions([]), []);
  // A malformed body (not an array) is the same answer, not a crash.
  assert.deepEqual(saleActions({} as unknown as AvailableEvent[]), []);
});

test("buttons are built from the server list, including which ones are disabled", () => {
  const actions = saleActions(paid);
  assert.deepEqual(
    actions.map((a) => a.event),
    ["capture", "release", "confirm", "refund"],
  );
  const capture = actions.find((a) => a.event === "capture");
  assert.equal(capture?.enabled, false);
  assert.equal(capture?.reason, "not an authorization awaiting capture");
  assert.equal(actions.find((a) => a.event === "confirm")?.enabled, true);
});

test("an event without a label_key is not a button (gateway callbacks are not actions)", () => {
  assert.deepEqual(saleActions([{ event: "pay", enabled: true }, { event: "fail", enabled: true }]), []);
});

test("an event the page cannot dispatch is dropped, not rendered dead", () => {
  assert.deepEqual(saleActions([{ event: "teleport", label_key: "sales.teleport", enabled: true }]), []);
});

test("the row vocabulary excludes confirm; the drawer keeps it", () => {
  const row = saleActions(paid, ["capture", "release", "refund"]).map((a) => a.event);
  assert.deepEqual(row, ["capture", "release", "refund"]);
  const drawer = saleActions(paid).map((a) => a.event);
  assert.ok(drawer.includes("confirm"), "the drawer is where confirm lives");
});

test("enabledSaleActions keeps only what the server says will succeed", () => {
  const enabled = enabledSaleActions(saleActions(paid)).map((a) => a.event);
  assert.deepEqual(enabled, ["confirm", "refund"]);
});

test("a terminal state's empty list renders no buttons", () => {
  assert.deepEqual(saleActions([]), []);
  assert.deepEqual(enabledSaleActions([]), []);
});

// The page resolves the server's `label_key` at runtime (`t(a.labelKey as Key)`),
// so the i18n scanner cannot see which keys those are — that is why this file
// carries a dynamic-call-site budget in src/lib/i18nKeys.test.ts. This is the
// guard that pays for it: every LabelKey the Go lifecycle table can send must
// exist, non-empty, in all five locales. Read from the Go source rather than
// copied, so adding a button to the table without its words fails HERE instead
// of rendering the raw key to a merchant.
test("every label_key the Go lifecycle table can send exists in all five locales", () => {
  const go = readFileSync(
    fileURLToPath(new URL("../../../api/internal/lifecycle/sales.go", import.meta.url)),
    "utf8",
  );
  const keys = new Set([...go.matchAll(/LabelKey:\s*"([^"]+)"/g)].map((m) => m[1]));
  assert.ok(
    keys.size >= 4,
    `only ${keys.size} LabelKey(s) found in api/internal/lifecycle/sales.go — this guard would ` +
      `otherwise check almost nothing, which is how a raw key ships`,
  );
  for (const key of keys) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(entry, `${key} is a lifecycle button label the API sends but the dictionary lacks`);
    for (const lang of LANGS) {
      assert.ok(
        String(entry![lang] ?? "").trim().length > 0,
        `${key}.${lang} is empty — the sales row/drawer would show a blank button in that language`,
      );
    }
  }
});

// ── "Capture all" counts what the API will charge (t_ee368b62) ─────────────
//
// The page used to decide this itself: skip_capture && !captured_at && (status
// is pending/authorized || authorized === true). It therefore counted a
// `confirmed` sale whose authorization was still uncaptured — and a `paid` one
// with authorized=false — as NOTHING, so the button's count, its prompt total and
// the refs it sent all omitted a hold the per-sale endpoint charges. The
// authorization expired and the merchant was never charged. The rule now comes
// from available_events, which is the same guard the API applies.
test("capturable is the server's enabled capture action, not a status list", () => {
  const serverSays = (enabled: boolean): AvailableEvent[] => [
    { event: "pay", enabled: true },
    {
      event: "capture",
      label_key: "bld.capture",
      enabled,
      ...(enabled ? {} : { reason: "not an authorization awaiting capture" }),
    },
    { event: "confirm", label_key: "sales.confirmSale", enabled: true },
  ];

  // The reported case: a sale CONFIRMED before its authorization was captured.
  assert.equal(capturable(serverSays(true)), true);
  // A guard refusal (already captured, or not an authorization at all).
  assert.equal(capturable(serverSays(false)), false);
  // Fail-closed: no answer means "I do not know".
  assert.equal(capturable(undefined), false);
  assert.equal(capturable(null), false);
  assert.equal(capturable([]), false);
  // A capture with no label_key is not a person's action (a gateway callback),
  // so it cannot be what the button counts either.
  assert.equal(capturable([{ event: "capture", enabled: true }]), false);
});

test("the sales page derives its bulk-capture rows from the server (t_ee368b62)", () => {
  const page = readFileSync(
    fileURLToPath(new URL("../app/sales/page.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    page,
    /const isCapturePending = \(s: Sale\) => capturable\(s\.available_events\)/,
    "isCapturePending no longer reads the server's available_events",
  );
  assert.doesNotMatch(
    page,
    /s\.status === "pending" \|\| s\.status === "authorized"/,
    "the client-side status list is back: it drops a confirmed or paid hold from \"Capture all\"",
  );
});
