// The workspace-delete confirm's ARMED state, pinned at the source (t_2b129c85).
//
// WHY THIS IS A SOURCE SCAN AND NOT A BROWSER ASSERTION. The confirm button is
// armed only when `WorkspaceTab.tsx`'s `canDelete` is true, and `canDelete` ANDs
// three things: the server's `can_delete`, the typed slug matching the real one,
// and an idle button. The last two are measured behaviourally by
// `e2e/a11y-dialogs.spec.ts` (a prefix, an inverted case, the exact slug, the
// exact slug padded with whitespace, and a submit held open). The FIRST could
// not be, and that was the hole: no test under `web/` ever stubbed
// `can_delete: false`, so `const canDelete = slugMatches && !busy;` — the
// mutation this file exists to fail — left the whole tree green.
//
// It cannot be measured at runtime because the state it governs is unreachable:
//
//   * the dialog has exactly ONE entry point, the danger-zone trigger, and that
//     trigger is itself `disabled={!check.can_delete || check.last_workspace}` —
//     with `can_delete: false` there is no click to make;
//   * react-dom will not run a disabled control's handler even when the DOM
//     attribute is stripped: `shouldPreventMouseEvent` reads the FIBER props
//     (`props.disabled && isInteractive(type)`, react-dom-bindings/
//     events/getListener.js). Measured: attribute removed, `el.disabled === false`,
//     and a dispatched click AND a real click both did nothing — the behaviour
//     facebook/react#8308 describes ("when the user enables the button from
//     devtools by removing the disabled attribute, the onClick still will not
//     fire");
//   * the third route into the state — the deletion check being re-read while the
//     dialog is open — does not hold either. `load` re-runs only when the locale
//     or the workspace scope changes, and a scope change rebuilds the session
//     tree. Measured with a second tab firing a real `storage` event (the app's
//     own "another tab signed out / switched workspace" path): the open dialog was
//     gone within 250ms and every page fetch had re-run.
//
// So this conjunct is defence in depth, and the guard for it has to be the
// source — the same shape as `src/lib/rowGuardCoverage.test.ts` and the other
// source pins in this directory. The expression IS the contract here.
//
// BITE (verified by mutation in the PR evidence): replacing the expression with
// `slugMatches && !busy` fails the first assertion by name.
//
// Run with:  npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const workspaceTab = read("../components/WorkspaceTab.tsx");

/** The RHS of every `const canDelete = …;`, whitespace-normalised. */
function canDeleteExpressions(src: string): string[] {
  return [...src.matchAll(/const canDelete\s*=\s*([^;]+);/g)].map((m) =>
    m[1].replace(/\s+/g, " ").trim(),
  );
}

test("the workspace-delete confirm ANDs the server's can_delete into its armed state", () => {
  const exprs = canDeleteExpressions(workspaceTab);
  assert.equal(exprs.length, 1, "WorkspaceTab must derive `canDelete` exactly once");
  assert.equal(
    exprs[0],
    "check?.can_delete === true && slugMatches && !busy",
    "the workspace-delete confirm must be armed only by the server's `can_delete === true`, " +
      "the typed slug matching, and an idle button — dropping any conjunct (e.g. " +
      "`slugMatches && !busy`) arms a destructive confirm on a workspace the server has " +
      "already said may not be deleted, and nothing else in the tree sees it",
  );
});

test("that expression is what the confirm button reads", () => {
  // Wired, not merely present: the destructive button's disabled state is
  // `!canDelete`. The rename button beside it reads `!canRename`, so this cannot
  // be satisfied by an unrelated control.
  assert.ok(
    workspaceTab.includes("disabled={!canDelete}"),
    "the confirm button must take its disabled state from `canDelete`",
  );
});
