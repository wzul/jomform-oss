// The chat page's SEND cap and the server's RETENTION cap are one number, and this
// guard is what keeps them one number.
//
// THE DEFECT (t_4c29963a, measured on production, api v1.16.24/4238550). Three
// numbers described the same thing and agreed with none of the others:
//
//   web/src/app/chat/page.tsx         MAX_HISTORY = 32    turns the page SENT
//   api/internal/httpapi/rest_chat.go chatMaxHistory = 40 the edge's reject
//                                                         threshold for ONE request
//   api/internal/chat/chat.go         chat.MaxTurns = 16  turns the runner KEEPS
//
// and the page's comment credited the wrong one ("Mirrors the server's cap
// (chatMaxHistory in rest_chat.go)"), so the number it claimed to mirror was never
// a retention cap at all. The consequence was silent in the worst direction:
// signed in, a 19-turn history whose FIRST turn carried a unique passphrase came
// back "Tiada. Sepanjang perbualan ini (giliran 4 hingga 19) tiada kata laluan
// disebut" — the model citing the exact window it could see (the last 16) while the
// user could still scroll turns 1-3, and nothing on the page said the two
// transcripts had different lengths.
//
// THE FIX. The page no longer carries a second copy of the number: it sends the
// last `limits.max_history_turns` turns — the cap the availability probe reports,
// which IS chat.MaxTurns (chatLimitsOut.MaxHistoryTurns is that constant, pinned on
// the Go side by TestChatLimitsReportsEveryEnforcedCap) — and MAX_HISTORY survives
// only as the fallback used before the probe answers. THAT is the property pinned
// here, so the client and the runner cannot drift apart again.
//
// WHY A SOURCE SCAN. This repo has no React/DOM test runner (see src/lib/i18n.test.ts),
// so the send cap exists only in the page source and the retention cap only in Go.
// Both are read here — the idiom of homeServerContent.test.ts (sessionCookie.ts) and
// chatPage.test.ts: one property living in two languages, compared rather than
// copied. The rule is a pure function over its three sources so the bite test below
// can run the SAME rule against the pre-fix page; a proof that exercised a
// paraphrase would prove nothing about this rule.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot execute React, so it does not observe what the page puts on the
//     wire; it pins the source that decides it. The browser half is in the PR.
//   * It does not bound the transcript ON SCREEN. The page renders every message it
//     holds; the model is given the last `max_history_turns`. That bound is the
//     server's retention bound and is documented for the user on /docs/chat ("The
//     conversation you send is bounded to the last 16 turns ... Older turns are
//     dropped from what the model sees").
//   * It says nothing about the OTHER chat caps (rate, message length, tool steps);
//     those are pinned by chatPage.test.ts and the Go limits guard.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const page = read("../app/chat/page.tsx");
// The Go side of the same number, read cross-language on purpose: "how much history
// does the model get" is one value living in two languages, and the page trusting a
// different one is exactly the defect.
const goChat = read("../../../api/internal/chat/chat.go");
const goRest = read("../../../api/internal/httpapi/rest_chat.go");

/**
 * The number a Go `const X = N` is declared with, read out of its source.
 *
 * NOT a copy of the value (the idiom of chatPage.test.ts's goConstNumber): a "16"
 * written here would be a fourth copy of the number and a fifth thing to edit — the
 * defect this guard exists for, wearing a guard's clothes. Line-anchored, so a
 * comment that merely mentions the name cannot be read as its declaration, and a
 * name that is gone (or no longer a plain integer) FAILS rather than handing the
 * comparisons `undefined`.
 */
function goIntConst(src: string, name: string): number {
  const m = src.match(new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)\\b`, "m"));
  assert.ok(
    m,
    `${name} is not declared as an integer literal in the Go source this guard reads — retarget the ` +
      `guard at whatever replaced it instead of comparing against nothing`,
  );
  return Number(m[1]);
}

/**
 * The JSON name of the cap the API reports, read from the Go struct TAG.
 *
 * Read rather than written here for the same reason as the value: the wire name is
 * one value in two languages, and a rename on the Go side must not leave the page
 * reading a field that no longer exists (which would silently fall back to the
 * constant on every request — the pre-fix shape, with no error anywhere).
 */
function reportedCapField(src: string): string {
  const m = src.match(/MaxHistoryTurns\s+int\s+`json:"([^",]+)/);
  assert.ok(
    m,
    "chatLimitsOut no longer declares MaxHistoryTurns with a json tag — this guard reads the wire " +
      "name from there; retarget it rather than letting the page read a field nothing sends",
  );
  return m[1];
}

/**
 * The problems with one literal cap — `const <name> = <n>` — that the wire can be
 * built from, judged against the runner's retention cap.
 *
 * Shared by the TWO places a literal cap can hide (the derivation's fallback, and
 * any literal the transcript is directly sliced by), so the comparison is stated
 * once and neither site can be the one nobody checks.
 */
function literalCapProblems(name: string, n: number, retained: number): string[] {
  const out: string[] = [];
  if (n > retained) {
    out.push(
      `${name} is ${n} turns but the runner keeps only ${retained} (chat.MaxTurns in ` +
        `api/internal/chat/chat.go): turns ${retained + 1}..${n} would be sent and silently dropped — ` +
        `the 32-against-16 divergence this guard exists for`,
    );
  }
  if (n < 1) {
    out.push(`${name} is ${n}: slice(-0) is the WHOLE transcript, not a cap`);
  }
  return out;
}

/**
 * Everything wrong with one (page, rest_chat.go, chat.go) triple, as sentences.
 *
 * Pure over its three sources so the bite test can mutate exactly one of them and
 * show the SAME rule red (the idiom of chatPage.test.ts's BITES map).
 */
function historyCapProblems(pageSrc: string, restSrc: string, chatSrc: string): string[] {
  const out: string[] = [];

  // The three numbers, each read from where it is declared.
  const retained = goIntConst(chatSrc, "MaxTurns");
  const edge = goIntConst(restSrc, "chatMaxHistory");
  const wire = reportedCapField(restSrc);

  // ── 1. THE PAGE SENDS WHAT THE SERVER KEEPS ────────────────────────────────
  // The cap must come from the reported limit — the one place that number is
  // stated — with the constant only as the pre-probe fallback, and the value the
  // transcript is actually sliced by must be that derived cap.
  const derivations = [
    ...pageSrc.matchAll(/const\s+(\w+)\s*=\s*limits\?\.(\w+)\s*(?:\|\||\?\?)\s*(\w+);/g),
  ];
  const sliced = [...pageSrc.matchAll(/\.slice\(-(\w+)\)/g)].map((m) => m[1]);
  const cap = derivations.find((d) => sliced.includes(d[1]));
  if (!cap) {
    out.push(
      "the page no longer sends the last limits.max_history_turns turns: there is no " +
        "`const <cap> = limits?.<field> || <FALLBACK>;` whose value the transcript is sliced by. " +
        "A cap written as a literal is the 32-against-16 divergence this guard exists for",
    );
  } else {
    const [, , field, fallbackName] = cap;
    if (field !== wire) {
      out.push(
        `the page reads limits.${field} but the API reports the cap as "${wire}" — the page would ` +
          `silently fall back to its constant on every request`,
      );
    }
    const declared = pageSrc.match(
      new RegExp(`^\\s*const\\s+${fallbackName}\\s*=\\s*(\\d+)\\b`, "m"),
    );
    if (!declared) {
      out.push(
        `${fallbackName} is not declared as an integer literal, so the guard cannot compare it to ` +
          `chat.MaxTurns — the fallback is the one place a literal cap is still allowed, so it has ` +
          `to be readable`,
      );
    } else {
      out.push(...literalCapProblems(fallbackName, Number(declared[1]), retained));
    }
  }
  // Nothing may slice the transcript by something that is not the derived cap:
  // `.slice(-MAX_HISTORY)` is the pre-fix shape (32 sent, 16 kept).
  for (const v of sliced) {
    if (!derivations.some((d) => d[1] === v)) {
      out.push(`the transcript is sliced by ${v}, which is not the cap derived from the reported limit`);
    }
    // A literal the wire can be BUILT from is compared to the retention cap even
    // when it is not the fallback — this is the half that reds the page exactly as
    // it shipped (`const MAX_HISTORY = 32` + `.slice(-MAX_HISTORY)`, which has no
    // derivation at all to hang the comparison on).
    const declared = pageSrc.match(new RegExp(`^\\s*const\\s+${v}\\s*=\\s*(\\d+)\\b`, "m"));
    if (declared) out.push(...literalCapProblems(v, Number(declared[1]), retained));
  }

  // ── 2. THE REPORTED CAP IS THE RETENTION CAP ───────────────────────────────
  // The page trusts this number; if the endpoint reported one the runner does not
  // enforce, the client would track a cap nobody applies.
  if (!/MaxHistoryTurns:\s*chat\.MaxTurns\b/.test(restSrc)) {
    out.push(
      "chatLimits() no longer reports MaxHistoryTurns: chat.MaxTurns — the page would track a cap " +
        "the runner does not enforce",
    );
  }

  // ── 3. THE EDGE THRESHOLD IS A DIFFERENT, HIGHER NUMBER ────────────────────
  // chatMaxHistory rejects an implausible transcript; it must never sit below what
  // the runner keeps, or a request the runner could have answered is refused at the
  // edge. It is also the number the page's comment used to credit with retention.
  if (!(edge > retained)) {
    out.push(
      `the edge's reject threshold (chatMaxHistory = ${edge}) is not above the runner's retention ` +
        `cap (chat.MaxTurns = ${retained}): the edge would refuse a transcript the runner can keep`,
    );
  }

  // ── 4. THE COMMENT NAMES THE RIGHT CONSTANT ────────────────────────────────
  // The shipped comment credited chatMaxHistory with a retention it never had
  // ("Mirrors the server's cap (chatMaxHistory in rest_chat.go)"), and THAT is how
  // the 32 looked justified. So: the page must name the constant that governs
  // retention, and must not claim its cap mirrors the edge's reject threshold.
  //
  // The second half reads the CLAIM VERB, not the name: the page is allowed to name
  // chatMaxHistory in order to say what it is NOT (the three-numbers story is worth
  // having where the cap is defined), and it is the claim that was wrong. This is
  // prose, so it is the weaker half of the rule and says so: a re-worded claim that
  // uses none of these verbs would slip — the STRUCTURAL half (rules 1-3) is what
  // makes the number itself impossible to get wrong.
  if (/(?:mirror\w*|same as|matches?)[^.\n]{0,100}chatMaxHistory/i.test(pageSrc) ||
      /chatMaxHistory[^.\n]{0,100}(?:mirror\w*|same as|matches?)/i.test(pageSrc)) {
    out.push(
      "the page's source still claims its cap mirrors chatMaxHistory — that is rest_chat.go's reject " +
        "threshold for an implausible transcript, not the number of turns the runner keeps",
    );
  }
  if (!/chat\.MaxTurns/.test(pageSrc)) {
    out.push(
      "the page's source does not name chat.MaxTurns (api/internal/chat/chat.go), the constant that " +
        "decides how much history the model is given — the page's own comment is where the next " +
        "reader looks for the cap",
    );
  }

  return out;
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(page.includes("chat-input"), "app/chat/page.tsx is not the chat page source");
  assert.ok(goChat.includes("func (r *Runner) Run"), "api/internal/chat/chat.go is not the runner source");
  assert.ok(goRest.includes("chatLimits"), "api/internal/httpapi/rest_chat.go is not the REST source");
  // The three numbers are readable, and readable as numbers: a constant that is
  // gone or no longer a literal would make every comparison below meaningless.
  const retained = goIntConst(goChat, "MaxTurns");
  const edge = goIntConst(goRest, "chatMaxHistory");
  assert.ok(retained > 0 && edge > 0, `read MaxTurns=${retained}, chatMaxHistory=${edge}`);
  assert.equal(reportedCapField(goRest), "max_history_turns", "the reported cap's wire name moved");
});

test("the page sends no more history than the runner keeps, and takes the cap from the reported limit", () => {
  const problems = historyCapProblems(page, goRest, goChat);
  assert.deepEqual(
    problems,
    [],
    `the chat page's history cap and the server's retention cap disagree:\n  - ${problems.join("\n  - ")}`,
  );
});

// ── THE BITE ──────────────────────────────────────────────────────────────────
//
// Each mutation is the shape that was SHIPPED (or that a revert would produce),
// applied to the real page source, and the SAME rule is asked for its problems.
// Every mutation is asserted to have actually changed the source first: a
// .replace() that silently matched nothing would make this test pass while proving
// nothing, which is the failure mode every guard in this repo has been bitten by.
test("the guard bites: each pre-fix shape is RED under this rule", () => {
  const mutations: Array<{
    what: string;
    want: RegExp;
    page?: string;
    rest?: string;
    chat?: string;
  }> = [
    {
      what: "the pre-probe fallback is the shipped 32 again",
      page: page.replace(/const MAX_HISTORY = \d+;/, "const MAX_HISTORY = 32;"),
      want: /the runner keeps only 16/,
    },
    {
      what: "the cap is a literal again instead of the reported limit",
      page: page.replace(
        /const historyCap = limits\?\.max_history_turns \|\| MAX_HISTORY;/,
        "const historyCap = MAX_HISTORY;",
      ),
      want: /no longer sends the last limits\.max_history_turns/,
    },
    {
      what: "the transcript is sliced by the constant, not the derived cap",
      page: page.replace(".slice(-historyCap)", ".slice(-MAX_HISTORY)"),
      want: /is not the cap derived from the reported limit/,
    },
    {
      what: "the shipped comment crediting chatMaxHistory with retention",
      page:
        page +
        "\n// Mirrors the server's cap (chatMaxHistory in rest_chat.go). The server drops older turns.\n",
      want: /still claims its cap mirrors chatMaxHistory/,
    },
    {
      what: "the endpoint reports the edge's threshold instead of the retention cap",
      rest: goRest.replace("MaxHistoryTurns: chat.MaxTurns", "MaxHistoryTurns: chatMaxHistory"),
      want: /no longer reports MaxHistoryTurns: chat\.MaxTurns/,
    },
    {
      what: "the wire name the page reads is renamed on the Go side",
      rest: goRest.replace('json:"max_history_turns"', 'json:"max_history"'),
      want: /the API reports the cap as "max_history"/,
    },
    {
      what: "the edge's reject threshold drops onto the retention cap",
      rest: goRest.replace(/chatMaxHistory = \d+/, "chatMaxHistory = 16"),
      want: /is not above the runner's retention cap/,
    },
    {
      what: "the runner's retention cap drops below the page's fallback",
      chat: goChat.replace(/MaxTurns = \d+/, "MaxTurns = 8"),
      want: /the runner keeps only 8/,
    },
  ];

  for (const m of mutations) {
    const p = m.page ?? page;
    const r = m.rest ?? goRest;
    const c = m.chat ?? goChat;
    assert.notEqual(
      `${p}\u0000${r}\u0000${c}`,
      `${page}\u0000${goRest}\u0000${goChat}`,
      `the mutation "${m.what}" changed nothing — this proof would be vacuous`,
    );
    const problems = historyCapProblems(p, r, c);
    assert.ok(
      problems.some((s) => m.want.test(s)),
      `"${m.what}" is NOT caught by this rule (expected a problem matching ${m.want}):\n` +
        `  reported: ${problems.length ? problems.join("\n  - ") : "(nothing)"}`,
    );
  }
});
