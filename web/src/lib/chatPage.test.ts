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

const page = read("../app/chat/page.tsx");
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
/** The determiners that can open the gerund's object. */
const DET = `(?:the|a|an|your|my|his|her|its|their|our|this|that|these|those|some|any)`;
/** The prepositions that can open a phrase that continues the gerund's noun phrase. */
const G_PREP = `(?:from|to|into|out\\s+of|onto|upon|in|on|at|with|for|about|by|via|through|across|under|over|within|without|before|after|during|against|between|among|of)`;
// The gerund's OWN OBJECT — one common noun, the shape the object reading uses
// ("a form", "forms", "the trash"). Held to ONE noun on purpose: a second noun here is
// indistinguishable from the reported clause's finite verb ("… a form CONTINUES"), and
// reading that verb as part of the object would green a genuine claim.
const NP_OBJ = `(?:${DET}\\s+)?[a-z]+`;
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
function reportedGerundIsSubject(rest: string): boolean {
  // The gerund and its noun phrase: the gerund, an optional one-noun object, any PPs.
  const phrase = new RegExp(`${GER_HEAD}(?:\\s+${NP_OBJ})?(?:${GER_PP})*`, "i").exec(rest);
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
 * The assistant's OWN verb in a match (A) made against the normalised clause — the verb
 * that decides whether the later bin token is an action or an object of knowledge.
 * Returns null when the verb is the assistant's own bin verb ("the assistant bins"),
 * which is the claim itself.
 */
function assistantVerb(matched: string): string | null {
  const m = /^assistant\b\s+/i.exec(matched);
  if (!m) return null;
  let rest = matched.slice(m[0].length);
  const aux = new RegExp(`^(?:${ASSISTANT_AUX})\\b\\s*`, "i").exec(rest);
  if (aux) rest = rest.slice(aux[0].length);
  const verb = /^[a-z]+(?:'[a-z]+\b)?(?:\s+up\b)?/i.exec(rest);
  return verb ? verb[0].toLowerCase() : null;
}

/** The copulas: an auxiliary that can make the bin token a PASSIVE on the thing. */
const COPULA_AUX = /^(?:is|are|was|were|be|been|being)$/;

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
function assistantBinsInsideReport(matched: string, fullClause: string = matched): boolean {
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
    return !reportedGerundIsSubject(rest);
  }
  const claim = new RegExp(
    `^\\s+(?:it|they|he|she|the\\s+assistant)\\b(\\s+(?:${ASSISTANT_AUX}))?\\s+(${BIN_VOCAB})\\b`,
    "i",
  ).exec(rest);
  if (!claim) return false;
  const aux = (claim[1] ?? "").trim().toLowerCase();
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
function binClaimInClause(raw: string): string | null {
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
      // is reporting a claim, not naming the bin (see `assistantBinsInsideReport`).
      const verb = assistantVerb(m[0]);
      if (!verb || !HAS_NON_ACTION.test(verb) || assistantBinsInsideReport(m[0], s)) return m[0];
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
function assistantBinClaims(text: string): string[] {
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
    const m = binClaimInClause(folded);
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
  const docs = read(DOCS_PAGE);
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
      docs.includes(want),
      `${DOCS_PAGE} no longer says "${want}" (${what}) — the docs quote the cap the server ` +
        `enforces, so the constant and this sentence cannot move apart`,
    );
  }

  // ── 2. the dictionary the page shows on a 429 ────────────────────────────
  // Digit presence, not the whole phrasing: a translation is free to be reworded
  // (and is, in four locales), but the NUMBER a user is told on a refusal has to
  // be the enforced one. The live rate-limit note (chat.limitNote) needs no pin
  // here — the page fills its {n} from the wire's limits block, which PR 695's
  // Go guard already ties to the constant.
  const dictCaps: Array<{ key: string; n: number; what: string }> = [
    { key: "chat.rateLimited", n: msgPerHour, what: "message cap" },
    { key: "chat.confirmRateLimited", n: confirmPerHour, what: "confirmation cap" },
  ];
  for (const { key, n, what } of dictCaps) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(entry, `${key} is gone from the dictionary — the page shows this string on a 429`);
    for (const lang of LANGS) {
      assert.ok(
        String(entry[lang]).includes(String(n)),
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

test("a refused tool call is shown, not swallowed", () => {
  assert.match(
    page,
    /"refused"/,
    "the page no longer separates refused steps from successful ones",
  );
  assert.ok(
    page.includes("chat.refused"),
    "a refusal has no message — the user would read a silent refusal as a broken answer",
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
  // run alone, then reverted):
  //   * the 400 branch deleted            -> RED here (19/20)
  //   * `setAvailable(false)` replaced by the scope setErr on the generic path
  //     -> RED here (19/20), because the unavailable branch must survive
  // The browser-level half of the same defect is e2e/chat-probe-400.spec.ts,
  // which drives the built page and fails on the pre-fix build with
  // "locator('p[role=\"alert\"]') element(s) not found".
  //
  // The branch is located by its own site, the runLoad outcome's `!r.ok` guard —
  // not by a line number — and the slice runs to the statement after that block
  // (`setAvailable(r.data.available …)`), so it covers the WHOLE !r.ok branch
  // including the generic fall-through the scope branch must not replace.
  const probeAt = page.indexOf("const r = await runLoad(");
  assert.ok(
    probeAt !== -1,
    "app/chat/page.tsx no longer reads availability through runLoad — this guard cannot judge the " +
      "probe's status handling and refuses to guess",
  );
  const probeFrom = page.indexOf("if (!r.ok) {", probeAt);
  assert.ok(
    probeFrom !== -1,
    "the chat availability probe has no !r.ok branch — the scope refusal would be consumed by " +
      "runLoad and the page would offer a box whose every message 400s",
  );
  const probeEnd = page.indexOf("setAvailable(r.data.available", probeFrom);
  assert.ok(
    probeEnd > probeFrom,
    "could not find the statement after the availability probe's !r.ok branch — this guard refuses " +
      "to slice a branch it cannot delimit",
  );
  const probeBranch = page.slice(probeFrom, probeEnd);
  assert.match(
    probeBranch,
    /if \(r\.status === 400\) \{\s*setErr\(t\("chat\.scopeRequired"\)\);\s*return;/,
    `the availability probe's !r.ok branch does not answer a 400 with t("chat.scopeRequired") — a ` +
      `two-workspace session with no workspace selected would read "The assistant is not available ` +
      `right now." (chat.unavailable) instead of the scope copy, and the composer would be ` +
      `disabled. Branch was:\n${probeBranch}`,
  );
  // And the generic branch survives: a 5xx / network failure is still
  // "unavailable", so the scope branch cannot have replaced it.
  assert.match(
    probeBranch,
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

  // Routes whose data is the PERSON's, not an account's (see above).
  const USER_LEVEL = /\/api\/rest\/(feedback|uploads\/avatar|profile\/avatar)/;
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
    // Every /api/rest/... URL this page builds as a string/template literal.
    const urls = [...code.matchAll(/[`"](\/api\/rest\/[^`"]*)[`"]/g)].map((m) => m[1]);
    const accountScoped = urls.filter((u) => !USER_LEVEL.test(u));
    if (accountScoped.length === 0) continue;
    subjectPages += 1;
    const scoped = code.includes("workspaceQuery");
    if (!scoped) {
      offenders.push(
        `${relative(appDir, abs)}: calls ${JSON.stringify([...new Set(accountScoped)])} with no ` +
          `workspaceQuery() — the server resolves Accounts.ByOwner(...)[0], the user's FIRST ` +
          `account, not the workspace they selected`,
      );
    }
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

test('the inline "?" doc link points at the docs page that documents the boundary', () => {
  assert.ok(
    page.includes('href="/docs/chat"'),
    'the assistant has no inline "?" doc link (AGENTS.md DOCUMENTATION PARITY step 4)',
  );
  const docs = read(DOCS_PAGE);
  // The page must say what the assistant CANNOT do — that is the feature.
  assert.ok(
    /cannot change/i.test(docs) || /cannot delete/i.test(docs),
    "the docs page does not state what the assistant cannot do — the boundary is the whole design",
  );
  assert.ok(
    docs.includes("submit_feedback"),
    "the docs page does not name the MCP tool behind the one write (submit_feedback)",
  );
  // Phase 2: the docs page must document the confirmation lane too — how a
  // proposal is confirmed, and that the assistant cannot confirm it itself.
  assert.ok(
    docs.includes("propose_write"),
    "the docs page does not name propose_write — the phase-2 tool would be undocumented " +
      "(AGENTS.md DOCUMENTATION PARITY step 2)",
  );
  assert.ok(
    /confirm/i.test(docs),
    "the docs page does not explain confirmation — the whole phase-2 design is that a human " +
      "confirms and the server executes",
  );
  // The workspace scope is user-VISIBLE: a member of two workspaces can hit the
  // refusal, so it must be documented where the inline "?" leads, or the user
  // reads a 403 with no explanation of what to do about it.
  assert.ok(
    /selected workspace/i.test(docs),
    "the docs page does not say a confirmation writes in the SELECTED workspace — a user who hits " +
      "the scope refusal has nothing telling them why, or that choosing a workspace is the fix",
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
test("the docs say the assistant may CREATE a form, and that a permanent purge stays human-only", () => {
  const docs = read(DOCS_PAGE);

  // Vacuity, in the file's idiom: prove this is the page that carries the section,
  // and the Go file that carries the set, before either claim is believed.
  assert.ok(
    docs.includes("What it may propose"),
    `${DOCS_PAGE} is not the chat docs page this guard means to judge (its "What it may propose" ` +
      `section is missing)`,
  );
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

  // ── 1. The sentence that shipped the contradiction cannot come back. ──
  assert.doesNotMatch(
    docs,
    /creating anything new/i,
    "the docs page claims the proposable set excludes \"creating anything new\" again — #958 " +
      "(t_804040be) makes upsert_form proposable, so that sentence is the shipped defect",
  );
  // The surrounding sentence it lived in is gone too: the old copy framed the whole
  // set as field edits with "no new object created", which is the same false claim
  // one clause longer.
  assert.doesNotMatch(
    docs,
    /no new object created/i,
    "the docs page still describes the set as field edits with \"no new object created\" — a " +
      "confirmed upsert_form card CAN create a new form, so the framing is false",
  );

  // ── 2. The creation is stated, and it says WHICH HALF. ──
  // `upsert_form` is BOTH a create and an update, so a page that only said "edits
  // a form" would misdescribe a card that is about to create one. The Go summary
  // (proposalDocs["upsert_form"].summary) names both halves for exactly this
  // reason; the English page has to say the same thing.
  assert.ok(
    docs.includes("upsert_form"),
    "the docs page no longer names upsert_form — the proposable creation would be undocumented, " +
      "and the page is the inline \"?\" the chat panel points at (AGENTS.md DOCUMENTATION PARITY)",
  );
  assert.match(
    docs,
    /upsert_form[\s\S]{0,200}(CREATES a new form|creates a new form)[\s\S]{0,120}(UPDATES|updates)/,
    "the docs page names upsert_form but does not say it CREATES a new form (when it names none) " +
      "as well as UPDATES one it names — the two halves are the difference the confirmation is FOR, " +
      "and the Go card summary states both (proposal.go proposalDocs[\"upsert_form\"])",
  );
  // The claim is only true while the tool is really proposable — read from Go, not
  // asserted from the page's own words.
  assert.ok(
    proposable.includes("upsert_form"),
    `the docs page tells the user a confirmed card can create a form, but upsert_form is NOT in ` +
      `chat_tools.go's chatProposableTools (${proposable.join(", ")}) — the page and the server ` +
      `disagree, and the page is wrong`,
  );

  // ── 3. The bin is named — it is the whole reason the creation is admissible. ──
  // Wan's decision was "the assistant may take it as far as the bin; only a person
  // purges". A page that allowed the creation but never mentioned the bin would
  // leave the user without the recovery path the decision rests on.
  assert.match(
    docs,
    /(to the trash|the trash|from the trash)/i,
    "the docs page allows a created form but never names the trash/bin — the recoverability of a " +
      "mistaken creation is the entire justification for letting the assistant create one " +
      "(Wan, t_804040be: \"assistant boleh buat sampai masuk ke bin\")",
  );

  // ── 4. The permanent step is stated as human-only, and it really is. ──
  assert.ok(
    docs.includes("purge_form"),
    "the docs page never names purge_form — the ONE step with no inverse is the thing a user " +
      "confirming a created form most needs to be told about",
  );
  assert.match(
    docs,
    /purge_form[\s\S]{0,200}(never propose|cannot propose|never be proposed|is not proposable)/i,
    "the docs page names purge_form but does not say the assistant can never propose it — a reader " +
      "would be left believing a permanent delete is one click away in the chat, which is the " +
      "opposite of Wan's rule (\"Manusia sahaja boleh purge kekal\")",
  );
  // And the page's claim is true: purge_form must NOT be proposable.
  assert.ok(
    !proposable.includes("purge_form"),
    "chat_tools.go's chatProposableTools contains purge_form — the docs page says the assistant " +
      "can never propose a permanent delete, and it now can. Wan's rule: the assistant takes a " +
      "form as far as the bin; only a person purges (chat_tools_guard_test.go's " +
      "TestChatNeverProposesAPermanentDelete pins the same rule from the Go side)",
  );

  // ── 5. The bin is the PERSON's action, not the assistant's. ──
  // The creation above is admissible because a form is BINNED, not destroyed — but
  // the bin itself is not an assistant capability. delete_form/restore_form are not
  // proposable AND not in the chat's read allowlist, so the chat has no lane to bin
  // or restore a form at all; only purge_form is called out as human-only, yet it is
  // not the only step that is. The rework copy shipped the same class of defect it
  // set out to fix, one claim over: "the assistant can take a form as far as the
  // trash" credited the model with an action the server gives it no route to take.
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
  // And the page itself: no sentence may name the assistant as the AGENT that bins,
  // trashes, restores or deletes a form. The page MAY name the trash — the fix does,
  // to attribute the action to the person — so the guard keys on the assistant as
  // the actor, not on the word "trash" appearing anywhere and NOT on adjacency. The
  // scan is STRUCTURAL (see the arm's header): it normalises each clause and asserts
  // the assistant is the subject or the agent of a bin verb, so every phrasing below
  // — the infinitive ("ask the assistant to bin"), the passive ("…binned by the
  // assistant"), the bare verb ("the assistant bins"), the perfect ("the assistant
  // has binned"), the periphrastic future ("the assistant is going to delete"), the
  // relative clause ("the assistant is the one who bins"), the possessive genitive
  // ("binning a form is the assistant's job"), the fronted gerund ("restoring a form
  // is something the assistant does"), the appositive ("the assistant, not you,
  // deletes"), the light verb + gerund ("the assistant handles the binning"), the
  // copular predication ("binning is what the assistant is for"), the topicalised
  // cleft ("it is to the assistant that the form is binned"), the reversed nominal
  // ("the binner of a form is the assistant") and the non-restrictive relative
  // ("the assistant, whose job is to bin a form") — is RED by the same rule, not by
  // a shape written for it (the actor-adjacency shape this replaces caught only the
  // one phrasing the rework happened to remove).
  const credited = assistantBinClaims(docs);
  assert.deepEqual(
    credited,
    [],
    "the docs page credits the ASSISTANT with binning, trashing, restoring or deleting a form — " +
      "delete_form/restore_form are not proposable and not in the chat's read allowlist, so the " +
      "bin is the person's action on the forms page, never the assistant's. Offending phrasing(s): " +
      `${JSON.stringify(credited)}. (The shipped rework copy said \"the assistant can take a form as ` +
      "far as the trash\"; the infinitive, passive and bare-verb re-wordings of that claim are the " +
      "same defect one phrasing over.)",
  );
  // Positive control: the page must actually attribute the bin to the person rather
  // than dropping the recovery path that justifies the creation. A copy that simply
  // deleted the false sentence would pass a negative-only guard while leaving the
  // reader with no idea who bins the form.
  assert.match(
    docs,
    /bin belongs to (you|the person|the owner)|not the assistant[\s\S]{0,60}(forms page|trash)|you[\s\S]{0,40}move[s]? it to the trash/i,
    "the docs page never says the BIN is the person's action — a reader told the assistant may " +
      "create a form needs to know who can bin it, and the page must not leave that to the " +
      "assistant (Wan, t_804040be: \"assistant boleh buat sampai masuk ke bin\"; the bin is the " +
      "person's steps on the forms page)",
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
test("BITES: the assistant-credits-the-bin prose arm catches the infinitive, the passive, the bare verb, the perfect, the future, the relative clause, both genitive orders, the gerund, both appositive variants, the it-cleft, the periphrastic passive, the nominal, the light verb + gerund, the copular predication, the topicalised cleft, the reversed nominal, the non-restrictive relative, the pro-verb across a clause boundary, the claim reported inside an information verb's clause and the bin gerund such a verb reports", () => {
  const shipped = read(DOCS_PAGE);
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
