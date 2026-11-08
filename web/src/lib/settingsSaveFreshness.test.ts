// Structural net for card t_818349ab: after a successful Save, Settings kept
// showing the OLD state until the page was reloaded.
//
// THE DEFECT
//   `web/src/app/settings/page.tsx` has two refresh functions with confusingly
//   similar names:
//
//     * `load()`        APPLIES the session bundle this render closed over;
//     * `reload()`      drops the shared cache and RE-FETCHES the bundle.
//
//   Every successful write ended with `load()` — evaluated in the handler's
//   closure, i.e. with the PRE-write bundle. It therefore wrote the old payload
//   straight back over the page's state, and nothing on the page was ever going
//   to correct it: `save()` put back the pre-save `chip_status` (badge stuck on
//   NOT SET after the merchant entered a working key — the reported bug),
//   `clearCreds()` put back the pre-clear one (badge stuck on ACTIVE · TEST after
//   Disconnect), and the webhook writers put back the pre-write list (a newly
//   added hook missing until a reload).
//
// WHY THIS FILE EXISTS ALONGSIDE THE BROWSER SPEC
//   The behavioural proof is `web/e2e/settings-save-reflects-status.spec.ts`,
//   which drives the real page in a real browser and reads the rendered badge
//   with no reload — measured RED (3 failed) on the pre-fix tree and GREEN
//   (3 passed) after. That spec needs a production build; this file runs in the
//   `web` gate's unit half in milliseconds and pins the RULE so the next writer
//   who reaches for `load()` after a write finds it stated, with a failure
//   message that names the replacement. Neither replaces the other.
//
// WHAT IT PINS
//   1. Each write handler refreshes through `reload()`, never `load()`.
//   2. The CHIP writers publish the SERVER'S answer into the badge state
//      (`setStatus(`) instead of waiting for someone else to re-derive it.
//   3. `load()` has exactly ONE call site left — the mount effect — so a new
//      `load()` in a write path (or a retry button) is a deliberate edit here,
//      not an accident.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const settings = readFileSync(
  fileURLToPath(new URL("../app/settings/page.tsx", import.meta.url)),
  "utf8",
);

/**
 * Drop comments: these checks are about CODE, and the fix's own comments name
 * `load()` and `reload()` in prose — a naive substring test would match the
 * explanation instead of the call.
 */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, "") // block + JSX {/* … */} comments
    .split("\n")
    .map((line) => line.replace(/(^|[^:])\/\/.*$/, "$1")) // `// …` (not `http://`)
    .join("\n");
}

/**
 * The body of `signature` (an exact prefix such as `async function save(`),
 * delimited by brace matching — so the assertions below are about THAT handler
 * and not about a neighbour's copy of the same call.
 */
function bodyOf(src: string, signature: string): string {
  const at = src.indexOf(signature);
  assert.ok(at >= 0, `settings/page.tsx no longer defines \`${signature}…\` — update this guard`);
  const open = src.indexOf("{", at);
  let depth = 0;
  for (let i = open; i < src.length; i += 1) {
    if (src[i] === "{") depth += 1;
    else if (src[i] === "}") {
      depth -= 1;
      if (depth === 0) return src.slice(open, i + 1);
    }
  }
  assert.fail(`unbalanced braces after \`${signature}…\``);
}

const code = stripComments(settings);

const WRITE_HANDLERS = [
  "async function save(e: React.FormEvent)", // CHIP credentials
  "async function clearCreds()", // CHIP disconnect
  "async function addWebhook(e: React.FormEvent)",
  "async function saveWebhook(e: React.FormEvent)",
];

test("every successful write refreshes by RE-FETCHING, never by re-applying the cached bundle", () => {
  for (const signature of WRITE_HANDLERS) {
    const body = bodyOf(code, signature);
    assert.match(
      body,
      /\breload\(\)/,
      `\`${signature}\` must call reload() after a successful write — load() re-applies the bundle the handler closed over, which is the PRE-write payload (card t_818349ab)`,
    );
    assert.doesNotMatch(
      body,
      /\bload\(\)/,
      `\`${signature}\` still calls load(): that re-applies the stale bundle and the page keeps showing the pre-write state until a manual reload`,
    );
  }
});

test("the webhook delete control refuses the stale re-apply too", () => {
  const at = code.indexOf("/api/rest/webhooks/${wh.id}");
  assert.ok(at > 0, "the webhook delete control moved — update this guard");
  const line = code.slice(at, code.indexOf("</button>", at));
  assert.match(line, /\breload\(\)/, "deleting a webhook must reload(), not load()");
  assert.doesNotMatch(line, /\bload\(\)/, "deleting a webhook still re-applies the pre-delete bundle");
});

test("the CHIP writers publish the server's answer into the badge state", () => {
  for (const signature of ["async function save(e: React.FormEvent)", "async function clearCreds()"]) {
    const body = bodyOf(code, signature);
    assert.match(
      body,
      /setStatus\(/,
      `\`${signature}\` must set the badge state from the write's own answer — otherwise the badge only moves on the next bundle read`,
    );
  }
  // The save path trusts the API's `status`/`mode`/`verified`, not a guess.
  const save = bodyOf(code, "async function save(e: React.FormEvent)");
  assert.match(save, /d\.status/, "save() should read the PUT's `status` field");
  assert.match(save, /d\.mode/, "save() should read the PUT's `mode` field");
  assert.match(save, /verified/, "save() should keep the PUT's `verified` verdict");
});

test("the mount effect is the ONLY caller of the apply-from-cache helper", () => {
  const calls = code.match(/\bload\(\)/g) || [];
  assert.equal(
    calls.length,
    1,
    `expected exactly one load() call site (the mount effect) — found ${calls.length}. A write handler that calls load() re-applies the stale bundle (card t_818349ab).`,
  );
  assert.match(
    code,
    /useEffect\(\(\) => \{\s*load\(\);\s*\}, \[load\]\);/,
    "the one remaining load() call should be the mount/apply effect",
  );
});
