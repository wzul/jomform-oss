// Regression guard for issue #6177 — the dashboard must NEVER render the API's
// prose, and every API error code the server can name must have a translation.
//
// THE BUG. The API answers with Malay prose (`{"error":"workspace tidak
// dijumpai"}`) and the dashboard rendered it verbatim:
//
//     setErr(d.error || t("tpl.useFail"));
//
// `t(...)` is localized; `d.error` — the branch that fires on a real server
// failure — was the Malay sentence. A user on `lang=en` read Malay. The fix is
// the machine `code` (api/internal/httpapi/errcode.go) plus this dictionary.
//
// WHAT THIS GUARD PINS, AND WHY EACH ONE IS THE THING THAT BREAKS
//   1. NO component renders `.error` off an API body. This is a source walk over
//      every .tsx/.ts under src/ — the count is asserted against a floor, so a
//      walk that silently matches nothing cannot pass vacuously.
//   2. The reported case specifically: a body carrying the server's Malay prose
//      is turned into an ENGLISH sentence on `lang=en` — asserted for all five
//      locales, so a missing translation shows as the English fallback rather
//      than as Malay.
//   3. An UNKNOWN code falls back to the caller's localized message, never to the
//      prose. This is the property that makes the mechanism safe against a code
//      the server adds later.
//   4. Every code the SERVER names is translated here. The code list is read out
//      of the Go source (not restated), so adding a code on the server and
//      forgetting the dictionary fails the build.
//   5. The dictionary entries are real (all five locales, non-empty).
//
// Run with the repo's Node test runner:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { API_ERROR_KEYS, apiErr, apiErrorKey, hasServerProse, translateApiErr } from "./apiErr.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const SRC = fileURLToPath(new URL("..", import.meta.url));
const HERE = fileURLToPath(new URL(".", import.meta.url));

// The server's code table, read from source. Deriving the expectation from the
// Go file (rather than restating it) is what makes check 4 catch the drift that
// matters: a code added on the server with no dictionary entry fails HERE.
const errcodeGo = readFileSync(
  fileURLToPath(new URL("../../../api/internal/httpapi/errcode.go", import.meta.url)),
  "utf8",
);

/** Every `Code* = "..."` constant value in errcode.go. */
function serverSpecificCodes(): string[] {
  const out: string[] = [];
  for (const m of errcodeGo.matchAll(/^\s*Code\w+\s*=\s*"([^"]+)"/gm)) out.push(m[1]);
  return out.sort();
}

/**
 * The constant NAMES the package actually uses to build an error body, read from
 * every non-test .go file in api/internal/httpapi — not just errcode.go.
 *
 * WHY THE WHOLE PACKAGE: a handful of handlers answer a richer envelope than
 * writeErr and set their code directly (`CodeStepUpRequired` in auth_stepup.go,
 * `CodeChipCredentialsBad` in rest_chip.go). Reading only the lookup table would
 * report those as unreachable while they are in fact returned on every step-up
 * challenge and every rejected CHIP credential.
 */
function usedCodeNames(): Set<string> {
  const dir = fileURLToPath(new URL("../../../api/internal/httpapi/", import.meta.url));
  const names = new Set<string>();
  for (const name of readdirSync(dir)) {
    if (!name.endsWith(".go") || name.endsWith("_test.go") || name === "errcode.go") continue;
    const src = readFileSync(`${dir}/${name}`, "utf8");
    for (const m of src.matchAll(/\bCode[A-Z]\w+\b/g)) names.add(m[0]);
  }
  // errcode.go's lookup tables reference codes by constant name too.
  const tables = errcodeGo.replace(/^const \([\s\S]*?\n\)/gm, "");
  for (const m of tables.matchAll(/\bCode[A-Z]\w+\b/g)) names.add(m[0]);
  return names;
}

/** Every source file under src/ (excluding tests and the lib itself where apt). */
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = `${dir}/${name}`;
    if (statSync(p).isDirectory()) walk(p, out);
    else if (name.endsWith(".tsx") || name.endsWith(".ts")) out.push(p);
  }
  return out;
}

const sources = walk(SRC);

test("the source walk is non-empty and sees the real app (the guard cannot pass vacuously)", () => {
  assert.ok(
    sources.length > 40,
    `expected to walk the whole dashboard, got ${sources.length} source files`,
  );
  // Anchor: the file this bug was reported from must be in the walk, or the
  // guard is looking somewhere else entirely.
  assert.ok(
    sources.some((p) => p.endsWith("components/TemplatePicker.tsx")),
    "TemplatePicker.tsx is not in the walked set",
  );
  const codes = serverSpecificCodes();
  assert.ok(codes.length > 20, `expected the server's code table, got ${codes.length} codes`);
  assert.ok(codes.includes("workspace.notFound"), "the reported case must be a server code");
  assert.ok(
    usedCodeNames().size > 20,
    `expected the package's code usages, got ${usedCodeNames().size}`,
  );
});

test("no call site falls back to the server's error prose", () => {
  // THE guard, aimed at the exact bug shape. The reported defect was always
  //
  //     setErr(d.error || t("tpl.useFail"));
  //
  // i.e. `body.error` as the PREFERRED branch of a `||` fallback: it wins
  // whenever the server sent a message, which is precisely when it matters. So
  // the banned pattern is a `.error` READ feeding a `||`/`??` fallback.
  //
  // NOT banned (verified, not assumed):
  //   * `setErr(r.error)` — `r` is a LoadOutcome from runLoad() (lib/wsfetch.ts),
  //     which is ALREADY localized (it carries `errorKey`). Every such site was
  //     read and confirmed.
  //   * a plain `t("...", { error: x.error })` where the surrounding sentence is
  //     localized and the interpolated field is an English infrastructure
  //     diagnostic, not a localized API fault message.
  //
  // Exceptions are named here, one line each, WITH the reason — so a new
  // occurrence cannot slip in as a silent wildcard.
  const ALLOWED = [
    // The auto-CNAME 200-OK envelope: `{auto_cname:"failed", error:<Cloudflare
    // API diagnostic, English>}`. Wrapped in an already-localized sentence.
    't("set.autoCnameFailed", { error: res.error || "" })',
    // The webhook delivery LOG column: a monospace diagnostic trail
    // ("webhook url blocked: ...", a Go http client error). Never a message.
    '{d.error || "—"}',
  ];
  const BUG = /(?<![\w$])([A-Za-z_$][\w$]*)\s*\??\.\s*error\s*(\|\||\?\?)/;
  const offenders: string[] = [];
  for (const p of sources) {
    const rel = p.slice(SRC.length);
    // apiErr.ts legitimately reads `.error` (that is its job); wsfetch.ts reads
    // the body to hand it to apiErr(). Tests reference `error` directly.
    if (rel === "lib/apiErr.ts" || rel === "lib/wsfetch.ts") continue;
    if (rel.endsWith(".test.ts")) continue;
    const lines = readFileSync(p, "utf8").split("\n");
    lines.forEach((line, i) => {
      if (/^\s*(\/\/|\*|\/\*)/.test(line)) return; // a comment quoting the bug
      if (/(?<![\w$])(r|ws|outcome)\s*\??\.\s*error\s*(\|\||\?\?)/.test(line)) return; // runLoad outcome
      if (!BUG.test(line)) return;
      if (ALLOWED.some((a) => line.includes(a))) return;
      offenders.push(`${rel}:${i + 1}: ${line.trim()}`);
    });
  }
  assert.deepEqual(
    offenders,
    [],
    `these sites prefer the API's prose over a localized message (issue #6177) — ` +
      `route them through apiErr(body, t("your.fallback"), t):\n  ` +
      offenders.join("\n  "),
  );

  // And the fix must really be in place at the reported call site, not merely
  // absent: TemplatePicker is where the bug was observed.
  const picker = readFileSync(`${SRC}components/TemplatePicker.tsx`, "utf8");
  assert.match(
    picker,
    /apiErr\(d,\s*t\("tpl\.useFail"\)/,
    "the reported call site no longer translates the API error code",
  );
});

test("the reported case renders ENGLISH on lang=en, not the server prose", () => {
  // The exact payload from api/internal/httpapi/rest_scope.go:69 — the request
  // that produced Wan's report.
  const body = { error: "workspace tidak dijumpai", code: "workspace.notFound" };
  assert.ok(hasServerProse(body), "the fixture must really carry server prose");

  // DICT entries carry an extra `icon` field on some rows, so the cast is to
  // Record<string, unknown> and each locale is read as a string.
  const tIn = (lang: string) => (k: string) => {
    const entry = (DICT as Record<string, Record<string, unknown>>)[k];
    assert.ok(entry, `missing dictionary key ${k}`);
    const v = entry[lang];
    return typeof v === "string" ? v : (entry.en as string);
  };

  for (const lang of LANGS) {
    const rendered = apiErr(body, "FALLBACK", tIn(lang));
    assert.ok(
      !rendered.includes("workspace tidak dijumpai"),
      `lang=${lang} rendered the server's Malay prose: ${JSON.stringify(rendered)}`,
    );
    assert.notEqual(rendered, "FALLBACK", `lang=${lang} did not translate a KNOWN code`);
    assert.equal(
      rendered,
      apiErr(body, "FALLBACK", tIn(lang)),
      `lang=${lang} render is not deterministic`,
    );
    // English must not equal the Malay string either — the whole point.
    if (lang === "en") {
      assert.notEqual(rendered, tIn("ms")("apiErr.workspaceNotFound"));
    }
  }

  // The English text specifically, so a dictionary entry that quietly became
  // Malay for `en` is caught rather than merely "not the prose".
  assert.match(apiErr(body, "FALLBACK", tIn("en")), /workspace was not found/i);
});

test("an UNKNOWN code falls back to the caller's localized message, never the prose", () => {
  // A code the server adds later, or a body from an older build: the page's own
  // sentence must win. If this ever fell back to `body.error`, the whole fix
  // would be defeated by any code the dictionary has not caught up with.
  const unknown = { error: "workspace tidak dijumpai", code: "future.code.notInThisBuild" };
  assert.equal(apiErrorKey(unknown), "");
  assert.equal(apiErr(unknown, "Localized fallback.", () => "T"), "Localized fallback.");
  assert.equal(translateApiErr(unknown, () => "T"), "");

  // No code at all (a proxy error page, an OAuth error document).
  assert.equal(apiErr({ error: "some proxied prose" }, "Localized fallback.", () => "T"), "Localized fallback.");
  assert.equal(apiErr(null, "Localized fallback.", () => "T"), "Localized fallback.");
  assert.equal(apiErr(undefined, "Localized fallback.", () => "T"), "Localized fallback.");
  // A non-string code must not be treated as a lookup key.
  assert.equal(apiErr({ code: 42 }, "Localized fallback.", () => "T"), "Localized fallback.");
});

test("every code the server names has a translation in this dictionary", () => {
  const missing = serverSpecificCodes().filter((code) => !(code in API_ERROR_KEYS));
  assert.deepEqual(
    missing,
    [],
    `the API names these codes but the dashboard cannot translate them, so a ` +
      `user gets the page's generic message instead of the reason:\n  ` +
      missing.join("\n  "),
  );
  // The reverse direction: a mapping entry pointing at a dictionary key that
  // does not exist would render a bare key string.
  for (const [code, key] of Object.entries(API_ERROR_KEYS)) {
    assert.ok(key in DICT, `API_ERROR_KEYS["${code}"] → "${key}", which is not in DICT`);
  }
});

test("every API error message is translated in all five locales", () => {
  const incomplete: string[] = [];
  for (const key of Object.values(API_ERROR_KEYS)) {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    if (!entry) {
      incomplete.push(`${key} (absent)`);
      continue;
    }
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") incomplete.push(`${key}.${lang}`);
    }
  }
  assert.deepEqual(incomplete, [], `incomplete API error messages:\n  ${incomplete.join("\n  ")}`);
});

test("every declared code is actually reachable (no dead dictionary entries)", () => {
  // A code constant that nothing returns is dead: its translation can never be
  // reached. This is the drift that keeps every other check green while the
  // dictionary grows entries for codes that were renamed or dropped.
  const used = usedCodeNames();
  const dead = serverSpecificCodes().filter((code) => {
    const m = [...errcodeGo.matchAll(/^\s*(Code\w+)\s*=\s*"([^"]+)"/gm)].find((x) => x[2] === code);
    assert.ok(m, `no constant definition for ${code}`);
    return !used.has(m[1]);
  });
  assert.deepEqual(
    dead,
    [],
    `these codes are declared but never returned by any handler, so their ` +
      `translations are unreachable:\n  ${dead.join("\n  ")}`,
  );
});
