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

## What enforces this

| where | what it catches |
|---|---|
| `scripts/design-guard.py` (the `design` preflight gate, with a per-run planted-control proof) | a raw hex, `text-[Npx]`, `transition-all`, a raw `duration-<n>ms`, a missing reduced-motion block |
| `web/src/lib/designTokens.test.ts` (runs inside `test:unit`, so `npm run build` fails) | a raw colour, a `--jf-*` declared outside `globals.css`, an arbitrary `[Npx]` a scale step names, a colour token with no measured pair, a primitive missing from `/design`, an unmeasured ratio |
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
