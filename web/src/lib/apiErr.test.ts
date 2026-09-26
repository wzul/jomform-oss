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

// ---------------------------------------------------------------------------
// Shared source-scanning helpers.
//
// The comment and string stripping below is what makes the checks DERIVED from
// the source rather than from a hand-maintained list of variable names: an
// earlier version of this guard hardcoded `(r|ws|outcome)` as "the things that
// hold a runLoad outcome", which is true by luck and goes stale silently.
// ---------------------------------------------------------------------------

/** `code` is the line with string/template CONTENTS blanked, so an identifier
 *  match cannot come from inside a literal (e.g. a sentence quoting `d.error`). */
const stripStrings = (l: string): string => l.replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g, '""');
const isCommentLine = (l: string): boolean => /^\s*(\/\/|\*|\/\*)/.test(l);
type ScanLine = { n: number; text: string; code: string };
const scanLines = (p: string): ScanLine[] =>
  readFileSync(p, "utf8")
    .split("\n")
    .map((text, i) => ({ n: i + 1, text, code: stripStrings(text) }));

// Exceptions, one line each, WITH the reason — so a new occurrence cannot slip
// in as a silent wildcard. Shared by every check below.
const ALLOWED = [
  // The auto-CNAME 200-OK envelope: `{auto_cname:"failed", error:<Cloudflare
  // API diagnostic, English>}`. Wrapped in an already-localized sentence.
  't("set.autoCnameFailed", { error: res.error || "" })',
  // The webhook delivery LOG column: a monospace diagnostic trail
  // ("webhook url blocked: ...", a Go http client error). Never a message.
  '{d.error || "—"}',
];

/**
 * `const NAME = <expr>` initializers in one file, for the alias hop below
 * (`const msg = apiErr(d, …)` then `setErr(msg)`).
 */
function localInitializers(lines: ScanLine[]): Map<string, string> {
  const m = new Map<string, string>();
  for (const l of lines) {
    if (isCommentLine(l.text)) continue;
    const g = /^\s*(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(.*)$/.exec(l.code);
    if (g && !m.has(g[1])) m.set(g[1], g[2]);
  }
  return m;
}

/** True when an expression resolves — through one alias hop — to an apiErr() or
 *  translateApiErr() call. This is the property the card asks for: every
 *  failure-body render goes THROUGH the shared helper. */
function bindsApiErr(expr: string, inits: Map<string, string>, seen = new Set<string>()): boolean {
  if (/(?<![\w$.])(?:apiErr|translateApiErr)\s*\(/.test(expr)) return true;
  const id = /^\s*([A-Za-z_$][\w$]*)\s*;?\s*$/.exec(expr);
  if (!id) return false;
  const init = inits.get(id[1]);
  if (init === undefined || seen.has(id[1])) return false;
  seen.add(id[1]);
  return bindsApiErr(init, inits, seen);
}

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
  // NOT banned: `setErr(r.error)` where `r` is a LoadOutcome from runLoad()
  // (lib/wsfetch.ts) — that `error` is ALREADY localized. That exemption is
  // DERIVED below (the variable is assigned from a runLoad call in the same
  // file), not hardcoded as a name list, and the sibling check further down
  // covers the case where such a variable is read WITHOUT a fallback.
  const BUG = /(?<![\w$])([A-Za-z_$][\w$]*)\s*\??\.\s*error\s*(\|\||\?\?)/;
  const offenders: string[] = [];
  for (const p of sources) {
    const rel = p.slice(SRC.length);
    // apiErr.ts legitimately reads `.error` (that is its job); wsfetch.ts reads
    // the body to hand it to apiErr(). Tests reference `error` directly.
    if (rel === "lib/apiErr.ts" || rel === "lib/wsfetch.ts") continue;
    if (rel.endsWith(".test.ts")) continue;
    const src = readFileSync(p, "utf8");
    // Variables in THIS file that hold a runLoad outcome: one whose assignment
    // sits at the start of a statement and mentions runLoad. Derived, so a page
    // that renames `r` to `load` keeps its exemption and no stale name list can
    // silently widen it.
    const loadVars = new Set<string>();
    for (const m of src.matchAll(/(?:^|[;{}(\s])(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\s+runLoad\b/g)) {
      loadVars.add(m[1]);
    }
    scanLines(p).forEach(({ n, text, code: stripped }) => {
      if (isCommentLine(text)) return; // a comment quoting the bug
      const read = BUG.exec(stripped);
      if (!read) return;
      if (loadVars.has(read[1])) return; // a runLoad outcome — already localized
      if (ALLOWED.some((a) => text.includes(a))) return;
      offenders.push(`${rel}:${n}: ${text.trim()}`);
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

test("a failure body is never rendered without going through apiErr/translateApiErr", () => {
  // THE HOLE THIS CLOSES (found while fixing /connect, issue #6177 follow-up).
  // The check above only bans a `.error` read that FEEDS a `||`/`??` fallback.
  // Two identical-prose defects walked straight past it:
  //
  //   setError(await r.text())      // the RAW JSON envelope, prose included
  //   setUploadMsg(d.error)         // prose with NO fallback at all
  //
  // Both were proven green against the old guard by mutation. So this check is
  // a RENDER-REACHABILITY rule instead of a fallback-shape rule: if the value
  // handed to an error-state setter comes from an HTTP failure body — the whole
  // body, its `.text()`, or its `.error` — it must resolve to apiErr() /
  // translateApiErr(). Anything else is a failure body reaching the screen
  // verbatim, which is the bug, whatever the syntax around it looks like.
  //
  // NOT offenders (each verified by running the suite):
  //   * `setErr(r.error)` / `setLoadErr("")` etc. where the argument is a
  //     runLoad outcome — `error` there is ALREADY localized (see wsfetch.ts),
  //     and which names hold one is DERIVED from the file, not hardcoded.
  //   * a setter whose argument never touches a Response body (a literal, a
  //     `t(...)` sentence, an `err.message`, a validation string) — and a
  //     caught error's `.message`, which is not the HTTP prose channel.
  //
  // `text`/`json` are deliberately in the pattern: reading the raw body is the
  // exact defect this card fixes, and a legitimate need for it (a non-JSON
  // upstream diagnostic) belongs in an ALLOWED entry with its reason.
  const SETTER = /(?<![\w$])set([A-Za-z_$][\w$]*)\s*\(([^;]*?)\)\s*;/g;
  /** Does this expression read an HTTP failure body — the whole body, or the
   *  `error` prose of one that is not a localized runLoad outcome? */
  const readsRawBody = (arg: string, loadVars: Set<string>): boolean => {
    if (/\.\s*(?:text|json)\s*\(\s*\)/.test(arg)) return true;
    for (const m of arg.matchAll(/([A-Za-z_$][\w$]*)\s*\??\.\s*error\b/g)) {
      if (!loadVars.has(m[1])) return true;
    }
    return false;
  };
  const offenders: string[] = [];
  for (const p of sources) {
    const rel = p.slice(SRC.length);
    if (rel === "lib/apiErr.ts" || rel === "lib/wsfetch.ts") continue;
    if (rel.endsWith(".test.ts")) continue;
    const lines = scanLines(p);
    const inits = localInitializers(lines);
    const loadVars = new Set<string>();
    for (const m of readFileSync(p, "utf8").matchAll(/(?:^|[;{}(\s])(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\s+runLoad\b/g)) {
      loadVars.add(m[1]);
    }
    // The state a setter writes must be an ERROR state — a name ending in
    // "Err"/"Error"/"Msg"/"Fail". That is the convention every call site in
    // this repo already follows, and it keeps a data setter out of the walk.
    const isErrorState = (s: string) => /(err|error|msg|fail)$/i.test(s);
    for (const { n, text, code: stripped } of lines) {
      if (isCommentLine(text)) continue;
      for (const m of stripped.matchAll(SETTER)) {
        if (!isErrorState(m[1])) continue;
        const arg = m[2].trim();
        if (arg === "") continue;
        if (ALLOWED.some((a) => text.includes(a))) continue;
        if (!readsRawBody(arg, loadVars)) continue;
        if (bindsApiErr(arg, inits)) continue;
        offenders.push(`${rel}:${n}: ${text.trim()}`);
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `these sites render an HTTP failure body without translating it (issue #6177) — ` +
      `parse the body and pass it to apiErr(body, t("your.fallback"), t):\n  ` +
      offenders.join("\n  "),
  );

  // The /connect page is the file this card was filed against: assert the fix
  // is PRESENT there, not merely that the walk found nothing.
  const connect = readFileSync(`${SRC}app/connect/page.tsx`, "utf8");
  assert.match(
    connect,
    /setError\(apiErr\(body,\s*t\("connect\.fail"\),\s*t\)\)/,
    "the reported call site no longer translates the connect error code",
  );
  assert.match(
    connect,
    /setError\(apiErr\(body,\s*t\("connect\.resendFail"\),\s*t\)\)/,
    "the resend call site no longer translates the connect error code",
  );
  // Code only, not prose: this file's own comment quotes `r.text()` to explain
  // the defect, and an assertion that trips on the explanation would be wrong.
  const rawBodyReads = scanLines(`${SRC}app/connect/page.tsx`)
    .filter((l) => !isCommentLine(l.text) && /\.\s*text\s*\(/.test(l.code))
    .map((l) => `${l.n}: ${l.text.trim()}`);
  assert.deepEqual(
    rawBodyReads,
    [],
    `the connect page is reading the raw failure body again:\n  ${rawBodyReads.join("\n  ")}`,
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
