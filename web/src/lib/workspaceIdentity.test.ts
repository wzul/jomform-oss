// The workspace NAME cap — one value, written in two languages, in one unit.
//
// THE DEFECT THIS GUARD EXISTS FOR. The server checked the cap with
// `len(newName) > 120`, which counts BYTES, while the dashboard's name field
// carried `maxLength={120}`, which counts UTF-16 CODE UNITS. The field therefore
// allowed names the API refused — 120 accented characters showed as 120/120 with
// Save enabled, then the save failed. The create form had no cap at all, so it
// accepted an unbounded name and only found out from a failed request. And the
// refusal was a fresh error that reached no arm of the transports' switches, so
// what the merchant actually saw was `500 {"error":"internal error"}`.
//
// All of that is fixed in Go (dbstore.ValidateWorkspaceName, called from both
// create paths and from rename). THIS file is the web half of the contract, and
// it is read CROSS-LANGUAGE on purpose — the same idiom as langPlacement.test.ts
// and formPublicURL.test.ts — because the invariant is one value living in two
// languages, and a change on either side is otherwise invisible from the other.
//
// WHAT IT PINS
//   1. The shared web constant equals the Go constant. Bumping one alone fails.
//   2. Both workspace-name fields (rename and create) use the shared constant,
//      so neither can drift back to a bare literal — and the create field, which
//      had NO cap, can never lose it again.
//   3. The Go rule is still CHARACTER-counted and is the ONE rule both create
//      and rename call. A revert to `len(` or a second copy fails here.
//   4. The refusal is a SENTINEL reachable by the transports' switches, so it
//      cannot go back to being an unmapped 500.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   This repo has no JSX/DOM test runner (see src/lib/i18n.test.ts), so this file
//   asserts the wiring from source — it does not type into the field, run React,
//   or send a request. The EXECUTED proof is on the Go side:
//   dbstore/workspace_name_cap_test.go, httpapi/workspace_name_cap_rest_test.go
//   and mcpsrv/workspace_name_cap_mcp_test.go drive real Postgres and a real
//   transport, and this card's handoff reports the browser reading of the field.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { WORKSPACE_NAME_MAX_CHARS } from "./workspaceIdentity.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const goWorkspace = read("../../../api/internal/dbstore/workspace.go");
const goRest = read("../../../api/internal/httpapi/rest_workspaces.go");
const goMcp = read("../../../api/internal/mcpsrv/workspaces.go");

const workspaceTab = read("../components/WorkspaceTab.tsx");
const switcher = read("../components/WorkspaceSwitcher.tsx");

test("the web cap equals the Go cap — one value, two languages", () => {
  const goConst = goWorkspace.match(/WorkspaceNameMaxChars\s*=\s*(\d+)/);
  assert.ok(goConst, "dbstore/workspace.go no longer defines WorkspaceNameMaxChars");
  assert.equal(
    Number(goConst[1]),
    WORKSPACE_NAME_MAX_CHARS,
    `the Go name cap (${goConst[1]}) and the web cap (${WORKSPACE_NAME_MAX_CHARS}) disagree — a merchant ` +
      `would be allowed to type a name the server refuses, or stopped short of one it accepts`,
  );
});

test("both workspace-name fields use the shared constant, not a bare literal", () => {
  // The rename field carried a bare 120; the create field had no cap at all.
  // Both halves are asserted so neither can regress alone.
  assert.match(
    workspaceTab,
    /id="ws-rename-name"[\s\S]{0,400}?maxLength=\{WORKSPACE_NAME_MAX_CHARS\}/,
    "the Settings → Workspace name field must cap at the shared constant",
  );
  assert.doesNotMatch(
    workspaceTab,
    /id="ws-rename-name"[\s\S]{0,400}?maxLength=\{120\}/,
    "the rename field went back to a bare 120 — it must use the shared constant so the two fields cannot drift",
  );
  assert.match(
    switcher,
    /aria-label=\{t\("workspace\.name"\)\}[\s\S]{0,400}?maxLength=\{WORKSPACE_NAME_MAX_CHARS\}/,
    "the switcher's create form must cap the name too — it shipped with no cap, so a merchant could type " +
      "an unbounded name and only found out from a failed request",
  );
  // And both fields import it rather than re-declaring a number.
  assert.match(workspaceTab, /import \{ WORKSPACE_NAME_MAX_CHARS \} from "\.\.\/lib\/workspaceIdentity"/);
  assert.match(switcher, /import \{ WORKSPACE_NAME_MAX_CHARS \} from "\.\.\/lib\/workspaceIdentity"/);
});

test("the Go rule counts CHARACTERS, not bytes, and only judges a supplied name", () => {
  // The unit. `len(newName) > 120` was the reported defect: 45 CJK characters is
  // 135 bytes and was refused, while the field the user typed into allowed it.
  assert.match(
    goWorkspace,
    /utf8\.RuneCountInString\(n\)\s*>\s*WorkspaceNameMaxChars/,
    "ValidateWorkspaceName must count characters (utf8.RuneCountInString) — a byte count is stricter " +
      "than the field the merchant types into",
  );
  assert.doesNotMatch(
    goWorkspace,
    /len\(newName\)\s*>\s*120/,
    "the byte-based name check is back",
  );
  // An OMITTED name must not be judged by the cap it is not changing: the check
  // runs on in.Name, not on the cur.Name fallback.
  const rename = goWorkspace.slice(goWorkspace.indexOf("func (s *WorkspaceStore) Rename("));
  assert.match(
    rename,
    /if newName == "" \{\s*newName = cur\.Name\s*\} else \{/,
    "Rename must apply the cap only to a name the caller SUPPLIED — checking the cur.Name fallback is " +
      "what made a slug-only PATCH on a long-named workspace impossible",
  );
  assert.match(rename, /ValidateWorkspaceName\(in\.Name\)/);
});

test("create and rename call the SAME name rule, on both transports", () => {
  // The asymmetry the card was written to remove: create checked only `name == ""`.
  for (const [file, label] of [
    [goRest, "httpapi/rest_workspaces.go"],
    [goMcp, "mcpsrv/workspaces.go"],
  ]) {
    assert.match(
      file,
      /dbstore\.ValidateWorkspaceName\(in\.Name\)/,
      `${label}'s create path does not call the shared name rule — the cap would be enforced on one of ` +
        `two write paths, which is exactly the create/rename asymmetry`,
    );
    assert.match(
      file,
      /errors\.Is\(err, dbstore\.ErrWorkspaceNameLength\)/,
      `${label} does not classify the name refusal — an unclassified error reaches the default arm and ` +
        `the merchant gets 500 "internal error" for a name that is merely too long`,
    );
  }
  assert.match(
    goWorkspace,
    /ErrWorkspaceNameLength\s*=\s*errors\.New\(/,
    "ErrWorkspaceNameLength must be a SENTINEL — a fresh errors.New is what no switch could match",
  );
});

test("the switcher's create form reports the NAME's problem, not a guessed address one", () => {
  // The defect: the create form mapped every 400 to "Slug must be 3-40 chars…",
  // so a too-long NAME was reported as a broken ADDRESS.
  //
  // HOW IT IS FIXED NOW, and why this guard changed shape. The form no longer
  // reads the response itself — it delegates to the shared helper
  // (lib/workspace.createWorkspace), which is the ONE place both create surfaces
  // meet. That helper decodes the server's machine `code` and returns an i18n
  // KEY; the form renders it.
  //
  // The old form of this assertion pinned an inline `d.error` read in the
  // switcher. That inline read is now FORBIDDEN — lib/apiErr.test.ts and
  // lib/workspaceCreateErr.test.ts both fail the build on any call site that
  // renders a failure body without translating it (issue #6177), because the
  // API's prose is Malay and an English-locale merchant would read it. Pinning
  // the old shape here would pin the untranslated-prose defect, so the
  // assertion moves to the property that has to survive the refactor: an
  // over-long name is answered with a sentence about the NAME.
  assert.match(
    switcher,
    /t\(r\.errorKey\)/,
    "the create form does not render the key the shared helper returned — a too-long name would " +
      "show nothing, or fall back to the address message",
  );
  assert.doesNotMatch(
    switcher,
    /d\??\.error/,
    "the create form reads the API's `error` prose again — that string is Malay and is meant for " +
      "curl/MCP callers (#6177)",
  );
  // The name's own code must be REACHABLE end to end: the dictionary key that
  // apiErr maps the code onto, and the sentence it renders. Without both, the
  // helper's decode lands on the address sentence and the original defect is
  // back — with the guard above still passing.
  assert.match(
    read("../lib/apiErr.ts"),
    /"workspace\.nameTooLong":\s*"apiErr\.workspaceNameTooLong"/,
    "apiErr does not map the name-cap code — a too-long name is reported as an invalid address",
  );
  assert.match(
    read("../components/i18nDict.ts"),
    /"apiErr\.workspaceNameTooLong":\s*\{/,
    "the name-cap dictionary key has no sentence, so the merchant would see a raw key",
  );
});
