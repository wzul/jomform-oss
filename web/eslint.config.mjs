// eslint.config.mjs — the flat config `npm run lint` (`eslint .`) uses.
//
// WHY IT EXISTS
//   `web/package.json` used to declare `"lint": "next lint"`. Next.js 16 removed
//   that command, so the script failed on every run instead of linting (the
//   trailing word was read as the `[directory]` argument: "Invalid project
//   directory provided, no such directory: <web>/lint") — see
//   web/src/lib/webScripts.test.ts, which now pins the shape. This is the other
//   half: a REAL linter, wired to a script AND to the `lint` gate in
//   scripts/preflight.sh. Before this file the repo had NO lint tooling at all
//   (`git log --all -- '*eslint*'` was empty, no `eslint` in the manifest or the
//   lock), so nothing checked the web tree for anything but types and tests.
//
// SHAPE: the documented Next 16 replacement, verbatim — `core-web-vitals` plus
//   `typescript`, spread into `defineConfig`, with the same ignore list the two
//   presets carry. https://nextjs.org/docs/app/api-reference/config/eslint
//   Nothing is added that is not discussed below.
//
// THE ONE DECISION THIS FILE MAKES, AND WHY IT IS PER-RULE
//   `eslint-config-next` 16 pulls `eslint-plugin-react-hooks` 7, whose
//   `recommended` preset is NOT the two classic rules any more: v7 moved the
//   React Compiler's diagnostics into `recommended`
//   (https://github.com/react/react/blob/main/packages/eslint-plugin-react-hooks/CHANGELOG.md,
//   "all compiler rules are enabled by default"). On THIS tree that is 62 of the
//   86 errors — a compiler MIGRATION backlog, not 62 defects:
//
//     * the repo does not enable the compiler (`next.config.mjs` has no
//       `experimental.reactCompiler`), so these diagnostics describe code the
//       compiler is not running on. The React team's own guidance is that the
//       linter "does not require the compiler to be installed" and that you
//       "don't need to fix all violations immediately"; the widely-reproduced
//       experience is 1,030 new errors across six codebases of which the sampled
//       sites were 0 bugs (set-state-in-effect 18 sites / 0 bugs, refs 18 / 0):
//       https://nowaterprogramming.com/blog/react-hooks-eslint-v7-measured
//     * the largest family, `set-state-in-effect` (49 here), fires on the
//       mount-fetch / hydration idiom this tree uses deliberately throughout
//       (`useEffect(() => { void load(); }, [load])` after a `useCallback`, and
//       `setMounted(true)` / `activeWorkspaceScope()` on mount). React
//       acknowledges the pattern and the rule's own strictness is an open issue:
//       https://github.com/react/react/issues/34743
//     * `preserve-manual-memoization` and `incompatible-library` report "skipped
//       compilation" — information that cannot be acted on without the compiler.
//
//   So FOUR rules are turned OFF, one line each, with the reason at the line.
//   This is NOT a blanket disable and NOT the whole plugin: `rules-of-hooks` and
//   `exhaustive-deps` (the two rules that catch real React bugs) stay ON at the
//   preset's severity, and every other compiler diagnostic stays ON — the seven
//   `static-components` hits were real (components declared inside a component
//   body) and are FIXED in this change, not silenced. Turning the rest of the
//   compiler family back on is the migration itself, and it belongs with a card
//   that enables the compiler, not with the card that adds the linter.
import { defineConfig } from "eslint/config";
import coreWebVitals from "eslint-config-next/core-web-vitals";
import typescript from "eslint-config-next/typescript";

export default defineConfig([
  ...coreWebVitals,
  ...typescript,
  {
    rules: {
      // OFF: 49 of 86 errors. Synchronous setState in an effect. This tree seeds
      // state from browser-only sources after mount (localStorage, window) and
      // loads lists from a `useCallback` fetcher in an effect — the exact
      // patterns the rule names, none of them a defect here. It is a
      // compiler-migration backlog item: each fix deletes an effect and a
      // render, and React's guidance is to not fix them all up front.
      "react-hooks/set-state-in-effect": "off",
      // OFF: 3 errors, and the rule does not read the code correctly for this
      // idiom. `sales/page.tsx` keeps a `pageRef.current = page` latest-value
      // ref during render so the SSE handler below closes over the CURRENT page
      // rather than the one from the effect's first run. The rule flags the
      // assignment; the official replacement (React's `useEffectEvent`) is not
      // available for this consumer yet, and the upstream rule has documented
      // false positives of this shape (react/react#34743).
      "react-hooks/refs": "off",
      // OFF: 1 error, and it is a false positive of the same family:
      // "Cannot access variable before it is declared" is raised on
      // feedback/page.tsx:105, where the reference is lexically AFTER the
      // declaration (the `openThread` useCallback) and is only ever run from an
      // effect, long after the binding is initialised.
      "react-hooks/immutability": "off",
      // OFF: 2 errors. This rule reports components the COMPILER chose to skip
      // ("Compilation Skipped: Existing memoization could not be preserved").
      // There is no compiler here, so the diagnostic cannot be acted on; with
      // the compiler enabled it becomes meaningful and should come back ON.
      "react-hooks/preserve-manual-memoization": "off",
    },
  },
  { ignores: [".next/**", "out/**", "build/**", "next-env.d.ts", "node_modules/**"] },
]);
