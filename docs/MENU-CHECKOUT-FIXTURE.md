# Menu-checkout option fixture — how to render a radio / image-choice checkout off-prod

A verification lane that needs a **menu checkout carrying an option group** used to
have nowhere to get one. No published form in production carries a `radio` or
`image-choice` block (read-only SQL, 2026-10-31: 69 live forms, 2 menu-enabled, **0**
with either block), and manufacturing one on a live workspace is forbidden — prod data
must not be written to verify a flow. Two QA cards therefore improvised a probe each
(one injected the template markup into the live page client-side, the other built a
throwaway Go probe) and deleted it with the card, so the next card started from zero.

This fixture is that probe, kept.

## One command

```bash
source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}   # JOMFORM_TEST_DATABASE_URL
bash scripts/menu-fixture-e2e.sh
```

It builds `api/cmd/menu-fixture`, seeds a scratch schema of the **shared test
database**, serves the real public handler on a loopback port, measures the served
checkout in Chromium at 1200px and 375px, and tears everything down (schema included).
Exit 0 means the page really renders both option groups and the geometry is right.

To look at the page yourself — or to drive it with a browser tool — keep it up:

```bash
bash scripts/menu-fixture-e2e.sh --serve
```

It prints the URL (e.g. `http://127.0.0.1:43991/menu-fixture`) and waits. Add a dish,
click the basket bar, and the sheet shows the merchant's option groups. The URL works
in any browser on the host that runs it: the fixture pins the request `Host` to its own
workspace subdomain, so there is no `/etc/hosts` entry, no DNS and no proxy involved.

## What the fixture is

| Path | What it does |
|---|---|
| `api/internal/menufixture` | Owns a scratch schema (`test_menufixture_fx_p<pid>`), applies the real migrations, seeds a user + workspace + account + two products + a published menu form whose attached published order form carries a `radio` block (`sd1`) and an `image-choice` block (`ic1`), and wires the real `httpapi.PublicHandlers`. |
| `api/cmd/menu-fixture` | That fixture as a command. Prints one JSON line on stdout (URL, slugs, block ids, product ids, schema) and serves on a loopback listener until SIGINT/SIGTERM. `-keep` keeps the schema, `-quiet` silences the migration log. |
| `api/internal/menufixture/fixture_test.go` | The `api` `test` gate's half: the served page carries **both** groups (a radio per declared choice, named by block id, inside `.sheetfoot`), the buyer fields and pay button are there, and a real `POST /<slug>/order` stores the chosen answers (`bank_direct`) with integer-sen money while an answer outside the choices is refused. |
| `web/e2e/menu-checkout-option-group.spec.ts` | The Chromium half: control size, the gap to each control's **own** label, full-width buyer fields, no sideways scroll — at 1200px and 375px — plus "answering one group does not clear the other". |
| `web/playwright.menufixture.config.ts` | A one-spec config with **no** `webServer`, so the measurement needs no `next build`/`next start`. |

## What it asserts, and where

| Claim | Guard |
|---|---|
| The checkout renders both group kinds, keyed by block id | `api/internal/menufixture/fixture_test.go` (Go, real HTTP + real Postgres) |
| The order route accepts and stores the chosen answers | same file, `TestFixtureOrderStoresOptionAnswers` |
| The served stylesheet's **cascade** keeps the control intrinsic and the buyer fields full width | `api/internal/httpapi/menu_radio_size_t_664549fc_test.go` (Go, cascadia) |
| The **layout** really is 13×13 with an 8px gap, and the buyer fields really are full width | `web/e2e/menu-checkout-option-group.spec.ts` (Chromium) |

The first three run on every `./scripts/preflight.sh`. The fourth cannot: it needs a
layout engine, a database and a long-lived server, so it runs on demand through
`scripts/menu-fixture-e2e.sh`. **It is not a gate** and must not become one.

## Measured on this host (2026-11-02, Chromium 151 via playwright's `chromium-1234`)

At 1200px: option rows 448 wide, controls 13×13 with an 8px gap to their own text,
buyer name/email 448×44, pay button 448×50, `scrollWidth == clientWidth`.
At 375px: rows 343, controls 13×13, gap 8, buyer 343×44, pay 343×50, no sideways
scroll.

The guards were proven to bite, one sabotage at a time, then restored:

| Sabotage | What went red |
|---|---|
| reset rule `width:auto;height:auto` → `width:100%;height:44px` (the t_664549fc defect) | both viewport arms: control width measured **448** (1200px) and **343** (375px), want ≤16 |
| reset selector `.sheetfoot .radios input[type=radio]` → `.sheetfoot input` (scope widened) | both arms: buyer name field **217px wide in a 448px row** (and 343px at 375px) |
| both radio arms emit `name="notes"` (the t_5d625806 defect) | the Go test: `the radio group "sd1" has 0 radios, want 2`; the browser: `no option control is named "sd1"` |
| `order_form_slug` dropped from the menu config | the Go test: `the checkout sheet carries 0 option groups, want 2`, and the order test fails to find the stored answers |

## Rules this fixture obeys

- **One scratch SCHEMA, never a new database.** It uses `$JOMFORM_TEST_DATABASE_URL`
  and `CREATE SCHEMA test_menufixture_fx_p<pid>`; the name's pid suffix is what lets
  `api/internal/testdb`'s existing sweeper reclaim a schema left by a killed run.
- **It drops what it created.** `Close` (and the command's signal handler) issues
  `DROP SCHEMA ... CASCADE`; `scripts/menu-fixture-e2e.sh` waits for that before it
  removes its work directory.
- **It cannot address production.** The payment client is constructed with an explicit
  `http://127.0.0.1:1` base URL — never `chip.New("")`, which silently defaults to
  CHIP's **production** base URL. `payment_method:"chip"` against the fixture therefore
  fails at a dead loopback port (the payfail path); `bank_direct` needs no provider.
- **No production data.** Nothing here reads or writes a deployment; `--serve` exposes
  a loopback listener only.

## Limits

- It needs `JOMFORM_TEST_DATABASE_URL`, a Go toolchain and `web/node_modules` with a
  Chromium installed (`cd web && npm ci && npx playwright install chromium`). Each
  missing prerequisite is a loud failure, never a skip.
- The seed is a menu of two simple products: no variations, no categories, no stock.
  A card that needs those should add them here rather than build a fourth probe.
- In the `e2e` gate the spec **skips** (`MENU_FIXTURE_URL` is unset — that gate serves
  the Next.js dashboard and has no menu); the skip is named in its output.
