// The in-app assistant (phase 1) — the dashboard half of the read-only chat.
//
// WHAT CAN BREAK HERE, and why a source scan is the right guard:
//
//   1. THE BOUNDARY CLAIM DRIFTS FROM THE SERVER. The page tells the user the
//      assistant cannot change their account. That claim is only true if the
//      server dispatches its tools under a read-only scope. So this test reads
//      the Go source and pins the scope line, the route, and the per-user
//      limiter — a page claiming read-only over a server that is not read-only
//      is the worst possible bug for this feature, and it is invisible in every
//      UI-level test.
//   2. THE DOC LINK (AGENTS.md DOCUMENTATION PARITY step 4). The "?" button must
//      point at a docs page that exists and that actually documents the
//      boundary, and the docs sidebar must link it (an unreachable docs page is
//      an undocumented feature).
//   3. THE UNAVAILABLE DIRECTION. A deployment without a usable LLM credential
//      reports available:false and the page must stop offering the box. Offering
//      a box that always errors is worse than no box, and inverting that read
//      turns a feature that is off into one that appears broken. The flag itself
//      must mean "can this deployment answer" — never "was a runner object
//      built", which is true even with no credential at all.
//   4. THE NAV ENTRY, and that /chat exists — a nav link to a missing page is a
//      404 in the sidebar.
//   5. THE FILING IS ANNOUNCED. The card requires the user to SEE a filing
//      happen; if the `filed` branch is dropped from the transcript the thread
//      still appears on /feedback, so nothing else would fail.
//   6. The i18n keys, in all five locales.
//   7. THE TRANSLATION HALF OF THE SAME FILE THAT #747'S SCOPE IS GUARDED IN.
//      The availability fetch passes runLoad an `errorKey` routing the server's
//      machine `code` through lib/apiErr, so a refused request reads as the
//      localized reason rather than the page's generic "the assistant is
//      unavailable". Losing ONLY that half is invisible to the whole suite —
//      measured, not reasoned about (see the test's comment for the numbers) —
//      so this file pins it in the direction that was missing. The scope half
//      and the translation half are pinned by the two adjacent tests.
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the rendered transcript, the scroll, or the Enter-to-send key
// handling. The server-side behaviour (read-only refusal, filing, limits) is
// pinned by the Go tests, which is where that behaviour actually lives. The
// translation half is pinned ONLY once the tree carries the #6177 mechanism —
// the guard says which state it judged rather than passing quietly, and it
// cannot make the mechanism's own absence a failure (that is a different card's
// business, and this file must stay green on the tree that has no lib/apiErr).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, relative } from "node:path";

import { DICT, LANGS } from "../components/i18nDict.ts";
// The repo's shared comment/JSX-text-aware blanker (see its header): a class or
// an option quoted in a comment must neither raise nor satisfy an assertion.
// Imported rather than re-implemented — the copy this replaces had a defect that
// only surfaced on a page neither guard had seen yet.
import { codeOnly } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));
const readAbs = (abs: string) => readFileSync(abs, "utf8");
const appDir = fileURLToPath(new URL("../app", import.meta.url));

/**
 * Every `page.tsx` under `src/app`, as absolute paths.
 *
 * Module scope rather than inside a test: two guards read this census now (the
 * workspace scope, and the machine-code translation on runLoad call sites), and
 * a page that one of them judges must be judged by both. Hoisted so the two
 * cannot drift into looking at different sets of files.
 */
function walkAppPages(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) out.push(...walkAppPages(p));
    else if (entry === "page.tsx") out.push(p);
  }
  return out;
}
const pages = walkAppPages(appDir);

/** The source root (`web/src`), for censuses that are not page-shaped. */
const srcDir = fileURLToPath(new URL("..", import.meta.url));

/**
 * Every source file under `src` that MENTIONS `runLoad` — pages, components and
 * lib alike — as paths relative to `src`.
 *
 * Found by content, not by an allowlist of names: the guard's job is to judge
 * every call site that exists, and a hard-coded list of files is how a new one
 * ships unjudged. `.test.*` files are excluded (they hold expected strings, not
 * call sites).
 */
function walkRunLoadFiles(dir: string, base: string = dir): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = `${dir}${e.name}`;
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name.startsWith(".")) continue;
      out.push(...walkRunLoadFiles(`${full}/`, base));
    } else if (
      (e.name.endsWith(".tsx") || e.name.endsWith(".ts")) &&
      !/\.test\.tsx?$/.test(e.name) &&
      readFileSync(full, "utf8").includes("runLoad(")
    ) {
      out.push(full.slice(base.length));
    }
  }
  return out.sort();
}

// THE CHAT PAGE IS READ THROUGH `codeOnly` (t_9b40f272), not as raw bytes.
//
// This const is the ONE reader of app/chat/page.tsx in this file, and a dozen
// tests make POSITIVE claims about what the user reads off it. Read raw, a
// `//` line or a `{/* … */}` JSX comment satisfied those claims: measured on
// main 81946afc, `page.includes("chat.refused")` ("a refused tool call is shown,
// not swallowed") was passing on a COMMENT alone — the rendered block calls
// refusalHeading()/refusalText() and never names the key — so the refusal's
// wording could stop being rendered while every unit gate stayed green (measured
// on this tree: 998 tests, 998 pass, 0 fail). `codeOnly` is the repo's shared, JSX-text-aware blanker
// (lib/jsxSource.ts); it keeps every byte offset, so the `slice` readers below
// are unaffected, and it is the same reader the docs-page tests use since
// t_629da97d.
const CHAT_PAGE = "../app/chat/page.tsx";
const page = codeOnly(read(CHAT_PAGE));
const shell = read("../components/Shell.tsx");
const docsNav = read("../app/docs/layout.tsx");
const DOCS_PAGE = "../app/docs/chat/page.tsx";

// The Go side of the same claim, read cross-language on purpose (the idiom of
// salesRows.test.ts / spamJudgeToggle.test.ts): "read-only" is one property
// living in two languages, and a UI that asserts it while the server does not
// enforce it is the failure this guard exists for.
const goChat = read("../../../api/internal/chat/chat.go");
const goChatProposal = read("../../../api/internal/chat/proposal.go");
const goRest = read("../../../api/internal/httpapi/rest_chat.go");
const goRestRoutes = read("../../../api/internal/httpapi/rest.go");
const goChatTools = read("../../../api/internal/mcpsrv/chat_tools.go");

// ── THE PROSE ARM: who the page credits with the BIN ────────────────────────
//
// WHAT IT POLICES. delete_form/restore_form are neither proposable nor in the
// chat's read allowlist, so the bin is the person's action on the forms page and
// the /docs/chat copy must never credit it to the assistant. The arm that
// shipped with the rework (t_3a321078) keyed on `assistant` IMMEDIATELY followed
// by a modal/bare verb AND a bin/trash noun within 120 chars AFTER that, so it
// caught exactly ONE phrasing ("the assistant can take a form as far as the
// trash"). Measured on the shipped page (t_48daca7f): three natural re-wordings
// that credit the assistant with the bin — the INFINITIVE ("ask the assistant to
// bin a form"), the PASSIVE ("a form can be binned by the assistant") and the
// BARE VERB with no trailing noun ("the assistant bins a form") — all passed it
// GREEN. A guard that under-tests the class it exists to police is the worst kind
// of green — and the widening above STILL shipped with a residue (t_5bdeda83):
// its first shape demanded a MODAL from a closed set immediately on the
// assistant, and its other shapes demanded the assistant be ADJACENT to the bin
// verb (or be the passive's agent). Six natural re-wordings therefore credited
// the assistant with the bin and all passed it GREEN:
//
//   perfect tense       "The assistant has binned a form."
//   perfect + movement  "The assistant has moved a form to the trash."
//   periphrastic future "The assistant is going to delete a form."
//   relative clause     "The assistant is the one who bins a form."
//   possessive genitive "Binning a form is the assistant's job."
//   fronted gerund      "Restoring a form is something the assistant does."
//   appositive          "The assistant, not you, deletes the form."
//
// This arm closes the enumerated residue AND, as of t_b3877cfb, replaces the
// enumeration itself with a STRUCTURAL rule. Four rounds each left a residue because
// the scan was a closed list of surface shapes (t_48daca7f, t_5bdeda83, t_fa053255);
// t_b3877cfb measured five more phrasings still GREEN and required that the class be
// closed structurally rather than by an eleventh and twelfth shape.
//
// THE RULE, in one sentence: normalise the clause, then assert the assistant is the
// SUBJECT or the AGENT of a bin verb. That is what "credits the assistant with the
// bin" means, and it does not depend on which surface shape carries it. The five the
// enumerated scan missed fall out of the same rule with no new shape:
//
//   "The assistant handles the binning."           light verb + gerund -> "<assistant> bins"
//   "Binning is what the assistant is for."         copular predication -> "<assistant> does"
//   "It is to the assistant that a form is binned." topicalised cleft   -> agent of the passive
//   "The binner of a form is the assistant."        agent noun subject  -> assistant is its predicate
//   "The assistant, whose job is to bin a form …"   non-restrictive rel -> "<assistant> to bin"
//
// and the mirror of the same predicate — the bin verb's subject IS the assistant
// ("trashing a form falls to the assistant", "who bins a form is the assistant") —
// lands in the fifth position below rather than in a shape of its own.
//
// WHAT "NORMALISE" DOES — each step is a rewrite toward the canonical subject+verb the
// rule reads, NOT a new surface shape:
//
//   * the appositive that disowns "you" — ", not you," / ", rather than you," /
//     ", instead of you," — is stripped. It disowns the reader, not the assistant, so
//     the sentence still credits the assistant.
//   * the it-cleft and the topicalised cleft are unfolded: "it is the assistant who
//     <v>" -> "the assistant <v>"; "it is to the assistant that <x> is binned" -> the
//     assistant is read as the passive's agent. Their DENIALS unfold too, with the
//     negation carried across ("it is not to the assistant that …" -> "the assistant
//     not …"), so the denial stays GREEN.
//   * the non-restrictive relative "the assistant, whose job is to <v>" is unfolded to
//     "the assistant <v>".
//   * the copular predication "what the assistant is for" is read as a light-verb
//     predication of the bin.
//   * an AGENT NOUN ("the binner of a form") is canonicalised to its verb, in both
//     orders: "the binner … is the assistant" and "the assistant is the binner".
//   * a light verb + gerund ("handles the binning", "is responsible for binning", "is
//     tasked with trashing") collapses to the verb.
//
// THE FIVE POSITIONS the rule reads are then (A) assistant = subject of a bin verb,
// (B) assistant = agent of a passive, (C) assistant's ROLE = subject ("the assistant's
// job is to bin" / "binning is the assistant's job"), (D) assistant = subject of a light
// verb with the bin earlier, and (E) assistant = the predicate the bin verb's subject
// lands on. See `binClaimInClause` for each.
//
// THE CLAUSE, NOT THE SENTENCE. That is the other half of the design: the page
// legitimately names the assistant in one clause and the bin in another ("… it
// requires a token the model never sees"), and a whole-sentence "assistant AND a bin
// verb somewhere" scan is exactly what forced the enumerated shapes to keep widening
// their windows. A clause boundary is a sentence terminator followed by whitespace, an
// em-dash, a semicolon, or a coordinating/subordinating conjunction. A comma is NOT a
// boundary, so an appositive or a list stays inside its sentence; the appositive is
// stripped first, and a list carries no assistant-subject bin verb.
//
// A DENIAL THE ASSISTANT OWNS IS NOT A CLAIM. The page is allowed to state the
// boundary, so a negation token (not/never/cannot/…) sitting BETWEEN the assistant and
// the bin token disqualifies the match. That keeps "the assistant cannot delete a
// form", "the assistant's job is not to bin a form", "it is not the assistant who
// bins" and "deleting a form is not the assistant's job" GREEN — the disowning is the
// bin's, not the assistant's. It does NOT fire on the person-attribution sentence
// ("The bin belongs to you, not the assistant — deleting a form from the forms page
// …"), because that sentence puts the assistant in an earlier clause than the bin.
//
// THE PRO-VERB ACROSS A CLAUSE BOUNDARY, which fixes the boundary's one cost. Because
// the bin is a verb the first clause carries and the second clause only POINTS AT
// ("Binning a form — the assistant does that."), a naive split loses the claim
// entirely: the second clause has no bin for the rule to read. The fix folds it back:
// when a clause's verb is a PRO-VERB followed by the anaphor that points at the clause
// before it ("does|handles|performs … that|it"), the pro-verb is rewritten to that
// clause's bin word, and the rule reads it in one clause like any other. Measured: this
// shape was RED under the previous scan (its window crossed the em-dash, because an
// em-dash is not a sentence terminator) and GREEN under the split, i.e. the split alone
// REGRESSED it — it is in the BITES map below so the regression cannot come back. The
// fold is the boundary's normalisation, not a tenth position, and it also closes the
// same pro-verb at a semicolon or a conjunction, where the previous window crossed
// those too.
//
// A REPORT IS NOT AN ACTION, the boundary the rule needs on the other side. The bin
// token is usually a NOUN ("the trash"), so a clause merely NAMING it in what the
// assistant reads or reports — "the assistant can tell you where the trash is" — puts
// "assistant … trash" inside position (A)'s window while crediting the assistant with
// nothing. When the assistant's own verb is an information verb (tell/show/list/…), the
// bin token later in its clause is its object of knowledge, and (A) stands down; the
// same sentence with the bin as the assistant's AGENT ("… binned by the assistant") is
// still RED, because (B) does not depend on the verb introducing it.
//
// THE VERB ALONE IS NOT THE REPORT, the boundary this arm's information-verb paragraph
// needs. "A REPORT IS NOT AN ACTION" was first read on the assistant's own verb alone,
// and the verb alone cannot tell a report from a claim: "the assistant tells you where
// the trash is" and "the assistant tells you IT BINS a form" carry the SAME verb, and the
// second credits the assistant with the bin inside the clause the first only points at.
// Measured (t_9717fec1): reading the verb alone suppressed seven genuine claims the head
// scan caught — "tells you it bins", "reports that it deleted", "says it can trash",
// "shows you it can delete", "tells you it has binned", "explains that it restores",
// "mentions it deletes". So the guard now reads the REPORTED CLAUSE too: the information
// verb stands (A) down only when nothing after it puts the assistant back in the bin
// verb's subject position, which is the object-of-knowledge reading the paragraph above
// describes. A bin token the assistant is merely the REPORTER of still stands (A) down —
// see `assistantBinsInsideReport`.
//
// THE INFORMATION VERB BEHIND AN AUXILIARY CHAIN, and the infinitive that keeps the catch
// (t_b0bf328c). "A REPORT IS NOT AN ACTION" reads the assistant's own verb, so the verb has
// to come out of the verb PHRASE, not out of its last auxiliary: the words in `ASSISTANT_AUX`
// STACK ("has been telling", "will be telling", "is going to tell"), and a reader that strips
// at most ONE of them returns "been"/"be"/"going" — no information verb — so a pure report
// behind a two- or three-word chain was red. `assistantVerb` now strips the WHOLE chain, so
// the report stands down. The same strip would have removed a genuine CATCH the parent's own
// auxiliary-blindness was holding: "the assistant has been asked TO BIN a form" reads the
// auxiliary on the parent (no information verb, (A) holds, red), and recognising "asked"
// stands (A) down unless the BARE BIN INFINITIVE after it is read back as the claim position
// (A)'s infinitive shape already catches ("ask the assistant to bin") — `binInfinitiveAfterVerb`
// is that read. Measured over this card's battery: on the stacked base the fix reds 0 of the
// report controls and 6 genuinely-RED claim classes are held (not lost); on `main`, where
// #1069's verb-alone stand-down is absent, the same strip would remove those catches outright —
// this card is only safe stacked on the parent, exactly like t_31293346.
//
// RE-CUT ONTO `main` (t_cee3d61f). That stacked-base measurement is the HISTORIC one and is left
// as it was written; its "only safe stacked" conclusion held because both of this card's
// prerequisites were absent from the `main` of that day. Both have landed since — #1069's
// verb-alone stand-down and #1073's particle group — so the cut is re-derived on today's `main`
// rather than retargeted, and the reader keeps #1073's `(?:\s+up\b)?` tail as well as the list
// keeps its `(?:looks?|looked|looking)\s+up` group, because the #1074 head predated #1073 and a
// naive re-cut drops both. Re-measured on these bytes: 0 catches removed over the claim battery
// (identical to the #1074 head, both 208 RED) and the 140 report sentences that battery reds on
// today's `main` are all GREEN here.
//
// THE GERUND THE INFORMATION VERB REPORTS (t_7b69a77a), the boundary one position over,
// now decided rather than documented. An information verb's OBJECT can be a bin GERUND —
// "the assistant reports deleting a form" — and the reported clause puts no assistant
// pronoun back in the bin verb's subject position, so the clause reading above stood the
// claim down. It is a genuine claim (the assistant IS the deleter) that the head scan
// 3542c039 caught and t_9717fec1's guard lost, measured on the same extracted scans. The
// same string also carries the opposite reading — "the assistant says binning a form is
// easy", in which the gerund is the SUBJECT of a clause of its own and credits the
// assistant with nothing — so the two are told apart by the gerund's ROLE in the reported
// clause, resolved in `reportedGerundIsSubject`: the gerund is the information verb's
// OBJECT (the claim) when nothing predicates of it, and the clause's SUBJECT (a report)
// when the reported clause has anything of its own after the gerund phrase.
//
// WHAT THE ROLE READING GOT WRONG FIRST (t_255182df, this rework). The first cut decided
// the role from a curated 47-item list of finite verbs, and its handoff declared "0 NEW
// false positive" — measured over a corpus that only carried predicates FROM that list, so
// the claim was true of the corpus and false of the rule. A genuine SUBJECT-gerund REPORT
// whose predicate was a real finite verb OUTSIDE the list — "binning a form REMOVES it from
// the list", "restoring a form BRINGS it back" — was read as the information verb's object
// and RED, exactly as "… deleting a form" (a claim) is. That is the UNSAFE direction, and
// it was structurally identical to this rule's OWN accepted control below ("explains
// that binning a form IS permanent"): the same shape, split only by whether the predicate
// happened to be curated. Over a 2784-sentence report corpus of that shape the first cut
// reded 2784 where origin/main reds 0. The role is now read from the clause's SHAPE — the
// gerund phrase is consumed and anything left over is the predication — so the rule fails
// toward the REPORT reading whenever a predication is present, whatever verb carries it.
// Only a clause that is the gerund phrase and nothing else ("reports deleting forms", the
// claim, vs "reports deleting works", the report) still needs a verb list, because there
// the two readings differ by the token's identity alone; see `AMBIGUOUS_VERB`.
//
// The header's earlier claim that the ONLY subject-gerund strings the rule reds are the
// "has/had told you" PAST-tense class was FALSE and is withdrawn: the first cut reded that
// class AND every non-curated-predicate report above, and the past-tense class is not red
// by this rule at all (it is caught by the report paragraph's own window). Measured on the
// structural rule: 0 NEW false positives vs origin/main over a 22464-sentence report corpus
// that includes the non-curated predicates the first cut reded.
//
// THE PAST-TENSE CLASS IS FIXED (t_31293346, re-cut onto main by t_aaf569ee). The header
// paragraph ABOVE this note used to end by naming the "has/had told you" subject-gerund
// strings as still RED — the one class the guard's own window red, because the information
// verb's PAST-tense form was not in `INFO_VERB`, so `assistantBinsInsideReport` never ran
// for it and position (A) held. That was a second, independent defect, closed by widening
// `INFO_VERB` to every surface form of the verb (the missing past, and the progressive):
// those strings now stand down like the present-tense ones, and the past/perfect/
// progressive report rows are pinned in the report controls below. See the `INFO_VERB`
// comment. (The paragraph above already withdrew the OLDER claim that the past-tense class
// was the only red class; both facts — that the class was red, and that it is now fixed —
// are true of different rules.)
//
// THE REPORTED CLAUSE'S AUXILIARY IS A CHAIN TOO (t_d7178bff). The past-tense paragraph
// above fixed the information verb's form; this closes the SAME reader's other half, one
// axis over. The reported clause's reader (`assistantBinsInsideReport`) accepted AT MOST ONE
// auxiliary between its subject pronoun and the bin verb, so a reported CLAIM whose verb
// phrase carries a chain had no match at all and was read as a report — a false NEGATIVE,
// the opposite direction from t_b0bf328c's false positive and at a different call site. The
// seven the card names were measured GREEN on main, on the stacked base and on t_b0bf328c's
// head: \"tells you it has been binning\", \"will be deleting\", \"is going to delete\",
// \"reports that it has been trashing\", \"says it has been restoring\", \"mentions it had been
// deleting\", \"explains that it is about to bin\". The single-auxiliary shapes were caught
// and hid it — they are the reader's own controls. Measured with the scan extracted from this
// file: a 420-sentence claim battery (7 information verbs x 10 chains x 4 bin verbs) goes
// 280 missed -> 0, and a 616-sentence report battery gains 0 false positives. The group now
// takes the WHOLE chain and the copula is tested against its LAST auxiliary — the only one
// the passive/progressive distinction reads — so \"… it has been BINNED\" stays a report while
// \"… it has been BINNING\" is the claim; testing the chain as an ATOM would red the passive.
//
// Everything a single clause can PREDICATE of the assistant — including the reported
// clause of an information verb that puts the assistant back in the bin verb's subject
// position, and the bin gerund such a verb reports — is inside the five positions and the
// fold above, and is proved by the BITES map below. The report/reference reading the
// boundary exists to protect is STILL decided inside position (A) rather than by a
// position of its own, and its own controls are pinned below.
//
// Split out of the assertions so the bite proof below runs the IDENTICAL scan against
// a mutated page: a proof that exercised a paraphrase would prove nothing about the
// scan (the idiom of trashCopyClaims.test.ts's scanClaims).
const BIN_VOCAB = String.raw`(?:bins?|binned|binning|trash(?:es|ed|ing)?|restores?|restored|restoring|deletes?|deleted|deleting)`;
// The AGENT NOUNS for the same four actions — "the binner", "the deleter". They are
// canonicalised to the verb before the rule runs, so an agent noun as the sentence's
// subject ("the binner of a form is the assistant") credits the assistant just as the
// verb does.
const BIN_NOUN = String.raw`(?:binner|binners|trasher|trashers|restorer|restorers|deleter|deleters|destroyer|destroyers)`;
// The possessive-genitive ROLE words: "the assistant's job/task/role is to bin".
const BIN_ROLE = String.raw`(?:job|task|work|role|duty|responsibility|purpose|part)`;
// The auxiliaries that can sit between the assistant and its bin verb. Not a closed
// set of the verb's shapes — just the words a verb phrase may open with — so the rule
// does not have to be re-taught when a modal the copy had not used before appears.
//
// AN AUXILIARY CHAIN, NOT ONE AUXILIARY (t_b0bf328c). These words STACK: a modal over a
// perfect over a progressive (“has been telling”, “will be telling”), a modal over the
// periphrastic future (“is going to tell”), a perfect over a passive (“has been asked”).
// `assistantVerb` below strips the WHOLE chain, because reading the verb after only the
// FIRST auxiliary returns the chain’s last auxiliary — “been” for “has been telling”,
// “be” for “will be telling”, “going” for “is going to tell” — and none of those is an
// information verb, so a pure REPORT behind a chain was read as an ACTION and red. The
// single-auxiliary shapes (“has told you”, “can tell you”) were already GREEN, which is
// why the gap was invisible. The strip is safe in ONE direction on purpose: no information
// verb is a member of this set (the two sets do not intersect), so consuming every
// auxiliary can only ever expose a real verb token, never swallow one.
const ASSISTANT_AUX = String.raw`(?:can|may|will|would|could|should|shall|must|might|has|have|had|is|are|was|were|be|been|being|does|do|did|takes?|moves?|going\s+to|about\s+to|due\s+to|needs?\s+to|wants?\s+to|to)`;
// A copula, as a word (" is") or a contraction ("'s" / "’s"), for the cleft rewrites.
const COPULA = String.raw`(?:\s+(?:is|was|are|were)|(?:'|’)s)`;
// The NEGATION tokens. A negation BETWEEN the assistant and the bin token is a denial
// the assistant OWNS and disqualifies the match; see the header.
const NEGATION = String.raw`(?:not|never|cannot|can't|won't|doesn't|don't|didn't|isn't|aren't|wasn't|weren't|hasn't|haven't|hadn't|without)`;
/**
 * A lazy span that refuses to cross a negation token — the between-ness is the rule,
 * so the span IS the guard and the positions below share it. The bound (120) is the
 * reach a single clause's verb phrase can have; it cannot cross a clause boundary
 * because the text is split first.
 */
const noNeg = (span: string) => `((?:(?!\\b(?:${NEGATION})\\b)${span}){0,120}?)`;
const HAS_NEGATION = new RegExp(`\\b(?:${NEGATION})\\b`, "i");
// The assistant's INFORMATION verbs — the ones under which a later bin token is an
// object of knowledge, not an action. Read at the assistant's own verb in a match (A)
// makes; see the header's "A REPORT IS NOT AN ACTION".
const INFO_VERB = String.raw`(?:tells?|told|telling|shows?|showed|shown|showing|lists?|listed|listing|reads?|reading|saw|seen|sees?|seeing|asks?|asked|asking|says?|said|saying|reports?|reported|reporting|knows?|knew|known|knowing|finds?|found|finding|explains?|explained|explaining|describes?|described|describing|answers?|answered|answering|checks?|checked|checking|(?:looks?|looked|looking)\s+up|mentions?|mentioned|mentioning)`;
// THE VERB'S TENSE IS PART OF THE VERB (t_31293346, re-cut t_aaf569ee). This list is
// read where the assistant's OWN verb decides whether a later bin token is an action or
// an object of knowledge, so every surface form of the verb has to be in it — not just
// the forms a present-tense sentence happens to use. It shipped with the present-simple
// and third-person forms (`tells?`, `shows?`, `reports?`, …) plus a few irregular pasts,
// so a pure REPORT in the PAST or the PERFECT — "the assistant has told you where the
// trash is" — or in the PROGRESSIVE — "the assistant is telling you where the trash is"
// — was read as if `told`/`telling` were an ACTION verb: position (A)'s own window
// (`assistant` + up to 120 chars + a bin token) flagged the sentence and the
// information-verb guard never stood it down. That is a false positive that blocks the
// page from stating a fact about the bin. The gap was invisible because the shapes the
// guard's own controls use are present-tense. Widened here: `told` (the missing
// irregular past) and the `-ing` form of every listed verb — leaving the particle
// alternative exactly as this file's `look up` fix writes it. Measured with the scan
// extracted from THIS file, over a 420-sentence report battery (60 verb phrases x 7
// readings): false positives on the report readings go 119 -> 0, with 0 new false
// negatives and 0 catches removed (a 138-sentence object-gerund claim battery stays
// 138/138 RED).
// A SUBORDINATOR right after the assistant opens a clause of its own — "ask the
// assistant WHETHER a form is in the trash" — so the bin token beyond it is that
// clause's, not the assistant's. Same disqualification as an information verb, same
// reason: the assistant is named without being the actor. (Only the two subordinators
// that cannot also be a relative pronoun: "which"/"where" would collide with a
// relative clause and are not listed.)
const SUBORDINATOR = String.raw`(?:whether|if)`;
/** The token after the assistant that means it is NOT the actor of the later bin. */
const HAS_NON_ACTION = new RegExp(`\\b(?:${INFO_VERB}|${SUBORDINATOR})\\b`, "i");
// The bin GERUNDS, for the reported clause that is headed by one — "reports deleting a
// form". Only the `-ing` forms: the NOUN reading ("the trash") is handled by the
// information-verb paragraph, and a gerund is what can be either an object or a subject.
const BIN_GERUND = `(?:binning|trashing|restoring|deleting)`;
/**
 * The QUANTIFIER determiners — the class t_82129fb4 adds to `DET`.
 *
 * Two shapes, because a quantifier can stand alone ("deleting EVERY form") or follow an
 * article that it belongs with ("deleting THE WHOLE form", "an ENTIRE form", "A FEW
 * forms"). Both are determiners and both are listed BEFORE the article alternatives in
 * `DET`, so the compound wins the alternation: if `the` matched first, the quantifier word
 * would fall through to the bare `[a-z]+` and the defect would simply reappear one token
 * over. Named and set apart so the bite test below can run the shipped rule with exactly
 * this class removed (`DET_WITHOUT_QUANT`) and show the rows it guards return GREEN.
 */
const QUANT_DET = `(?:the|an?)\\s+(?:whole|entire|other|same|few|little|lot|couple|number|majority|handful|dozen|rest|remainder)|every|each|all|both|no|many|several|most|whole|entire|one|two|three|four|five|six|seven|eight|nine|ten|few|little|much|other|same|various|numerous|countless`;
/** The articles, possessives and demonstratives — `DET`'s non-quantifier half. */
const ARTICLE_DET = `the|a|an|your|my|his|her|its|their|our|this|that|these|those|some|any`;
/**
 * The determiners that can open the gerund's object — the QUANTIFIERS above, then the
 * articles, possessives and demonstratives.
 *
 * THE QUANTIFIERS ARE DETERMINERS TOO (t_82129fb4). Their absence was the defect: with no
 * alternative for every|each|all|both|no|many|…, `NP_OBJ`'s optional determiner matched
 * nothing and the bare `[a-z]+` ate the QUANTIFIER as if it were the object noun —
 * "deleting EVERY form" consumed "deleting every", left " form." behind as a phantom
 * predication, and the rule read the gerund as a SUBJECT and stood the claim down. That is
 * a false negative (the claim goes GREEN), so no report was ever newly red on it and it
 * slipped through; it is a gap against the pre-rework branch ef3af046, which caught all of
 * them. Measured on the extracted scan: the 480-sentence object-claim corpus goes from
 * 240 missed at the pre-fix head to 0 here, with 0 catches lost and 0 NEW report-side
 * false positives (an 11760-sentence report corpus built from the quantifier objects and
 * predicates OUTSIDE the pre-rework curated list stays 0/11760 red, exactly as on
 * origin/main).
 */
const DET = `(?:${QUANT_DET}|${ARTICLE_DET})`;
/**
 * The pre-fix determiner set — the articles, possessives and demonstratives WITHOUT the
 * quantifiers, i.e. exactly `DET` before t_82129fb4. Kept (not deleted) so the bite test
 * below can run the REAL rule with the quantifier handling removed: the card's mutation is
 * "drop the quantifier handling", and reverting `DET` to this set is that mutation verbatim
 * rather than a paraphrase of it.
 *
 * GROUPED, and it must be. The pre-fix `DET` was itself the grouped alternation
 * `(?:the|a|an|…|any)`, and `npObj` composes the determiner as `(?:${det}\s+)?[a-z]+`. An
 * UNGROUPED article set (`the|a|an|…|any`) would let the `\s+` bind to the LAST alternative
 * only (`any`), so for every other article/possessive/demonstrative the determiner would not
 * be consumed and the phrase would leave the very phantom predication this card is about —
 * i.e. the "pre-fix" oracle would not be the pre-fix rule. Measured on the 480-claim corpus
 * (6 info verbs x 4 bin gerunds x 20 objects): ungrouped disagrees with the real pre-fix scan
 * on 140/480 rows, grouped on 0/480.
 *
 * The grouping is ASSERTED, not only documented: the t_82129fb4 test below requires an
 * article-object claim the real pre-fix scan CAUGHT to be caught by THIS oracle, so reverting
 * the value to the ungrouped `ARTICLE_DET` reds the suite instead of passing silently
 * (t_93ef586c).
 */
const DET_WITHOUT_QUANT = `(?:${ARTICLE_DET})`;
/** The prepositions that can open a phrase that continues the gerund's noun phrase. */
const G_PREP = `(?:from|to|into|out\\s+of|onto|upon|in|on|at|with|for|about|by|via|through|across|under|over|within|without|before|after|during|against|between|among|of)`;
// The gerund's OWN OBJECT — one common noun, the shape the object reading uses
// ("a form", "forms", "the trash"). Held to ONE noun on purpose: a second noun here is
// indistinguishable from the reported clause's finite verb ("… a form CONTINUES"), and
// reading that verb as part of the object would green a genuine claim.
//
// A FUNCTION OF THE DETERMINER CLASS, not a constant, so the bite test below can rebuild the
// gerund phrase from a determiner set with the QUANTIFIERS removed and run the REAL rule on
// it. The mutation the card names is exactly "drop the quantifier handling" (t_82129fb4),
// and a proof that paraphrased the rule would prove nothing about the scan — the file's own
// doctrine (see the note above `BIN_VOCAB`). The default argument is the shipped `DET`, so
// every production call site is unchanged.
const npObj = (det: string) => `(?:${det}\\s+)?[a-z]+`;
// The noun phrase INSIDE a prepositional phrase, where a compound is unambiguous because
// the preposition already opened a phrase ("… from the forms PAGE"). One or two nouns.
const NP_PP = `(?:${DET}\\s+)?[a-z]+(?:\\s+[a-z]+)?`;
/**
 * A prepositional phrase continuing the gerund's noun phrase — the adjunct an
 * OBJECT-reading clause hangs off it ("deleting a form FROM THE FORMS PAGE"). Consumed as
 * part of the gerund's phrase so a trailing adjunct is not mistaken for the predication
 * that decides the gerund's role; any number of them, because an object reading may carry
 * more than one.
 */
const GER_PP = `\\s+${G_PREP}\\s+${NP_PP}`;
/** The gerund heading the reported clause: "deleting a form", "the binning". */
const GER_HEAD = `^\\s+(?:the\\s+)?${BIN_GERUND}\\b`;
// The finite verbs that are ALSO plural nouns — "works", "counts", "matters". For a clause
// that is the gerund phrase and nothing else the only token that can make the gerund a
// SUBJECT is one of these ("reports deleting WORKS" = deleting works, a report), while a
// plain plural object ("reports deleting FORMS") leaves the gerund the information verb's
// object (the claim). The two readings are the same shape, so THAT shape alone is resolved
// from the list; every longer clause is resolved structurally, above, with no list at all.
const AMBIGUOUS_VERB = `(?:is|are|was|were|becomes?|remains?|seems?|appears?|looks?|sounds?|can|will|would|shall|should|may|might|must|has|have|had|does|do|did|means?|matters?|helps?|needs?|takes?|makes?|lets?|wastes?|causes?|leaves?|requires?|involves?|creates?|costs?|gives?|keeps?|stays?|turns?|gets?|works?|counts?|depends?|differs?|varies?|lasts?|holds?|stands?|applies?|fits?|fails?|passes?|brings?|removes?|deletes?|bins?|trashes?|restores?)`;
/**
 * The reported clause HEADED BY A BIN GERUND, and the gerund's role in it — the decision
 * t_7b69a77a needed (the header's "THE GERUND THE INFORMATION VERB REPORTS"). `rest` is
 * the text after the information verb. True means the gerund is the reported clause's
 * SUBJECT (a report about the bin, nothing predicated of the assistant), so position (A)
 * stands down; false means no such predication was read, i.e. the gerund is the
 * information verb's OBJECT and the assistant IS the deleter, which is the claim.
 *
 * The gerund is the SUBJECT when the reported clause PREDICATES something of it — when the
 * clause does not END at the gerund phrase. So the role is decided STRUCTURALLY, not from a
 * curated list of finite verbs: the gerund and its own noun phrase (its object and any
 * adjunct PPs) are consumed, and whatever is left is the predication. Anything left over —
 * "… IS permanent", "… REMOVES it from the list", "… BRINGS it back" — makes the gerund a
 * subject; nothing left over ("… deleting a form FROM THE FORMS PAGE") leaves it the
 * information verb's object.
 *
 * WHY NOT A LIST OF FINITE VERBS (t_255182df). The first cut of this rule read the
 * predicate off a curated 47-item list, so a reported clause whose predicate was a real
 * finite verb OUTSIDE it — "binning a form REMOVES it from the list" — was read as the
 * information verb's object and reded a genuine report. That is the UNSAFE direction: a
 * list can only ever name the predicates already seen, and the copy it has not seen is
 * exactly the copy it must not block. So the predicate is now the clause's SHAPE — whatever
 * trails the gerund phrase — and the rule fails toward the report reading whenever a
 * predication is present.
 *
 * The one shape SHAPE cannot decide is a clause that is the gerund phrase and nothing else:
 * `AMBIGUOUS_VERB` above is the finite-verb list kept for exactly that shape, because
 * "reports deleting works" (a report) and "reports deleting forms" (a claim) differ only by
 * the token's identity. Every clause with more than that token is list-free.
 */
function reportedGerundIsSubject(rest: string, det: string = DET): boolean {
  // The gerund and its noun phrase: the gerund, an optional one-noun object, any PPs.
  const phrase = new RegExp(`${GER_HEAD}(?:\\s+${npObj(det)})?(?:${GER_PP})*`, "i").exec(rest);
  if (!phrase) return false;
  // Whatever the gerund phrase does not account for is a PREDICATION of it: the reported
  // clause has a verb of its own, so the gerund is that clause's SUBJECT (a report). Only
  // trailing punctuation is not a predication.
  const remainder = rest.slice(phrase[0].length).replace(/[\s.,;:!?\u2014-]+/g, "");
  if (remainder.length > 0) return true;
  // Nothing after the gerund phrase: the ambiguous one-token clause (§ AMBIGUOUS_VERB).
  return new RegExp(`${GER_HEAD}\\s+${AMBIGUOUS_VERB}\\s*[.!?]*$`, "i").test(rest);
}

// Clause boundaries, and why each is one. A sentence terminator must be followed by
// whitespace (or end the text), so a dotted token the copy really uses — AGENTS.md,
// jomform.com — is not torn in half. An em-dash and a semicolon start a new clause.
// A comma does NOT (an appositive or a list stays inside its sentence).
const CLAUSE_SPLIT = /(?<=[.!?])\s+|\s*[—;]\s*|\s+(?:and|but|so|then|while|whereas|although|though|because)\s+/i;

/**
 * Rewrite a clause toward the canonical subject+verb the rule reads. Every step is a
 * normalisation; the rule below is the SAME subject-or-agent-of-a-bin-verb test
 * whatever surface form arrived.
 */
function normaliseClause(raw: string): string {
  let s = ` ${raw.toLowerCase()} `;
  // the appositive that disowns "you": ", not you," / ", rather than you," ...
  s = s.replace(/,\s*(?:not\s+you|not\s+yourself|never\s+you|rather\s+than\s+you|instead\s+of\s+you)\s*,/g, " ");
  // the topicalised cleft: "it is to the assistant that ..." -> "the assistant ..."
  s = s.replace(new RegExp(`\\b(?:it|this|that|there)${COPULA}\\s+to\\s+(?:the\\s+)?assistant\\b`, "g"), " the assistant");
  // its DENIAL: "it is not to the assistant that <x> is binned" — the topicalised
  // cleft with a negated expletive. Unfolded to "the assistant not <x>", so the
  // negation lands between the assistant and the bin and the clause stays GREEN (a
  // denial the assistant owns).
  s = s.replace(
    new RegExp(`\\b(?:it|this|that|there)${COPULA}\\s+(not\\s+|never\\s+)to\\s+(?:the\\s+)?assistant\\b`, "g"),
    (_m: string, neg: string) => ` the assistant ${neg}`,
  );
  // the it-cleft: "it is the assistant who|that <v>" -> "the assistant <v>"
  s = s.replace(
    new RegExp(`\\b(?:it|this|that)${COPULA}\\s+(not\\s+|never\\s+)?(?:the\\s+|a\\s+|an\\s+)?assistant\\s+(?:who|that)\\b`, "g"),
    (_m: string, neg: string) => ` the assistant ${neg || ""}`.replace(/\s+$/, ""),
  );
  // the non-restrictive relative: "the assistant, whose job is to <v>"
  s = s.replace(
    new RegExp(`\\b(the\\s+)?assistant\\s*,\\s*whose\\s+${BIN_ROLE}\\s+is\\s+(not\\s+|never\\s+)?to\\b`, "g"),
    (_m: string, the: string, neg: string) => ` ${the || ""}assistant ${neg || ""}to`,
  );
  // the copular predication: "binning is what the assistant is for"
  s = s.replace(/\bwhat\s+(?:the\s+)?assistant\s+is\s+for\b/g, "something the assistant does");
  // an AGENT NOUN as the subject, the assistant as its predicate:
  //   "the binner of a form is the assistant" -> "the assistant bins"
  s = s.replace(
    new RegExp(`\\b(?:the\\s+|an?\\s+)?${BIN_NOUN}\\b[^.!?]{0,40}?\\b(?:is|was|are|were)\\s+(not\\s+|never\\s+)?(?:the\\s+|an?\\s+)?assistant\\b`, "g"),
    (_m: string, neg: string) => ` the assistant ${neg || ""}bins`,
  );
  // the assistant predicated of an AGENT NOUN: "the assistant is the binner" -> "… bins"
  s = s.replace(
    new RegExp(`\\bassistant\\b\\s+(?:is|was)\\s+(not\\s+|never\\s+)?(?:the\\s+|an?\\s+)?${BIN_NOUN}\\b`, "g"),
    (_m: string, neg: string) => ` assistant ${neg || ""}bins`,
  );
  // a light verb + gerund collapses to the third-person verb: "handles the binning" -> "bins"
  for (const [finite, gerund] of [["bins", "binning"], ["trashes", "trashing"], ["restores", "restoring"], ["deletes", "deleting"]]) {
    s = s.replace(
      new RegExp(`\\b(?:handles?|does|do|did|performs?|takes?\\s+care\\s+of|is\\s+responsible\\s+for|is\\s+tasked\\s+with|is\\s+in\\s+charge\\s+of|is\\s+for|is\\s+about)\\s+(?:the\\s+)?${gerund}\\b`, "g"),
      finite,
    );
  }
  // the relative clause "is the one who <v>" -> <v>
  s = s.replace(new RegExp(`\\b(?:is|was|are|were)\\s+(?:the\\s+|an?\\s+)?one\\s+(?:who|that)\\s+`, "g"), " ");
  return s.replace(/\s+/g, " ").trim();
}

/**
 * The assistant's verb phrase in a match (A): the WHOLE auxiliary chain stripped, then the
 * verb token, then whatever follows it. ONE reader, so `assistantVerb` and the bare-infinitive
 * check below (`binInfinitiveAfterVerb`) cannot drift in how they strip a chain.
 *
 * The auxiliary chain is stripped WHOLE (t_b0bf328c). `ASSISTANT_AUX` is a set of words that
 * STACK, so a verb phrase may open with two or three of them ("has been telling", "will be
 * telling", "is going to tell"). Reading the verb after only the FIRST auxiliary returns the
 * chain's last auxiliary — "been", "be", "going" — and HAS_NON_ACTION reads none of those as
 * an information verb, so a pure REPORT behind a chain was red. One token is not the verb
 * phrase; the chain is. The strip is safe in ONE direction on purpose: no information verb is
 * a member of that set (the two sets do not intersect), so consuming every auxiliary can only
 * expose a real verb token, never swallow one.
 */
function assistantVerbPhrase(matched: string): { verb: string; after: string } | null {
  const m = /^assistant\b\s+/i.exec(matched);
  if (!m) return null;
  let rest = matched.slice(m[0].length);
  const chain = new RegExp(`^(?:(?:${ASSISTANT_AUX})\\b\\s*)+`, "i").exec(rest);
  if (chain) rest = rest.slice(chain[0].length);
  const verb = /^[a-z]+(?:'[a-z]+\b)?(?:\s+up\b)?/i.exec(rest);
  if (!verb) return null;
  return { verb: verb[0].toLowerCase(), after: rest.slice(verb[0].length) };
}

/**
 * The assistant's OWN verb in a match (A) made against the normalised clause — the verb
 * that decides whether the later bin token is an action or an object of knowledge.
 * Returns null when the verb is the assistant's own bin verb ("the assistant bins"),
 * which is the claim itself.
 */
function assistantVerb(matched: string): string | null {
  return assistantVerbPhrase(matched)?.verb ?? null;
}

/**
 * True when the assistant's verb phrase is followed by a BARE BIN INFINITIVE — "has been
 * asked TO BIN a form", "can be told TO DELETE a form". There the bin token is the
 * information verb's own COMPLEMENT and the assistant is the one who would bin it, which is
 * the claim position (A)'s infinitive shape already catches from the other end ("ask the
 * assistant to bin a form"). It matters only because the CHAIN strip above exposes the
 * information verb behind its auxiliaries: on the parent tree ("has been asked to bin") the
 * chain read the auxiliary "been", which is no information verb, so position (A) held and
 * the claim was red; recognising the verb without this check would green it, i.e. lose the
 * catch. The "you" that makes it a report ("tells YOU to bin") is not a bare infinitive and
 * lands here as false, so the stand-down is unchanged for it.
 */
function binInfinitiveAfterVerb(matched: string): boolean {
  const phrase = assistantVerbPhrase(matched);
  if (!phrase) return false;
  return new RegExp(`^\\s+to\\s+${BIN_VOCAB}\\b`, "i").test(phrase.after);
}

/** The copulas: an auxiliary that can make the bin token a PASSIVE on the thing. */
const COPULA_AUX = /^(?:is|are|was|were|be|been|being)$/;

// ── THE REPORTED CLAUSE'S CONTRACTED SUBJECT (t_6ad53e35) ────────────────────
//
// The reported clause's reader read its SUBJECT as a bare pronoun or the noun
// (`it|they|he|she|the assistant`) and then required WHITESPACE before the verb phrase.
// A CONTRACTED subject is one token that carries BOTH the pronoun and the copula or
// auxiliary, so `it's` / `they're` / `he's` / `we're` / `you're` / `it's been` /
// `they've been` matched none of the alternatives: the `\b` after `it` holds (an
// apostrophe is a non-word char) but the next token is `'s`, not whitespace, so the
// required `\s+` before the verb group failed and the reader returned false. A genuine
// CLAIM whose reported clause contracts its subject was therefore read as a report — a
// false NEGATIVE. Measured on the extracted scan: a 192-sentence battery (6 information
// verbs x 8 contracted subjects x 4 bin gerunds) is 192 missed / 0 caught, while the same
// frame with the subject uncontracted is caught.
//
// THE CONTRACTION IS NOT A WHITESPACE TOKEN, so the reader is a SECOND alternation rather
// than a widened pronoun list: the subject may be the bare form, or one of these
// contractions as a single token, and only then is the whitespace-separated verb group
// read. The cores are the pronouns this reader already treats as standing for the
// assistant (`it|they|he|she`, and `the assistant` itself when bare) plus `we`, which the
// card's own sentence calls a claim ("the assistant mentions WE'RE restoring a form" — the
// assistant among the actors). `you` is deliberately NOT a core: a `you` in the reported
// clause is the person the assistant reports TO ("tells you YOU are deleting a form"), and
// the addressee strip above already consumes it — which is also why extending the
// alternation with `you` changes nothing (measured: the mutation is a no-op). The
// un-contracted `we`/`you` twins are left out for the same reason one token over: they are
// the addressee or the assistant's "we", not the assistant, and reding them is the false
// positive the contraction-only arm exists to avoid (measured: adding bare `we` reds the
// "tells you we are deleting" control).
//
// THE CONTRACTION CARRIES THE AUXILIARY TOO. `it's` is `it is` OR `it has`, so the
// copula test (which admits the `-ing` form only) cannot read the whitespace group alone
// — `it's` has no whitespace group — or `it's binning` would be read as a CLAIM and the
// report `it's binned` as a claim too. The contraction's own lexical core is folded onto
// the front of the chain the reader tests, so the last auxiliary before the bin verb is
// still the token the passive/progressive distinction reads. `'s`/`'re`/`'ve` all open
// with a copula or `have`, which is why folding them in cannot red a progressive claim.
//
// THE MODAL CONTRACTIONS `'d` (had OR would) AND `'ll` (will) ARE THE SAME CLASS ONE OVER
// (t_7cae27c8). They contract a MODAL or `had`, not the copula, so they were missed by the
// same shape of gap: `he'd been deleting` is `he had been deleting` and `it'll be binning`
// is `it will be binning` — both genuine CLAIMS the reader read as a report. Measured on
// the live tree: the contracted form is GREEN while its uncontracted twin is RED, across
// six information verbs x five pronouns x the claim shapes, while the passives and the
// addressee stay GREEN. The un-contracted twins are caught by the BARE arm, so the miss is
// the contraction token alone — the identical false negative t_6ad53e35 fixed for
// `'s`/`'re`/`'ve`, one contraction class over.
//
// `'d` IS AMBIGUOUS — had OR would — AND THE AMBIGUITY IS INERT HERE. The folded core is
// read only by the copula test, and neither reading is a copula (`had`, `would`, `will` are
// all outside `COPULA_AUX`; pinned by this file's own test), so one representative suffices:
// the reading that decides a passive (`it'd been binned` = `it had been binned`) comes from
// the `been`, never from the core. What the fold still has to get right is the chain's LAST
// token in the two shapes a modal contraction opens — `it'd BE binning` (would + progressive
// = claim) and `he'd BEEN deleted` (had + passive = report) — and the EMPTY chain (`it'd bin
// a form`: no auxiliary between the contraction and the verb), where the core alone is the
// last token and, being a modal rather than a copula, leaves it the claim.
/** The pronoun cores a contracted reported-clause subject can carry. */
const CONTRACTED_SUBJ = String.raw`it|they|he|she|we`;
/** A contracted subject as ONE token: the pronoun plus `'s` / `'re` / `'ve` / `'d` / `'ll`. */
const CONTRACTION = String.raw`(?:${CONTRACTED_SUBJ})(?:'|\u2019)(?:s|re|ve|d|ll)\b`;
/**
 * The auxiliary core a contraction folds into its verb chain. `'ll` is `will`; `'d` is
 * `had` OR `would`, and the two are indistinguishable to the only reader of this map (the
 * copula test) because neither is a copula — so `had` is the representative (see the note
 * above `CONTRACTION`).
 */
const CONTRACTION_CORE: Record<string, string> = { s: "is", re: "are", ve: "have", d: "had", ll: "will" };

/**
 * Does the clause the assistant's information verb REPORTS put the assistant back in the
 * bin verb's subject position — "the assistant tells you IT BINS a form"? True means the
 * information verb is the REPORTER of a claim rather than a report ABOUT the bin, so
 * position (A)'s match is the claim itself (the header's "THE VERB ALONE IS NOT THE
 * REPORT").
 *
 * The reported clause is read as the same canonical subject+verb shape position (A) reads
 * on the assistant itself, with the assistant's PRONOUN (or the noun again) as its
 * subject, and the bin token must be its VERB — which is what separates the claim from
 * the report/reference the same verb can introduce:
 *
 *   "tells you IT BINS a form"        -> the assistant is the subject of `bins`  -> claim
 *   "reports that THE ASSISTANT deleted a form"                      -> claim
 *   "tells you WHERE the trash is"    -> an OBJECT follows the verb  -> report, stands down
 *   "tells you WHICH form is in the trash"                           -> report, stands down
 *   "lists may already be in the trash"                              -> report, stands down
 *
 * The copula is the one aux that cannot be taken at face value: "it IS BINNED" / "it is in
 * the trash" is a PASSIVE on the thing, not an action of the assistant's, while "it IS
 * BINNING" is the progressive claim — so a copula admits the `-ing` form only.
 */
function assistantBinsInsideReport(matched: string, fullClause: string = matched, det: string = DET, contracted: boolean = true): boolean {
  const info = new RegExp(`\\b(?:${INFO_VERB})\\b`, "i").exec(fullClause);
  if (!info) return false;
  // The clause the verb reports: strip the "that" that introduces it and the "you" the
  // assistant reports TO ("tells YOU it bins" — that "you" is an indirect object, not the
  // reported clause's subject), in either order.
  let rest = fullClause.slice(info.index + info[0].length);
  for (let i = 0; i < 3; i++) {
    const stripped = rest.replace(/^\s+(?:that|you|the\s+person)\b/i, "");
    if (stripped === rest) break;
    rest = stripped;
  }
  // THE GERUND THE INFORMATION VERB REPORTS (t_7b69a77a). A bin GERUND is the reported
  // clause's OBJECT when nothing predicates of it — "reports deleting a form", where the
  // assistant IS the deleter (the claim) — and its SUBJECT when something does — "says
  // binning a form is easy", where the assistant is the reporter (a report). The role is
  // resolved from the reported clause, in `reportedGerundIsSubject` above; a gerund that
  // heads the clause with nothing predicated of it is the information verb's object, which
  // credits the assistant, so the claim holds. This is read from the FULL clause, not the
  // match: the match ends at the bin token, so it cannot see the predicate that decides.
  if (new RegExp(`^\\s+(?:the\\s+)?${BIN_GERUND}\\b`, "i").test(rest)) {
    return !reportedGerundIsSubject(rest, det);
  }
  // THE SUBJECT IS A BARE PRONOUN OR ITS CONTRACTION (t_6ad53e35). The second alternation
  // is the contracted subject as ONE token (`it's`, `they're`, `he's`, `we're`, `you're`)
  // — see the note above `CONTRACTION`. It is taken only when `contracted` is true, which is
  // what lets this test's pre-fix reader (`assistantBinClaims(s, DET, false)`) run the
  // shipped rule with exactly that arm removed. Its whitespace-verb group is the same chain
  // the bare subject takes, so a contracted perfect (`it's been binning`) reads exactly like
  // `it has been binning`, and the contraction's own lexical core (`is`/`are`/`have`) is
  // folded onto the front of the chain the copula test below reads.
  const subject = contracted
    ? `(?:(?:${CONTRACTION})|(?:it|they|he|she|the\\s+assistant)\\b)`
    : `(?:it|they|he|she|the\\s+assistant)\\b`;
  const claim = new RegExp(
    `^\\s+${subject}((?:\\s+(?:${ASSISTANT_AUX}))*)\\s+(${BIN_VOCAB})\\b`,
    "i",
  ).exec(rest);
  if (!claim) return false;
  // The chain's LAST auxiliary is the one before the bin verb. (A repeated capturing group
  // would hold only its last iteration, so the chain is captured in full and its final
  // token taken here.) A contraction carries its auxiliary INSIDE the subject token, so it
  // is folded on first — otherwise `it's binning` (the progressive claim) would have no
  // auxiliary to test and read as a bare copula-less claim, reding the report direction.
  const contractedHit = contracted ? new RegExp(`^\\s+(${CONTRACTION})\\b`, "i").exec(rest) : null;
  const core = contractedHit ? (CONTRACTION_CORE[contractedHit[1].toLowerCase().slice(-2).replace(/['\u2019]/, "")] ?? "") : "";
  const chain = `${core} ${(claim[1] ?? "").trim()}`.trim().toLowerCase();
  const aux = chain ? chain.split(/\s+/).pop()! : "";
  const bin = claim[2].toLowerCase();
  // A copula before a non-progressive form is the passive ("it is binned"), not a claim.
  return !COPULA_AUX.test(aux) || /ing$/.test(bin);
}

/**
 * One phrasing in `clause` that credits the assistant with the bin, or null.
 *
 * THE RULE, in five syntactic positions — not five phrasings. Once the clause is
 * normalised, "the assistant is the subject or the agent of a bin verb" can only be
 * one of these, and each is a position, so a new surface phrasing lands in one of them
 * without a new rule:
 *   (A) the assistant is the SUBJECT of a bin verb      — "the assistant bins"
 *   (B) the assistant is the AGENT of a passive         — "<binned> by the assistant"
 *   (C) the assistant's ROLE is the subject             — "the assistant's job is to bin"
 *   (D) the assistant is the subject of a LIGHT VERB    — "<binning> … the assistant does"
 *   (E) the bin verb's SUBJECT is the assistant, the verb having moved to a later
 *       position under a copula or a fused relative    — "the binner … is the assistant"
 *       / "who bins a form is the assistant". (E) is the mirror of (A) and (B): the
 *       predication is the same, read from the far end.
 * The possessive-genitive predicate ("<binning> is the assistant's job") is (C) read
 * from the other end and is folded into it: it is the same predication.
 */
function binClaimInClause(raw: string, det: string = DET, contracted: boolean = true): string | null {
  const s = normaliseClause(raw);
  if (!/\bassistant\b/.test(s)) return null;
  // (A) the assistant as SUBJECT of a bin verb, no negation between.
  {
    const m = new RegExp(`\\bassistant\\b\\s+(?:(?:${ASSISTANT_AUX})\\s+)?${noNeg("[^.!?]")}${BIN_VOCAB}\\b`, "i").exec(s);
    if (m && !HAS_NEGATION.test(m[1])) {
      // The assistant's OWN verb decides: under an information verb, or a subordinator
      // opening the assistant's own clause, the later bin token is what it read or what
      // a sub-clause says, not what it did (see the header's "A REPORT IS NOT AN
      // ACTION"). Action verbs and every other shape are unaffected. The verb alone is
      // not the report, though: an information verb that REPORTS a clause putting the
      // assistant back in the bin verb's subject position ("tells you IT BINS a form")
      // is reporting a claim, not naming the bin (see `assistantBinsInsideReport`), and
      // an information verb followed by a BARE BIN INFINITIVE ("has been asked TO BIN")
      // is the claim position (A)'s infinitive shape, not a report (see
      // `binInfinitiveAfterVerb`).
      const verb = assistantVerb(m[0]);
      if (
        !verb ||
        !HAS_NON_ACTION.test(verb) ||
        binInfinitiveAfterVerb(m[0]) ||
        assistantBinsInsideReport(m[0], s, det, contracted)
      )
        return m[0];
    }
  }
  // (B) the AGENT of a passive: "<bin> by the assistant" (bare or periphrastic).
  {
    const m = new RegExp(`\\b${BIN_VOCAB}\\b[^.!?]{0,60}?\\bby\\s+(?:the\\s+)?assistant\\b`, "i").exec(s);
    if (m) return m[0];
  }
  // (C) the GENITIVE SUBJECT and its predicate: "the assistant's job is to <bin>" /
  //     "<bin> is the assistant's job".
  {
    const m = new RegExp(`\\bassistant(?:'|’)?s\\b${noNeg("[^.!?]")}${BIN_VOCAB}\\b`, "i").exec(s);
    if (m && !HAS_NEGATION.test(m[1])) return m[0];
    const p = new RegExp(
      `\\b${BIN_VOCAB}\\b[^.!?]{0,60}?\\b(?:is|was|are|were|remains?)\\s+(?:not\\s+|never\\s+)?(?:the\\s+|a\\s+|your\\s+)?assistant(?:'|’)?s\\b`,
      "i",
    ).exec(s);
    if (p) {
      const before = p[0].slice(0, p[0].search(/assistant(?:'|’)?s/i));
      if (!HAS_NEGATION.test(before)) return p[0];
    }
  }
  // (E) the bin verb's SUBJECT is the assistant, the verb having moved to a later
  //     position. The bin word before it and a finite "is the assistant" land on it:
  //     "<binning> … IS/falls to the assistant" and "who|that <bins> … is the
  //     assistant" (a fused relative — "who bins a form IS the assistant"). A
  //     negation here is the assistant DISOWNING the bin ("deleting a form is NOT the
  //     assistant's job"), so it must fall through to (C) and stay GREEN — hence no
  //     optional negative in this pattern.
  {
    const m = new RegExp(
      `\\b${BIN_VOCAB}\\b\\b[^.!?]{0,40}?\\b(?:is|was|falls?\\s+to|belongs?\\s+to)\\s+(?:the\\s+|to\\s+the\\s+)?assistant\\b`,
      "i",
    ).exec(s);
    if (m) return m[0];
  }
  // (D) the assistant as the subject of a LIGHT VERB, the bin in an earlier position:
  //     "<binning> … is something the assistant does".
  {
    const m = new RegExp(`\\b${BIN_VOCAB}\\b[^.!?]{0,80}?\\bassistant\\b\\s+(?:does|do|did|performs?|handles?)\\b`, "i").exec(s);
    if (m) return m[0];
  }
  return null;
}

// A REPORT IS NOT AN ACTION. The bin token is often a NOUN ("the trash"), so a clause
// that merely NAMES the trash in what the assistant reads or reports — "the assistant
// can tell you where the trash is", "a form the assistant lists may already be in the
// trash" — puts "assistant … trash" inside position (A)'s window while crediting the
// assistant with nothing at all. When the assistant's OWN verb is an information verb
// (tell/show/list/read/see/ask/say/report/know/find/…), the bin noun later in its
// clause is the assistant's object of KNOWLEDGE rather than an action of its, and the
// match is disqualified. It is the assistant's own verb that is read, not the window:
// a claim whose verb is an ACTION verb ("the assistant can take a form as far as the
// trash") is untouched, and the same sentence with the bin as the assistant's AGENT
// ("… binned by the assistant") is still RED, because (B) does not depend on the verb
// that introduces it. Measured: both sentences above were RED under the head scan
// (the second newly, from the structural rewrite); the shipped page carries neither.
//
// ...BUT THE VERB ALONE IS NOT THE REPORT (t_9717fec1). "The assistant tells you where
// the trash is" and "the assistant tells you IT BINS a form" carry the same information
// verb and only the second claims the bin, so standing (A) down on the verb alone
// suppressed seven genuine claims the head scan caught (the header's "THE VERB ALONE IS
// NOT THE REPORT"). The reported clause is therefore read too, by
// `assistantBinsInsideReport`: a bin token the assistant is merely the REPORTER of — an
// object of knowledge, or a clause ABOUT the bin — still stands (A) down, while a
// reported clause that puts the assistant back in the bin verb's subject position is the
// claim the sentence carries and (A) holds.
//
// THE PRO-VERB ACROSS A CLAUSE BOUNDARY. A clause boundary splits the bin from the
// word that stands for it: "Binning a form — the assistant does that." The second
// clause alone has no bin for the rule to read, so an em-dash (or a semicolon, or a
// conjunction) costs the shape entirely. This fold is that boundary's normalisation:
// when a clause's only verb is a PRO-VERB ("does/do/did/performs/handles") followed by
// the anaphor ("that"/"it") that points back at the previous clause, the pro-verb is
// rewritten to that clause's bin word, and the rule then reads it in one clause like
// any other. Only the anaphoric pro-verb is folded — "the assistant does that" with no
// antecedent is not a claim about the bin, and a fresh "the assistant does <something>"
// is untouched — and only onto the bin of the clause IMMEDIATELY before it, which is
// the antecedent the anaphor can actually have. This is the parity the enumerated scan
// got for free: its window `[^.!?]{0,80}` crossed the boundary because an em-dash is
// not a sentence terminator, and the fold restores exactly that without reopening the
// whole-sentence window the structural arm was built to close.
const PRO_VERB = String.raw`(?:does|do|did|performs?|handles?)`;
const ANAPHOR = String.raw`(?:that|it)`;
/**
 * The bin word of `clause`, or null — the antecedent the fold above carries across the
 * boundary.
 */
function binWordInClause(clause: string): string | null {
  const m = new RegExp(`\\b${BIN_VOCAB}\\b`, "i").exec(clause);
  return m ? m[0] : null;
}

/** Every phrasing in `text` that credits the assistant with the bin, in order. */
function assistantBinClaims(text: string, det: string = DET, contracted: boolean = true): string[] {
  const out: string[] = [];
  let prevBin: string | null = null;
  for (const clause of text.split(CLAUSE_SPLIT)) {
    if (!clause.trim()) continue;
    const folded = prevBin
      ? clause.replace(
          new RegExp(
            `\\bassistant\\b(\\s+(?:${ASSISTANT_AUX}))?\\s+\\b${PRO_VERB}\\b\\s+${ANAPHOR}\\b`,
            "i",
          ),
          (_m, aux: string | undefined) => `assistant${aux || " "}${prevBin}`,
        )
      : clause;
    const m = binClaimInClause(folded, det, contracted);
    if (m) out.push(m);
    // The antecedent for the NEXT clause: a clause with no bin resets it rather than
    // letting a pro-verb reach back over an unrelated clause.
    prevBin = binWordInClause(clause);
  }
  return out;
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(page.includes("chat-input"), "app/chat/page.tsx is not the chat page source");
  assert.ok(
    goChat.includes("func ReadOnlyScopes") || goChat.includes("ScopeReadonly"),
    "could not find the read-only scope in api/internal/chat/chat.go",
  );
  assert.ok(
    existsSync(path(DOCS_PAGE)),
    `the docs page the inline "?" points at (${DOCS_PAGE}) does not exist`,
  );
});

test("the server really runs the chat's tools read-only (the UI's claim is not just copy)", () => {
  // The scope the tool calls are dispatched under.
  assert.match(
    goRest,
    /Scopes:\s*chat\.ReadOnlyScopes\(\)/,
    "rest_chat.go no longer builds the identity with chat.ReadOnlyScopes() — the page still " +
      "tells the user the assistant cannot change their account; that claim must stay true",
  );
  assert.match(
    goChat,
    /func ReadOnlyScopes\(\)\s*\[\]string\s*\{\s*return\s*\[\]string\{dbstore\.ScopeReadonly\}/,
    "chat.ReadOnlyScopes no longer returns exactly the readonly scope",
  );
  // Session-only: an API key must not reach the chat.
  assert.ok(
    goRest.includes("sessionOwnerID"),
    "rest_chat.go no longer resolves the caller from the session — a chat belongs to a person",
  );
  // The in-process tools and nothing else.
  assert.match(
    goChatTools,
    /chatReadTools\s*=\s*\[\]string\{/,
    "the chat's read allowlist moved — this guard reads it to prove it contains no writer",
  );
  assert.ok(
    goChatTools.includes("ChatSubmitFeedbackTool") && goChatTools.includes("submit_feedback"),
    "chat_tools.go no longer declares the in-process filing tool (submit_feedback)",
  );
});

test("phase 2: a write is only reachable through a human's confirmation", () => {
  // The confirmed tool runs under a DIFFERENT scope from a message. Both facts are
  // read from the same file so the pair cannot drift: a message read-only, a
  // confirmation the human's write.
  assert.match(
    goRest,
    /Scopes:\s*chat\.WriteScopes\(\)/,
    "rest_chat.go no longer builds a write identity for the confirm path — either the feature " +
      "cannot work at all, or the write scope has been moved onto the message path",
  );
  assert.match(
    goChatProposal,
    /func WriteScopes\(\)\s*\[\]string\s*\{\s*return\s*\[\]string\{dbstore\.ScopeReadWrite\}/,
    "chat.WriteScopes no longer returns exactly the readwrite scope",
  );
  // The confirmation requires a token the MODEL never sees. This is the whole
  // design: a `confirm` boolean the model wrote is not a confirmation, so the
  // confirm input must not carry one.
  assert.match(
    goRest,
    /type chatConfirmRequestIn struct\s*\{[^}]*Token\s+string\s+`json:"token"`[\s\S]{0,200}?ArgsHash\s+string\s+`json:"args_hash"`/,
    "the confirm request no longer carries exactly {token, args_hash} — the token IS the " +
      "authorisation, and the hash is what binds the execution to the arguments the user read",
  );
  assert.doesNotMatch(
    goRest,
    /confirm\s*(?:bool|string)[^`]*`json:"confirm"/i,
    "the confirm request accepts a `confirm` field again — a boolean on the wire proves nothing " +
      "about who sent it, which is exactly the MCP shape this lane must not copy",
  );
  // The route is session-only and its own route, so the message path cannot reach it.
  assert.ok(
    goRestRoutes.includes('"POST /v1/rest/chat/confirm"'),
    "POST /v1/rest/chat/confirm is not registered — the card's Confirm button would 404",
  );
  assert.ok(
    goRestRoutes.includes('"POST /v1/rest/chat/cancel"'),
    "POST /v1/rest/chat/cancel is not registered — a Cancel that only hides the card would " +
      "leave a live authorisation in the browser",
  );
  // The confirmation has its OWN per-user bucket: a confirmation costs no model
  // call, so bounding it is about decision pace, not spend.
  assert.ok(
    goRest.includes("ChatConfirmLimiter") && goRest.includes("chatConfirmPerHour"),
    "the confirmation path has no per-user limit — a script could be walked through cards as " +
      "fast as it can post",
  );
});

test("phase 2: the proposable set excludes the destructive classes, and says so", () => {
  // The set itself: reversible field edits only. If this grows, the UI's promise
  // that the assistant can only prepare a small reversible change has to be
  // re-argued — which is what the assertion is for.
  assert.match(
    goChatTools,
    /var chatProposableTools\s*=\s*\[\]string\{[\s\S]*?update_product[\s\S]*?\}/,
    "the proposable set moved or no longer includes update_product",
  );
  for (const forbidden of ["delete_form", "refund_sale", "archive_product", "cancel_subscription"]) {
    assert.ok(
      !new RegExp(`chatProposableTools[\\s\\S]{0,900}?"${forbidden}"`).test(goChatTools),
      `${forbidden} is in the proposable set — delete/refund/archive/cancel are phase-3 decisions ` +
        "at the earliest, and the card is explicit that Wan must say so",
    );
  }
  // And the exclusions are WRITTEN DOWN, not merely absent.
  assert.ok(
    /WHAT IS EXCLUDED/i.test(goChatTools),
    "chat_tools.go no longer documents which classes are excluded — an unwritten exclusion is " +
      "an invitation to add one back",
  );
});

test("the page renders the proposal as a decision, not as done work", () => {
  // The card carries the token and the hash; nothing else may authorise.
  assert.match(
    page,
    /args_hash/,
    "the page no longer sends args_hash — without it the server has nothing binding the " +
      "execution to the arguments the user read",
  );
  // One decision call site, parameterised by action, hitting the two routes.
  assert.match(
    page,
    /"\/api\/rest\/chat\/confirm"\s*:\s*"\/api\/rest\/chat\/cancel"/,
    "the page no longer posts a decision to the literal /api/rest/chat/{confirm,cancel} routes — " +
      "a computed path is one the route-parity guard cannot resolve, so a typo would be a 404 " +
      "at the moment the user has committed to a decision",
  );
  assert.match(
    page,
    /"confirm"\s*\|\s*"cancel"/,
    "the decision call no longer distinguishes confirm from cancel",
  );
  // Both actions exist as buttons; a card with only Confirm is not a decision.
  assert.match(page, /chat\.confirm/, "the card has no Confirm button");
  assert.match(page, /chat\.cancel/, "the card has no Cancel button");
  // The badge that says nothing has happened yet.
  assert.match(
    page,
    /chat\.proposalBadge/,
    "the card carries no 'needs your confirmation' badge — a user who believes the change was " +
      "already made will not read the card",
  );
  // A spent or expired card must not keep offering the buttons: the server would
  // answer 409 to a click the user cannot act on.
  assert.ok(
    page.includes('"stale"') || page.includes("proposalStale"),
    "the page does not handle a stale (spent/expired) proposal — the user would get a bare error",
  );
  assert.match(
    page,
    /state\s*===\s*"pending"/,
    "the buttons are no longer gated on the card's state — a settled card would be re-clickable",
  );
  // A settled card must stop claiming it still needs the user. Verified in a real
  // browser against a production build: with the badge hard-coded, a CONFIRMED
  // card still read "NEEDS YOUR CONFIRMATION" and looked like an unfinished task.
  //
  // Matched as the literal CALL, not as a substring: a mutation run showed that
  // `page.includes("chat.proposalBadgeDone")` also passes for a mangled key like
  // `chat.proposalBadgeDoneMUTANT`, i.e. it would not have caught the key being
  // broken — only removed. The call form is the thing that has to exist.
  for (const key of [
    "chat.proposalBadgeDone",
    "chat.proposalBadgeCancelled",
    "chat.proposalBadgeStale",
    "chat.proposalBadgeFailed",
  ]) {
    assert.match(
      page,
      new RegExp(`t\\("${key}"\\)`),
      `the card's badge is not state-dependent (${key} is not used in a t() call) — a settled card ` +
        `would keep saying it needs your confirmation and invite a second click the server refuses`,
    );
  }
});

test("the chat routes are registered, GET for availability and POST for a message", () => {
  assert.ok(
    goRestRoutes.includes('"GET /v1/rest/chat"'),
    "GET /v1/rest/chat is not registered — the page could not ask whether the assistant is available",
  );
  assert.ok(
    goRestRoutes.includes('"POST /v1/rest/chat"'),
    "POST /v1/rest/chat is not registered — the page would post into a 404",
  );
});

test("the per-user limit is real, so the docs' 60/hour is not a decorative number", () => {
  assert.match(
    goRest,
    /chatRatePerHour\s*=\s*60/,
    "the chat rate limit changed — the UI and docs quote it, so update them together",
  );
  assert.ok(
    goRest.includes("ChatLimiter"),
    "rest_chat.go no longer consults a limiter: every message is at least one LLM call, and an " +
      "unlimited chat is an unbounded bill",
  );
});

/**
 * The number a Go `const` is declared with, read out of its source.
 *
 * NOT a copy of the value: a web test carrying "30" would be a THIRD copy of the
 * number and a fourth thing to edit — the defect this guard exists for, wearing
 * a guard's clothes. A name that is missing, or is no longer a simple numeric
 * literal, FAILS rather than handing the comparisons `undefined` (which is a
 * substring of nothing, so every check below would pass).
 */
function goConstNumber(src: string, name: string): number {
  const m = src.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+(?:\\.\\d+)?)\\b`));
  assert.ok(
    m,
    `${name} is no longer declared as a numeric literal in the chat's Go source — retarget this ` +
      `guard at whatever replaced it. The docs and the 429 bodies quote this number, so it has to ` +
      `stay readable from here`,
  );
  return Number(m[1]);
}

/**
 * A Go `const X = N * time.Minute` declaration, as whole minutes.
 *
 * Minutes rather than seconds on purpose: the sentence this is compared against
 * is written in minutes ("it expires 5 minutes after it appears"), so converting
 * the duration here (and not in the sentence) keeps both sides readable.
 */
function goDurationMinutes(src: string, name: string): number {
  const m = src.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+)\\s*\\*\\s*time\\.Minute\\b`));
  assert.ok(
    m,
    `${name} is no longer declared as "<n> * time.Minute" — this guard compares it to a sentence ` +
      `written in minutes, so retarget the guard rather than letting it compare nothing`,
  );
  return Number(m[1]);
}

/**
 * `needle` as a regex that refuses a LONGER number in its place (t_4912a9bd).
 *
 * A bare `String.includes` is a SUBSTRING read, and every number this guard
 * quotes sits where a longer one still contains it: with the constant at 60, the
 * 429 string "160 messages an hour" satisfies `includes("60")`, and a docs
 * sentence's trailing "in bursts of 10" is inside "in bursts of 106". Measured
 * on the merged tree: the `en` refusal reworded to "160 messages an hour" left
 * this guard GREEN while the constant was still 60 — a refusal telling the user
 * a limit the server does not enforce, which is the drift this guard exists for.
 * So the digits have to END somewhere: no digit may sit immediately before or
 * after the match. A following "." or "," is ordinary prose ("in bursts of 10.")
 * and still matches.
 *
 * Only DIGITS are boundaries. A thousands separator (`1,060`) is not anchored,
 * and that is deliberate: the copy these sentences quote is prose, where a
 * quoted sentence is legitimately followed by a comma ("in bursts of 5, and …"),
 * so anchoring separators would trade this measured hole for a false red on the
 * next reword. No sentence or locale string here carries a grouped number.
 *
 * The needle is escaped and interpolated as a LITERAL: these are sentences, not
 * patterns.
 */
function quotesExactly(needle: string): RegExp {
  const esc = needle.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?<![0-9])${esc}(?![0-9])`);
}

/** The scope branch the availability probe must answer a 400 with (t_997bb646). */
const PROBE_400_SCOPE_BRANCH =
  /if \(r\.status === 400\) \{\s*setErr\(t\("chat\.scopeRequired"\)\);\s*return;/;

/**
 * The availability probe's whole `!r.ok` branch, AS CODE — comments blanked — or
 * `null` when the page no longer delimits it.
 *
 * Located by its own site, the `runLoad` outcome's `!r.ok` guard — not by a line
 * number — and the slice runs to the statement after that block
 * (`setAvailable(r.data.available …`), so it covers the WHOLE branch including
 * the generic fall-through the scope branch must not replace. It returns `null`
 * rather than guessing when any of those anchors is gone.
 *
 * CODE, NOT RAW SOURCE (t_4912a9bd). The branch is matched with a pattern, and a
 * raw read is satisfied by a `//` line carrying the same text: measured on the
 * merged tree, deleting the REAL branch and leaving
 * `// if (r.status === 400) { setErr(t("chat.scopeRequired")); return; }` in its
 * place kept this guard GREEN (21 pass / 0 fail) while the page rendered "The
 * assistant is not available right now." with the composer disabled. The
 * deletion CONTROL (nothing left behind) was the only mutation that redded it,
 * so that green was not a dead instrument. `codeOnly` is this repo's shared,
 * JSX-text-aware blanker and it preserves every byte offset, so this is the SAME
 * region with the non-code text removed.
 *
 * A shared function rather than an inline slice so the bite proof runs the
 * IDENTICAL reader against a mutated page — the file's doctrine (see the note
 * above `BIN_VOCAB`): a proof that exercised a paraphrase would prove nothing
 * about the scan.
 */
function probeBranchCode(src: string): string | null {
  const code = codeOnly(src);
  const at = code.indexOf("const r = await runLoad(");
  if (at === -1) return null;
  const from = code.indexOf("if (!r.ok) {", at);
  if (from === -1) return null;
  const end = code.indexOf("setAvailable(r.data.available", from);
  if (end <= from) return null;
  return code.slice(from, end);
}

test("the phase-2 caps the docs quote are read from the constants, so prose and enforcement move together", () => {
  // WHY THIS EXISTS, AND WHY IT IS NOT PR 695's TEST AGAIN.
  //
  // PR 695 made the WIRE follow the constant: chatLimits() references chat.*
  // instead of restating the number, and TestChatLimitsSourcesEveryCapFromItsConstant
  // refuses a numeric literal in that struct. It did not touch the PROSE, and
  // nothing else watches the prose — measured on this tree before this guard
  // existed: chatConfirmPerHour 30 -> 45 left `go test ./internal/httpapi/
  // -run TestChatLimits` GREEN (correct: the wire follows the constant) and the
  // whole web suite GREEN, while app/docs/chat/page.tsx still told the user "at
  // 30 per hour". That is the same drift PR 695 removed one layer out, so the
  // guard reads the constants and demands the prose quote THOSE values.
  //
  // The phase-1 pair is in here too: the 60/hour test above pins the constant,
  // but it never made the docs page, the dictionary, or the 429 body follow it.
  //
  // THE DOCS HALF READS RENDERED TEXT, NOT RAW SOURCE (t_4912a9bd). `read`
  // returns the page's BYTES, and a sentence can live in those bytes without
  // reaching a reader: `// One answer can put at most 3 proposals in front of
  // you` is the sentence this guard looks for, in text no user ever sees.
  // Measured on the merged tree: the guarded sentence moved into a comment near
  // the imports, with the RENDERED prose given a wrong number ("at most NINE
  // proposals"), left this guard GREEN (0 fail) — the deletion control (the
  // sentence removed from the source entirely) was the only mutation that redded
  // it, so the green was not a dead instrument. `codeOnly` is this repo's shared,
  // JSX-text-aware blanker: a comment cannot satisfy a sentence, while a string
  // literal — where this page's copy actually lives — is left untouched.
  const docs = codeOnly(read(DOCS_PAGE));
  const msgPerHour = goConstNumber(goRest, "chatRatePerHour");
  const burst = goConstNumber(goRest, "chatBurst");
  const confirmPerHour = goConstNumber(goRest, "chatConfirmPerHour");
  const confirmBurst = goConstNumber(goRest, "chatConfirmBurst");
  const maxProposals = goConstNumber(goChatProposal, "MaxProposalsPerRun");
  const ttlMinutes = goDurationMinutes(goChatProposal, "ProposalTTL");

  // Vacuity, in the neighbours' idiom: prove this is reading the real numbers
  // and the real docs page before any of the comparisons below are believed.
  for (const [name, n] of Object.entries({
    chatRatePerHour: msgPerHour,
    chatBurst: burst,
    chatConfirmPerHour: confirmPerHour,
    chatConfirmBurst: confirmBurst,
    MaxProposalsPerRun: maxProposals,
    ProposalTTLMinutes: ttlMinutes,
  })) {
    assert.ok(
      Number.isFinite(n) && n > 0,
      `${name} read as ${n} — this guard is not reading the constant it thinks it is`,
    );
  }
  assert.ok(
    docs.includes("Confirming a proposed change") && docs.includes("Limits and cost"),
    `${DOCS_PAGE} is not the chat docs page this guard means to judge`,
  );

  // ── 1. the docs page ─────────────────────────────────────────────────────
  // Each entry is the sentence WITH the number built from the constant, so a
  // constant change and a docs change have to land together. The phrasing is
  // pinned as tightly as the 60/hour test pins the Go line; a copyedit that
  // rewrites the sentence has to update this list, which is the point.
  //
  // The match is DIGIT-BOUNDED (`quotesExactly`), not a substring read: with the
  // constant at 60, a page reading "160 messages per hour, in bursts of 10" used
  // to satisfy the wanted "60 messages per hour, in bursts of 10" — measured
  // GREEN — so the guard could report the page quotes the enforced number while
  // it told the reader a different one (t_4912a9bd).
  const sentences: Array<{ want: string; what: string }> = [
    {
      want: `${msgPerHour} messages per hour, in bursts of ${burst}`,
      what: "the per-user message cap and its burst",
    },
    {
      want: `at ${confirmPerHour} per hour in bursts of ${confirmBurst}`,
      what: "the confirmation cap and its burst",
    },
    {
      want: `at most ${maxProposals} proposals`,
      what: "the proposals-per-answer cap",
    },
    {
      want: `expires ${ttlMinutes} minutes after it appears`,
      what: "the proposal expiry",
    },
  ];
  for (const { want, what } of sentences) {
    assert.ok(
      quotesExactly(want).test(docs),
      `${DOCS_PAGE} no longer says "${want}" (${what}) — the docs quote the cap the server ` +
        `enforces, so the constant and this sentence cannot move apart`,
    );
  }

  // ── 2. the dictionary the page shows on a 429 ────────────────────────────
  // Digit presence, not the whole phrasing: a translation is free to be reworded
  // (and is, in four locales), but the NUMBER a user is told on a refusal has to
  // be the enforced one — and it has to END there: `includes("60")` is satisfied
  // by "160 messages an hour", which is how this half could pass while the
  // refusal quoted a limit the server does not enforce (t_4912a9bd, the same
  // class the docs half carried). The live rate-limit note (chat.limitNote) needs
  // no pin here — the page fills its {n} from the wire's limits block, which PR
  // 695's Go guard already ties to the constant.
  const dictCaps: Array<{ key: string; n: number; what: string }> = [
    { key: "chat.rateLimited", n: msgPerHour, what: "message cap" },
    { key: "chat.confirmRateLimited", n: confirmPerHour, what: "confirmation cap" },
  ];
  for (const { key, n, what } of dictCaps) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(entry, `${key} is gone from the dictionary — the page shows this string on a 429`);
    for (const lang of LANGS) {
      assert.ok(
        quotesExactly(String(n)).test(String(entry[lang])),
        `${key}.${lang} does not quote the enforced ${what} (${n}): "${entry[lang]}" — the UI ` +
          `tells the user a number on a 429, so the constant and this string move together`,
      );
    }
  }

  // ── 3. the 429 bodies ────────────────────────────────────────────────────
  // The third prose copy, and the one that used to be a literal: phase 1's
  // refusal said "(60 mesej sejam)" as text, so it would have kept telling a user
  // 60 after the constant moved. Every sentence that quotes a bucket has to be
  // BUILT from the constant, which is what the two assertions below measure.
  const chatLines = goRest.split("\n").filter((l) => l.includes("mesej sejam"));
  assert.equal(
    chatLines.length,
    1,
    "rest_chat.go no longer answers a rate-limited MESSAGE with one 'mesej sejam' sentence — " +
      "retarget this guard at whatever replaced it",
  );
  assert.ok(
    chatLines[0].includes("chatRatePerHour"),
    `the message 429 body quotes its own number instead of the enforced one: ` +
      `${chatLines[0].trim()} — a constant change would leave the refusal stating the old limit`,
  );
  assert.doesNotMatch(
    chatLines[0],
    /\(\s*\d+\s*mesej sejam/,
    `the message 429 body restates the number as text (${chatLines[0].trim()}) — build it from ` +
      `chatRatePerHour instead`,
  );

  const confirmLines = goRest.split("\n").filter((l) => l.includes("had pengesahan dicapai"));
  assert.equal(
    confirmLines.length,
    2,
    "the confirmation bucket is enforced in two handlers (confirm and cancel) and their 429 " +
      "bodies must both be here — a guard that reads one is blind to the other",
  );
  for (const l of confirmLines) {
    assert.ok(
      l.includes("chatConfirmPerHour"),
      `a confirmation 429 body quotes its own number instead of the enforced one: ${l.trim()}`,
    );
  }
  assert.doesNotMatch(
    goRest,
    /\(\s*\d+\s*sejam/,
    "a 429 body restates the confirmation number as text — build it from chatConfirmPerHour " +
      "(fmt.Sprintf) instead",
  );
});

// ── THE PHASE-1 RUNNER CAPS THE SAME PAGE QUOTES (t_306d1389) ─────────────────
//
// WHAT THIS CLOSES. The test above pins the PHASE-2 caps' prose. More caps live in
// api/internal/chat/chat.go and FOUR of them are restated as NUMBERS in the very
// same "Limits and cost" section of the very same page — sentences no test read.
// Measured on this tree before this guard existed (the card's own table, and the
// bite proof below re-derives it): moving MaxTurns 16 -> 33, MaxMessageChars
// 2000 -> 7777, MaxReplyChars 4000 -> 9999 or MaxSteps 4 -> 9 left the whole web
// suite GREEN while the rendered page kept advertising the old number — with a
// CONTROL on the same harness (chatRatePerHour 60 -> 90, a cap the test above DOES
// read) RED. So the harness worked; these four were simply not in it.
//
// ONE CLAIM IN THE CARD IS NOT SO, measured here rather than repeated: the card
// says six caps "are restated on the same page". Four are. The other two are READ
// here — a rename or a removal of either reds, because `goConstNumber` throws
// rather than handing the comparisons `undefined` — but neither has a number on
// the page to compare against, and they part company on purpose:
//
//   * MaxToolResultChars (6000) WAS the second numberless claim ("Each lookup is
//     bounded in size"), so no constant move could make it false. Card t_902ab8bb
//     closed that: the section now STATES the figure — "Each lookup's result is
//     trimmed to 6000 characters before the assistant reads it" — because that is
//     the number a user can act on (it is why a long list comes back summarised
//     rather than wholesale, and why an answer can be partial). It is pinned in
//     `stated` below with the other four, and moving it REDS.
//
//   * MaxTurnChars (4000) is therefore the ONLY cap here the section states no
//     number for, and that is a DECISION, not an unfiled gap. It bounds ONE
//     CLIENT-SUPPLIED TURN (buildTurns, chat.go:770). The new message is bounded by
//     MaxMessageChars (chat.go:772) and the assistant's own answer by MaxReplyChars,
//     so every turn an honest client sends is already under 4000: a sentence stating
//     it would tell a user nothing they can act on, and would invite reading "4000"
//     as the length of their own message — which is 2000. The figure bites only a
//     hand-built client replaying a huge transcript, which is the reader the docs
//     page is not written for. So the CLAIM this cap backs is pinned instead — "not
//     for holding a long document": a pasted document IS one turn, and the per-turn
//     bound is what makes that sentence true. Note also that the 4000 of "The
//     assistant's own answer is capped at 4000 characters" is MaxReplyChars and NOT
//     this constant: pinning that sentence to both would red a page that is still
//     TRUE the moment MaxTurnChars moves, which is the false RED every guard in this
//     file is least entitled to. Moving MaxTurnChars 4000 -> 9999 therefore leaves
//     this guard GREEN with the page CORRECT, and that verdict is asserted below
//     rather than left to look like a gap.
//
// THE COUNT IN "up to five" IS MaxSteps + 1, NOT MaxSteps. The loop is
// `for step := 0; step < MaxSteps` (chat.go:408) and its LAST round adds the final
// `ChatWithTools(ctx, turns, nil)` answer call (chat.go:463), so the section counts
// MODEL CALLS where MaxSteps caps tool ROUND-TRIPS. The +1 is arithmetic on the
// constant, not a second copy of the number, and the section writes that count as a
// WORD — so both forms are accepted, because a copyedit that writes "up to 5" is
// not a defect.
//
// WHY codeOnly AND NOT THE RAW SOURCE. The sentences below are matched against the
// page with its COMMENTS blanked (lib/jsxSource.ts, the repo's shared blanker): a
// comment quoting a number is not what the user reads, and it must not stand in for
// the sentence. The test above reads the page through `codeOnly` too (that is card
// t_4912a9bd itself), so a comment cannot satisfy its pins either, and this guard
// neither changes that nor depends on it. The page's prose lives in string
// literals, which the blanker keeps byte for byte, so every pin below still reads
// what the page renders.
//
// THE CONST IS READ THROUGH codeOnly ITSELF (t_629da97d), not only inside
// capProseProblems: the two claims this module makes about the page OUTSIDE that
// function — the vacuity check in the test below, and the `turnPin` the comment
// control is derived from — are positive prose claims too, and a `//` line could
// have satisfied them. capProseProblems then blanks an already-blanked source, which
// is a no-op: every byte offset, and every newline, is preserved.
const docsPage = codeOnly(read(DOCS_PAGE));

/** The word a small count is written as in prose, for the one sentence that spells its number. */
const COUNT_WORDS = [
  "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
  "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen",
  "nineteen", "twenty",
];

/** `n` as digits, and as a word when the list above covers it. */
function countForms(n: number): string[] {
  const word = COUNT_WORDS[n];
  return word ? [String(n), word] : [String(n)];
}

/** Every cap the section's prose stands on, by the name it is declared with. */
const CAP_NAMES = [
  "MaxTurns",
  "MaxTurnChars",
  "MaxMessageChars",
  "MaxSteps",
  "MaxToolResultChars",
  "MaxReplyChars",
] as const;

/**
 * The claims the section still makes WITHOUT a number, and the cap each one stands
 * behind — at MODULE SCOPE so the bite proof below mutates the very strings this
 * guard pins. A proof keyed on its own copy of the sentence rots into a no-op the
 * day the sentence is legitimately reworded and this list is updated with it, and
 * a no-op row reds a tree that is correct.
 *
 * ONE entry, since t_902ab8bb: MaxToolResultChars used to sit here too and now has a
 * number on the page, so it is pinned in `stated` like the other four. What is left
 * is the cap whose figure has no honest sentence to carry it (see the header).
 */
const NUMBERLESS_CLAIMS: Array<{ want: string; what: string; cap: string; why: string }> = [
  {
    want: "not for holding a long document",
    what: "the bound on one turn's text",
    cap: "MaxTurnChars",
    why:
      `a pasted document is ONE turn and the runner truncates each one (buildTurns, chat.go:770), ` +
      `so this is the sentence the per-turn cap stands behind`,
  },
];

/**
 * Everything wrong with the runner caps' prose in `docsSrc`, judged against the
 * constants in `chatSrc`, as sentences.
 *
 * Pure over its two sources so the bite proof below can move ONE constant (or
 * rewrite ONE sentence) and ask the SAME rule for its problems — the idiom of
 * chatHistoryCap.test.ts's historyCapProblems. Inline assertions would leave that
 * proof re-stating the checks, which proves nothing about them.
 */
function capProseProblems(docsSrc: string, chatSrc: string): string[] {
  const out: string[] = [];
  const docs = codeOnly(docsSrc);
  // Every constant the section's prose stands on. A name that is gone, or is no
  // longer a numeric literal, THROWS here (goConstNumber) rather than handing the
  // comparisons `undefined` — which is a substring of nothing, so every check below
  // would pass vacuously.
  const caps: Record<string, number> = {};
  for (const name of CAP_NAMES) caps[name] = goConstNumber(chatSrc, name);
  for (const [name, n] of Object.entries(caps)) {
    if (!Number.isFinite(n) || n <= 0) {
      out.push(
        `${name} read as ${n} — the section states these as caps a user can rely on, so a cap of ` +
          `zero or less backs no sentence at all`,
      );
    }
  }

  // ── the five numbers the section states ─────────────────────────────────────
  // Each entry is the sentence WITH the number built from the constant, so a
  // constant move and a page edit have to land together (the idiom of the phase-2
  // `sentences` list above). The phrasing is pinned tightly on purpose: a rewrite
  // of one of these sentences has to update this list.
  const stated: Array<{ what: string; want: string[] }> = [
    {
      what: `the turn cap (MaxTurns = ${caps.MaxTurns})`,
      want: [`bounded to the last ${caps.MaxTurns} turns`],
    },
    {
      what: `the message cap (MaxMessageChars = ${caps.MaxMessageChars})`,
      want: [`and ${caps.MaxMessageChars} characters for your message`],
    },
    {
      what: `the answer cap (MaxReplyChars = ${caps.MaxReplyChars})`,
      want: [`capped at ${caps.MaxReplyChars} characters`],
    },
    {
      what: `the model-call ceiling (MaxSteps = ${caps.MaxSteps}, so ${caps.MaxSteps} + 1 = ` +
        `${caps.MaxSteps + 1} calls)`,
      want: countForms(caps.MaxSteps + 1).map((n) => `up to ${n}`),
    },
    {
      // t_902ab8bb: this one was a NUMBERLESS claim until the section was given the
      // figure. It is stated as a per-lookup bound because that is the number a user
      // can act on — it is why a long list comes back summarised.
      what: `the per-lookup cap (MaxToolResultChars = ${caps.MaxToolResultChars})`,
      want: [
        `trimmed to ${caps.MaxToolResultChars} characters before the assistant reads it`,
      ],
    },
  ];
  for (const { what, want } of stated) {
    if (!want.some((w) => docs.includes(w))) {
      out.push(
        `${DOCS_PAGE} no longer quotes ${what} — none of ${JSON.stringify(want)} is in the page's ` +
          `text. The section states the cap the server enforces, so the constant and this sentence ` +
          `cannot move apart`,
      );
    }
  }

  // ── the two claims the section makes WITHOUT a number ───────────────────────
  // Nothing to compare (see the header), so what is pinned is the claim itself —
  // the sentence each of those two caps stands behind. A copyedit that drops one
  // leaves the page silent about a bound the server still applies.
  for (const { want, what, cap, why } of NUMBERLESS_CLAIMS) {
    if (!docs.includes(want)) {
      out.push(`${DOCS_PAGE} no longer says "${want}" (${what}, ${cap} = ${caps[cap]}) — ${why}`);
    }
  }
  return out;
}

test("the runner caps the chat docs quote are read from the constants, so the limits' prose cannot advertise a cap the server stopped applying", () => {
  // Vacuity, in the neighbours' idiom: prove this is the real page (a section title
  // and a sentence only it carries) and the real Go source BEFORE any comparison
  // below is believed.
  assert.ok(
    docsPage.includes("Limits and cost") && docsPage.includes("at least one AI model call"),
    `${DOCS_PAGE} is not the chat docs page this guard means to judge`,
  );
  assert.ok(
    goChat.includes("func (r *Runner) Run") && goChat.includes("MaxToolResultChars"),
    "api/internal/chat/chat.go is not the runner source this guard reads its caps from",
  );
  const problems = capProseProblems(docsPage, goChat);
  assert.deepEqual(
    problems,
    [],
    `the chat docs' "Limits and cost" prose and the runner's caps disagree:\n  - ${problems.join("\n  - ")}`,
  );
});

// ── THE BITE ──────────────────────────────────────────────────────────────────
//
// Each row is a mutation of a CONSTANT with the page left alone — exactly the state
// that measured GREEN before this guard existed — or a page-side rewrite that stops
// quoting the number. The two controls the header's honesty depends on are measured
// in the second half: a sentence quoted only in a COMMENT must not satisfy a pin,
// and the one cap the section genuinely states no number for must stay GREEN when it
// moves, because pinning it to a sentence would red a page that is still true.
test("the caps-prose guard bites: a moved constant or a reworded sentence is RED, and the one genuinely numberless cap stays GREEN", () => {
  const problems = capProseProblems(docsPage, goChat);
  assert.deepEqual(
    problems,
    [],
    `CONTROL: the untouched page and the shipped constants are not green:\n  - ${problems.join("\n  - ")}`,
  );

  // The comment control's source: the SAME sentence the page renders, moved into a
  // comment, where the user cannot read it. The sentence is taken OFF THE PAGE, so
  // this control cannot go stale when the cap moves and the page is updated with it
  // (a hard-coded "the last 16 turns" would become a no-op that reds a correct tree).
  const turnPin = /bounded to the last \d+ turns/.exec(docsPage);
  assert.ok(turnPin, "the section no longer states the turn cap — retarget this control");
  const commented = docsPage
    .replace(turnPin[0], "bounded to the last few turns")
    .concat(`\n// The conversation you send is ${turnPin[0]}.\n`);

  /**
   * `src` with the integer literal `name` is declared with moved by `by`, and the
   * value it moved to.
   *
   * DERIVED FROM THE SHIPPED VALUE rather than written as a literal. A row that
   * moved MaxSteps to a hard-coded 9 would become a NO-OP the day 9 is what the
   * runner really ships, and a no-op row reds a tree that is correct — a false RED.
   * Measured, not reasoned about: on this card's on-disk move run the two
   * MaxTurnChars / MaxToolResultChars rows below did exactly that with literals.
   */
  const moveCap = (src: string, name: string, by: number): { src: string; next: number } => {
    const m = new RegExp(`\\b${name}\\s*=\\s*(\\d+)\\b`).exec(src);
    assert.ok(
      m,
      `${name} is no longer declared as an integer literal in the chat source — retarget this bite proof`,
    );
    const next = Number(m[1]) + by;
    return { src: src.replace(m[0], `${name} = ${next}`), next };
  };

  // (1) THE FIVE NUMBERS THE SECTION STATES: the constant moves and the sentence
  // keeps quoting the shipped value — the state that measured GREEN on this file
  // before this guard existed (16 -> 33, 2000 -> 7777, 4000 -> 9999, 4 -> 9, and
  // 6000 -> 1234 for the per-lookup cap, which was the state until t_902ab8bb gave
  // that cap a number on the page).
  for (const name of [
    "MaxTurns",
    "MaxMessageChars",
    "MaxReplyChars",
    "MaxSteps",
    "MaxToolResultChars",
  ]) {
    const { src: moved, next } = moveCap(goChat, name, 7);
    assert.notEqual(moved, goChat, `the ${name} move changed nothing — this row would be vacuous`);
    const found = capProseProblems(docsPage, moved);
    assert.ok(
      found.some((s) => s.includes(`${name} = ${next}`)),
      `${name} moved to ${next} and the section still quotes the shipped value, yet this guard ` +
        `reports nothing:\n  ${found.length ? found.join("\n  - ") : "(nothing)"}`,
    );
  }

  // (2) THE NUMBERLESS CLAIM: the section stops making the claim the cap stands
  // behind. The mutation is derived from the PIN itself (NUMBERLESS_CLAIMS), so a
  // legitimate reword that updates the pin cannot rot this row into a no-op.
  for (const { want } of NUMBERLESS_CLAIMS) {
    assert.equal(
      docsPage.split(want).length - 1,
      1,
      `the section does not carry exactly one "${want}" — the row below would prove nothing`,
    );
    const found = capProseProblems(docsPage.replace(want, "a different wording entirely"), goChat);
    assert.ok(
      found.some((s) => s.includes(`"${want}"`)),
      `the section drops "${want}" and this guard did not notice:\n  ` +
        `${found.length ? found.join("\n  - ") : "(nothing)"}`,
    );
  }

  // (3) THE COMMENT CONTROL: the same sentence, left only in a comment, must NOT
  // satisfy the pin. First that the mutated source really does carry it (otherwise
  // the row proves nothing), then that the guard reds anyway — the comment-stripped
  // read is what makes the difference, not a coincidence.
  assert.ok(
    commented.includes(turnPin[0]),
    "the comment control does not actually carry the sentence — that row proves nothing",
  );
  const commentedProblems = capProseProblems(commented, goChat);
  assert.ok(
    commentedProblems.some((s) => s.includes(JSON.stringify(turnPin[0]))),
    `a number left only in a COMMENT satisfies the pin, which is where the user cannot read it:\n  ` +
      `${commentedProblems.length ? commentedProblems.join("\n  - ") : "(nothing reported)"}`,
  );

  // (4) THE ONE CAP THE SECTION STILL STATES NO NUMBER FOR — MEASURED, not asserted
  // in prose. Its move leaves the page correct, so the guard stays GREEN (a RED here
  // would be the false RED the header refuses), and a rename still throws: it is
  // READ, not merely mentioned. MaxToolResultChars used to sit in this loop too; it
  // now has a number on the page, so it moved up to row (1), where a move REDS.
  for (const name of ["MaxTurnChars"]) {
    const { src: moved, next } = moveCap(goChat, name, 1);
    assert.notEqual(moved, goChat, `the ${name} move changed nothing — this row would be vacuous`);
    assert.deepEqual(
      capProseProblems(docsPage, moved),
      [],
      `${name} moved to ${next}, a value the page states nowhere, so nothing the page claims became ` +
        `false — a RED here would be a false RED, which is why the guard pins the CLAIM and not a number`,
    );
    assert.throws(
      () => capProseProblems(docsPage, moved.replace(`${name} = `, `${name}Renamed = `)),
      `${name} is renamed on the Go side and this guard did not notice — the constant has to be ` +
        `READ, not merely mentioned`,
    );
  }
});

// ── THE CAPS GUARD'S OWN BITE (t_4912a9bd) ───────────────────────────────────
//
// That guard compares two prose sources to the Go constants, and each half of the
// READ could be satisfied by something that is not the claim:
//
//   1. `read(DOCS_PAGE)` returned the page's raw BYTES, so the guarded sentence
//      could sit in a `//` comment while the rendered prose said something else.
//      Measured on the merged tree (guard = `node --test src/lib/chatPage.test.ts`):
//      the sentence moved into a comment near the imports with the rendered
//      sentence changed to "NINE messages per hour, in bursts of NINE" → GREEN
//      (0 fail), against a CONTROL (the sentence removed from the source
//      entirely) that was RED (1 fail). A guard satisfied by its own comment is
//      satisfied by nothing.
//   2. `String.includes(String(n))` was a SUBSTRING read, so "160 messages an
//      hour" satisfied `includes("60")` while the constant was still 60 → GREEN,
//      measured on both halves (the docs sentence and the `en` refusal).
//
// So this proof drives the SAME readers the guard uses — `codeOnly` and
// `quotesExactly` — over the real page source and the real dictionary entry,
// mutated in memory (nothing is written to disk, the idiom of the prose arm's
// proof below). BOTH directions are pinned: the shipped page and the shipped
// refusal must still MATCH, so this test cannot pass by making the reader refuse
// everything.
test("BITES: the caps guard reads rendered docs text, and only a bounded number", () => {
  // Through `codeOnly` like the guard it proves (t_629da97d): the anchor must be text
  // the page RENDERS, or a `//` line could anchor a mutation as well as satisfy a pin.
  const shippedDocs = codeOnly(read(DOCS_PAGE));
  const want = `${goConstNumber(goRest, "chatRatePerHour")} messages per hour, in bursts of ${goConstNumber(goRest, "chatBurst")}`;
  // Vacuity: the anchor must be on the page, or every mutation below lands
  // nowhere and this proof asserts nothing.
  assert.ok(
    shippedDocs.includes(want),
    `the docs page no longer carries the sentence this proof mutates (${JSON.stringify(want)}) — ` +
      `re-anchor the proof rather than letting it mutate nothing`,
  );
  assert.ok(
    quotesExactly(want).test(codeOnly(shippedDocs)),
    "the unmutated docs page does not satisfy the shipped reader — a red below could not be " +
      "attributed to the mutation",
  );

  // HOLE 1 — the sentence in a `//` comment, the rendered prose given a WRONG
  // number. The pre-fix raw read is shown to be satisfied first (`includes`),
  // so the hole is demonstrated rather than asserted.
  const commented = `// ${want}\n${shippedDocs.replace(want, "NINE messages per hour, in bursts of NINE")}`;
  assert.ok(
    commented.includes(want) && commented !== shippedDocs,
    "the comment mutation did not land, so this case proves nothing",
  );
  assert.ok(
    !quotesExactly(want).test(codeOnly(commented)),
    "a sentence inside a `//` COMMENT satisfies the caps guard again: the reader has gone back to " +
      "raw source, and a page can state a cap in text no user ever sees (t_4912a9bd)",
  );

  // HOLE 2 — a LONGER number in the rendered sentence, the constant unchanged.
  const widened = shippedDocs.replace(want, `1${want}`);
  assert.ok(
    widened.includes(want) && widened !== shippedDocs,
    "the longer-number mutation did not land, so this case proves nothing",
  );
  assert.ok(
    !quotesExactly(want).test(codeOnly(widened)),
    `"1${want}" satisfies the caps guard again: a substring read lets a page quote a DIFFERENT ` +
      `number (160 where the server enforces 60) while the guard reports it quotes the enforced one`,
  );

  // HOLE 3 — the same class on the dictionary half, driven on the real `en`
  // string the page shows on a 429.
  const n = goConstNumber(goRest, "chatRatePerHour");
  const entry = String((DICT["chat.rateLimited"] as Record<string, string>)["en"]);
  assert.ok(
    quotesExactly(String(n)).test(entry),
    `the shipped 429 string does not quote ${n} — the guard above would be red for a reason that ` +
      `is not this proof's, and a red below would be unattributable`,
  );
  const longerEntry = entry.replace(String(n), `1${n}`);
  assert.ok(
    longerEntry !== entry && longerEntry.includes(String(n)),
    "the dictionary mutation did not land, so this case proves nothing",
  );
  assert.ok(
    !quotesExactly(String(n)).test(longerEntry),
    `a 429 string quoting a LONGER number ("${longerEntry}") satisfies the guard again — the user ` +
      `would be told a limit the server does not enforce`,
  );
});

test("the page reads availability and stops offering the box when it is false", () => {
  assert.match(
    page,
    /\/api\/rest\/chat/,
    "the page does not call the chat status endpoint",
  );
  assert.match(
    page,
    /available\s*===\s*false/,
    "the page no longer treats available:false as 'do not offer the box'",
  );
  assert.ok(
    page.includes("chat.unavailable"),
    "the unavailable state has no message",
  );
  // The inverted read: a truthy check on `available` would treat null (still
  // loading) as unavailable and hide the box for every user.
  assert.doesNotMatch(
    page,
    /available\s*\?\s*<textarea/,
    "the box must be gated on available === false, not on a truthy/null read",
  );
});

test("the availability flag means \"can this deployment answer\", not \"was a struct built\"", () => {
  // The server used to answer `available: h.Chat != nil`. The runner is built
  // unconditionally (main.go), so that answered "did we construct the object",
  // not "can a message be answered" — and the page offered a box whose every
  // message died, which the comment above promises it never does. Pin the
  // indirection so the flag cannot silently go back to an object check.
  assert.ok(
    goRest.includes("h.chatAvailable()"),
    "rest_chat.go no longer reports availability through h.chatAvailable() — the GET flag and " +
      "the POST refusal must answer the same question",
  );
  assert.doesNotMatch(
    goRest,
    /"available":\s*h\.Chat\s*!=\s*nil/,
    'rest_chat.go reports `available: h.Chat != nil` again — a runner object exists even with no ' +
      "LLM credential, so the page would offer a box that only errors",
  );
  // The runner itself answers it, and it must consult the credential, not just
  // the presence of a model object.
  assert.match(
    goChat,
    /func\s*\(r \*Runner\)\s*Ready\(\)\s*bool/,
    "chat.Runner has no Ready() — the REST edge would have nothing to ask",
  );
  assert.ok(
    goChat.includes("HasCredential"),
    "Ready() no longer consults the model's credential — a keyless deployment would report itself " +
      "available",
  );
  // A rejected key is a second, later answer: the POST must map it to the 503
  // the page's setAvailable(false) branch handles, not to a bare 500.
  assert.ok(
    goRest.includes("respondAIErr"),
    "rest_chat.go no longer maps a rejected model credential to 503 — a rotated key would turn " +
      "every message into a 500 and the page's unavailable branch would never fire",
  );
});

test("a filed feedback is announced in the transcript with a link to /feedback", () => {
  assert.match(
    page,
    /filed\s*\?/,
    "the page no longer renders the filed card — the user would discover the thread later on " +
      "/feedback instead of seeing it happen",
  );
  assert.ok(
    page.includes('href="/feedback"'),
    "the filed card has no way to go read the thread it just filed",
  );
  assert.ok(
    page.includes("chat.filed"),
    "the filed card has no message",
  );
});

// ── THE REFUSAL BLOCK, READ AS THE USER READS IT (t_9b40f272) ────────────────
//
// WHAT WAS WRONG. The test below read the page's RAW bytes and asked
// `page.includes("chat.refused")`. That key left the page when t_7f1694cb moved
// the block's heading into lib/chatRefusal.ts, so on main 81946afc the string was
// on the page ONLY inside the comment above the block: the assertion was answered
// by a comment, and a comment proves nothing about what renders. Measured on main
// by this card (the real page mutated in place, restored by sha256): the rendered
// line replaced by a hard-coded English literal with the call site left in a
// `{/* … */}` comment kept the file GREEN (47 pass / 0 fail across both refusal
// files) and the WHOLE unit suite GREEN (998 tests, 998 pass, 0 fail). Deleting
// the rendered call outright was GREEN too: the comment above the block names the
// key, so the old pin could not see a deletion either.
//
// WHAT IT PINS INSTEAD. The block must RENDER its words: the heading and each
// refused step's why have to reach JSX, not merely be computed. The rule is a pure
// function over the source, so the BITES test below drives the IDENTICAL reader.

/**
 * Problems with the transcript's refusal block, as sentences.
 *
 * `src` is the page's raw source; the rule blanks comments itself, so a call site
 * left behind in a `//` line or wrapped in a JSX comment cannot stand in for a
 * rendered one.
 */
function refusedBlockProblems(src: string): string[] {
  const out: string[] = [];
  const code = codeOnly(src);
  // The block is only reachable if the wire's refused steps are separated out.
  if (!/"refused"/.test(code)) {
    out.push("the page no longer separates refused steps from successful ones");
  }
  // The transcript's refusal block: `{msgs.map(… m.refused ? ( … ) : null)}`.
  const at = code.indexOf("m.refused ?");
  const end = at === -1 ? -1 : code.indexOf(") : null,", at);
  if (at === -1 || end === -1) {
    out.push(
      "the transcript has no refusal block (`m.refused ? … : null`) — a refused step would be " +
        "swallowed and the user would read it as a broken answer",
    );
    return out;
  }
  const block = code.slice(at, end);
  out.push(
    ...renderedCallProblems(block, "refusalHeading(", "the block's heading is never rendered"),
    ...renderedCallProblems(block, "refusalText(", "a refused step's why is never rendered"),
  );
  return out;
}

/**
 * Problems with `block`'s rendering of the call `call` (written with its paren, so
 * `refusalText(` does not also match the import line). Rendered means the result
 * reaches JSX: inline (`{refusalText(…)`) or through a binding that is then
 * interpolated (`const why = refusalText(…); … ${why} …`), which is the shape the
 * page has. A call whose result is never interpolated is COMPUTED AND DROPPED —
 * the same defect class as the availability probe's message (t_57a77d02 above):
 * the tool name renders and the reason does not.
 */
function renderedCallProblems(block: string, call: string, what: string): string[] {
  const esc = call.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  if (new RegExp(`\\{\\s*${esc}`).test(block)) return [];
  for (const m of block.matchAll(new RegExp(`(?:const|let)\\s+([A-Za-z_$][\\w$]*)\\s*=\\s*${esc}`, "g"))) {
    const id = m[1].replace(/\$/g, "\\$");
    if (new RegExp(`\\$\\{\\s*${id}\\s*\\}|\\{\\s*${id}\\s*\\}`).test(block)) return [];
  }
  return [
    `${what} — ${call}…) is called but its result never reaches the block's JSX, so the user ` +
      `reads a bare tool name`,
  ];
}

test("a refused tool call is shown, not swallowed", () => {
  const problems = refusedBlockProblems(read(CHAT_PAGE));
  assert.deepEqual(problems, [], `the refusal block no longer renders:\n  - ${problems.join("\n  - ")}`);
});

// ── THE BITES: what this guard refuses, run through the guard's OWN rule ────
//
// The three mutations are the shapes the pre-fix read could not see. Each one is
// asserted on BOTH halves — the mutated source still carries the text (so a
// deletion cannot pass as evidence for this hole) and nothing the reader keeps
// carries it — and then the rule above is asked for its verdict. A regression to a
// raw `read()` or a `page.includes(...)` pin reds this test, not just a harness.
test("BITES: a commented-out or dropped refusal site cannot satisfy the refusal guard", () => {
  const shipped = read(CHAT_PAGE);
  // Vacuity: the shipped page satisfies the shipped rule, or a red below could be
  // the reader's fault rather than this proof's mutation.
  assert.deepEqual(refusedBlockProblems(shipped), [], "the shipped page does not satisfy the refusal rule");

  const HEADING = "{refusalHeading(m.refused, t)}";
  const WHY_RENDER = '{why ? ` — ${why}` : ""}';
  assert.ok(
    shipped.includes(HEADING) && shipped.includes(WHY_RENDER),
    "the refusal block's call sites moved — re-anchor this proof (heading / why render)",
  );

  // m1 — the heading's rendered result replaced by a literal, the call site left
  //      verbatim in a JSX comment. This is the measured 845/845-green mutation.
  const m1 = shipped.replace(
    HEADING,
    '{"That action is not allowed here."}{/* refusalHeading(m.refused, t) */}',
  );
  assert.ok(
    m1.includes("refusalHeading(m.refused, t)"),
    "m1 deleted the call instead of commenting it out — it would measure a deletion, not this hole",
  );
  assert.ok(!codeOnly(m1).includes("refusalHeading("), "m1 still RENDERS the heading — nothing was moved");
  assert.ok(
    refusedBlockProblems(m1).length > 0,
    "the refusal guard stays GREEN with the rendered heading replaced by a literal and the call site " +
      "left in a comment — a user would read one fixed English sentence in all five locales",
  );

  // m2 — the why computed and then DROPPED: `refusalText` is still called, the step
  //      renders the tool name alone. The comment hole's sibling (t_57a77d02's class).
  const m2 = shipped.replace(WHY_RENDER, "");
  assert.ok(
    m2.includes("const why = refusalText(s, t);"),
    "m2 removed the why's computation too — it would measure a deletion, not a dropped result",
  );
  assert.ok(!codeOnly(m2).includes("${why}"), "m2 still interpolates why");
  assert.ok(
    refusedBlockProblems(m2).length > 0,
    "the refusal guard stays GREEN when it no longer interpolates the why — a refusal would render a " +
      "bare tool name and no reason",
  );

  // m3 — the why's interpolation moved into a comment, its text left behind so a
  //      raw read still sees `{why}`.
  const m3 = shipped.replace(WHY_RENDER, '{""}{/* why ? ` — ${why}` : "" */}');
  assert.ok(m3.includes("{/* why ?"), "m3 deleted the interpolation instead of commenting it out");
  assert.ok(!codeOnly(m3).includes("${why}"), "m3 still RENDERS the why");
  assert.ok(
    refusedBlockProblems(m3).length > 0,
    "the refusal guard stays GREEN with the why's interpolation moved into a comment — the raw read " +
      "this card replaced was satisfied by exactly that",
  );
});

// ── THE WORKSPACE SCOPE (the defect this guard exists for) ───────────────────
//
// WHAT BROKE. This page talked to /api/rest/chat, /chat/confirm and /chat/cancel
// with no ?workspace= at all, so the server resolved `Accounts.ByOwner(...)[0]` —
// the user's FIRST account, not the workspace selected in the Shell. Phase 1 that
// answered about the wrong workspace; phase 2 made it WRITABLE, because the
// confirmation executes the stored proposal under the account the request
// resolved. A merchant with two workspaces selected the second, read a card naming
// the first one's product, and confirming it edited the first one.
//
// WHY A SOURCE GUARD AND NOT A UI TEST. This repo has no React/DOM runner (see
// src/lib/i18n.test.ts), so the only place the request URLs exist is the page
// source. Pinning the three call sites there is what makes the omission
// impossible: a fourth chat call added without the scope, or a hand-rolled
// `?workspace=` string, both fail here.
//
// The Go half is pinned cross-language in the same test: the server refuses an
// unscoped chat call from a multi-workspace session (requireResolvedWorkspaceScope
// in api/internal/httpapi/rest_scope.go), so the two halves cannot drift into
// "the page sends it" / "the server stopped caring" or the reverse.
const workspaceHelper = read("./workspace.ts");
const scopeFile = read("../../../api/internal/httpapi/rest_scope.go");
const chatTest = read("./chatPage.test.ts");

// ── THE ACCOUNT-SCOPE CENSUS'S READS AND ITS VERDICT, at module scope ────────
//
// Lifted out of the census test below rather than inlined, because the proof of
// the verdict's own bite runs THESE functions over a page mutated by value, and
// a proof that re-stated the rule would prove nothing about it (this file's
// `assistantBinClaims`/the BITES map above, trashCopyClaims.test.ts's
// `scanClaims`).

// Routes whose data is the PERSON's, not an account's — see the census test
// below for why each of the three is legitimately unscoped.
const USER_LEVEL = /\/api\/rest\/(feedback|uploads\/avatar|profile\/avatar)/;

/**
 * Every ACCOUNT-scoped REST URL the page builds as a string or template literal
 * — an /api/rest/… path, and what the census has to judge.
 *
 * (The path is written WITHOUT quote or backtick delimiters on purpose:
 * api/internal/httpapi/route_parity_test.go extracts every delimited /api/*
 * literal from web/src and requires it to resolve to a registered backend route,
 * and a delimited sample path here would be read as a real call. That guard
 * caught this very comment when it was first written with backticks around it.)
 */
function accountScopedUrls(code: string): string[] {
  return [...code.matchAll(/[`"](\/api\/rest\/[^`"]*)[`"]/g)]
    .map((m) => m[1])
    .filter((u) => !USER_LEVEL.test(u));
}

/**
 * THE CALL the census keys on: the shared helper's INVOCATION, however it is
 * spaced. A pattern rather than a literal because the bite proof below counts
 * the calls it removes with the same one.
 */
const WORKSPACE_QUERY_CALL = String.raw`\bworkspaceQuery\s*\(`;

/**
 * True when `code` actually CALLS the shared workspace-scope helper.
 *
 * A CALL, NOT A MENTION (t_a39a1bd4). This census decided with
 * `code.includes("workspaceQuery")`, and the helper's own NAME is also in the
 * page's import line — `import { workspaceQuery } from "../../lib/workspace"` —
 * so a page whose calls had all been removed but whose import was left behind
 * read as SCOPED. An unused import is exactly what a careless revert leaves, so
 * the census green-lit the very shape it exists to stop. Measured by QA on PR
 * 780's head (the merge preview of main 57f3d53 x #780 x #788): app/forms/
 * page.tsx with all 7 `${workspaceQuery()}` calls removed but the import left
 * ran the whole file 19/19 GREEN. Reproduced on this tree before the fix, by
 * value, through a probe that reads the page the way this census does:
 *
 *   app/chat/page.tsx   3 calls -> 0, import present, 3 account-scoped URLs left
 *                       -> the old rule still read the page as scoped
 *   app/forms/page.tsx  7 calls -> 0, import present, 6 account-scoped URLs left
 *                       -> the old rule still read the page as scoped
 *
 * Both shapes now read as offenders; the bite proof below is that measurement,
 * in the suite, over the WHOLE page set.
 *
 * WHAT IT STILL DOES NOT CATCH, stated honestly: a call whose RESULT the page
 * then ignores (`void workspaceQuery();`) reads as scoped, because following the
 * value into the URL it is interpolated into is dataflow a byte-level census
 * cannot do. That is also why the shape keyed on is the INVOCATION and not
 * `${workspaceQuery()}`: five of the thirteen subject pages legitimately bind it
 * (`const sp = workspaceQuery();`) and compose the URL with `${sp}`. Measured by
 * running the shipped census with the inline-only read substituted for this one
 * — the census test reds against a page that is not defective
 * (`app/activity/page.tsx`, the subject page whose only call is the bound form),
 * and the bite proof below reds with it. A false RED is the failure this guard
 * is least entitled to, so the read is the call, whichever shape carries it.
 */
function pageScopesWorkspace(code: string): boolean {
  return new RegExp(WORKSPACE_QUERY_CALL).test(code);
}

/**
 * The census's PAGE-LEVEL VERDICT for one page: the offender line, or null when
 * the page is not subject or is scoped.
 *
 * The verdict, not a count of pages: the whole point of this census is that an
 * unscoped page must be NAMED, and a test that only asserted the census was
 * non-empty would pass with every page's verdict wrong. `rel` is the page's
 * app-relative label — `app/chat/page.tsx`, the name the failure has to name —
 * which is what both callers pass and what the bite proof's substitution map is
 * keyed on.
 */
function accountScopeOffender(rel: string, code: string): string | null {
  const accountScoped = accountScopedUrls(code);
  if (accountScoped.length === 0) return null; // not a subject page
  if (pageScopesWorkspace(code)) return null;
  return (
    `${rel}: calls ${JSON.stringify([...new Set(accountScoped)])} with no ` +
    `workspaceQuery() — the server resolves Accounts.ByOwner(...)[0], the user's FIRST ` +
    `account, not the workspace they selected`
  );
}

test("the chat page scopes EVERY call with workspaceQuery() — the shared helper, not a hand-rolled string", () => {
  // Every chat call site the page builds must go through the helper. Counted
  // rather than merely "present": one call site left unscoped is the whole defect,
  // and a substring check for `workspaceQuery` passes with two of three converted.
  //
  // The call sites are counted by their FETCH, not by a URL literal, because the
  // decision route is written literally into `url` and fetched as
  // `${url}${workspaceQuery()}` — the route-parity rule demands the literal path
  // (see the phase-2 test above), so the guard has to look at the fetch site.
  const chatFetches = [...page.matchAll(/fetch(?:Json<[^>]*>)?\(\s*(`[^`]*`|"[^"]*")/g)].map(
    (m) => m[1],
  );
  const chatCalls = chatFetches.filter(
    (u) => u.includes("/api/rest/chat") || u === "`${url}${workspaceQuery()}`",
  );
  assert.ok(
    chatCalls.length >= 3,
    `expected the page to build at least three chat calls (status, message, decision), found ` +
      `${chatCalls.length}: ${JSON.stringify(chatFetches)}`,
  );
  for (const u of chatCalls) {
    assert.ok(
      u.includes("${workspaceQuery()}"),
      `the chat call ${u} does not append \${workspaceQuery()} — an unscoped chat call resolves the ` +
        `user's FIRST account, not the workspace they selected, and the confirmation lane WRITES there`,
    );
  }
  // The decision URL still gets scoped even though its path is literal.
  assert.ok(
    page.includes("await fetch(`${url}${workspaceQuery()}`"),
    "the confirm/cancel decision fetch no longer appends the workspace scope — the confirmation " +
      "would write to the user's first account",
  );
  // The page renders the scope refusal as its own message. Without this the user
  // sees the generic "could not answer" for a condition they can fix by picking a
  // workspace, and the server's explicit refusal is wasted.
  assert.ok(
    page.includes('t("chat.scopeRequired")'),
    "the page no longer renders a message for the workspace-scope refusal — the server refuses " +
      "explicitly precisely so the user can act on it",
  );
  // The PROBE's 400 must be separated from the generic failure too, not only the
  // message path's. The availability GET is the page's FIRST chat call and it
  // fires before the Shell has selected a workspace on a fresh session, so on a
  // two-workspace session it is the call that actually meets the scope refusal
  // (t_997bb646). Falling through to `setAvailable(false)` renders
  // chat.unavailable ("not available right now") — false, and it closes the box
  // for a condition the user fixes by choosing a workspace.
  //
  // MEASURED, not assumed (both mutations applied to this tree, the file's test
  // run alone, then reverted; the parenthesised totals are the file's size when
  // each card measured it, which has since grown):
  //   * the 400 branch deleted            -> RED here (19/20)
  //   * `setAvailable(false)` replaced by the scope setErr on the generic path
  //     -> RED here (19/20), because the unavailable branch must survive
  //
  // t_4912a9bd re-measured the first of those against the reader this test now
  // uses: deleting the branch outright was RED, and leaving its text
  // behind as a `//` comment was GREEN on a raw-source read — measured, 21 pass /
  // 0 fail, with the page rendering chat.unavailable — and is RED on
  // `probeBranchCode`, which the bite proof below drives.
  // The browser-level half of the same defect is e2e/chat-probe-400.spec.ts,
  // which drives the built page and fails on the pre-fix build with
  // "locator('p[role=\"alert\"]') element(s) not found".
  //
  // The branch is located by its own site, the runLoad outcome's `!r.ok` guard —
  // not by a line number — and the slice runs to the statement after that block
  // (`setAvailable(r.data.available …)`), so it covers the WHOLE !r.ok branch
  // including the generic fall-through the scope branch must not replace.
  //
  // AND IT READS CODE, NOT RAW SOURCE (t_4912a9bd). The reader is
  // `probeBranchCode` above, so the bite proof below drives the IDENTICAL
  // function: the branch deleted and its text left as a `//` line kept this
  // guard GREEN (21 pass / 0 fail) on the merged tree, while the deletion
  // control redded it.
  const probeBranch = probeBranchCode(page);
  assert.notEqual(
    probeBranch,
    null,
    "app/chat/page.tsx no longer delimits the availability probe's !r.ok branch — this guard " +
      "refuses to slice a branch it cannot delimit rather than guessing. It anchors on " +
      "`const r = await runLoad(`, the `if (!r.ok) {` branch after it, and the " +
      "`setAvailable(r.data.available …` statement that ends that block (t_4912a9bd)",
  );
  assert.match(
    probeBranch as string,
    PROBE_400_SCOPE_BRANCH,
    `the availability probe's !r.ok branch does not answer a 400 with t("chat.scopeRequired") — a ` +
      `two-workspace session with no workspace selected would read "The assistant is not available ` +
      `right now." (chat.unavailable) instead of the scope copy, and the composer would be ` +
      `disabled. Branch was:\n${probeBranch}`,
  );
  // And the generic branch survives: a 5xx / network failure is still
  // "unavailable", so the scope branch cannot have replaced it.
  assert.match(
    probeBranch as string,
    /setAvailable\(false\)/,
    "the availability probe no longer closes the box on a genuine failure — the scope branch must " +
      "be ADDITIONAL to the unavailable branch, not a replacement for it",
  );
  // 400 must not be folded into the 401/403 branch (that branch redirects to
  // /login, which would be wrong for a signed-in user whose scope was missing).
  assert.ok(
    !/status === 400[^\n]*goToLogin/.test(page),
    "the page treats a 400 as a dead session — a missing workspace scope would redirect a " +
      "signed-in merchant to /login",
  );
  // No hand-rolled query string in CODE: the active-workspace rule lives in one
  // place (lib/workspace.ts), and a second spelling is how it drifts. Comments are
  // stripped first — this page documents the `?workspace=` rule in prose, and a
  // guard that fires on its own documentation is a guard people delete.
  const pageCode = page
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((l) => !/^\s*(\/\/|\*)/.test(l))
    .join("\n");
  assert.ok(
    !/workspace=\$\{|[?&]workspace=/.test(pageCode),
    "app/chat/page.tsx builds ?workspace= by hand instead of using workspaceQuery() — the " +
      "active-workspace rule must stay in ONE place (lib/workspace.ts)",
  );
  assert.ok(
    /import \{[^}]*\bworkspaceQuery\b[^}]*\} from "\.\.\/\.\.\/lib\/workspace"/.test(page),
    "app/chat/page.tsx does not import workspaceQuery from the shared helper",
  );
  // And the helper is still the real thing, not a stub the page could satisfy by
  // importing it and sending nothing.
  assert.match(
    workspaceHelper,
    /export function workspaceQuery\(\): string \{\s*const sp = [^;]+;\s*return sp \? `\?workspace=\$\{encodeURIComponent\(sp\)\}` : "";/,
    "lib/workspace.ts workspaceQuery() no longer returns `?workspace=<slug>` — the page would import a " +
      "helper that scopes nothing",
  );
  //
  // The scope it reads is the SESSION-CONFIRMED one, falling back to the stored
  // slug while no answer has arrived (t_da4b2031). Both halves are load-bearing:
  // without the confirmed scope a stale slug still goes out on first paint, and
  // without the fallback every first request is unscoped.
  assert.match(
    workspaceHelper,
    /confirmedScope \?\? workspaceScope\(\)/,
    "workspaceQuery() no longer prefers the confirmed scope — a slug the server has NOT confirmed (a " +
      "workspace renamed or deleted while selected) would be sent again on the next fetch, which is the " +
      "stale-scope defect this card exists for",
  );
});

// ── THE PROBE BRANCH'S OWN BITE (t_4912a9bd) ─────────────────────────────────
//
// The branch assertion above is a PATTERN match, and the reader used to slice the
// RAW page — so a `//` line carrying the branch satisfied it. Measured on the
// merged tree (guard = `node --test src/lib/chatPage.test.ts`, the file's test run
// alone): the REAL 400 branch deleted and
// `// if (r.status === 400) { setErr(t("chat.scopeRequired")); return; }` left in
// its place → GREEN (21 pass / 0 fail) while the page rendered "The assistant is
// not available right now." with the composer disabled; the deletion CONTROL
// (nothing left behind) was RED (1 fail), so that green was not a dead harness.
//
// The mutation below is in memory, on the real page source, and it drives
// `probeBranchCode` — the IDENTICAL function the assertion above calls, so a
// regression to a raw slice cannot pass this proof. The hole column is the
// pre-fix read of the same region (raw source, the same delimiters), so the green
// is shown rather than asserted.
test("BITES: a commented-out probe 400 branch cannot satisfy the availability guard", () => {
  const raw = read("../app/chat/page.tsx");
  const SCOPE = 'setErr(t("chat.scopeRequired"));';
  // The pre-fix reader, verbatim: the same region, from RAW source.
  const rawSlice = (src: string) => {
    const at = src.indexOf("const r = await runLoad(");
    const from = src.indexOf("if (!r.ok) {", at);
    const end = src.indexOf("setAvailable(r.data.available", from);
    return src.slice(from, end);
  };
  // Vacuity: the shipped page satisfies the shipped reader, or a red below could
  // be the reader rather than this proof's mutation.
  const shipped = probeBranchCode(raw);
  assert.notEqual(shipped, null, "the shipped page no longer delimits the probe's !r.ok branch");
  assert.match(
    shipped as string,
    PROBE_400_SCOPE_BRANCH,
    "the shipped page does not satisfy the branch assertion, so a red below could not be " +
      "attributed to this proof's own mutation",
  );

  // The real branch, cut out by the same anchors the reader uses, then replaced
  // by the single-line comment form of its OWN text.
  const at = raw.indexOf("if (r.status === 400) {");
  const scopeAt = raw.indexOf(SCOPE, at);
  const retAt = raw.indexOf("return;", scopeAt);
  const closeAt = raw.indexOf("}", retAt);
  assert.ok(
    at !== -1 && scopeAt > at && retAt > scopeAt && closeAt > retAt,
    "the page's first 400 branch is no longer the probe's scope refusal — re-anchor this proof",
  );
  const branch = raw.slice(at, closeAt + 1);
  assert.ok(
    branch.includes(SCOPE),
    "the cut branch does not answer a 400 with the scope copy — re-anchor this proof",
  );
  const commented =
    raw.slice(0, at) +
    `// ${branch.split("\n").map((l) => l.trim()).join(" ")}` +
    raw.slice(at + branch.length);
  assert.notEqual(commented, raw, "the comment mutation did not land");

  // THE HOLE, as it was: the RAW-source read still holds the branch's text.
  assert.match(
    rawSlice(commented),
    PROBE_400_SCOPE_BRANCH,
    "the raw-source read no longer finds the branch in its comment — this case does not reproduce " +
      "the hole the card closed, and the assertion below would prove nothing",
  );
  // THE FIX: the shipped reader cannot be satisfied by it.
  const after = probeBranchCode(commented);
  assert.notEqual(after, null, "the mutated page no longer delimits the probe's !r.ok branch");
  assert.ok(
    !PROBE_400_SCOPE_BRANCH.test(after as string),
    "a `//` COMMENT carrying the 400 branch satisfies this guard again: the real branch could be " +
      "gone while the suite stays green, and a two-workspace session would read \"The assistant is " +
      "not available right now.\" with the composer disabled (t_4912a9bd)",
  );
  // …and the generic branch is still inside the mutated slice, so the assertion
  // above is not passing because the slice collapsed to nothing.
  assert.match(
    after as string,
    /setAvailable\(false\)/,
    "the mutated slice lost the generic unavailable branch as well — this proof is not measuring " +
      "the comment form",
  );
});

test("the SERVER refuses a chat call whose workspace would be guessed", () => {
  // The gate exists, is wired into the chat lane, and is not merely a 403 the
  // page could ignore: a single-workspace session must still be served.
  assert.match(
    scopeFile,
    /func \(h \*RESTHandlers\) requireResolvedWorkspaceScope\(/,
    "rest_scope.go no longer defines the refusal for an unresolvable workspace scope — the chat would " +
      "fall back to Accounts.ByOwner(...)[0] again",
  );
  assert.match(
    scopeFile,
    /case scopeUnresolved:[\s\S]{0,200}?http\.StatusForbidden/,
    "the foreign-slug refusal is no longer a 403",
  );
  // The MISSING-scope refusal must NOT be a 401/403. wsfetch.ts is the module
  // every dashboard fetch goes through, and the chat page repeats the rule by
  // hand: 401/403 means "the session is gone" and redirects to /login. A
  // signed-in, two-workspace merchant whose client omitted the scope would be
  // logged out of the page they are reading — a worse bug than the one this card
  // fixes. Pinned here because it is a CLIENT contract living in Go code.
  assert.match(
    scopeFile,
    /case scopeAmbiguous:[\s\S]{0,200}?http\.StatusBadRequest/,
    "the missing-scope refusal must be a 400, not a 401/403 — wsfetch.ts redirects 401/403 to " +
      "/login, so a 403 here would bounce a signed-in merchant to the login page",
  );
  //
  // THE OTHER HALF OF THE SAME HAZARD (t_da4b2031): a 403 that IS reachable —
  // the foreign-slug refusal above — must not be read as a dead session either.
  // wsfetch splits it by the body's machine code instead of by status, so the
  // rule this guard protects is now "401, or a 403 the API did NOT name as a
  // tenancy refusal". Pinned as source because the contract lives in Go
  // (scopeOr403's 403 + the tenancy code) and is consumed in TS.
  assert.match(
    scopeFile,
    /case scopeUnresolved:[\s\S]{0,200}?http\.StatusForbidden/,
    "the foreign-slug refusal is no longer a 403 — wsfetch.ts's split (and this guard) key on that pair",
  );
  assert.ok(
    read("./wsfetch.ts").includes("return !isScopeRefusal(body);"),
    "wsfetch.ts no longer splits a 403 by the tenancy code — every 403 would be a dead session again, " +
      "so a stale jf_workspace would sign a merchant out (t_da4b2031)",
  );
  assert.match(
    goRest,
    /func \(h \*RESTHandlers\) chatWorkspaceScopeOr403\(/,
    "rest_chat.go no longer applies the scope gate — the page sending ?workspace= must not be the only " +
      "thing standing between a two-workspace merchant and a write to the wrong workspace",
  );
  // The gate is applied on all FOUR chat routes: a decision endpoint left
  // unscoped is the same defect with a narrower door.
  const gateCalls = [...goRest.matchAll(/h\.chatWorkspaceScopeOr403\(w, r, actx\)/g)].length;
  assert.ok(
    gateCalls >= 4,
    `rest_chat.go applies the scope gate at ${gateCalls} call site(s), want 4 (GET status, POST message, ` +
      `POST confirm, POST cancel) — an unscoped route resolves the first account`,
  );
  // Each of the four handlers must be one of the call sites (a call sitting in a
  // dead helper would satisfy the count above and gate nothing).
  for (const handler of [
    "handleChatStatus",
    "handleChat",
    "handleChatConfirm",
    "handleChatCancel",
  ]) {
    const body = goRest.slice(goRest.indexOf(`func (h *RESTHandlers) ${handler}(`));
    const next = body.indexOf("\nfunc ", 1);
    const fn = next > 0 ? body.slice(0, next) : body;
    assert.ok(
      fn.includes("chatWorkspaceScopeOr403"),
      `${handler} does not refuse an unresolvable workspace scope — that route would resolve the ` +
        `caller's first account`,
    );
  }
  // The two halves must be tested together, or this file could pass while the Go
  // guard is deleted: the server-side criteria live in
  // chat_workspace_scope_test.go.
  assert.ok(
    existsSync(path("../../../api/internal/httpapi/chat_workspace_scope_test.go")),
    "the Go guard for this rule is gone — the source pins above would then be the only thing " +
      "asserting it, and they would pass with the server refusing nothing",
  );
  assert.ok(
    chatTest.includes("chatWorkspaceScopeOr403"),
    "this guard no longer reads the gate it claims to pin",
  );
});

test("no dashboard page calls an ACCOUNT-scoped REST route without the workspace scope", () => {
  // THE CLASS, not the instance. The chat's omission was not special: every
  // dashboard page that resolves its account from ?workspace= must send it, and
  // the next page written the same way fails here rather than shipping.
  //
  // WHAT MAKES A PAGE SUBJECT TO THE RULE. Only pages that reach an
  // ACCOUNT-scoped route. Two families are legitimately unscoped and must NOT be
  // flagged, because on those routes the scope is not what identifies the data:
  //
  //   * USER-level routes — /api/rest/feedback*, /api/rest/uploads/avatar,
  //     /api/rest/profile/avatar. The handlers key them on sessionOwnerID (the
  //     person), not on an account, so there is no workspace to send. Sending one
  //     would be noise, and requiring it would be a rule that cannot be obeyed.
  //   * Non-REST and public-URL fetches (a form's own /f/<slug>, docs assets).
  //
  // Everything else under /api/rest/ that a Shell page calls IS account-scoped:
  // the lists, the products, the builder, settings, deliveries, templates.
  //
  // `pages` is the module-level census (walkAppPages above) so this guard and the
  // machine-code guard below judge the same set of files.
  assert.ok(
    pages.length >= 13,
    `expected the dashboard's pages, got ${pages.length} — the walk must be finding the real app tree`,
  );

  // `USER_LEVEL` (which routes are the PERSON's, not an account's) and the two
  // reads the verdict is built from live at module scope above, beside that
  // verdict — so the bite proof below runs THIS census rather than a
  // restatement of it.
  //
  // Comments stripped by the repo's SHARED, JSX-text-aware blanker — the pages
  // document the ?workspace= rule in prose, and a class quoted in a comment must
  // not satisfy this census.
  //
  // It used to hand-roll `src.replace(/\/\*[\s\S]*?\*\//g, "")`, and that copy
  // carried the bug jsxSource.ts exists to remove (measured on this tree, not
  // reasoned about): JSX text renders literals like `/v1/rest/*`, whose `/`+`*`
  // opened a phantom block comment that blanked 12,419 characters of
  // app/forms/page.tsx — including its /api/rest/ URLs. A page with EVERY
  // workspaceQuery removed then read GREEN, because the census could no longer
  // see what it was supposed to judge. `codeOnly` keeps every byte offset, so the
  // census sees the same bytes a compiler would.
  const stripComments = (src: string) => codeOnly(src);

  const offenders: string[] = [];
  let subjectPages = 0;
  for (const abs of pages) {
    const code = stripComments(readAbs(abs));
    // The two reads the verdict is made of: which ACCOUNT-scoped REST URLs the
    // page builds (the subject test), and whether it CALLS the scope helper
    // (`accountScopeOffender`, which is the guard's whole point — a bare mention
    // is not a call; see `pageScopesWorkspace`).
    if (accountScopedUrls(code).length === 0) continue;
    subjectPages += 1;
    // The label a failure names: `app/chat/page.tsx`, not the bare path under
    // `src/app` (the census's own messages have to be grep-able as page names).
    const off = accountScopeOffender(join("app", relative(appDir, abs)), code);
    if (off) offenders.push(off);
  }
  assert.ok(
    subjectPages >= 10,
    `only ${subjectPages} page(s) were judged account-scoped — the census is not seeing the real ` +
      `pages, so this guard would pass vacuously`,
  );
  assert.deepEqual(offenders, [], `account-scoped pages missing the workspace scope:\n  ${offenders.join("\n  ")}`);
  // The chat is the page this guard exists for, so it must be one of the judged
  // pages — not excluded by the filters above.
  assert.ok(
    pages.some((p) => p.endsWith(join("app", "chat", "page.tsx"))),
    "the walk did not include app/chat/page.tsx — the page whose omission this guard exists for",
  );
});

// ── THE CENSUS'S OWN BITE: A CALL, NOT A MENTION (t_a39a1bd4) ────────────────
//
// WHAT THE HOLE WAS. The census above decided "scoped" with
// `code.includes("workspaceQuery")`, and the helper's name is also in the page's
// IMPORT line, so a page reverted to unscoped with the now-unused import left
// behind read as scoped. Measured by QA on PR 780's head (the merge preview of
// main 57f3d53 x #780 x #788): app/forms/page.tsx with all 7 calls removed but
// the import left ran the whole file 19/19 GREEN, and app/chat/page.tsx with its
// 3 calls removed was not among the run's 2 failures. Reproduced on this tree
// before the fix, by value: chat 3 calls -> 0 and forms 7 calls -> 0, the import
// still present, and the old page-level verdict still "scoped".
//
// WHY THE PROOF RE-RUNS THE WHOLE CENSUS AND NOT ONE PAGE. The subject here is a
// page-LEVEL verdict, so a test that only asserted the census was non-empty
// would be satisfied by a census that judged every page wrongly. The mutation is
// substituted into the real page set and the census's own loop is re-run over
// it: the two mutated pages must be NAMED, and nothing else may move.
//
// The mutation is the card's shape BY VALUE — every `${workspaceQuery()}` call
// removed, the import left exactly as a careless revert leaves it — so what is
// refused is the state itself rather than a paraphrase of it.
test("the ACCOUNT-scope census reads a CALL, not a mention — an unused import cannot satisfy it (t_a39a1bd4)", () => {
  // The card's measured call counts, per page, as FLOORS: a call ADDED later is
  // not a failure, but a page whose calls are already missing before the
  // mutation is — that would make this proof vacuous.
  const CASES = [
    { page: join("chat", "page.tsx"), calls: 3 },
    { page: join("forms", "page.tsx"), calls: 7 },
  ];
  // The name the census gives a page in its failure (`app/chat/page.tsx`), built
  // the same way the census loop below builds it — so the substitution map is
  // keyed on the census's own labels and not on a second spelling of them.
  const label = (page: string) => join("app", page);

  const mutated = new Map<string, string>();
  for (const { page, calls } of CASES) {
    const rel = label(page);
    const shipped = codeOnly(readAbs(join(appDir, page)));

    // CONTROL. The shipped page is a SUBJECT page and is SCOPED, so the flip
    // below is the mutation's doing and not a census that was already flagging
    // this page for some other reason.
    const shippedCalls = [...shipped.matchAll(new RegExp(WORKSPACE_QUERY_CALL, "g"))].length;
    assert.ok(
      shippedCalls >= calls,
      `${rel}: only ${shippedCalls} workspaceQuery() call site(s) in the shipped page, want at ` +
        `least ${calls} — the page this proof mutates is not the page the card measured`,
    );
    assert.ok(
      accountScopedUrls(shipped).length > 0,
      `${rel}: the shipped page builds no account-scoped URL, so it is not a subject page at all`,
    );
    assert.equal(
      accountScopeOffender(rel, shipped),
      null,
      `${rel}: the SHIPPED page is already an offender — this proof would pass for the wrong reason`,
    );

    // THE MUTATION, by value: every call goes, the import stays.
    const mutant = codeOnly(readAbs(join(appDir, page)).replace(/workspaceQuery\s*\(\s*\)/g, ""));
    assert.notEqual(
      mutant,
      shipped,
      `${rel}: the mutation changed nothing — this proof would be vacuous`,
    );
    assert.equal(
      [...mutant.matchAll(new RegExp(WORKSPACE_QUERY_CALL, "g"))].length,
      0,
      `${rel}: a workspaceQuery() call survived the mutation — the card's shape is "every call removed"`,
    );
    assert.ok(
      /import\s*\{[^}]*\bworkspaceQuery\b[^}]*\}\s*from\s*"[^"]*lib\/workspace"/.test(mutant),
      `${rel}: the mutation removed the import too — the shape this card is about is "the calls are ` +
        `gone and the unused import is left behind", and removing both is a state the OLD rule caught`,
    );
    // The old rule's entire input, said out loud: the MENTION is still there, so
    // `code.includes("workspaceQuery")` would still read this page as scoped.
    assert.ok(
      mutant.includes("workspaceQuery"),
      `${rel}: the mutation left no mention at all — this proof would then not tell a mention-keyed ` +
        `rule from a call-keyed one`,
    );
    assert.ok(
      !pageScopesWorkspace(mutant),
      `${rel}: a page with every call removed but its import left behind still reads as SCOPED — the ` +
        `census is keying on a mention, not on a call`,
    );
    // And it is still SUBJECT: the URLs the removed calls were appending to are
    // still there, so the page cannot slip out of the census instead of being
    // named by it.
    assert.ok(
      accountScopedUrls(mutant).length > 0,
      `${rel}: the mutation also removed the account-scoped URLs, so the page would fall out of the ` +
        `census rather than be named by it`,
    );
    mutated.set(rel, mutant);
  }

  // THE CENSUS, RE-RUN — the same loop the test above runs, over the real page
  // set with the mutated bytes substituted for the two pages.
  const offenders: string[] = [];
  let subjectPages = 0;
  for (const abs of pages) {
    const rel = join("app", relative(appDir, abs));
    const code = mutated.get(rel) ?? codeOnly(readAbs(abs));
    if (accountScopedUrls(code).length === 0) continue;
    subjectPages += 1;
    const off = accountScopeOffender(rel, code);
    if (off) offenders.push(off);
  }
  assert.ok(
    subjectPages >= 10,
    `only ${subjectPages} page(s) were judged account-scoped over the mutated set — the census went ` +
      `hollow under the mutation, so this proof would pass without judging anything`,
  );
  assert.deepEqual(
    [...new Set(offenders.map((o) => o.split(":")[0]))].sort(),
    CASES.map((c) => label(c.page)).sort(),
    `the census over the mutated page set did not name exactly the two mutated pages — an unused ` +
      `import makes a page read as scoped again, or an untouched page was newly flagged:\n  ` +
      offenders.join("\n  "),
  );
  for (const { page } of CASES) {
    const rel = label(page);
    assert.ok(
      offenders.some((o) => o.startsWith(`${rel}:`)),
      `the scope guard does not FAIL naming ${rel} with every call removed and the import left: ` +
        `${JSON.stringify(offenders)}`,
    );
  }
});

// ── THE TRANSLATION HALF (the #6177 direction this file was missing) ─────────
//
// WHAT BROKE, AND WHY IT WAS INVISIBLE. The scope test above fires the moment
// #747's `workspaceQuery()` leaves the chat page. The other half of the SAME
// call site — #6177's `errorKey: (b) => translateApiErr(b, t)`, which turns the
// server's machine `code` into the user's language — had no guard at all.
// Measured on a real merge candidate (PR 742 x main, chat/page.tsx resolved as
// the union of both), neutralising that `errorKey` by value:
//
//     node --test src/lib/chatPage.test.ts src/lib/apiErr.test.ts
//       -> tests 24, pass 24, fail 0        (before this test existed)
//
// and the page silently went back to printing only its own generic
// "the assistant is unavailable" for a refusal the server named precisely.
// `apiErr.test.ts`'s banned-shape scan looks for `body.error ||` fallbacks —
// dropping the option entirely is not that shape, so nothing noticed.
//
// WHY THE EXTRACTOR IS THE POINT. The obvious guard is `page.includes(...)`,
// and it is the one that fails: `errorKey` is a property name that also occurs
// in `lib/workspace.ts`, so a substring check passes on a page that dropped it.
// The two helpers below read the call site's actual argument object and the
// actual value of its `errorKey`, and every assertion quotes what it saw — so a
// wrong read reports a wrong value rather than a confident green.

/**
 * The source of every `runLoad(...)` call in `src`.
 *
 * Balanced-paren slice from the call's `(` to its matching `)`. Deliberately not
 * a full TypeScript parse: the file under test is the thing being judged, and a
 * guard that needs the parser stack to hold an opinion about it would be a
 * heavier dependency than the two properties it checks (the call is a chat call;
 * its `errorKey` routes through apiErr). Offsets are irrelevant here, only the
 * slice's text is.
 */
function runLoadCalls(src: string): string[] {
  const out: string[] = [];
  const re = /\brunLoad\s*\(/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    let i = m.index + m[0].length;
    let depth = 1;
    let quote = "";
    for (; i < src.length && depth > 0; i++) {
      const c = src[i];
      if (quote) {
        if (c === "\\") i++;
        else if (c === quote) quote = "";
        continue;
      }
      if (c === '"' || c === "'" || c === "`") {
        quote = c;
        continue;
      }
      if (c === "(" || c === "{" || c === "[") depth++;
      else if (c === ")" || c === "}" || c === "]") depth--;
    }
    out.push(src.slice(m.index, i));
  }
  return out;
}

/**
 * The literal source text of `key`'s value inside a call slice, or null when the
 * key is absent. Stops at the depth-0 `,` or closing bracket, so a caller reads
 * `"\"" ` for `errorKey: ""` and the arrow function itself for the real site.
 */
function propValue(slice: string, key: string): string | null {
  const m = new RegExp(`(?<![\\w$])${key}\\s*:\\s*`).exec(slice);
  if (!m) return null;
  let i = m.index + m[0].length;
  let depth = 0;
  let quote = "";
  const start = i;
  for (; i < slice.length; i++) {
    const c = slice[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = "";
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      quote = c;
      continue;
    }
    if (c === "(" || c === "{" || c === "[") depth++;
    else if (c === ")" || c === "}" || c === "]") {
      if (depth === 0) break;
      depth--;
    } else if (c === "," && depth === 0) break;
  }
  return slice.slice(start, i).trim();
}

test("the chat page's availability fetch keeps the machine-code translation, not only the scope", () => {
  // The tree's state, read rather than assumed. On the branch that carries
  // #6177 the mechanism is lib/apiErr; on a tree without it the option does not
  // exist anywhere in the app and there is nothing to pin yet. Both states are
  // stated in the assertion message below, so neither can be mistaken for the
  // other in a log.
  const treeHasApiErr = existsSync(path("./apiErr.ts"));
  const appHasErrorKey = walkRunLoadFiles(srcDir).some((rel) =>
    /(?<![\w$])errorKey\s*:/.test(readAbs(join(srcDir, rel))),
  );
  assert.ok(
    !appHasErrorKey || treeHasApiErr,
    "a page passes runLoad an `errorKey` but lib/apiErr.ts does not exist — the option " +
      "has no meaning on a tree without the #6177 mechanism",
  );

  // THE call site. Isolated by its URL rather than by a line number, so an edit
  // above it cannot silently move the guard onto a different call.
  const pageCode = codeOnly(page);
  const availability = runLoadCalls(pageCode).filter((s) => /\/api\/rest\/chat\b/.test(s));
  assert.equal(
    availability.length,
    1,
    `expected exactly one runLoad site on the chat availability route, found ` +
      `${availability.length}: this guard judges one call site and refuses to guess which`,
  );
  const site = availability[0];

  if (!treeHasApiErr) {
    // The #6177 mechanism is not in this tree, so the option does not exist to
    // pass. NOT a pass on the behaviour — the state the guard judged, said out
    // loud. The moment lib/apiErr.ts lands, the branches below are live and the
    // failure above (a page using `errorKey` without the module) is impossible.
    assert.equal(
      propValue(site, "errorKey"),
      null,
      "the tree has no lib/apiErr.ts yet the page passes an errorKey — see the assertion " +
        "above this one",
    );
    return;
  }

  // The union is the thing that must survive: the scope AND the translation on
  // the one call. The scope's half is asserted by the test above; its presence
  // is re-checked here in the same slice only so a failure names the whole
  // call site rather than one half of it.
  assert.match(
    site,
    /\/api\/rest\/chat\$\{workspaceQuery\(\)\}/,
    `the chat availability fetch lost #747's workspace scope — an unscoped chat call ` +
      `resolves the user's FIRST account, not the workspace they selected. Site: ${site.trim()}`,
  );

  const value = propValue(site, "errorKey");
  assert.notEqual(
    value,
    null,
    `the chat availability fetch has no errorKey — the server's machine code is dropped and ` +
      `the user reads only the page's generic "the assistant is unavailable" for a refusal ` +
      `the server named precisely (#6177). Site: ${site.trim()}`,
  );
  assert.match(
    value as string,
    /\btranslateApiErr\s*\(/,
    `the chat availability fetch's errorKey (${JSON.stringify(value)}) does not route through ` +
      `translateApiErr, so the API's machine code is not translated to the user's language (#6177)`,
  );
  assert.match(
    value as string,
    /\bt\b/,
    `the errorKey at ${JSON.stringify(value)} does not receive the page's translator, so it ` +
      `cannot render anything localized`,
  );

  // And #6177's helper is really in this file: the option is a function of the
  // imported module, not a local re-implementation the guard would happily read.
  assert.match(
    pageCode,
    /import\s*\{[^}]*\btranslateApiErr\b[^}]*\}\s*from\s*"\.\.\/\.\.\/lib\/apiErr"/,
    "app/chat/page.tsx's errorKey names translateApiErr without importing it from lib/apiErr",
  );
});

test("every runLoad call site that translates an API code routes through lib/apiErr", () => {
  // THE CLASS, not just the chat instance above. A hand-rolled body read —
  // `errorKey: (b) => (b as any).code` — re-introduces exactly the defect the
  // option exists to remove: an untranslated machine key rendered to the user.
  //
  // The census is every runLoad CALL SITE under src/, found by reading the files
  // that mention runLoad — NOT the app/ page walk. Five of the app's sites live
  // in a component (components/MembersTab.tsx) rather than a page.tsx, and a
  // census that judged only pages would leave those unread while looking
  // complete. Measured: with the page-shaped census, neutralising MembersTab's
  // errorKey left this file fully green (25/25) and the hole was invisible.
  //
  // Guarded on the tree's state for the same reason the chat test is: without
  // lib/apiErr.ts in the tree the option does not exist to judge.
  if (!existsSync(path("./apiErr.ts"))) return;

  const ROUTES_THROUGH_APIERR = /\b(translateApiErr|apiErrorKey|apiErr)\s*\(/;
  const callSiteFiles = walkRunLoadFiles(srcDir);
  assert.ok(
    callSiteFiles.length >= 15,
    `expected the app's runLoad call sites across at least 15 files, found ` +
      `${callSiteFiles.length} — the census is not seeing the real tree`,
  );
  const offenders: string[] = [];
  let errorKeySites = 0;
  for (const rel of callSiteFiles) {
    for (const s of runLoadCalls(codeOnly(readAbs(join(srcDir, rel))))) {
      const v = propValue(s, "errorKey");
      if (v === null) continue;
      errorKeySites += 1;
      if (!ROUTES_THROUGH_APIERR.test(v)) {
        offenders.push(`${rel}: errorKey: ${v}`);
      }
    }
  }
  // A census that reads few errorKeys would pass vacuously the moment the
  // extractor stopped parsing — the count is asserted for the same reason the
  // page walk's is. The floor is a little below the 26 sites the app has today,
  // so adding or removing a call site is not itself a failure.
  assert.ok(
    errorKeySites >= 20,
    `only ${errorKeySites} errorKey call site(s) were read across ${callSiteFiles.length} files ` +
      `— the extractor is not seeing the argument objects it claims to judge`,
  );
  assert.deepEqual(
    offenders,
    [],
    `these runLoad sites build their errorKey without the lib/apiErr helpers, so the server's ` +
      `machine code is not translated the way #6177 requires:\n  ${offenders.join("\n  ")}`,
  );
  // The component the page-shaped census missed must really be in the walk,
  // otherwise this guard can silently shrink back to pages-only.
  assert.ok(
    callSiteFiles.some((rel) => rel.endsWith("components/MembersTab.tsx")),
    "MembersTab.tsx is not in the call-site walk — the census is pages-only again, and a " +
      "component's errorKey would go unjudged (the measured hole this test exists for)",
  );
});

// ── THE RENDER HALF: the message was computed and DISCARDED ──────────────────
//
// WHAT BROKE (t_57a77d02 — measured in a browser, by driving the built page, not
// by reading it). The test above pins the OPTION. Nothing pinned what happens to
// the message the option produces, and the availability probe computed it and
// threw it away: `setErr` is the ONLY writer of the page's alert, and `r.error`
// is errorKey's ONLY consumer. Both were true of the shipped page, and together
// they made the option INERT — a page that keeps `errorKey` and drops `r.error`
// passed every assertion in this file.
//
// The consequence is a FALSE statement, not merely a less precise one: a refused
// workspace closed the box and the user read "The assistant is not available
// right now." while the sentence they could act on ("That workspace was not
// found, or it is not one you have access to.") was computed and discarded. The
// browser arm is e2e/chat-probe-400.spec.ts's named-code test; this guard is the
// cheap one that fails the moment the discard comes back.
test("the availability fetch's translated message is RENDERED, not computed and dropped", () => {
  const pageCode = codeOnly(page);

  // 1. The branch the message is built in must hand it to the alert's state.
  //    Located by the same two sites the scope test above slices between, so an
  //    edit above the probe cannot move this guard onto another call.
  const probeAt = pageCode.indexOf("const r = await runLoad(");
  assert.ok(
    probeAt !== -1,
    "app/chat/page.tsx no longer reads availability through runLoad — this guard cannot judge " +
      "the probe's failure branch and refuses to guess",
  );
  const branchFrom = pageCode.indexOf("if (!r.ok) {", probeAt);
  const branchEnd = pageCode.indexOf("setAvailable(r.data.available", branchFrom);
  assert.ok(
    branchFrom !== -1 && branchEnd > branchFrom,
    "could not delimit the availability probe's !r.ok branch — this guard refuses to slice a " +
      "branch it cannot bound",
  );
  const branch = pageCode.slice(branchFrom, branchEnd);
  assert.match(
    branch,
    /setErr\(\s*r\.error\s*\)/,
    `the availability probe's failure branch never reads r.error — the message runLoad built from ` +
      `the server's machine code (lib/apiErr, #6177) is computed and discarded, so every refusal ` +
      `renders the page's generic chat.unavailable instead of the reason. Branch was:\n${branch}`,
  );

  // 2. And the render must not print that sentence twice: the transcript's own
  //    generic line stands down while the alert carries the reason. Both states
  //    land in the alert (an unknown/absent code reaches it as this same
  //    chat.unavailable string, a named one translated), so an unconditional
  //    line here would duplicate the sentence one region over — which is a
  //    measured break, not a style point: the 5xx arm of
  //    e2e/chat-probe-400.spec.ts reads chat.unavailable by text and a second
  //    copy makes that locator ambiguous.
  const renderAt = pageCode.indexOf("{available === false &&");
  const renderEnd = pageCode.indexOf("msgs.length === 0", renderAt);
  assert.ok(
    renderAt !== -1 && renderEnd > renderAt,
    "could not delimit the transcript's unavailable line — this guard refuses to slice JSX it " +
      "cannot bound",
  );
  const render = pageCode.slice(renderAt, renderEnd);
  assert.match(
    render,
    /t\("chat\.unavailable"\)/,
    "the unavailable state no longer has a message of its own",
  );
  const condition = pageCode.slice(renderAt, pageCode.indexOf("<p", renderAt));
  assert.match(
    condition,
    /\berr\b/,
    `the transcript's generic line is rendered on \`available === false\` alone, so a failure's ` +
      `own sentence (the alert, set from r.error) is printed alongside it — the same words twice, ` +
      `and the generic claim stays on screen for a condition it is false of. Condition was:\n${condition}`,
  );

  // 3. The alert is the slot that sentence is rendered in — the one place
  //    r.error has ever been able to reach, and therefore the half a fix has to
  //    keep wired.
  assert.match(
    pageCode,
    /\{err && \(\s*<p role="alert"/,
    "the page no longer renders `err` in its role=\"alert\" — the availability probe's message " +
      "would have no consumer again, which is the defect this guard exists for",
  );
});

test("the nav entry and the page exist together, and the docs page is reachable", () => {
  assert.ok(
    shell.includes('href: "/chat"'),
    "the sidebar has no /chat entry — the feature would be unreachable",
  );
  assert.ok(
    shell.includes('"chat"'),
    'the Shell "active" union does not accept "chat"',
  );
  assert.ok(
    docsNav.includes('"/docs/chat"'),
    "the docs sidebar does not link /docs/chat — the page would be unreachable",
  );
});

// ── THE INLINE "?" LINK'S PAGE, READ AS THE USER READS IT (t_629da97d) ────────
//
// WHAT WAS WRONG. This guard and the creation guard below read the docs page with
// `read(DOCS_PAGE)` — its raw BYTES — and then made POSITIVE claims about prose the
// user reads ("the page names submit_feedback", "the page names purge_form"). A `//`
// comment satisfies a positive claim exactly as well as the rendered sentence does,
// and this page's own header comments already name both tools (page.tsx:9, :13-14).
// Measured on the merged tree (the guard itself, `node --test
// src/lib/chatPage.test.ts`, 35 tests): every RENDERED occurrence of the two names
// taken out of the page — the string literals AND the JSX text in the "Related:"
// line — with the comments left carrying them, kept this file GREEN (35 pass / 0
// fail), against a CONTROL (the same names deleted from the file entirely) that was
// RED and named this test. A guard satisfied by its own comment is satisfied by
// nothing.
//
// `codeOnly` is this repo's shared, JSX-text-aware blanker (lib/jsxSource.ts): a
// comment cannot satisfy a pin, while JSX text and string literals — where this
// page's copy actually lives — are kept byte for byte, so the guard still reads
// exactly what the page renders.
//
// THE RULE IS A PURE FUNCTION over the two sources, so the bite proof below runs the
// IDENTICAL reader against a mutated page: a proof that re-stated the checks would
// prove nothing about them (the file's doctrine — see capProseProblems).

/**
 * `src` with every occurrence of `name` that `codeOnly` KEEPS — JSX text and string
 * literals, i.e. what the page actually renders — replaced by `replacement`. An
 * occurrence the reader already blanks (a `//` line) is left where it is, so the RAW
 * source still carries the name while nothing the user reads does.
 *
 * This is the mutation the guard below exists to refuse, so it has to reproduce the
 * shape the page really has; the proof asserts BOTH halves rather than assuming them.
 */
function unrender(src: string, name: string, replacement: string): string {
  const code = codeOnly(src);
  let out = "";
  let from = 0;
  for (let at = src.indexOf(name); at !== -1; at = src.indexOf(name, from)) {
    out += src.slice(from, at);
    out += code.slice(at, at + name.length) === name ? replacement : name;
    from = at + name.length;
  }
  return out + src.slice(from);
}

/** Every problem with the boundary the inline "?" leads to, as sentences. */
function docLinkProblems(pageSrc: string, docsSrc: string): string[] {
  const out: string[] = [];
  const page = codeOnly(pageSrc);
  const docs = codeOnly(docsSrc);
  if (!page.includes('href="/docs/chat"')) {
    out.push('the assistant has no inline "?" doc link (AGENTS.md DOCUMENTATION PARITY step 4)');
  }
  // The page must say what the assistant CANNOT do — that is the feature.
  if (!/cannot change/i.test(docs) && !/cannot delete/i.test(docs)) {
    out.push(
      "the docs page does not state what the assistant cannot do — the boundary is the whole design",
    );
  }
  if (!docs.includes("submit_feedback")) {
    out.push("the docs page does not name the MCP tool behind the one write (submit_feedback)");
  }
  // Phase 2: the docs page must document the confirmation lane too — how a proposal
  // is confirmed, and that the assistant cannot confirm it itself.
  if (!docs.includes("propose_write")) {
    out.push(
      "the docs page does not name propose_write — the phase-2 tool would be undocumented " +
        "(AGENTS.md DOCUMENTATION PARITY step 2)",
    );
  }
  if (!/confirm/i.test(docs)) {
    out.push(
      "the docs page does not explain confirmation — the whole phase-2 design is that a human " +
        "confirms and the server executes",
    );
  }
  // The workspace scope is user-VISIBLE: a member of two workspaces can hit the
  // refusal, so it must be documented where the inline "?" leads, or the user reads
  // a 403 with no explanation of what to do about it.
  if (!/selected workspace/i.test(docs)) {
    out.push(
      "the docs page does not say a confirmation writes in the SELECTED workspace — a user who " +
        "hits the scope refusal has nothing telling them why, or that choosing a workspace is the fix",
    );
  }
  return out;
}

test('the inline "?" doc link points at the docs page that documents the boundary', () => {
  const problems = docLinkProblems(page, read(DOCS_PAGE));
  assert.deepEqual(
    problems,
    [],
    `the inline "?" doc link and the docs page it points at disagree:\n  - ${problems.join("\n  - ")}`,
  );
});

test('BITES: the "?" doc-link claims read RENDERED prose — a name left in a comment does not satisfy them', () => {
  const docsSrc = read(DOCS_PAGE);
  assert.deepEqual(
    docLinkProblems(page, docsSrc),
    [],
    "CONTROL: the shipped page and the docs page it points at are not green — a red below could " +
      "not be attributed to the mutation",
  );

  const buried = unrender(
    unrender(docsSrc, "submit_feedback", "the report tool"),
    "propose_write",
    "the proposal lane",
  );
  // THE MUTATION'S SHAPE, asserted rather than assumed: the raw read is still satisfied
  // (the page's own comments name both tools) while nothing the page renders does. That
  // is the state measured GREEN on this file before t_629da97d.
  for (const name of ["submit_feedback", "propose_write"]) {
    assert.ok(
      buried.includes(name),
      `the mutation left ${name} nowhere in the RAW source — it measures a DELETION, not a pin a ` +
        `comment can satisfy`,
    );
    assert.ok(
      !codeOnly(buried).includes(name),
      `the mutation still RENDERS ${name} — it would prove nothing about the reader`,
    );
  }

  const found = docLinkProblems(page, buried);
  for (const name of ["submit_feedback", "propose_write"]) {
    assert.ok(
      found.some((s) => s.includes(name)),
      `a page that names ${name} only in a COMMENT satisfies this guard again: the reader has gone ` +
        `back to raw source, so the name can be dropped from the prose a user reads while the guard ` +
        `stays green. Reported:\n  ${found.length ? found.join("\n  - ") : "(nothing)"}`,
    );
  }
  // The other pins are untouched by this mutation, so they must not be what reds here.
  assert.deepEqual(
    found.filter((s) => !s.includes("submit_feedback") && !s.includes("propose_write")),
    [],
    "the mutation reds a pin it did not touch — retarget it rather than reading the two above as " +
      "this reader's work",
  );
});

// ── THE CREATION HALF (t_e214d119) ───────────────────────────────────────────
//
// WHAT BROKE. #958 (t_804040be, Wan's C) made `upsert_form` proposable — the
// assistant may CREATE a form, subject to the human Confirm click, because the
// bin gives a mistaken creation an inverse. The CODE shipped and is live; the
// /docs/chat page was never updated, and its "What it may propose" section still
// said the set excluded "creating anything new". A user reading the page was told
// the opposite of what the product does, which is the one thing a docs page must
// never do.
//
// WHY THIS IS A SOURCE GUARD, cross-language, and not only a copy assertion. The
// page's claim ("a confirmed card can create a form") is only TRUE while
// `upsert_form` is in the Go `chatProposableTools`, and the page's OTHER claim
// ("purge_form is never proposable") is only true while it is NOT. A guard that
// read the page's prose alone would pass on a tree where the Go set moved out
// from under it — which is exactly the direction that shipped this defect. So the
// set is derived from `chat_tools.go` and both halves are asserted against it.
/**
 * The two sentences the creation guard pins by PATTERN, at module scope so the bite
 * proof below takes the very strings the guard looks for OFF the page (the idiom of
 * NUMBERLESS_CLAIMS): a proof keyed on its own copy of a sentence rots into a no-op
 * the day the sentence is legitimately reworded and this list is updated with it.
 */
const UPSERT_CREATE_SENTENCE =
  "The fourth creates: upsert_form is proposable, and a confirmed card CREATES a new form when it names none, or UPDATES the one it names.";
const PURGE_HUMAN_ONLY_SENTENCE =
  "The one step with no inverse is deleting a form for good, purge_form: it destroys the form and everything attached to it, and no restore brings it back. It is human-only and the assistant can never propose it";

/**
 * `src` with the string-literal body `sentence` moved onto a `//` line immediately
 * above the literal that renders it, and `replacement` left in the literal. The RAW
 * source still carries the sentence — the state that measured GREEN before
 * t_629da97d — while nothing the user reads does.
 *
 * ADJACENT rather than appended: the two pins below are patterns with a bounded
 * window, so a comment at the end of the file would not satisfy the raw read either,
 * and a mutation the raw reader already refuses measures a deletion rather than the
 * hole this guard is about.
 */
function sentenceInAComment(src: string, sentence: string, replacement: string): string {
  const at = src.indexOf(sentence);
  assert.ok(
    at !== -1,
    `the page no longer renders ${JSON.stringify(sentence)} — retarget this control rather than ` +
      `letting it mutate nothing`,
  );
  assert.ok(
    !sentence.includes("\n"),
    "the sentence spans a line break — this control is written for one string literal",
  );
  const lineStart = src.lastIndexOf("\n", at) + 1;
  const indent = /^\s*/.exec(src.slice(lineStart, at + 1))?.[0] ?? "";
  return (
    src.slice(0, lineStart) +
    `${indent}// ${sentence}\n` +
    src.slice(lineStart, at) +
    replacement +
    src.slice(at + sentence.length)
  );
}

/**
 * Every problem with the /docs/chat CREATION copy, as sentences.
 *
 * Pure over `docsSrc` so the bite proof below runs the IDENTICAL reader against a
 * page whose tool names live only in a comment. The page is read through `codeOnly`
 * (t_629da97d — see the note above docLinkProblems): every claim here is about prose
 * the user reads, and this page's own comments name tools too.
 */
function creationDocsProblems(docsSrc: string): string[] {
  const out: string[] = [];
  const docs = codeOnly(docsSrc);

  // Vacuity, in the file's idiom: prove this is the page that carries the section
  // before either claim is believed.
  if (!docs.includes("What it may propose")) {
    out.push(
      `${DOCS_PAGE} is not the chat docs page this guard means to judge (its "What it may ` +
        `propose" section is missing)`,
    );
  }

  // ── 1. The sentence that shipped the contradiction cannot come back. ──
  if (/creating anything new/i.test(docs)) {
    out.push(
      'the docs page claims the proposable set excludes "creating anything new" again — #958 ' +
        "(t_804040be) makes upsert_form proposable, so that sentence is the shipped defect",
    );
  }
  // The surrounding sentence it lived in is gone too: the old copy framed the whole
  // set as field edits with "no new object created", which is the same false claim
  // one clause longer.
  if (/no new object created/i.test(docs)) {
    out.push(
      'the docs page still describes the set as field edits with "no new object created" — a ' +
        "confirmed upsert_form card CAN create a new form, so the framing is false",
    );
  }

  // ── 2. The creation is stated, and it says WHICH HALF. ──
  // `upsert_form` is BOTH a create and an update, so a page that only said "edits a form"
  // would misdescribe a card that is about to create one. The card's own summary states
  // both halves for exactly this reason — and since t_30ffa28a that sentence is a
  // DICTIONARY entry (proposalDocs["upsert_form"].summaryKey →
  // chat.proposal.summary.upsert_form; src/lib/chatProposal.test.ts fails if the key has
  // no entry), so the English page has to say the same thing.
  if (!docs.includes("upsert_form")) {
    out.push(
      "the docs page no longer names upsert_form — the proposable creation would be " +
        'undocumented, and the page is the inline "?" the chat panel points at (AGENTS.md ' +
        "DOCUMENTATION PARITY)",
    );
  }
  if (!/upsert_form[\s\S]{0,200}(CREATES a new form|creates a new form)[\s\S]{0,120}(UPDATES|updates)/.test(docs)) {
    out.push(
      "the docs page names upsert_form but does not say it CREATES a new form (when it names none) " +
        "as well as UPDATES one it names — the two halves are the difference the confirmation is " +
        'FOR, and the Go card summary states both (proposal.go proposalDocs["upsert_form"])',
    );
  }

  // ── 3. The bin is named — it is the whole reason the creation is admissible. ──
  // Wan's decision was "the assistant may take it as far as the bin; only a person
  // purges". A page that allowed the creation but never mentioned the bin would leave
  // the user without the recovery path the decision rests on.
  if (!/(to the trash|the trash|from the trash)/i.test(docs)) {
    out.push(
      "the docs page allows a created form but never names the trash/bin — the recoverability of " +
        "a mistaken creation is the entire justification for letting the assistant create one " +
        '(Wan, t_804040be: "assistant boleh buat sampai masuk ke bin")',
    );
  }

  // ── 4. The permanent step is stated as human-only. ──
  if (!docs.includes("purge_form")) {
    out.push(
      "the docs page never names purge_form — the ONE step with no inverse is the thing a user " +
        "confirming a created form most needs to be told about",
    );
  }
  if (!/purge_form[\s\S]{0,200}(never propose|cannot propose|never be proposed|is not proposable)/i.test(docs)) {
    out.push(
      "the docs page names purge_form but does not say the assistant can never propose it — a " +
        "reader would be left believing a permanent delete is one click away in the chat, which is " +
        'the opposite of Wan\'s rule ("Manusia sahaja boleh purge kekal")',
    );
  }

  // ── 5. The bin is the PERSON's action, not the assistant's. ──
  // The creation above is admissible because a form is BINNED, not destroyed — but the bin
  // itself is not an assistant capability. delete_form/restore_form are not proposable AND
  // not in the chat's read allowlist, so the chat has no lane to bin or restore a form at
  // all; only purge_form is called out as human-only, yet it is not the only step that is.
  // The rework copy shipped the same class of defect it set out to fix, one claim over:
  // "the assistant can take a form as far as the trash" credited the model with an action
  // the server gives it no route to take.
  const credited = assistantBinClaims(docs);
  if (credited.length > 0) {
    out.push(
      "the docs page credits the ASSISTANT with binning, trashing, restoring or deleting a form — " +
        "delete_form/restore_form are not proposable and not in the chat's read allowlist, so the " +
        "bin is the person's action on the forms page, never the assistant's. Offending phrasing(s): " +
        `${JSON.stringify(credited)}. (The shipped rework copy said "the assistant can take a form ` +
        'as far as the trash"; the infinitive, passive and bare-verb re-wordings of that claim are ' +
        "the same defect one phrasing over.)",
    );
  }
  // Positive control: the page must actually attribute the bin to the person rather than
  // dropping the recovery path that justifies the creation. A copy that simply deleted the
  // false sentence would pass a negative-only guard while leaving the reader with no idea
  // who bins the form.
  if (
    !/bin belongs to (you|the person|the owner)|not the assistant[\s\S]{0,60}(forms page|trash)|you[\s\S]{0,40}move[s]? it to the trash/i.test(
      docs,
    )
  ) {
    out.push(
      "the docs page never says the BIN is the person's action — a reader told the assistant may " +
        "create a form needs to know who can bin it, and the page must not leave that to the " +
        'assistant (Wan, t_804040be: "assistant boleh buat sampai masuk ke bin"; the bin is the ' +
        "person's steps on the forms page)",
    );
  }
  return out;
}

test("the docs say the assistant may CREATE a form, and that a permanent purge stays human-only", () => {
  // The prose half, through the shared reader: `codeOnly` inside creationDocsProblems,
  // and a PURE rule so the bite proof below drives the IDENTICAL reader (t_629da97d).
  const problems = creationDocsProblems(read(DOCS_PAGE));
  assert.deepEqual(
    problems,
    [],
    `the /docs/chat creation copy and the proposable set disagree:\n  - ${problems.join("\n  - ")}`,
  );

  // ── the Go half: the page's claims are only TRUE while the set says so ──
  const setMatch = goChatTools.match(/var chatProposableTools\s*=\s*\[\]string\{([\s\S]*?)\}/);
  assert.ok(
    setMatch,
    "chat_tools.go no longer declares `var chatProposableTools = []string{…}` as a parseable " +
      "literal — this guard reads the set out of it to judge whether the docs' claims are true, " +
      "and refuses to guess",
  );
  const proposable = [...setMatch![1].matchAll(/"([a-z0-9_]+)"/g)].map((m) => m[1]);
  assert.ok(
    proposable.length >= 3,
    `parsed ${proposable.length} name(s) out of chatProposableTools — the extractor has drifted ` +
      `from the literal's shape, so every claim below would be judged against nothing`,
  );
  // The claim is only true while the tool is really proposable — read from Go, not
  // asserted from the page's own words.
  assert.ok(
    proposable.includes("upsert_form"),
    `the docs page tells the user a confirmed card can create a form, but upsert_form is NOT in ` +
      `chat_tools.go's chatProposableTools (${proposable.join(", ")}) — the page and the server ` +
      `disagree, and the page is wrong`,
  );
  // And the page's claim is true: purge_form must NOT be proposable.
  assert.ok(
    !proposable.includes("purge_form"),
    "chat_tools.go's chatProposableTools contains purge_form — the docs page says the assistant " +
      "can never propose a permanent delete, and it now can. Wan's rule: the assistant takes a " +
      "form as far as the bin; only a person purges (chat_tools_guard_test.go's " +
      "TestChatNeverProposesAPermanentDelete pins the same rule from the Go side)",
  );

  // ── the read allowlist: the chat has no lane to reach the bin at all. ──
  // The set is read from Go so the copy cannot drift in EITHER direction.
  const readSetMatch = goChatTools.match(/var chatReadTools\s*=\s*\[\]string\{([\s\S]*?)\}/);
  assert.ok(
    readSetMatch,
    "chat_tools.go no longer declares `var chatReadTools = []string{…}` as a parseable literal — " +
      "this guard reads the chat's read allowlist out of it to prove the assistant cannot even " +
      "reach the bin, and refuses to guess",
  );
  const readTools = [...readSetMatch![1].matchAll(/"([a-z0-9_]+)"/g)].map((m) => m[1]);
  for (const binTool of ["delete_form", "restore_form"]) {
    assert.ok(
      !proposable.includes(binTool),
      `chat_tools.go's chatProposableTools contains ${binTool} — the docs page must not let the ` +
        `user believe the assistant can bin or restore a form (set: ${proposable.join(", ")})`,
    );
    assert.ok(
      !readTools.includes(binTool),
      `chat_tools.go's chatReadTools contains ${binTool} — the chat can dispatch a bin/restore ` +
        `while the docs page credits that action to the person, so one of the two is wrong ` +
        `(read allowlist: ${readTools.join(", ")})`,
    );
  }
});

// ── THE CREATION GUARD'S OWN BITE (t_629da97d) ───────────────────────────────
//
// The same hole as the "?" guard's, in the reader t_4912a9bd did not own: this half
// read `read(DOCS_PAGE)` — the page's raw BYTES — and then claimed positively that the
// page NAMES upsert_form and purge_form, and that it says what each one does. A `//`
// line satisfies a positive claim as well as the rendered sentence does, and the pins
// here are PATTERNS with a bounded window, so the comment has to sit where the sentence
// was, not at the end of the file.
test('BITES: the creation guard reads RENDERED docs prose — a tool named only in a comment does not satisfy it', () => {
  const shipped = read(DOCS_PAGE);
  assert.deepEqual(
    creationDocsProblems(shipped),
    [],
    "CONTROL: the shipped page is not green — a red below could not be attributed to the mutation",
  );

  const buried = sentenceInAComment(
    sentenceInAComment(
      shipped,
      UPSERT_CREATE_SENTENCE,
      "The fourth creates a new form when it names none, or updates the one it names.",
    ),
    PURGE_HUMAN_ONLY_SENTENCE,
    "The one step with no inverse is deleting a form for good: it destroys the form and everything " +
      "attached to it, and no restore brings it back. It is human-only and the assistant can never " +
      "propose it",
  );

  const pins: Array<[string, RegExp]> = [
    [
      "upsert_form",
      /upsert_form[\s\S]{0,200}(CREATES a new form|creates a new form)[\s\S]{0,120}(UPDATES|updates)/,
    ],
    ["purge_form", /purge_form[\s\S]{0,200}(never propose|cannot propose|never be proposed|is not proposable)/i],
  ];
  // THE MUTATION'S SHAPE, asserted rather than assumed: the RAW source still satisfies
  // both pins, and nothing the page RENDERS carries either name.
  for (const [name, pin] of pins) {
    assert.ok(
      buried.includes(name) && pin.test(buried),
      `the mutation must leave the RAW source satisfying the ${name} pin — otherwise it measures a ` +
        `DELETION rather than a name a comment can carry`,
    );
    assert.ok(
      !codeOnly(buried).includes(name),
      `the mutation still RENDERS ${name} — it would prove nothing about the reader`,
    );
  }

  const found = creationDocsProblems(buried);
  for (const [name] of pins) {
    assert.ok(
      found.some((s) => s.includes(name)),
      `a page that names ${name} only in a COMMENT satisfies the creation guard again: the reader ` +
        `has gone back to raw source, so the tool can be dropped from the prose a user reads while ` +
        `the guard stays green. Reported:\n  ${found.length ? found.join("\n  - ") : "(nothing)"}`,
    );
  }
  // The other pins are untouched by this mutation, so they must not be what reds here.
  assert.deepEqual(
    found.filter((s) => !s.includes("upsert_form") && !s.includes("purge_form")),
    [],
    "the mutation reds a pin it did not touch — retarget it rather than reading the two above as " +
      "this reader's work",
  );
});

// ── THE PROSE ARM'S OWN BITE PROOF (t_48daca7f; widened in t_5bdeda83, t_fa053255
//    and t_b3877cfb) ─────────────────────────────────────────────────────────────
//
// The rework's prose arm shipped GREEN on the three re-wordings it existed to
// catch, because nothing in the suite ever proved it could fail on anything but
// the one sentence it was written beside. A guard whose failure mode is never
// observed rots with every gate green — which is exactly how this defect shipped,
// on the paragraph that had just been "fixed". So the IDENTICAL scan is run here
// against the SHIPPED page with one phrasal injection at the anchor the QA card
// names (docs page line 69, right after "A creation is the one shape the field-edit
// rule does not cover,"), and every shape the previous regex missed — the three
// from t_48daca7f, the seven the follow-up t_5bdeda83 found still GREEN, the
// eighteen the follow-up t_fa053255 found still GREEN, and the five the follow-up
// t_b3877cfb found still GREEN under the ten-shape scan — must go RED, as must the
// shipped defect sentence it did catch. Nothing on disk is written: the mutation is
// in memory, on the real page source read at module load.
test("BITES: the assistant-credits-the-bin prose arm catches the infinitive, the passive, the bare verb, the perfect, the future, the relative clause, both genitive orders, the gerund, both appositive variants, the it-cleft, the periphrastic passive, the nominal, the light verb + gerund, the copular predication, the topicalised cleft, the reversed nominal, the non-restrictive relative, the pro-verb across a clause boundary, the claim reported inside an information verb's clause, the bin gerund such a verb reports and a claim behind an auxiliary chain inside the reported clause", () => {
  // Through `codeOnly` like the creation guard this proves (t_629da97d): the anchor the
  // injections land on has to be text the page RENDERS, or the proof would be measuring
  // the raw bytes while the guard reads the prose.
  const shipped = codeOnly(read(DOCS_PAGE));
  const anchor = "A creation is the one shape the field-edit rule does not cover,";
  // Vacuity: if the anchor is gone the injections below land nowhere and this
  // proof would pass while testing nothing.
  assert.ok(
    shipped.includes(anchor),
    `the docs page no longer contains the injection anchor (${JSON.stringify(anchor)}) — this ` +
      `proof would inject into a string that does not contain it and assert nothing`,
  );

  // The three shapes the actor-adjacency regex missed (A/B/C in the t_48daca7f QA
  // card), the seven the follow-up t_5bdeda83 measured as still GREEN, and the
  // shipped defect sentence the old regex did catch. Each must be found by the scan
  // that now runs in the test above. The residual seven carry all four bin verbs
  // (bin/trash/restore/delete) across their shapes.
  const shapes: Record<string, string> = {
    "infinitive (A)": "You can ask the assistant to bin a form.",
    "passive (B)": "A form can be binned by the assistant.",
    "bare verb (C)": "The assistant bins a form.",
    "shipped defect": "The assistant can take a form as far as the trash.",
    "infinitive + object (D)": "You can ask the assistant to move a form to the trash.",
    // t_5bdeda83 — the residue the four shapes above still missed.
    "perfect (E)": "The assistant has binned a form.",
    "perfect + movement (F)": "The assistant has moved a form to the trash.",
    "periphrastic future (G)": "The assistant is going to delete a form.",
    "relative clause (H)": "The assistant is the one who bins a form.",
    "possessive genitive (I)": "Binning a form is the assistant's job.",
    "fronted gerund (J)": "Restoring a form is something the assistant does.",
    "appositive (K)": "The assistant, not you, deletes the form.",
    "perfect · trash (L)": "The assistant has trashed a form.",
    "perfect · restore (M)": "The assistant has restored a form.",
    "perfect · delete (N)": "The assistant has deleted a form.",
    "future · trash (O)": "The assistant is going to trash a form.",
    "future · restore (P)": "The assistant is going to restore a form.",
    "relative · trash (Q)": "The assistant is the one who trashes a form.",
    "relative · restore (R)": "The assistant is the one who restores a form.",
    "relative · delete (S)": "The assistant is the one who deletes a form.",
    "genitive · trash (T)": "Trashing a form is the assistant's job.",
    "genitive · restore (U)": "Restoring a form is the assistant's job.",
    "genitive · delete (V)": "Deleting a form is the assistant's job.",
    "gerund · trash (W)": "Trashing a form is something the assistant does.",
    "gerund · delete (X)": "Deleting a form is something the assistant does.",
    "appositive · bin (Y)": "The assistant, not you, bins the form.",
    "appositive · trash (Z)": "The assistant, not you, trashes the form.",
    "appositive · restore (AA)": "The assistant, not you, restores the form.",
    // t_fa053255 — the residue the six shapes above still missed, each over its
    // four bin verbs: the genitive SUBJECT, the it-cleft, the periphrastic passive,
    // the ", rather than you," appositive and the NOMINAL (agent noun).
    "genitive subject · bin (AB)": "The assistant's job is to bin a form.",
    "genitive subject · trash (AC)": "The assistant's job is to trash a form.",
    "genitive subject · restore (AD)": "The assistant's job is to restore a form.",
    "genitive subject · delete (AE)": "The assistant's job is to delete a form.",
    "it-cleft · bin (AF)": "It is the assistant who bins a form.",
    "it-cleft · trash (AG)": "It is the assistant who trashes a form.",
    "it-cleft · restore (AH)": "It is the assistant who restores a form.",
    "it-cleft · delete (AI)": "It is the assistant who deletes a form.",
    "it-cleft · contraction (AJ)": "It's the assistant who deletes a form.",
    "periphrastic passive · bin (AK)": "Binning a form is done by the assistant.",
    "periphrastic passive · trash (AL)": "Trashing a form is done by the assistant.",
    "periphrastic passive · restore (AM)": "Restoring a form is done by the assistant.",
    "periphrastic passive · delete (AN)": "Deleting a form is done by the assistant.",
    "appositive rather-than · bin (AO)": "The assistant, rather than you, bins a form.",
    "appositive rather-than · trash (AP)": "The assistant, rather than you, trashes a form.",
    "appositive rather-than · restore (AQ)": "The assistant, rather than you, restores a form.",
    "appositive rather-than · delete (AR)": "The assistant, rather than you, deletes a form.",
    "nominal · bin (AS)": "The assistant is the binner of a form.",
    "nominal · trash (AT)": "The assistant is the trasher of a form.",
    "nominal · restore (AU)": "The assistant is the restorer of a form.",
    "nominal · delete (AV)": "The assistant is the deleter of a form.",
    // t_b3877cfb — the residue the ten shapes above still missed, each now closed by
    // the STRUCTURAL rule rather than by a shape: the light verb + gerund (including
    // "is responsible for"/"is tasked with"/"is in charge of"), the copular
    // predication, the topicalised cleft, the reversed nominal (agent noun as subject)
    // and the non-restrictive relative clause. Every one of these was measured GREEN
    // under the previous (t_fa053255 head) scan.
    "light verb + gerund · bin (AW)": "The assistant handles the binning.",
    "light verb + gerund · trash (AX)": "The assistant handles the trashing.",
    "light verb + gerund · restore (AY)": "The assistant handles the restoring.",
    "light verb + gerund · delete (AZ)": "The assistant handles the deleting.",
    "light verb + gerund · responsible (BA)": "The assistant is responsible for binning a form.",
    "light verb + gerund · tasked (BB)": "The assistant is tasked with trashing a form.",
    "light verb + gerund · in charge (BC)": "The assistant is in charge of deleting a form.",
    "copular predication · bin (BD)": "Binning is what the assistant is for.",
    "copular predication · trash (BE)": "Trashing is what the assistant is for.",
    "copular predication · restore (BF)": "Restoring is what the assistant is for.",
    "copular predication · delete (BG)": "Deleting is what the assistant is for.",
    "topicalised cleft · bin (BH)": "It is to the assistant that the form is binned.",
    "topicalised cleft · trash (BI)": "It is to the assistant that a form is trashed.",
    "topicalised cleft · restore (BJ)": "It is to the assistant that a form is restored.",
    "topicalised cleft · delete (BK)": "It is to the assistant that a form is deleted.",
    "reversed nominal · bin (BL)": "The binner of a form is the assistant.",
    "reversed nominal · trash (BM)": "The trasher of a form is the assistant.",
    "reversed nominal · restore (BN)": "The restorer of a form is the assistant.",
    "reversed nominal · delete (BO)": "The deleter of a form is the assistant.",
    "non-restrictive relative · bin (BP)": "The assistant, whose job is to bin a form, does the rest.",
    "non-restrictive relative · trash (BQ)": "The assistant, whose job is to trash a form, handles it.",
    "non-restrictive relative · restore (BR)": "The assistant, whose job is to restore a form, does the rest.",
    "non-restrictive relative · delete (BS)": "The assistant, whose job is to delete a form, does the rest.",
    // Position (E) — the bin verb's SUBJECT is the assistant, the verb moved later.
    // (E) also catches the FUSED relative, where the bin verb has no subject of its
    // own and the assistant is what the whole clause predicates: "who bins a form IS
    // the assistant".
    "bin-verb subject · bin (BT)": "Binning a form falls to the assistant.",
    "bin-verb subject · trash (BU)": "Trashing a form falls to the assistant.",
    "bin-verb subject · restore (BV)": "Restoring a form falls to the assistant.",
    "bin-verb subject · delete (BW)": "Deleting a form falls to the assistant.",
    "fused relative · bin (BX)": "Who bins a form is the assistant.",
    "fused relative · trash (BY)": "Who trashes a form is the assistant.",
    "fused relative · restore (BZ)": "Who restores a form is the assistant.",
    "fused relative · delete (CA)": "Who deletes a form is the assistant.",
    // t_8ffff05e — the PRO-VERB across a clause boundary. This shape was caught by the
    // enumerated scan (its window crossed the em-dash) and LOST by the structural split
    // alone, i.e. the split REGRESSED it; the pro-verb fold above restores it. The three
    // anaphor/verb variants the QA card names, plus the same pro-verb at the two other
    // boundaries the old window crossed (a semicolon, a conjunction).
    "pro-verb across · does that (CB)": "Binning a form — the assistant does that.",
    "pro-verb across · does it (CC)": "Binning a form — the assistant does it.",
    "pro-verb across · handles that (CD)": "Binning a form — the assistant handles that.",
    "pro-verb across · semicolon (CE)": "Binning a form; the assistant does that.",
    "pro-verb across · conjunction (CF)": "Binning a form is easy, and the assistant does that.",
    // t_9717fec1 — the reported claim. Every one of these is a genuine claim (the
    // assistant IS the binner) that sits INSIDE the clause an information verb
    // reports, and every one was GREEN at the fix 09e2e6f0 because the information-verb
    // guard read the assistant's own verb alone and stood position (A) down. The head
    // scan 3542c039 caught all seven; the guard now reads the reported clause too
    // (`assistantBinsInsideReport`) and catches them again. The seven are pinned here,
    // not merely named in the header, because removing that one clause in the guard is
    // the mutation that reds them — see the mutation note after this map.
    "reported claim · tells/object (CG)": "The assistant tells you it bins a form.",
    "reported claim · reports/that (CH)": "The assistant reports that it deleted a form.",
    "reported claim · says/modal (CI)": "The assistant says it can trash a form.",
    "reported claim · shows/you+modal (CJ)": "The assistant shows you it can delete a form.",
    "reported claim · tells/perfect (CK)": "The assistant tells you it has binned a form.",
    "reported claim · explains/that (CL)": "The assistant explains that it restores a form.",
    "reported claim · mentions/object (CM)": "The assistant mentions it deletes forms.",
    // t_d7178bff — the SAME reported-claim reader (t_9717fec1's seven rows above), with an
    // auxiliary CHAIN in the reported clause's verb phrase. The reader accepted AT MOST ONE
    // auxiliary between the clause's subject pronoun and the bin verb, so a claim whose verb
    // phrase carries a chain had no match at all: after the single auxiliary the next token is
    // another auxiliary ("been"/"be"/"going"/"about"), not the bin verb, so the reader returned
    // false and a genuine claim was read as a report — the opposite direction from the parent
    // card's false positive, at this call site. The single-auxiliary shapes above were caught
    // and hid it. These seven are the shapes the card names, one per chain; the mutation that
    // reds them is reverting the chain group to its single-auxiliary form (see the note after
    // this map).
    "chain claim · has been binning (DQ)": "The assistant tells you it has been binning a form.",
    "chain claim · will be deleting (DR)": "The assistant tells you it will be deleting a form.",
    "chain claim · is going to delete (DS)": "The assistant tells you it is going to delete a form.",
    "chain claim · has been trashing (DT)": "The assistant reports that it has been trashing forms.",
    "chain claim · has been restoring (DU)": "The assistant says it has been restoring a form.",
    "chain claim · had been deleting (DV)": "The assistant mentions it had been deleting forms.",
    "chain claim · is about to bin (DW)": "The assistant explains that it is about to bin a form.",
    // t_7b69a77a — the bin GERUND the information verb reports, one position over from
    // the seven above. Every one of these is a genuine claim (the assistant IS the
    // deleter) with no assistant pronoun in the reported clause, and every one was GREEN
    // at the fix 09e2e6f0 (and at this branch before the gerund rule). The head scan
    // 3542c039 caught all five; `reportedGerundIsSubject` now resolves the gerund's role
    // and catches them again. The five are pinned here because removing the gerund clause
    // from `assistantBinsInsideReport` is the mutation that reds them.
    "gerund object · reports (CN)": "The assistant reports deleting a form.",
    "gerund object · mentions (CO)": "The assistant mentions deleting forms.",
    "gerund object · describes (CP)": "The assistant describes deleting a form.",
    "gerund object · reports-from (CQ)": "The assistant reports deleting a form from the forms page.",
    "gerund object · mentions-restore (CR)": "The assistant mentions restoring a form.",
    // t_31293346 — the SAME reported claims with the information verb's TENSE changed
    // (past / perfect / progressive), the class the widening adds to `INFO_VERB`. These
    // are genuine claims and must stay RED; they are pinned here because they are the
    // positive control for the widening's OTHER direction: a mutation that made
    // `assistantBinsInsideReport` stand the reported clause down unconditionally (or
    // that read the new `-ing` forms as an ACTION verb) turns these green, and nothing
    // else in the map covers the new verb forms. CS-CU put the assistant's pronoun back
    // in the bin verb's subject position (the t_9717fec1 shape); CV-CW are the bin
    // gerund the verb reports (the t_7b69a77a shape).
    "tense claim · told/pronoun (CS)": "The assistant told you it bins a form.",
    "tense claim · is telling/pronoun (CT)": "The assistant is telling you it can trash a form.",
    "tense claim · has told/pronoun (CU)": "The assistant has told you it deleted a form.",
    "tense claim · told/gerund-object (CV)": "The assistant told you deleting a form.",
    "tense claim · is reporting/gerund-object (CW)": "The assistant is reporting deleting a form.",
    // t_82129fb4 — the QUANTIFIER-DETERMINER object. The gerund's object opens with a
    // quantifier (every|each|all|both|no|many|several|most|a few|one|the whole|an entire),
    // which `DET` did not list, so the bare `[a-z]+` ate the QUANTIFIER as if it were the
    // object noun, the rest of the noun phrase was left as a phantom predication, and the
    // rule read the gerund as a SUBJECT and stood the claim down — a false negative (the
    // claim went GREEN). `DET` now carries the quantifier class; the rows below are the
    // claim side of it, and dropping that class from `DET` returns exactly these to GREEN
    // (see the bite proof after this map). Two shapes are covered: a STANDALONE quantifier
    // and the article+quantifier compound ("the whole", "an entire", "a few"), whose
    // compound alternative must precede the bare article in `DET` or the defect reappears
    // one token over.
    "gerund object · quantifier-every (CX)": "The assistant reports deleting every form.",
    "gerund object · quantifier-all (CY)": "The assistant reports binning all forms.",
    "gerund object · quantifier-no (CZ)": "The assistant mentions trashing no form.",
    "gerund object · quantifier-a-few (DA)": "The assistant describes restoring a few forms.",
    "gerund object · quantifier-whole (DB)": "The assistant explains deleting the whole form.",
    "gerund object · quantifier-entire (DC)": "The assistant reports binning an entire form.",
    // t_6ad53e35 — the reported clause's CONTRACTED SUBJECT. The reader read the subject as
    // a BARE pronoun (or the noun) and then required whitespace before the verb phrase, so a
    // subject that contracts the pronoun with its copula/auxiliary into ONE token (`it's`,
    // `they're`, `he's`, `we're`, `you're`) matched nothing and the claim was read as a
    // report — a false negative. These rows are the contracted twins of the reported-claim
    // rows CG-CM (and, for the last four, of the t_d7178bff `chain claim` class, since a
    // contraction can open a chain: `it's been …`, `they've been …`). The mutation that reds
    // them is reverting the reader's subject to the bare alternation; see the note after
    // this map. A contraction is ALSO the auxiliary (`it's` = `it is`/`it has`), so the
    // contracted PASSIVES are pinned as report controls in the array below — they are what
    // reds if the contraction's core is not folded onto the chain the copula test reads.
    "contracted subject · it's binning (DH)": "The assistant tells you it's binning a form.",
    "contracted subject · they're binning (DI)": "The assistant says they're binning forms.",
    "contracted subject · he's deleting (DJ)": "The assistant reports that he's deleting a form.",
    "contracted subject · she's trashing (DK)": "The assistant explains that she's trashing a form.",
    "contracted subject · we're restoring (DL)": "The assistant mentions we're restoring a form.",
    "contracted subject · it's been binning (DM)": "The assistant tells you it's been binning a form.",
    "contracted subject · they've been trashing (DN)": "The assistant says they've been trashing forms.",
    "contracted subject · he's been deleting (DO)": "The assistant reports that he's been deleting forms.",
    "contracted subject · we've been restoring (DP)": "The assistant explains that we've been restoring a form.",
    // t_b0bf328c — the SAME auxiliary chain on the CLAIM side, i.e. the catches the chain
    // strip must NOT lose. A chain is not only a report's shape: an information verb behind
    // its auxiliaries can carry a BARE BIN INFINITIVE ("has been asked TO BIN"), and there
    // the assistant IS the one who would bin, which position (A)'s infinitive shape already
    // catches from the other end ("ask the assistant to bin"). These were RED on main (the
    // chain read the auxiliary, no information verb) and would green the moment the chain
    // strip recognised the verb without the companion check (`binInfinitiveAfterVerb`) — so
    // they are this card's claim-side bite against exactly that over-fix. The first two were
    // RED on main and on the parent; the third, "is told to bin", is a NEW catch the chain
    // strip adds. RENUMBERED EA/EB/EC by the re-cut onto `main` (t_1130759a): as cut against
    // the #1074 branch these rows carried CX/CY/CZ, which #1077 (t_82129fb4) had already
    // taken for the quantifier rows above — the labels are keys of this map, so a repeat is
    // a duplicate key, not a cosmetic clash. The full label is the key; the short code is
    // only a handle for the diff, so renumbering changes nothing but the handle. (The
    // contracted-subject rows above are #1081's (t_6ad53e35, DH-DP); this card's are EA-EC.)
    "chain claim · asked/infinitive (EA)": "The assistant has been asked to bin a form.",
    "chain claim · told/infinitive (EB)": "The assistant can be told to bin a form.",
    "chain claim · told/plain (EC)": "The assistant is told to bin a form.",
    // t_7cae27c8 — the SAME contracted-subject class one contraction over: `'d` (had OR
    // would) and `'ll` (will). A modal contraction carries a modal or `had`, not the copula,
    // so it matched neither the alternation nor the core map, and the claim was read as a
    // report — measured on the live tree: the contracted form GREEN while its uncontracted
    // twin (`he HAD been deleting`) is RED. EJ is the EMPTY-chain shape (no auxiliary
    // between the contraction and the verb), which the folded core alone decides; that the
    // ambiguity of `'d` (had OR would) cannot move it is pinned by this card's own test —
    // neither reading is a copula, so the ACTIVE reading is the same under either. These
    // rows bite like DH-DP: reverting the reader's subject to the bare alternation returns
    // them to GREEN while the rest of the map stays green. RENUMBERED ED-EJ by the re-cut
    // onto `main` (t_1db00e1d): DQ-DW belongs to the `chain claim` class and this card's own
    // cut had claimed the same seven slots, so one side had to move — see the note at the
    // map's tail, which says which class owns which handles.
    "contracted 'd subject · he'd deleting (ED)": "The assistant tells you he'd been deleting forms.",
    "contracted 'd subject · it'd binning (EE)": "The assistant tells you it'd been binning a form.",
    "contracted 'd subject · we'd restoring (EF)": "The assistant mentions we'd been restoring a form.",
    "contracted 'll subject · it'll binning (EG)": "The assistant tells you it'll be binning a form.",
    "contracted 'll subject · they'll deleting (EH)": "The assistant says they'll be deleting forms.",
    "contracted 'll subject · she'll trashing (EI)": "The assistant explains that she'll be trashing a form.",
    "contracted 'd subject · it'd bare-verb (EJ)": "The assistant reports that it'd bin a form.",
  };

  const missed: string[] = [];
  for (const [name, sentence] of Object.entries(shapes)) {
    // A string pattern replaces only the first occurrence; the anchor is unique.
    const mutated = shipped.replace(anchor, `"${sentence} ${anchor}`);
    // Prove the mutation landed, or the shape is judged against the pristine page.
    assert.ok(
      mutated.includes(sentence),
      `the ${name} injection did not land on the page — this case asserts nothing`,
    );
    if (assistantBinClaims(mutated).length === 0) missed.push(`${name}: ${JSON.stringify(sentence)}`);
  }
  assert.deepEqual(
    missed,
    [],
    `the prose arm did not go RED on these phrasings, so a copy that credits the assistant with ` +
      `the bin would ship GREEN (the defect this card fixes):\n  ${missed.join("\n  ")}`,
  );

  // THE BITE, measured rather than asserted. The `reported claim` cases (CG-CM) are the
  // seven t_9717fec1 sentences, and the mutation that reds them is the one clause that
  // card added to position (A): dropping `|| assistantBinsInsideReport(m[0], s)` from the
  // guard restores the fix 09e2e6f0's behaviour and all seven come back GREEN — measured
  // on this file, the whole BITES map's other 60-odd cases staying green and ONLY the
  // seven reported-claim rows entering `missed`. t_7b69a77a's five `gerund object` rows
  // (CN-CR) bite the same way: deleting the gerund clause from `assistantBinsInsideReport`
  // (the `BIN_GERUND` test + `reportedGerundIsSubject` call) returns all five to GREEN
  // while the whole rest of the map stays green. In the other direction, widening
  // `assistantBinsInsideReport` to "an information verb followed by a bin anywhere"
  // reds the report controls below (`assistant can tell you where the trash`), and
  // resolving the reported gerund as an object UNCONDITIONALLY — dropping the
  // `reportedGerundIsSubject` predicate reading — reds the subject-gerund controls
  // (`assistant says binning a form is easy`, `… tells you restoring a form is
  // possible`), i.e. the predicate reading is what keeps the claim from swallowing a
  // report that happens to use the same gerund. All runs were done by hand on this file;
  // they are why these rows and controls are pinned rather than named in prose.
  //
  // t_6ad53e35's nine `contracted subject` rows (DH-DP) bite on this card's axis: reverting
  // the reported-clause reader's subject to the bare alternation (`it|they|he|she|the
  // assistant`, no contraction arm) returns exactly those nine to GREEN while the whole rest
  // of the map stays green — the bare-subject rows CG-CM are the controls that hid the gap.
  // In the other direction, NOT folding the contraction's lexical core onto the chain the
  // copula test reads (`COPULA_AUX.test(chain)` with `chain` built from the whitespace group
  // alone) reds the contracted PASSIVE report controls added to the array below
  // (`it's binned`, `they're trashed`) and nothing else: the contraction IS the auxiliary,
  // so the passive/progressive distinction has to read it. Both mutations were run by hand on
  // this file.
  //
  // This reader also carries the bare-subject arm's auxiliary group as a CHAIN (t_d7178bff's
  // change; the two are not independent for the lines this reader shares). The chain is what
  // lets the card's own contracted-perfect rows (`it's been binning`) read, and the bare
  // chain claims of #1078 are therefore red here too — its `chain claim` rows are pinned in
  // the same map at this tail, numbered DQ-DW so they collide with neither the contracted
  // rows' DH-DP above nor the quantifier rows' DA-DC.
  //
  // t_d7178bff's seven `chain claim` rows (DQ-DW) bite the same way, on the chain axis this
  // sibling adds: reverting the reported-clause reader's auxiliary group to its
  // single-auxiliary form (`(\s+(?:${ASSISTANT_AUX}))?`) returns exactly those seven to GREEN
  // while the whole rest of the map — including the single-auxiliary controls CG-CM and the
  // copula-passive report controls above — stays green. In the other direction, testing the
  // chain as a whole ATOM instead of taking its LAST auxiliary (`COPULA_AUX.test(chain)` in
  // place of `COPULA_AUX.test(chain.split(/\s+/).pop())`) reds the five `has been binned`
  // report controls in the report array above, and nothing else — that is the passive reading
  // the last-token test exists to keep. Both mutations were run by hand on this file.
  //
  // t_7cae27c8's seven `contracted 'd / 'll subject` rows (ED-EJ) bite the same way, one
  // contraction class over: the same revert returns them to GREEN as well (the whole rest of
  // the map staying green), which is the same measurement that proves the arm — not something
  // else in the reader — carries them, and the contracted `'d`/`'ll` PASSIVES added to the
  // report array below are the other direction's controls (`it'd been binned` would red if
  // the folded core, not the `been`, decided the chain).
  //
  // THE HANDLES SAY WHICH CLASS OWNS WHAT, one code per class and no code naming two. THIS
  // card's rows were cut as DQ-DW — the very seven slots the `chain claim` rows of #1078 had
  // already taken on `main` — so the re-cut onto `main` RENUMBERS this card's seven to ED-EJ,
  // the first free code past EC, which is the shape the t_1130759a re-cut used when it moved
  // the t_b0bf328c rows off CX-CZ and onto EA-EC rather than into a gap its base still had.
  // Nothing else moved: `chain claim` (t_d7178bff, #1085) keeps DQ-DW, the contracted copula
  // rows (t_6ad53e35) keep DH-DP, the quantifier rows keep CX-DC, and the infinitive chain
  // claims (t_b0bf328c) keep EA-EC.

  // And the POSITIVE CONTROL must not be what carries the detection: the person-
  // attribution sentence the fix ships (which names the assistant only to disown
  // the action) stays GREEN under the same scan. A scan that fired on it would be
  // unusable, and a scan that only fired on it would catch nothing real.
  const personCopy =
    "The bin belongs to you, not the assistant — deleting a form from the forms page moves it to " +
    "the trash, and restoring it from the trash brings it back with its address intact.";
  assert.deepEqual(
    assistantBinClaims(personCopy),
    [],
    "the scan fires on the page's own sentence that attributes the bin to the PERSON — it names " +
      "the assistant only to disown the action, so the arm would be red on the correct copy",
  );
  // ...and the denial shapes the page is allowed to use stay GREEN too — including
  // the denials the four NEW shapes (genitive subject, it-cleft, nominal, appositive)
  // could have swallowed if their lookaheads or the appositive had been written
  // carelessly. These are the "not/never" counterparts of the sentences above, and
  // any widening that reds on one of them is a false positive that would block the
  // page from stating its own boundary.
  for (const denial of [
    "The assistant cannot delete a form.",
    "The assistant can never delete or archive a form.",
    "The assistant may not restore a form.",
    "The assistant does not bin a form.",
    "The assistant will not trash a form.",
    "The assistant never deletes a form.",
    "It is not the assistant who bins a form.",
    "It's not the assistant who deletes the form.",
    "The assistant's job is not to bin a form.",
    "The assistant's job is never to delete a form.",
    "The assistant is not the binner of a form.",
    "The assistant is not the one who deletes a form.",
    "Deleting a form is not the assistant's job.",
  ]) {
    assert.deepEqual(
      assistantBinClaims(denial),
      [],
      `the scan fires on the page's own DENIAL (${JSON.stringify(denial)}) — the boundary is the ` +
        `thing the page is allowed to state, so the arm must not red on it`,
    );
  }
  // ...and a sentence that merely names the trash in what the assistant READS or REPORTS
  // stays GREEN too: the bin token there is the assistant's object of knowledge, not an
  // action of its (the header's "A REPORT IS NOT AN ACTION"). Both were RED under the
  // head scan — the second newly, from the structural rewrite — and the shipped page
  // carries neither, so this is the false-positive side of the same rule, pinned here so
  // a correct future sentence that mentions the assistant and the trash is not reded.
  //
  // t_9717fec1 added the three below, and t_7b69a77a the rest. They are the same shape
  // one step further in: the assistant's information verb reports a clause, but that
  // clause is about the bin rather than putting the assistant back in the bin verb's
  // subject position ("the form is in the trash", "the trash is empty"), OR it is headed
  // by a bin GERUND that is that clause's SUBJECT ("binning a form is easy"). The clamp
  // that catches a reported CLAIM must not swallow these, and they are what reds if
  // `assistantBinsInsideReport` is widened to "information verb … bin anywhere", or if
  // `reportedGerundIsSubject`'s predicate reading is dropped (the gerund rows after the
  // first two are the subject-gerund CONTROLS for exactly that mutation).
  //
  // t_687e46a3 added the last four. `look(s) up` is an information verb in this file's
  // own `INFO_VERB` list, but it is a PARTICLE verb: the assistant's verb in a match (A)
  // is the two words "look up", and only a reader that keeps the particle can see it.
  // Each of these was RED before this card — measured with the scan extracted from this
  // file — because `looks?\\s+up` was written with TWO backslashes inside a `String.raw`
  // template (a dead alternative: a literal backslash then `s+` then `up`, which no
  // sentence contains), and because `assistantVerb` read one token
  // ("looks"/"looked"/"looking"), so the fixed pattern alone was still unreachable.
  for (const report of [
    "The assistant can tell you where the trash is.",
    "A form the assistant lists may already be in the trash.",
    "The assistant shows you which form is in the trash.",
    "You can ask the assistant whether a form is in the trash.",
    "The assistant tells you the form is in the trash.",
    "The assistant says the trash is empty.",
    "The assistant says binning a form is easy.",
    "The assistant explains that binning a form is permanent.",
    "The assistant reports that binning a form is easy.",
    "The assistant mentions binning a form is easy.",
    "The assistant tells you restoring a form is possible.",
    "The assistant describes binning a form as easy.",
    // t_31293346 — the information verb in the PAST, the PERFECT and the PROGRESSIVE.
    // The list was read only at the verb's present-simple / third-person surface, so
    // these forms were read as ACTION verbs and red (the card's defect). Every one is
    // a pure report: the assistant is the verb's subject only as the REPORTER, and the
    // bin token later in its clause is its object of knowledge. They are the report
    // side of the same widening the `tense claim` rows CS-CW are the claim side of —
    // revert `INFO_VERB` and every one of these reds again.
    "The assistant told you where the trash is.",
    "The assistant told you the form is in the trash.",
    "The assistant told you nothing about the trash.",
    "The assistant had told you where the trash is.",
    "The assistant has told you the form is in the trash.",
    "The assistant has told you binning a form is easy.",
    "The assistant is telling you where the trash is.",
    "The assistant is showing you which form is in the trash.",
    "The assistant is listing the trash.",
    "The assistant is reading where the trash is.",
    "The assistant is seeing the trash.",
    "The assistant is asking where the trash is.",
    "The assistant is saying the trash is empty.",
    "The assistant is reporting that the form is in the trash.",
    "The assistant is knowing the trash.",
    "The assistant is finding the trash.",
    "The assistant is explaining where the trash is.",
    "The assistant is describing the trash.",
    "The assistant is answering where the trash is.",
    "The assistant is checking where the trash is.",
    "The assistant is mentioning the trash.",
    // t_255182df — the false positive this rework closes, pinned so it cannot reopen. The
    // rule's first cut read the predicate off a curated list of finite verbs, so a genuine
    // SUBJECT-gerund report whose predicate was a real finite verb OUTSIDE it was read as
    // the information verb's object and RED (the unsafe direction). These are that class:
    // the same "information verb + that + bin gerund + object + predicate" shape as the
    // accepted control above, distinguished only by the predicate's identity. The rule now
    // decides on the clause's SHAPE, so all four are GREEN for the same reason as
    // "… IS permanent" — the gerund phrase ends before the clause does.
    "The assistant explains that binning a form removes it from the list.",
    "The assistant tells you restoring a form brings it back.",
    "The assistant reports that deleting a form frees up space.",
    "The assistant mentions that binning a form hides it from the dashboard.",
    // t_687e46a3 — the particle-verb reports (see the comment above).
    "The assistant looks up where the trash is.",
    "The assistant looked up where the trash is.",
    "The assistant is looking up where the trash is.",
    "The assistant can look up where the trash is.",
    // t_82129fb4 — the QUANTIFIER-object REPORT, the control the quantifier widening must
    // not swallow. Same quantifier objects as the claim rows CX-DC, but the reported clause
    // is headed by a bin GERUND that is the clause's SUBJECT and has a predicate of its own,
    // so the rule must still read the predication and stand the claim down. Predicates are
    // OUTSIDE the pre-rework curated list on purpose (the t_255182df class): the reading
    // comes from the clause's SHAPE, so it cannot depend on which verb carries the
    // predication. A `DET` that over-consumed — eating the predication as part of the noun
    // phrase — would red these instead.
    "The assistant explains that deleting every form removes it from the list.",
    "The assistant tells you restoring all forms brings them back.",
    "The assistant reports that binning a few forms frees up space.",
    "The assistant mentions that trashing the whole form hides it from the dashboard.",
    "The assistant says binning each form is permanent.",
    "The assistant says deleting no form is possible.",
    // t_6ad53e35 — the CONTRACTED-subject reports, which must stay GREEN. The four PASSIVES
    // are the unsafe direction of the fix — a contraction carries the auxiliary too
    // (`it's binned` = `it is binned`, a passive on the thing), so reding them is the false
    // positive the contraction's folded core exists to prevent. The two ADDRESSEE clauses
    // are the other edge: `you're` behind the `you` the assistant reports TO is the PERSON,
    // not the assistant, and `you` is deliberately not a contraction core for exactly this
    // reason. (`we're` is NOT here: the card's own "the assistant mentions WE'RE restoring a
    // form" is a claim, so `we` IS a core — see the note above `CONTRACTED_SUBJ`.)
    "The assistant tells you it's binned.",
    "The assistant says they're trashed.",
    "The assistant reports that he's been deleted.",
    "The assistant explains that she's been trashed.",
    "The assistant tells you you're binning a form.",
    "The assistant tells you you're deleting forms.",
    // t_d7178bff — the BARE-subject CHAIN passives, the report READING of a reported clause
    // the chain reader must NOT red: the chain ends in a COPULA before a PASSIVE, so "… it
    // has been BINNED" is a passive on the thing (a report), while "… it has been BINNING" is
    // the progressive claim (the chain rows DQ-DW). They differ only by the auxiliary chain's
    // LAST token plus the bin form, so they are the positive control for the chain reader's
    // copula test: a mutation that tests the chain as a whole ATOM (so no chain ever reads as
    // a copula) turns every one of these RED — the unsafe direction this card must not open.
    // All were GREEN on main and stay GREEN here — measured, not reasoned.
    "The assistant tells you it has been binned.",
    "The assistant says it will be binned.",
    // t_b0bf328c — the information verb behind an auxiliary CHAIN. `assistantVerb` stripped
    // at most ONE auxiliary, so on the parent tree the verb it read was the chain's LAST
    // auxiliary ("been", "be", "going"), which is no information verb, and position (A)'s
    // own window flagged the sentence — a pure report read as a claim. Every one of these is
    // a report (the assistant is the verb's subject only as the REPORTER) and GREEN only
    // once the WHOLE chain is stripped. Reverting `assistantVerb`'s chain strip to the
    // single-auxiliary form reds exactly this block. (Kept alongside the quantifier reports
    // and #1081's contracted-subject reports above, not in place of them: both landed on
    // `main` after this card's base and every one of these blocks is a GREEN control — the
    // re-cut onto `main` (t_1130759a) is a union.)
    "The assistant has been telling you where the trash is.",
    "The assistant had been telling you where the trash is.",
    "The assistant will be telling you where the trash is.",
    "The assistant has been asked where the trash is.",
    "The assistant can be told where the trash is.",
    "The assistant is going to tell you where the trash is.",
    "The assistant would have been telling you where the trash is.",
    "The assistant is going to be telling you where the trash is.",
    "The assistant will have been told where the trash is.",
    "The assistant might have been told where the trash is.",
    "The assistant has been showing you where the trash is.",
    "The assistant has been asking you where the trash is.",
    "The assistant has been checking whether a form is in the trash.",
    "The assistant has been telling you whether a form is in the trash.",
    "The assistant tells you it had been trashed.",
    "The assistant says it will be restored.",
    "The assistant reports that it is going to be deleted.",
    "The assistant explains that it is about to be binned.",
    // t_7cae27c8 — the same controls for the `'d` / `'ll` contractions. The four PASSIVES are
    // this class's unsafe direction: a modal contraction can carry `had` (`it'd been binned`
    // = `it had been binned`), so the `been` is the last auxiliary before the participle and
    // the sentence is a report on the thing. The two ADDRESSEE clauses are the other edge —
    // `you` is deliberately not a contraction core, so `you'd`/`you'll` behind the `you` the
    // assistant reports TO stays the person. The asymmetry with the ACTIVE `it'd BINNED`
    // (pinned in this card's own test: no auxiliary, so the core is a modal and the perfect
    // is the claim, exactly as the bare `it has binned` row CK is) is the point of both.
    "The assistant tells you it'd been binned.",
    "The assistant says they'll be trashed.",
    "The assistant reports that he'd been deleted.",
    "The assistant explains that she'll be trashed.",
    "The assistant tells you you'd been binning a form.",
    "The assistant tells you you'll be binning a form.",
  ]) {
    assert.deepEqual(
      assistantBinClaims(report),
      [],
      `the scan fires on a sentence where the assistant only READS or REPORTS the bin ` +
        `(${JSON.stringify(report)}) — the trash token is its object of knowledge, not an action, ` +
        `and reding on it would block the page from naming the trash at all`,
    );
  }
});

// ── THE QUANTIFIER OBJECT'S OWN BITE (t_82129fb4) ────────────────────────────
//
// THE DEFECT this test guards: `DET` listed the articles, possessives and demonstratives
// but none of the QUANTIFIER determiners (every|each|all|both|no|many|several|most|a few|
// one|the whole|an entire|…). For an object-gerund CLAIM whose object opens with one, the
// optional determiner matched nothing, the bare `[a-z]+` ate the QUANTIFIER as if it were
// the object noun, and the rest of the noun phrase (" form.") stayed in the clause as a
// PHANTOM predication — so `reportedGerundIsSubject` read the gerund as a SUBJECT and stood
// the claim down. The claim went GREEN: a false negative, i.e. a copy that credits the
// assistant with the bin would ship past the scan.
//
// WHY IT IS PROVED HERE AND NOT MERELY ASSERTED. The rows CX-DC above are pinned in the
// BITES map, so a regression reds THIS suite — but a map row and a guard can drift apart
// silently (the map judges the SHIPPED page plus an injected sentence, a different code
// path from the scan's own reading of a bare claim). This test closes that gap by running
// the REAL scan with the quantifier handling REMOVED: `assistantBinClaims` is parameterised
// on its determiner class, and `DET_WITHOUT_QUANT` is exactly `DET` before this card — so
// `assistantBinClaims(sentence, DET_WITHOUT_QUANT)` IS the pre-fix rule, not a paraphrase
// of it (the file's own doctrine: see the note above `BIN_VOCAB`). Both directions are
// pinned: the shipped `DET` catches every row, the pre-fix `DET` misses every row, and the
// report controls stay GREEN in BOTH — so the test would also fail if the quantifier class
// were widened to eat the predication.
test("the quantifier-determiner object is caught by the SHIPPED determiner set and lost by the pre-fix one (t_82129fb4)", () => {
  // The claim side: a quantifier object, the assistant IS the deleter.
  const claims = [
    "The assistant reports deleting every form.",
    "The assistant reports binning all forms.",
    "The assistant mentions trashing no form.",
    "The assistant describes restoring a few forms.",
    "The assistant explains deleting the whole form.",
    "The assistant reports binning an entire form.",
    "The assistant mentions deleting each form.",
    "The assistant says trashing both forms.",
    "The assistant reports restoring many forms.",
    "The assistant describes deleting most forms.",
    "The assistant reports deleting one form.",
    "The assistant says binning several forms.",
  ];
  // The report side: the SAME quantifier objects, but the gerund is the reported clause's
  // SUBJECT with a predicate of its own, so BOTH determiner sets must stand the claim down.
  const reports = [
    "The assistant explains that deleting every form removes it from the list.",
    "The assistant tells you restoring all forms brings them back.",
    "The assistant reports that binning a few forms frees up space.",
    "The assistant mentions that trashing the whole form hides it from the dashboard.",
    "The assistant says binning each form is permanent.",
  ];

  const shippedMissed: string[] = [];
  for (const s of claims) if (assistantBinClaims(s).length === 0) shippedMissed.push(s);
  assert.deepEqual(
    shippedMissed,
    [],
    `the SHIPPED determiner set misses a quantifier-determiner object, so a claim that credits ` +
      `the assistant with the bin ships GREEN (the defect this card fixes):\n  ${shippedMissed.join("\n  ")}`,
  );

  // The bite: revert `DET` to its pre-card value and the SAME rows go GREEN. Vacuity first —
  // if the mutation did not change the pattern, everything below asserts nothing.
  assert.notEqual(
    DET,
    DET_WITHOUT_QUANT,
    "DET and DET_WITHOUT_QUANT are identical, so removing the quantifier class is a no-op and " +
      "this test would pass while proving nothing",
  );
  const preFixMissed = claims.filter((s) => assistantBinClaims(s, DET_WITHOUT_QUANT).length === 0);
  assert.deepEqual(
    preFixMissed,
    claims,
    `the pre-fix determiner set did not lose EVERY quantifier row, so some of them are carried ` +
      `by something other than the quantifier class and this test does not pin the card's defect:\n` +
      `  still caught without the quantifiers: ${JSON.stringify(
        claims.filter((s) => assistantBinClaims(s, DET_WITHOUT_QUANT).length > 0),
      )}`,
  );

  // …and the OTHER direction: the quantifier class must not swallow a REPORT. Both sets must
  // stand these down, or the widening would trade this false negative for a false positive.
  for (const s of reports) {
    assert.deepEqual(
      assistantBinClaims(s),
      [],
      `the SHIPPED scan reds a quantifier-object REPORT (${JSON.stringify(s)}) — the quantifier ` +
        `class is over-consuming, eating the predication that makes the gerund a subject`,
    );
    assert.deepEqual(
      assistantBinClaims(s, DET_WITHOUT_QUANT),
      [],
      `the pre-fix scan reds a quantifier-object REPORT (${JSON.stringify(s)}) — this is the ` +
        `t_255182df false-positive class and must be GREEN in both determiner sets`,
    );
  }

  // THE ORACLE'S GROUPING (t_93ef586c). Everything above is a proof about the SCAN only if
  // `DET_WITHOUT_QUANT` IS the pre-fix rule; an UNGROUPED article set (`the|a|…|any`) is a
  // PARAPHRASE of it, and the paraphrase fails in the same direction as the defect. `npObj`
  // composes the determiner as `(?:${det}\s+)?[a-z]+`, so an ungrouped set binds the `\s+`
  // to the LAST alternative only (`any`): for every other article/possessive/demonstrative
  // the determiner is not consumed, the phrase leaves the phantom predication this card is
  // about, and the "pre-fix" arm silently loses claims the real pre-fix scan CAUGHT — the
  // test stays green while proving the opposite. Pinned SEMANTICALLY (a shape check would
  // pin syntax instead): these article-object claims are all four CAUGHT by the real pre-fix
  // scan — measured against `1d9fa4a6`'s own grouped `DET` — and an ungrouped oracle returns
  // `[]` on every one of them.
  const preFixCaught = ["a form", "any form", "some form", "those forms"].map(
    (o) => `The assistant reports deleting ${o}.`,
  );
  for (const s of preFixCaught)
    assert.ok(
      assistantBinClaims(s, DET_WITHOUT_QUANT).length > 0,
      `the "pre-fix" oracle misses ${JSON.stringify(s)}, so it is NOT the pre-fix rule`,
    );
});

// ── THE REPORTED CLAUSE'S CONTRACTED SUBJECT (t_6ad53e35) ────────────────────
//
// THE DEFECT this test guards. `assistantBinsInsideReport` read the reported clause's
// SUBJECT as a BARE pronoun — `it|they|he|she` — or the noun `the assistant`, and then
// required WHITESPACE before the verb phrase. A subject that contracts the pronoun with its
// copula or auxiliary into ONE token matched none of them: the `\b` after `it` holds (an
// apostrophe is a non-word char) but the next token is `'s`, not whitespace, so the
// `\s+` before the verb group failed, the reader returned false, and a genuine CLAIM whose
// reported clause contracts its subject was read as a report. Measured with the scan
// extracted from this file: a 192-sentence battery (6 information verbs x 8 contracted
// subjects x 4 bin gerunds) is 192 missed / 0 caught at the pre-fix head, while the SAME
// frame with the subject uncontracted is caught. That is a false negative — the unsafe
// direction — and it is the axis t_d7178bff did NOT touch (its rows are the bare-subject
// chain claims).
//
// WHY IT IS PROVED HERE AND NOT MERELY ASSERTED. The nine `contracted subject` rows DH-DP in
// the BITES map above are pinned, so a regression reds THIS suite — but a map row and a
// guard can drift apart silently (the map judges the SHIPPED page plus an injected sentence,
// a different code path from the scan's own reading of a bare claim). This test runs the
// REAL scan directly, and the pre-fix behaviour is measured by running the shipped subject
// alternation without the contraction arm (`assistantBinClaims(s, DET, false)`), i.e. the
// pre-fix rule verbatim rather than a paraphrase of it (the file's own doctrine: see the
// note above `BIN_VOCAB`). Both directions are pinned: the shipped reader catches every
// contracted claim, the pre-fix reader misses every one, and the report controls — the
// contracted PASSIVES and the addressee/person clauses — stay GREEN in BOTH, so the test
// also fails if the contraction's folded core is dropped (reding the passives) or if the
// contraction arm is widened to the un-contracted `we`/`you`.
test("the contracted reported-clause subject is caught by the SHIPPED reader and lost by the pre-fix one (t_6ad53e35)", () => {
  const verbs = ["tells you", "reports that", "says", "explains that", "mentions", "describes"];
  const subjects = ["it's", "they're", "he's", "she's", "we're", "it's been", "they've been", "he's been"];
  const gerunds = ["binning a form", "trashing forms", "restoring a form", "deleting a form"];
  const claims: string[] = [];
  for (const v of verbs) for (const s of subjects) for (const g of gerunds) claims.push(`The assistant ${v} ${s} ${g}.`);

  // Vacuity: the pre-fix reader must actually differ from the shipped one, or the bite below
  // asserts nothing. (The same sentence with an uncontracted subject IS caught, which is why
  // the gap hid.)
  assert.deepEqual(
    assistantBinClaims("The assistant tells you it is binning a form.", DET, true),
    assistantBinClaims("The assistant tells you it is binning a form.", DET, false),
    "sanity: the uncontracted control changed under the pre-fix reader — the contraction arm " +
      "is meant to be the ONLY difference, so this test reads the wrong scan",
  );
  assert.deepEqual(
    assistantBinClaims("The assistant tells you it's binning a form.", DET, false),
    [],
    "the pre-fix reader (no contraction arm) already catches a contracted subject, so this test pins no defect",
  );

  const shippedMissed: string[] = [];
  for (const s of claims) if (assistantBinClaims(s).length === 0) shippedMissed.push(s);
  assert.deepEqual(
    shippedMissed,
    [],
    `the SHIPPED reader misses a claim whose reported clause contracts its subject, so a copy ` +
      `that credits the assistant with the bin ships GREEN (the defect this card fixes):\n  ${shippedMissed.join("\n  ")}`,
  );

  // The bite: the SAME 192-sentence battery read by the pre-fix subject alternation.
  const preFixMissed = claims.filter((s) => assistantBinClaims(s, DET, false).length === 0);
  assert.deepEqual(
    preFixMissed,
    claims,
    `the pre-fix reader did not lose EVERY contracted claim, so some are carried by something ` +
      `other than the contraction arm and this test does not pin the card's defect:\n` +
      `  still caught without the arm: ${JSON.stringify(claims.filter((s) => assistantBinClaims(s, DET, false).length > 0))}`,
  );

  // …and the OTHER direction: the contraction arm must not swallow a REPORT. A contraction
  // carries the auxiliary too, so `it's binned` is the PASSIVE on the thing; reding it (by
  // dropping the folded core) would trade this false negative for a false positive.
  const reports = [
    "The assistant tells you it's binned.",
    "The assistant says they're trashed.",
    "The assistant reports that he's been deleted.",
    "The assistant explains that she's been trashed.",
    "The assistant tells you it's been binned.",
    "The assistant tells you you're binning a form.",
    "The assistant tells you you're deleting forms.",
  ];
  for (const s of reports) {
    // The pre-fix reader must ALSO keep these green — they are reports in BOTH subject
    // alternations, so a test that only ran the shipped one would not prove the arm is
    // report-safe.
    assert.deepEqual(
      assistantBinClaims(s),
      [],
      `the SHIPPED scan reds a contracted-subject REPORT (${JSON.stringify(s)}) — a contraction ` +
        `that carries a copula before a participle is the passive on the thing, not a claim, and ` +
        `the addressee reading is the person, not the assistant`,
    );
    assert.deepEqual(
      assistantBinClaims(s, DET, false),
      [],
      `the pre-fix scan reds a contracted-subject REPORT (${JSON.stringify(s)}) — a control both ` +
        `readers must keep GREEN`,
    );
  }

  // The UNCONTRACTED `we`/`you` twins of the two claims the card names are REPORTS, not
  // claims, in both readers: `we`/`you` only stand for the assistant when the pronoun is
  // CONTRACTED (the card's "we're" is the assistant among the actors), so the un-contracted
  // form is left out of the alternation on purpose. These pin that boundary.
  for (const s of [
    "The assistant tells you we are deleting a form.",
    "The assistant says we are restoring a form.",
    "The assistant reports that you are trashing a form.",
  ]) {
    assert.deepEqual(assistantBinClaims(s), [], `the SHIPPED scan reds the un-contracted twin (${JSON.stringify(s)})`);
    assert.deepEqual(assistantBinClaims(s, DET, false), [], `the pre-fix scan reds the un-contracted twin (${JSON.stringify(s)})`);
  }
});

// ── THE MODAL CONTRACTIONS `'d` / `'ll` IN THE REPORTED CLAUSE (t_7cae27c8) ──
//
// THE DEFECT this test guards, one contraction class over from t_6ad53e35's `'s`/`'re`/`'ve`.
// The contracted-subject arm listed only the copula/`have` contractions, so a reported
// clause whose subject contracts a MODAL (`'ll` = will) or `had`/`would` (`'d`) into its one
// token matched neither the alternation nor the core map: `The assistant tells you he'd been
// deleting forms.` was read as a REPORT while its uncontracted twin (`he HAD been deleting`)
// was read as the claim it is — the unsafe direction, the same shape as #1081's gap.
// Measured on the live tree with the scan extracted from this file: contracted GREEN,
// uncontracted RED, across six information verbs x five pronouns x the claim shapes below.
//
// WHY IT IS PROVED HERE AND NOT MERELY ASSERTED: as for t_6ad53e35, the pre-fix behaviour is
// the shipped rule with exactly the contraction arm removed (`assistantBinClaims(s, DET,
// false)`), not a paraphrase of it. Both directions are pinned: the shipped reader catches
// every contracted claim, the pre-fix reader misses every one, the UNCONTRACTED twins are
// caught by BOTH (which is why the gap hid — the twin is carried by the bare arm), and the
// reports (the contracted passives, the addressee) stay GREEN in BOTH. The `'d` ambiguity is
// pinned too, since had/would is the one thing this contraction has that `'s`/`'ll` do not.
test("a reported-clause subject contracted with 'd or 'll is caught by the SHIPPED reader and lost by the pre-fix one (t_7cae27c8)", () => {
  const verbs = ["tells you", "reports that", "says", "explains that", "mentions", "describes"];
  const pronouns = ["it", "they", "he", "she", "we"];
  // A claim behind a modal contraction: `'d`/`'ll` + BE + gerund (would/will progressive) and
  // `'d` + BEEN + gerund (had perfect). `'ll been` is not English, so the perfect shape is
  // `'d`-only.
  const shapes: Array<[string, string]> = [
    ["'d", "be"],
    ["'d", "been"],
    ["'ll", "be"],
  ];
  const gerunds = ["binning a form", "trashing forms", "restoring a form", "deleting a form"];
  const claims: string[] = [];
  for (const v of verbs) {
    for (const p of pronouns) {
      for (const [contr, aux] of shapes) {
        for (const g of gerunds) claims.push(`The assistant ${v} ${p}${contr} ${aux} ${g}.`);
      }
    }
  }

  // Vacuity: the pre-fix reader must actually differ from the shipped one, or the bite below
  // asserts nothing. The same sentences with an UNCONTRACTED subject change NOTHING under the
  // pre-fix reader — the contraction arm is the only difference between the two readers, so
  // the twins are the control that hid this gap.
  for (const s of [
    "The assistant tells you it would be binning a form.",
    "The assistant tells you it had been binning a form.",
    "The assistant says they will be deleting forms.",
  ]) {
    assert.deepEqual(
      assistantBinClaims(s, DET, true),
      assistantBinClaims(s, DET, false),
      `sanity: the uncontracted control changed under the pre-fix reader — the contraction arm ` +
        `is meant to be the ONLY difference, so this test reads the wrong scan (${JSON.stringify(s)})`,
    );
    assert.notDeepEqual(
      assistantBinClaims(s),
      [],
      `the SHIPPED reader misses the UNCONTRACTED twin (${JSON.stringify(s)}) — the twin is carried ` +
        `by the bare-subject arm, so a scanner that misses it is broken for a reason other than ` +
        `the contraction token this card pins`,
    );
  }
  assert.deepEqual(
    assistantBinClaims("The assistant tells you he'd been deleting forms.", DET, false),
    [],
    "the pre-fix reader (no contraction arm) already catches a `'d` subject, so this test pins no defect",
  );
  assert.deepEqual(
    assistantBinClaims("The assistant tells you it'll be binning a form.", DET, false),
    [],
    "the pre-fix reader (no contraction arm) already catches a `'ll` subject, so this test pins no defect",
  );

  const shippedMissed = claims.filter((s) => assistantBinClaims(s).length === 0);
  assert.deepEqual(
    shippedMissed,
    [],
    `the SHIPPED reader misses a claim whose reported clause contracts its subject with 'd or 'll ` +
      `(${claims.length} sentences), so a copy that credits the assistant with the bin ships GREEN ` +
      `(the defect this card fixes):\n  ${shippedMissed.slice(0, 12).join("\n  ")}`,
  );

  // The bite: the SAME battery read by the pre-fix subject alternation.
  const preFixMissed = claims.filter((s) => assistantBinClaims(s, DET, false).length === 0);
  assert.deepEqual(
    preFixMissed,
    claims,
    `the pre-fix reader did not lose EVERY contracted claim, so some are carried by something ` +
      `other than the contraction arm and this test does not pin the card's defect:\n` +
      `  still caught without the arm: ` +
      JSON.stringify(claims.filter((s) => assistantBinClaims(s, DET, false).length > 0).slice(0, 12)),
  );

  // The TYPOGRAPHIC APOSTROPHE is the same token — the alternation accepts either — so a copy
  // pasted out of a word processor is caught too (and missed by the pre-fix reader as well).
  const typographic = "The assistant tells you he\u2019d been deleting forms.";
  assert.notDeepEqual(
    assistantBinClaims(typographic),
    [],
    "a `'d` subject written with the typographic apostrophe is read as a report — the alternation covers both apostrophes",
  );
  assert.deepEqual(
    assistantBinClaims(typographic, DET, false),
    [],
    "the pre-fix reader catches the typographic-apostrophe claim, so this row pins no defect",
  );

  // THE `'d` READING IS AMBIGUOUS — had OR would — AND THE FOLD IS SAFE UNDER EITHER, because
  // the core is read only by the copula test and NEITHER READING IS A COPULA. Pinned, so the
  // representative `CONTRACTION_CORE` picks cannot silently become one (which would start
  // reading a modal contraction as the passive).
  assert.ok(
    !COPULA_AUX.test("had") && !COPULA_AUX.test("would") && !COPULA_AUX.test("will"),
    "a `'d`/`'ll` core is in COPULA_AUX — folding one onto the chain could read a modal " +
      "contraction as the copula, which is the ambiguity the core's representative rests on",
  );
  // …and the two shapes the ambiguity could break, measured. The EMPTY chain (no auxiliary
  // between the contraction and the verb) is decided by the core alone, and a modal is not a
  // copula, so it is the ACTIVE perfect — the claim, exactly as the bare `it has binned` row
  // CK reads. The PASSIVE is decided one token later by the `been`, so it stays GREEN.
  assert.notDeepEqual(
    assistantBinClaims("The assistant tells you it'd bin a form."),
    [],
    "the empty chain behind a `'d` contraction reads as a report — the core is a modal " +
      "(`had`/`would`), not a copula, so this sentence credits the assistant with the bin and " +
      "must be RED, exactly as the bare `it has binned` row is",
  );
  assert.deepEqual(
    assistantBinClaims("The assistant tells you it'd been binned."),
    [],
    "`it'd been binned` is read as a claim — the contraction carries `had` and the `been` is " +
      "the last auxiliary before the participle, so this is the PASSIVE on the thing (a report)",
  );

  // …and the OTHER direction: the arm must not swallow a report. A contraction that carries a
  // copula or `had` before a participle is the passive on the thing, and `you` — deliberately
  // not a contraction core — stays the person the assistant reports TO.
  const reports = [
    "The assistant tells you it'd been binned.",
    "The assistant says they'll be trashed.",
    "The assistant reports that he'd been deleted.",
    "The assistant explains that she'll be trashed.",
    "The assistant tells you you'd been binning a form.",
    "The assistant tells you you'll be binning a form.",
  ];
  for (const s of reports) {
    assert.deepEqual(
      assistantBinClaims(s),
      [],
      `the SHIPPED scan reds a contracted-subject REPORT (${JSON.stringify(s)}) — a modal ` +
        `contraction before a participle is the passive on the thing, and the addressee reading ` +
        `is the person, not the assistant`,
    );
    assert.deepEqual(
      assistantBinClaims(s, DET, false),
      [],
      `the pre-fix scan reds a contracted-subject REPORT (${JSON.stringify(s)}) — a control both readers must keep GREEN`,
    );
  }
});

// ── THE CONTRACTION TAIL IS A WORD BOUNDARY, NOT AN ESCAPED BACKSLASH ────────
//
// Found while verifying PR #1063 (jf-qa card t_27dc1253): that PR's diff rewrote
// assistantVerb's contraction tail `\b` as `\\b`. Both COMPILE, so nothing objected.
// Inside a regex LITERAL `\\b` is a literal backslash followed by `b` — dead, never
// matching — so the optional group fails and the verb is truncated at the apostrophe:
// assistantVerb("assistant doesn't bin a form") returned "doesn", not "doesn't", and
// the same for 11 of the 12 contractions this file's own NEGATION list names
// (won't/isn't/hasn't/don't/didn't/…; measured, not reasoned about). The suite stayed
// GREEN at 23/23 on that, because nothing in it reads the exact token: the only
// consumer is HAS_NON_ACTION, which reads "doesn" and "doesn't" identically. So the
// drift was invisible, and stayed invisible until a later check reads the token — at
// which point it is wrong with no test to catch it. This is that test.
//
// WHAT IS PINNED IS THE LITERAL, deliberately rather than as a shortcut: the literal
// is what drifted and what the one-char revert restores, and the two forms are one
// keystroke apart. Executing the shipped literal here would need an eval-style
// constructor this file uses nowhere (and eslint refuses), while re-authoring the
// pattern in the test would assert a COPY instead of the line under test. The
// property asserted is the card's own — "the line no longer differs from main's" —
// made executable: one backslash before the `b`, never two.
test("the assistant-verb contraction tail is a word boundary, not an escaped backslash", () => {
  // The assignment line is the ONLY line whose trimmed text starts with the literal's
  // head, so the selector cannot match this test's own search line (which merely
  // mentions that head mid-expression).
  const tail = chatTest
    .split("\n")
    .filter((l) => l.trim().startsWith("const verb = /^[a-z]+"));
  assert.equal(
    tail.length,
    1,
    `expected exactly one assistantVerb contraction-tail line, found ${tail.length}: ` +
      `${JSON.stringify(tail)}`,
  );
  // One backslash before `b` — the word boundary that lets the contraction match…
  assert.ok(
    tail[0].includes("(?:'[a-z]+\\b)?"),
    `assistantVerb's contraction tail is not a word boundary, so every contraction token is ` +
      `truncated at the apostrophe ("doesn't" → "doesn"): ${JSON.stringify(tail[0].trim())}`,
  );
  // …and never two, which is an escaped backslash and matches no contraction at all.
  assert.ok(
    !tail[0].includes("(?:'[a-z]+\\\\b)?"),
    `assistantVerb's contraction tail carries an escaped backslash — inside a regex LITERAL ` +
      `that is a literal backslash followed by "b", so the optional group can never match and ` +
      `the tail is dead code: ${JSON.stringify(tail[0].trim())}`,
  );
});

// ── THE PARTICLE VERB `look up` (t_687e46a3) ─────────────────────────────────
//
// THE DEFECT, two layers on one arm. `INFO_VERB` lists the assistant's information
// verbs; one of them is the PARTICLE verb "look up". Two INDEPENDENT faults kept it
// dead on main, both measured with the scan extracted from this file:
//
//   1. the alternative was written `looks?\\s+up` with TWO backslashes inside a
//      `String.raw` template. String.raw keeps both, so the branch is `look(s)?` +
//      `\\` + `s+` + `up` — a literal backslash then `s+`, which no sentence can
//      contain, so the alternative matched nothing.
//   2. `assistantVerb` read ONE token, so the assistant's verb came back as
//      "looks"/"looked"/"looking" and `HAS_NON_ACTION.test("looks")` is FALSE: even
//      with (1) repaired, the two-word verb is unreachable by the guard.
//
// So a pure REPORT with the particle verb was RED (read as a claim): "The assistant
// looks up where the trash is." -> a bin claim. The four sentences are in the arm's
// report-control array above; this test pins the two layers that make them GREEN, and
// that neither layer alone is enough.
test("the particle verb `look up` is reachable — one live backslash in the INFO_VERB alternative, and a verb reader that keeps the particle", () => {
  // LAYER 1 — the escaping, read back off the assignment line.
  const infoLine = chatTest.split("\n").filter((l) => l.trim().startsWith("const INFO_VERB = "));
  assert.equal(
    infoLine.length,
    1,
    `expected exactly one INFO_VERB assignment line, found ${infoLine.length}: ${JSON.stringify(infoLine)}`,
  );
  const line = infoLine[0];
  assert.ok(
    line.includes("(?:looks?|looked|looking)\\s+up"),
    `INFO_VERB's particle alternative is not the one-backslash form — inside String.raw \`\\s\` is the whitespace escape, and two backslashes make it a dead branch that matches nothing: ${JSON.stringify(line.trim())}`,
  );
  assert.ok(
    !line.includes("(?:looks?|looked|looking)\\\\s+up") && !line.includes("looks?\\\\s+up"),
    `INFO_VERB's particle alternative carries an escaped backslash again (the dead branch this card fixes): ${JSON.stringify(line.trim())}`,
  );

  // LAYER 2 — the reader keeps the particle, so HAS_NON_ACTION sees the verb, not its
  // bare stem. This is the half that a fixed pattern alone does NOT provide.
  assert.equal(
    assistantVerb("assistant looks up where the trash is"),
    "looks up",
    'assistantVerb no longer returns the whole particle verb — a one-token reader returns "looks", and HAS_NON_ACTION.test("looks") is FALSE, so the two-word verb stays unreachable even with the pattern repaired',
  );
  assert.ok(
    !HAS_NON_ACTION.test("looks") && HAS_NON_ACTION.test("looks up"),
    "HAS_NON_ACTION must recognise the particle verb and not its bare stem — otherwise layer 1 alone would leave the sentence RED",
  );

  // …and the behaviour, so this test bites on its own as well as via the report array.
  for (const s of [
    "The assistant looks up where the trash is.",
    "The assistant looked up where the trash is.",
    "The assistant is looking up where the trash is.",
    "The assistant can look up where the trash is.",
  ]) {
    assert.deepEqual(
      assistantBinClaims(s),
      [],
      `the scan reads a pure REPORT with the particle verb as a bin claim (${JSON.stringify(s)}) — "look up" is an information verb in INFO_VERB, so a sentence that only names the trash in what the assistant looks up is GREEN`,
    );
  }
});

// ── THE AUXILIARY CHAIN IS STRIPPED WHOLE, AND THE INFINITIVE KEEPS ITS CATCH ────────
//
// t_b0bf328c, two layers on one reader. `assistantVerb` is what "A REPORT IS NOT AN ACTION"
// reads to decide whether the bin token later in the assistant's clause is an action or an
// object of knowledge. `ASSISTANT_AUX` names the words a verb phrase may OPEN with, and those
// words STACK — "has been telling", "will be telling", "is going to tell" are one phrase. The
// reader stripped AT MOST ONE of them, so the token it returned was the chain's LAST auxiliary
// ("been", "be", "going"), never the information verb behind it, and `HAS_NON_ACTION` reads
// none of those — so a pure report behind any two-word chain was red (the card's defect).
//
// WHAT MAKES IT BITE, measured rather than asserted, in two directions:
//   * this file's own report-control array (the t_b0bf328c block) reds if the chain strip is
//     reverted to the single-auxiliary form — exactly those rows, the rest of the map GREEN.
//   * the claim side is the reason the fix is not just "strip more". A bare bin INFINITIVE
//     after the recognised verb ("has been asked TO BIN a form") is a CLAIM: the assistant is
//     the one who would bin it, and position (A)'s infinitive shape catches the same
//     predication from the other end ("ask the assistant to bin a form"). The parent held
//     those catches because it could not READ the verb; once the chain strip reads it, only
//     `binInfinitiveAfterVerb` holds them. Dropping that one call reds the three
//     `chain claim … infinitive` rows and nothing else — the over-fix this test pins against.
test("the auxiliary chain is stripped as a whole, and the bin infinitive behind it stays a claim", () => {
  // LAYER 1 — the reader strips the chain, not one auxiliary. Read back off the source line,
  // because the difference is one `(...)+` and a literal-execution would need an eval-style
  // constructor this file uses nowhere. The needle carries a backtick so it cannot match this
  // test's own filter line.
  const NEEDLE = "const chain = new RegExp(" + "`";
  const readerLines = chatTest.split("\n").filter((l) => l.includes(NEEDLE));
  assert.equal(
    readerLines.length,
    1,
    `expected exactly one auxiliary-chain strip line, found ${readerLines.length}: ${JSON.stringify(readerLines)}`,
  );
  assert.match(
    readerLines[0],
    /RegExp\(`\^\(\?:\(\?:\$\{ASSISTANT_AUX\}\)[\s\S]*\)\+`/,
    `assistantVerb's chain strip is not the whole-chain form — a single-auxiliary reader returns the chain's last auxiliary ("been"/"be"/"going"), which is no information verb, so a pure report behind a two-word chain reds: ${JSON.stringify(readerLines[0].trim())}`,
  );
  assert.doesNotMatch(
    readerLines[0],
    /RegExp\(`\^\(\?:\$\{ASSISTANT_AUX\}\)/,
    `assistantVerb's chain strip is the single-auxiliary form this card replaces (no repeated group): ${JSON.stringify(readerLines[0].trim())}`,
  );

  // LAYER 2 — the reader's output, so a rewrite that kept the regex but not its effect fails.
  assert.equal(
    assistantVerb("assistant has been telling you where the trash is"),
    "telling",
    "assistantVerb no longer returns the information verb behind a two-auxiliary chain — a single-auxiliary reader returns \"been\", and HAS_NON_ACTION.test(\"been\") is FALSE, so the report stays red",
  );
  assert.equal(
    assistantVerb("assistant is going to tell you where the trash is"),
    "tell",
    'assistantVerb no longer strips the periphrastic future as a whole — a one-token reader returns "going", not the information verb "tell"',
  );
  assert.ok(
    !HAS_NON_ACTION.test("been") && HAS_NON_ACTION.test("telling"),
    "HAS_NON_ACTION must recognise the information verb behind the chain and not the auxiliary in front of it",
  );

  // …and the behaviour: the report controls GREEN, the infinitive claims RED.
  for (const report of [
    "The assistant has been telling you where the trash is.",
    "The assistant had been telling you where the trash is.",
    "The assistant will be telling you where the trash is.",
    "The assistant has been asked where the trash is.",
    "The assistant can be told where the trash is.",
    "The assistant is going to tell you where the trash is.",
  ]) {
    assert.deepEqual(
      assistantBinClaims(report),
      [],
      `the scan reads a pure REPORT behind an auxiliary chain as a bin claim (${JSON.stringify(report)}) — the chain strip never reached the information verb`,
    );
  }
  for (const claim of [
    "The assistant has been asked to bin a form.",
    "The assistant can be told to bin a form.",
    "The assistant is told to bin a form.",
    "The assistant has been told to bin a form.",
    "The assistant is going to be told to delete a form.",
  ]) {
    assert.ok(
      assistantBinClaims(claim).length > 0,
      `the scan now reads a CLAIM as a report (${JSON.stringify(claim)}) — the bin INFINITIVE behind the information verb is the assistant binning the form, and the chain strip must not stand it down (binInfinitiveAfterVerb)`,
    );
  }
  // …and the report's OWN infinitive stays GREEN, or the companion check would swallow the
  // very shape it distinguishes itself from.
  for (const report of [
    "The assistant tells you to bin a form.",
    "The assistant tells the person where the trash is.",
  ]) {
    assert.deepEqual(
      assistantBinClaims(report),
      [],
      `the scan fires on a report whose verb is followed by an indirect object and an infinitive (${JSON.stringify(report)}) — the "you" makes it a report, not the assistant's own bin`,
    );
  }
});

test("every chat dictionary key defines all five locales", () => {
  const keys = Object.keys(DICT).filter((k) => k.startsWith("chat.") || k === "nav.chat");
  assert.ok(keys.length >= 12, `expected the chat dictionary, got ${keys.length} keys`);
  const missing: string[] = [];
  for (const k of keys) {
    const entry = DICT[k as keyof typeof DICT] as Record<string, unknown>;
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") missing.push(`${k}.${lang}`);
    }
  }
  assert.deepEqual(missing, [], `missing translations:\n  ${missing.join("\n  ")}`);
});

// ── THE CONFIRM PATH'S 400 IS DECIDED BY CODE, NOT BY STATUS ─────────────────
//
// THE FIRST DEFECT (t_790b1016, found while verifying PR 747). The 400 branch on
// the confirm/cancel path did TWO things at once:
//
//     setState(msgIndex, p.token, "failed");   // → card footer: chat.confirmFail
//     setErr(t("chat.scopeRequired"));         // → role=alert: chat.scopeRequired
//
// so one 400 rendered both sentences at once, and the footer's advice was the
// wrong one: "The change could not be applied. Please try again." tells the user
// to repeat an action that fails identically without a workspace. Measured in a
// real browser on the PR 747 merge build (role=alert = the scope copy, card
// footer = the "try again" copy, same 400).
//
// WHY THE STATE IS THE FIX AND NOT THE FOOTER. The server refuses an unscoped
// call BEFORE it reads the proposal token (requireResolvedWorkspaceScope runs
// ahead of the token checks in handleChatConfirm), so nothing was attempted and
// "failed" was never true of this request. Giving the refusal its own state lets
// the card stop claiming a failure without inventing a second rendering of the
// scope copy — that duplication is what the pre-fix shape did, and this card says
// explicitly that the alert is the ONE place the copy belongs.
//
// THE SECOND DEFECT (t_fb393abe, found by the QA lane while verifying PR 779 and
// fixed here). The branch that got its own state was keyed on the BARE STATUS —
// `if (r.status === 400)` — and it returned before the body was ever parsed. A 400
// from this route is NOT necessarily a workspace-scope refusal:
// chatConfirmStatusFor maps everything it does not recognise to 400, and one such
// class arrives AFTER the token is claimed (rest_chat.go's default arm over
// chat/proposal.go's "server menolak: %s"). A read_only member confirming a
// prepared change got "Choose a workspace first" — advice they cannot act on,
// since the request carried their workspace, and the refusal they needed to read
// was the role. So the decision now branches on the API's machine CODE
// (lib/chatConfirm.ts, code `workspace.scopeRequired`), which is the only thing
// that can tell the two 400s apart.
//
// WHAT MAKES IT BITE. Two layers, and neither alone is enough:
//   * src/lib/chatConfirm.test.ts EXECUTES confirmDecisionFor, so a bare
//     `status === 400` there turns the non-scope case RED (the case that was
//     invisible while the decision lived inside the component).
//   * this file pins the PAGE: the outcome branch keeps the card out of "failed",
//     no `r.status === 400` survives anywhere in decide(), and the helper really is
//     the one the branch reads and really is imported.
// The card-footer half asserts the footer is keyed on chat.confirmFail ONLY when
// the state is "failed", and the dictionary half pins that a scopeRequired card
// cannot borrow the failure string: the two keys must not be the same sentence, or
// the "does not render confirmFail" property would be a distinction without a
// difference.
test("the confirm path's scope refusal is its own state, and cannot reach chat.confirmFail", () => {
  // THE BRANCH MOVED (t_fb393abe). What this guard pinned was the slice from
  // `if (r.status === 400) {` — i.e. the branch keyed on the BARE STATUS, which was
  // itself the defect this card fixes: any 400 answered "Choose a workspace first",
  // including a tool the server refused by ROLE after claiming the token. The
  // decision now lives in lib/chatConfirm.ts and is keyed on the API's machine
  // CODE, so the property this guard owns is re-anchored on the outcome branch.
  // The BEHAVIOUR of the decision (which 400 means what) is executed in
  // src/lib/chatConfirm.test.ts; what is pinned HERE is that the page keeps the
  // card out of the failure state for the scope outcome.
  const at = page.indexOf('if (outcome === "scopeRequired") {', page.indexOf("async function decide("));
  assert.ok(
    at !== -1,
    "app/chat/page.tsx has no scopeRequired outcome branch in decide() — the scope refusal would " +
      "either fall through to the generic failure branch (the user gets 'try again' for a condition " +
      "they fix by choosing a workspace) or be decided by the bare status again (t_fb393abe)",
  );
  const branch = page.slice(at, page.indexOf("return;", at) + "return;".length);

  assert.match(
    branch,
    /setState\(msgIndex,\s*p\.token,\s*"scopeRequired"\)/,
    `the scope refusal no longer marks the proposal "scopeRequired" — it would set a state the ` +
      `card renders as a failed change while the alert says to choose a workspace (both at once, ` +
      `which is the defect). Branch was:\n${branch}`,
  );
  assert.doesNotMatch(
    branch,
    /setState\(msgIndex,\s*p\.token,\s*"failed"\)/,
    "the scope refusal still marks the proposal \"failed\", so the card renders chat.confirmFail " +
      "('Please try again.') under an alert telling the user to choose a workspace — retrying without " +
      "a workspace fails identically, so the footer's advice is wrong",
  );
  assert.match(
    branch,
    /setErr\(t\("chat\.scopeRequired"\)\)/,
    "the scope refusal no longer renders the scope copy — the user would see only the card's " +
      "failure sentence and would not learn that choosing a workspace is the fix",
  );
  // The branch must be reached by the DECISION, not by a status test. A
  // `r.status === 400` left anywhere in decide() is the defect back: it cannot
  // tell the scope refusal from the post-claim tool refusal, and it renders the
  // scope copy for both. Counted rather than merely absent, because the point is
  // that NO status-only 400 branch survives.
  const fourHundreds = page
    .slice(page.indexOf("async function decide("))
    .match(/r\.status === 400/g);
  assert.equal(
    fourHundreds,
    null,
    `decide() still branches on the bare 400 status (${fourHundreds?.length} site(s)). That branch ` +
      `cannot distinguish a workspace-scope refusal from any other 400 the route answers — including ` +
      `the tool refusal that arrives AFTER the token is claimed — so it renders "Choose a workspace ` +
      `first" to a user who already had a workspace selected (t_fb393abe). The decision belongs to ` +
      `confirmDecisionFor (lib/chatConfirm.ts), which branches on the machine code.`,
  );
  // And the decision really is the one the branch reads: the outcome names must
  // come from the shared helper, not from a local re-implementation that could
  // drift from the tested one.
  assert.match(
    page.slice(page.indexOf("async function decide(")),
    /const outcome = confirmDecisionFor\(r\.status,\s*body\)/,
    "decide() no longer routes its answer through confirmDecisionFor — the branch would be deciding " +
      "what a response means by itself, which is the shape t_fb393abe is about",
  );
  assert.match(
    page,
    /import \{ confirmDecisionFor \} from "\.\.\/\.\.\/lib\/chatConfirm"/,
    "app/chat/page.tsx names confirmDecisionFor without importing it from lib/chatConfirm",
  );

  // The card's own footer. "failed" must be the ONLY state that renders
  // chat.confirmFail, and a scopeRequired card must not.
  const footerAt = page.indexOf('{state === "failed"');
  assert.ok(footerAt !== -1, "the card footer no longer renders a 'failed' state at all");
  const footer = page.slice(footerAt, page.indexOf("\n", page.indexOf("</p>", footerAt)));
  assert.ok(
    footer.includes('t("chat.confirmFail")'),
    "a genuine failure (5xx / network) on the confirm path no longer renders chat.confirmFail",
  );
  assert.ok(
    !new RegExp('state === "scopeRequired"[\\s\\S]{0,200}?chat\\.confirmFail').test(page),
    'the scope-required card renders chat.confirmFail — the card would tell a user with no workspace ' +
      'to "try again", which cannot work',
  );
  // The card must say what it NEEDS rather than wearing the red "did not go
  // through" badge over a request the server never ran.
  assert.match(
    page,
    /state === "scopeRequired"[\s\S]{0,120}?t\("chat\.proposalBadgeScopeRequired"\)/,
    'the scope-required card falls back to chat.proposalBadgeFailed ("Did not go through") — nothing ' +
      'was attempted, so the badge would claim a failure that did not happen',
  );
  assert.match(
    page,
    /type ProposalState\s*=[\s\S]{0,400}?"scopeRequired"/,
    "ProposalState has no scopeRequired member — the setState above would not type-check",
  );

  // The two strings must be DIFFERENT sentences, or "does not render
  // chat.confirmFail" would be satisfied by a key that says the same thing.
  const scopeCopy = DICT["chat.scopeRequired"].en;
  const failCopy = DICT["chat.confirmFail"].en;
  assert.notEqual(
    scopeCopy,
    failCopy,
    "the scope refusal and the generic confirm failure carry the SAME English sentence — the point of " +
      "the split was that they say different things, so one of the two has lost its copy",
  );
  assert.doesNotMatch(
    scopeCopy,
    /try again/i,
    `the scope refusal tells the user to "try again" ("${scopeCopy}") — retrying without a workspace ` +
      `fails identically; the actionable instruction is to choose a workspace`,
  );
  for (const lang of LANGS) {
    const v = (DICT["chat.proposalBadgeScopeRequired"] as Record<string, string>)[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `chat.proposalBadgeScopeRequired.${lang} is empty — the badge would render a blank pill`,
    );
  }

  // 409 (stale) and 429 (rate-limited) keep their own distinct copy: the two
  // other non-failure refusals this card names as unchanged. They are asserted on
  // the OUTCOME branch now, since the status test lives in lib/chatConfirm.ts
  // (whose behaviour chatConfirm.test.ts executes).
  const decide = page.slice(page.indexOf("async function decide("));
  assert.match(
    decide,
    /outcome === "stale"[\s\S]{0,300}?setState\(msgIndex,\s*p\.token,\s*"stale"\)/,
    "the stale outcome no longer marks the card stale — a spent proposal would keep offering buttons " +
      "the server refuses",
  );
  assert.match(
    decide,
    /outcome === "rateLimited"[\s\S]{0,300}?t\("chat\.confirmRateLimited"\)/,
    "the rate-limited outcome no longer renders chat.confirmRateLimited — a rate-limited user would " +
      "get the generic failure instead of the limit copy",
  );
});
