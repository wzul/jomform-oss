// The /docs registry guard (card t_45fd1bee).
//
// WHY THIS EXISTS
//   Breadcrumb, previous/next, related guides and the search index all read one generated
//   registry (src/lib/docsNav.gen.ts). That removes four hand-maintained lists, but it
//   introduces one new failure mode: the generated file going stale against the pages. A
//   stale registry does not fail the build on its own — the chrome renders, it just sends
//   the reader to a page that is no longer in the reading order, or omits a page that was
//   added. So this guard checks the registry against the TREE in both directions, checks
//   the chrome lookups against it, and finally re-runs the generator in --check mode.
//
// WHAT IT DOES NOT COVER: whether a section label is a good label, or whether the curated
// product→guide pairs are the right pairs. Those are review questions; the structural
// facts (every path real, every guide reachable both ways, every page adjacent in exactly
// one place) are what is asserted.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { DOCS_NAV, docsBreadcrumb, docsEntry, docsPager, relatedGuides, relatedProducts } from "./docsNav.ts";
import { docsRoutesOnDisk } from "./docsRoutes.ts";
// The generator itself: the temp-tree test below drives the SAME code `npm run docs:nav`
// runs, so the acceptance cannot be satisfied by a second, test-only implementation. The
// squash-landing test compares RENDERED registries, i.e. the bytes `--check` compares.
import { buildRegistry, renderNavSource } from "../../scripts/docsNav.mjs";

const WEB_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const DOCS_DIR = join(WEB_DIR, "src", "app", "docs");
const LAYOUT_FILE = join(DOCS_DIR, "layout.tsx");

const LAYERS = new Set(["Learn", "Product docs", "Reference"]);

test("the guard is looking at the real tree (it cannot pass vacuously)", () => {
  assert.ok(DOCS_NAV.length >= 30, `the registry holds ${DOCS_NAV.length} entries — the tree has 30 pages`);
  assert.ok(DOCS_NAV.some((entry) => entry.path === "/docs"));
  assert.ok(DOCS_NAV.some((entry) => entry.kind === "guide"), "no guide page is in the registry");
});

test("the registry covers every page on disk, once, in one contiguous order", () => {
  const onDisk = docsRoutesOnDisk(DOCS_DIR);
  const inRegistry = DOCS_NAV.map((entry) => entry.path);
  assert.deepEqual(
    [...inRegistry].sort(),
    onDisk,
    "the registry and the app/docs tree disagree — run `npm run docs:nav` after adding or removing a page",
  );
  assert.equal(new Set(inRegistry).size, inRegistry.length, "a path appears twice in the registry");
  assert.deepEqual(
    DOCS_NAV.map((entry) => entry.order),
    inRegistry.map((_, index) => index),
    "order must be the array position — prev/next walk this array",
  );
});

test("every page carries the data the chrome reads", () => {
  for (const entry of DOCS_NAV) {
    assert.ok(entry.label.trim().length > 0, `${entry.path} has no label`);
    assert.ok(entry.section.trim().length > 0, `${entry.path} has no section`);
    assert.ok(LAYERS.has(entry.layer), `${entry.path} has layer ${JSON.stringify(entry.layer)}`);
    assert.ok(["product", "guide"].includes(entry.kind), `${entry.path} has kind ${entry.kind}`);
    assert.match(entry.updated, /^\d{4}-\d{2}-\d{2}$/, `${entry.path} has no last-updated date`);
    assert.match(entry.digest, /^[0-9a-f]{16}$/, `${entry.path} has no page digest`);
  }
});

test("the related-guide mapping works in both directions", () => {
  const guides = DOCS_NAV.filter((entry) => entry.kind === "guide");
  assert.ok(guides.length >= 8, `expected the eight screenshot guides, found ${guides.length}`);
  for (const entry of DOCS_NAV.filter((candidate) => candidate.kind === "product")) {
    for (const guidePath of entry.guides) {
      const guide = docsEntry(guidePath);
      assert.ok(guide, `${entry.path} lists ${guidePath}, which is not a /docs page`);
      assert.equal(guide?.kind, "guide", `${entry.path} lists ${guidePath}, which is not a guide`);
    }
  }
  for (const guide of guides) {
    assert.ok(
      relatedProducts(guide.path).length > 0,
      `${guide.path} is reachable from no product page — a guide nobody can find`,
    );
  }
  for (const entry of DOCS_NAV) {
    for (const related of [...relatedGuides(entry.path), ...relatedProducts(entry.path)]) {
      assert.ok(docsEntry(related.path), `${entry.path} links a related entry outside the registry`);
    }
  }
});

test("the breadcrumb names the section and ends at the page", () => {
  const shipping = docsBreadcrumb("/docs/shipping");
  assert.deepEqual(shipping.map((crumb) => crumb.label), ["Home", "Docs", "Sales & payments", "Shipping by zone"]);
  assert.equal(shipping[shipping.length - 1].href, undefined, "the last crumb must not link to itself");

  const reference = docsBreadcrumb("/docs/api");
  assert.ok(reference.some((crumb) => crumb.label === "Reference"), "the Reference layer is missing from /docs/api's trail");

  const guide = docsBreadcrumb("/docs/guides/donations");
  assert.ok(guide.some((crumb) => crumb.label === "Screenshot guides"), "a guide's trail does not name its section");

  assert.deepEqual(docsBreadcrumb("/docs").map((crumb) => crumb.label), ["Home", "Docs"]);
  assert.deepEqual(docsBreadcrumb("/nowhere"), [{ label: "Home", href: "/" }], "an unknown route must not invent a trail");
});

test("previous/next walks the whole tree exactly once", () => {
  assert.equal(docsPager(DOCS_NAV[0].path).prev, undefined, "the first page must not have a previous link");
  assert.equal(
    docsPager(DOCS_NAV[DOCS_NAV.length - 1].path).next,
    undefined,
    "the last page must not have a next link",
  );
  for (let index = 0; index < DOCS_NAV.length; index++) {
    const pager = docsPager(DOCS_NAV[index].path);
    assert.equal(pager.prev?.path, index > 0 ? DOCS_NAV[index - 1].path : undefined);
    assert.equal(pager.next?.path, index < DOCS_NAV.length - 1 ? DOCS_NAV[index + 1].path : undefined);
  }
  assert.deepEqual(docsPager("/not-a-docs-page"), {}, "an unknown route gets no pager");
});

test("the generator indexes a page that did not exist when the index was written", () => {
  const tree = mkdtempSync(join(tmpdir(), "jf-docsnav-"));
  try {
    mkdirSync(join(tree, "freshly-added"));
    writeFileSync(
      join(tree, "freshly-added", "page.tsx"),
      [
        "export default function Page() {",
        "  return (",
        "    <div>",
        "      <h1>Freshly added page</h1>",
        "      <h2>Why it is indexed</h2>",
        "      <p>This page appeared after the search index was generated.</p>",
        "    </div>",
        "  );",
        "}",
        "",
      ].join("\n"),
    );
    // A nav that names nothing: the page is UNLISTED, which is the case the acceptance is
    // about — it must still be indexed, without editing a list.
    const layoutSource = [
      "const NAV_GROUPS = [",
      '  { layer: "Learn", label: "Start here", sections: [] },',
      "];",
      "",
    ].join("\n");
    const { entries, problems } = buildRegistry({
      docsDir: tree,
      layoutSource,
      guides: [],
      previousEntries: new Map(),
    });
    assert.deepEqual(problems, []);
    const added = entries.find((entry) => entry.path === "/docs/freshly-added");
    assert.ok(added, "a page on disk was not indexed — the index is a hand-maintained list after all");
    assert.match(added.text, /Freshly added page|appeared after the search index/);
    assert.match(added.headings.map((heading) => heading.text).join(" "), /Why it is indexed/);
    assert.equal(added.section, "More");
  } finally {
    rmSync(tree, { recursive: true, force: true });
  }
});

test("a squash landing cannot re-date a page nobody edited (t_746c9c5d)", () => {
  // This repo lands by SQUASH: one new commit holding every file in the PR diff, so every
  // docs page in it gets a new committer date — the landing time — while the registry holds
  // the date of the last PAGE edit. `updated` is therefore pinned to the page BYTES
  // (`digest`), not to a commit: identical bytes keep the committed date, changed bytes
  // recompute it and make the rendered registry differ, which is what `--check` refuses.
  //
  // The "commit date" comes from a STAND-IN binary, not from a real `git`: the web image
  // runs `npm run build` — and therefore this suite — inside node:24-alpine, which ships no
  // git, and a version of this test that shelled out to one died there with `spawnSync git
  // ENOENT` (green on the host, red in the shipping image). The stand-in answers the single
  // invocation the generator makes (`git log -1 --format=%cs -- <page>`), so the real
  // `lastUpdated` seam — child process, stdout, empty-string fallback — is still what these
  // assertions drive; only the executable behind it is a fixture, and `gitBin` is how the
  // test points at it.
  const root = mkdtempSync(join(tmpdir(), "jf-docsnav-squash-"));
  const docsDir = join(root, "web", "src", "app", "docs");
  const page = join(docsDir, "shipping", "page.tsx");
  const standIn = join(root, "stand-in-git");
  const commitDateFile = join(root, "commit-date");
  const callLog = join(root, "commit-date-queries");
  const savedEnv = { date: process.env.FAKE_GIT_DATE, log: process.env.FAKE_GIT_LOG };
  // "the page's last commit moved to <date>" — the one thing a squash landing changes.
  const landingAt = (date: string) => writeFileSync(commitDateFile, `${date}\n`);
  const pageSource = (heading: string) =>
    [
      "export default function Page() {",
      "  return (",
      "    <div>",
      `      <h1>${heading}</h1>`,
      "      <p>Body.</p>",
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n");
  const layoutSource = [
    "const NAV_GROUPS = [",
    '  { layer: "Learn", label: "Start here", sections: [{ href: "/docs/shipping", label: "Shipping" }] },',
    "];",
    "",
  ].join("\n");
  const previous = (entries: Array<{ path: string; updated: string; digest: string }>) =>
    new Map(entries.map((entry) => [entry.path, { updated: entry.updated, digest: entry.digest }]));
  const registryWith = (previousEntries: Map<string, { updated: string; digest: string }>) =>
    buildRegistry({ docsDir, layoutSource, guides: [], gitCwd: root, gitBin: standIn, previousEntries });

  try {
    writeFileSync(
      standIn,
      [
        "#!/bin/sh",
        "# Stand-in for git in the docs-registry squash test. The generator makes exactly one",
        "# invocation, `git log -1 --format=%cs -- <page>`; the date it answers with comes from",
        "# $FAKE_GIT_DATE, which the test rewrites to play a squash landing. Every invocation is",
        "# appended to $FAKE_GIT_LOG so the test can prove the seam was exercised.",
        'printf \'%s\\n\' "$*" >> "$FAKE_GIT_LOG"',
        'if [ "$1" = "log" ] && [ "$2" = "-1" ]; then',
        '  cat "$FAKE_GIT_DATE"',
        "  exit 0",
        "fi",
        'echo "stand-in git: unsupported invocation: $*" >&2',
        "exit 1",
        "",
      ].join("\n"),
    );
    chmodSync(standIn, 0o755);
    process.env.FAKE_GIT_DATE = commitDateFile;
    process.env.FAKE_GIT_LOG = callLog;

    mkdirSync(join(docsDir, "shipping"), { recursive: true });
    writeFileSync(page, pageSource("Shipping by zone"));
    landingAt("2026-11-01");

    const edited = registryWith(new Map());
    const committed = renderNavSource(edited.entries);
    assert.equal(edited.entries[0].updated, "2026-11-01");
    assert.ok(existsSync(callLog), "the generator never asked for a commit date — the stand-in is not on the path it uses");
    assert.match(
      readFileSync(callLog, "utf8"),
      /^log -1 --format=%cs -- /m,
      "the generator did not ask the stand-in for the commit date, so the date above came from somewhere else",
    );

    // The landing: SAME page bytes, a new commit date — exactly what a squash produces.
    // Before the digest this moved the entry to 2026-11-09 and left every clean checkout of
    // `main` red for a page nobody had touched.
    landingAt("2026-11-09");
    assert.equal(
      execFileSync(standIn, ["log", "-1", "--format=%cs", "--", relative(root, page)], { cwd: root, encoding: "utf8" }).trim(),
      "2026-11-09",
      "the fixture no longer reports the landing date, so the assertion below would pass vacuously",
    );
    const landed = registryWith(previous(edited.entries));
    assert.equal(landed.entries[0].updated, "2026-11-01", "the landing commit re-dated a page whose bytes did not change");
    assert.equal(renderNavSource(landed.entries), committed, "a squash landing must not change the generated registry");

    // …and the drift the guard exists for is still caught: an edited page re-dates, and the
    // rendered registry differs until `npm run docs:nav` is run.
    writeFileSync(page, pageSource("Shipping by zone, revised"));
    landingAt("2026-11-10");
    const stale = registryWith(previous(landed.entries));
    assert.equal(stale.entries[0].updated, "2026-11-10", "an edited page kept its old date");
    assert.notEqual(renderNavSource(stale.entries), committed, "a page edit must make the committed registry stale");
  } finally {
    if (savedEnv.date === undefined) delete process.env.FAKE_GIT_DATE;
    else process.env.FAKE_GIT_DATE = savedEnv.date;
    if (savedEnv.log === undefined) delete process.env.FAKE_GIT_LOG;
    else process.env.FAKE_GIT_LOG = savedEnv.log;
    rmSync(root, { recursive: true, force: true });
  }
});

test("the nav table in layout.tsx and the registry agree", () => {
  // The sidebar renders NAV_GROUPS (docsIndex.test.ts reads it as source text and the
  // generator reads it as data); the registry is what breadcrumb/prev-next/search read.
  // If the two disagree, the sidebar and the reading order describe different trees.
  const source = readFileSync(LAYOUT_FILE, "utf8");
  const start = source.indexOf("const NAV_GROUPS");
  assert.ok(start !== -1, "layout.tsx no longer declares NAV_GROUPS");
  const end = source.indexOf("\n];", start);
  assert.ok(end !== -1, "could not find the end of NAV_GROUPS");
  const region = stripComments(source.slice(start, end));
  const rows: { href: string; label: string }[] = [];
  for (const match of region.matchAll(/\{([^{}]*)\}/g)) {
    const href = match[1].match(/href:\s*"([^"]+)"/);
    const label = match[1].match(/label:\s*"([^"]+)"/);
    if (href && label) rows.push({ href: href[1], label: label[1] });
  }
  assert.ok(rows.length >= 20, `parsed only ${rows.length} sidebar rows — the nav shape moved`);
  const products = DOCS_NAV.filter((entry) => entry.kind === "product");
  assert.deepEqual(
    products.map((entry) => entry.path),
    rows.map((row) => row.href),
    "the sidebar order and the registry's reading order disagree",
  );
  assert.deepEqual(
    products.map((entry) => entry.label),
    rows.map((row) => row.label),
    "a sidebar label and its registry label disagree",
  );
});

test("the committed registry and search index are fresh", () => {
  try {
    execFileSync(process.execPath, ["scripts/docsNav.mjs", "--check"], {
      cwd: WEB_DIR,
      stdio: ["ignore", "pipe", "pipe"],
      encoding: "utf8",
    });
  } catch (error) {
    const stderr = (error as { stderr?: string }).stderr ?? String(error);
    assert.fail(`the generated docs files are stale — run \`npm run docs:nav\`: ${stderr.trim()}`);
  }
});

function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/^[ \t]*\/\/.*$/gm, " ")
    .replace(/[ \t]\/\/.*$/gm, " ");
}
