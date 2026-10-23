// The profile display-name cap — the web half of the contract.
//
// THE DEFECT THIS GUARD EXISTS FOR. api/internal/httpapi/profile.go capped the
// display name with a BYTE slice (`name = name[:80]`). A cut landing inside a
// multi-byte rune produced invalid UTF-8, Postgres refused the whole UPDATE
// (SQLSTATE 22021), the handler answered 500 "internal error", and users.name
// kept its old value: the save silently did nothing. A name of 78 ASCII
// characters followed by one CJK character triggers it, and 80 CHARACTERS of
// non-ASCII text is 160–320 bytes — so normal non-English names hit a cap that
// was supposed to be 80 characters.
//
// WHAT IT PINS
//   1. The shared web constant equals the Go constant. Bumping one alone fails.
//   2. The profile name field carries that constant as its maxLength, so a person
//      cannot paste a name the server would have to cut (the field had NO cap,
//      which is how the reported name reached the handler at all).
//   3. The Go handler no longer byte-slices, and caps with the shared rune-safe
//      helper against the same constant.
//   4. After a successful save the field shows what the server stored, not the
//      raw text that was typed (`load()` keeps whatever is already in the input,
//      so a trim/truncation used to appear only on the next mount).
//
// WHAT IT DOES NOT COVER (stated honestly)
//   This repo has no JSX/DOM test runner, so this file asserts the wiring from
//   source — it does not type into the field, run React, or send a request. The
//   EXECUTED proof on the Go side is api/internal/httpapi/profile_name_cap_test.go,
//   which drives the real route against real Postgres and reads the stored row
//   back; the field's live maxLength and the post-save value are read from the
//   running page in the browser, reported on the card.
//
// Run with:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { PROFILE_NAME_MAX_CHARS, normalizeProfileName } from "./profileIdentity.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const goProfile = read("../../../api/internal/httpapi/profile.go");
const page = read("../app/profile/page.tsx");

// The comments in profile.go QUOTE the pre-fix byte slice to explain why it was
// wrong, so a raw source scan would match its own documentation. Assertions about
// the CODE therefore run over code only (whole-line `//` comments removed).
const codeOnly = (src: string) =>
  src
    .split("\n")
    .filter((line) => !line.trimStart().startsWith("//"))
    .join("\n");

test("the web cap equals the Go cap — one value, two languages", () => {
  // The Go constant is UNEXPORTED (`profileNameMaxChars`), so the scan is
  // case-tolerant rather than pinned to one casing — exporting it later must not
  // silently stop this arm from matching, and a rename must still be caught.
  const goConst = goProfile.match(/[Pp]rofileNameMaxChars\s*=\s*(\d+)/);
  assert.ok(goConst, "httpapi/profile.go no longer defines profileNameMaxChars");
  assert.equal(
    Number(goConst[1]),
    PROFILE_NAME_MAX_CHARS,
    `the Go display-name cap (${goConst[1]}) and the web cap (${PROFILE_NAME_MAX_CHARS}) disagree — a person ` +
      `would be allowed to type a name the server truncates, or stopped short of one it stores whole`,
  );
});

test("the profile name field carries the shared cap, not no cap and not a bare literal", () => {
  // The reported defect was reachable because the field had NO maxLength at all:
  // a pasted name went to the handler as-is and was byte-cut there.
  assert.match(
    page,
    /id="prof-name"[\s\S]{0,500}?maxLength=\{PROFILE_NAME_MAX_CHARS\}/,
    "the profile display-name field must cap at the shared constant — it shipped with no cap, " +
      "which is how an over-long name reached the handler",
  );
  assert.doesNotMatch(
    page,
    /id="prof-name"[\s\S]{0,500}?maxLength=\{80\}/,
    "the field went back to a bare literal — it must use the shared constant so the two halves cannot drift",
  );
  assert.match(
    page,
    /import \{[^}]*\bPROFILE_NAME_MAX_CHARS\b[^}]*\} from "\.\.\/\.\.\/lib\/profileIdentity"/,
    "the profile page does not import the shared constant",
  );
});

test("the Go handler caps on a rune boundary with the shared helper", () => {
  // A byte slice is the bug: it cuts the 80th byte even when it is the first
  // byte of a multi-byte rune, which Postgres then refuses (22021).
  assert.doesNotMatch(
    codeOnly(goProfile),
    /name\[:\s*\d+\]|name\[:\s*[A-Za-z_]*MaxChars\]/,
    "the display name is byte-sliced again — a multi-byte name would be cut mid-rune and the " +
      "UPDATE refused with SQLSTATE 22021",
  );
  assert.match(
    codeOnly(goProfile),
    /name := textutil\.Truncate\(strings\.TrimSpace\(req\.Name\), [A-Za-z_]*MaxChars\)/,
    "the display name must be capped with textutil.Truncate over strings.TrimSpace(req.Name) against " +
      "the shared constant (characters) — the same rule the other bounded free-text writes use",
  );
});

test("normalizeProfileName mirrors the handler's trim-then-truncate, per code point", () => {
  // The handler stores textutil.Truncate(strings.TrimSpace(req.Name), 80). The
  // field must show that same value after a save, or the page reports "Saved"
  // next to text the database does not hold.
  assert.equal(normalizeProfileName("  Wan Zulkarnain  "), "Wan Zulkarnain");
  assert.equal(normalizeProfileName(""), "");
  assert.equal(normalizeProfileName("   "), "");
  // 80 characters of non-ASCII are stored whole — this is the case the BYTE cap
  // silently shortened.
  const cjk = "名".repeat(PROFILE_NAME_MAX_CHARS);
  assert.equal(normalizeProfileName(cjk), cjk);
  // Beyond the cap it cuts on a code-point boundary, never mid-surrogate.
  const emoji = "😀".repeat(PROFILE_NAME_MAX_CHARS + 5);
  const cut = normalizeProfileName(emoji);
  assert.equal(Array.from(cut).length, PROFILE_NAME_MAX_CHARS);
  assert.equal(cut, "😀".repeat(PROFILE_NAME_MAX_CHARS));
});

test("a successful name save puts the stored value back in the field", () => {
  // Without this the field keeps the raw typed text: load() does
  // setName((cur) => cur || sessionUser.name || ""), so the server's trim and
  // truncation only appear on the next mount (card t_5d034df2, QA observation 2).
  assert.match(
    page,
    /if \(await post\("\/api\/auth\/profile\/name", \{ name \}\)\) setName\(normalizeProfileName\(name\)\)/,
    "after a successful name save the field must show what the server stored (trimmed and capped), " +
      'not the raw text the user typed — otherwise the page says "Saved" next to a value the database ' +
      "does not hold until the next mount",
  );
  assert.match(
    page,
    /import \{ PROFILE_NAME_MAX_CHARS, normalizeProfileName \} from "\.\.\/\.\.\/lib\/profileIdentity"/,
    "the profile page does not import normalizeProfileName",
  );
});
