# Accessibility & Browser Matrix (issue #104)

This document is the QA reference for accessibility and cross-browser coverage.
It complements the automated axe-core scan (see below) with the manual
screen-reader and browser-matrix checklists that must be run before a release.

## Automated scan (axe-core)

The web app runs an **axe-core** scan as part of the local preflight
(`scripts/preflight.sh`, gate `a11y`). It covers the
key public pages and **fails the gate on any critical or serious violation**:

- `/login` (two-step 2FA login, passkey, magic-link, social)
- `/landing`
- `/reset-password`
- `/magic-link`
- `/brand`
- `/legal/privacy`
- `/legal/terms`
- `/docs` and every page under `web/src/app/docs` (`/docs/builder`,
  `/docs/customize`, `/docs/embed`, `/docs/subscriptions`, `/docs/variations`,
  `/docs/spam-detection`, `/docs/ses-reputation`, `/docs/placeholders`,
  `/docs/mcp`, `/docs/feedback`, `/docs/chat`). The knowledge base is public
  and the dashboard links into it from Settings with inline "?" buttons, so it
  is a route this scan exists for — it was outside the gate until card
  `t_f5a8730d` extended the list, which is how serious axe violations sat on
  `/docs/customize` (and every other docs route) unreported while the gate
  passed 7/7. `web/src/lib/docsA11y.test.ts` fails the unit suite if a docs
  page is missing from the scan, so a page added later cannot stay outside it.

Run it locally:

```sh
cd web
npm run a11y          # starts the app's own server, runs axe against the pages
# or run every gate at once, from the repo root:
../scripts/preflight.sh --only a11y
```

The scan lives in `web/e2e/a11y.spec.ts` (Playwright + `@axe-core/playwright`); two
further specs (`a11y-dialogs.spec.ts`, `a11y-switcher.spec.ts`) cover authenticated
surfaces with the same gate, and every file whose name matches `a11y*.spec.ts` is
selected by the `a11y` project automatically.
Minor/moderate findings are reported for triage but do not fail the gate.

### Confirmation dialogs (also in the automated scan)

The two **typed-slug confirmation dialogs** are scanned by the same gate, in
`web/e2e/a11y-dialogs.spec.ts` (a second spec in the same `web/e2e/` directory,
so `npm run a11y` runs it):

- **Delete workspace** — Settings → Workspace → *Delete this workspace*.
- **Delete a form for good** — Forms → Trash → *Delete for good*.

Both were added with the workspace-delete feature and live behind a session, so
they are **not** covered by the list of unauthenticated routes above. The spec
does not need a session cookie: it stubs the `dashboard-bundle` (and the few
per-page `/api/*` calls) at the browser level, so the real components render in
a real browser with no Go server, no database and no credential. Each dialog is
asserted to be **reachable** before it is scanned, and the axe scan is scoped
with `include('[role="dialog"]')` — axe throws rather than passing when that
subtree is absent, so the test cannot go green by quietly matching nothing.

Pinned per dialog: `role="dialog"` + `aria-modal="true"` + an `aria-labelledby`
that resolves to the shipped heading, focus in the confirm field on open, the
confirm button disabled until the exact slug is typed, Escape closes, and zero
critical/serious axe violations within the dialog. A regression there now fails
`npm run a11y` instead of passing unseen.

### The workspace switcher's tinted active item

The **workspace switcher's menu** is scanned by the same gate, in
`web/e2e/a11y-switcher.spec.ts`. It is on the authenticated chrome, so the
public-route scan cannot reach it, and the sidebar's tinted row was fixed in the
same redesign while the switcher's was left behind at 4.20:1 — only this
measurement catches that.

Like the dialogs above it needs no session: the spec stubs
`/api/rest/dashboard-bundle` at the browser level with **two** workspaces (with
one, the switcher renders a read-only chip and the tinted active row never
renders — a green reading over nothing), opens the menu, and:

- asserts the menu is REACHABLE and that exactly one row is painted on the tint,
  so the scan cannot pass over a menu that rendered no tinted row;
- **measures** the painted contrast of every row (colours round-tripped through a
  canvas, as the builder-canvas spec does) against the WCAG floor rather than
  trusting a class name;
- runs axe over `[role="menu"]`, failing on critical/serious;
- and carries the old hand-run probe's **CONTROL** arm: with the two
  `--jf-*-on-tint` tokens overridden to their PRE-FIX values, the measurement
  must drop BELOW the floor. Measured: the active row is 7.068:1 normally and
  **4.203:1** under the override, which is the shipped defect reproduced.

`web/scripts/a11y-switcher.mjs`, `web/scripts/a11y-drawer.mjs` and
`web/scripts/nav-evidence.mjs` are HAND-RUN probes: they need a `SESSION` token
and a local build, and **no gate executes them**. The switcher's coverage now
lives in the spec above; the other two remain hand-run, and nothing here claims
otherwise.

### The builder canvas, and a CONTRAST measurement rather than a class check

The same browser-level stub pattern reaches one more authenticated surface: the
**builder canvas**. `web/e2e/builder-canvas-price-contrast.spec.ts` drives
`/builder?form=<id>` with the `/api/*` calls stubbed, so the gate reaches it with
no session, no Go server and no database. It is a MEASUREMENT, not a class-name
assertion: it reads each price span's painted colours out of the live DOM,
resolves them by painting them onto a canvas (Tailwind v4 emits the slate ramp as
`lab()`/`oklch()`, not `rgb()`), and asserts the WCAG ratio. It exists because the
margin it guards is 0.008 — `#5e6ad2` on slate-50 `#f8fafc` is 4.492:1 against the
4.5 floor, while the same colour on white is 4.700:1 — so a guard that rounds, or
measures on white, or trusts a class name, reports the defect as a pass.

That third shape is the one to reach for on the **surfaces the other two cannot
cover**: a source rule cannot see a translucent tint's composited colour, and it
cannot measure a margin this small.


> **Do not reuse a running server.** `web/playwright.config.ts` sets
> `reuseExistingServer` to **false** unless `A11Y_REUSE_SERVER=1`. With a
> leftover `next-server` on the port, playwright would reuse it and axe would
> report a clean pass for a build this tree never produced. `preflight.sh`
> additionally picks a free port (`A11Y_PORT`) so a stale server cannot collide.
> Override the port with `A11Y_PORT=<n> npm run a11y` if 3100 is busy.
>
> Never rebuild `.next` while that server is running: the new chunks land under a
> server that still holds the previous build's manifests, and every page then
> fails client-side ("This page couldn't load"), which reads as a spec failure
> rather than as the build/server mismatch it is.

> **Note on authenticated pages.** The dashboard surfaces (`/`, `/forms`,
> `/builder`, `/settings`, `/profile`, `/products`) require a live session
> cookie from the Go API. The `web/e2e/a11y.spec.ts` gate covers the public
> routes only; the dashboard's own colours are measured out-of-band against a
> standalone build with a real session cookie, and held in place by the source
> guard documented below rather than by the gate. Two exceptions ARE scanned —
> the pair of typed-slug confirmation dialogs, and the canvas price spans (see
> "Confirmation dialogs" and "The builder canvas" above) — because stubbing the
> session-dependent fetch at the browser level puts the real component in the
> browser without needing a credential.

### Muted text and AA (source guard)

The scan above reaches the public surfaces only, so the dashboard's muted-gray
text was measured out-of-band (a production standalone build, a real session,
cleared storage, axe `wcag2aa`): 34 serious `color-contrast` nodes across
`/builder` (11), `/profile` (7) and `/settings` (16), every one a muted line,
tab label or placeholder at 11-14px. `web/src/lib/mutedText.test.ts` is the part
that stops it coming back — it walks every `.tsx` under `web/src` and fails on a
muted token paired with a surface it cannot clear 4.5:1 on, so it is not a
hand-listed file check.

Two tokens, and the rule for which one to use:

| token | measured | use it |
|---|---|---|
| `text-[var(--jf-muted)]` (#6b7280) | 4.54:1 on `--jf-bg` (#f7f8f8), 4.83:1 on white, 4.62:1 on slate-50 | the default muted line |
| `text-[var(--jf-muted-on-tint)]` (#5b6472) | 5.72:1 on slate-50, 5.46:1 on slate-100, 5.35:1 on indigo-50 | a muted line that sits **on a solid tint** (the token is not indigo-only) |

`text-slate-400` (#90a1b9, 2.63:1 on white) and `text-[#9ca3af]` (2.54:1 on
white) are below AA on every light surface in the app, and `text-slate-500`
(#62748e) is 4.76:1 on white but 4.48:1 on the page background — which is why
the guard compares a token against the surface it is *paired with* rather than
banning a class outright.

The guard checks four shapes, because this defect took all four:

1. the same class list renders a light surface **and** a token that cannot clear
   it there;
2. the token is *inherited* from a surface element above it — including the app's
   own page background (`html, body { background: var(--jf-bg) }`), which is what
   made the inactive tab labels a defect at 4.48:1 rather than a pass;
3. an **inline descendant** (`<code>`, `<span>`, …) paints a surface the inherited
   token cannot clear on — `<p class="…jf-muted">Brand <code class="bg-slate-100">ID</code></p>`
   is 4.41:1, and it shipped;
4. `text-[#9ca3af]`, which has no AA-safe placement in the app at all (2.54:1 at
   its best, on white).

A token is only reported when the pairing is real, so `--jf-muted` on the page
background and on white is fine, and an inline child that declares its own colour
is not reported against its ancestor.

The surface a token is judged against comes from the page's own markup **and**
from the `layout.tsx` chain that wraps it, nearest first — a per-file scan that
treated "no surface in this file" as "therefore the page background" reported
`text-slate-500` at 4.48:1 for text that actually renders on white (4.76:1,
passing) at `/docs/*`, where `app/docs/layout.tsx` is
`<main class="min-h-screen bg-white">`. The app page background
(`html, body { background: var(--jf-bg) }`, #f7f8f8) is only assumed when no
layout in the chain paints a light surface over its `{children}` — which is the
case for the dashboard pages, and what keeps the tab-strip defect at 4.48:1
reported.

### `--jf-primary` on a non-white surface (browser guard)

The base brand token `--jf-primary` (#5e6ad2) passes AA on white (4.700:1) and
**fails on the app page background** (4.417:1 on #f7f8f8) and on slate-50
(4.492:1). That is why a site has to be judged against the surface it actually
paints on rather than by the class list: the *same* active-tab class string
reads as green in the builder (`web/src/app/builder/page.tsx`, whose `<nav>`
declares `bg-white`) and as an axe `color-contrast` **serious** node in
`/form-settings` (whose `<nav>` paints nothing, so the page background shows
through). The AA-safe replacement already exists and needs no new token:
`--jf-primary-on-tint` (#4338ca) measures 7.428:1 on #f7f8f8, 7.554:1 on
slate-50 and 7.903:1 on white, so it is safe on every light surface here.

`web/e2e/form-settings-tabstrip-contrast.spec.ts` holds `/form-settings` in
place. It is a **browser** guard, not a source grep, and it runs in the `a11y`
gate (`npm run a11y`, the whole `web/e2e/` directory) with the API stubbed at
the browser level, so it needs no Go server, session or database:

1. it clicks a real tab and reads the painted foreground and the **composited**
   background out of the live DOM, resolving both by painting them onto a canvas
   (Tailwind v4 does not emit these colours as `rgb()`), then asserts the exact
   ratio against 4.5 **and** against the pre-fix 4.417 — so a rounded value
   cannot be argued into a pass and a `#ffffff` measurement cannot pass
   vacuously;
2. it scopes axe to that same strip and asserts `color-contrast` both reports
   nothing **and** actually ran over the subtree, so a scan that analysed
   nothing is not read as clean;
3. it asserts the active tab keeps its `border-[#7170ff]` underline, i.e. the
   non-colour affordance that marks it active survives the token change.

Verified to bite: with the pre-fix token restored the spec is red for the active
tab at 4.417:1 and axe names the exact node — "insufficient color contrast of
4.41 (foreground color: #5e6ad2, background color: #f7f8f8, font size: 10.5pt
(14px), font weight: normal)" on `.border-b-2`.

The guard's lexer and JSX scanner live in `web/src/lib/jsxSource.ts`, shared with
the tinted-surface guard. They blank comments before reading class lists, and
they only honour a comment opener outside JSX **text**: the literal route pattern
`/v1/rest/*` written in prose at `/docs/api` was read as a `/*` block-comment
opener, which blanked 41 lines of that page (including a `</code>`) and made the
guard report a `<code>` 180 lines away as the ancestor of unrelated spans.

> The rest of the panel — the `input`/`textarea` names, the contrast tokens, the
> ⋯ menu — is still NOT covered by the gate. Keep running the signed-in scan
> above for those.
>
> **The panel's form-control NAMES are now in the gate.** Two e2e guards read the
> name sources off the ACTUAL rendered controls (both `feature-e2e`, part of
> `scripts/preflight.sh`): `web/e2e/builder-panel-select-names.spec.ts` for every
> `<select>`, and `web/e2e/builder-panel-choice-names.spec.ts` (card `t_c4abb920`)
> for EVERY visible `input`/`textarea`/`select` — the latter refuses a control
> whose only name is its `placeholder`/`title`, which is exactly the class axe
> cannot see (axe accepts a placeholder as a name, so a clean scan does not prove
> the choices editor's rows are named). Each spec also carries a mutation case that
> unnams the controls and requires the SAME invariant to reject them, so a green
> reading cannot be a vacuum.
>
> **One canvas exception, added with the AA fix.** The canvas price spans (the
> product block's price and the `total` row) ARE in the gate now, via
> `web/e2e/builder-canvas-price-contrast.spec.ts` — a browser MEASUREMENT of the
> contrast ratio rather than a class check, because the failing margin is 0.008
> (`#5e6ad2` on slate-50 is 4.492:1 where the floor is 4.5, and the same colour
> on white is 4.700:1). Reach for that shape for a pair the source rules cannot
> judge — `mutedText.test.ts` and `tintedSurface.test.ts` both pair a token with
> a *named* surface (`bg-slate-50`, `bg-indigo-50`), so a token on a surface
> neither guard models needs the measured form. The panel itself is still outside
> the gate.
> **This is a real gap, not a formality.** The builder's edit panel went
> unscanned long enough to accumulate two **critical** unnamed form controls
> (the value textarea and the conditional-logic rule picker) plus a set of
> contrast failures the same axe run catches in a second. Before you change
> anything under `web/src/app/builder/`, scan it signed in rather than assuming
> the `a11y` gate above covers it:
>
> 1. start the Go API and build the web app against it
>    (`JOMFORM_API_URL=<api> npm run build && JOMFORM_API_URL=<api> npm run start -- -p <port>`);
> 2. seed a verified account + workspace + form — the builder needs a session
>    cookie and a form that belongs to the selected workspace
>    (`?workspace=<slug>` scopes the API; a form created outside that scope 404s
>    in the builder);
> 3. run axe (`@axe-core/playwright`) over the panel with that cookie, from
>    **fresh storage**, across: canvas / panel open on a text field / panel open
>    on an image block / panel open on a field with a conditional rule set / the
>    ⋯ menu open.
>
> Clear `localStorage` and cookies first: the app persists `jf_lang` there, so a
> state left by an earlier run renders the page in a language no new visitor
> sees — and the panel must be genuinely visible, because axe skips
> `display:none` content, which is exactly what hides it below `md`.
>
> **What the gate DOES reach, and how.** The block settings panel can be driven
> from `npm run a11y` after all, without a Go server, a session or a database,
> by stubbing the handful of `/api/*` calls the builder makes at the browser
> level (`page.route`) — the same trick `web/e2e/form-settings-status-chips.spec.ts`
> uses. `web/e2e/builder-panel-select-names.spec.ts` does exactly that and reads
> the live DOM: for every block type whose panel renders a `<select>` (name,
> date, select, product) it asserts each one reports its name from a `<label
> htmlFor>` or an aria reference, and refuses a placeholder-only name (which is
> what axe accepts but a screen reader loses the moment you type). Prefer that
> shape over a source-text assertion — the source grep cannot tell a sibling
> `<label>` from an associated one, which is how three unnamed selects passed a
> 7/7 mutation proof.
>

## Manual screen-reader checklist

Run these flows with a screen reader and confirm every control is announced
with a sensible label, every page has a heading structure, and no control is
unlabeled or unreachable.

### VoiceOver — macOS (Safari/Chrome) and iOS (Safari)

Enable: `Cmd+F5` (macOS) / Settings → Accessibility → VoiceOver (iOS).
Use the rotor (`Ctrl+Option+U`) to check headings, links, and form controls.

| # | Flow | Pass criteria |
|---|------|---------------|
| 1 | **Login (two-step 2FA)** | Email + password fields announced with labels; on wrong password the error is announced (`role="alert"`); the 2FA step announces the code input, the "trust this device" checkbox, and the back link; focus moves to the code input on step 2. |
| 2 | **Login — passkey** | "Log in without password" / passkey button announced; the WebAuthn prompt is reachable and cancellable. |
| 3 | **Login — magic link** | Email field labeled; success/invalid states announced. |
| 4 | **Build a form** | Builder palette blocks announced with names; canvas blocks selectable; the block's settings panel labeled — text, image URL, required checkbox, conditional logic, **and every `<select>`: name format, date/time format, product, and the conditional rule picker** (each announced from a real `<label>`/aria reference, never from a placeholder); the ↑/↓ move buttons and the ⋯ actions menu announced (`aria-haspopup` + `aria-expanded`), and Escape closes the menu returning focus to its ⋯ button. |
| 5 | **Buyer pays** | Public form: product checkboxes announced with name + price; name/email/phone fields labeled; total announced; submit button announced. |
| 6 | **Settings tabs** | Tab bar announced with `role="tab"` + `aria-selected`; each tab's content reachable; form fields labeled. |
| 7 | **Profile tabs** | Same as settings; 2FA setup modal announced as a dialog with a label; QR image has alt text. |
| 8 | **Mobile drawer** | Hamburger button announced ("Open menu"); drawer announced as a dialog; focus moves to the close button on open; Escape closes; Tab is trapped inside the drawer. |
| 9 | **Delete-workspace confirmation** (Settings → Workspace) | Danger-zone button is announced with its disabled state and the reason stated in text ("you need at least one workspace", or the blocker list with counts); the confirmation is announced as a dialog with a label (`role="dialog"` + `aria-modal` + `aria-labelledby`); focus moves to the type-the-slug input on open; Escape closes and clears the field; the inline "?" doc link is announced. |
| 10 | **Rename a workspace** (Settings → Workspace, identity card) | Name and address fields are labeled and seeded from the server's answer; the address field is `readOnly` (not `disabled`) for a non-owner, so it stays focusable and screen readers reach the explanation linked by `aria-describedby` instead of meeting a control that cannot be focused at all; the Save button's disabled state is accompanied by the reason in text ("nothing changed yet", who may change the address, or the address rule); the new public URL is shown as text below the address field; the old-address-keeps-working note and any refusal are announced (`role="status"` / `role="alert"`). |

### NVDA — Windows (Firefox/Chrome)

Enable: `Insert+F7` for element list. Same flows as above; confirm NVDA
announces the same labels and landmarks.

### Landmarks & structure (both readers)

- Every page has a single `<main>` landmark and a logical heading order
  (`h1` → `h2` → …).
- The app shell exposes a **skip-to-content** link (first Tab stop).
- Tables (dashboard sales, builder sales) have proper `<th>` headers.
- No control relies on color alone (status badges also carry text).

## Browser matrix

Verify the core flows on each browser/OS. Known risk areas are flagged.

| Flow | Chrome (Win/macOS/Android) | Firefox (Win/macOS) | Safari (macOS) | Safari (iOS) |
|------|:---:|:---:|:---:|:---:|
| **Login** (email+password) | ✅ | ✅ | ✅ | ✅ |
| **Login — 2FA two-step** (#71) | ✅ | ✅ | ✅ | ✅ |
| **Login — passkey / WebAuthn** (#8) | ✅ | ✅ | ⚠️ verify | ⚠️ verify |
| **Login — magic link** | ✅ | ✅ | ✅ | ✅ |
| **Build a form** (builder) | ✅ | ✅ | ✅ | ✅ |
| **Buyer pays** (public form + CHIP checkout) | ✅ | ✅ | ✅ | ✅ |
| **Checkbox rendering** (#58) | ✅ | ✅ | ✅ | ✅ |
| **Next client-side nav** (#29) | ✅ | ✅ | ✅ | ✅ |
| **Settings tabs** | ✅ | ✅ | ✅ | ✅ |
| **Mobile drawer** (focus trap, Escape) | ✅ | n/a | n/a | ✅ |

Legend: ✅ verified · ⚠️ verify (WebAuthn/passkeys are platform-dependent and
must be confirmed on the target device).

### Known risk areas

- **WebAuthn / passkeys (#8):** passkey availability and the native prompt
  differ per platform. Confirm on Safari (macOS + iOS) and Firefox.
- **Checkbox rendering (#58):** the public form uses `appearance:none`
  checkboxes with an explicit white background so Android Chrome and Safari
  draw a visible unchecked box. Confirm on Safari.
- **Next client-side nav (#29):** confirm sidebar/drawer links navigate
  without a full reload and focus is not lost.
- **Two-step 2FA login (#71):** confirm the step-1 → step-2 transition and
  the back link on all browsers.

## Keyboard + focus checklist

| Area | Pass criteria |
|------|---------------|
| **Login (two-step)** | Tab order: email → password → remember → forgot → submit → (step 2) code → trust → back. Visible focus ring on every control. |
| **Builder** | Palette blocks, canvas blocks, the block settings panel, the ↑/↓ move buttons and the ⋯ actions menu all reachable by Tab; the menu opens with Enter/Space, closes with Escape and returns focus to its button; visible focus ring. |
| **Settings tabs** | Tab bar reachable; arrow keys move between tabs (or Tab reaches each); visible focus ring. |
| **Mobile drawer** | Opens with focus on close button; Tab trapped inside; Escape closes and returns focus. |
| **Global** | `:focus-visible` ring (indigo outline) is applied to all interactive elements (see `globals.css`). |

## Fixes shipped in this change

- **Contrast (WCAG AA):** darkened low-contrast text across login tabs, the
  landing footer/badge, reset-password and magic-link error banners, the
  brand page, and the legal "Last updated" lines.
- **Focus:** added a global `:focus-visible` outline so keyboard focus is
  always visible (Safari included).
- **Mobile drawer:** added `role="dialog"` + `aria-modal`, focus-on-open,
  Tab trapping, and Escape-to-close (no focus trap).
- **Landmarks:** added a skip-to-content link and `aria-current="page"` on
  the active nav item.
- **Screen reader:** added `role="alert"` to the login error banner.
