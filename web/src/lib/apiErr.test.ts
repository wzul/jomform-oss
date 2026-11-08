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
const NOT_A_ERRCODE_CONSTANT = new Set(["reserved_subdomain", "form.slugReserved"]);

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

/**
 * The prose field's own computed key is CODE, not prose — and blanking it was the
 * second half of the defect card t_d7552433 closes.
 *
 * `stripStrings` blanks EVERY string literal, which rewrites `b["error"]` to
 * `b[""]`: the KEY of a bracket read disappears before any shape rule can see it.
 * Measured while writing the widening: with the shapes below run on `code` as it
 * was, an appended `const e = b["error"]` in lib/formPublicURL.ts was still GREEN —
 * the very mutation this card exists to catch went straight through the new rule.
 *
 * So the key literal is masked BEFORE blanking and unmasked AFTER it:
 *   * a key that is real code survives — `b["error"]`, `b[ "error" ]`, `b['error']`;
 *   * a key INSIDE a bigger string does not (that literal is blanked whole, and the
 *     mask with it) — so `'never read b["error"] here'` is still prose, not a read;
 *   * every index is preserved: the mask token is exactly as long as the literal it
 *     replaces (7 chars), which the `||`/`??` follow-up rule depends on — it slices
 *     the code AFTER a read and would be thrown off by a length change;
 *   * the restore spells the key `"error"` regardless of the quotes it was written
 *     with. This text is for MATCHING only (reports quote `text`, the raw line), and
 *     both quote styles are one shape.
 */
const PROSE_KEY_TOKEN = "\u0001\u0001\u0001\u0001\u0001\u0001\u0001";
const PROSE_KEY_LITERAL = /(["'])error\1(?=\s*\])/g;
const unblankingKey = (text: string): string =>
  stripStrings(text.replace(PROSE_KEY_LITERAL, PROSE_KEY_TOKEN)).replace(
    new RegExp(PROSE_KEY_TOKEN, "g"),
    '"error"',
  );

type ScanLine = { n: number; text: string; code: string };
/** One file's TEXT as scan lines — the shape `scanLines` reads from disk, split out so
 *  an in-memory fixture can be walked through the SAME walk the real tree goes
 *  through (card t_665b6539: a fixture is not the same instrument as a walk, unless
 *  it is handed to the walk). */
function linesOf(text: string): ScanLine[] {
  return text.split("\n").map((t, i) => ({ n: i + 1, text: t, code: unblankingKey(t) }));
}
const scanLines = (p: string): ScanLine[] => linesOf(readFileSync(p, "utf8"));

/**
 * One file's blanked lines as ONE code buffer, plus a lookup from a buffer offset
 * to the index of the line it sits on. The three shapes this file claims that span
 * lines — a setter call whose argument starts below it (card t_b6cd0f72), a
 * destructured parameter broken over several lines (card t_69966a9a) and a
 * `const`/`let`/`var` pattern broken over several lines (card t_665b6539) — share
 * this one buffer-and-lookup, so the walks cannot drift apart on what "the line a
 * match starts on" means.
 */
function joinLines(lines: ScanLine[]): { code: string; lineAt: (i: number) => number } {
  const code = lines.map((l) => l.code).join("\n");
  const starts: number[] = [];
  for (let i = 0, off = 0; i < lines.length; i++) {
    starts.push(off);
    off += lines[i].code.length + 1;
  }
  /** The index of the line whose first character is at or before `idx`. */
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
  return { code, lineAt };
}

// ---------------------------------------------------------------------------
// Reading the failure body's prose field — ONE definition, shared by the three
// checks below that reason about such a read (card t_d7552433).
//
// WHY ONE DEFINITION. The prose-field check matched the DOT spelling only
// (`const READ = /\.\s*error\b/`), and its comment claimed that did not matter
// because the rule "does not look at the syntax at all". It is a shape match, and
// the shapes are the whole reach: measured on that build with a reader appended to
// `lib/formPublicURL.ts`, `const e = b["error"]` and
// `const { error } = (body ?? {}) as { error?: unknown }` both left the file
// 9 pass / 0 fail — GREEN, prose reader undetected — while the dot control at the
// same site was RED naming `lib/formPublicURL.ts:62`. A bracket read is the
// natural spelling once the body is typed `Record<string, unknown>`, so the class
// the check exists to close was reachable again with the guard green. The sibling
// checks (`BUG` below, and `readsRawBody`) shared the blind spot, which is why the
// shapes live here ONCE instead of being respelled at each call site — a second
// spelling of "what a read of this field looks like" is how the three drift apart.
// ---------------------------------------------------------------------------

/** One read of the prose field: where it BEGINS and ENDS, and what it was read FROM. */
type ProseRead = {
  /**
   * Where the read begins — the receiver/property itself for a dot or bracket read,
   * the `const`/`let`/`var` head for a binding destructure (a pattern broken over
   * lines starts on the line above its `{`). A whole-file pass reports on the line
   * this offset sits on.
   */
  start: number;
  /**
   * The identifier the read is taken FROM when the source is a plain identifier —
   * `r.error` → "r", `r["error"]` → "r", `const { error } = r` → "r" — and null
   * when it is a compound expression (`(body ?? {})["error"]`,
   * `const { error } = pick(body)`), where no name can be attributed at all.
   *
   * The runLoad-outcome exemption needs this, and null must NEVER be read as
   * "known to be localized": an unattributable read is an offender.
   */
  recv: string | null;
  /** Index just past the read, so a caller can ask what the read FEEDS. */
  end: number;
};

/** The spellings ONE read of the field can be written in. The input is `scanLines`'
 *  `code` — string/template CONTENTS blanked, BUT the field's own bracket key kept
 *  (see `unblankingKey`) — so a sentence that merely mentions `d.error`, and a
 *  comment that quotes `b["error"]`, are not reads while `b["error"]` itself is.
 *  The receiver is optional in both: the DOT form must still match a read with no
 *  attributable name (`(body as ApiErrorBody)?.error`), which is how the
 *  pre-widening check's line filter saw those lines. */
const DOT_READ = /([A-Za-z_$][\w$]*)?\s*\??\s*\.\s*error\b/g;
const BRACKET_READ = /([A-Za-z_$][\w$]*)?\s*\??\s*(?:\.\s*)?\[\s*["']error["']\s*\]/g;
/** A binding pattern follows: `const`/`let`/`var` then `{`. `\s*` spans line breaks on
 *  purpose — a pattern whose `{` sits on the line below its `const` is the same
 *  binding, and the shape is matched over the whole file's buffer (card
 *  t_665b6539). */
const BINDING_HEAD = /\b(?:const|let|var)\s*\{/g;

/**
 * The index of the bracket that closes the one at `open` — a `(`/`{`/`[` — over a
 * whole file's `code`, or undefined when it never closes. Brackets nest, so the
 * `}` of an inner object does not close an outer pattern and the `(` of a call
 * inside a pattern does not close the parameter list.
 *
 * WHY IT TAKES A WHOLE FILE, NOT A LINE. Two shapes this file claims span lines:
 * a setter call whose argument starts below it (card t_b6cd0f72) and a
 * destructured parameter broken over several lines (card t_69966a9a). Both need
 * the CODE AROUND the shape to find its end, so the match is taken over the file
 * joined back into one buffer.
 *
 * A `(`/`{` inside a COMMENT or a regex literal is counted like code — the same
 * exposure `stripStrings` leaves everywhere (it blanks string literals, not
 * comments).
 */
function matchingBracket(code: string, open: number): number | undefined {
  const o = code[open];
  const c = o === "{" ? "}" : o === "(" ? ")" : "]";
  let depth = 0;
  for (let i = open; i < code.length; i++) {
    if (code[i] === o) depth++;
    else if (code[i] === c) {
      depth--;
      if (depth === 0) return i;
    }
  }
  return undefined;
}

/** The braces-balanced text of the pattern opening at `code[open]` (a `{`),
 *  INCLUDING both braces (`{ error }`, `{ a: { error } }`), or undefined when it
 *  never closes. The braces are kept because `bindsErrorProperty` reads the
 *  property in its real position, where the closing brace (or a comma) is what
 *  ends it.
 *
 *  This used to walk only the line it was handed, and the comment said a
 *  multi-line pattern was "not claimed". That was a fact about the WALK, not
 *  about the pattern: `bindingDestructuredReads` below fed it one line at a time,
 *  so a `const` pattern broken over lines was invisible while the one-line
 *  spelling of the same read reded. The span itself was already whole-file
 *  correct, which is what lets the parameter spelling be claimed in the shape it
 *  is written in; the binding walk now hands it the file's buffer too (card
 *  t_665b6539), so both spellings are claimed in the wrapped shape a long pattern
 *  is written in. */
function braceSpan(code: string, open: number): { pattern: string; close: number } | undefined {
  const close = matchingBracket(code, open);
  return close === undefined ? undefined : { pattern: code.slice(open, close + 1), close };
}

/** Does a destructuring pattern bind the property `error`, at any depth —
 *  `{ error }`, `{ error: m }`, `{ a: { error } }`, `{ error = "" }`, in any of the
 *  line shapes a wrapped pattern is written in?
 *
 *  This answers ONLY "does this pattern bind the field". Whether the text IS a
 *  pattern is the caller's question, and the two callers answer it differently: a
 *  `const`/`let`/`var` head is unambiguous, while a destructured PARAMETER is
 *  claimed only where its context is unambiguous (`function …(` before it, or `=>`
 *  after it) — the same text in argument position (`render({ error: msg })`) is an
 *  object LITERAL, a write, and no shape match can tell the two apart. */
const bindsErrorProperty = (pattern: string): boolean =>
  /(?:^|[{,\s])error\s*(?::\s*[A-Za-z_$][\w$]*\s*)?(?:=\s*[^,}]*)?(?=[,}])/.test(pattern);

/** `const|let|var { … } = …` bindings in `code` whose pattern binds `error`. */
type BindingRead = ProseRead & {
  /** The pattern itself, braces included (`{ error }`), as blanked code — so a
   *  multi-line pattern can be quoted in a report whose line does not contain it. */
  pattern: string;
};

/**
 * `const|let|var { … } = …` bindings in `code` whose pattern binds `error`.
 *
 * `code` MUST BE A WHOLE FILE'S BLANKED BUFFER, NEVER ONE LINE (card t_665b6539).
 *
 * The pattern is the shape a multi-key destructure of a response body is written
 * in — `{ code, error }` on one line once it is short, and
 *
 *     const {
 *       code,
 *       error,
 *     } = body;
 *
 * once it is not (a formatter, or a hand that wraps a long pattern). BOTH spell the
 * same read, and only the second one was invisible.
 * MEASURED, 2026-10-27: src/ carries 69 `const { … } = …` patterns on one line and
 * 0 broken over lines, so this is a FUTURE hole in this tree — closed here rather
 * than by waiting for the first wrapped pattern, exactly as the multi-line setter
 * hole was closed in t_b6cd0f72.
 *
 * Handed one line at a time, the walk saw `const {` and no closing brace, so
 * `braceSpan` returned undefined and the read was invisible to every check —
 * exactly the reach hole the one-line spelling had already closed. Measured on the
 * shipped guard: the wrapped pattern appended to `lib/formPublicURL.ts` left the
 * suite 10 pass / 0 fail, while `const { error } = b;` red on the same site.
 * The walk now supplies the same buffer the parameter pass uses, and reports on
 * the line the `const` head sits on — it may also be the line above the `{`,
 * which `BINDING_HEAD`'s `\s*` spans.
 */
function bindingDestructuredReads(code: string): { reads: BindingRead[]; sites: number } {
  const reads: BindingRead[] = [];
  let sites = 0;
  for (const m of code.matchAll(BINDING_HEAD)) {
    const start = m.index ?? 0;
    const span = braceSpan(code, start + m[0].length - 1);
    if (!span) continue;
    sites++;
    if (!bindsErrorProperty(span.pattern)) continue;
    // The initializer's source identifier when it is a plain one (`= r`), skipping
    // an annotation the pattern may carry (`: Body = r`).
    const after = code.slice(span.close + 1);
    const src = /^\s*(?::[^=]*)?=\s*([A-Za-z_$][\w$]*)?/.exec(after);
    reads.push({ start, recv: src?.[1] ?? null, end: span.close + 1, pattern: span.pattern });
  }
  return { reads, sites };
}

/**
 * Every read of the failure body's prose field `error` in one piece of source,
 * in EVERY spelling the field can be read with (card t_d7552433):
 *
 *     x.error    x?.error                            (dot / optional chain)
 *     x["error"] x['error']  x?.["error"]            (bracket)
 *     const { error } = x    const { error: m } = x  (binding destructure)
 *
 * Ordered by position, so "the first read on this line" is the first one written.
 * An assignment to the field (`x["error"] = …`) matches too, exactly as the old
 * dot rule did — the ban is on touching the field, and a writer of it is a reader
 * of whatever it just put there.
 *
 * The two halves are split because they need DIFFERENT input. A dot or bracket read
 * is written inside one line; a binding's PATTERN may be broken over several, so
 * only a caller holding the file's whole buffer can claim it (card t_665b6539) —
 * the ban check calls `inlineFieldReads` per line and `bindingDestructuredReads`
 * over the buffer. This composed form is for callers that hand it text where line
 * locality is not the question: a setter's argument, and the shape table below.
 */
function proseFieldReads(code: string): ProseRead[] {
  return [...inlineFieldReads(code), ...bindingDestructuredReads(code).reads].sort((a, b) => a.end - b.end);
}

/** The LINE-LOCAL spellings of one read: `x.error` and `x["error"]`, in the shapes
 *  listed above. Both are written inside one line, so this is what a per-line walk
 *  may be handed. */
function inlineFieldReads(code: string): ProseRead[] {
  const out: ProseRead[] = [];
  for (const m of code.matchAll(DOT_READ)) {
    out.push({ start: m.index ?? 0, recv: m[1] ?? null, end: (m.index ?? 0) + m[0].length });
  }
  for (const m of code.matchAll(BRACKET_READ)) {
    out.push({ start: m.index ?? 0, recv: m[1] ?? null, end: (m.index ?? 0) + m[0].length });
  }
  return out.sort((a, b) => a.end - b.end);
}

// ---------------------------------------------------------------------------
// The DESTRUCTURED FUNCTION PARAMETER that binds the prose field (card
// t_69966a9a) — the spelling `proseFieldReads` above cannot hold.
//
// THE HOLE, MEASURED ON THE SHIPPED GUARD (tree 1bd37f45). Appending
//
//     export function _probeErrorMessage({ error }: { error?: unknown }): string {
//       return String(error ?? "");
//     }
//
// to `lib/formPublicURL.ts` left ALL THREE checks that reason about such a read
// green (10 pass / 0 fail), while the `const { error } = b` spelling of the same
// helper reded on the same site. The read is a bare identifier in the body, and
// nothing in `x.error` / `x["error"]` / `const { error } = x` matches the
// pattern that BOUND it.
//
// WHY IT IS SCANNED OVER THE FILE WHILE THE SPELLINGS ABOVE ARE SCANNED PER LINE.
// A parameter binding is not a read written inside one line; what makes the text
// `{ error }` a binding rather than an object literal is the context AROUND the
// `(` — the `function` keyword before it, or the `=>` after it — and either can
// sit on another line, because a parameter list broken over lines is the ordinary
// shape for a component with more than one prop:
//
//     export function PublishError({
//       error,
//     }: Props) {
//
// So the pattern is matched over the file joined back into one buffer, and
// reported on the line it starts on.
//
// WHY THIS IS A DECISION AND NOT A WIDENING. The pattern text is IDENTICAL in a
// parameter list and in an object literal ARGUMENT (`render({ error: msg })` — a
// WRITE). No shape match separates the two texts; only the context does, and
// exactly two contexts are unambiguous:
//   * `function …(` before the `(` — `function` cannot take an expression, so a
//     `{ … }` there is always a binding, never a literal. The `(?<![\w$.])` keeps
//     a method NAMED `function` (`obj.function({ error: msg })`) out of the rule.
//   * `=>` after the `)` — the paren group IS an arrow's parameter list; a
//     parenthesised object literal followed by `=>` is not a construct.
// A call site produces neither, so a correct call site cannot red — pinned as a
// control beside the shape table below.
//
// WHAT IT MEANS FOR THE OTHER TWO CHECKS. Neither can fire on this shape, and
// that is a property of the shape, not of the rule: the fallback rule asks what
// FOLLOWS a read (a parameter pattern is followed by `:`/`)`/`,`, never `||`),
// and the setter rule asks what a setter's ARGUMENT reads (a caller passing the
// whole body — `setErr(errorMessage(body))` — reads no field at all). The ban is
// therefore where this spelling bites, and the ban is what forces the decision
// the card is about: the prose may travel through apiErr and nowhere else.
//
// NOT claimed, each measured absent from src/ on 2026-10-17, so they are stated
// rather than guessed at:
//   * a METHOD's parameter (`class C { h({ error }: Body) {} }`) — its only
//     signal is the `{` after the `)`, which `switch (x) {` produces too;
//   * a `catch` clause's (`catch ({ error }) {}`) — a binding position, but not a
//     function's parameter, so outside this card's class;
//   * a parenthesised destructuring ASSIGNMENT (`({ error } = body)`) — the same
//     read as the `const` spelling, with no position signal of its own.
// Claimed, and deliberately so: a FUNCTION TYPE (`type H = ({ error }: Body) =>
// void`) matches the `=>` signal. A signature names the field without reading it;
// there are none in src/ today, and a legitimate one is an ALLOWED entry with its
// reason — the mechanism this file already uses for a call site that must keep a
// shape the rule bans.
// ---------------------------------------------------------------------------

/** One destructured parameter pattern that binds the prose field. */
type ParamRead = {
  /** Index of the `(` that opens the parameter list — where the report anchors. */
  start: number;
  /** Index just past the pattern's closing brace — the read's end. */
  end: number;
  /** The pattern itself, braces included (`{ error }`). */
  pattern: string;
  /** The whole parameter list, `(` to `)`, whitespace-normalised for a report. */
  span: string;
};

/** The `(` of a `function` declaration/expression (named, anonymous, generic or
 *  not, `async` or not) or of an arrow's parameter list — the two positions a
 *  destructured parameter can open in. */
const PARAM_HEAD = /(?<![\w$.])function\s*\*?\s*(?:[A-Za-z_$][\w$]*)?\s*(?:<[^<>]*>)?\s*$/;
/** A paren group is an arrow's parameter list when `=>` follows it. */
const ARROW_TAIL = /^\s*=>/;
/** A paren group that opens with a destructuring pattern. */
const PAREN_BRACE = /\(\s*\{/g;

function paramFieldBindings(code: string): { reads: ParamRead[]; sites: number } {
  const reads: ParamRead[] = [];
  let sites = 0;
  for (const m of code.matchAll(PAREN_BRACE)) {
    const open = m.index ?? 0;
    const pattern = braceSpan(code, open + m[0].length - 1);
    if (!pattern) continue;
    const close = matchingBracket(code, open);
    if (close === undefined || close < pattern.close) continue;
    if (!PARAM_HEAD.test(code.slice(0, open)) && !ARROW_TAIL.test(code.slice(close + 1))) continue;
    sites++;
    if (!bindsErrorProperty(pattern.pattern)) continue;
    // The value a parameter binds from is supplied by the CALLER, so no receiver
    // can be attributed to the read — null, which the walk below never reads as
    // "known to be localized".
    reads.push({
      start: open,
      end: pattern.close + 1,
      pattern: pattern.pattern,
      span: code.slice(open, close + 1).replace(/\s+/g, " ").trim(),
    });
  }
  return { reads, sites };
}

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
  //
  // THE READ IS FOUND IN EVERY SPELLING (card t_d7552433). This check used to
  // carry its OWN regex for the read, and it was the dot form only — so the
  // bracket spelling of the very same fallback,
  //
  //     setErr(b["error"] || t("x.fail"));
  //
  // walked past it. The read is now the shared `proseFieldReads()` and the
  // fallback is read off what FOLLOWS the read, so the two live in one place and
  // cannot drift: a new spelling of the read is a new spelling for every check in
  // this file at once.
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
      const read = proseFieldReads(stripped).find((r) => /^\s*(?:\|\||\?\?)/.test(stripped.slice(r.end)));
      if (!read) return;
      if (read.recv !== null && loadVars.has(read.recv)) return; // a runLoad outcome — already localized
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

// ---------------------------------------------------------------------------
// THE PER-FILE WALK behind the ban below, factored out (card t_665b6539).
//
// "The walk sees this spelling" is a claim about the WALK, not about a matcher. The
// shape table at the end of the check drives the matchers in memory, and it cannot
// notice a walk that hands them the wrong input — which is exactly the defect this
// card is about: `bindsErrorProperty` saw the property wherever it sat inside the
// pattern it was GIVEN, and the walk never gave it more than one line, so a
// multi-key pattern (the wrapped shape) was invisible while every
// single-line spelling red. So the loop body is one function here, and both the real
// tree and every fixture below go through it.
// ---------------------------------------------------------------------------

type FileWalk = {
  /** `rel:line: …` for every read of the prose field in this file. */
  offenders: string[];
  /** Reads examined, so an empty offender list cannot mean "the walk saw nothing". */
  examined: number;
  /** Destructured parameters classified as parameter positions, binding the field or
   *  not — the floor that says the whole-file parameter pass is looking at real code. */
  paramSites: number;
  /** `const`/`let`/`var` patterns classified as binding positions, the same way. */
  bindingSites: number;
};

/** The ban's walk over one file: `rel` is its path under src/ (report only), `text` its
 *  source as written. */
function fileProseOffenders(rel: string, text: string): FileWalk {
  // A runLoad outcome's `.error` is ALREADY localized (runLoad renders `message` or
  // the machine code — see lib/wsfetch.ts). Which names hold one is DERIVED from the
  // file, exactly as in the check above, so a page that renames `r` keeps its
  // exemption and no stale name list can widen it.
  const loadVars = new Set<string>();
  for (const m of text.matchAll(/(?:^|[;{}(\s])(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\s+runLoad\b/g)) {
    loadVars.add(m[1]);
  }
  const lines = linesOf(text);
  const offenders: string[] = [];
  let examined = 0;

  // (1) THE LINE-LOCAL SPELLINGS. String/template CONTENTS are blanked (the shapes
  // take blanked input), so a sentence that merely mentions `d.error` —
  // documentation, a comment, an error-message string — is not a read.
  for (const { n, text: lineText, code: stripped } of lines) {
    if (isCommentLine(lineText)) continue;
    const reads = inlineFieldReads(stripped);
    if (reads.length === 0) continue;
    // The line is exempt only when EVERY read on it is a runLoad outcome, which is
    // the property the exemption is for. It used to be ANY read: the loop returned
    // on the first one whose name was a loadVar, so `setErr(r.error || d.error)` hid
    // the second (raw-prose) read behind the first — the same masking this check
    // exists to remove, and no live line mixes the two today, so the tightening
    // changes nothing but the reach.
    if (reads.every((r) => r.recv !== null && loadVars.has(r.recv))) continue;
    examined++;
    if (ALLOWED.some((a) => lineText.includes(a))) continue;
    offenders.push(`${rel}:${n}: ${lineText.trim()}`);
  }

  const buf = joinLines(lines);

  /** The RAW source from one buffer offset to another: the lines the match sits on,
   *  as the author wrote them. An ALLOWED entry is a raw snippet (both of today's
   *  contain a string literal, which blanking erases), so an exception has to be
   *  looked for in the text that was written, not in the blanked buffer. */
  const rawSpan = (from: number, to: number): string =>
    lines
      .slice(buf.lineAt(from), buf.lineAt(Math.max(from, to)) + 1)
      .map((l) => l.text)
      .join("\n");

  // (2) THE PARAMETER SPELLING (card t_69966a9a), which cannot be seen line by line
  // — the context that makes `{ error }` a binding (a `function` head, or a `=>`
  // tail) may sit on another line. See `paramFieldBindings` for why this spelling
  // needs a whole-file pass of its own, and `matchingBracket` for the span match
  // both whole-file passes share.
  const params = paramFieldBindings(buf.code);
  for (const r of params.reads) {
    const line = lines[buf.lineAt(r.start)];
    if (isCommentLine(line.text)) continue;
    examined++;
    // The pattern is tested beside the line: a parameter list broken over several
    // lines has no single line that spells the exception in full.
    if (ALLOWED.some((a) => r.pattern.includes(a) || line.text.includes(a))) continue;
    // The line as it was written, plus the whole parameter list when the line does
    // not contain it (the multi-line spelling — the pattern is otherwise invisible
    // in the report).
    offenders.push(`${rel}:${line.n}: ${line.text.trim()}${line.text.includes(r.pattern) ? "" : ` ${r.span}`}`);
  }

  // (3) THE BINDING SPELLING (card t_665b6539), over the same buffer for the same
  // reason: the pattern may be broken over lines, and then no single line holds it.
  // Reported on the line the `const`/`let`/`var` head sits on, which is not
  // necessarily the line the `{` is on.
  const bindings = bindingDestructuredReads(buf.code);
  for (const r of bindings.reads) {
    const line = lines[buf.lineAt(r.start)];
    if (isCommentLine(line.text)) continue;
    // The runLoad-outcome exemption, applied to THIS read's receiver rather than to
    // "every read on the line": a binding read is one read of one pattern. Nothing
    // is lost by the difference — a line that mixes a loadVar read with a raw one is
    // still caught by pass (1), which reads that line.
    if (r.recv !== null && loadVars.has(r.recv)) continue;
    examined++;
    // THE EXEMPTION, FOR A MATCH THAT MAY SPAN LINES. The parameter pass answers the
    // same question by testing the pattern AND the line, because a list broken over
    // lines has no single line that spells an exception in full. This pass tests the
    // line AND the raw statement the match sits on — not `r.pattern`, which is
    // BLANKED code (`t("set.autoCnameFailed", …)` reaches it as `t("", …)`), while
    // every ALLOWED entry is a raw snippet with a string literal in it. An exception
    // is therefore looked for in the source as written, on whichever line it is.
    const statement = rawSpan(r.start, r.end - 1).replace(/\s+/g, " ").trim();
    if (ALLOWED.some((a) => line.text.includes(a) || statement.includes(a))) continue;
    // The line as written when it holds the pattern; otherwise the whole statement,
    // whitespace-normalised — a multi-line pattern is invisible in a report that
    // quotes only its opening line (the same reason the parameter pass appends its
    // span).
    offenders.push(`${rel}:${line.n}: ${line.text.includes(r.pattern) ? line.text.trim() : statement}`);
  }

  return { offenders, examined, paramSites: params.sites, bindingSites: bindings.sites };
}

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
  // So this check does not look at the CALL SITE's syntax — no `||`, no setter
  // name, no reachability through an alias. It looks at WHO MAY TOUCH THE PROSE
  // FIELD. Reading `.error` is apiErr()'s job and nobody else's, and the ban is
  // derived — any new reader fails the build — so the next module that wants to
  // "just parse the body into a message field" cannot reintroduce the class. The
  // exemption list is asserted to be exactly one entry: a growing list is how a
  // rule like this rots.
  //
  // IT IS STILL A SHAPE MATCH, AND THE SHAPES ARE THE WHOLE REACH (card
  // t_d7552433). This comment used to claim the check "does not look at the syntax
  // at all", which was false: it carried its own `const READ = /\.\s*error\b/`,
  // dot only. Measured on that build with a reader appended to
  // `lib/formPublicURL.ts`: `const e = b["error"]` and
  // `const { error } = (body ?? {}) as { error?: unknown }` both stayed GREEN
  // (9 pass / 0 fail, reader undetected) while the dot control at the same site
  // was RED naming `lib/formPublicURL.ts:62`. A bracket read is the natural
  // spelling once the body is typed `Record<string, unknown>`, so the class was
  // reachable again with the guard green. The read is now `proseFieldReads()`,
  // shared with the two neighbouring checks, which catches the dot, the
  // optional-chain, the bracket and the binding-destructure spelling of the SAME
  // read — so the claim "any new reader fails the build" is now true of every
  // spelling the field can be read with, not of one of them.
  //
  // AND THE SHAPES ARE ONLY AS GOOD AS WHAT THE WALK HANDS THEM (card t_665b6539).
  // The sentence above was still not the whole truth, for a reason no in-memory
  // shape table can show: the walk fed the shapes ONE LINE AT A TIME, so a spelling
  // whose shape SPANS LINES was invisible however well it was matched. Measured on
  // the guard t_69966a9a shipped, with the reader appended to
  // `lib/formPublicURL.ts`:
  //
  //     const {          <- GREEN 10 pass / 0 fail (reader undetected)
  //       error,
  //     } = b;
  //
  // while `const { error } = b;` red at the same site, naming
  // `lib/formPublicURL.ts:62`. That is the shape a MULTI-KEY destructure of a
  // response body takes once it is wrapped instead of fitting on one line, and it
  // was the last silent reach hole of the class t_10453dd9 opened. (src/ carries 69
  // one-line `const { … } = …` patterns and 0 wrapped ones today, so this is a
  // FUTURE hole in this tree — closed the way t_b6cd0f72 closed the multi-line
  // setter.) The spellings that can span lines (a destructured parameter, a
  // `const`/`let`/`var` pattern) are now matched over the file joined into one
  // buffer, reported on the line the match STARTS on, and the walk itself lives in
  // `fileProseOffenders` so a fixture is exercised by the walk and not only by the
  // matcher.
  assert.deepEqual(
    [...PROSE_READERS_ALLOWED],
    ["lib/apiErr.ts"],
    "the exemption list grew — every entry is a file that may put the API's prose in a `message`",
  );
  const offenders: string[] = [];
  let examined = 0;
  /** Destructured parameters the whole-file scan classified as parameter
   *  positions, whether or not they bind the field. A floor, so "the parameter
   *  spelling is claimed" cannot be true only of the shape table in memory. */
  let paramSites = 0;
  /** The same floor for the binding spelling (card t_665b6539): `const { … } = …` is
   *  ordinary in src/, so a whole-file pass that classified none of them is not
   *  looking at real code and its silence about a multi-key pattern means nothing. */
  let bindingSites = 0;
  for (const p of sources) {
    // NOTE: this walk's `rel` carries a leading "/" (SRC resolves to the src
    // directory itself), so it is normalised here rather than compared raw.
    const rel = p.slice(SRC.length).replace(/^\/+/, "");
    if (rel.endsWith(".test.ts")) continue;
    if (PROSE_READERS_ALLOWED.has(rel)) continue;
    // The whole walk for one file — the three passes, the two exemptions and what
    // each of them counted — is `fileProseOffenders`, so the fixtures below are held
    // to the same instrument the real tree is.
    const walk = fileProseOffenders(rel, readFileSync(p, "utf8"));
    offenders.push(...walk.offenders);
    examined += walk.examined;
    paramSites += walk.paramSites;
    bindingSites += walk.bindingSites;
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
  // The same floor for the parameter spelling: the whole-tree pass must have
  // classified at least one real destructured parameter (components/docsChrome.tsx
  // carries `export function T({ k }: { k: Key })`, which binds no `error` — the
  // shape is present in the tree even when it is not an offender).
  assert.ok(
    paramSites > 0,
    "the whole-file pass classified no destructured parameter anywhere in src/ — it is " +
      "not looking at real code, so a parameter binding this field would go unseen",
  );
  // And the same floor for the binding spelling this card widened to a whole-file scan.
  assert.ok(
    bindingSites > 20,
    `the whole-file pass classified ${bindingSites} \`const { … } = …\` patterns in src/ — ` +
      "a multi-key pattern binding this field would go unseen, so the pass is not looking " +
      "at the code it claims to",
  );

  // THE SHAPE TABLE ITSELF, pinned in memory (card t_d7552433). The walk above only
  // ever exercises the spellings that exist in src/ TODAY — which is precisely how a
  // dot-only rule looked correct — so the reach is asserted directly against the
  // shipped helpers: every spelling of a read must be seen, and prose that merely
  // MENTIONS the field must not. This is the assertion that fails if a future edit
  // drops a shape, or drops the key-preserving blanking `b["error"]` needs to survive
  // it, instead of the rule quietly shrinking back to one spelling.
  const readsIn = (line: string) => proseFieldReads(unblankingKey(line));
  for (const line of [
    "const e = b.error;",
    "const e = b?.error;",
    "const e = (body as { error?: unknown } | null)?.error;",
    `const e = b["error"];`,
    `const e = b['error'];`,
    `const e = b?.["error"];`,
    `const e = b[ "error" ];`,
    "const { error } = b;",
    "const { error: m } = b;",
    "const { a: { error } } = b;",
    `const { error = "" } = b;`,
    // THE PATTERN BROKEN OVER LINES (card t_665b6539) — how a multi-key pattern is
    // written once it is wrapped instead of fitting on one line, which is the shape
    // a multi-key body destructure takes.
    "const {\n  error,\n} = b;",
    "const {\n  code,\n  error,\n} = b;",
    "const\n  {\n    error,\n  } = b;",
    "const {\n  a: {\n    error,\n  },\n} = b;",
  ]) {
    assert.equal(
      readsIn(line).length,
      1,
      `a read of the prose field was not seen in this spelling (t_d7552433): ${line}`,
    );
  }
  for (const line of [
    `const s = 'never read b["error"] here';`,
    `const s = "x.error";`,
    "const { errorKey } = b;",
    `const s = "error";`,
    // The multi-line negatives the whole-file binding pass needs: a pattern that does
    // not bind the field, and an object LITERAL initializer (a WRITE), which
    // `BINDING_HEAD` cannot reach because there is more than `{` after the `const`.
    "const {\n  errorKey,\n} = b;",
    "const out = {\n  error: msg,\n};",
  ]) {
    assert.equal(
      readsIn(line).length,
      0,
      `prose that only MENTIONS the field was counted as a read: ${line}`,
    );
  }
  // A comment quoting the field is skipped by the walk ITSELF (`isCommentLine`, before
  // any shape runs), so it is asserted through that predicate rather than the shapes.
  assert.ok(
    isCommentLine(`// the prose field, e.g. b["error"], is nobody else's to read`),
    "a comment quoting the field must be skipped by the walk, not counted as a read",
  );

  // THE PARAMETER SPELLING TOO (card t_69966a9a), pinned in memory in BOTH
  // directions. The walk above only exercises the parameters the tree HAS (one
  // today, in components/docsChrome.tsx, binding no `error`), so the shape this
  // card is about is asserted here: the helper the card measured must be seen, and
  // the object literal a CALL SITE passes must not — the two are the same text,
  // separated only by the context around the `(`.
  const paramReadsIn = (src: string) => paramFieldBindings(unblankingKey(src));
  for (const src of [
    // The exact helper the card measured green on the shipped guard, verbatim.
    `export function _probeErrorMessage({ error }: { error?: unknown }): string {\n  return String(error ?? "");\n}`,
    "function f({ error }: Body) { return String(error); }",
    "export default function PublishError({ error }: Props) {",
    "const h = ({ error }: Body) => String(error);",
    "const h = async ({ error }: Body) => String(error);",
    "function f<T>({ error }: Body) {",
    "function f ({ error }: Body) {",
    `function f({ error = "" }: Body) {`,
    "function f({ error: m }: Body) {",
    "function f({ a: { error } }: Body) {",
    // The multi-line spelling — the reason this pass is whole-file.
    "export function PublishError({\n  error,\n}: Props) {",
  ]) {
    assert.equal(
      paramReadsIn(src).reads.length,
      1,
      `the prose field bound by a parameter was not seen (t_69966a9a): ${src}`,
    );
  }
  for (const src of [
    // The card's control: the SAME pattern text in argument position is a WRITE.
    "render({ error: msg });",
    "setErr({ error: msg });",
    `setErr(t("x.fail", { error: msg }));`,
    "const o = pick({ error: msg });",
    // A method NAMED `function` is a call site, not a parameter list.
    "obj.function({ error: msg });",
    "function f({ errorKey }: Body) {",
    // The ANNOTATION mentions the field; the pattern does not bind it.
    "function f({ ok }: { error?: unknown }) {",
    // A string that spells the shape is blanked before any shape runs.
    `const s = "function f({ error }: Body)";`,
  ]) {
    assert.equal(
      paramReadsIn(src).reads.length,
      0,
      `a write or a mere mention was counted as a parameter read (t_69966a9a): ${src}`,
    );
  }
  // The scan must be classifying POSITIONS, not just matching a regex: the arrow
  // tail alone must not turn a paren group into a parameter list (this is the
  // control above, asserted through the counter that the walk's floor uses).
  assert.equal(
    paramReadsIn("function f({ k }: { k: Key }) {").sites,
    1,
    "a real destructured parameter was not classified as a parameter position",
  );
  assert.equal(
    paramReadsIn("render({ error: msg });").sites,
    0,
    "a call site was classified as a parameter position — the rule could red a correct call",
  );

  // BOTH MULTI-LINE SHAPES, THROUGH THE WALK (card t_665b6539). Everything above drives
  // MATCHERS in memory, and a matcher is not the same instrument as the walk: this
  // card's whole defect was a walk that handed the matcher one line, which no
  // in-memory fixture could have shown. So the multi-line parameter this file claimed
  // in t_69966a9a and the multi-line `const` pattern this card claims are both driven
  // through `fileProseOffenders` — the same function the real tree goes through above
  // — and each one has a control that must come back empty.
  const walked = (text: string) => fileProseOffenders("lib/__fixture.ts", text);

  // The card's probe, verbatim. The report anchors on the `const` head (line 2), which
  // is the line a reader can open, not on the pattern's closing brace, and it quotes
  // the whole statement because the pattern is not on that line.
  assert.deepEqual(
    walked(
      [
        "export function _probeConstMulti(b: { error?: unknown }): string {",
        "  const {",
        "    error,",
        "  } = b;",
        '  return String(error ?? "");',
        "}",
      ].join("\n"),
    ).offenders,
    ["lib/__fixture.ts:2: const { error, } = b;"],
    "a `const` pattern broken over lines was not seen BY THE WALK (t_665b6539)",
  );
  // The head may also sit on the line ABOVE its `{` (`BINDING_HEAD`'s `\s*` spans it);
  // the report is still the head's line, because that is where the read begins.
  assert.deepEqual(
    walked(
      [
        "export function f(b: unknown): string {",
        "  const",
        "    {",
        "      error,",
        "    } = b;",
        '  return String(error ?? "");',
        "}",
      ].join("\n"),
    ).offenders,
    ["lib/__fixture.ts:2: const { error, } = b;"],
    "a `const` head on the line above its pattern was not seen by the walk (t_665b6539)",
  );
  // And the one-line spelling of the SAME read still fires through this instrument, so
  // the whole-file pass did not replace the reach it used to have with a narrower one.
  assert.deepEqual(
    walked(
      [
        "export function f(b: { error?: unknown }): string {",
        "  const { error } = b;",
        '  return String(error ?? "");',
        "}",
      ].join("\n"),
    ).offenders,
    ["lib/__fixture.ts:2: const { error } = b;"],
    "the one-line `const` spelling stopped firing when the walk became whole-file",
  );
  // The parameter reach t_69966a9a added, now asserted by the WALK rather than only by
  // the shape table: a parameter list broken over lines, the way a component with
  // several props is wrapped.
  const walkedParam = walked(
    ["export function PublishError({", "  error,", "}: Props) {", '  return String(error ?? "");', "}"].join("\n"),
  );
  assert.equal(
    walkedParam.offenders.length,
    1,
    `a destructured parameter broken over lines was not seen BY THE WALK (t_69966a9a): ${JSON.stringify(walkedParam.offenders)}`,
  );
  assert.match(
    walkedParam.offenders[0],
    /^lib\/__fixture\.ts:1: export function PublishError\(/,
    "the parameter report is not anchored on the line the parameter list opens on",
  );
  assert.ok(
    walkedParam.offenders[0].includes("{ error,"),
    `the parameter report does not name the pattern: ${walkedParam.offenders[0]}`,
  );
  // The controls, through the same walk: the SAME pattern text in argument position is
  // a WRITE, and a destructured runLoad outcome is already localized.
  assert.deepEqual(
    walked(
      ["export function f(msg: string): string {", "  return String(render({", "    error: msg,", "  }));", "}"].join(
        "\n",
      ),
    ).offenders,
    [],
    "a call site passing an object literal was reported by the walk — the rule would red a correct call",
  );
  assert.deepEqual(
    walked(
      [
        "export async function f(): Promise<string> {",
        '  const r = await runLoad("/x");',
        "  const {",
        "    error,",
        "  } = r;",
        '  return String(error ?? "");',
        "}",
      ].join("\n"),
    ).offenders,
    [],
    "a destructured runLoad outcome was reported — the derived exemption does not reach the whole-file binding pass",
  );

  // A read taken off a COMPOUND expression has no attributable name; it must still be
  // a read, and its null receiver must never be read as "known to be localized".
  const compound = readsIn(`const e = (b ?? {})["error"];`);
  assert.equal(compound.length, 1, "a bracket read on a compound expression was not seen");
  assert.equal(
    compound[0].recv,
    null,
    "an unattributable read must carry a null receiver, not a guessed one",
  );

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
   *  `error` prose of one that is not a localized runLoad outcome?
   *
   *  The prose read is the SHARED `proseFieldReads()`, in every spelling (card
   *  t_d7552433). This check used to carry its own dot-only regex, so
   *  `setUploadMsg(b["error"])` — the natural spelling for a body typed
   *  `Record<string, unknown>` — was invisible to it while the dot spelling of the
   *  same call RED'd. */
  const readsRawBody = (arg: string, loadVars: Set<string>): boolean => {
    if (/\.\s*(?:text|json)\s*\(\s*\)/.test(arg)) return true;
    for (const r of proseFieldReads(arg)) {
      if (r.recv === null || !loadVars.has(r.recv)) return true;
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
    // is why it is closed here rather than by reformatting call sites. The
    // buffer-and-lookup is `joinLines`, shared with the parameter pass in the check
    // above so the two agree on where a match starts.
    const { code: joined, lineAt } = joinLines(lines);
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
  const body = { error: "workspace not found", code: "workspace.notFound" };
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
      !rendered.includes("workspace not found"),
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
  const unknown = { error: "workspace not found", code: "future.code.notInThisBuild" };
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

