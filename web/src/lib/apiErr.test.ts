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
//   6. The dictionary maps no code the server has RETIRED. The reverse of 4: that
//      check reads only the codes errcode.go declares, so a mapping for a code
//      renamed or retired out of the Go file is invisible to it, and the entry can
//      never fire. Measured before this landed: switching apiErr.ts's commented-out
//      `workspace.notEmpty` row back on left the whole file green, so the
//      retirement's comment claimed a guard nothing enforced. Pinned by re-planting
//      that retired key in memory (last test), so the predicate is seen to bite.
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
 * The dictionary's keys that are NOT the value of any `Code*` constant in
 * errcode.go — i.e. a mapping for a code the server cannot send (card
 * t_ef27e70a).
 *
 * A code the server RETIRED looks exactly like a code it never had: the entry
 * can never fire, so its translation is unreachable. The reachability check below
 * cannot see this direction at all — it walks the codes the server DOES declare —
 * so this is the half that catches a retired row (the `workspace.notEmpty` the
 * workspace-trash move retired) being switched back on in apiErr.ts.
 *
 * ONE key is not a `Code*` constant on purpose: `reserved_subdomain` is
 * urlguard's policy token (ReservedSubdomainCode), documented at its entry in
 * apiErr.ts — the name is well formed and simply belongs to JomForm. It is
 * allow-listed here rather than added to errcode.go, because the Go file's
 * constants are the shape/scope codes and pretending otherwise would put the
 * token in a table that never returns it.
 */
const NOT_A_ERRCODE_CONSTANT = new Set(["reserved_subdomain"]);

function undeclaredMappings(keys: string[]): string[] {
  const declared = new Set(serverSpecificCodes());
  return keys.filter((c) => !declared.has(c) && !NOT_A_ERRCODE_CONSTANT.has(c)).sort();
}

/**
 * `text` with Go comments removed, so a mention of a constant in PROSE does not count as a
 * use of it (card t_a65dfe2b).
 *
 * The reachable check accepted a comment as evidence that a code is returned. Measured: with
 * the four `errCodeByMessage` entries of `t_74c46aaa` deleted, the only remaining mention of
 * e.g. `CodeConnectTokenMissing` was `social.go`'s comment naming it — and the guard stayed
 * GREEN, so a declaration-only revert of that fix passed every web check. The Go-side guard
 * bites in the same mutation, so the pair was half-covered.
 *
 * Both forms are removed: `/* … *​/` blocks (across lines) and `//` tails. Strings are
 * blanked FIRST, so a `//` inside a literal does not start a comment — measured, that
 * ordering matters: the `url.notHttps` prefix entry is `{" mesti bermula dengan https://",
 * CodeURLNotHTTPS}`, and stripping `//` tails without blanking the string first ate the
 * constant name and reported a LIVE code as dead. (That was this function's own first cut,
 * caught by running it.)
 */
function goCodeOnly(text: string): string {
  const noBlocks = text.replace(/\/\*[\s\S]*?\*\//g, "");
  return noBlocks
    .split("\n")
    .map((l) => stripStrings(l).replace(/\/\/.*$/, ""))
    .join("\n");
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
    const src = goCodeOnly(readFileSync(`${dir}/${name}`, "utf8"));
    for (const m of src.matchAll(/\bCode[A-Z]\w+\b/g)) names.add(m[0]);
  }
  // errcode.go's lookup tables reference codes by constant name too — and the TABLE
  // MENTIONS are what keep a literal-keyed refusal reachable: the handler answers the
  // literal, the table maps it to the constant, and the runtime lookup is where the code
  // is returned. So these count, and they are read from CODE, not prose.
  const tables = goCodeOnly(errcodeGo.replace(/^const \([\s\S]*?\n\)/gm, ""));
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
 * The ONE file allowed to read a `.error` field, with its reason. An explicit
 * list, not a pattern: every other file that touches `.error` must go through
 * apiErr(), and the point of the check below is that a module cannot re-export
 * the prose under a new name (t_10453dd9 — `publishResult.ts` did exactly that
 * with an `outcomeMessage()` helper, which is why that module is NOT in this
 * list and `publishErrorText` routes through apiErr instead).
 *
 *   * `lib/apiErr.ts` — reading `.error` IS its job; it converts the envelope
 *     into a dictionary sentence and never returns the prose.
 *
 * NOT listed, deliberately: `lib/publishResult.ts` (classifies the response; it
 * reads the `code` sibling only), `lib/wsfetch.ts` (parks the parsed body on
 * HttpError for apiErr's callers, never the prose) and the generated spec/example
 * modules (schemas, no failure rendering). If any of them starts reading
 * `.error` for a message, this check fails rather than letting the prose travel
 * one hop further from the wire.
 */
const PROSE_READERS_ALLOWED = new Set(["lib/apiErr.ts"]);

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

test("no source file reads the failure BODY's prose field", () => {
  // THE HOLE THIS CLOSES (t_10453dd9, reproduced in a browser). Both checks above
  // reason about the SYNTAX around a read — a `.error` feeding a `||`, or a
  // setter's argument resolving to apiErr. Neither can see a module that reads
  // `.error` somewhere else and hands the string on under a new name, which is
  // exactly what happened:
  //
  //   publishResult.ts   errorMessage(body) → { kind:"error", message }   // prose in
  //   builder/page.tsx   message: outcomeMessage(outcome) || t("…")       // prose out
  //
  // The call site's argument never contained a `.error` read (it was a local
  // helper call), so the second check cleared it, and there was no `||` on the
  // read itself, so the first one did too. The Malay sentence reached the screen
  // through a green suite — measured on the "Publish anyway" retry, which answers
  // an uncoded 409.
  //
  // So this check does not look at the syntax at all: it looks at WHO MAY TOUCH
  // THE PROSE FIELD. Reading `.error` is apiErr()'s job and nobody else's, and
  // the ban is derived — any new reader fails the build — so the next module that
  // wants to "just parse the body into a message field" cannot reintroduce the
  // class. The exemption list is asserted to be exactly one entry: a growing list
  // is how a rule like this rots.
  assert.deepEqual(
    [...PROSE_READERS_ALLOWED],
    ["lib/apiErr.ts"],
    "the exemption list grew — every entry is a file that may put the API's prose in a `message`",
  );
  const READ = /\.\s*error\b/;
  const offenders: string[] = [];
  let examined = 0;
  for (const p of sources) {
    // NOTE: this walk's `rel` carries a leading "/" (SRC resolves to the src
    // directory itself), so it is normalised here rather than compared raw.
    const rel = p.slice(SRC.length).replace(/^\/+/, "");
    if (rel.endsWith(".test.ts")) continue;
    if (PROSE_READERS_ALLOWED.has(rel)) continue;
    // A runLoad outcome's `.error` is ALREADY localized (runLoad renders
    // `message` or the machine code — see lib/wsfetch.ts). Which names hold one
    // is DERIVED from the file, exactly as in the check above, so a page that
    // renames `r` keeps its exemption and no stale name list can widen it.
    const src = readFileSync(p, "utf8");
    const loadVars = new Set<string>();
    for (const m of src.matchAll(/(?:^|[;{}(\s])(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\s+runLoad\b/g)) {
      loadVars.add(m[1]);
    }
    // String/template CONTENTS are blanked, so a sentence that merely mentions
    // `d.error` (documentation, a comment, an error-message string) is not a read.
    scanLines(p).forEach(({ n, text, code: stripped }) => {
      if (isCommentLine(text)) return;
      if (!READ.test(stripped)) return;
      for (const m of stripped.matchAll(/([A-Za-z_$][\w$]*)\s*\??\.\s*error\b/g)) {
        if (loadVars.has(m[1])) return; // a runLoad outcome — already localized
      }
      examined++;
      if (ALLOWED.some((a) => text.includes(a))) return;
      offenders.push(`${rel}:${n}: ${text.trim()}`);
    });
  }
  assert.deepEqual(
    offenders,
    [],
    `these files read the failure body's \`error\` prose (t_10453dd9) — the prose may ` +
      `travel exactly one hop (apiErr) and never become a message a page renders. ` +
      `Pass the BODY to apiErr(body, t("your.fallback"), t) instead, or read the ` +
      `machine \`code\`:\n  ` +
      offenders.join("\n  "),
  );
  // A floor, so an empty offender list cannot be the result of the walk breaking
  // (a rename of `src/`, a broken glob) rather than of the rule holding.
  assert.ok(examined > 0, "no `.error` read was examined at all — the walk is broken");

  // And the two modules this card is about must stay message-less: the prose had
  // a `message` field to hide in, and that field (not the helper name) is what
  // put the sentence on the screen.
  const publish = readFileSync(`${SRC}lib/publishResult.ts`, "utf8");
  assert.doesNotMatch(
    publish,
    /message\s*:\s*errorMessage|kind:\s*"error",\s*message:/,
    "publishResult.ts carries a pre-rendered message again — that field is where the prose reached the dashboard",
  );
  assert.match(
    publish,
    /publishErrorText[\s\S]*?apiErr\(o\.body/,
    "publishErrorText no longer routes the failure through apiErr",
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
  //
  // AND THE NAME RULE IS NOT ALLOWED TO BE THE ONLY NET (issue #6177 follow-up).
  // The state a setter writes must be an error state — a name ending in
  // "Err"/"Error"/"Msg"/"Fail". That convention is what keeps a data setter out
  // of this walk, but it is also a hole: `setNotice(d.error)` renders the very
  // same prose and is invisible here because of its NAME. Measured: at the time
  // this was added, 4 setter-shaped calls in src/ read a failure body and 0 of
  // them sat outside the rule — so there is no live defect, and the hole is a
  // future one. It is closed below by asserting that the out-of-rule bucket is
  // EMPTY: a new `setNotice(d.error)` (or `setBanner(await r.text())`) fails the
  // build and forces a decision — rename the state so the rule covers it, route
  // the body through apiErr(), or add an explicit ALLOWED entry with the reason.
  // The `examined` floor keeps that empty-list assertion from passing vacuously
  // if the walk ever stops finding setter-shaped body reads at all.
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
  /** Setter-shaped calls that read a failure body but whose NAME the rule above
   *  does not cover — the blind spot this follow-up closes. Must stay empty. */
  const unexamined: string[] = [];
  /** How many setter-shaped body reads the walk actually examined. A floor, so
   *  the `unexamined === []` assertion cannot pass by seeing nothing. */
  let examined = 0;
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
    // THE WALK IS WHOLE-FILE, NOT PER LINE (card t_b6cd0f72).
    //
    // This used to run SETTER against each line's stripped text. That is a hole
    // exactly one layer under the one this file was written to close: the pattern
    // is `set…([^;]*?);`, so a call whose ARGUMENT starts on the next line
    // (`setError(\n  d.error\n)`) matched on no single line and was invisible to
    // BOTH the name rule and the out-of-rule bucket — even when the setter's name
    // IS covered by the rule. Measured: that mutation at MediaPicker.tsx:104 left
    // this guard GREEN 8/8, while the single-line `setUploadMsg(d.error)` at the
    // same site was RED.
    //
    // So the stripped text is joined into one buffer (line boundaries kept, so the
    // shape of the code is unchanged) and matched as a whole; a match's offset is
    // mapped back to the line it STARTS on for the report. There is no formatter in
    // web/ that emits the multi-line shape today, so this is a future hole — which
    // is why it is closed here rather than by reformatting call sites.
    const joined = lines.map((l) => l.code).join("\n");
    const starts: number[] = [];
    for (let i = 0, off = 0; i < lines.length; i++) {
      starts.push(off);
      off += lines[i].code.length + 1;
    }
    const lineAt = (idx: number): number => {
      let lo = 0;
      let hi = starts.length - 1;
      let best = 0;
      while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (starts[mid] <= idx) {
          best = mid;
          lo = mid + 1;
        } else {
          hi = mid - 1;
        }
      }
      return best;
    };
    for (const m of joined.matchAll(SETTER)) {
      const li = lineAt(m.index ?? 0);
      const firstLine = lines[li];
      if (isCommentLine(firstLine.text)) continue;
      const arg = m[2].trim();
      if (arg === "") continue;
      // The reported span, whitespace-normalised: a multi-line call reads as one
      // line in the message, and the line number is where it begins.
      const span = (m[0] ?? "").replace(/\s+/g, " ").trim() || firstLine.text.trim();
      if (ALLOWED.some((a) => span.includes(a) || firstLine.text.includes(a))) continue;
      if (!readsRawBody(arg, loadVars)) continue;
      if (!isErrorState(m[1])) {
        // Not covered by the name rule — an `setNotice(d.error)`-shaped hole.
        // Reported regardless of apiErr binding: if it binds apiErr the fix is
        // just to name the state for what it holds (or add an ALLOWED entry);
        // if it does not, it is also a live render of the raw body.
        unexamined.push(`${rel}:${firstLine.n}: ${span}`);
        continue;
      }
      examined++;
      if (bindsApiErr(arg, inits)) continue;
      offenders.push(`${rel}:${firstLine.n}: ${span}`);
    }
  }
  // The completeness half, asserted BEFORE the primary check so a reader sees
  // the hole named first: no setter-shaped body read may sit outside the name
  // rule, because the rule is a NAME convention and a name is not a safety
  // property. An empty list is only meaningful if something was examined.
  assert.deepEqual(
    unexamined,
    [],
    `these setters read an HTTP failure body but their NAME (not err/error/msg/fail) ` +
      `keeps them out of the check above, so the body can reach the screen untranslated ` +
      `(issue #6177 follow-up). Rename the state so it is covered, or route the body ` +
      `through apiErr(body, t("your.fallback"), t):\n  ` +
      unexamined.join("\n  "),
  );
  assert.ok(
    examined > 0,
    "the name rule examined no setter-shaped body reads at all — the walk is broken, " +
      "so its empty offender list proves nothing",
  );
  assert.deepEqual(
    offenders,
    [],
    `these sites render an HTTP failure body without translating it (issue #6177) — ` +
      `parse the body and pass it to apiErr(body, t("your.fallback"), t):\n  ` +
      offenders.join("\n  "),
  );

  // The /connect page is the file this card was filed against: assert the fix
  // is PRESENT there, not merely that the walk found nothing. The token path now
  // stores the raw body and derives the sentence AT RENDER (`error` below) — see
  // src/lib/connectTokenEffect.test.ts for why (a pre-translated string freezes
  // in the provider's initial language, and translating inside the effect makes
  // it depend on the translator's identity, which re-POSTs the token). The
  // property asserted is "the failure body goes through apiErr with a runtime
  // translator", not "the call is spelled setError(t(...))".
  const connect = readFileSync(`${SRC}app/connect/page.tsx`, "utf8");
  assert.match(
    connect,
    /apiErr\(\s*[\w.]*body,\s*t\("connect\.fail"\),\s*t\)/,
    "the token-failure call site no longer translates the connect error code",
  );
  assert.match(
    connect,
    /setResendError\(apiErr\(\s*body,\s*t\("connect\.resendFail"\),\s*t\)\)/,
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

test("every code this dictionary maps is one the server still declares (no mapping for a retired code)", () => {
  // The OTHER half of the dead-entry drift, and the one this test's sibling
  // above cannot see: it walks the codes errcode.go DECLARES, so a mapping whose
  // code was retired/renamed on the server is invisible to it — the key is not
  // in its list to check. Measured before this guard existed (2026-10-13):
  // switching the commented-out `"workspace.notEmpty"` row in apiErr.ts back on
  // left the whole file GREEN, so the retirement's own comment ("apiErr.test.ts
  // fails on a mapping for a code errcode.go does not declare") was a claim
  // nothing enforced.
  //
  // WHY IT MATTERS rather than being cosmetic: apiErr.ts keeps retired codes OUT
  // of the map precisely so no translation can be attached to a code the server
  // cannot send. A re-added row is the drift the retirement was written to stop,
  // and it also re-arms the mapping if the code ever comes back with different
  // semantics — the `workspace.notEmpty` refusal is now impossible (a workspace
  // goes to the Trash instead of being refused for its content).
  const keys = Object.keys(API_ERROR_KEYS);
  // Floor: the filter must be looking at the real dictionary, not at nothing.
  assert.ok(keys.length >= 40, `API_ERROR_KEYS has shrunk to ${keys.length} keys — the check below is vacuous`);
  assert.deepEqual(
    undeclaredMappings(keys),
    [],
    `these codes are mapped here but no longer declared in errcode.go (the ` +
      `mapping can never fire, so its translation is unreachable):\n  ` +
      undeclaredMappings(keys).join("\n  "),
  );

  // BITE PROOF (in memory, nothing on disk): the retired code planted back into
  // the key list must be REPORTED by the very predicate above. Without this, a
  // predicate that returned [] for any input would pass the assertion, and the
  // guard would be a claim rather than a check.
  const planted = [...keys, "workspace.notEmpty"];
  assert.deepEqual(
    undeclaredMappings(planted),
    ["workspace.notEmpty"],
    "the retired code planted back into the dictionary was not reported as undeclared",
  );
});

