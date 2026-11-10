// docs/accessibility.md must not hand-enumerate the scanned /docs routes
// (card t_1c6aa36b).
//
// THE BUG, as measured on the tree when this card was created.
// `docs/accessibility.md` listed the scanned docs routes by hand:
//
//   - `/docs` and every page under `web/src/app/docs` (`/docs/builder`,
//     `/docs/customize`, `/docs/embed`, `/docs/subscriptions`, `/docs/variations`,
//     `/docs/spam-detection`, `/docs/ses-reputation`, `/docs/placeholders`,
//     `/docs/mcp`, `/docs/feedback`, `/docs/chat`)
//
// while `web/e2e/a11y.spec.ts` — the list the `a11y` gate actually runs — held
// eighteen. The prose named twelve and omitted /docs/workspaces, /docs/sales,
// /docs/email, /docs/security, /docs/activity and /docs/api. It had drifted at
// least twice before:
// `git log -S'/docs/workspaces' -- web/e2e/a11y.spec.ts` -> 2089b6fb,
// `-S'/docs/activity'` -> 48943425, `-S'/docs/api'` -> 171ca95a — cards that
// updated the spec and not the prose, because nothing connects the two.
//
// WHY A GUARD RATHER THAN A ONE-TIME EDIT. Correcting the copy to today's
// eighteen routes fixes today and restores the defect tomorrow. The list's
// authority is `PAGES` in web/e2e/a11y.spec.ts, and `web/src/lib/docsA11y.test.ts`
// already fails the unit suite when a docs page ON DISK is missing from that
// list — so the hand-copy was redundant as well as stale, and dropping it is the
// fix. This file is what stops it coming back.
//
// WHERE IT LOOKS, AND WHY ONLY THERE. The scanned routes are listed in ONE
// region: the `## Automated scan (axe-core)` section, from that heading to the
// `Run it locally:` line. The rule reads that region and nothing else, because
// the rest of the document legitimately names a single route in an incident note
// (`/docs/api` was read as a `/*` comment opener by the JSX lexer) — a
// document-wide count would have to allow a number so high it stopped catching
// a list. Scope is a limit and is stated rather than hidden: an enumeration
// moved to another section would escape this floor.
//
// A FLOOR, NOT A PROOF. Naming a couple of routes as examples is useful prose
// (this document now names `/docs`, `/docs/builder`, `/docs/customize`), and a
// rule of "zero" would forbid that and be gamed by a list split across lines.
// What a count cannot judge is exactly what a count never can — three examples
// is arithmetically the same as three survivors of a longer list. It is a speed
// bump on the enumeration returning, in the same spirit as `PER_ENTRY_FLOOR` in
// `docsFacts.test.ts`.
//
// The second rule catches the other half of the drift shape: the prose names a
// spec FILE, so a renamed or moved spec leaves the document describing a guard
// nobody runs. Every `*.spec.ts` written in the document must exist in web/e2e.
//
// WHAT IT DOES NOT COVER (stated, not hidden):
//   * It reads SOURCE text. There is no renderer for this file — it is read on
//     GitHub and in an editor — so the source IS what a reader gets; but the
//     extractor sees a code span, not "a list", and cannot tell a deliberate
//     enumeration from prose that happens to name several routes in one
//     sentence.
//   * It judges a COUNT in one region and nothing else (see scope above).
//   * It does not check a prose CLAIM about which gate runs a spec (the `a11y`
//     vs `feature-core` split by filename prefix at `web/playwright.config.ts`);
//     it checks that the spec a claim is about is a real file.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const DOC = pathOf("../../../docs/accessibility.md");
const E2E_DIR = pathOf("../../e2e");
const REPO_ROOT = pathOf("../../..");

// THE WEB IMAGE BUILD DOES NOT SHIP THIS FILE, so the guard must not run there.
// `web/Dockerfile` copies `web/`, `api/` and `MCP-PARITY.md` into the build context
// and NOT the repo-root `docs/` tree, while `npm run build` (= this suite, via
// `test:unit`) runs inside that image — so the document is genuinely absent and the
// four tests below would fail on ENOENT (measured: the `webimage` gate went RED with
// `ENOENT /repo/docs/accessibility.md`). That is not a regression in the guard and
// not a reason to weaken it: the subject does not exist in that environment.
//
// The skip is identified the SAME way the two sibling guards that read checkout-only
// files do (`web/src/lib/nextManagedFiles.test.ts`, `web/src/lib/harnessLock.test.ts`,
// card `t_10065a1e`): no `.git` AND no checkout-only root entry. The conjunction is
// load-bearing in both directions — a real checkout whose git metadata was lost
// (a pruned linked worktree, a renamed `.git`, git missing from PATH) still carries
// `.gitignore` and therefore still FAILS here rather than quietly skipping, which is
// the shape the siblings' comments record as the reason for the second half.
const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];

// imageBuildSkipReasonAt is exported-shaped (a pure function of a root) so its
// premise can be reasoned about rather than guessed; the caller passes REPO_ROOT.
function imageBuildSkipReasonAt(root: string): string | false {
  if (existsSync(join(root, "docs", "accessibility.md"))) return false;
  const gitMetadata = existsSync(join(root, ".git"));
  const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(root, e)));
  if (gitMetadata || rootEntries.length > 0) return false; // a real checkout: let the guard fail
  return (
    `the web image build has no repository and no repo-root docs/ tree: ${root} has no .git ` +
    `and no checkout-only root entry (${CHECKOUT_ONLY_ROOT_ENTRIES.join(", ")}), and ` +
    `web/Dockerfile copies web/, api/ and MCP-PARITY.md but not docs/ — so this guard's ` +
    `subject is absent by construction, not by regression`
  );
}

// skipInImageBuild is called at the top of every test: an absent document in a real
// checkout must FAIL below, and only the image build may skip.
function skipInImageBuild(t: { skip: (reason?: string) => void }): boolean {
  const reason = imageBuildSkipReasonAt(REPO_ROOT);
  if (reason === false) return false;
  t.skip(reason);
  return true;
}

// At most this many docs routes may be written out by hand in the scanned-pages
// region before it is read as an enumeration again. See the header.
const MAX_EXAMPLE_ROUTES = 4;

const SECTION_START = "## Automated scan (axe-core)";
const SECTION_END = "Run it locally:";

// The scanned-pages region: from the `## Automated scan` heading to the
// `Run it locally:` line, which is where the route list lives. Returns the whole
// document when either marker is missing, so a heading rename makes the rule
// STRICTER rather than silently narrower.
function scannedPagesRegion(src: string): string {
  const start = src.indexOf(SECTION_START);
  const end = src.indexOf(SECTION_END);
  if (start < 0 || end < 0 || end <= start) return src;
  return src.slice(start, end);
}

// Every route-shaped code span — a backtick span whose whole content is `/docs`
// or `/docs/<slug>` — as distinct sorted routes.
//
// Keyed on the BACKTICK SPAN rather than on `/docs/...` anywhere in the text on
// purpose: an enumeration is written as code spans, and a bare mention in a
// sentence ("the page at /docs/x now …") is prose, not a list. Distinctness is
// the measure, because a route repeated in two sentences is one entry in a
// reader's list.
function enumeratedDocsRoutes(src: string): string[] {
  const found = new Set<string>();
  for (const m of src.matchAll(/`(\/docs(?:\/[a-z0-9-]+)?)`/g)) found.add(m[1]);
  return [...found].sort();
}

// Every spec file the document names, as basenames, whatever directory prefix
// the code span carries. The span is matched whole and then reduced to its last
// path segment on purpose: a bare `builder-x.spec.ts` and a full
// `web/e2e/builder-x.spec.ts` must both be checked, and a lazy prefix inside the
// span would match the basename mid-word (reporting a renamed spec as a fragment
// of itself rather than by name).
//
// A name containing a glob metacharacter (`a11y*.spec.ts`, which the document
// uses to describe the project's testMatch rule) is a PATTERN, not a file, and is
// skipped: asserting that a pattern is "not there" would be a permanent red.
function namedSpecFiles(src: string): string[] {
  const found = new Set<string>();
  for (const m of src.matchAll(/`([^`\s]*\.spec\.ts)`/g)) {
    const base = m[1].slice(m[1].lastIndexOf("/") + 1);
    if (/[*?[\]]/.test(base)) continue;
    found.add(base);
  }
  return [...found].sort();
}

// THE PRE-FIX BYTES, verbatim as they stood in the document when this card was
// created. Keeping the real text (rather than a hand-simplified list) is what
// makes the rule's bite checkable: if the extractor stops recognising this
// shape, the assertion below fails instead of every later list passing unseen.
const PRE_FIX_ENUMERATION = [
  "- `/docs` and every page under `web/src/app/docs` (`/docs/builder`,",
  "  `/docs/customize`, `/docs/embed`, `/docs/subscriptions`, `/docs/variations`,",
  "  `/docs/spam-detection`, `/docs/ses-reputation`, `/docs/placeholders`,",
  "  `/docs/mcp`, `/docs/feedback`, `/docs/chat`). The knowledge base is public",
].join("\n");

test("the guard is reading the real document (it cannot pass vacuously)", (t) => {
  if (skipInImageBuild(t)) return;
  assert.ok(existsSync(DOC), `${DOC} does not exist — this guard is about that file`);
  const src = readFileSync(DOC, "utf8");
  assert.ok(src.length > 1000, `read only ${src.length} chars of ${DOC}`);
  assert.ok(
    src.includes("docsA11y.test.ts"),
    "the document does not name the guard that enforces the route list — either this is " +
      "not the QA reference this guard was written against, or that link was removed",
  );
  assert.notEqual(
    scannedPagesRegion(src),
    src,
    `neither ${JSON.stringify(SECTION_START)} nor ${JSON.stringify(SECTION_END)} is in the ` +
      `document, so the region this rule reads could not be located — a heading rename must ` +
      `be reflected here, not silently drop the check to nothing`,
  );
  assert.ok(
    namedSpecFiles(src).length > 0,
    "no `*.spec.ts` path was parsed out of the document, so the existence rule below is vacuous",
  );
});

test("the extractor catches the enumeration this card removed (the rule is not vacuous)", () => {
  const before = enumeratedDocsRoutes(PRE_FIX_ENUMERATION);
  assert.ok(
    before.length >= 10,
    `the pre-fix block yields only ${before.length} routes (${before.join(", ")}) — the ` +
      `extractor no longer recognises the shape it exists to catch, so a hand-list coming ` +
      `back would pass unnoticed`,
  );
  assert.ok(
    before.length > MAX_EXAMPLE_ROUTES,
    `the pre-fix block yields ${before.length}, which a floor of ${MAX_EXAMPLE_ROUTES} would ` +
      `not have caught — the floor is above the defect it guards`,
  );
});

test("the scanned-pages list does not hand-enumerate the docs routes", (t) => {
  if (skipInImageBuild(t)) return;
  const region = scannedPagesRegion(readFileSync(DOC, "utf8"));
  const routes = enumeratedDocsRoutes(region);
  assert.ok(
    routes.length <= MAX_EXAMPLE_ROUTES,
    `${routes.length} docs routes are spelled out in the scanned-pages list of ` +
      `docs/accessibility.md: ${routes.join(", ")}. The scanned list is PAGES in ` +
      `web/e2e/a11y.spec.ts, and web/src/lib/docsA11y.test.ts fails when a docs page on ` +
      `disk is missing from it — a copy here is redundant and is what drifted twice ` +
      `(twelve named while the spec listed eighteen). Name at most ${MAX_EXAMPLE_ROUTES} as ` +
      `examples and point at the spec instead.`,
  );
});

test("every spec file the document names exists under web/e2e", (t) => {
  if (skipInImageBuild(t)) return;
  const missing = namedSpecFiles(readFileSync(DOC, "utf8")).filter(
    (f) => !existsSync(`${E2E_DIR}/${f}`),
  );
  assert.deepEqual(
    missing,
    [],
    `docs/accessibility.md names spec file(s) that are not there: ${missing.join(", ")} — ` +
      `a renamed or moved spec leaves the prose describing a guard nobody runs`,
  );
});
