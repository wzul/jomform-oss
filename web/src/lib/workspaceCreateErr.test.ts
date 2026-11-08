// Issue #6177: the workspace-create failure path must be decided by the machine
// CODE, with the HTTP status kept only as a fallback.
//
// THE BUG THIS PINS. The create path branched on `res.status`:
//
//     if (res.status === 409) msg = t("workspace.slugTaken");
//     else if (res.status === 400) msg = t("workspace.slugInvalid");
//
// That is the shape the card calls out: the server already answers
// `{"error":"slug already in use","code":"workspace.slugTaken"}`, so a status
// branch is both redundant AND fragile — reword a message, or move the refusal
// to a different status, and the page silently degrades to "Failed to create
// workspace." while the API still names the exact reason.
//
// WHERE THIS NOW LIVES. On current main the status → message mapping moved out
// of WorkspaceSwitcher into the SHARED helper (`lib/workspace.createWorkspace`)
// so the switcher and the first-run ask cannot drift. The #6177 precedence was
// re-applied THERE on the rebase, so that is where it is asserted: a
// source-order check inside the component would now be reading a call site that
// no longer makes the decision at all. The cases below therefore EXECUTE the
// real helper against a stubbed response instead of restating its rules.
//
// WHAT IS ASSERTED, AND WHY IT BITES
//   1. A 409 body carrying workspace.slugTaken resolves to the SLUG-TAKEN
//      sentence, not the generic create-failure one, in every locale.
//   2. A 400 body carrying workspace.slugInvalid resolves to SLUG-INVALID.
//   3. When the CODE and the STATUS DISAGREE, the code wins. This is the only
//      case that proves the PRECEDENCE rather than restating that both branches
//      exist: a status-first implementation answers the wrong sentence here.
//   4. A body with NO code (an older build, a proxy) still resolves from the
//      status — the fallback must not have been deleted with the branch — and so
//      does a code this build has never heard of.
//   5. The server's `error` prose is never what gets rendered, in any of them.
//   6. In the helper's source the code lookup appears BEFORE the status branch,
//      so a later reorder is visible in review and not only behaviourally.
//
// READ SHAPE. Cases 1 and 3 also pin HOW the helper reads the body. Both
// refusals this path answers are decided from one `res.json()` parse (see
// lib/workspace.ts), so the stub deliberately offers `json` and NO `clone`: a
// body read that needs a cloned response resolves these cases through the
// status fallback instead of the machine code, which is the #6177 defect.
//
// Run with the repo's Node test runner: npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { createWorkspace } from "./workspace.ts";
import { DICT } from "../components/i18nDict.ts";

const SWITCHER = fileURLToPath(new URL("../components/WorkspaceSwitcher.tsx", import.meta.url));
const WORKSPACE_LIB = fileURLToPath(new URL("./workspace.ts", import.meta.url));

/** A translator bound to one locale, reading the same DICT the UI renders from. */
function tIn(lang: string) {
  return (k: string) => {
    const entry = (DICT as Record<string, Record<string, unknown>>)[k];
    assert.ok(entry, `missing dictionary key ${k}`);
    const v = entry[lang];
    return typeof v === "string" ? v : (entry.en as string);
  };
}

/** The exact refusal envelopes the live API answers (verified against a running
 *  build: POST /v1/workspaces with a taken slug → 409 + this body). */
const TAKEN = { error: "slug already in use", code: "workspace.slugTaken" };
const INVALID = { error: "slug 3-40 characters", code: "workspace.slugInvalid" };

/** A fake fetch answering one status with one body, recording the request.
 *
 *  It answers `json()` but offers NO `clone()`, deliberately: the create path
 *  decides BOTH refusals (the reserved-name token and the machine error code)
 *  from one `res.json()` parse, so a read shaped for a cloned response is not
 *  something this path can use. Making the stub narrower than a real Response
 *  on purpose is what turns that into a failing case instead of a silent fall
 *  through to the status branch. */
function stubFetch(status: number, body: unknown) {
  const calls: Array<{ url: string; method: string; body: { name: string; slug: string } }> = [];
  const fn = (async (url: string, init?: { method?: string; body?: string }) => {
    calls.push({ url, method: init?.method || "GET", body: JSON.parse(init?.body || "{}") });
    const payload = () => Promise.resolve(body);
    return {
      ok: status >= 200 && status < 300,
      status,
      json: payload,
    };
  }) as unknown as typeof fetch;
  return { fn, calls };
}

/** Run the REAL createWorkspace against a stubbed response. */
async function createAgainst(status: number, body: unknown) {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = stubFetch(status, body).fn;
    return await createWorkspace({ name: "My Shop", slug: "my-shop" });
  } finally {
    globalThis.fetch = original;
  }
}

test("a 409 duplicate slug renders the slug-taken sentence (from the CODE, not the status)", async () => {
  const r = await createAgainst(409, TAKEN);
  assert.equal(r.ok, false, "a 409 was reported as a success");
  const key = (r as { errorKey: string }).errorKey;
  assert.equal(key, "apiErr.workspaceSlugTaken");

  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    const t = tIn(lang);
    const rendered = t(key);
    assert.equal(rendered, t("workspace.slugTaken"), `lang=${lang}`);
    assert.notEqual(rendered, t("workspace.createFail"), `lang=${lang} degraded to the generic line`);
    assert.ok(!rendered.includes("slug already in use"), `lang=${lang} rendered the server prose`);
  }
});

test("a 400 bad slug renders the slug-invalid sentence (from the CODE, not the status)", async () => {
  const r = await createAgainst(400, INVALID);
  assert.equal(r.ok, false, "a 400 was reported as a success");
  const key = (r as { errorKey: string }).errorKey;
  assert.equal(key, "apiErr.workspaceSlugInvalid");

  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    const t = tIn(lang);
    const rendered = t(key);
    assert.equal(rendered, t("workspace.slugInvalid"), `lang=${lang}`);
    assert.notEqual(rendered, t("workspace.createFail"), `lang=${lang}`);
    assert.ok(!rendered.includes("slug 3-40 characters"), `lang=${lang} rendered the server prose`);
  }
});

test("the CODE wins when the code and the status DISAGREE (the precedence itself)", async () => {
  // A 409 whose body names the SLUG-INVALID refusal. A status-first
  // implementation answers workspace.slugTaken here, so this is the assertion
  // that actually distinguishes the two implementations.
  assert.deepEqual(
    await createAgainst(409, { error: "slug 3-40 characters", code: "workspace.slugInvalid" }),
    { ok: false, errorKey: "apiErr.workspaceSlugInvalid" },
    "a 409 carrying workspace.slugInvalid was answered from the STATUS — the code is not read first",
  );

  // The mirror image, so neither branch can be the accidental winner.
  assert.deepEqual(
    await createAgainst(400, TAKEN),
    { ok: false, errorKey: "apiErr.workspaceSlugTaken" },
    "a 400 carrying workspace.slugTaken was answered from the STATUS",
  );
});

test("a body with NO code still falls back to the status branch (the fallback survives)", async () => {
  // Simulates a response from an older build (or a proxy) that carries prose
  // only. The status branch is what answers — the property that must NOT have
  // been deleted along with the branch.
  assert.deepEqual(await createAgainst(409, { error: "slug already in use" }), {
    ok: false,
    errorKey: "workspace.slugTaken",
  });
  assert.deepEqual(await createAgainst(400, { error: "slug 3-40 characters" }), {
    ok: false,
    errorKey: "workspace.slugInvalid",
  });
  // A code this build does not know is that same case: the page's own sentence,
  // never the prose.
  assert.deepEqual(
    await createAgainst(409, { error: "slug already in use", code: "future.code.notInThisBuild" }),
    { ok: false, errorKey: "workspace.slugTaken" },
  );
  // No body at all (a proxy error page, an empty 500).
  assert.deepEqual(await createAgainst(500, undefined), {
    ok: false,
    errorKey: "workspace.createFail",
  });
  assert.deepEqual(await createAgainst(502, { error: "bad gateway" }), {
    ok: false,
    errorKey: "workspace.createFail",
  });

  // And the sentences the fallback renders are distinct, localized strings —
  // not the generic line.
  const t = tIn("en");
  assert.equal(t("workspace.slugTaken"), "That slug is already taken.");
  assert.equal(t("workspace.slugInvalid"), "Slug must be 3-40 chars, a-z, 0-9, dash.");
  assert.notEqual(t("workspace.slugTaken"), t("workspace.createFail"));
  assert.notEqual(t("workspace.slugInvalid"), t("workspace.createFail"));
});

test("the helper reads the machine code BEFORE the status branch (source order)", () => {
  const src = readFileSync(WORKSPACE_LIB, "utf8");
  const body = src.slice(src.indexOf("export async function createWorkspace"));
  assert.ok(body.length > 0, "could not locate createWorkspace in lib/workspace.ts");

  const codeAt = body.indexOf("apiErrorKey(");
  const statusAt = body.indexOf("res.status === 409");
  assert.ok(codeAt !== -1, "the code lookup is gone — the path is status-only again");
  assert.ok(statusAt !== -1, "the status fallback was deleted; a codeless response would lose it");
  assert.ok(
    codeAt < statusAt,
    "the status branch is evaluated BEFORE the machine code, so a code the " +
      "server sends cannot win (issue #6177)",
  );
  // The raw prose must not be decoded anywhere in this path.
  assert.doesNotMatch(body, /\.error\b/);
});

test("both create call sites render the translated KEY, and neither re-posts the endpoint", () => {
  const switcher = readFileSync(SWITCHER, "utf8");
  assert.match(
    switcher,
    /const r = await createWorkspace\(\{ name, slug \}\)[\s\S]{0,200}?setError\(t\(r\.errorKey\)\)/,
    "the switcher no longer renders the dictionary key from the shared helper",
  );
  assert.ok(
    !switcher.includes('fetch("/api/workspaces"'),
    "the switcher POSTs /api/workspaces itself again — the #6177 precedence in " +
      "lib/workspace would no longer cover it",
  );
  assert.doesNotMatch(switcher, /\.error\b/, "the switcher reads the API prose");
});
