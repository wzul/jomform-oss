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

The scan lives in `web/e2e/a11y.spec.ts` (Playwright + `@axe-core/playwright`).
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

> **Do not reuse a running server.** `web/playwright.config.ts` sets
> `reuseExistingServer` to **false** unless `A11Y_REUSE_SERVER=1`. With a
> leftover `next-server` on the port, playwright would reuse it and axe would
> report a clean pass for a build this tree never produced. `preflight.sh`
> additionally picks a free port (`A11Y_PORT`) so a stale server cannot collide.
> Override the port with `A11Y_PORT=<n> npm run a11y` if 3100 is busy.

> **Note on authenticated pages.** The dashboard surfaces (`/`, `/forms`,
> `/builder`, `/settings`, `/profile`, `/products`) require a live session
> cookie from the Go API, which the scan does not provision. They are exercised by
> the manual screen-reader and browser-matrix checklists below instead. The one
> exception is the pair of confirmation dialogs, which ARE scanned (see
> "Confirmation dialogs" above) because stubbing the session-dependent fetch
> puts the dialog itself in the browser without needing a credential.

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
| 4 | **Build a form** | Builder palette blocks announced with names; canvas blocks selectable; the block's settings panel (text, image URL, product, required checkbox, conditional logic) labeled; the ↑/↓ move buttons and the ⋯ actions menu announced (`aria-haspopup` + `aria-expanded`), and Escape closes the menu returning focus to its ⋯ button. |
| 5 | **Buyer pays** | Public form: product checkboxes announced with name + price; name/email/phone fields labeled; total announced; submit button announced. |
| 6 | **Settings tabs** | Tab bar announced with `role="tab"` + `aria-selected`; each tab's content reachable; form fields labeled. |
| 7 | **Profile tabs** | Same as settings; 2FA setup modal announced as a dialog with a label; QR image has alt text. |
| 8 | **Mobile drawer** | Hamburger button announced ("Open menu"); drawer announced as a dialog; focus moves to the close button on open; Escape closes; Tab is trapped inside the drawer. |
| 9 | **Delete-workspace confirmation** (Settings → Workspace) | Danger-zone button is announced with its disabled state and the reason stated in text ("you need at least one workspace", or the blocker list with counts); the confirmation is announced as a dialog with a label (`role="dialog"` + `aria-modal` + `aria-labelledby`); focus moves to the type-the-slug input on open; Escape closes and clears the field; the inline "?" doc link is announced. |

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
