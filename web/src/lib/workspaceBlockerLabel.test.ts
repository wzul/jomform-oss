// Guard for the Settings -> Workspace Danger zone's census labels (card
// t_088521a4).
//
// THE DEFECT THIS EXISTS FOR. `GET /v1/workspaces/{id}/deletion-check` answers
// each blocker as `{kind, count, label}`, where `label` is hardcoded MALAY in the
// API (`blockerLabels` in api/internal/dbstore/workspace.go). `WorkspaceTab.tsx`
// rendered `{b.label}` verbatim while the sentence ABOVE the list went through
// `t("ws.notEmpty")`, so an English dashboard read:
//
//     This workspace still holds the following. None of it is destroyed by
//     moving the workspace to the Trash:
//       borang (termasuk yang dalam tong sampah): 22
//       produk: 12
//       jualan: 38
//       domain tersuai: 2
//       webhook: 1
//       kunci API: 3
//
// Measured on prod 2026-10-16 in a real signed-in session with locale `en`. No
// gate saw it: the strings compile, the page is 200, and the API is answering
// exactly what it was written to answer.
//
// THE FIX IS ONE LIST. The visible name now comes from the dictionary, keyed on
// the stable `kind` code (`ws.blocker.<kind>` — see ../lib/workspaceBlockerLabel.ts
// for why the key is DERIVED rather than mapped through a hand-written table).
// The API's `label` is still sent and still Malay; it survives only as the
// fallback for a kind this build does not know.
//
// WHAT IS PINNED HERE, and why each assertion can fail
//
//   1. THE PREMISE: the Go map parses, it names the categories we think it names,
//      and the component is the file this guard claims to read. A guard that
//      silently parses nothing passes against nothing.
//   2. FORWARD: every kind the server can emit has a `ws.blocker.<kind>` entry
//      with all five locales set. A category added on the server without a
//      dictionary entry renders Malay on an English page again — that is the
//      defect, so it has to fail here.
//   3. REVERSE: every `ws.blocker.*` entry names a kind the server can still emit.
//      A renamed kind otherwise leaves its old translation behind, and the dead
//      key looks exactly like a live one.
//   4. THE VISIBLE en LABEL IS NOT THE SERVER'S LABEL. Assertion 2 alone would
//      still pass if the fix had copied the Malay string into the en entry.
//   5. THE COMPONENT ROUTES THROUGH THE DICTIONARY: the row no longer renders
//      `{b.label}` raw, and the fallback branch is the ONLY place `b.label` is
//      read — a fallback that is not last is a shadowed dictionary.
//   6. THE BITE PROOFS (last three tests) run assertions 2/3 against MUTATED
//      inputs built in memory — a kind added to the server map with no entry, an
//      entry deleted, an entry that names no kind — and require each to go RED
//      naming what is wrong. Nothing on disk is written. Without this the guard's
//      failure mode would never be observed inside the suite that runs it, which
//      is exactly how a green suite can pin nothing at all.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * TRANSLATION QUALITY — whether the Tamil actually says "custom domains". A
//     fluent review is the only thing that can judge that.
//   * The API's own `label` strings. They are still Malay on every locale; this
//     card makes the DASHBOARD stop depending on them, not the API multilingual.
//     The MCP census tool reports the same `label` to agent clients, which is a
//     separate (non-user-facing) question.
//   * Rendered output. The label path is exercised in the browser on the card's
//     PR evidence; this file is the standing guard, not the measurement.
//
// Run with:  npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { BLOCKER_LABEL_PREFIX, blockerLabelKey, blockerLabelKeys } from "./workspaceBlockerLabel.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const GO_WORKSPACE = read("../../../api/internal/dbstore/workspace.go");
const WORKSPACE_TAB = read("../components/WorkspaceTab.tsx");

type ServerKind = { kind: string; label: string };
/** The shape both checks need: key -> locale -> string. */
type DictShape = Record<string, Record<string, unknown>>;

/** The kinds `blockerLabels` declares, read out of the Go source. */
function parseServerKinds(src: string): ServerKind[] {
  const block = src.match(/var blockerLabels = map\[string\]string\{([\s\S]*?)\n\}/);
  assert.ok(
    block,
    "could not find `var blockerLabels = map[string]string{…}` in api/internal/dbstore/workspace.go — " +
      "the map this guard reads was renamed or moved; re-anchor the guard rather than deleting it",
  );
  const out: ServerKind[] = [];
  const entry = /"([a-z_]+)"\s*:\s*"((?:[^"\\]|\\.)*)"/g;
  for (let m = entry.exec(block[1]); m !== null; m = entry.exec(block[1])) {
    out.push({ kind: m[1], label: m[2] });
  }
  return out;
}

/** Kinds with no complete `ws.blocker.<kind>` entry — the FORWARD direction. */
function unreachedKinds(kinds: ServerKind[], dict: DictShape): string[] {
  const bad: string[] = [];
  for (const { kind } of kinds) {
    const key = `${BLOCKER_LABEL_PREFIX}${kind}`;
    const entry = dict[key];
    if (!entry) {
      bad.push(`${kind}: no dictionary entry "${key}", so an English page would fall back to the API's Malay label`);
      continue;
    }
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") {
        bad.push(`${kind}: "${key}".${lang} is missing or blank`);
      }
    }
  }
  return bad;
}

/** `ws.blocker.*` entries naming no server kind — the REVERSE direction. */
function orphanKeys(kinds: ServerKind[], dict: DictShape): string[] {
  const live = new Set(kinds.map((k) => `${BLOCKER_LABEL_PREFIX}${k.kind}`));
  return Object.keys(dict)
    .filter((k) => k.startsWith(BLOCKER_LABEL_PREFIX) && !live.has(k))
    .map((k) => `${k}: names a kind the server cannot emit`);
}

const SERVER_KINDS = parseServerKinds(GO_WORKSPACE);

// 1. The premise. Without this the checks below could pass against an empty parse.
test("the guard is reading the real server map and the real component", () => {
  assert.ok(SERVER_KINDS.length >= 7, `parsed only ${SERVER_KINDS.length} blocker kinds out of workspace.go`);
  assert.deepEqual(
    SERVER_KINDS.map((k) => k.kind),
    ["forms", "products", "sales", "subscriptions", "domains", "webhooks", "api_keys"],
    "the server's blocker kinds changed — if that is deliberate, add the matching ws.blocker.* entries " +
      "in all five locales and update this expectation",
  );
  assert.ok(WORKSPACE_TAB.length > 5000, "read a suspiciously short WorkspaceTab.tsx — the path is wrong");
  assert.match(WORKSPACE_TAB, /import \{ blockerLabelKey \} from "\.\.\/lib\/workspaceBlockerLabel";/);
});

// 2. Forward.
test("every blocker kind the server can emit has a five-locale dictionary entry", () => {
  assert.deepEqual(unreachedKinds(SERVER_KINDS, DICT as unknown as DictShape), []);
});

// 3. Reverse.
test("every ws.blocker.* entry names a kind the server can still emit", () => {
  assert.deepEqual(orphanKeys(SERVER_KINDS, DICT as unknown as DictShape), []);
  // A partial lookup (no entries at all) would satisfy the reverse check trivially,
  // so require the family to be the size the server says it is.
  assert.equal(blockerLabelKeys().length, SERVER_KINDS.length);
});

// 4. The visible string is the English one, not the API's Malay label.
test("the en label a user sees is not the API's Malay label", () => {
  for (const { kind, label } of SERVER_KINDS) {
    const key = `${BLOCKER_LABEL_PREFIX}${kind}` as keyof typeof DICT;
    const en = DICT[key].en as string;
    assert.notEqual(
      en,
      label,
      `"${key}".en is byte-identical to the server's label ("${label}") — the dictionary copied the ` +
        "hardcoded Malay string instead of naming the category in English",
    );
  }
});

// 5. The component routes through the dictionary, and the raw label is only the
//    last-resort branch.
test("WorkspaceTab renders the dictionary name for a blocker, not {b.label}", () => {
  assert.doesNotMatch(
    WORKSPACE_TAB,
    /\{b\.label\}/,
    "the census row still renders the API's hardcoded Malay label verbatim (the defect this file pins)",
  );
  assert.match(
    WORKSPACE_TAB,
    /const labelKey = blockerLabelKey\(b\.kind\);/,
    "the row no longer derives its dictionary key from the blocker kind",
  );
  assert.match(
    WORKSPACE_TAB,
    /\{labelKey \? t\(labelKey\) : b\.label\}/,
    "the row must render t(labelKey) and fall back to the server label ONLY when the dictionary has no entry",
  );
  const reads = WORKSPACE_TAB.match(/b\.label/g) ?? [];
  assert.equal(
    reads.length,
    1,
    `b.label is read ${reads.length} times — it belongs in exactly ONE place, the fallback branch`,
  );
});

// 6a. BITE: a kind added to the server map with no dictionary entry.
test("BITE: a new server kind with no dictionary entry is reported", () => {
  const grown = [...SERVER_KINDS, { kind: "archived_forms", label: "borang arkib" }];
  assert.deepEqual(unreachedKinds(SERVER_KINDS, DICT as unknown as DictShape), [], "control: the real pair is clean");
  const bad = unreachedKinds(grown, DICT as unknown as DictShape);
  assert.equal(bad.length, 1, `expected exactly one reported kind, got ${JSON.stringify(bad)}`);
  assert.match(bad[0], /^archived_forms: no dictionary entry "ws\.blocker\.archived_forms"/);
});

// 6b. BITE: an entry that loses a locale, or disappears entirely.
test("BITE: an entry with a blank locale and an entry that vanished are reported", () => {
  const clone = JSON.parse(JSON.stringify(DICT)) as DictShape;
  clone["ws.blocker.sales"] = { ...clone["ws.blocker.sales"], ta: "   " };
  delete clone["ws.blocker.domains"];
  const bad = unreachedKinds(SERVER_KINDS, clone);
  assert.deepEqual(bad, [
    'sales: "ws.blocker.sales".ta is missing or blank',
    'domains: no dictionary entry "ws.blocker.domains", so an English page would fall back to the API\'s Malay label',
  ]);
});

// 6c. BITE: a dictionary entry that names no server kind (the rename direction).
test("BITE: an entry for a kind the server dropped is reported", () => {
  const clone = JSON.parse(JSON.stringify(DICT)) as DictShape;
  clone["ws.blocker.archived_forms"] = { en: "archived forms", ms: "x", zh: "x", ta: "x", ar: "x" };
  assert.deepEqual(orphanKeys(SERVER_KINDS, clone), [
    "ws.blocker.archived_forms: names a kind the server cannot emit",
  ]);
});

// 7. The lookup itself: unknown kinds resolve to null (never to a raw key the page
//    would print), and inherited object members are not keys.
test("blockerLabelKey never hands t() a key the dictionary lacks", () => {
  for (const { kind } of SERVER_KINDS) {
    const key = blockerLabelKey(kind);
    assert.ok(key, `${kind} must resolve to a dictionary key`);
    assert.ok(Object.prototype.hasOwnProperty.call(DICT, key), `${key} is not in DICT`);
    assert.equal((DICT[key].en as string).trim().length > 0, true, `${key}.en is blank`);
  }
  for (const bogus of ["archived_forms", "toString", "constructor", "__proto__", "", " "]) {
    assert.equal(blockerLabelKey(bogus), null, `"${bogus}" must not resolve to a dictionary key`);
  }
});
