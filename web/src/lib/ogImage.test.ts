// og:image — the default social card must be named by its CONTENT, not by a
// fixed path.
//
// THE DEFECT THIS GUARDS (card t_ae566270)
//   Every metadata block named `https://jomform.com/og-default.png` while the
//   file behind it was replaced in place (a4883d20: 70,643 bytes → 143,664
//   bytes). Origin and edge both served the new bytes; the preview stayed old
//   for anyone whose client had already cached that URL, because unfurl clients
//   cache per URL (Telegram per client, WhatsApp ~7 days, X per URL). Nothing on
//   the server was wrong, so no re-deploy could fix it — the URL had to move.
//
// WHAT IS ASSERTED HERE
//   1. The name in the metadata is a function of the bytes on disk: the
//      committed constant is re-derived from `public/og-default.png` in this
//      run, and a mismatch turns the suite red with the command that fixes it.
//   2. The versioned file exists, ships in `public/`, and is byte-identical to
//      the source — the URL a client is told about is a file the build serves.
//   3. The unversioned `og-default.png` still exists, so a link shared before
//      this change still resolves to a real image.
//   4. No page under `src/app` names the URL itself: one constant, or the next
//      card change silently reaches five pages and misses the sixth.
//
// WHAT IT CANNOT SEE
//   It reads the tree, not a built server: it cannot prove Next emits the value
//   in `<meta property="og:image">`. That half is checked by rendering a built
//   page and reading the DOM (and `docs:facts` already pins the built-HTML
//   reader for docs). A green run here plus a stale built page is still caught —
//   `prebuild` regenerates the constant before `next build` runs.

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  OG_IMAGE_FILE,
  OG_IMAGE_HASH,
  OG_IMAGE_URL,
  OG_IMAGE_WIDTH,
  OG_IMAGE_HEIGHT,
  OG_IMAGE_ALT,
} from "./ogImage.ts";
import {
  OG_IMAGE_HASH_CHARS,
  OG_IMAGE_SOURCE_FILE,
  ogImageDigest,
  ogImageFileName,
  ogImageHash,
} from "./ogImageVersion.ts";
import { SITE_URL } from "./siteName.ts";

const LIB_DIR = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(LIB_DIR, "..", ".."); //            <repo>/web
const PUBLIC_DIR = join(WEB_DIR, "public");
const APP_DIR = join(WEB_DIR, "src", "app");

/** The card as it is on disk right now — the oracle for everything below. */
const CARD = readFileSync(join(PUBLIC_DIR, OG_IMAGE_SOURCE_FILE));

/** Every file under `dir`, following no symlinks. */
function walk(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full));
    else out.push(full);
  }
  return out;
}

/** The metadata blocks that publish an og:image, as repo-relative paths. */
const METADATA_BLOCKS = [
  "src/app/page.tsx",
  "src/app/mcp/page.tsx",
  "src/app/docs/layout.tsx",
  "src/app/legal/terms/page.tsx",
  "src/app/legal/privacy/page.tsx",
  "src/app/brand/page.tsx",
];

test("the hash rule is sha256 over the bytes, truncated to the documented width", () => {
  // A KNOWN VECTOR, because the drift test below compares the constant against
  // this same helper: if the helper were wrong, both sides would agree and the
  // pair would be green about nothing. sha256("abc") is the FIPS 180-2 vector.
  assert.equal(ogImageDigest(Buffer.from("abc")), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
  assert.equal(ogImageHash(Buffer.from("abc")), "ba7816bf8f01");
  assert.equal(OG_IMAGE_HASH_CHARS, 12);
  assert.equal(
    ogImageFileName("0123456789ab"),
    "og-default-0123456789ab.png",
    "the versioned name must carry the hash and keep the extension a client sniffs",
  );
});

test("a changed card byte yields a different URL — the whole point of the card", () => {
  // The defect was a name that did NOT move when the bytes moved. Flipping one
  // bit has to move it, or every assertion below is decoration.
  const before = ogImageFileName(ogImageHash(CARD));
  const mutated = Buffer.from(CARD);
  mutated[mutated.length - 1] ^= 0x01;
  const after = ogImageFileName(ogImageHash(mutated));
  assert.notEqual(after, before, "flipping one byte of the card left the og:image URL unchanged");
  assert.match(after, /^og-default-[0-9a-f]{12}\.png$/);
  assert.match(before, /^og-default-[0-9a-f]{12}\.png$/);
});

test("a hand-written version fails loudly instead of becoming a path", () => {
  for (const bad of ["", "2f1062e1", "2F1062E183C8", "2f1062e183c8.png", "../og-default"]) {
    assert.throws(() => ogImageFileName(bad), /not 12 lower-case hex/, `accepted ${JSON.stringify(bad)}`);
  }
});

test("the committed constant is the content hash of the card on disk", () => {
  const expectedHash = ogImageHash(CARD);
  const expectedFile = ogImageFileName(expectedHash);
  assert.equal(
    OG_IMAGE_HASH,
    expectedHash,
    `src/lib/ogImage.gen.ts names ${OG_IMAGE_HASH} but public/${OG_IMAGE_SOURCE_FILE} hashes to ` +
      `${expectedHash} (${CARD.length} bytes). The card was replaced without regenerating the URL — ` +
      `which is exactly the defect card t_ae566270 was filed for: the metadata would keep naming the ` +
      `old URL while the bytes changed. Run: cd web && npm run og:image`,
  );
  assert.equal(OG_IMAGE_FILE, expectedFile);
});

test("the URL a client is told about is a file this build actually serves", () => {
  // Derive the path back out of the published URL rather than trusting the
  // constant a second time — this is the end of the wire the client sees.
  assert.equal(SITE_URL, "https://jomform.com/");
  assert.equal(OG_IMAGE_URL, `https://jomform.com/${OG_IMAGE_FILE}`);
  assert.match(OG_IMAGE_URL, /^https:\/\/jomform\.com\/og-default-[0-9a-f]{12}\.png$/);
  assert.notEqual(
    OG_IMAGE_URL,
    `https://jomform.com/${OG_IMAGE_SOURCE_FILE}`,
    "the metadata is back on the unversioned URL — a cached client would never be forced to the new card",
  );

  const rel = OG_IMAGE_URL.slice("https://jomform.com/".length);
  const served = join(PUBLIC_DIR, rel);
  assert.ok(existsSync(served), `${rel} is named in the metadata but missing from web/public`);
  assert.ok(
    readFileSync(served).equals(CARD),
    `${rel} ships different bytes than public/${OG_IMAGE_SOURCE_FILE} — the version would be a lie`,
  );
});

test("the unversioned card stays served, so links already shared still resolve", () => {
  assert.ok(
    existsSync(join(PUBLIC_DIR, OG_IMAGE_SOURCE_FILE)),
    `public/${OG_IMAGE_SOURCE_FILE} was deleted. It is the source the version is derived from AND ` +
      `the path shared links already point at: keep it, version a copy instead of renaming it.`,
  );
});

test("the card's unfurl contract is unchanged (1200x630, labelled)", () => {
  assert.equal(OG_IMAGE_WIDTH, 1200);
  assert.equal(OG_IMAGE_HEIGHT, 630);
  assert.equal(OG_IMAGE_ALT, "JomForm");
  assert.ok(OG_IMAGE_ALT.trim().length > 0);
});

test("every metadata block reads the shared constant, and none writes the URL out again", () => {
  const seen: string[] = [];
  for (const rel of METADATA_BLOCKS) {
    const src = readFileSync(join(WEB_DIR, rel), "utf8");
    seen.push(rel);
    assert.match(
      src,
      /import \{[^}]*\bOG_IMAGE\b[^}]*\} from "(?:\.\.\/)+lib\/ogImage";/,
      `${rel} does not import the og:image constant from lib/ogImage`,
    );
    assert.ok(src.includes("images: [OG_IMAGE]"), `${rel} openGraph.images does not use OG_IMAGE`);
    assert.ok(src.includes("images: [OG_IMAGE_URL]"), `${rel} twitter.images does not use OG_IMAGE_URL`);
    assert.equal(
      /["'`]og-default/.test(src),
      false,
      `${rel} still spells the card path out as a string — that is how six metadata blocks drift ` +
        `apart. Use OG_IMAGE / OG_IMAGE_URL from src/lib/ogImage.ts.`,
    );
  }
  assert.deepEqual(seen, METADATA_BLOCKS);

  // The rule has to hold for pages that do not exist yet, so sweep the whole
  // app tree: no quoted og-default path may appear anywhere under src/app.
  const scan = walk(APP_DIR);
  assert.ok(scan.length >= 50, `walked only ${scan.length} files under src/app — the sweep found nothing to judge`);
  const offenders = scan
    .filter((f: string) => /\.(ts|tsx|js|jsx|mjs)$/.test(f))
    .filter((f: string) => /["'`]og-default/.test(readFileSync(f, "utf8")))
    .map((f: string) => relative(WEB_DIR, f));
  assert.deepEqual(offenders, [], `these files spell the card path out as a string: ${offenders.join(", ")}`);
});
