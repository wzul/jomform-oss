#!/usr/bin/env node
// web/scripts/og-image-version.mjs — content-version the default social card.
//
// WHAT IT WRITES
//   1. `public/og-default-<hash>.png` — a byte-identical copy of
//      `public/og-default.png`, named after the sha256 of its OWN bytes.
//   2. `src/lib/ogImage.gen.ts` — the constant the page metadata reads.
//
// WHY A STEP AND NOT A HAND-EDITED CONSTANT
//   The defect this closes (card t_ae566270) was a card replaced in place at a
//   fixed URL, so every unfurl client kept its cached old preview. A hand-kept
//   hash constant would only move the same mistake one level down: the artwork
//   changes, nobody remembers the constant, and the URL does not move. Here the
//   URL is a function of the bytes, and it is a function of the bytes at BUILD
//   time — `prebuild`/`predev` in package.json run this before `next build` and
//   before the unit suite, so a stale hand-edited value cannot be what ships.
//
// WHAT IT DOES NOT DO
//   * It does not delete older `og-default-<hash>.png` copies. A page HTML that
//     an edge cache is still holding can name the previous hash, and deleting
//     that file would 404 the preview for everyone behind that cache. Cards
//     change rarely; keeping the old names costs one file each and is the whole
//     point of content addressing.
//   * It does not touch `og-default.png` itself. That file stays the source of
//     truth AND stays served, so a link shared before this change still resolves.
//
// USAGE
//   npm run og:image            write (idempotent — nothing changes when the
//                               card and the constant already agree)
//   npm run og:image -- --check  report drift and exit 1 instead of writing
//
// Run with the repo's node (the `node` floor is the one `npm run build` needs —
// type stripping, because the hash rule is shared with src/lib).

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  OG_IMAGE_SOURCE_FILE,
  ogImageDigest,
  ogImageFileName,
  ogImageHash,
} from "../src/lib/ogImageVersion.ts";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const SOURCE = join(WEB_ROOT, "public", OG_IMAGE_SOURCE_FILE);
const GEN_FILE = join(WEB_ROOT, "src", "lib", "ogImage.gen.ts");
const CHECK = process.argv.includes("--check");

/** Read the card, or refuse loudly — a missing card must not become a hash of "". */
function readCard() {
  if (!existsSync(SOURCE)) {
    throw new Error(
      `the default social card is missing: ${SOURCE}. This script derives the og:image URL from ` +
        `that file's bytes, so an absent file cannot be versioned — restore it, do not skip this step.`,
    );
  }
  return readFileSync(SOURCE);
}

function genSource(hash, file, digest, bytesLength) {
  return `// GENERATED FILE — DO NOT EDIT.
//
// The default social card at public/${file} is a byte-identical copy of
// public/${OG_IMAGE_SOURCE_FILE}, and its name carries the card's content hash, so the URL in the
// page metadata changes whenever the artwork changes. An unfurl client caches per
// URL (Telegram per client, WhatsApp ~7 days, X per URL), so without the version
// component a client that cached the old card kept rendering it after the bytes
// had changed even though the origin served the new file — card t_ae566270.
//
// Regenerate:  cd web && npm run og:image
// Guard:       src/lib/ogImage.test.ts re-hashes the file and fails on drift.
//
// sha256   ${digest}
// bytes    ${bytesLength}

export const OG_IMAGE_HASH = ${JSON.stringify(hash)};
export const OG_IMAGE_FILE = ${JSON.stringify(file)};
`;
}

function main() {
  const bytes = readCard();
  const digest = ogImageDigest(bytes);
  const hash = ogImageHash(bytes);
  const file = ogImageFileName(hash);
  const versionedPath = join(WEB_ROOT, "public", file);
  const nextGen = genSource(hash, file, digest, bytes.length);
  const currentGen = existsSync(GEN_FILE) ? readFileSync(GEN_FILE, "utf8") : null;
  const copyMatches = existsSync(versionedPath) && readFileSync(versionedPath).equals(bytes);
  const genMatches = currentGen === nextGen;

  if (genMatches && copyMatches) {
    console.log(`og:image ok — public/${file} (sha256 ${digest.slice(0, 16)}…, ${bytes.length} bytes)`);
    return;
  }

  const drift = [
    genMatches ? null : `src/lib/ogImage.gen.ts does not name ${file}`,
    copyMatches ? null : `public/${file} is missing or differs from public/${OG_IMAGE_SOURCE_FILE}`,
  ].filter(Boolean);

  if (CHECK) {
    console.error(
      `og:image STALE — ${drift.join("; ")}. The metadata would name a card the build does not ` +
        `serve, or serve a card at a URL no client has been told about. Run: cd web && npm run og:image`,
    );
    process.exit(1);
  }

  mkdirSync(dirname(versionedPath), { recursive: true });
  writeFileSync(versionedPath, bytes);
  writeFileSync(GEN_FILE, nextGen);
  console.log(`og:image wrote public/${file} and src/lib/ogImage.gen.ts`);
  for (const line of drift) console.log(`  · ${line}`);
}

try {
  main();
} catch (err) {
  console.error(`og:image FAILED — ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
}
