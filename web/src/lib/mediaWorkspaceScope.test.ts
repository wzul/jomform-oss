// Guard for card t_e19368af: the media library ignored the workspace switcher.
//
// THE DEFECT, measured on prod v1.21.239 (pre-existing since `baa8a75c`, issue #382).
// It has three halves and this file pins all three, because each one alone leaves the
// merchant working in a workspace while the page reads or writes another:
//
//   A. /media built its list URL as
//
//          `/api/rest/media?${params.toString()}${workspaceQuery()}`
//
//      and `workspaceQuery()` opens with `?` (it is designed for a bare path). With the
//      scope `abc` the resource timing showed
//
//          https://jomform.com/api/rest/media?page=1&limit=24?workspace=abc
//
//      Go folds everything after the second `?` into the FIRST parameter's value, so the
//      name became `24?workspace`, `workspace` was ABSENT, and the API answered about the
//      caller's FIRST owned account — the grid rendered another workspace's library while
//      the switcher said `abc`.
//
//   B. every upload call site posted to a bare "/api/rest/uploads/image". Without
//      ?workspace= the API resolves the session's FIRST owned account, so a file uploaded
//      from a workspace-scoped page was written into the first workspace's library and
//      charged to ITS quota (measured: abc 0 → 0 bytes, first account +142, file
//      unreachable from the workspace being worked in).
//
//   C. the API answered `?workspace=<unknown>` on the live list with 200 + the first
//      account's rows instead of 403 "workspace tidak dijumpai". That half is the Go
//      suite's (httpapi/media_workspace_scope_test.go); this file only proves the client
//      actually SENDS a well-formed scope for the server to refuse.
//
// WHAT IS ASSERTED HERE, and why each can fail:
//
//   1. `mediaListUrl` COMPOSES the list request as a value: exactly one `?`, and
//      `workspace` parses as its OWN parameter next to `page`/`limit`. Driving the value
//      is the point — the bug was a property of string concatenation, invisible until a
//      real request went out with a parameter literally named `24?workspace`.
//   2. Every `/api/rest/uploads/image` call site in the tree goes through `workspaceUrl(`.
//      Reverting one to a bare string literal fails this scan.
//   3. NO template literal interpolates `${workspaceQuery()}` after a literal `?` (at the
//      template's top level, so `?include_archived=1${...}` and `?${params}${...}` are
//      both caught, while optional chaining `${row?.id}${...}` is not).
//   4. VACUITY on both scans: a reflow that stops matching must fail, not pass green
//      while checking nothing.
//
// SCOPE: this file does not drive the Go handlers and does not render the page. The
// server half lives in the api suite; the rendered half was measured in the browser.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = join(fileURLToPath(new URL(".", import.meta.url)), "..", "..");
const srcRoot = join(webRoot, "src");

type WorkspaceModule = {
  setConfirmedWorkspaceScope?: (slug: string | null) => void;
};
const workspaceMod = (await import("../lib/workspace.ts")) as unknown as WorkspaceModule;
const { mediaListUrl } = (await import("../lib/mediaListUrl.ts")) as {
  mediaListUrl: (params: URLSearchParams) => string;
};

// The scope is published through the module's own setter, the way SessionProvider
// does it, rather than by reaching into its private state.
function withScope<T>(slug: string | null, fn: () => T): T {
  workspaceMod.setConfirmedWorkspaceScope?.(slug);
  try {
    return fn();
  } finally {
    workspaceMod.setConfirmedWorkspaceScope?.(null);
  }
}

function mediaParams(): URLSearchParams {
  const params = new URLSearchParams();
  params.set("page", "1");
  params.set("limit", "24");
  return params;
}

// ── 1. the list URL ─────────────────────────────────────────────────────────

test("mediaListUrl sends the scope as its OWN parameter, after exactly one `?`", () => {
  const url = withScope("abc", () => mediaListUrl(mediaParams()));

  assert.equal(
    url.split("?").length - 1,
    1,
    `the list request must carry exactly one '?'; this is the t_e19368af defect shape ` +
      `(workspaceQuery() opens with '?' so appending it to a query string produced ` +
      `?page=1&limit=24?workspace=abc): ${url}`,
  );
  assert.ok(
    !url.includes("24?workspace"),
    `the scope was folded into the previous parameter's VALUE (that is what the Go side ` +
      `reads as a parameter named '24?workspace'): ${url}`,
  );
  const q = new URLSearchParams(url.slice(url.indexOf("?") + 1));
  assert.equal(q.get("workspace"), "abc", `the workspace scope must survive: ${url}`);
  assert.equal(q.get("page"), "1", `the page parameter must not absorb the scope: ${url}`);
  assert.equal(q.get("limit"), "24", `the limit parameter must not absorb the scope: ${url}`);
});

test("mediaListUrl escapes a slug that needs it", () => {
  const url = withScope("ws a/b", () => mediaListUrl(mediaParams()));
  const q = new URLSearchParams(url.slice(url.indexOf("?") + 1));
  assert.equal(q.get("workspace"), "ws a/b", `the slug must round-trip through a parser: ${url}`);
  assert.ok(!url.includes(" "), `the slug must be escaped in the URL: ${url}`);
});

test("mediaListUrl adds nothing when no workspace is selected", () => {
  const url = withScope(null, () => mediaListUrl(mediaParams()));
  assert.equal(
    url,
    "/api/rest/media?page=1&limit=24",
    "an unscoped request must stay byte-identical to the pre-fix URL, so first paint is unchanged",
  );
});

// ── 2/3. the source scans ───────────────────────────────────────────────────

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\./.test(name)) out.push(p);
  }
  return out;
}

test("every /api/rest/uploads/image call site carries the workspace scope", () => {
  const files = walk(srcRoot);
  assert.ok(files.length > 50, `only ${files.length} source files walked — the scan is broken`);

  const offenders: string[] = [];
  let sites = 0;
  for (const f of files) {
    for (const line of readFileSync(f, "utf8").split("\n")) {
      const code = line.replace(/\/\/.*$/, "").trim();
      if (!code.includes('"/api/rest/uploads/image"') && !code.includes("'/api/rest/uploads/image'")) continue;
      sites += 1;
      // The scoped form is workspaceUrl("/api/rest/uploads/image"), which yields
      // `?workspace=<slug>`. A bare literal posts UNSCOPED and the API resolves the
      // session's FIRST owned account (resolveSessionAccount), so the file lands in —
      // and is charged to — a different workspace than the one on screen.
      if (!code.includes('workspaceUrl("/api/rest/uploads/image")')) {
        offenders.push(`${relative(webRoot, f)}: ${line.trim()}`);
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "an upload is posted without ?workspace=, so it is written into the session's FIRST " +
      "owned account instead of the workspace the switcher names (card t_e19368af). Wrap " +
      `the path in workspaceUrl(...):\n  ${offenders.join("\n  ")}`,
  );
  // Vacuity: five image-upload sites exist (media page, MediaPicker, products list,
  // product editor, builder). A scan that stops finding them must fail loudly, not pass.
  assert.ok(sites >= 5, `only ${sites} /api/rest/uploads/image call site(s) found — the scan went hollow`);
});

// scanWorkspaceQueryAppends walks the source with a small tokenizer and reports every
// template literal that interpolates `${workspaceQuery()}` while its own literal text
// already contains a query separator.
//
// The tokenizer is needed because the defect spans an INTERPOLATION:
//
//     `?${params.toString()}${workspaceQuery()}`     <- the defect
//     `?include_archived=1${workspaceQuery()}`       <- the defect (the older site)
//     `/api/rest/forms${workspaceQuery()}`           <- correct
//     `${row?.id}${workspaceQuery()}`                <- correct (optional chaining)
//     `?a=1&workspace=x` (no interpolation)          <- not a ${workspaceQuery()} site
//
// A line regex cannot tell the first from the second without also flagging optional
// chaining; the tokenizer tracks template/interpolation nesting instead, and skips
// comments and ordinary strings.
type Append = { file: string; line: number; prefix: string };

function scanWorkspaceQueryAppends(src: string, file: string): Append[] {
  const out: Append[] = [];
  type Frame = { kind: "template"; lit: string; start: number } | { kind: "expr"; depth: number };
  const frames: Frame[] = [];
  let i = 0;
  let line = 1;

  const atTopOfCode = () => frames.length === 0 || frames[frames.length - 1].kind === "expr";

  while (i < src.length) {
    const ch = src[i];
    const top = frames[frames.length - 1];

    if (atTopOfCode()) {
      // Comments and ordinary strings are code-shaped noise: skip them whole.
      if (ch === "/" && src[i + 1] === "/") {
        while (i < src.length && src[i] !== "\n") i += 1;
        continue;
      }
      if (ch === "/" && src[i + 1] === "*") {
        i += 2;
        while (i < src.length && !(src[i] === "*" && src[i + 1] === "/")) {
          if (src[i] === "\n") line += 1;
          i += 1;
        }
        i += 2;
        continue;
      }
      if (ch === '"' || ch === "'") {
        const quote = ch;
        i += 1;
        while (i < src.length && src[i] !== quote) {
          if (src[i] === "\\") i += 1;
          if (src[i] === "\n") line += 1;
          i += 1;
        }
        i += 1;
        continue;
      }
      if (ch === "`") {
        frames.push({ kind: "template", lit: "", start: i });
        i += 1;
        continue;
      }
      if (top?.kind === "expr") {
        if (ch === "{") top.depth += 1;
        else if (ch === "}") {
          if (top.depth === 0) frames.pop();
          else top.depth -= 1;
        }
      }
      if (ch === "\n") line += 1;
      i += 1;
      continue;
    }

    // Inside a template literal's TEXT.
    const tpl = top as { kind: "template"; lit: string };
    if (ch === "\\") {
      tpl.lit += ch + (src[i + 1] ?? "");
      i += 2;
      continue;
    }
    if (ch === "`") {
      frames.pop();
      i += 1;
      continue;
    }
    if (ch === "$" && src[i + 1] === "{") {
      const body = src.slice(i + 2, i + 2 + "workspaceQuery()".length);
      if (body === "workspaceQuery()") {
        // A `?` in the literal so far is a query separator UNLESS it starts `?.`
        // (optional chaining, which is not a query at all).
        if (/\?(?!\.)/.test(tpl.lit.replace(/\\\?/g, ""))) out.push({ file, line, prefix: tpl.lit });
      }
      frames.push({ kind: "expr", depth: 0 });
      i += 2;
      continue;
    }
    if (ch === "\n") line += 1;
    tpl.lit += ch;
    i += 1;
  }
  return out;
}

test("no call site appends workspaceQuery() to a URL that already has a `?`", () => {
  const files = walk(srcRoot);
  assert.ok(files.length > 50, `only ${files.length} source files walked — the scan is broken`);

  const offenders: Append[] = [];
  let sites = 0;
  for (const f of files) {
    const src = readFileSync(f, "utf8");
    sites += src.split("${workspaceQuery()}").length - 1;
    offenders.push(...scanWorkspaceQueryAppends(src, relative(webRoot, f)));
  }
  assert.deepEqual(
    offenders.map((o) => `${o.file}:${o.line}: …${o.prefix}${"${workspaceQuery()}"}`),
    [],
    "a template literal composes a query with '?' and then interpolates workspaceQuery(), " +
      "which itself opens with '?' — the Go side then folds the scope into the previous " +
      "parameter's value and the request runs UNSCOPED, resolving the caller's FIRST owned " +
      "account (card t_e19368af / t_4010be39). Use workspaceUrl(\"/path?existing=1\") or the " +
      "mediaListUrl helper instead.",
  );
  // Vacuity: `workspaceQuery()` is imported by dozens of call sites. If the tokenizer
  // stops seeing them, this guard is green while checking nothing, which is how the
  // original defect survived a guard that already existed for it.
  assert.ok(sites > 40, `only ${sites} workspaceQuery() call sites found — the scan went hollow`);
});
