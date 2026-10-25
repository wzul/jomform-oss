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
- `/mcp` — the MCP setup page. It was in **no** scan until card `t_2db0fc94`:
  it is not a `/docs/*` route, so nothing policed it, and the axe list did not
  name it either. It carries the second `bg-slate-900` code slab on a public
  page (`/landing` is the other), a pairing `src/lib/emeraldText.test.ts`
  deliberately abstains on because a class-name scan cannot composite a dark
  background. `src/lib/a11yDarkSlabCoverage.test.ts` now fails the unit suite
  when a route that paints text on a dark slab is neither scanned here nor
  behind a session, so the next such page cannot sit outside the gate again.
  The one route class that guard cannot judge itself is a **dynamic** route
  (`/products/[id]`-style segments, card `t_70a79006`): a `[id]` directory is not
  a path a static list can spell, so no entry above can name it. A dark-slab page
  there is therefore pinned by hand in `PINNED_DYNAMIC_CARRIERS` inside that
  guard — the set is empty today (measured: the only dynamic route sources are a
  route handler, which paints nothing, and a wrapper whose slab lives on a
  component that declares the session chrome, so it is judged by the
  behind-a-session arm instead), and any
  new one fails the unit suite until it is pinned or gated. Stated here because an
  unstated blind spot reads exactly like coverage.
- `/reset-password`
- `/magic-link`
- `/brand`
- `/legal/privacy`
- `/legal/terms`
- `/docs` and every guide under `web/src/app/docs` — three of them are `/docs`,
  `/docs/builder` and `/docs/customize`. **The route list is not repeated here**:
  it is `PAGES` in `web/e2e/a11y.spec.ts`, and `web/src/lib/docsA11y.test.ts`
  fails the unit suite when a docs page on disk is missing from it, so a
  hand-copy of the list in prose is redundant and is the first thing to go
  stale. The knowledge base is public and the dashboard links into it from
  Settings with inline "?" buttons, so it is a route this scan exists for — it
  was outside the gate until card `t_f5a8730d` extended the list, which is how
  serious axe violations sat on `/docs/customize` (and every other docs route)
  unreported while the gate passed 7/7. This paragraph used to name the routes
  by hand and drifted twice (twelve named while the spec listed eighteen), so
  `web/src/lib/a11yScanProse.test.ts` now fails if a hand-written list comes
  back.

Run it locally:

```sh
cd web
npm run a11y          # starts the app's own server, runs axe against the pages
# or run every gate at once, from the repo root:
../scripts/preflight.sh --only a11y
```

The scan lives in `web/e2e/a11y.spec.ts` (Playwright + `@axe-core/playwright`). The
further `web/e2e/a11y-*.spec.ts` files — the typed-slug dialogs, the workspace
switcher and the chat confirmation card — cover surfaces the unauthenticated
route list above cannot reach, and every file whose name matches `a11y*.spec.ts`
is selected by the `a11y` project automatically, so a new one is in the gate by
its name alone. Which specs exist is `npm run a11y -- --list`, not a list here.
Minor/moderate findings are reported for triage but do not fail the gate.

### axe cannot judge a SINGLE-CHARACTER element (card `t_07eb69c8`)

The `a11y` gate fails on critical/serious **violations**, and axe-core 4.13.0 cannot
produce one for text one character long. In `web/node_modules/axe-core/axe.js`:

```
27197  if (ignoreUnicode && textIsEmojis(visibleText)) { messageKey: 'nonBmp'; return void 0; }
27277  var shortTextContent = visibleText.length === 1;
27280  } else if (!isValid && shortTextContent && !ignoreLength) missing = 'shortTextContent';
27294  if (... || shortTextContent && !ignoreLength && !isValid) { ...; return void 0; }   // cantTell
31351  shortTextContent: 'Element content is too short to determine if it is actual text content',
31352  nonBmp: 'Element content contains only non-text characters',
```

The rule's answer for such a node depends on nothing but its LENGTH: a one-character
element that PASSES is a `pass`, and one that FAILS is an `incomplete` (`cantTell`) —
never a violation, and so never a red gate. Measured live (axe injected into a
production build, fresh state, the sidebar's own nav glyph on the dashboard: 12px/400,
`--jf-muted` #6b7280 at `opacity-70` over the sidebar's white = #979ca6 =
**2.755:1** against a 4.5 floor):

- the real one-character node: `violations: []`, and `incomplete` carries it twice
  over — "Element content contains only non-text characters" for the pictographic
  glyphs and "Element content is too short to determine if it is actual text
  content" for the letter-like ones;
- the SAME paint on a two-character node planted in the same nav: a
  `color-contrast` **serious** violation — "insufficient color contrast of 2.58
  (foreground color: #9298a6, background color: #eef2ff, font size: 9.0pt (12px))".

Nothing about the colour, the token or the surface differs between those two nodes.

**The site it hid, and the fix.** A rendered-page census measured every visible
element whose OWN text is exactly one character, on its own painted surface and at
its own composited `opacity`: 21 of the 28 routes visited rendered such an element,
319 in total. Exactly one family was under its floor, and it was on every signed-in
page — **the sidebar nav glyph** (`Shell.tsx`, the one span per nav item, the same
component for the desktop sidebar and the mobile drawer), where `opacity-70` dimmed
the row's own token to 2.755:1 over the sidebar's white and 3.714:1 on the active
row's `bg-indigo-50`. The fix is the alpha, not a new token: the row's existing
`--jf-muted` at full strength is 4.834:1 on white and `--jf-primary-on-tint` is
7.068:1 on the tint. That is the same shape the `/chat` confirmation card's
`opacity-70` lines already got (card `t_f5f269a8`). After it the census re-runs at
the same 319 elements over the same 21 routes with **no** element under its floor —
including the two avatar-initial circles this tree also carries, which PR 1102 fixed
to 6.414:1 (the 12px sidebar initial) and 3.814:1 at 24px (the `/profile` one, which
is WCAG large text against its 3:1 floor).

`web/e2e/a11y-singlechar-text.spec.ts` is the guard and is in the `a11y` gate by its
filename prefix. One test per route asserts that the page was not redirected, that
its own shell rendered, that it found at least the MEASURED number of
single-character elements, and that every one of them — with no exemption — clears
the floor its own measured size and weight imply; its last arm reproduces the
pre-fix paint on the real elements, requires the measurement to drop below the
floor, requires live axe to report NOTHING for it, and then requires axe to name the
same paint on a two-character node — the blindness and the liveness of the rule in
one run. That last arm proves it is on `/` BEFORE it counts anything: not bounced to
`/login` (a page with no sidebar has no glyphs, so a 0 there is a reading over
nothing), and painted inside the settle ceiling — a page that is not the signed-in
shell at all (a cookie-less `/` renders the marketing landing) is named as that too,
not as a missing glyph. The failure message says which of the two it was, so a red
names a bounce or an unpainted shell rather than always
reading as "the sidebar rendered no nav glyph spans". Its `ROUTES` table is the
authority for what is scanned, and every element it measured is printed as a
`[single-char]` line in the gate's own output.

**What this does NOT cover (stated, not hidden).** Only the routes in that table are
scanned: a single-character element on any other surface — a dynamic
`/products/[id]`-style segment, a page that only renders with real data, the
`/admin` page, which this spec's browser-level stubs do not render — is still judged
by nothing, and the fix for that is to add the route. And axe is still the wrong
guard for this class: this file exists because the rule cannot see it, not because
the rule changed.

### The sidebar nav links' accessible name carried the icon (card `t_950256ab`)

`Shell.tsx` paints every sidebar row as a decorative glyph and a label:
`<Link href={n.href}><span className="w-4 text-center text-xs" aria-hidden="true">{n.icon}</span>{n.label}</Link>`.
The span used to carry **no `aria-hidden`**, and because a link's accessible name is
computed from its whole content, every nav name in the accessibility tree was the
glyph PLUS the label. MEASURED on a production build served locally, fresh browser
state (storage cleared, no `jf_lang`, `html lang="en"`), the API stubbed at the
browser level, `/` read two ways — Playwright's `ariaSnapshot()` and Chromium's own
AX tree over CDP (`Accessibility.getPartialAXTree` on each anchor), which agreed:

```
aside nav a[href="/forms"]      name "✎ Forms"
aside nav a[href="/spam"]       name "⚠ Spam"
aside nav a[href="/templates"]  name "🗂 Templates"      … on all 14 rows
```

A screen reader therefore announced "pen Forms", "warning Spam" — the icon, again —
for every destination, and the *mobile drawer* said the same thing, because both
surfaces render the ONE `NavList`. The glyph is decoration; the label beside it is
the name. The fix is `aria-hidden="true"` on that span: the glyph is still PAINTED
(it is part of the sidebar's design) and only AT stops hearing it. That is the same
rule the sortable-table ▲/▼ glyph already carries (`web/e2e/a11y-products.spec.ts`:
"the ▲/▼ glyph must be `aria-hidden` so the button keeps its plain label").

`web/e2e/a11y-nav-icon-name.spec.ts` is the guard (in the `a11y` gate by its
filename prefix). It asserts on the accessible **NAME**, never on the attribute — a
guard that greps `aria-hidden` can pass while AT still hears the glyph — reading it
twice per surface (Chromium's AX tree and `ariaSnapshot`), requiring both to equal
the row's label and to agree with each other, on the desktop `<aside>` AND through
the real "Open menu" control in the mobile drawer. It also requires the glyph to
still be painted (`<glyph><label>` in the row's own text), so the fix cannot be
"delete the icons". Its control arm removes `aria-hidden` from the real spans and
requires both readings to go RED, then restores it and requires them to read the
labels again — the reading tracks the DOM, not the page load.

**What this does NOT cover (stated, not hidden).** The axe family's own rules cannot
see this at all — MEASURED: with the glyph back in the names, axe scoped to the nav
reports **0 violations** and `link-name` **passes on all 14 links**, exactly as it did
with the fix in place (a name that carries an icon is still a non-empty name; no rule
inspects one for decoration). That is why this is measured in a spec rather than left
to `a11y.spec.ts`. Only the two sidebar surfaces are scanned: a decorative glyph
inside another interactive component (a button, a tab, a table control) would need
its own arm, and `a11y-products.spec.ts` holds the one that already exists for the
sort headers.

### The document declaration must match the prose (card `t_c9d23e2c`, WCAG 3.1.1)

`web/src/components/i18n.tsx` writes the dashboard locale to
`document.documentElement` — `lang`, and `dir="rtl"` for Arabic. The knowledge
base and the legal pages are written in **English only** (`app/docs/docsChrome.tsx`:
the /docs prose stays English, and only the index's chrome labels are
translated), so that write used to declare an English page Arabic and mirror it
right-to-left. Measured live on jomform.com v1.18.0 (fresh browser state) by card
`t_6c4ccef4`: with `jf_lang=ar`, `/docs/embed` read `lang="ar"` / `dir="rtl"`
over English prose (h1 "Embed a form on your site", every nav label English), and
`/legal/privacy` the same.

The rule now lives in **one place**, `web/src/lib/proseSurfaces.ts`: `/docs` and
`/legal` are the English-only prose surfaces and keep `lang="en"` and LTR
whatever the visitor's locale is, while every other surface — the Arabic
dashboard included — still follows the locale and keeps its RTL. The match is on
a **segment boundary**, so a future `/docsomething` is not swept in.

Two guards hold it, and both were driven RED on the mutation they exist for:

- `web/src/lib/docsA11y.test.ts` walks the routes **on disk** under
  `src/app/docs` and `src/app/legal` and fails when one of them is not matched by
  the rule (a new /legal page or a nested docs route is covered the day it
  lands), and reads the provider to check the write really is gated on the rule
  and on the ROUTE — a client-side navigation out of the docs tree must move the
  declaration back.
- `web/e2e/a11y-docs-lang.spec.ts` (in the `a11y` gate) drives both states in a
  real browser — fresh, and `jf_lang=ar` — and reads `html` `lang` / `dir` plus
  the computed `direction` out of the live DOM. `dir` is written **only** by that
  effect, after hydration (`app/layout.tsx` sends `<html lang="en">` with no
  `dir`), so it doubles as the hydration witness: a page that never hydrated
  fails there instead of passing on the server-rendered value.

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
confirm button's disabled state at the boundary that actually guards the write —
disabled with the field empty, **still disabled for a wrong slug of the same
length, for a prefix of the real slug, and for the exact slug in inverted case**;
enabled for the exact slug, and for the exact slug padded with whitespace (the
shipped `trim()`); disabled again **while a submit is in flight** (the
double-submit guard, measured by holding the request open) — Escape closes, and
zero critical/serious axe violations within the dialog. The workspace dialog
additionally pins the server's `can_delete`: with `can_delete: false` the dialog
is not offered at all, and the conjunct that keeps its confirm off in that state
is held by `web/src/lib/workspaceDeleteConfirmGate.test.ts`, because no browser
can be driven into it (the guard's header records both measurements).

The slug states are one boundary. Card `t_b2463a72` measured that only the empty
state was ever checked, so a confirm that armed on any non-empty text
(`typed.trim().length === 0` in place of the slug comparison) shipped green on
both dialogs. Card `t_2b129c85` measured the rest: a `startsWith` comparison, a
case-folded one, and the dropped `busy` / `can_delete` conjuncts each shipped
green as well. A regression in any of them now fails `npm run a11y` — or, for
the unreachable `can_delete` term, `npm run test:unit` — instead of passing
unseen.

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
and a local build, and **no gate executes them** (the switcher's own coverage is
now the `a11y-switcher.spec.ts` spec named above; `nav-evidence.mjs` is a plain
evidence tool with no assertion to promote).

A hand-run probe still prints a verdict someone will read, so it runs the SAME
rule the gate runs (card `t_c912998e`). `a11y-switcher.mjs` used to declare its
own `withTags(["wcag2aa", "wcag21aa"])` — the six-of-105 tag set — and so could
print `critical/serious: 0` over a menu the gate fails, on the one surface
(authenticated chrome) where no other reading exists. It now calls
`gateScan()`/`blockersOf()` from `web/e2e/axeGate.ts`, and
`web/src/lib/a11yGateRule.test.ts` fails the build if any script under
`web/scripts/` narrows the rule set or if the switcher probe stops going through
the shared gate.

**The same menu's `workspace.setDefault` row ("Make this the default workspace")
is held PER STATE** (card `t_9d8f0431`). It is the row whose two states paint on
*different* surfaces, and only one of them used to be measured:

| state | ink | surface | ratio |
|---|---|---|---|
| resting | `--jf-muted` #6b7280 | the panel's white #ffffff | 4.834:1 PASS |
| hovered | `--jf-primary` #5e6ad2 (pre-fix) | `bg-slate-50` #f8fafc | **4.492:1 FAIL** |

axe named the hovered node `color-contrast (serious)` — "insufficient color
contrast of 4.49 (foreground color: #5e6ad2, background color: #f8fafc, font size:
10.5pt (14px), font weight: normal)". The row now paints
`--jf-primary-on-tint` #4338ca in the hover state (7.554:1) and keeps
`--jf-muted` at rest, and `a11y-switcher.spec.ts` holds both:

- it drives a REAL pointer onto the row and **measures both states** — a fix that
  swapped the whole class list would have regressed the resting state, which
  already passed;
- it asserts the hover actually applied (the painted ink and the surface must both
  move) and that the hovered ink is `--jf-primary-on-tint`;
- it runs the scoped axe scan **with the row hovered** and requires 0
  `color-contrast` violations — axe only reads `:hover` colours if the element is
  hovered while it scans, so this is the run shape that reported the defect;
- it carries a **CONTROL** that forces the pre-fix hover ink back on and requires
  the measurement to reproduce **4.492:1** *and* axe to name the node again.

Two properties of that arm to know before re-anchoring it. (1) It is driven on
`/form-settings?form=<id>`, not `/settings`, for its stubs — but the axe half no
longer depends on WHICH page paints under the panel (card `t_711cefb9`). On some
dashboard pages the dropdown panel overlaps the page content and axe **cannot
determine the row's background at all** — it returns the node as `incomplete`
(`elmPartiallyObscuring`, "Element's background color could not be determined
because it partially overlaps other elements"), where a 0-violation reading is a
scan that gave up rather than a pass. axe reaches that state when its background
samples disagree **across the node's text rects**, and this row's label wraps onto
two lines: on `origin/main` both samples fell on the same tab-strip element, and
PR 1259's eighth tab moved a tab *button* under the second line, so the identical
arm went red in both directions (`incomplete` in the arm, 0 violations in its own
CONTROL). The arm therefore now calls `hidePageBehindRow` before the scan: it
asserts the row's own hover background and the menu panel are **opaque**, asserts
the row is the **topmost painted element** at every text sample (so a page layer
that really covers the menu still fails loudly), and only then hides the page
elements that overlap the row — `visibility: hidden`, so the row cannot move, and
the measurement after the hide is asserted identical to the one before. The arm
also still asserts the row was actually JUDGED, not merely not flagged, so a
future `incomplete` reading fails instead of passing. (2) The menu trigger is
visible in the server-rendered HTML *before* React hydrates, so the open helper
retries the click until the app reports `aria-expanded="true"`; clicking blind was
measured as a flake on this file.

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
cover**: a source rule composites a translucent tint against the **darkest opaque
surface this app paints** that is darker than the tint (falling back to
`--jf-bg`), not the real stack beneath a given site, and it takes the conservative
side of the composited pixel's 8-bit rounding — it still cannot measure a margin
this small, so the browser specs above remain the backstop.

### A dashboard page with a TABLE — `/products` (card t_3fbc6953)

The **products page** is scannable by the same browser-level stub pattern, and
until card `t_3fbc6953` it was scanned by nothing: `a11y.spec.ts` lists public
routes, and none of the `a11y-*.spec.ts` siblings visited a dashboard page with a
table. The gate recorded `pass` while the live `/products` (the served v1.20.62
bundle) carried **four** critical axe-core 4.13.0 nodes:

- `aria-allowed-attr` ×3 — `aria-sort` was written on the `<button>` inside each
  sortable `<th>`. The attribute is supported by the roles `columnheader` and
  `rowheader` only, so it was both an axe violation and invisible to assistive
  technology as a sort state. `SortHeader` now renders the `<th>` itself and puts
  the state there, exactly as the
  [W3C ARIA APG sortable-table pattern](https://www.w3.org/WAI/ARIA/apg/patterns/table/examples/sortable-table/)
  does; the inactive columns omit the attribute rather than carrying
  `aria-sort="none"`, and the ▲/▼ glyph is `aria-hidden` so it stays out of the
  button's accessible name.
- `select-name` ×1 — the create row's `<select>` had no accessible name (no
  wrapped or explicit `<label>`, no `aria-label`, no `aria-labelledby`). It and
  the other controls in that row are named with the same `aria-label={t(...)}`
  shape the two filter selects already used.

`web/e2e/a11y-products.spec.ts` reaches `/products` with `/api/*` stubbed (no Go
server, no session, no database), asserts the page and its product rows really
rendered **before** scanning (a scan of the loading shell proves nothing), then
pins the two defects by name — `button[aria-sort]` must be zero, the sorted `<th>`
must carry the state and it must MOVE on a sort click, every `<select>` must have
a non-empty accessible name — and runs a full-page axe scan failing on
critical/serious. A `button[aria-sort]` regression, or an unlabelled control
added to that page, now reds `npm run a11y` instead of shipping unseen.

### The rest of the signed-in routes — one route, one full-document scan (card t_9500edf3)

`/products` was the first signed-in page with a TABLE to be scanned, and it was
alone for a while: **twelve** authenticated routes were reached by NO a11y-family
spec at all. That is the shape that let 15 critical `aria-required-parent` nodes
ship green on `/profile` + `/settings`, and the shape that hid two more criticals
on `/profile` until its first full-document scan ran. `web/e2e/a11y-authed-pages.spec.ts`
now reaches every one of them — `/sales`, `/deliveries`, `/templates`, `/media`,
`/chat`, `/spam`, `/subscriptions`, `/activity`, `/feedback`, `/form-settings`,
`/builder`, `/connect` — with the session and the API stubbed at the browser
level, and runs a **full-document** axe scan per route, failing on
critical/serious. `src/lib/a11yAuthedCoverage.test.ts` pins each route to a page
on disk AND to a spec that really runs axe, so one cannot quietly fall out again —
and, because that route→spec half is a substring search any sibling naming the
path would satisfy, a second rule in the same file reads
`a11y-authed-pages.spec.ts`'s own `ROUTES` table and fails BY NAME when one of the
twelve is no longer an entry. (The substring limitation itself is the standing
follow-up `t_2275886d`.)

**A reachable route is asserted, not assumed.** Each arm requires the document to
have been served **2xx** (a 404 renders Next's "not found" page, which has no
landmarks and no controls — the lesson of `t_81b2a928`), requires the route **not
to have bounced** (the middleware redirect is followed by `page.goto`, so it
answers 200 and would otherwise be scanned as `/login`), and requires the CLIENT
render to have **settled** before axe runs: a React marker on the page's own
`<h1>`, the page's dictionary title, the shell, and a MEASURED floor on the
rendered text. That last part is not politeness — these routes are statically
prerendered, so the served markup alone is not evidence that the client component
ran. An arm that scans a loading shell is worse than no arm: it reports the route
as clean.

**`/form-settings` is walked tab by tab, and that is where this paid off.** A tab
is one click away in the SAME document, so the scan covers all seven of them; the
first run found **nine critical `label` nodes across three tabs** — General (the
title, slug and colour inputs), Restrictions (two `datetime-local` inputs and
three textareas) and Notifications (the email body textarea). Every one was a
sibling `<label>` with no `for`/`id`, i.e. the exact class `/profile` had been
fixed for, and every one is fixed. The same walk also refuses a control whose only
name is its **placeholder** — the class axe cannot see, because axe accepts a
placeholder as a name, so a clean scan does not prove the field is named.

**A tab is not the only state, so the walk does not stop at tabs (card
t_16ef9793).** One or two clicks INSIDE the Notifications tab — "+ Add
notification", then "+ Add condition" — renders the notification-rule editor, and
that state still carried **four critical `select-name` nodes and a
placeholder-only input** while every tab-level reading was green: the editor's
EVENT `<select>` had a sibling `<label>` with no `for`/`id`, its NAME input was
named only by `placeholder="Admin Notification"`, and the reusable
`ConditionPicker` (rendered here, on the Confirmation tab and in the CHIP feed
card) had an unnamed field select, an unnamed operator select and a value control
named only by its placeholder. The controls are now named — `fs-notif-name` /
`fs-notif-event` carry `id` + `<label htmlFor>`, and the picker's three controls
carry `aria-label`s from three new dictionary keys (`fs.condFieldName`,
`fs.condOpName`, `fs.condValueName`, all five locales; a numbered suffix
distinguishes rows when the caller renders a list) — and an arm drives that
editor state, both with and without a condition and in both shapes of the value
control (text input and choice dropdown), running the same axe scan and the same
placeholder census on each. A second arm proves the scan still bites by removing
the operator's `aria-label` from the LIVE DOM and requiring `select-name` to name
that element again. Measured on the pre-fix tree: `select-name` critical ×1 in the
editor and ×3 with a condition, with five controls in the census.

**The placeholder-refusing census is no longer scoped to one route.** It used to
run only on that `/form-settings` walk, so the class it refuses could — and did —
ship elsewhere while every scan was green: `/products`' create row named its
**name / price / stock** inputs only by their placeholders (the row's three
`<select>`s had already been given `aria-label`s for the `select-name` critical,
and the sibling INPUTS were left to the exemption axe gives a placeholder), and
`/builder` named its **slug input** (`placeholder="slug-url"`) and its **AI-prompt
textarea** the same way. Five controls, no gate, no axe violation. The census now
lives in `web/e2e/controlNames.ts` and runs on **every route that renders
controls**: all twelve signed-in routes in `a11y-authed-pages.spec.ts`, each of
`/form-settings`' seven tabs, and the whole of `/products` in
`a11y-products.spec.ts` — and the five sites are named from `aria-label`
(`bld.formSlug` and `bld.aiPromptLabel` are the two new dictionary keys, in all
five locales) so the announced name is the translated one and it survives the user
typing. The arms are proven to bite by removal: delete one `aria-label` and rebuild,
and that route's arm fails naming the control.

**Two redirect shells have no document of their own, and the spec says so rather
than reporting a scan of nothing.** `/space-picker` is a permanent 307 to
`/workspace-picker`; `/post-login` resolves the post-login target and replaces the
location within a frame. A full-document scan of either measures whatever it
landed on, so the arms assert that each LANDS on a route this family scans
(`/workspace-picker`, and the dashboard) instead of claiming coverage of the
shell.

**What is still NOT covered (stated, not hidden).** `/forms` is reached by
`a11y-dialogs.spec.ts`, but that scan is scoped to the dialog subtree
(`AxeBuilder().include('[role="dialog"]')`), so its DOCUMENT — the table, its
toolbar and its filters — is still outside every axe scan; `/forms` is therefore
the next route to add to this file, not a route to assume. `/admin`'s document is
in the same position (its feedback ROW is measured, by
`a11y-admin-feedback-row.spec.ts`). The dashboard's own contrast remains measured
out-of-band and held by the source guard below, with the exceptions this file
names.


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
> guard documented below rather than by the gate. Several exceptions ARE scanned —
> the pair of typed-slug confirmation dialogs, the canvas price spans,
> `/products` with its create row, and, since card `t_9500edf3`, EVERY signed-in
> dashboard route this file lists above — because stubbing the session-dependent
> fetch at the browser level puts the real component in the browser without
> needing a credential. What is still measured out-of-band is what no arm reaches:
> a route's DOCUMENT when only a subtree of it is scanned (`/forms`, `/admin` —
> see the section above), a dynamic `/products/[id]`-style segment, and any state
> the browser-level stubs do not render.

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

The guard checks five shapes, because this defect took all five:

1. the same class list renders a light surface **and** a token that cannot clear
   it there;
2. the token is *inherited* from a surface element above it — including the app's
   own page background (`html, body { background: var(--jf-bg) }`), which is what
   made the inactive tab labels a defect at 4.48:1 rather than a pass;
3. an **inline descendant** (`<code>`, `<span>`, …) paints a surface the inherited
   token cannot clear on — `<p class="…jf-muted">Brand <code class="bg-slate-100">ID</code></p>`
   is 4.41:1, and it shipped;
4. `text-[#9ca3af]`, which has no AA-safe placement in the app at all (2.54:1 at
   its best, on white);
5. a **GRAY label on a `.jf-btn-ghost` button**, judged on the three surfaces the
   button paints itself — `.jf-btn-ghost`, `:hover:not(:disabled)` and
   `:active:not(:disabled)` in `globals.css` (#ffffff / #f3f4f6 / #e5e7eb, read
   from the stylesheet on every run, never remembered). Those rules are
   unlayered, so a `hover:bg-*` utility on the same button never paints and the
   label lands on a darker surface the moment the pointer arrives:
   `text-slate-500` (#62748e) is 4.76:1 at rest and fails hovered (4.33:1) and
   pressed (3.85:1) — measured in Chromium on the built app. Resting-only scans
   cannot see it (axe never hovers a button), which is why this is a source rule.

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

**The surface is the NEAREST one, across all three colour guards.** CSS paints
through the innermost opaque background between the text and the page, so a token
nested inside two surfaces must be judged against the inner one. All three source
rules (`dangerText.test.ts`, `emeraldText.test.ts`, `mutedText.test.ts`) take the
last light-surface ancestor for this reason, and each pins the shape with a
fixture. Reading the *outermost* surface instead errs in **both** directions, and
the guards measure both: it hides a failure (`text-emerald-700` inside a
`bg-slate-200` chip within a white card is 4.35:1 — under AA — but the wrapper
reads 5.36:1 and passes) and it invents one (`text-slate-500` on an inner
`bg-white` card is 4.76:1 and passes, while the `bg-[var(--jf-bg)]` region behind
it reads 4.48:1). The outermost read is how the `/chat` card footer hid: see the
red/sky family below.

### The red/sky failure family, and the two shapes it is judged on

`web/src/lib/dangerText.test.ts` is the sibling rule for the **red and sky text
family** (`text-red-50..950`, `text-sky-50..950`) — the load-failure sentence, a
refused destructive action, an expired-link notice. Two of its properties are
worth stating because a token that reads as "obviously fine" on white is neither
of them:

* **The surface is the NEAREST light surface the text is painted on.** CSS paints
  through the innermost background between the text and the page, so a token
  nested inside two surfaces is judged against the inner one. Reading the
  *outermost* one is a 0.3:1 error in both directions and it is how a real site
  hid: the `/chat` confirmation card's failure footer sits on the card's own
  `bg-sky-50` tint, where `text-red-600` is **4.474:1** — under AA — but the
  ancestor chain's outermost surface is the `bg-white` transcript wrapper around
  it, where the same token reads 4.770:1 and passes (card `t_624b7275`).
* **A translucent LIGHT tint is judged at its OPAQUE base *and* at the composite
  — the minimum of the two.** A source scan cannot see the real backdrop, so the
  two honest options are to abstain or to judge a named surface. This guard judges
  BOTH, because each one alone let a site hide:
  - `/settings`' webhook-delivery cells are `text-red-600` on `bg-red-50/60`,
    which reads **4.511:1** on the tint but **4.360:1** on the `bg-red-50` it
    names — the entire margin over the 4.5 floor is 0.011, carried by the `/60`,
    and dropping it would have been invisible to every gate (card `t_624b7275`).
  - For a tint *lighter* than `--jf-bg` (`bg-slate-50` #f8fafc lum 0.95356 vs
    `--jf-bg` #f7f8f8 lum 0.93686, and the same for `bg-emerald-50` #ecfdf5 and
    `bg-amber-50` #fffbeb) the OPAQUE base is the *lighter* of the two, so it is
    **not** the strictest bound: `text-red-600` reads **4.5589:1** on the opaque
    `bg-slate-50` (legal) but **4.4831:1** on `bg-slate-50/20` composited over
    the page (under AA). Judging only the base passed that pairing. Card
    `t_8c10928a`.

  **The composite is judged against the DARKEST surface, not `--jf-bg`, and on
  the DARKER side of the 8-bit rounding** (card `t_fea22a0b`, two corrections of
  what this section used to claim):
  - *Backdrop.* `--jf-bg` is the LIGHTEST realistic backdrop, so a tint over a
    darker surface composites darker and can be sub-AA where the `--jf-bg`
    composite passes. `text-red-600` on `bg-slate-50/20` is **4.4831:1** over
    `--jf-bg` but **3.9817:1** over `bg-slate-200`, and on `bg-slate-50/10`
    **4.4470** vs **3.9123** (measured in Chromium). The guard therefore
    composites against the darkest opaque surface in the list below that is
    darker than the tint — `bg-slate-200` today — and the finding names it
    (`… over bg-slate-200`).
  - *Rounding.* The composited pixel is not reproducible in a source scan: the
    browser blends a PREMULTIPLIED 8-bit tint (rounding the alpha and the
    premultiplied source before the sum), so it lands **at most one 8-bit step**
    off the model — every one of the 891 (tint, alpha) pairs over `--jf-bg`
    differs by no more than a single step per channel. Which pairs differ at all
    is not reproducible (a source-over canvas and `color-mix(in oklab, …)`
    disagree with the model on different pairs, sometimes in opposite directions),
    so no mismatch count is quotable here — the bound is. The old bare-`contrast`
    model PASSED a pairing the browser paints sub-AA — measured: `text-red-600`
    on `bg-slate-50/30` is **4.4859:1** in the browser where the guard read
    **4.5139:1** and passed. The guard now evaluates the ratio on the modelled
    pixel AND on the pixel one step toward the token, and refuses if EITHER is
    sub-AA.

  For the six tints darker than `--jf-bg` the opaque base *is* the minimum over
  alpha OVER `--jf-bg`, so the composite over it can only be lighter. That no
  longer holds with the darker backdrop — there the composite is DARKER than the
  opaque base, so the opaque base is no longer the minimum and the two shapes CAN
  overlap — but the bound is still a genuine tightening rather than a behaviour
  change: `text-red-700` and `text-sky-700` clear AA on every tint AND every
  composite over EITHER backdrop (worst **5.2085:1** and **4.7502:1** — the bound
  fires on 0 of the 891 pairs for both tokens), so this guard's census and the
  repo-wide rule are unchanged by it.

`text-red-700` (#c10007) is the token both sites now use: 5.87:1 on `bg-red-50`,
6.02:1 on the `bg-sky-50` card, 6.42:1 on white. It is the step the whole
failure family moved to, and it clears the floor on every surface in the list.

Two boundaries, stated so the rule is not over-claimed. The saturated fill is
**PINNED by a fixture** in `web/src/lib/dangerText.test.ts` rather than left
implied: a saturated fill such as `bg-red-500/10` is **not** a light surface, so
the login / workspace-picker / magic-link / connect banners keep their alpha with
the browser (the two specs that paint them assert the measured ratio). A
translucent **white** (`bg-white/95`, the builder header) is *not* an abstention:
`white` is in the surface alternation, so the guard composites and judges it like
any other light surface — against the darkest surface darker than white
(`bg-slate-200`). That is safe rather than a hole, because such a surface is
strictly *darker* than white, so compositing white toward it can only land the
pixel between the two — still light, never darker than the base — so the
composite can never under-bound. A DARK slab, an overlay or a gradient is
likewise left to `web/e2e/a11y.spec.ts`. The `/chat` surface is the one site in
this family the `a11y` gate renders: `web/e2e/a11y-chat-card.spec.ts` measures the
failure footer's painted colour and the card's tint, and — since card
`t_53c75f82` — the page-level `role="alert"` as well, whose ink is painted on the
chat pane's white rather than on the card's tint, so the source rule and the
browser agree on both.

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
place. It is a **browser** guard, not a source grep, and it runs in the `e2e`
gate: its name does not start with `a11y`, so `playwright.config.ts` selects it
for the `feature-e2e` project rather than the `a11y` one — both are gates in
`scripts/preflight.sh` — with the API stubbed at the browser level, so it needs
no Go server, session or database:

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

The same shape guards the **other** slate-50 site, the workspace switcher's
`workspace.setDefault` row in its hover state (card `t_9d8f0431`, 4.492:1) — see
the switcher section above for that arm, its control, and why it is driven on
`/form-settings`.

The guard's lexer and JSX scanner live in `web/src/lib/jsxSource.ts`, shared with
the tinted-surface guard. They blank comments before reading class lists, and
they only honour a comment opener outside JSX **text**: the literal route pattern
`/v1/rest/*` written in prose at `/docs/api` was read as a `/*` block-comment
opener, which blanked 41 lines of that page (including a `</code>`) and made the
guard report a `<code>` 180 lines away as the ancestor of unrelated spans.

> The rest of the panel — the contrast tokens, the ⋯ menu and the choices
> editor — is still NOT covered by the gate. Keep running the signed-in scan
> above for those.
>
> **The panel's form-control NAMES are now in the gate (the `e2e` one).** Two e2e
> guards read the name sources off the ACTUAL rendered controls (both
> `feature-e2e`, part of `scripts/preflight.sh`):
> `web/e2e/builder-panel-select-names.spec.ts` for every `<select>`, and
> `web/e2e/builder-panel-choice-names.spec.ts` (card `t_c4abb920`)
> for EVERY visible `input`/`textarea`/`select` — the latter refuses a control
> whose only name is its `placeholder`/`title`, which is exactly the class axe
> cannot see (axe accepts a placeholder as a name, so a clean scan does not prove
> the choices editor's rows are named). Each spec also carries a mutation case that
> unnams the controls and requires the SAME invariant to reject them, so a green
> reading cannot be a vacuum.
>
> The builder's OWN controls are covered by the same rule from the other family:
> the `/builder` arm of `web/e2e/a11y-authed-pages.spec.ts` runs the shared
> census in `web/e2e/controlNames.ts` over the whole rendered page, which is how
> the toolbar's **slug input** and the **AI-prompt textarea** were found named only
> by their placeholders — the class axe accepts — and then given `aria-label`s
> (`bld.formSlug`, `bld.aiPromptLabel`, card `t_becb0f50`).
>
> **Another canvas pair, measured by the same shape.** The same browser-level
> stub pattern reaches one more authenticated surface: the **builder canvas**.
> `web/e2e/builder-canvas-price-contrast.spec.ts` drives `/builder?form=<id>`
> with the `/api/*` calls stubbed, so the gate reaches it with no session, no Go
> server and no database. It is a MEASUREMENT, not a class-name assertion: it
> reads each price span's painted colours out of the live DOM, resolves them by
> painting them onto a canvas (Tailwind v4 emits the slate ramp as
> `lab()`/`oklch()`, not `rgb()`), and asserts the WCAG ratio. It exists because
> the margin it guards is 0.008 — `#5e6ad2` on slate-50 `#f8fafc` is 4.492:1
> against the 4.5 floor, while the same colour on white is 4.700:1 — so a guard
> that rounds, or measures on white, or trusts a class name, reports the defect
> as a pass.
>
> That third shape is the one to reach for on the **surfaces the other two
> cannot cover**: a source rule can only composite a translucent tint against
> one fixed backdrop (`--jf-bg`), not the real stack of surfaces beneath it, and
> it cannot measure a margin this small.
>
> **This is a real gap, not a formality.** The builder's edit panel went
> unscanned long enough to accumulate two **critical** unnamed form controls
> (the value textarea and the conditional-logic rule picker) plus a set of
> contrast failures the same axe run catches in a second. Its control NAMES are
> in the `e2e` gate now (above), but the panel's CONTRAST is not covered by any
> spec. Before you change anything under `web/src/app/builder/`, scan it signed
> in rather than assuming the gates above cover it:
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
> from the `e2e` gate after all, without a Go server, a session or a database,
> by stubbing the handful of `/api/*` calls the builder makes at the browser
> level (`page.route`) — the same trick `web/e2e/form-settings-status-chips.spec.ts`
> uses. `web/e2e/builder-panel-select-names.spec.ts` and
> `web/e2e/builder-panel-choice-names.spec.ts` do exactly that and read
> the live DOM: for every block type whose panel renders a control they assert
> each one reports its name from a `<label htmlFor>` or an aria reference, and
> refuse a placeholder-only name (which is what axe accepts but a screen reader
> loses the moment you type). Prefer that shape over a source-text assertion —
> the source grep cannot tell a sibling `<label>` from an associated one, which
> is how three unnamed selects passed a 7/7 mutation proof.
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
