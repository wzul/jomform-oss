// Regression guard for the /connect double-POST (follow-up to issue #6177).
//
// THE DEFECT. The /connect page's token effect was written with the translator
// `t` in its dependency array:
//
//     }, [token, router, t]);
//
// `t` is a useCallback in components/i18n.tsx whose identity changes when the
// PROFILE language resolves (the module fetches /api/auth/profile and calls
// setLangState, which rebuilds `t` over `[lang]`). So for every user whose
// dashboard language is not the provider's initial "en" — ms, zh, ta, ar — the
// effect ran a second time and POSTed the emailed connect token to
// /v1/auth/social/connect TWICE per page load. Measured server-side at a
// request-counting stub on that pre-fix shape: 1 POST at lang=en, 2 POSTs at
// lang=ms, on the failure AND the success path. The shipped shape — no `t` in
// the effect's deps, the sentence derived at render — issues exactly 1 POST at
// every locale, on both paths.
//
// It is benign today by luck of two other facts — dbstore.UserStore.LinkIdentity
// is `INSERT … ON CONFLICT DO UPDATE`, and the token is stateless with a 24h TTL
// and no nonce store — which is exactly why it must be pinned in the source and
// not merely observed once: a single-use token, or a LinkIdentity that stops
// being idempotent, turns this into a hard failure with no gate in between.
//
// WHAT THIS GUARD PINS
//   1. The /connect token effect's dependency array does not contain a
//      translator/message-function identifier — the shape of the bug.
//   2. The fix is PRESENT, not merely absent — and it is the RENDER-TIME
//      derivation, not a ref. The token effect stores the parsed failure body
//      RAW (setFailure({ body }) in page.tsx) and the sentence is derived at
//      render: `const error = failure ? apiErr(failure.body, t("connect.fail"), t) : …`.
//      The sentence is therefore a pure function of (body, lang), and the effect
//      depends on nothing whose identity changes when the profile language
//      resolves.
//
//      A REF HOLDING THE TRANSLATOR (useRef(t) + a sync effect +
//      translator.current("connect.fail") inside the token effect) was built,
//      driven, and REJECTED — do not reintroduce it believing it is the pinned
//      shape. It fails OPEN, and how often depends on WHEN the sentence is
//      captured relative to the async profile fetch: the profile language arrives
//      from /api/auth/profile, so a failure that lands before that response
//      freezes the provider's initial English. That is a RACE, not a certainty,
//      and its rate is a property of the timing, not of the shape being safe:
//      rebuilt and driven on a dev build with 6 fresh samples at lang=ms (always
//      exactly 1 POST), with the profile answering ~900ms late and the token POST
//      failing immediately, ALL 6 rendered the English sentence while
//      htmlLang=ms. (The card that removed the shape from the page measured 1 of
//      6 on a production build — the same defect at a different timing, which is
//      the point: do not quote a single rate for it.) The render-time derivation
//      has no such window: same harness, 0 of 6 samples wrong, 1 POST, because
//      the body is stored untranslated and the sentence is rebuilt whenever the
//      language changes.
//   3. The /connect page still asks for its own sentences (connect.fail /
//      connect.resendFail) — a "fix" that hardcodes English or drops the
//      translation is not a fix.
//
// Scope is deliberately narrow: the source-level rule is limited to /connect's
// token effect, because THAT effect performs a one-shot server POST of a
// bearer-like capability. A re-run of a GET-and-render effect is not the same
// defect, and three other effects in the repo keep `t` in their deps on purpose,
// so their own re-translation follows a language switch (measured on this tree:
// 95 effects across 66 .tsx files, 3 of them depending on `t`, none performing a
// write). A page-wide ban would be a behaviour change, not a guard.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, statSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const SRC = fileURLToPath(new URL("..", import.meta.url));
const CONNECT = `${SRC}app/connect/page.tsx`;
const I18N = `${SRC}components/i18n.tsx`;

/** Comments and string/template literals blanked, so an identifier match cannot
 *  come from inside a comment or a sentence (the file's own prose talks about
 *  `t` and about `[token, router, t]`). */
const stripStrings = (l: string): string =>
  l.replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g, '""');

type Line = { n: number; text: string; code: string };
const lines = (p: string): Line[] =>
  readFileSync(p, "utf8")
    .split("\n")
    .map((text, i) => ({ n: i + 1, text, code: stripStrings(text) }));

const isComment = (l: string): boolean => /^\s*(\/\/|\*|\/\*)/.test(l);
const codeLines = (p: string): Line[] => lines(p).filter((l) => !isComment(l.text));

/** Index of the closing bracket matching the opener at `open` (a `(`, `[` or
 *  `{`), across the file, ignoring comments and literals. */
function matchBracket(src: Line[], open: number): number {
  const pair: Record<string, string> = { "(": ")", "[": "]", "{": "}" };
  const close = pair[src[open].code.replace(/^[^([{]*/, "").charAt(0)];
  let depth = 0;
  for (let i = open; i < src.length; i++) {
    for (const ch of src[i].code) {
      if (ch === "(" || ch === "[" || ch === "{") depth++;
      else if (ch === ")" || ch === "]" || ch === "}") {
        depth--;
        if (depth === 0) {
          assert.equal(
            ch,
            close,
            `${src[open].n}:${src[open].text.trim()} — unbalanced brackets while reading an effect`,
          );
          return i;
        }
      }
    }
  }
  throw new Error(`${src[open].n}: unterminated effect body`);
}

/** Every `useEffect(…)` in a file: the line of `useEffect(`, its body as
 *  comment-free RAW text (so a URL in a string literal is still visible) and as
 *  string-stripped code (so brackets inside a sentence cannot confuse the
 *  bracket matcher), plus the text of the final dependency array. */
function effects(src: Line[]) {
  const out: { at: number; raw: string; body: string; deps: string; depsLine: number }[] = [];
  for (const l of src) {
    const m = /useEffect\s*\(/.exec(l.code);
    if (!m) continue;
    const start = src.findIndex((x) => x.n === l.n);
    const end = matchBracket(src, start);
    const span = src.slice(start, end + 1);
    const body = span.map((x) => x.code).join("\n");
    const raw = span.map((x) => x.text).join("\n");
    // The dependency array is the last bracket group before the closing paren.
    const deps: string[] = [];
    let depsLine = 0;
    for (let i = start; i <= end; i++) {
      const mm = /\}\s*,\s*\[([^\]]*)\]\s*\)/.exec(src[i].code);
      if (mm) {
        deps.push(mm[1]);
        depsLine = src[i].n;
      }
    }
    out.push({ at: l.n, raw, body, deps: deps[deps.length - 1] ?? "", depsLine });
  }
  return out;
}

test("the guard sees the real /connect effect (it cannot pass vacuously)", () => {
  const src = codeLines(CONNECT);
  const found = effects(src);
  assert.ok(found.length >= 1, "no useEffect found in app/connect/page.tsx at all");
  const tokenEffect = found.find((e) => /\/api\/auth\/social\/connect\b/.test(e.raw));
  assert.ok(
    tokenEffect,
    "the effect that POSTs the connect token is not in the walk — the guard is looking at the wrong thing",
  );
  assert.ok(
    tokenEffect.deps.includes("token"),
    `the token effect's deps (line ${tokenEffect.depsLine}) no longer mention the token: "${tokenEffect.deps}"`,
  );
});

test("the /connect token effect does not depend on the translator's identity", () => {
  // A translator/message function is what makes this effect re-run when the
  // profile language resolves. `t` is the repo's spelling of it; the identifier
  // set is derived from the actual hook binding (`const { t } = useI18n()`), not
  // hardcoded, so a rename cannot silently step around the rule.
  const connect = codeLines(CONNECT);
  const bind = /const\s*\{\s*([^}]*)\}\s*=\s*useI18n\s*\(/.exec(connect.map((l) => l.code).join("\n"));
  assert.ok(bind, "app/connect/page.tsx no longer destructures useI18n()");
  const translatorNames = bind[1]
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s === "t" || s.startsWith("t:") || s === "translate");
  assert.ok(
    translatorNames.length > 0,
    `expected a translator binding from useI18n(), got {${bind[1]}}`,
  );
  const names = translatorNames.map((s) => s.split(":")[0].trim());

  const offenders: string[] = [];
  for (const e of effects(connect)) {
    if (!/\/api\/auth\/social\/connect\b/.test(e.raw)) continue;
    const deps = e.deps.split(",").map((s) => s.trim());
    for (const n of names) {
      if (deps.includes(n)) {
        offenders.push(
          `app/connect/page.tsx:${e.depsLine}: the token effect depends on \`${n}\` — ` +
            `its identity changes when the profile language resolves, so the token POST ` +
            `happens twice for every user whose language is not the default.`,
        );
      }
      // …and it must not CALL it either. A translator call inside the effect
      // freezes the sentence at whatever language the provider held when the
      // request failed — which is "en" for a ms/zh/ta/ar user (React runs child
      // effects before the provider's). The sentence belongs at render time.
      const call = new RegExp(`(?<![\\w$.])${n}\\s*\\(`);
      if (call.test(e.body)) {
        offenders.push(
          `app/connect/page.tsx:${e.at}: the token effect calls \`${n}(…)\` directly. ` +
            `Keep the failure BODY in state and derive the sentence at render, or the ` +
            `sentence is frozen in the provider's initial language.`,
        );
      }
    }
  }
  assert.deepEqual(offenders, [], offenders.join("\n"));

  // The fix must be PRESENT, not merely absent: the failure sentence is derived
  // at render from the body the effect stored.
  const src = readFileSync(CONNECT, "utf8");
  assert.match(
    src,
    /const\s+error\s*=\s*\w+\s*\?\s*apiErr\(\s*\w+\.body,\s*t\("connect\.fail"\),\s*t\)/,
    "the /connect page no longer derives its failure sentence from the stored body at render",
  );
  assert.match(
    src,
    /setFailure\(\s*\{\s*body[\s,}]/,
    "the token effect no longer stores the raw failure body for the render-time sentence",
  );
});

test("the /connect page still asks for its own sentences", () => {
  const src = readFileSync(CONNECT, "utf8");
  assert.match(src, /connect\.fail/, "the token failure sentence key is gone");
  assert.match(src, /connect\.resendFail/, "the resend failure sentence key is gone");
});

test("the translator's identity really does change with the language", () => {
  // The premise of the whole guard. If `t` ever became a module-level constant
  // the source rule above would be pinning nothing — and the double POST would
  // be gone for a different reason, which the guard would then be misreporting
  // as still-protected. Assert the shape it depends on, in the module that
  // builds it.
  const i18nLines = codeLines(I18N);
  const at = i18nLines.findIndex((l) => /const\s+t\s*=\s*useCallback\s*\(/.test(l.code));
  assert.ok(
    at >= 0,
    "components/i18n.tsx no longer builds `t` with useCallback — re-derive this guard",
  );
  const tail = i18nLines.slice(at).map((l) => l.code).join("\n");
  const dep = /useCallback\s*\([\s\S]*?\}\s*,\s*\[([^\]]*)\]\s*,?\s*\)/.exec(tail);
  assert.ok(dep, "could not read the useCallback dependency array for `t`");
  assert.match(
    dep[1],
    /\blang\b/,
    `t's identity is no longer derived from the language (deps: [${dep[1]}]); this guard's premise is stale`,
  );
});

test("only /connect's one-shot token POST is under this rule (no page-wide ban)", () => {
  // Stated explicitly and checked, so a future reader cannot mistake this guard
  // for "no effect may depend on t". Other pages keep `t` in their deps on
  // purpose: their GET-and-render effects must re-translate on a language
  // switch. A page-wide rule would change all of them.
  const walk = (dir: string, out: string[] = []): string[] => {
    for (const name of readdirSync(dir)) {
      const p = `${dir}/${name}`;
      if (statSync(p).isDirectory()) walk(p, out);
      else if (name.endsWith(".tsx")) out.push(p);
    }
    return out;
  };
  const files = walk(SRC);
  assert.ok(files.length > 40, `expected to walk the dashboard, got ${files.length} .tsx files`);
  const depsOnT = (e: { deps: string }) => /(^|,)\s*t\s*(,|$)/.test(e.deps);
  const dependOnT = files.filter((p) => effects(codeLines(p)).some(depsOnT));
  // Measured on this tree: 3 files (app/builder:420,
  // app/products/ProductEditor:177, app/settings:285) — all GET-and-render, none
  // performing a write. The floor is low on purpose: it is here to catch the walk
  // going blind (0 files), not to pin a census that a legitimate refactor may
  // move.
  assert.ok(
    dependOnT.length >= 3,
    `expected the other pages to keep their \`t\` dependency (saw ${dependOnT.length}); ` +
      `if that changed, this guard's scope statement is stale`,
  );

  // /connect must have NO effect at all depending on `t` now: the token effect
  // stores the body and the sentence is derived at render, so nothing in this
  // page needs to re-run on a language change.
  const connectDeps = effects(codeLines(CONNECT)).filter(depsOnT);
  assert.deepEqual(
    connectDeps.map((e) => `app/connect/page.tsx:${e.depsLine}: [${e.deps}]`),
    [],
    "an effect on the /connect page depends on `t` again",
  );
});
