// Structural net for card t_de220c75: Settings → Domains rendered the PREVIOUS
// workspace's custom-domain list after a workspace switch.
//
// THE DEFECT
//   `bundle` is workspace-scoped (chip status, keys, domains, webhooks, einvoice)
//   and a switch publishes the new scope one commit before the new payload can
//   arrive. Two things then went wrong, either of which is enough:
//
//     1. `Shell.switchWorkspace` calls `useSession().reload()` right after
//        `enterWorkspace(slug)` publishes the NEW slug, but `reload` was a
//        `useCallback` over `[workspace]` — so its closure still held the slug
//        being LEFT and every switch re-fetched that workspace's bundle;
//     2. whichever `dashboard-bundle` answer landed last was assigned to
//        `bundle`, with no check that it was still the active scope's answer.
//
//   Measured on prod v1.22.91 (`wan` has two verified domains, `abc` has none):
//   opening the tab on `abc` was correct, switching to `wan` left the list empty
//   under `wan`'s chip, and switching back to `abc` painted `wan`'s two rows —
//   each with a live `Remove` button — under `abc`'s chip. One switch behind, in
//   both directions, with BOTH scoped bundles on the wire.
//
// WHY THIS FILE EXISTS ALONGSIDE THE BROWSER SPEC
//   The behavioural proof is `web/e2e/settings-domains-workspace-switch.spec.ts`,
//   which drives the real switcher and reads the rendered rows (the repo has no
//   JSX/DOM unit runner — see `src/lib/cfStatusBeforeFetch.test.ts`). That spec
//   needs a production build; this file runs in the `web` gate's unit half in
//   milliseconds and pins the SHAPES the fix has to keep. Neither replaces the
//   other: a source scan cannot see what is painted, and the browser spec cannot
//   tell you which line came back.
//
// WHAT IT PINS
//   1. The session refuses a settled read whose scope is no longer the ACTIVE one
//      (the guard sits above the assignment, and every applied bundle is tagged
//      with the slug it was fetched for).
//   2. `reload()` resolves the scope AT CALL TIME — not from the render it closed
//      over, which is what re-read the workspace being left.
//   3. The Domains panel applies a bundle only when its scope is the one on
//      screen, and empties the lists — never paints another workspace's rows —
//      while it holds the previous workspace's payload.
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

const session = read("../components/session.tsx");
const settings = read("../app/settings/page.tsx");

/**
 * Drop whole-line `//` comments: the checks below are about CODE, and these
 * functions' own comments explain the fix by naming the very identifiers a naive
 * substring test would match on.
 */
function stripLineComments(src: string): string {
  return src
    .split("\n")
    .filter((line) => !line.trimStart().startsWith("//"))
    .join("\n");
}

/** The body of a `const X = useCallback(…` declaration, delimited by its deps. */
function callbackBody(src: string, marker: string, terminator: RegExp): string {
  const start = src.indexOf(marker);
  assert.ok(start >= 0, `${marker} is gone — the guard's anchor moved`);
  const slice = src.slice(start);
  const tail = terminator.exec(slice);
  assert.ok(tail, `could not delimit the body of ${marker} (no matching dependency array)`);
  return slice.slice(0, tail.index + tail[0].length);
}

test("the guard is looking at the real sources (it cannot pass vacuously)", () => {
  assert.ok(
    session.includes("const applyBundleRead = useCallback("),
    "components/session.tsx no longer declares applyBundleRead — the guard's anchor moved",
  );
  assert.ok(
    session.includes("const [bundleScope, setBundleScope] = useState"),
    "components/session.tsx no longer tags the bundle with its scope — the guard's anchor moved",
  );
  assert.ok(
    settings.includes("const load = useCallback("),
    "app/settings/page.tsx no longer declares load — the guard's anchor moved",
  );
});

test("the session applies a read only while its scope is still the ACTIVE one", () => {
  const body = stripLineComments(callbackBody(session, "const applyBundleRead = useCallback(", /\n {2}\}, \[\]\);/));

  const gate = body.indexOf("if (slug !== activeWorkspaceScope()) return;");
  assert.ok(
    gate >= 0,
    "applyBundleRead does not compare the read's scope to activeWorkspaceScope() — the answer that lands last " +
      "wins, which is how a switch left the panel one workspace behind (card t_de220c75)",
  );

  const assign = body.indexOf("setBundle(");
  assert.ok(assign >= 0, "applyBundleRead no longer sets the bundle — the guard's anchor moved");
  assert.ok(gate < assign, "the scope gate sits BELOW the bundle assignment — it has to return before it");

  assert.match(
    body,
    /setBundleScope\(slug\)/,
    "the applied bundle is not tagged with the scope it was fetched for — a consumer cannot tell the payload " +
      "on screen from the previous workspace's (card t_de220c75)",
  );
});

test("reload() reads the scope at CALL time, not the one its render closed over", () => {
  const body = stripLineComments(callbackBody(session, "const reload = useCallback(", /\n {2}\}, \[applyBundleRead\]\);/));

  assert.match(
    body,
    /const slug = activeWorkspaceScope\(\);/,
    "reload() does not resolve the active scope at call time — `Shell.switchWorkspace` calls it immediately " +
      "after publishing the NEW slug, so a `[workspace]` closure re-fetches the workspace being LEFT and its " +
      "late answer repaints the old rows (card t_de220c75)",
  );

  assert.ok(
    !body.includes("fetchBundle(workspace)"),
    "reload() still fetches `workspace` from its render closure — that is the read of the workspace being left " +
      "(card t_de220c75)",
  );
});

test("the Domains panel applies a bundle only for the scope on screen", () => {
  const body = stripLineComments(
    callbackBody(settings, "const load = useCallback(", /\n {2}\}, \[bundle, bundleScope, workspace\]\);/),
  );

  const gate = body.indexOf("bundleScope === workspace");
  const apply = body.indexOf("apply(bundle)");
  assert.ok(
    gate >= 0,
    "load() applies `bundle` without checking that it belongs to the workspace on screen — that is the stale " +
      "apply which painted another workspace's domains, each with a Remove button (card t_de220c75)",
  );
  assert.ok(apply >= 0, "load() no longer applies the bundle — the guard's anchor moved");
  assert.ok(gate < apply, "the scope check sits BELOW the apply — it has to gate the call");

  assert.match(
    body,
    /setDomains\(\[\]\)/,
    "load() does not empty the domain list while it holds another workspace's payload — the previous " +
      "workspace's rows stay on screen (and actionable) until the new answer lands (card t_de220c75)",
  );
});
