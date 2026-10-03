// Guard for the workspace-Trash copy's claim about WHERE the permanent delete is
// reached (card t_2a686ada).
//
// WHY THIS FILE EXISTS
//
// Three merchant-facing strings told the merchant that destroying a workspace for
// good happens in the **Trash view**:
//
//   ws.dangerHelp     "...Deleting it for good is separate, lives in the Trash view..."
//   ws.notEmptyHint   "Destroying it for good happens in the Trash view..."
//   ws.confirmBody    "...Deleting it for good is a separate, human-only step in the Trash view."
//
// There is no such control. `WorkspaceTab.tsx` renders ONE button per Trash row
// (`ws.trashRestore` -> POST /api/workspaces/{id}/restore) and its own header says
// "PERMANENT DELETE IS NOT OFFERED HERE"; nothing in `web/src` issues a DELETE to the
// workspace purge route at all (asserted over the tree by the sibling guard in
// api/internal/mcpsrv/purge_reachability_claim_test.go, which owns that fact).
//
// The consequence is the worst kind of copy defect: the merchant bins a workspace,
// goes to the Trash view to finish the job, finds no control, and concludes the
// product is broken — with no error to report.
//
// WHAT IS PINNED HERE, and why each assertion can fail
//
//   1. The phrase that sent them there (the Trash view named beside a permanent
//      verb) must be GONE from every locale. The defect shipped in all five, so a
//      fix that only touched `en` would leave four locales lying.
//   2. Each corrected string must NAME where the step really is (the REST API), per
//      locale. Deleting the promise without naming the truth would stop the copy from
//      lying but still leave the merchant with nowhere to go.
//   3. The Trash promise itself is gone from `ws.dangerHelp` while the word "Trash"
//      survives — the sentence still has to describe the bin.
//   4. THE BITE PROOF (last test) puts the PRE-FIX wording back into the dictionary
//      IN MEMORY (nothing on disk is written) and requires the scan to go RED naming
//      the key. A guard whose failure mode is never observed inside the suite that
//      runs it rots with every gate green — which is how this defect shipped.

import { test } from "node:test";
import assert from "node:assert/strict";

import { DICT } from "../components/i18nDict.ts";

const PERMANENT_VERB_IN_TRASH_VIEW = [
  // en
  "lives in the Trash view",
  "happens in the Trash view",
  "step in the Trash view",
  // ms
  "di paparan Tong Sampah",
  // zh
  "位于回收站视图中",
  "在回收站视图中进行",
  "回收站视图中单独的",
  // ta
  "குப்பைத் தொட்டி பார்வையில்",
  // ar
  "في عرض سلة المهملات",
];

const TRASH_COPY_KEYS = ["ws.dangerHelp", "ws.notEmptyHint", "ws.confirmBody"];

type Dict = Record<string, Record<string, string>>;

// scanClaims returns the offending "key[locale]: needle" strings in a dictionary.
// Split out from the tests so the bite proof runs the IDENTICAL judgement against a
// mutated copy — a proof that exercised a paraphrase of the scan would prove nothing
// about the scan.
function scanClaims(d: Dict): string[] {
  const bad: string[] = [];
  for (const key of TRASH_COPY_KEYS) {
    const entry = d[key];
    if (!entry) {
      bad.push(`${key}: MISSING from the dictionary`);
      continue;
    }
    for (const [loc, text] of Object.entries(entry)) {
      if (typeof text !== "string") continue;
      for (const needle of PERMANENT_VERB_IN_TRASH_VIEW) {
        if (text.includes(needle)) bad.push(`${key}[${loc}]: ${needle}`);
      }
    }
  }
  return bad;
}

test("the Trash copy never sends the merchant to the Trash view for a permanent delete", () => {
  const bad = scanClaims(DICT as unknown as Dict);
  assert.deepEqual(
    bad,
    [],
    `the Trash copy tells the merchant the permanent delete is done in the Trash view, but that ` +
      `view offers Restore only (WorkspaceTab.tsx: "PERMANENT DELETE IS NOT OFFERED HERE"), so ` +
      `the merchant goes there and finds nothing:\n  ${bad.join("\n  ")}`,
  );
});

test("each corrected string names where the permanent step really is, in every locale", () => {
  // Where the step really is: the REST API (or an MCP client acting for the owner).
  // "REST API" is not translated in any locale, so one marker covers all five.
  const missing: string[] = [];
  for (const key of TRASH_COPY_KEYS) {
    const entry = (DICT as unknown as Dict)[key] ?? {};
    for (const loc of ["en", "ms", "zh", "ta", "ar"]) {
      const text = entry[loc];
      if (typeof text !== "string" || text === "") {
        missing.push(`${key}[${loc}]: locale absent`);
        continue;
      }
      if (!text.includes("REST API")) {
        missing.push(`${key}[${loc}]: does not name the REST API`);
      }
    }
  }
  assert.deepEqual(
    missing,
    [],
    `a corrected string must say where the permanent step IS reached, not merely stop claiming ` +
      `the Trash view — otherwise the merchant is left with nowhere to go:\n  ${missing.join("\n  ")}`,
  );
});

test("the Trash promise is gone while the sentence still describes the bin", () => {
  const en = ((DICT as unknown as Dict)["ws.dangerHelp"] ?? {}).en ?? "";
  assert.ok(!en.includes("lives in the Trash view"), "the promise is still there");
  assert.ok(
    en.includes("Trash"),
    "the sentence no longer mentions the Trash at all — removing the true half of the copy is " +
      "not the fix, redirecting the permanent step is",
  );
});

test("BITES: the pre-fix wording put back into the dictionary is caught, by key", () => {
  // Put the pre-fix string back IN MEMORY. The mutation must land on the real
  // dictionary, or this proof asserts nothing.
  const mutated = JSON.parse(JSON.stringify(DICT)) as Dict;
  mutated["ws.dangerHelp"].en =
    "Moving a workspace to the Trash destroys nothing. Deleting it for good is " +
    "separate, lives in the Trash view, and only the owner (a person) can do it.";

  assert.ok(
    mutated["ws.dangerHelp"].en.includes("lives in the Trash view"),
    "the pre-fix wording was not substituted, so this proof is vacuous",
  );
  const bad = scanClaims(mutated);
  assert.ok(
    bad.length > 0,
    "with the pre-fix wording restored, the scan found nothing — so the live check above cannot " +
      "fail on a regression and this guard is decorative",
  );
  assert.ok(
    bad.join(" ").includes("ws.dangerHelp"),
    `the scan must name the offending key; it reported: ${bad.join(", ")}`,
  );
});
