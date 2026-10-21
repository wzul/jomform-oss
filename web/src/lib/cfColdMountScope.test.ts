// Fast structural net for card t_a7e3c4d4: the Settings → Domains Cloudflare
// panel must not issue a status read, nor paint an OAuth href, before the
// session has published the active workspace.
//
// THE DEFECT
//   The panel is the one section whose every route is ACCOUNT-scoped
//   (`cfStatus` reads `actx.Account`'s sealed token, `cfDisconnect` clears it),
//   and an UNSCOPED session call resolves to the caller's FIRST-OWNED account.
//   #1350 added `?workspace=` to every call — but on a COLD mount (no
//   `jf_workspace` in localStorage) `workspaceQuery()` still answers "" until the
//   session publishes a workspace (~500ms on prod), and the read fired anyway.
//   Measured on prod v1.21.261:
//
//       t=186ms  GET /api/cf/status                   -> 200  <-- BARE
//       t=546ms  GET /api/cf/status?workspace=wan     -> 200
//
//   The bare answer resolved to the first-owned account, landed BEFORE the scoped
//   one, and so was APPLIED (`cfStatusSeq` had nothing newer to drop it for). The
//   same window left a bare `/api/cf/start` href paintable whenever the
//   first-owned account was unlinked — one click from an OAuth flow whose
//   callback seals against that account.
//
// WHY THIS FILE EXISTS ALONGSIDE THE BROWSER SPEC
//   The behavioural proof is `web/e2e/cf-status-workspace-scope.spec.ts`'s
//   cold-mount arm, which records the real component's requests and every href
//   it paints. That spec needs a production build; this file runs in the `web`
//   gate's unit half and pins the SHAPE the gate has to keep — the failure mode
//   is a one-line deletion (`if (!workspace) return;`) plus a one-token revert of
//   the state initializer, and nothing else in the fast suite would notice.
//
// WHAT IT PINS
//   1. `loadCfStatus` returns BEFORE it can build a URL while `workspace` is "".
//   2. That URL is built from `workspace` — the value the gate checked — not
//      from `workspaceQuery()`, which reads the module-level scope and can
//      disagree with the render.
//   3. `cfStartHref` starts EMPTY: a bare `/api/cf/start` initial value is
//      paintable in the pre-scope window (the write arm of the defect).
//   4. The anchor is gated on a non-empty href as well as the answered flag.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const settings = read("../app/settings/page.tsx");

test("the guard is looking at the real settings page (it cannot pass vacuously)", () => {
  assert.ok(
    settings.includes("const loadCfStatus = useCallback("),
    "app/settings/page.tsx no longer declares loadCfStatus — the guard's anchor moved",
  );
  assert.ok(
    settings.includes("const [cfStartHref, setCfStartHref] = useState("),
    "app/settings/page.tsx no longer holds the Cloudflare start href in state — the guard's anchor moved",
  );
});

/** The body of loadCfStatus, delimited by its own `}, […]` dependency array. */
function loadCfStatusBody(): string {
  const start = settings.indexOf("const loadCfStatus = useCallback(");
  assert.ok(start >= 0, "loadCfStatus is gone");
  const tail = /\n {2}\}, \[[^\]]*\]\);/.exec(settings.slice(start));
  assert.ok(tail, "could not delimit loadCfStatus's body (no `}, […]` dependency array)");
  return settings.slice(start, start + tail.index + tail[0].length);
}

/**
 * Drop whole-line `//` comments. The checks below are about CODE, and this
 * function's own comments explain the fix by naming `workspaceQuery()` — a naive
 * substring test would fail on the explanation instead of the implementation.
 * Only whole-line comments are stripped (a `//` inside a string literal, e.g. in
 * a URL, must survive).
 */
function stripLineComments(src: string): string {
  return src
    .split("\n")
    .filter((line) => !line.trimStart().startsWith("//"))
    .join("\n");
}

test("the status read is gated on a KNOWN workspace, above the fetch", () => {
  const body = loadCfStatusBody();

  const gate = body.indexOf("if (!workspace)");
  assert.ok(
    gate >= 0,
    "loadCfStatus does not return while `workspace` is empty — a cold mount (no jf_workspace) issues a BARE " +
      "status request with no query string, which the API resolves to the first-owned account and the panel " +
      "then APPLIES (card t_a7e3c4d4)",
  );

  const fetchAt = body.indexOf("fetch(`/api/cf/status");
  assert.ok(fetchAt >= 0, "loadCfStatus no longer fetches /api/cf/status — the guard's anchor moved");
  assert.ok(
    gate < fetchAt,
    "the `if (!workspace)` gate sits BELOW the status fetch — it has to return before a URL is built, or the " +
      "unscoped request still leaves (card t_a7e3c4d4)",
  );

  // The URL must be built from the SAME value the gate checked. `workspaceQuery()`
  // reads the module-level confirmed scope, so a stale render (non-empty
  // `workspace`) plus a module scope that has just been dropped to "" yields a
  // gate that passes and a URL with no query — the same bare request, one step
  // removed.
  assert.ok(
    !stripLineComments(body).includes("workspaceQuery("),
    "loadCfStatus builds its URL through workspaceQuery() — the scope the gate checked is `workspace`, and a " +
      "URL built from a different source can go out unscoped (card t_a7e3c4d4)",
  );
  assert.match(
    body,
    /\?workspace=\$\{encodeURIComponent\(workspace\)\}/,
    "loadCfStatus does not scope the status URL to `workspace` — the scoped read must name the workspace whose " +
      "state the panel is about to describe (card t_a7e3c4d4)",
  );
});

test("the OAuth href can never be painted unscoped", () => {
  // A bare initial value is paintable: the anchor is gated on `cfStatusLoaded &&
  // !cfLinked`, both of which the pre-fix bare read could satisfy before the
  // scope existed. Empty is the only initial value that cannot name an account.
  assert.ok(
    /const \[cfStartHref, setCfStartHref\] = useState\(""\)/.test(settings),
    "the OAuth href must start EMPTY — initialising it to the bare cf start path paints an unscoped link in " +
      "the pre-scope window, whose callback seals against the first-owned account (card t_a7e3c4d4)",
  );

  // …and the effect that fills it must derive from `workspace`, not from the
  // browser scope, for the same reason as the read above.
  const at = settings.indexOf("const [cfStartHref, setCfStartHref] = useState(");
  const effect = settings.slice(at, at + 1200);
  assert.match(
    effect,
    /workspace \? `\/api\/cf\/start\?workspace=\$\{encodeURIComponent\(workspace\)\}` : ""/,
    "the cfStartHref effect does not build the href from `workspace` and clear it when the scope is unknown " +
      "(card t_a7e3c4d4)",
  );
  assert.ok(
    !/setCfStartHref\(`\/api\/cf\/start\$\{workspaceQuery\(\)\}`\)/.test(effect),
    "the cfStartHref effect still uses workspaceQuery() — it can emit a bare href while `workspace` is empty " +
      "(card t_a7e3c4d4)",
  );

  // The anchor itself refuses an empty href, so even a transient `cfStatusLoaded`
  // from a dropped scope cannot paint a link that names no workspace.
  assert.ok(
    /\{cfStatusLoaded && cfStartHref !== "" && !cfLinked && \(\s*<a href=\{cfStartHref\}/.test(settings),
    "the Cloudflare login anchor is not gated on a non-empty href — an empty href resolves to the current " +
      "page, and the pre-scope window is exactly when it would paint (card t_a7e3c4d4)",
  );
});
