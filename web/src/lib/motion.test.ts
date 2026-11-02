// Unit guard for the motion layer (card t_d5336ec6).
//
// WHAT WENT WRONG BEFORE THIS CARD
//   The web app had 0 `@keyframes`, 0 `animation:` rules and 63 transitions that
//   all ran at Tailwind's default 150ms, and `prefers-reduced-motion` appeared
//   NOWHERE in `web/src`. So nothing entered, nothing acknowledged a submit, and
//   the one animation that existed ignored the OS accessibility setting.
//
// WHAT THIS PINNED
//   1. the six motion tokens exist with the exact values the card specifies —
//      a component may consume them, but the numbers live in one place;
//   2. no duration token is longer than 300ms (the card's ceiling);
//   3. `transition-all` never appears in the stylesheet — it transitions layout
//      properties, which is the one thing the card forbids;
//   4. the reduced-motion baseline is present, comes AFTER the patterns it has
//      to win over, collapses both animation and transition durations, and
//      zeroes the transform step tokens so movement is dropped, not just
//      accelerated.
//
// The `motionCssFindings` function takes the stylesheet as an argument on
// purpose: the control test hands it doctored bytes and requires it to name the
// defect. Without that, "the real stylesheet is clean" could mean only that the
// checker finds nothing anywhere.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const CSS = readFileSync(
  fileURLToPath(new URL("../app/globals.css", import.meta.url)),
  "utf8",
);

const REQUIRED_TOKENS: Record<string, string> = {
  "--jf-dur-fast": "120ms",
  "--jf-dur-base": "180ms",
  "--jf-dur-slow": "280ms",
  "--jf-ease-out": "cubic-bezier(0.22, 1, 0.36, 1)",
  "--jf-ease-in-out": "cubic-bezier(0.65, 0, 0.35, 1)",
  "--jf-rise": "4px",
};

const MAX_MS = 300;

/** Everything wrong with a stylesheet's motion layer, as a list. */
export function motionCssFindings(css: string): string[] {
  const out: string[] = [];

  for (const [name, value] of Object.entries(REQUIRED_TOKENS)) {
    if (!new RegExp(`${name}\\s*:\\s*${value.replace(/[()]/g, "\\$&")}\\s*;`).test(css)) {
      out.push(`token ${name} is not declared as ${value}`);
    }
  }

  for (const m of css.matchAll(/--jf-dur-[a-z]+:\s*(\d+)ms/g)) {
    const ms = Number(m[1]);
    if (ms > MAX_MS) out.push(`duration token ${m[0]} is ${ms}ms, over the ${MAX_MS}ms ceiling`);
  }

  if (/\btransition-all\b/.test(css)) {
    out.push("transition-all is present — it transitions every property, layout ones included");
  }

  const reduceAt = css.indexOf("@media (prefers-reduced-motion: reduce)");
  if (reduceAt < 0) {
    out.push("no @media (prefers-reduced-motion: reduce) baseline");
  } else {
    const lastPattern = Math.max(
      css.indexOf(".jf-stagger-row"),
      css.indexOf(".jf-panel-in"),
      css.indexOf(".jf-ok-in"),
    );
    if (lastPattern > reduceAt) {
      out.push("the reduced-motion block does not come after the patterns it must win over");
    }
    const tail = css.slice(reduceAt);
    if (!tail.includes("animation-duration")) {
      out.push("the reduced-motion block never collapses animation-duration");
    }
    if (!tail.includes("transition-duration")) {
      out.push("the reduced-motion block never collapses transition-duration");
    }
    if (!/--jf-rise:\s*0px/.test(tail)) {
      out.push("the reduced-motion block does not drop the transform (--jf-rise stays > 0)");
    }
  }
  return out;
}

test("the stylesheet's motion layer is complete", () => {
  assert.deepEqual(motionCssFindings(CSS), []);
});

test("every declared motion token has the value the card specifies", () => {
  for (const [name, value] of Object.entries(REQUIRED_TOKENS)) {
    assert.match(CSS, new RegExp(`${name}\\s*:\\s*${value.replace(/[()]/g, "\\$&")}\\s*;`));
  }
});

test("no duration token is longer than 300ms", () => {
  const durations = [...CSS.matchAll(/--jf-dur-[a-z]+:\s*(\d+)ms/g)].map((m) => Number(m[1]));
  assert.ok(durations.length >= 3, `expected the three duration tokens, got ${durations}`);
  assert.ok(durations.every((ms) => ms <= MAX_MS), `over the ceiling: ${durations}`);
});

test("the checker BITES: a missing baseline, a 400ms token and transition-all are named", () => {
  const noBaseline = CSS.replace("@media (prefers-reduced-motion: reduce)", "@media (prefers-reduced-motion: no-preference)");
  assert.ok(
    motionCssFindings(noBaseline).some((f) => f.includes("prefers-reduced-motion")),
    "removing the baseline did not bite",
  );

  const tooSlow = CSS.replace("--jf-dur-slow: 280ms", "--jf-dur-slow: 400ms");
  assert.ok(
    motionCssFindings(tooSlow).some((f) => f.includes("400ms")),
    "a 400ms duration did not bite",
  );

  const allProp = CSS + "\n.jf-oops { transition-all: 150ms; }\n";
  assert.ok(
    motionCssFindings(allProp).some((f) => f.includes("transition-all")),
    "a transition-all did not bite",
  );
});
