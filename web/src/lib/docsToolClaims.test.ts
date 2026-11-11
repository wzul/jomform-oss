// The docs pages that name MCP tools — "no shipped page presents a tool that
// is not registered as one".
//
// WHY THIS EXISTS (the bug it was written for): v1.12.0 shipped
// web/src/app/docs/chat/page.tsx claiming the chat's proposal lane "exists over
// MCP as the propose_write tool", and its "Related:" footer listed
// propose_write beside submit_feedback as if both were entries of the linked
// MCP tool reference. propose_write is deliberately NOT registered as an MCP
// tool (MCP-PARITY.md, "In-app assistant — the proposal lane"; the confirm step
// is a session-only route, so a registered version would hand every MCP caller a
// token it could never spend, and api/internal/mcpsrv/chat_tools_guard_test.go
// fails if it is ever registered). The code was right and the COPY was wrong —
// which is exactly the class of defect nothing on the Go side can see.
//
// WHAT IT GUARDS:
//   1. A name the docs CALL a tool must be a name the Go registry actually registers.
//      Five phrasings, named for the name shape they carry: "as the X tool" / "X, the
//      tool that …" in both a strict form (X is snake_case, no stoplist needed) and a
//      loose one (X is any lower-case identifier, held off ordinary prose by a CLOSED
//      class of English determinatives and pronouns excluded by rule — FUNCTION_WORDS —
//      plus the measured residue in PROSE_WORDS), plus the parenthetical form "… the MCP
//      tool <phrase> (X)", which is strict — X must be snake_case, and this repo's card-id
//      spelling is skipped as a
//      citation — because in a bracket position a bare English word and a name are
//      structurally identical (card t_3926a5dd; the shape's own note below carries the
//      measurement). The parenthetical is the phrasing the page's own HEADER COMMENT
//      shipped the false claim in, and a comment is in scope because this guard reads
//      whole file text.
//   2. The two exact sentences that shipped the false claim cannot come back.
//   3. The chat page keeps saying the lane is in-app / NOT an MCP tool, so a
//      silent deletion of the correction fails here.
//   4. MCP-PARITY.md still records the decision as NOT registered — the page's
//      claim and the parity file have to agree, or one of them is lying.
//   5. Every `{ name: … }` entry of the MCP tool reference (/docs/mcp) is a
//      registered tool, so that page cannot advertise a tool that does not exist
//      (the reverse direction of 1).
//   6. The loose shapes' prose exclusions cannot hide a registered tool: no entry of the
//      rule's closed class (FUNCTION_WORDS) or of the measured stoplist (PROSE_WORDS) may
//      be a registered name, the stoplist stays small enough to audit, and the class stays
//      closed against the ordinary English a written corpus puts in its slot.
//
// HOW THE REGISTRY IS READ, and why it is not a hardcoded list: the tool
// registry lives in Go, so a hardcoded name list here would be a second,
// silently-drifting copy of it — the same failure mode as the bug. It is derived
// from the source instead, cross-language, in the idiom of salesRows.test.ts /
// spamJudgeToggle.test.ts.
//
// THE SCOPE IS THE WHOLE `api/internal/mcpsrv` PACKAGE, not `tools.go`. That
// distinction is this guard's own measured defect: it read `tools.go` alone, the
// file the registry lived in for years, until a refactor moved one tool's
// literal out of it (disable_2fa's constructor into parity_issue396.go) and this
// guard silently extracted 176 names against a 177-tool registry — one tool too
// few, so the reverse direction accused a row that IS registered and a correct
// /docs/mcp page went red. Reading the package is what PR 766's sibling
// mcpToolInventory.test.ts already does; the two guards must agree on the same
// set or they contradict each other.
//
// Three properties keep the set from shrinking quietly again, all over the
// widened scope:
//   - EXPECTED_TOOL_COUNT pins the exact size. The `>= 150` floor this guard
//     shipped with is what let the drift through: 176 parsed against 177 is
//     still >= 150, so the count said nothing and only test 5 saw the phantom.
//   - Every `&mcp.Tool{` literal in the package must yield a name, so a
//     registration shape this extractor does not understand fails HERE instead
//     of quietly shrinking the set.
//   - More than one source file must be read, so a future narrowing back to a
//     single file fails on the scope itself.
// The registration shapes handled: `addTool(s, &mcp.Tool{Name: …}, h)` (the
// per-call wrapper that resolves and scopes the caller) and the older
// `mcp.AddTool(s.mcp, &mcp.Tool{Name: …}` call sites.
//
// WHAT IT DOES NOT COVER (stated honestly): it reads source text, so it cannot
// prove the served HTML — the rendered-page check is `npm run build` plus a
// browser read of /docs/chat. It also does not check that a listed tool's
// DESCRIPTION is accurate, only that the tool exists. And the loose shapes cover
// the phrasings named above, not every way English can call something a tool: a
// new wording ("the deleteall helper") is still a copy defect this guard cannot
// see. Inside the phrasings it does cover, the closed class it excludes is the
// determinatives and pronouns ordinary prose puts in that slot; an OPEN-class word
// there ("as the main tool for the workspace") is still read as a claim, exactly as
// it was before the rule — no exclusion can leave every adjective alone and still
// catch a single-word phantom name ("as the deleteall tool"), because at that
// position a name and a common noun are spelled identically. The comma shape has the
// same residual from the other side: its slot is an appositive HEAD, so a noun gloss
// ("The chat page, the tool that answers every message, is documented.") is still read
// as a claim on "page" — the price of keeping the single-word phantom ("wipeall, the
// tool that removes everything") catchable, and pinned as a measured line in the
// footprint test rather than left to be discovered. The parenthetical shape has a
// spelling gap of its own, deliberately
// (card t_3926a5dd): a SINGLE-WORD tool name in brackets ("… the MCP tool the lane
// pairs with (deleteall)") is NOT read as a claim, because at that position a bare
// English word cannot be told from a name — the loose spelling read four forms of
// non-claim (three of ordinary prose plus a card id) as phantoms, which is the defect
// that narrowed it, and the spelling is the only signal left. The single-word claim
// stays covered in the two positions where the sentence itself says "this is a tool".
// It narrows the mouth of the hole; it does not close the class.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const dir = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const DOCS_DIR = dir("../app/docs");
const MCP_DIR = dir("../../../api/internal/mcpsrv/");
const PARITY_MD = "../../../MCP-PARITY.md";
// Card t_0356ed4c: the tool reference is a module now (the rows) plus two pages
// under /docs/mcp. The claims this guard reads are in the rows' descriptions, so
// it reads the module — the same array the pages render.
const TOOL_REFERENCE_PAGE = "../lib/mcpToolGroups.ts";
const CHAT_DOCS_PAGE = "../app/docs/chat/page.tsx";
const ACTIVITY_DOCS_PAGE = "../app/docs/activity/page.tsx";

// Every docs page in the tree, recursively — a new /docs/<x> page is covered by
// this guard the day it lands, without editing this file.
function docsPages(base: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const name of readdirSync(base)) {
    const full = join(base, name);
    if (statSync(full).isDirectory()) {
      for (const [k, v] of docsPages(full)) out.set(k, v);
    } else if (name === "page.tsx") {
      out.set(full, readFileSync(full, "utf8"));
    }
  }
  return out;
}

const PAGES = docsPages(DOCS_DIR);

// The registered MCP tool set, derived from the Go registry source.
//
// The source is the mcpsrv PACKAGE, not one file of it: the registry lived in
// tools.go for years, so reading tools.go alone looked right — until #732 moved
// disable_2fa's literal into parity_issue396.go and this extractor silently
// returned 176 names against a 177-tool registry. Every non-test `.go` file is
// read, in sorted order so the join (and any failure message) is deterministic.
// mcpToolInventory.test.ts discovers its source the same way; a fix to one scope
// without the other would leave the two guards disagreeing.
const MCP_SOURCES = readdirSync(MCP_DIR)
  .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
  .sort()
  .map((f) => readFileSync(MCP_DIR + f, "utf8"));

// One literal per registered tool, so the registry has exactly this many names.
// Pinned rather than floored: the `>= 150` floor this guard shipped with could
// not see 176 vs 177, which is the drift that reddened main. A landing that adds
// or removes a tool moves this number — that is the point, and the message below
// says which direction to correct.
//
// 181: re-derived on the INTEGRATED tree, not copied from either side. Main's
// pin was 178 (#691's `update_workspace`, measured there) and this branch's was
// 180 (the three workspace-trash tools: `list_trashed_workspaces`,
// `restore_workspace`, `purge_workspace`). Neither is the answer, because
// neither tree is the one this pin lands on: the integration keeps BOTH sides'
// registrations, and the package on the merged tree registers 181 distinct
// names (181 `&mcp.Tool{` literals, 0 duplicate names, across 44 non-test
// files). A pin of 178 here would mean main's `update_workspace` was dropped
// and a pin of 180 would mean this branch's three trash tools were — both are
// the silently-reverted-main failure this guard exists to make loud, so the
// number doubles as the check that the merge kept both sides.
//
// 187: re-derived on THIS tree by this test's own regex over api/internal/mcpsrv,
// not copied from either side. main carries 184 — the shipping pair
// (`get_shipping_rates`, `set_shipping_rates`) landed with the zone-rate layer —
// and this branch adds the courier trio that quotes the same blocks live
// (`get_shipping_couriers`, `connect_shipping_courier`,
// `disconnect_shipping_courier`): 184 + 3. Neither side's number is right for
// the integrated registry, so a pin of 184 would mean the courier surface was
// dropped and one of the branch's old pins would mean main's newest tools were.
// 188: card t_467614db added `convert_currency` — the rate store's conversion had
// no production caller at all (0 call sites since issue #185), so the "used for
// conversion" copy on three surfaces was false. The tool is that caller; the pin
// moves by one.
// 190 → 191: card t_52429d6f added `describe_lifecycle`, the aggregate lifecycle
// table served to agents (and, with an id, that sale's available_events). One
// tool, so the pin moves by one again.
// 191 → 192: card t_94e5cca3 added `admin_dormancy_report`, the read-only
// per-account dormancy report behind the /admin dormancy section. One tool again.
const EXPECTED_TOOL_COUNT = 195;

const REGISTRY_SRC = MCP_SOURCES.join("\n");

function registeredTools(): Set<string> {
  const names = new Set<string>();
  for (const m of REGISTRY_SRC.matchAll(/&mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)"/g)) names.add(m[1]);
  return names;
}

const REGISTERED = registeredTools();

// The phrasings that present a bare name as a tool. Two name shapes, deliberately,
// and the shapes are NAMED so a test can prove which one catches which phrasing
// (see "every claim shape is load-bearing" below) rather than trusting that the
// list as a whole does something.
//
//   * TOOLNAME (snake_case — >= 1 underscore) is the STRICT shape, and it is what
//     catches the two sentences that shipped the false claim. It needs no stoplist:
//     a snake_case token in this position is not English prose.
//   * BARE_NAME (any lower-case identifier, underscore optional) is the LOOSE shape.
//     The strict shape's underscore requirement is what made a single-word phantom
//     ("the assistant can delete every product as the deleteall tool") invisible, and
//     that was a real hole: the guard's claim is "a name the docs CALL a tool must be
//     a name the registry registers", which does not depend on how the name is spelled.
//     All registered names are snake_case today, so this is latent rather than live
//     — but the guard has to be right about the claim, not only about today's registry.
//
// The loose shape DOES hit ordinary English, because "tool" is a common noun: real
// shipped prose says "has no tool to call" and "names submit_feedback as the one tool
// of its own". So it carries an explicit STOPLIST, measured against the real corpus —
// every entry below is a word that only ever appeared as prose there, and none is a
// registered tool (asserted below, so a tool cannot be silently stoplisted away).
//
// THAT STOPLIST WAS NEVER ENOUGH ON ITS OWN (card t_ceef983f, whose defect report is QA
// card t_034ae078). It is measured against the corpus that happens to exist, and the
// shape is broader than that corpus: ordinary English puts a possessive or a determiner
// in exactly this slot, and none of those is a corpus word. Measured on the real page,
// each of these was RED under the shape as it shipped (the guard naming the slot word):
//
//   "the assistant can be used as their tool of choice"  -> presents "their" as a tool
//   "it behaves as this tool does"                       -> presents "this" as a tool
//   "the page acts as your tool to reach the API"        -> presents "your" as a tool
//   "the same as any tool on the platform"               -> presents "any" as a tool
//   "the assistant works as the tool it is told to be"   -> presents "the" as a tool
//
// Every one of those words is a DETERMINATIVE or a PRONOUN: a CLOSED class of English,
// finite and enumerable from grammar, not a vocabulary to be re-measured per corpus. So
// the loose shape excludes that class BY RULE — FUNCTION_WORDS, compiled into both loose
// patterns as a negative lookahead below — instead of growing a stoplist one failure at a
// time. Adding each word the failure named is the route this file already warns is "a
// guard that goes green on the exact wording it exists to stop", and the rule is asserted
// CLOSED in both directions instead: every member of the class is driven through both
// loose shapes (so deleting a member reddens, in "the loose shapes' footprint …"), and no
// member may be a registered tool name (so a tool cannot be silently ruled out of scope).
//
// PROSE_WORDS is NOT retired for it, and the two are not each other's fallback. The class
// carries determinatives and pronouns; the corpus-measured residue stays measured. "one"
// is reached by real shipped prose ("names submit_feedback as the one tool of its own"),
// is not a word the rule needs to exclude, and stays where the corpus put it.
//
// WHAT THIS DOES NOT CLOSE, stated rather than papered over: the class is the closed-class
// vocabulary of English determinatives and pronouns. An OPEN-class word in the same slot
// ("as the main tool for the workspace") is still read as a claim, exactly as it was
// before this rule — no rule can both catch a single-word phantom ("as the deleteall
// tool") and leave every ordinary adjective alone, because the slot holds a name and a
// common noun identically. That residual is stated under WHAT IT DOES NOT COVER.
//
// The third shape is the parenthetical form the page's own HEADER COMMENT shipped with:
// "the MCP tool the proposal lane pairs with (propose_write)". The guard reads whole
// file text, so a comment was already in scope and needed covering, not a new scope. The
// window is bounded at 60 chars — measured against the real form, where "tool" sits 33
// chars before the "(" of the name, even when the comment wraps onto the next line.
//
// IT CARRIES THE STRICT NAME SHAPE, and that is a measurement, not a style choice (card
// t_3926a5dd, whose defect report is QA card t_c016f6e3). It shipped carrying BARE_NAME,
// so ANY lower-case word in brackets within 60 chars after "tool"/"tools" read as a tool
// name the docs claim — and it reported a phantom for three forms of ordinary English,
// each reproduced against this file as it stood:
//
//   "// the tool (deprecated) is no longer listed"   -> claimed "deprecated"
//   "This page describes the MCP tools (overview) …"  -> claimed "overview"
//   '// words after the word "tool(s)" reads as …'    -> claimed "s"
//
// None of those is a near-miss of a claim; they are shapes English produces — a gloss,
// an appositive, a plural marker — and at that position the SPELLING is the only signal
// that separates a name from a gloss: "the tool (deprecated)" and a hypothetical
// "the tool (deleteall)" differ in nothing else, so any rule that reads the second as a
// claim reads the first as one too. snake_case is what is left: English prose does not
// put snake_case in brackets, and every registered name is snake_case (asserted by the
// parenthetical test below, so a bare-word tool fails there with what to do about it).
// The loose single-word claim stays covered where the sentence structure itself says
// "this is a tool" — "as the X tool" and "X, the tool that" — and the spelling gap this
// leaves is stated under WHAT IT DOES NOT COVER.
//
// THE STRICT SHAPE IS NOT ENOUGH ON ITS OWN, which was measured rather than assumed: a
// KANBAN CARD ID is spelled `t_` + 8 hex, which is snake_case, so it matches both name
// shapes — on this tree "// these tools (t_cde2e73a) are added to the registry here"
// still read as `presents "t_cde2e73a" as a tool`. A bracketed aside is exactly where a
// docs comment cites a card, so the shape also SKIPS this repo's card-id spelling
// (CARD_ID below, asserted never to match a registered name). The card's own quoted
// sentence — "// registry (t_cde2e73a). Keep it in step when tools are added or
// changed" — reads GREEN either way, because its bracket follows "registry" and the
// word "tools" comes later with no bracket after it (QA card t_c016f6e3 re-measured
// that, correcting the defect report).
//
// The shape is NOT dead on the real corpus, which the note here used to get wrong ("it
// matches nothing in the shipped docs corpus today"): /docs/activity renders "the MCP
// tool reference (list_events)", the one parenthetical claim the shipped docs carry, and
// the parenthetical test below pins it, so narrowing this shape to the spelling the docs
// actually write is measured rather than assumed.
const TOOLNAME = String.raw`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`;
const BARE_NAME = String.raw`[a-z][a-z0-9_]*`;

// The identifier spellings this repo uses for something that is NOT a tool. A kanban card
// id is the one that reaches the docs corpus: comments cite cards, a citation is often a
// bracketed aside, and `t_cde2e73a` is snake_case — so the strict capture cannot tell it
// from a tool name and the shape needs this exclusion to stop reading citations as claims.
// It cannot hide a real tool: a registered name matching it fails the parenthetical test
// below (`CARD_ID`), which is the same both-directions check PROSE_WORDS carries.
const CARD_ID = /^t_[0-9a-f]{8}$/;

// Words the loose shape matches on real prose. Keep this list measured, not guessed:
// add an entry only for a word that actually appears in a docs page, and never one that
// is a registered tool name.
const PROSE_WORDS = new Set(["a", "an", "no", "one", "own"]);

// The CLOSED class the loose shapes exclude BY RULE (card t_ceef983f): the English
// determinatives and pronouns that can sit in the loose slot — "as (the )?X tool" and
// "X, the tool that" — where X is otherwise read as a claimed tool name. Closed class
// means finite and enumerable from grammar, which is the point: the loose shape does not
// need a corpus to be re-measured against (PROSE_WORDS, above), it needs the vocabulary
// that English can put at that position and nothing else can.
//
// The entries are grouped by the grammatical class each one comes from, so this list can
// be AUDITED against English rather than against a failure log:
const FUNCTION_WORDS = new Set([
  // articles
  "a", "an", "the",
  // demonstratives
  "this", "that", "these", "those",
  // possessive determiners and possessive pronouns
  "my", "your", "his", "her", "its", "our", "their",
  "mine", "yours", "hers", "ours", "theirs",
  // personal pronouns
  "i", "you", "he", "she", "it", "we", "they",
  "me", "him", "us", "them",
  // reflexive pronouns
  "myself", "yourself", "himself", "herself", "itself",
  "ourselves", "yourselves", "themselves",
  // quantifiers
  "all", "any", "both", "each", "either", "enough", "every", "few", "fewer",
  "half", "less", "little", "many", "more", "most", "much", "neither",
  "no", "none", "several", "some", "such", "whole",
  // indefinite pronouns
  "another", "anybody", "anyone", "anything", "everybody", "everyone",
  "everything", "nobody", "nothing", "other", "others", "somebody",
  "someone", "something",
  // wh-words
  "what", "whatever", "which", "whichever", "who", "whoever", "whom",
  "whomever", "whose",
]);

// The class compiled into the loose patterns. Longest alternative first so the spelling
// reads the way it matches; the trailing `\b` is what makes the order matter-free either
// way (for "their_tool", the `their` alternative fails on the boundary and the snake_case
// name is captured, which is the case a name that merely STARTS with a function word
// needs). Case-insensitive, like the shapes themselves — the class carries "their", and
// the shapes' `i` flag means "Their" reaches the same slot.
const FUNCTION_WORD_ALT = [...FUNCTION_WORDS].sort((a, b) => b.length - a.length).join("|");
const NOT_FUNCTION_WORD = String.raw`(?!${FUNCTION_WORD_ALT}\b)`;

// Named so a failure can say WHICH shape caught a name, and so the load-bearing test
// below can point at one shape by name instead of by index. `skip` is a per-shape
// exclusion for a spelling that cannot be a tool name at all — the parenthetical carries
// one, because a bracketed aside is where the docs cite a card (see CARD_ID).
const CLAIM_SHAPES: { shape: string; re: RegExp; skip?: (name: string) => boolean }[] = [
  { shape: "as-the-tool/strict", re: new RegExp(String.raw`\bas (?:the )?(${TOOLNAME}) tool\b`, "gi") },
  { shape: "comma-the-tool/strict", re: new RegExp(String.raw`\b(${TOOLNAME}),? the tool (?:that|which|for)\b`, "gi") },
  { shape: "as-the-tool/loose", re: new RegExp(String.raw`\bas (?:the )?${NOT_FUNCTION_WORD}(${BARE_NAME}) tool\b`, "gi") },
  { shape: "comma-the-tool/loose", re: new RegExp(String.raw`\b${NOT_FUNCTION_WORD}(${BARE_NAME}),? the tool (?:that|which|for)\b`, "gi") },
  // The parenthetical: the docs name a tool, then give the name in brackets a few
  // words later. It pins the header-comment form the original defect shipped in, and it
  // is the one shape whose name is in a GLOSS position — which is why it is strict,
  // skips a card-id citation, and neither of the others does (see the note above it).
  {
    shape: "parenthetical/strict",
    re: new RegExp(String.raw`\btools?\b[^()]{0,60}?\((${TOOLNAME})\)`, "gi"),
    skip: (name) => CARD_ID.test(name),
  },
];

// Every name a page CLAIMS is a tool, with the offset it was found at and the shape
// that caught it. Deduplicated by offset+name, because the strict and loose shapes both
// match a snake_case name in the same place and a duplicate would double-report one
// offender.
function namesClaimedAsTools(src: string): { name: string; index: number; shape: string }[] {
  const out: { name: string; index: number; shape: string }[] = [];
  const seen = new Set<string>();
  for (const { shape, re, skip } of CLAIM_SHAPES) {
    for (const m of src.matchAll(re)) {
      const name = m[1];
      if (PROSE_WORDS.has(name)) continue;
      if (skip?.(name)) continue;
      const at = m.index ?? 0;
      const key = `${at}:${name}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push({ name, index: at, shape });
    }
  }
  return out;
}

// The names each shape claims, in order — used by the load-bearing test to prove a
// single shape is what catches a given phrasing, with the others taken out of the way.
function namesClaimedByShape(shape: string, src: string): string[] {
  const found = CLAIM_SHAPES.find((s) => s.shape === shape);
  assert.ok(found, `no claim shape named ${JSON.stringify(shape)} — the test is stale`);
  return [...src.matchAll(found!.re)]
    .map((m) => m[1])
    .filter((n) => !PROSE_WORDS.has(n) && !found!.skip?.(n));
}

// A tool name as it appears anywhere in docs prose or in a tool-reference entry.
const NAMED_IN_PROSE = /\b[a-z][a-z0-9]*_[a-z0-9_]+\b/g;

test("the guard is looking at the real docs tree and the real registry (it cannot pass vacuously)", () => {
  assert.ok(
    PAGES.size >= 10,
    `walked only ${PAGES.size} docs page(s) under src/app/docs — the walk is broken`,
  );
  assert.ok(
    PAGES.has(fileURLToPath(new URL(CHAT_DOCS_PAGE, import.meta.url))),
    "the docs walk did not pick up /docs/chat — the page this guard was written for",
  );
  // The scope itself is asserted, not assumed: more than one source file is
  // read. This guard's red on main came from a single-file scope that no test
  // named, so a future narrowing back to one file has to fail here.
  assert.ok(
    MCP_SOURCES.length > 1,
    `read ${MCP_SOURCES.length} non-test source file(s) from api/internal/mcpsrv/ — the registry ` +
      "is a package, and a single-file scope is the defect this guard was reddened by",
  );
  // The count is PINNED, not floored. `>= 150` is what let 176-parsed-against-177
  // pass: the drift was invisible in the count and surfaced only as a phantom on
  // test 5. A landing that adds or removes a tool moves EXPECTED_TOOL_COUNT — and
  // the failure says which way to correct it, so the number cannot be nudged
  // blindly.
  assert.equal(
    REGISTERED.size,
    EXPECTED_TOOL_COUNT,
    `parsed ${REGISTERED.size} registered tool(s) from the non-test api/internal/mcpsrv ` +
      `package, but this guard pins ${EXPECTED_TOOL_COUNT}. If tools were added or removed, update ` +
      "EXPECTED_TOOL_COUNT in the same change. If they were not, the extractor has drifted from " +
      "the registration shapes in that package — which is how it silently lost disable_2fa.",
  );
  // Every `&mcp.Tool{` literal must yield a name. A construct the extractor does
  // not understand fails HERE, with the registry set quietly incomplete, instead
  // of shrinking the set (making the two direction tests require less and less).
  const literals = (REGISTRY_SRC.match(/&mcp\.Tool\{/g) ?? []).length;
  assert.equal(
    literals,
    REGISTERED.size,
    `${literals} \`&mcp.Tool{\` literal(s) in the non-test api/internal/mcpsrv package but only ` +
      `${REGISTERED.size} name(s) extracted — a registration shape the extractor does not ` +
      "understand, so the registered set is incomplete",
  );
  for (const known of ["submit_feedback", "list_products", "set_form_theme", "update_product"]) {
    assert.ok(REGISTERED.has(known), `the registry extractor missed ${known}, a tool registered in Go`);
  }
  // The tool that exposed the single-file scope: it is registered (its literal now
  // lives in parity_issue396.go), and it is exactly what a tools.go-only extractor
  // drops. Naming it here means a narrowing fails loudly and immediately.
  assert.ok(
    REGISTERED.has("disable_2fa"),
    "the registry extractor missed disable_2fa — the tool whose literal moved out of tools.go, " +
      "and the reason this guard must read the whole mcpsrv package",
  );
  // The whole point of the guard: these two are the names the docs must be
  // careful about. One is registered, one is deliberately not.
  assert.ok(REGISTERED.has("submit_feedback"), "submit_feedback is no longer a registered MCP tool");
  assert.ok(
    !REGISTERED.has("propose_write"),
    "propose_write is registered as an MCP tool — that is the decision the docs, MCP-PARITY.md " +
      "and api/internal/mcpsrv/chat_tools_guard_test.go all record as deliberately NOT taken",
  );
});

test("a name the docs call a tool is a tool that is actually registered", () => {
  const offenders: string[] = [];
  for (const [file, src] of PAGES) {
    for (const { name, index } of namesClaimedAsTools(src)) {
      if (!REGISTERED.has(name)) {
        const line = src.slice(0, index).split("\n").length;
        offenders.push(`${file}:${line} presents "${name}" as a tool, but no such tool is registered`);
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `the docs present a name as an MCP tool that the non-test api/internal/mcpsrv package does ` +
      `not register:\n  ` +
      offenders.join("\n  ") +
      "\nEither the tool is not registered (then do NOT describe it as one — see MCP-PARITY.md's " +
      '"In-app assistant — the proposal lane") or it is registered and the claim is true.',
  );
});

test("the guard's own prose stoplist cannot hide a real tool name", () => {
  // The loose shapes need a stoplist to stay quiet on real English, and a stoplist is
  // exactly where a guard can quietly stop guarding: one entry that IS a registered
  // tool name would make every mention of that tool invisible to test 2. Pin both
  // directions, and pin that the list stays small enough to audit by eye.
  for (const word of PROSE_WORDS) {
    assert.ok(
      !REGISTERED.has(word),
      `the stoplist entry ${JSON.stringify(word)} is a REGISTERED MCP tool name — the loose scan ` +
        "now ignores every doc that calls it a tool, which is the hole this guard exists to close",
    );
  }
  assert.ok(
    PROSE_WORDS.size <= 12,
    `the stoplist has grown to ${PROSE_WORDS.size} entries — that is no longer small enough to ` +
      "audit by eye, and a stoplist nobody re-reads is where a guard silently stops guarding",
  );
  // Every entry must be a plain English word, never a plausible tool identifier: an
  // entry with an underscore or a digit is a name, not prose.
  for (const word of PROSE_WORDS) {
    assert.match(
      word,
      /^[a-z]+$/,
      `the stoplist entry ${JSON.stringify(word)} is not a plain English word — if it is an ` +
        "identifier, it belongs in the registry, not in a list the guard ignores",
    );
  }
});

test("the two sentences that shipped the false claim cannot come back", () => {
  const chatDocs = read(CHAT_DOCS_PAGE);
  assert.doesNotMatch(
    chatDocs,
    /exists over MCP as the propose_write tool/i,
    "the shipped v1.12.0 sentence is back: it told users the proposal lane exists over MCP as " +
      "propose_write, which is not registered as an MCP tool",
  );
  assert.doesNotMatch(
    chatDocs,
    /, and propose_write, the tool that/i,
    "the shipped v1.12.0 footer is back: it listed propose_write beside submit_feedback as if " +
      "both were entries of the linked MCP tool reference",
  );
});

test("the chat page still says the proposal lane is in-app and NOT an MCP tool", () => {
  const chatDocs = read(CHAT_DOCS_PAGE);
  // Still documented (AGENTS.md DOCUMENTATION PARITY step 2): the fix is to make
  // the CLAIM true, not to delete the name.
  assert.ok(
    chatDocs.includes("propose_write"),
    "the chat docs no longer name propose_write at all — the phase-2 lane would be undocumented",
  );
  assert.match(
    chatDocs,
    /propose_write[\s\S]{0,160}not an MCP tool|no propose_write over MCP/i,
    "the chat docs name propose_write but no longer say it is not an MCP tool — without that, " +
      "the name alone reads as a registered tool again",
  );
  // The accurate half of the original paragraph must survive the fix as well.
  assert.match(
    chatDocs,
    /deliberately not something an API call can supply/i,
    "the docs lost the true half of the original sentence: the confirmation is a signed-in " +
      "person's click and not something an API call can supply",
  );
});

test("MCP-PARITY.md still records the decision as NOT registered, in step with the page", () => {
  const parity = read(PARITY_MD);
  assert.match(
    parity,
    /`propose_write` — NOT registered/,
    "MCP-PARITY.md no longer records propose_write as deliberately NOT registered — the docs " +
      "page and the parity file now contradict each other",
  );
  assert.match(
    parity,
    /session-only/,
    "MCP-PARITY.md no longer gives the reason (the confirm step is a session-only route), which " +
      "is the reason the docs page leans on",
  );
  assert.match(
    parity,
    /no MCP caller could ever confirm/i,
    "MCP-PARITY.md no longer says why registering propose_write would be wrong (the token goes to " +
      "the browser and an MCP caller could never confirm it)",
  );
});

test("every entry of the MCP tool reference is a registered tool", () => {
  const reference = read(TOOL_REFERENCE_PAGE);
  const entries = [...reference.matchAll(/\{\s*name:\s*"([a-z0-9_]+)"/g)].map((m) => m[1]);
  assert.ok(
    entries.length >= 100,
    `parsed only ${entries.length} tool entries from /docs/mcp — the extractor has drifted from ` +
      "that page's entry shape",
  );
  assert.ok(
    entries.includes("list_products"),
    "the /docs/mcp extractor did not find list_products, a listed tool — the parse is wrong",
  );
  const phantom = [...new Set(entries)].filter((n) => !REGISTERED.has(n));
  assert.deepEqual(
    phantom,
    [],
    `/docs/mcp advertises tool(s) that the non-test api/internal/mcpsrv/ package does not ` +
      `register: ${phantom.join(", ")} — the reference page must only list tools that exist`,
  );
});

test("the prose scan does not mistake a field or a phrase for a tool name", () => {
  // A guard against this file going over-eager: these are NOT tool names and the
  // scan must leave them alone. `as no tool to call` is real chat/page.tsx prose,
  // and it is the exact string the loose `as (the )?X tool` shape hits — which is
  // why the stoplist exists.
  const chatDocs = read(CHAT_DOCS_PAGE);
  const phrases = ["as no tool to call", "calls that change's own tool itself", "as the one tool"];
  for (const phrase of phrases) {
    assert.ok(chatDocs.includes(phrase), `chat/page.tsx no longer contains ${JSON.stringify(phrase)}`);
    assert.deepEqual(
      namesClaimedAsTools(phrase).map((c) => c.name),
      [],
      `the scan claims a tool name inside the non-tool phrase ${JSON.stringify(phrase)} — the ` +
        "scan would produce a false positive on ordinary prose",
    );
  }
  // Quiet is not enough on its own: name WHICH shape fires on ordinary prose and check
  // that the stoplist is what absorbs it. A shape that reaches a phrase with a word that
  // is NOT in the stoplist would be an unhandled false positive, and this test is where
  // that is caught rather than in an unexplained red on the test above.
  let reached = 0;
  for (const { shape, re } of CLAIM_SHAPES) {
    for (const phrase of phrases) {
      for (const m of phrase.matchAll(re)) {
        reached++;
        assert.ok(
          PROSE_WORDS.has(m[1]),
          `shape ${shape} matched ${JSON.stringify(m[1])} inside the ordinary phrase ` +
            `${JSON.stringify(phrase)} and that word is not in the stoplist — either add it ` +
            "(with the corpus measurement) or the shape is too broad",
        );
      }
    }
  }
  assert.ok(
    reached > 0,
    "no shape matched any ordinary phrase — the loose shapes are not being exercised, so this " +
      "test is passing vacuously",
  );
  // And the prose name matcher must not be so broad that it flags the whole page.
  const names = [...chatDocs.matchAll(NAMED_IN_PROSE)].map((m) => m[1]);
  assert.ok(names.length > 0, "the prose name matcher found nothing in the chat docs");
});

test("the parenthetical shape is strict, and the spelling is what keeps it off prose", () => {
  // WHY THIS TEST EXISTS (card t_3926a5dd, whose defect report is QA card t_c016f6e3).
  // The parenthetical shape shipped carrying BARE_NAME, so anything in brackets after the
  // word "tool"/"tools" was reported as a tool the docs claim and the registry does not
  // have. It has TWO faces, and this test covers both: ordinary English (a gloss, an
  // appositive, a plural marker), which the STRICT spelling keeps out, and a kanban card
  // id, which is snake_case and so reaches even the strict capture — that one is skipped
  // as a citation. Each aside below was measured red against the shipped shape; the first
  // three were real drafts that had to be reworded purely to appease the pattern.
  const BRACKET_ASIDES = [
    "// the tool (deprecated) is no longer listed",
    "This page describes the MCP tools (overview) of the assistant.",
    '// words after the word "tool(s)" reads as a phantom claim',
    // The citation face. The card that filed this defect could not write its own card id
    // in brackets after the word "tools" — measured on this tree after the strict capture
    // landed, before the shape skipped the spelling.
    "// these tools (t_cde2e73a) are added to the registry here",
  ];
  for (const text of BRACKET_ASIDES) {
    assert.deepEqual(
      namesClaimedByShape("parenthetical/strict", text),
      [],
      `the parenthetical shape claims a name inside ${JSON.stringify(text)} — a bracket after ` +
        'the word "tool" holding something that is not a tool name is an aside, not a claim, ' +
        "and reading it as one is the false positive card t_3926a5dd fixed",
    );
    assert.deepEqual(
      namesClaimedAsTools(text).map((c) => c.name),
      [],
      `${JSON.stringify(text)} is read as presenting a tool — but it is an aside, so ` +
        'the guard\'s message ("presents … as a tool, but no such tool is registered") would ' +
        "be about a claim nobody made",
    );
  }
  // The asides are load-bearing, not decoration: under the loose spelling the shape
  // shipped with, every one IS claimed, with exactly these names — so a phrase that is
  // quiet for some unrelated reason cannot let the legs above pass vacuously, and the
  // card id's capture is recorded as the thing the strict spelling alone did NOT fix.
  // Asserted on the shape's own anchor and window: if BARE_NAME or the window is ever
  // moved, re-derive this against the shipped form rather than deleting the leg.
  const LOOSE_PARENTHETICAL = new RegExp(String.raw`\btools?\b[^()]{0,60}?\((${BARE_NAME})\)`, "gi");
  assert.deepEqual(
    BRACKET_ASIDES.flatMap((t) => [...t.matchAll(LOOSE_PARENTHETICAL)].map((m) => m[1])),
    ["deprecated", "overview", "s", "t_cde2e73a"],
    "the measured asides no longer reproduce the false positive under the loose spelling, so " +
      "they no longer show what keeps this guard off asides that are not claims",
  );
  // MEASURED AFTER THE PROSE REWRITE: the LIVE-CORPUS site this leg was written for
  // is gone. /docs/activity used to spell the parenthetical form — "the MCP tool
  // reference (list_events)" — but the converted page says "Related: [the MCP tool
  // reference](/docs/mcp)" and names the tool in its own sentence ("Over MCP,
  // `list_events` is the same read …"). No docs page spells the parenthetical form
  // today (measured: 0 sites across the tree), so an assertion that the real corpus
  // carries one cannot be kept true — recorded here rather than deleted quietly,
  // and the claim it was standing in for is still asserted below.
  //
  // What keeps the shape load-bearing against the REAL registry: every registered
  // name, spelled in the form the shape narrows for, is read as a claim. A capture
  // that stopped matching snake_case, a window too short for the phrase, or a skip
  // rule that swallowed names fails here on a tool the product really has — not on
  // a synthetic one.
  const activity = read(ACTIVITY_DOCS_PAGE);
  assert.ok(
    activity.includes("list_events"),
    "/docs/activity no longer names list_events at all — the tool-reference aside this leg " +
      "was rewritten around has been cut, not merely reworded",
  );
  const unreadRegistered = [...REGISTERED].filter(
    (n) => !namesClaimedByShape("parenthetical/strict", `an MCP tool aside (${n})`).includes(n),
  );
  assert.deepEqual(
    unreadRegistered,
    [],
    `the parenthetical shape no longer reads registered tool(s) ${JSON.stringify(unreadRegistered)} ` +
      `in "… tool … (<name>)" — the strict spelling or its 60-char window has drifted from the ` +
      "registry, so a parenthetical claim of that tool would be invisible",
  );
  // And the strict spelling is compatible with the registry by construction: every
  // registered name is snake_case, so no registered tool is unreachable from this shape.
  // A registered bare-word tool would turn the spelling rule into a hole, so it fails
  // HERE, with the fix, rather than silently inside the shape.
  const notStrict = [...REGISTERED].filter((n) => !new RegExp(`^${TOOLNAME}$`).test(n));
  assert.deepEqual(
    notStrict,
    [],
    `registered MCP tool(s) ${JSON.stringify(notStrict)} are not snake_case, so a ` +
      'parenthetical claim of one ("… the MCP tool … (<name>)") is invisible to the strict ' +
      "parenthetical shape — give that spelling its own shape, or widen the capture with the " +
      "measurement showing it does not read prose as a claim",
  );
  // So is the citation exclusion: a registered tool spelled like a card id would be
  // invisible to the parenthetical shape, which is the hole CARD_ID must never open. Same
  // both-directions check the stoplist carries.
  const idSpelled = [...REGISTERED].filter((n) => CARD_ID.test(n));
  assert.deepEqual(
    idSpelled,
    [],
    `registered MCP tool(s) ${JSON.stringify(idSpelled)} are spelled like a kanban card id, so ` +
      "the parenthetical shape's citation exclusion would hide a real tool — rename the `CARD_ID` " +
      "exclusion (or the tool) rather than leaving a registration the guard cannot see",
  );
});

test("every claim shape is load-bearing, and the guard catches what it missed", () => {
  // WHY THIS TEST EXISTS. The two coverage gaps this file was fixed for were only ever
  // measured with a mutation applied by hand and the guard re-run — which proves the
  // guard bites only for as long as someone repeats that by hand. The phrasings are
  // pinned HERE instead, so deleting a shape from CLAIM_SHAPES (or widening the
  // stoplist until a shape is inert) turns this red on the next run.
  //
  // The phrasings below are the two the guard MISSED, plus the two it already caught.
  // Each is asserted in BOTH directions: the shape that must catch it does, and the
  // name it captures is genuinely absent from the registry (so the assert in test 2 is
  // a real failure, not a coincidence of the phrasing being harmless).
  const MUST_BE_CAUGHT: { label: string; text: string; by: string; name: string }[] = [
    {
      // Gap 1. Underscore-free name in the shipped phrasing — the wording that was
      // invisible while the capture required snake_case.
      label: "single-word phantom, 'as the X tool'",
      text: "the assistant can delete every product as the deleteall tool",
      by: "as-the-tool/loose",
      name: "deleteall",
    },
    {
      label: "single-word phantom, 'X, the tool that'",
      text: "and wipeall, the tool that removes everything in the workspace",
      by: "comma-the-tool/loose",
      name: "wipeall",
    },
    {
      // Gap 2. The page's own HEADER COMMENT form of the original defect. The guard
      // reads whole file text, so this was always in scope — only the phrasing was
      // uncovered. Asserted twice: on one line, and wrapped across two comment lines
      // as the real header comment was.
      label: "parenthetical, one line",
      text: "// It documents the MCP tool the proposal lane pairs with (propose_write).",
      by: "parenthetical/strict",
      name: "propose_write",
    },
    {
      label: "parenthetical, wrapped across two comment lines",
      text: "// It documents the MCP tool the proposal lane pairs with\n// (propose_write), the reversible set it may prepare,",
      by: "parenthetical/strict",
      name: "propose_write",
    },
    {
      label: "the shipped v1.12.0 sentence",
      text: "The same capability exists over MCP as the propose_write tool for an agent to describe a change;",
      by: "as-the-tool/strict",
      name: "propose_write",
    },
    {
      label: "the shipped v1.12.0 footer",
      text: "(submit_feedback, the tool that files a report, and propose_write, the tool that prepares a change for confirmation).",
      by: "comma-the-tool/strict",
      name: "propose_write",
    },
  ];
  for (const c of MUST_BE_CAUGHT) {
    assert.ok(
      namesClaimedByShape(c.by, c.text).includes(c.name),
      `the ${c.by} shape no longer claims ${JSON.stringify(c.name)} in ${c.label} — the guard has ` +
        `gone blind to the phrasing it was fixed for (or the shape was renamed and this test ` +
        `was not)`,
    );
    assert.ok(
      namesClaimedAsTools(c.text).some((x) => x.name === c.name),
      `${c.label} is no longer caught by ANY shape`,
    );
    assert.ok(
      !REGISTERED.has(c.name),
      `${JSON.stringify(c.name)} is now a registered tool, so ${c.label} is no longer an offender ` +
        "and this case has gone vacuous — pick a name the registry does not have",
    );
  }
  // The shapes must not all be the same regex in disguise: if one shape catches
  // everything the others do, the "by" assertions above pass for any of them and prove
  // nothing about the shape actually doing the work.
  const looseOnly = "the assistant can delete every product as the deleteall tool";
  assert.deepEqual(
    namesClaimedByShape("as-the-tool/strict", looseOnly),
    [],
    "the strict shape now catches an underscore-free name — the loose shape is redundant",
  );
  const parenOnly = "// It documents the MCP tool the proposal lane pairs with (propose_write).";
  for (const shape of ["as-the-tool/strict", "comma-the-tool/strict", "as-the-tool/loose", "comma-the-tool/loose"]) {
    assert.deepEqual(
      namesClaimedByShape(shape, parenOnly),
      [],
      `the ${shape} shape now catches the parenthetical form — the parenthetical shape is redundant`,
    );
  }
});

test("the loose shapes' footprint on ordinary English is a CLOSED class, not a hand-grown stoplist", () => {
  // WHY THIS TEST EXISTS (card t_ceef983f, whose defect report is QA card t_034ae078).
  //
  // The test above ("the prose scan does not mistake a field or a phrase for a tool name")
  // measures the loose shapes on THREE hardcoded phrases and asserts that the words they
  // reach there are stoplisted. Against the shipped corpus that was quiet, because the
  // corpus contains exactly one unstoplisted prose hit ("one", which PROSE_WORDS absorbs).
  // It measured the corpus, not the shape. Ordinary English puts a possessive or a
  // determiner in exactly this slot, and the shape read five of them as claimed tool names
  // (their, this, your, any, the — the table is in the note above the shapes). The fix is a
  // closed class excluded BY RULE (FUNCTION_WORDS); this test is what keeps that class
  // closed and keeps the cliff visible: it measures the shapes' footprint on ordinary
  // English WRITTEN HERE rather than read off the corpus, and asserts every word the
  // pre-rule capture reaches there (and across the real docs tree) is excluded. So the next
  // widening of BARE_NAME, or the deletion of a class member, reddens HERE instead of
  // silently moving the cliff.

  // The PRE-RULE spellings — the loose shapes exactly as this file shipped them before the
  // rule, built from the SAME BARE_NAME the guard uses. This is what makes the measurement
  // a measurement: it captures the words the class has to hold off, wherever BARE_NAME is
  // moved to. (If BARE_NAME is widened, these follow it, and the legs below see the wider
  // capture. If they were spelled out as literals here they would measure the old shape.)
  const UNRULED_AS = new RegExp(String.raw`\bas (?:the )?(${BARE_NAME}) tool\b`, "gi");
  const UNRULED_COMMA = new RegExp(String.raw`\b(${BARE_NAME}),? the tool (?:that|which|for)\b`, "gi");
  const unruledHits = (text: string) =>
    [...text.matchAll(UNRULED_AS), ...text.matchAll(UNRULED_COMMA)].map((m) => m[1]);
  // Mirrors what the guard itself skips, so the two cannot disagree about what "excluded"
  // means: the class is matched case-insensitively (the shapes carry the `i` flag), the
  // stoplist is matched as measured (exactly, the way namesClaimedAsTools does).
  const excluded = (word: string) => FUNCTION_WORDS.has(word.toLowerCase()) || PROSE_WORDS.has(word);

  // ORDINARY ENGLISH THAT IS NOT IN THE SHIPPED CORPUS — one sentence per grammatical
  // subclass the class carries, plus the near-misses that would only be reached if the
  // capture WIDENED (a possessive with an apostrophe, which BARE_NAME cannot spell). These
  // are English a docs page could legitimately carry; none of them claims a tool.
  const SLOT_PROSE = [
    // the real shipped prose the stoplist was measured on (its registered tool name
    // removed, so LEG 3 below is about prose and not about a true claim)
    "the chat page says the assistant has no tool to call",
    "it is named as the one tool of its own",
    // articles, including the "the" the shape has to back off from
    "described as a tool for the workspace",
    "the reference names the page as a tool for the build",
    "the assistant works as the tool it is told to be",
    "the sidebar is listed as the tool to reach the settings",
    // demonstratives
    "it behaves as this tool does",
    "the export works the same as that tool does",
    "the reply is not as these tools are",
    "the runner is not as those tools are",
    // possessives
    "the assistant can be used as their tool of choice",
    "the page acts as your tool to reach the API",
    "each workspace treats the assistant as its tool for bulk edits",
    "the extension is documented as our tool for importing",
    "the shortcut is listed as my tool for the inbox",
    "the walkthrough shows it as his tool for the first draft",
    "the template is offered as her tool for the outline",
    // quantifiers
    "the same as any tool on the platform",
    "the sidebar is described as some tool for the job",
    "the panel is advertised as each tool in the suite",
    "the runner is offered as every tool at once",
    "the reference names the CLI as either tool in the pair",
    "the reference names the CLI as neither tool of the pair",
    "the assistant is not documented as no tool at all",
    // indefinite and wh-words
    "the panel is described as whatever tool fits the job",
    "the guide does not say as which tool it runs",
    // near misses: English the capture cannot spell today — a widened BARE_NAME reaches
    // these, and they are not in the class, so leg 2 below reddens on that widening
    "the reply is written as the model's tool of choice",
    "the panel is documented as a client's tool",
    "the runner is documented as the read-only tool for the build",
    // the second loose shape, "X, the tool that" — its slot is an appositive head, and
    // these are the pronouns that head one in ordinary English
    "it, the tool that answers every message, is on every page",
    "this, the tool that writes the reply, is documented",
    "they, the tool that answers the message, are the assistant",
    "nothing, the tool that does it, is registered",
  ];

  // LEG 1 — the corpus has to be IN the shapes' mouth, or every assertion below passes
  // vacuously (a corpus of sentences that the shapes never reach measures nothing).
  const hits = SLOT_PROSE.flatMap(unruledHits);
  assert.ok(
    hits.length >= 20,
    `this corpus reaches the loose slot only ${hits.length} time(s) — the sentences below ` +
      "have drifted out of the shapes' mouth and this test would pass without measuring " +
      "anything (re-derive the corpus against the shipped BARE_NAME)",
  );

  // LEG 2 — the class and the stoplist must exclude every word the pre-rule capture reaches
  // on ordinary English. This is the closure the rule has against English rather than
  // against the corpus: no word in this list was measured off a failure log, and a member
  // deleted from FUNCTION_WORDS lands here.
  const unexcluded = [...new Set(hits)].filter((w) => !excluded(w));
  assert.deepEqual(
    unexcluded,
    [],
    `the loose shapes reach ${JSON.stringify(unexcluded)} in ordinary English, and neither the ` +
      "closed class (FUNCTION_WORDS) nor the measured stoplist (PROSE_WORDS) excludes them. " +
      "Each of those is a name the docs do not claim and the registry has never had — the " +
      "false positive card t_ceef983f measured. Either the word's grammatical class belongs " +
      "in FUNCTION_WORDS (and the class is then closed against this corpus), or the capture " +
      "has WIDENED past the English this corpus was measured on.",
  );

  // LEG 3 — and the guard itself must be quiet on the same sentences: excluded words are
  // not enough if a shape still reports one of them.
  for (const sentence of SLOT_PROSE) {
    assert.deepEqual(
      namesClaimedAsTools(sentence).map((c) => `${c.name} (${c.shape})`),
      [],
      `ordinary English is read as presenting a tool: ${JSON.stringify(sentence)} — the ` +
        'guard\'s message ("presents … as a tool, but no such tool is registered") would be ' +
        "about a claim nobody made",
    );
  }

  // LEG 4 — the same measurement on the SHIPPED corpus, so the class is not closed on the
  // sentences above while the real tree drifts the other way. Everything the pre-rule
  // capture reaches on the docs tree is either a registered name (a true claim) or
  // excluded; and the loose shapes must still reach a registered name there, or this leg
  // would pass on shapes that had gone blind rather than quiet.
  const treeHits = [...PAGES.values()].flatMap(unruledHits);
  const realUnexcluded = [
    ...new Set(treeHits.filter((w) => !REGISTERED.has(w) && !excluded(w))),
  ];
  assert.deepEqual(
    realUnexcluded,
    [],
    `/docs prose reaches ${JSON.stringify(realUnexcluded)} in the loose slot and neither the ` +
      "class nor the stoplist excludes it — the shapes now read shipped English as a claimed " +
      "tool name (or the capture widened over the tree)",
  );
  // MEASURED AFTER THE PROSE REWRITE: the shipped corpus no longer puts a REGISTERED
  // name in the loose slot. /docs/chat used to say "exists over MCP as the
  // submit_feedback tool"; the converted page says "names submit_feedback as the one
  // tool of its own", and "one" is the PROSE_WORDS entry that absorbs it. So the old
  // `some(registered)` over the tree is false for a reason about the CORPUS, not
  // about the shapes — stated here rather than deleted quietly. Two assertions keep
  // this leg honest in that state: the shapes still reach shipped prose at all (so
  // this is measuring a corpus, not an empty haystack), and every REGISTERED name,
  // spelled in the loose slot, is still reached — which is the property the vanished
  // corpus hit was standing in for.
  assert.ok(
    treeHits.length > 0,
    "the loose shapes reach nothing in the shipped /docs tree — the corpus or the capture has moved",
  );
  const unreached = [...REGISTERED].filter((n) => !unruledHits(`as the ${n} tool`).includes(n));
  assert.deepEqual(
    unreached,
    [],
    `the loose shapes no longer reach registered tool(s) ${JSON.stringify(unreached)} in ` +
      `"as the <name> tool" — BARE_NAME has narrowed past a name the registry really has, so ` +
      "a doc calling that tool a tool would be invisible",
  );

  // LEG 5 — the class is CLOSED, auditable by eye, and cannot hide a real tool. The same
  // both-directions check PROSE_WORDS carries, on the rule that now does most of the work:
  // an entry that is a registered name would make every mention of that tool invisible.
  for (const word of FUNCTION_WORDS) {
    assert.match(
      word,
      /^[a-z]+$/,
      `the class entry ${JSON.stringify(word)} is not a plain English word — the class is a ` +
        "grammatical closed class (determinatives and pronouns), and an identifier in it is a " +
        "tool the guard can no longer see",
    );
  }
  const classIsRegistered = [...FUNCTION_WORDS].filter((w) => REGISTERED.has(w));
  assert.deepEqual(
    classIsRegistered,
    [],
    `the class entry/entries ${JSON.stringify(classIsRegistered)} are REGISTERED MCP tool ` +
      "names — the loose scan now ignores every doc that calls them a tool, which is the hole " +
      "this guard exists to close (rename the tool or move the entry, do not leave both)",
  );
  assert.ok(
    FUNCTION_WORDS.size <= 96,
    `the class has grown to ${FUNCTION_WORDS.size} entries — that is past the size of the ` +
      "closed class of English determinatives and pronouns, so it is being used as a stoplist " +
      "again: one word per failure, re-measured against whatever corpus is in front of it",
  );

  // LEG 6 — the pattern and the declared class must AGREE, in both loose shapes, for every
  // member. The rule is compiled into the regex from FUNCTION_WORDS, so a hand-written
  // alternation that drifts from the set (or an entry added to the set and not to the
  // pattern) is caught here rather than by an unexplained false positive on a docs page.
  for (const word of FUNCTION_WORDS) {
    assert.deepEqual(
      unruledHits(`as ${word} tool`),
      [word],
      `${JSON.stringify(word)} does not reach the loose slot at all ("as X tool") — this ` +
        "member of the class was measured against nothing, so LEG 2/4 cannot see it either",
    );
    for (const shape of ["as-the-tool/loose", "comma-the-tool/loose"]) {
      assert.deepEqual(
        namesClaimedByShape(shape, shape === "as-the-tool/loose" ? `as ${word} tool` : `${word}, the tool that`),
        [],
        `the ${shape} shape still claims ${JSON.stringify(word)} in its own slot — the class ` +
          "entry is declared but not compiled into that pattern, so ordinary English reaches " +
          "it as a claimed tool name",
      );
    }
  }

  // MEASURED RESIDUAL — stated, not fixed by this card, and pinned so it is a line of
  // output rather than a surprise in someone else's red. The comma shape's slot is an
  // appositive HEAD, and English heads an appositive with a NOUN as readily as with a
  // pronoun, so "The chat page, the tool that answers every message, is documented." still
  // reads as a claim on "page". It cannot be ruled out by class: the same slot is what
  // keeps the single-word phantom ("wipeall, the tool that removes everything") catchable,
  // and at that position a noun and a name are spelled identically — the same trade the
  // parenthetical shape makes in the other direction (its single-word leg above). If this
  // ever changes, re-derive it against the shipped form rather than deleting the leg.
  const NOUN_GLOSS = "The chat page, the tool that answers every message, is documented.";
  assert.deepEqual(
    namesClaimedByShape("comma-the-tool/loose", NOUN_GLOSS),
    ["page"],
    "the comma shape's noun-headed appositive no longer reproduces its measured residual — " +
      "either the shape was narrowed (then update WHAT IT DOES NOT COVER and delete this " +
      "leg, with the measurement) or it was widened (then check LEG 2)",
  );
});
