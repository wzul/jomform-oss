// Unit guard — no dot-prefixed FILE may ship under web/public.
//
// WHY THIS EXISTS (the defect it was written for: card t_92b520d1)
//   `GET https://jomform.com/.gitkeep` answered **500 Internal Server Error**,
//   not 404, for as long as the release images carried it. The file was a 0-byte
//   placeholder tracked at `web/public/.gitkeep`; `web/Dockerfile` copies the
//   web tree into the build stage (`COPY web/ ./`) and the runtime stage takes
//   `COPY --from=build /repo/web/public ./public`, so it became
//   `/app/public/.gitkeep` in every image, and the Next standalone server
//   refused it — a public 5xx on a path any scanner can request.
//
//   WHERE THE 500 COMES FROM (read out of the shipped image, next 16.3.8)
//     Next builds its `publicFolderItems` set from a walk of `public/` that
//     INCLUDES dot-prefixed names, so `/.gitkeep` matches a public file and is
//     handed to the static handler (`next/dist/server/serve-static.js` →
//     `send`). `send` then refuses the path: its `dotfiles` option defaults to
//     `undefined`, which makes it act as `ignore` when the FINAL path segment
//     starts with a dot, and it errors 404. Next's router only accepts
//     400/412/416 back from `serveStatic` (`router-server.js`, the
//     `POSSIBLE_ERROR_CODE_FROM_SERVE_STATIC` set — 404 is commented out on
//     purpose, "we will use Next.js built-in 404 page"), so the 404 is
//     normalised to 400 and re-rendered as an error page; that render fails and
//     the request falls through to the bare `res.end('Internal Server Error')`.
//     Nothing is logged, so the 5xx is silent on the host.
//
//   THE RULE IS ABOUT THE FINAL SEGMENT, NOT ABOUT ANY DOT SEGMENT — measured
//   on `jomform-web:v1.21.246` with a bind-mounted `public/`:
//     /.gitkeep                    500     dot-prefixed FILE at the root
//     /probe/.dotfile              500     dot-prefixed FILE one level down
//     /.dotfile-root               500     dot-prefixed FILE at the root
//     /.well-known/security.txt    200     dot-prefixed DIRECTORY, plain file
//     /.well-known                 404     bare directory: not a public item
//     /probe/ok.txt                200     the control
//   Same image with `/app/public/.gitkeep` removed from the tree: `/.gitkeep`
//   → 404, everything else unchanged. So a dot-prefixed DIRECTORY is fine
//   (`.well-known/` is a legitimate future use and must stay allowed) and a
//   dot-prefixed FILE is the thing that turns a would-be 404 into a 500.
//
// WHAT THIS GUARD DOES NOT DO
//   It reads the working tree, not an image. The runtime `public/` is the build
//   stage's `web/public` verbatim (`COPY --from=build /repo/web/public
//   ./public`), so a tree with no dot-file cannot produce an image with one —
//   but this test cannot see a file that something ELSE adds inside the image.
//
// Run with:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const LIB_DIR = dirname(fileURLToPath(import.meta.url));
const PUBLIC_DIR = resolve(LIB_DIR, "..", "..", "public"); // <repo>/web/public

/**
 * Relative POSIX paths of every dot-prefixed REGULAR FILE under `root`.
 *
 * Directories are walked into and never reported: `/.well-known/security.txt`
 * is served 200 (measured, see the header), so a dot-named directory is not a
 * defect and banning it would forbid a legitimate file. Symlinks and other
 * non-file entries are skipped — Next's walk of `public/` does not serve them
 * either, and this guard is about what the static handler will be handed.
 */
function dotFilesUnder(root: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const abs = join(root, entry.name);
    if (entry.isDirectory()) {
      found.push(...dotFilesUnder(abs).map((rel) => `${entry.name}/${rel}`));
      continue;
    }
    if (!entry.isFile()) continue;
    if (entry.name.startsWith(".")) found.push(relative(root, abs).split(sep).join("/"));
  }
  return found.sort();
}

test("dotFilesUnder reports dot-prefixed FILES only, never a dot-prefixed directory", () => {
  const root = mkdtempSync(join(tmpdir(), "jf-public-dotfiles-"));
  try {
    mkdirSync(join(root, "brand"), { recursive: true });
    mkdirSync(join(root, "nested"), { recursive: true });
    mkdirSync(join(root, ".well-known"), { recursive: true });
    writeFileSync(join(root, ".gitkeep"), "");
    writeFileSync(join(root, "brand", "icon.svg"), "<svg/>");
    writeFileSync(join(root, ".well-known", "security.txt"), "Contact: mailto:x@example.com\n");
    writeFileSync(join(root, "nested", ".DS_Store"), "");
    writeFileSync(join(root, "nested", "ok.txt"), "ok\n");

    // `.well-known/security.txt` is deliberately absent from the result: the
    // dot is on a DIRECTORY, and that path is served 200. If this helper ever
    // starts reporting it, the real-tree assertion below would forbid a working
    // public path.
    assert.deepEqual(
      dotFilesUnder(root),
      [".gitkeep", "nested/.DS_Store"],
      "expected the two dot-prefixed FILES and not the dot-prefixed directory",
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("web/public ships no dot-prefixed file (each one answers 500 instead of 404)", () => {
  // Non-vacuity: a walk that cannot see the tree would report "no dotfiles" and
  // pass for the wrong reason, so require two files that are certainly there.
  for (const required of ["robots.txt", "llms.txt"]) {
    assert.ok(
      existsSync(join(PUBLIC_DIR, required)),
      `web/public/${required} is missing — this walk found nothing to judge, so a green ` +
        `result here would prove nothing`,
    );
  }

  const offenders = dotFilesUnder(PUBLIC_DIR);
  assert.deepEqual(
    offenders,
    [],
    `web/public ships ${offenders.length} dot-prefixed file(s): ${offenders.join(", ")}. ` +
      `Next matches each of them as a public item and the static handler then refuses it, ` +
      `and that refusal surfaces as HTTP 500 (not 404) at https://jomform.com/<path> — ` +
      `the defect of card t_92b520d1. There is no reason to ship one: the directory already ` +
      `holds robots.txt, llms.txt, llms-full.txt, openapi.json, og-default.* and brand/, so a ` +
      `placeholder file is not needed to keep it in git. Delete the file (a dot-prefixed ` +
      `DIRECTORY such as .well-known/ is fine and is not reported here).`,
  );
});
