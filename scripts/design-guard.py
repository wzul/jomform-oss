#!/usr/bin/env python3
"""design-guard — the design-system governance gate for the web tree.

WHY THIS EXISTS
  The web app has a working token layer (the `--jf-*` custom properties in
  `web/src/app/globals.css`, consumed ~1,100 times) and NOTHING enforcing it.
  Measured on `origin/main` 2026-11-01: 188 raw `#rrggbb` literals across 23
  `.tsx` files — three different indigos standing in for one brand (`#5e6ad2`,
  `#7170ff`, `#4f46e5`) — and 250 arbitrary `text-[Npx]` font sizes in six
  ad-hoc steps. A colour or size typed at a call site is a decision nobody
  reviewed, and the drift is invisible because the rendered page still looks
  right. This gate is what makes a raw literal a BUILD FAILURE instead.

WHAT FAILS
  A `web/src/**/*.tsx` file that contains, in CODE (comments are not code —
  the reasoning comments in this repo quote hex values on purpose):

    raw-hex        `#rrggbb`, or a 3-digit `#rgb` that carries a letter
                   (`#fff`), outside the documented exemptions below.
                   Use `var(--jf-…)`; the token must be declared in
                   `globals.css`, which is NOT scanned (it is where literals
                   are allowed to live).
    type-scale     `text-[<n>px]`. Use the named step from the `@theme` type
                   scale (`text-jf-4xs` … `text-jf-md`), which preserves the
                   pixel value — a rename, not a redesign.
    transition-all `transition-all`. It transitions EVERY property, including
                   width/height/top/left, so it turns a resize into a per-frame
                   layout. Name the properties (`transition-colors`).
    raw-duration   `duration-<n>ms` or `duration-[<n>ms]`. Tailwind's default is
                   150ms and the app's steps are design tokens
                   (`--jf-dur-fast|base|slow`); a number typed at a call site is
                   the same defect a raw hex is.
    motion-baseline (not a .tsx rule) `web/src/app/globals.css` MUST carry an
                   `@media (prefers-reduced-motion: reduce)` block that collapses
                   `animation-duration` and `transition-duration`. Motion that
                   ignores the OS accessibility setting is a defect, and a card
                   that ships animation without this block fails here.

  A 3-digit ALL-DIGIT literal (`#503`) is NOT flagged: it is indistinguishable
  from an issue reference, and this guard must never red a correct tree. Use a
  six-digit value or a token.

DOCUMENTED EXCEPTIONS
  A literal that genuinely cannot be a token — a third-party brand mark, a
  concrete colour value round-tripped through `<input type="color">`, the brand
  palette page whose hex values ARE the content — carries a directive. Two
  forms, both needing a reason of at least 8 characters:

      // TRAILING — exempts its own line only
      const [c, setC] = useState("#7c3aed"); // design-tokens-exempt: <why>

      // BLOCK — exempts every line up to the `-end` marker
      // design-tokens-exempt: <why a token cannot be used here>
      ...
      // design-tokens-exempt-end

  A directive with no reason, a block that is never closed, a stray `-end` and a
  nested block are each a FAILURE of the guard (exit 2), not a shrug: an
  exemption nobody had to justify, or one with no boundary, is a hole. Every
  exemption the guard honours is printed on the pass line so the list is
  auditable rather than invisible.

USAGE
  scripts/design-guard.py [--root DIR]

  exit 0  clean          exit 1  findings          exit 2  cannot scan

  `--root` exists for the harness (`scripts/design-guard_test.sh`), which runs
  the same scanner over a sandbox tree so its verdicts are attributable.
"""
from __future__ import annotations

import argparse
import os
import re
import sys

# A six-digit value. `(?![0-9a-fA-F])` stops `#12345` matching a six-digit run
# it does not have (an 8-digit `#rrggbbaa` is caught by its six-digit head).
HEX6 = re.compile(r"#([0-9a-fA-F]{6})(?![0-9a-fA-F])")
# A three-digit value that carries a LETTER. All-digit three-character runs are
# ambiguous with an issue reference (`#503`), and this guard must not red a
# correct tree — see the module docstring. The lookahead refuses a WORD
# continuation as well as a hex digit, because a URL fragment (`#affiliate…`,
# `#aff`) is not a colour: measured on this tree, `#affiliate-marketing` in two
# docs pages was the whole of the 3-digit false-positive class.
HEX3 = re.compile(r"(?<![&\w])#([0-9a-fA-F]{3})(?![0-9a-zA-Z_-])")
TEXTPX = re.compile(r"text-\[(\d+)px\]")
# `transition-all` transitions EVERY property — the layout properties included.
# Word-boundary guarded so `transition-colors` / `transition-opacity` pass.
TRANSITION_ALL = re.compile(r"\btransition-all\b")
# A duration typed at a call site, in either shape Tailwind accepts:
# `duration-150ms` and `duration-[150ms]`. The app's steps are tokens
# (--jf-dur-fast|base|slow); a number here is the animation's version of a raw
# hex literal.
DURATION_MS = re.compile(r"\bduration-(?:\[\d+ms\]|\d+ms)(?![0-9a-zA-Z])")
# The reduced-motion baseline the stylesheet must carry.
REDUCE_MEDIA = "@media (prefers-reduced-motion: reduce)"
# The block form's terminator is matched first, so `design-tokens-exempt-end` is
# never read as an opener with the reason "-end".
END = re.compile(r"design-tokens-exempt-end\b")
EXEMPT = re.compile(r"design-tokens-exempt:(?!-end)\s*(.*)$")
EXEMPT_MIN_REASON = 8
# The prefixes a STANDALONE directive may carry: line/block comment openers and
# nothing else. Anything else before the marker is code, i.e. a trailing comment.
STANDALONE_PREFIX = {"", "//", "/*", "{/*", "*", "/**"}


def code_lines(text: str) -> list[str]:
    """Return the file's lines with COMMENT text blanked out, columns preserved.

    A finding's line/column must point at the source the author wrote, so the
    blanking replaces characters with spaces instead of deleting them.

    The states handled, and why each one is here rather than a regex:
      * `/* … */` (including a JSX `{/* … */}`) — the repo's contrast-rationale
        blocks quote hex values across several lines, and a line-based filter
        misses every continuation line that does not start with `//`;
      * `// …` — unless it is part of a URL (`https://`), which is why the
        preceding character is checked;
      * `"…"` and `'…'` — treated as DATA but NOT blanked (a colour inside a
        string literal, e.g. `fill="#EA4335"`, is a real literal and must be
        caught). They are scanned only so a `//` or `/*` INSIDE a string is not
        mistaken for a comment. String state is LINE-BOUNDED: JSX prose such as
        `Don't` carries a lone apostrophe, and a state that survived a newline
        would hide every literal after it until the next quote;
      * `` `…` `` may span lines (a template literal is a single token), and its
        content is NOT blanked — a colour in a template string is still a
        literal this guard should see.
    """
    out = []
    in_block = False
    in_tmpl = False
    for line in text.splitlines():
        buf = list(line)
        i = 0
        n = len(line)
        while i < n:
            if in_block:
                end = line.find("*/", i)
                if end < 0:
                    for k in range(i, n):
                        buf[k] = " "
                    i = n
                else:
                    for k in range(i, end + 2):
                        buf[k] = " "
                    i = end + 2
                    in_block = False
                continue
            if in_tmpl:
                end = line.find("`", i)
                while end > 0 and line[end - 1] == "\\":
                    end = line.find("`", end + 1)
                if end < 0:
                    i = n
                else:
                    i = end + 1
                    in_tmpl = False
                continue
            ch = line[i]
            if ch == "/" and i + 1 < n:
                if line[i + 1] == "*":
                    in_block = True
                    buf[i] = buf[i + 1] = " "
                    i += 2
                    continue
                if line[i + 1] == "/" and not (i > 0 and line[i - 1] == ":"):
                    for k in range(i, n):
                        buf[k] = " "
                    i = n
                    continue
            if ch == "`":
                in_tmpl = True
                i += 1
                continue
            if ch in "\"'":
                quote = ch
                j = i + 1
                while j < n:
                    if line[j] == "\\":
                        j += 2
                        continue
                    if line[j] == quote:
                        j += 1
                        break
                    j += 1
                i = j
                continue
            i += 1
        out.append("".join(buf))
    return out


def exemptions(raw: list[str]) -> tuple[dict[int, str], list[str]]:
    """line number (1-based) -> reason, plus any structural errors.

    Two forms, both requiring a reason:

      * TRAILING — `const d = "#7c3aed"; // design-tokens-exempt: <reason>`
        exempts its OWN line only. This is the form for a literal inside a
        statement, where a line above would be fine but further away.
      * BLOCK — a standalone directive line opens the block, and
        `design-tokens-exempt-end` closes it. This is the form for a JSX child
        whose tag cannot carry a comment inside it, and for a data literal like
        the brand palette where the values ARE the content.

    Structural errors are returned rather than ignored: an unterminated block, a
    stray end, a nested start and a reasonless directive are each a broken
    exemption, and a broken exemption must fail the run instead of silently
    exempting nothing (or everything).
    """
    out: dict[int, str] = {}
    errors: list[str] = []
    open_line: int | None = None
    open_reason = ""
    for idx, line in enumerate(raw):
        if END.search(line):
            if open_line is None:
                errors.append(f"line {idx + 1}: `design-tokens-exempt-end` with no open block")
            else:
                # `open_line` is the 1-based number of the line AFTER the
                # directive; the end marker's own 1-based number is idx+1, so the
                # exempted range is open_line .. idx (inclusive) — in 0-based
                # `range` terms that is `range(open_line, idx + 1)`.
                for ln in range(open_line, idx + 1):
                    out[ln] = open_reason
                open_line = None
            continue
        m = EXEMPT.search(line)
        if not m:
            continue
        reason = m.group(1).strip().rstrip("*/").strip()
        reason = reason.rstrip(" */}").strip()
        if len(reason) < EXEMPT_MIN_REASON:
            errors.append(
                f"line {idx + 1}: `design-tokens-exempt:` with no reason "
                f"(need >= {EXEMPT_MIN_REASON} chars after the colon) — an "
                f"exemption nobody had to justify is a hole, so it exempts nothing"
            )
            continue
        prefix = line[: m.start()].strip()
        if prefix not in STANDALONE_PREFIX:
            out[idx + 1] = reason
            continue
        if open_line is not None:
            errors.append(f"line {idx + 1}: nested `design-tokens-exempt:` (block opened at line {open_line})")
            continue
        open_line = idx + 2  # 1-based, starting on the NEXT line
        open_reason = reason
    if open_line is not None:
        errors.append(
            f"line {open_line - 1}: `design-tokens-exempt:` block is never closed — "
            f"an unterminated exemption is an unbounded hole, so the run fails"
        )
    return out, errors


def motion_baseline(web_src: str) -> list[str]:
    """Findings for the stylesheet's reduced-motion baseline.

    `globals.css` is not scanned for literals (it is the declaration site), but
    it IS where the app's only stylesheet-level motion baseline can live, so it
    is checked for exactly one property: the `prefers-reduced-motion: reduce`
    block, with the two duration collapses the block exists to perform.

    A missing file is an ERROR (exit 2) rather than a finding, because a scan
    that cannot see the stylesheet proves nothing — the same reason a missing
    `web/src` is exit 2. A file that exists but has no block (or has one that
    does not collapse durations) is a FINDING (exit 1): it is a defect in code
    the author wrote, not a broken run.
    """
    css_path = os.path.join(web_src, "app", "globals.css")
    if not os.path.isfile(css_path):
        print(
            f"design-guard: FAIL — {css_path} does not exist. This gate checks the "
            f"app's motion baseline there; a missing stylesheet is a broken run, "
            f"never a pass.",
            file=sys.stderr,
        )
        raise SystemExit(2)
    text = open(css_path, encoding="utf-8").read()
    at = text.find(REDUCE_MEDIA)
    if at < 0:
        return [
            "app/globals.css has no `@media (prefers-reduced-motion: reduce)` "
            "block — motion that ignores the OS accessibility setting is a defect"
        ]
    tail = text[at:]
    out = []
    for prop, why in (
        ("animation-duration", "keyframe animations would still run at full speed"),
        ("transition-duration", "the 60+ transitions would still run at full speed"),
    ):
        if prop not in tail:
            out.append(
                f"app/globals.css's reduced-motion block never collapses `{prop}` — {why}"
            )
    return out


def scan(root: str) -> tuple[
    list[str], list[str], list[str], list[str], list[str],
    list[tuple[str, int, str, str]], int, list[str],
]:
    """Return (hex, px, transition_all, duration, baseline, exemptions, files, errors)."""
    web_src = os.path.join(root, "web", "src")
    if not os.path.isdir(web_src):
        print(
            f"design-guard: FAIL — {web_src} does not exist. This gate scans the "
            f"shipped web source; a missing tree is a broken run, never a pass.",
            file=sys.stderr,
        )
        raise SystemExit(2)

    paths = []
    for dirpath, dirnames, filenames in os.walk(web_src):
        dirnames[:] = sorted(d for d in dirnames if d not in {"node_modules", ".next"})
        for fn in sorted(filenames):
            # A `.test.tsx` is not shipped code (Tailwind's own @source rules
            # exclude it for the same reason): a fixture may carry a literal.
            if fn.endswith(".tsx") and not fn.endswith(".test.tsx"):
                paths.append(os.path.join(dirpath, fn))
    if not paths:
        print(
            "design-guard: FAIL — 0 .tsx files found under web/src. An empty "
            "scan passes every rule vacuously, so it is an error, not a pass.",
            file=sys.stderr,
        )
        raise SystemExit(2)

    hex_findings: list[str] = []
    px_findings: list[str] = []
    all_findings: list[str] = []
    dur_findings: list[str] = []
    exempted: list[tuple[str, int, str, str]] = []
    errors: list[str] = []
    for path in paths:
        rel = os.path.relpath(path, root).replace(os.sep, "/")
        text = open(path, encoding="utf-8").read()
        raw = text.splitlines()
        code = code_lines(text)
        exempt, errs = exemptions(raw)
        errors.extend(f"{rel}: {e}" for e in errs)
        for ln, line in enumerate(code, 1):
            hits = []
            for m in HEX6.finditer(line):
                hits.append("#" + m.group(1).lower())
            for m in HEX3.finditer(line):
                lit = m.group(1)
                if any(c in "abcdefABCDEF" for c in lit):
                    hits.append("#" + lit.lower())
            if hits:
                if ln in exempt:
                    for h in hits:
                        exempted.append((rel, ln, h, exempt[ln]))
                else:
                    hex_findings.append(f"{rel}:{ln}  {' '.join(hits)}  ({line.strip()[:90]})")
            for m in TEXTPX.finditer(line):
                px_findings.append(f"{rel}:{ln}  text-[{m.group(1)}px]")
            if TRANSITION_ALL.search(line):
                all_findings.append(f"{rel}:{ln}  transition-all  ({line.strip()[:90]})")
            for m in DURATION_MS.finditer(line):
                dur_findings.append(f"{rel}:{ln}  {m.group(0)}  (use var(--jf-dur-…))")
    baseline = motion_baseline(web_src)
    return (
        hex_findings, px_findings, all_findings, dur_findings, baseline,
        exempted, len(paths), errors,
    )


def main() -> int:
    ap = argparse.ArgumentParser(add_help=True)
    ap.add_argument("--root", default=os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    args = ap.parse_args()
    root = os.path.abspath(args.root)

    (hex_findings, px_findings, all_findings, dur_findings, baseline,
     exempted, scanned, errors) = scan(root)
    if errors:
        print("design-guard: FAIL — the exemption directives themselves are broken")
        for e in errors:
            print("  " + e)
        return 2
    if hex_findings or px_findings or all_findings or dur_findings or baseline:
        print("design-guard: FAIL")
        if hex_findings:
            print(
                f"\n{len(hex_findings)} raw hex literal(s) in web/src/**/*.tsx. "
                "Use `var(--jf-…)` — declare the token in web/src/app/globals.css "
                "if the role is genuinely new. If the value cannot be a token "
                "(third-party brand mark, a colour-picker value), put a "
                "`// design-tokens-exempt: <reason>` line above it."
            )
            for f in hex_findings:
                print("  " + f)
        if px_findings:
            print(
                f"\n{len(px_findings)} arbitrary font size(s) in web/src/**/*.tsx. "
                "Use the named type scale: text-jf-4xs (9px), text-jf-3xs (10px), "
                "text-jf-2xs (11px), text-jf-xs (12px), text-jf-sm (13px), "
                "text-jf-md (15px)."
            )
            for f in px_findings:
                print("  " + f)
        if all_findings:
            print(
                f"\n{len(all_findings)} `transition-all` in web/src/**/*.tsx. It "
                "transitions EVERY property, layout ones included, so it turns a "
                "resize into a per-frame layout. Name the properties "
                "(transition-colors / transition-opacity / transition-shadow)."
            )
            for f in all_findings:
                print("  " + f)
        if dur_findings:
            print(
                f"\n{len(dur_findings)} raw duration(s) in web/src/**/*.tsx. The "
                "app's steps are tokens — var(--jf-dur-fast) 120ms, "
                "var(--jf-dur-base) 180ms, var(--jf-dur-slow) 280ms — and a "
                "number typed at a call site is the animation's raw hex."
            )
            for f in dur_findings:
                print("  " + f)
        if baseline:
            print(
                f"\n{len(baseline)} motion-baseline finding(s) in "
                "web/src/app/globals.css. Every animation this app ships has to "
                "ship with the reduced-motion block that collapses it."
            )
            for f in baseline:
                print("  " + f)
        return 1

    print(
        f"design-guard: PASS — {scanned} .tsx file(s) scanned, 0 raw hex literals, "
        f"0 arbitrary font sizes, 0 transition-all, 0 raw durations, "
        f"reduced-motion baseline present, {len(exempted)} documented exemption(s)"
    )
    for rel, ln, lit, reason in exempted:
        print(f"  exempt {rel}:{ln}  {lit}  — {reason}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
