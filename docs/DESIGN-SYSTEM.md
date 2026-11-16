# The JomForm design system — how to add a colour (and a size)

Three rules, in order. They exist because the app is 127 `.tsx` files and 40+ pages
written by different hands over months, and the only thing that kept the UI
coherent was somebody noticing.

**Token first. Primitive second. A page-level colour class, never.**

## 1. Token first — `web/src/app/globals.css`

Every colour, size, radius and shadow is a custom property in `globals.css`. If the
value you need does not exist, the change starts HERE:

```css
:root {
  /* What it is FOR, and the ratio it was measured at on the surface it lands on. */
  --jf-thing: #4f46e5;   /* 6.29:1 on --jf-surface; 5.92:1 on --jf-bg-soft */
}
```

- **Never rename an existing `--jf-*` token.** They are referenced ~1,000 times; add a
  name with the new meaning instead, and say in a comment what the two differ by.
- **Colour only: measure it.** Every colour token needs a row in
  `web/src/lib/designTokenRatios.ts` (its foreground, the background it lands on, and
  the WCAG floor). The unit suite re-derives every ratio from `globals.css` and fails
  when a token has no measurement or a recorded number has drifted. `4.5:1` is the
  floor for normal-size text, `3:1` for a border, an icon or a control edge.
- **Measure against the surface it LANDS on.** `--jf-primary` is 4.70:1 on a white
  card and 4.20:1 on `--jf-surface-2`. Text and links on a tinted surface use the
  tinted pair (`--jf-primary-strong`, `--jf-primary-on-tint`, `--jf-muted-on-tint`);
  `/design` lists every pairing that fails and the token to use instead.
- A **dark** value goes in the `[data-theme="dark"]` block under the SAME name. Every
  colour token in `:root` needs one, with two documented exceptions: the ink painted
  on a coloured fill, and the landing page's fixed dark band.
- Sizes: there is a 4px space scale (`--jf-space-1…12`), a radius ladder
  (4/8/12/16/pill), three elevations and a reading type scale with a paired
  line-height. A new step is a new row in `web/src/lib/designTokenScale.ts` plus its
  `@theme` alias — not a number typed at a call site.

## 2. Primitive second — `web/src/components/ui/`

A control, a card, a field or a message belongs in the primitive layer, exported from
`components/ui/index.ts`. That layer uses tokens only (it is scanned), and `/design`
must show it in every state it can be in: add it to `PRIMITIVE_STATES` in
`web/src/lib/designStates.ts` — `null` for a state it has, a sentence for a state it
does not — and the page will not compile until the specimen exists.

If a page needs a control the layer does not have, **extend the layer**. A new
primitive is one file, one barrel export, and one state-matrix entry; the unit suite
fails if any of the three is missing.

## 3. A page-level colour class, never

```tsx
// NO — a literal, or a raw Tailwind colour, is a decision nobody measured
<div className="bg-[#4f46e5] text-white">

// NO — the value in the class list, even "just this once"
<div className="bg-indigo-600">

// YES — the token, or a primitive that already applies it
<Button>Save changes</Button>
<div className="bg-[var(--jf-primary)]">
```

An arbitrary length is the same defect when a scale step already names it: 44px is
`--jf-space-11`, so `min-h-[44px]` is `min-h-jf-11` — the same pixels, via the token.
A length NO step names (`w-[720px]`, a decorative blur) is fine and stays.

A raw Tailwind palette colour — `bg-indigo-600`, `text-slate-500`, `bg-white/95` — is the
same defect too, and since card `t_2fb8ae1b` the unit suite enforces it: rule 4
(`scanPalette`) in `web/src/lib/designTokens.test.ts` reports every
`<prefix>-<family>-<step>` for the seven colour utilities Tailwind spells that way, plus
`bg-white` / `text-white` / `bg-black`. It does not report the token form
`bg-[var(--jf-…)]` — that is the replacement — and it honours `design-tokens-exempt:`
exactly as the hex rule does.

## What enforces this

| where | what it catches |
|---|---|
| `scripts/design-guard.py` (the `design` preflight gate, with a per-run planted-control proof) | a raw hex, `text-[Npx]`, `transition-all`, a raw `duration-<n>ms`, a missing reduced-motion block |
| `web/src/lib/designTokens.test.ts` (runs inside `test:unit`, so `npm run build` fails) | a raw colour, a raw Tailwind palette colour utility (`bg-indigo-600`, `bg-white`), a `--jf-*` declared outside `globals.css`, an arbitrary `[Npx]` a scale step names, a colour token with no measured pair, a primitive missing from `/design`, an unmeasured ratio |
| `/design` (`web/src/app/design/page.tsx`, `robots: noindex`) | the manifest — every token with its ratio, every primitive in every state |

**Two exceptions, both narrow and both printed:**

- `design-tokens-exempt: <reason>` (8+ chars) on a line, or a
  `design-tokens-exempt-end`-closed block, is for a value that IS the content — the
  brand page's swatch palette, a colour picker's persisted default. It must say why.
- `ARBITRARY_PX_ALLOWLIST` in `web/src/lib/designTokenAllowlist.ts` holds a per-file
  **count** of a debt already measured. It may only SHRINK: a file with more findings
  than it was seeded with fails, and a file that reaches zero must have its entry
  deleted. The unit suite prints the total on every run, so the shrink is visible.

Both lists are debt, never a parking space. A new deviation is fixed or justified on
the line, in the same commit.

## Rule 3's palette half — enforced since card `t_2fb8ae1b`, and the 793 that are left

Rule 3 says a raw Tailwind colour is the same defect as a hex. Until card `t_2fb8ae1b`
that was the one half **no gate caught**: both scanners looked for LITERALS — a
`#rrggbb`, a `bg-[#fff]`, a `text-[<n>px]` — and `bg-indigo-600` is not a literal, so it
passed both. The gap is now closed, and it had to close in this ORDER: a repo-wide rule
seeded before the vocabulary had consumers would have red every branch in flight that
touched a page, with a policy nothing could satisfy yet. So the six worst pages moved
first — one card per page, each moving the page AND the source guards that read its
palette class names in the same commit — and the rule landed on top of them.

**What enforces it.** `scanPalette`, rule 4 of `web/src/lib/designTokens.test.ts`, i.e.
inside `test:unit`, i.e. the `web` gate. It reports `<prefix>-<family>-<step>` for the
seven colour utilities Tailwind spells that way (`bg-`, `text-`, `border-`, `ring-`,
`divide-`, `fill-`, `stroke-`), with an optional `/<alpha>`, plus `bg-white` /
`text-white` / `bg-black`. It does NOT report `bg-[var(--jf-…)]` — the replacement — and
it honours `design-tokens-exempt:` exactly as the hex rule does. Both are control tests:
one plants `bg-slate-50` beside `bg-[var(--jf-surface-2)]` and requires the finding for
the first and none for the second; another plants a justified exemption and requires no
finding at all.

Measured on the tree the rule landed on (127 shipped `.tsx`; the suite prints the two
allowlist totals on every run):

| what | measured |
|---|---|
| raw hex in CODE (comments stripped, exemptions honoured) | **0** |
| arbitrary colour utilities (`bg-[#…]`) | **0** |
| `[<n>px]` that a scale step already names | **7** in 4 files — `ARBITRARY_PX_ALLOWLIST` |
| raw Tailwind palette colour utilities | **793** in 47 files — `PALETTE_UTILITY_ALLOWLIST`, shrink-only |
| of which the six migrated pages | **1** — `app/builder/page.tsx`, `bg-white/95` on the sticky header's translucent backdrop; the six files carried 601 of the 1,449 before the migration |
| `--jf-*` colour utilities (the token form) | **2,223** in shipped `.tsx` — colour-prefixed `[var(--jf-…)]` occurrences, counted on the tree this rule landed on |

The palette seed moved once, between the rule being written and it landing: card
`t_cff48545` tokenised the last seven palette utilities in `components/FormModeControl.tsx`
just before this rule merged, so that entry was re-measured 8 → 1 and the total 800 → 793
on the landing tree. The seed is the count that tree carries — a higher one would leave
slack in which a file could regress without the gate noticing, and a bumped one would hide
exactly the drift the rule exists to catch.

The migration moved the mass; the rule now holds the remainder. The concentration is
`app/admin/page.tsx` 114, `app/products/page.tsx` 60, `components/WorkspaceTab.tsx` 55,
`app/mcp/mcp-client.tsx` 46, `app/chat/page.tsx` 31, `app/forms/page.tsx` 31,
`app/products/ProductEditor.tsx` 31, `app/media/page.tsx` 28 — 47 files in all. That is
a seeded debt, not a licence: the allowlist is keyed by file with the COUNT it was seeded
with, so a file may get better and never worse, and a file that reaches zero FAILS until
its entry is deleted. The suite prints
`design-tokens: raw palette: N finding(s) in M file(s), K allowlisted (seeded S)` on
every run, so the shrink is visible without reading a diff.

**How to pay a file down.** Replace the class with the token that already means it —
`bg-slate-100` → `bg-[var(--jf-code-bg)]`, `text-slate-500` → `text-[var(--jf-muted)]`,
`bg-red-50` → `bg-[var(--jf-danger-bg)]` — and measure it against the surface it LANDS on
(`--jf-muted` is 4.42:1 on the failure tint, which is why `--jf-muted-on-tint` exists).
If the class is the SUBJECT of a source guard, that guard's model moves in the SAME
commit: a page migrated off the names a guard matches does not trip the guard, it makes
the guard stop SEEING the element — the silent-skip shape each of them refuses. The six
page cards each did exactly that, and the guards they moved are named in their PRs.
