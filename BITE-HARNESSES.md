# Bite-harness inventory — the proofs that a guard *bites*, and what runs them

This file answers one question that a green `./scripts/preflight.sh` cannot:

> **A guard runs on every gate. What proves the guard would fail if the behaviour
> it pins were removed?**

That is a **bite proof** (a *biteproof* / *bite harness*): it sabotages one thing
at a time in a guard's subject, runs the guard, and requires the guard to go RED —
then restores the source byte-identically. A guard nobody has ever seen fail is a
tautology, and it looks exactly like a guard that works.

**None of these harnesses is a per-merge gate.** The `scripts/preflight.sh`
table in `GATE-INVENTORY.md` names every gate a green run actually executed; this
file names the instruments that a green run did **not** execute. The two lists do
not overlap, and that is deliberate — see *Why they are not gates* below.

## The harnesses

One row per harness, the path backticked first. The `api test` gate checks this
table in both directions (`api/internal/biteharness`): a harness on disk that has
no row here fails the build, and a row naming a file that does not exist fails it
too. So the table cannot rot into a claim about instruments that were renamed away.

| Harness | Proves (the guard it backs) | Cases | Wall time here |
|---|---|---|---|
| `api/scripts/biteproof_menu_variation.py` | the menu-variation guards in `api/internal/httpapi/menu_variation_test.go`: the checkout payload carries the variant, live variation resolution, the selector markup, the card-tap path, per-row attribution, sold-out disabling. **STALE ON MAIN, measured 2026-09-26: 3 of its 8 cases run, then it aborts with `HARNESS BUG: 'card tap adds the base price...' occurs 0 times, expected 1, exit 2`. PR 770 took that path through the shared `addIfBuyable` helper, so the anchor it searches for is gone; `biteproof_menu_variant_gaps.py` covers that behaviour (and 22 more) and is green, so the net is intact, but this instrument needs re-anchoring before it proves anything again.** | 3 of 8 run | 110s (aborts) |
| `api/scripts/biteproof_menu_variant_gaps.py` | every guard declared in `api/internal/httpapi/menu_variant_scope_test.go` (each declared guard must be named by a case, and a case counts only when its named guard is among the failures; `TestMenuInStockVariantPickStillAdds` is the one listed control): the hint and clearPick lookups, the shared add path on both the change handler and the card tap, the live-stock re-checks (render-time attribute vs a live read, the basket on open, the checkout POST, the scoped live-stock route), the form/menu selector contract in both directions, and the sold-out locales | 23 | 486s |
| `api/scripts/biteproof_ses_replyto.py` | the SES Reply-To wiring guard in `api/internal/config/ses_replyto_wiring_guard_test.go`: both `email.SMTPConfig` senders in `cmd/jomform-api/main.go` carry `ReplyTo: cfg.SESReplyTo`, and an unset `SES_REPLY_TO` writes **no** header instead of falling back to `SES_FROM` | 5 | 122s |
| `api/scripts/biteproof_model_default_guard.py` | the docs-parity half of `api/internal/llm/model_default_guard_test.go` (card `t_8853fc29`): that guard read the pinned MCP tool descriptions out of ONE file (`api/internal/mcpsrv/tools.go`) while the registry the rest of the repo agrees on is the PACKAGE (#732 moved `disable_2fa` into `parity_issue396.go`). Sabotages a MOVE of `generate_form` / `generate_legal_page` into a sibling file and requires each reader to answer about the registry: the one-file reader falsely blames `tools.go` when the literal merely moved (and passes against a STALE copy left behind), the package reader resolves the move, refuses to pick between two pairs for one name, and refuses a registry file in a subdirectory instead of under-reading it. Each case's pair is LIFTED from `tools.go` with the guard's own pattern rather than hand-typed, every mutation must still compile, and both legs run at a base revision (`BITEPROOF_BASE`, default `origin/main`) against the working tree. **The refusal is a CAPABILITY test, not a byte test (card `t_9df01abd`): after every case it requires that at least one case made the base reader ANSWER DIFFERENTLY from the tree reader (verdict or claim text), and exits 2 without a verdict and without a bite count otherwise. A byte-equality refusal could not see a base whose reader has `origin/main`'s widening merged (PR 835) while still differing in bytes, and printed a false `0/7`.** **Two corrections by card `t_34ab7a00`, both measured at `origin/main` `272fda17` and both in this harness's own output: (1) the revision the refusal NAMES is derived from the commit that INTRODUCED the package-wide registry reader (`introducing_commit()`, reading the guard's own text for `mcpRegistryDir` at each revision) and its parent is only offered once its guard has been re-read and shown NOT to declare the marker — the old rule ("the parent of the newest commit at the base that touched the guard") was off by one the moment any later commit touched the file, and at `272fda17` it named `eda839c1`, a caught-up reader that refuses again, so the remedy the operator was handed looped; it now names `87040114b980`, the parent of `ba4a7d10`, which gives `BITES: 7/7` / `EXIT=0` when fed back in. The refusal also no longer closes with an unmeasured `7/7` for the revision it names — that number had been copied from the `t_9df01abd` run and was false for `eda839c1`; every figure it prints is measured by that run. (2) the byte-equality fast path now READS AND COMPARES THE SAME THING, so it can fire: the base body was read through the `git()` helper, which `.strip()`s stdout, while the tree side is the on-disk text — every Go file ends with a newline, so `base_guard == ORIG_GUARD` was unsatisfiable at any byte distance (measured: 49593 chars vs 49594) and the path was dead code, which is also why the card that found it had the CAUSE wrong (the two files were not "one byte apart"; the comparison could never hold). It reads through `git_text()` (no strip) and fires on a tree whose guard is byte-identical to the base's — which is the default run when this harness is executed on the tree the base points at** | 7 | see the measured line below the table |
| `api/scripts/biteproof_verify_fail_page.py` | the GET /v1/auth/verify failure-response guards in `api/internal/httpapi/verify_fail_page_test.go` (card `t_8c67149d`): a GET on a bad/expired link answers a localized HTML page rather than the machine JSON envelope, POST keeps the envelope, the three failure classes read differently, Arabic sets `dir=rtl`, and the page stays over the browser's 512-byte discard floor | 6 | 201s |
| `api/scripts/biteproof_unlock_class_guard.py` | the unlock class guard in `api/internal/httpapi/unlock_ratelimit_class_test.go` (cards `t_b2888b6c`, `t_5c6413aa`, `t_36bbfec4`, `t_48fd3de2`): every `.../unlock` pattern the SERVED route table holds answers a password burst with a 429, however its registration was spelled. Nine sabotages, each a compiling revert with `go build ./...` asserted OK: an INDIRECT unwrapped route (the shape the guard's FIRST, text-regex version could not see), the same route WRAPPED as a control that must stay green, a LITERAL unwrapped route (the shape the old guard did catch, so reworking it regressed nothing), a SHIPPED route losing its wrapper, an INDIRECT route registered from ANOTHER FILE in the package, an UNWRAPPED route registered INLINE on the served table in `server.go` (added by `t_5c6413aa` — the shape the SECOND guard's hand-re-listed mirror could not see) plus the same inline route WRAPPED as its control, and — added by `t_48fd3de2` — an UNWRAPPED route registered INLINE in `Server.Handler()` (the shape the walk missed because it built the table via `routes()` without ever calling `Handler()`, so the memoising `sync.Once` cached the pre-handler table; measured 193 patterns and 3 unlock routes, route absent, guard GREEN) plus the same `Handler()`-inline route WRAPPED as its control. A red must NAME the offending route, so a failure cannot be attributed to a sibling. Every case — the controls included — must also show the guard RAN and DROVE the route (added by `t_36bbfec4`): the named test's `--- PASS:`/`--- FAIL:` marker and the route inside the guard's own enumeration block, which needs `-v` because `go test` swallows `t.Logf` on a passing run. Without that a control's green could not be told from "the guard never ran" — `go test -run` matching nothing exits 0 with "no tests to run". The evidence check is itself self-checked on every run against a REAL vacuous output (must be rejected) and a REAL driven one (must be accepted) | 9 | 405s |
| `api/scripts/biteproof_model_id_scan.py` | the REPO-WIDE model-id scan in `api/internal/llm/model_default_guard_test.go` (cards `t_6784f5bb`, `t_16fa0164`, `t_492976fe`, `t_dba05702`): the scan that claims every file naming a model names the shipped default. Fourteen cases, each a compiling mutation with `go build ./...` asserted OK, and each comparing a BASE revision's guard against this tree's at its own named needle. Shape (a), the extension whitelist: a NEW `.mjs`, a NEW extensionless file, a NEW `.txt`, each carrying the retired id. Shape (b), the `deepseek-`-only token: a shipped `.go` file's mention swapped to a dead id from another family, and an id whose family is not on the guard's list at all (caught by its model SIZE tag). Shape (c) — card `t_16fa0164` — a LISTED family's id with NO DIGIT: the token rule's blanket digit filter dropped every `<family>-<tail>` id that happens to carry no digit, in this tree's probes from the `deepseek`, `gemini` and `claude` families (spelled here as families on purpose — this row is a shipped file the scan reads, so naming the full ids would make this document a stray mention itself). **Its BASE leg is RED, not GREEN**, because the pre-widening `deepseek`-prefixed reader CAUGHT that shape, which is what makes the loss a regression rather than a residual. Plus the shape that already bit before this card (a NEW `.ts`, BASE RED), and three NEGATIVE controls that must stay GREEN: the shipped id beside non-model colon tokens (a port, an npm script name, a Go module hash), a dead id inside a SKIPPED tree (`.next`, the recorded Turbopack artefact), and bare family words with no id separator (`claude` followed by a bare hyphen as a company name, and `phi`/`solar` as English words). **Card `t_492976fe` added the listed family's `id:tag` spelling** (three cases): the exemplar whose family carries a hyphen (BASE pinned at `5376463e`, the digit-filtered head the commit under review REPLACED, where it is RED — so the loss is a regression rather than a residual), the same shape with a HYPHENLESS family (BASE GREEN, the pre-widening reader is blind), and the negative control that a family word followed by a colon and a SPACE stays prose. **Its run-wide base is now DERIVED, not typed**: PR 841 MERGED, so `origin/main` stopped being the pre-widening reader and every legacy BASE expectation silently inverted (measured: 5 of 10 cases failed their BASE leg naming no guard defect). The default is the parent of the newest commit at `origin/main` that touched the guard, capability-checked against a colon-shape probe and REFUSED if that reader has caught up. Every mutated file is restored byte-identically and the tree is required unchanged. **Card `t_dba05702` added case 14**, the NEGATIVE control for an English hyphenated compound whose first word is a family name: BRANCH GREEN, BASE RED at `3b410e52` (the shipped pre-fix reader, pinned BY SHA precisely because the run-wide derivation is stale, see below), naming the probe file and the compound. It also recorded that **the run-wide derivation is stale for cases 1-5** — measured byte-identically red (5 of 13) on a pristine checkout of the base tree, so a pre-existing defect and not this card's regression: the derivation picks `c5427aed` (the parent of #861, which FOLLOWS the widening #841), a reader that already reads every file and every family, so it catches those five mutations while the capability check (which only probes the `id:tag` colon shape, a shape `c5427aed` does not catch) accepts it. Filed separately. | 14 | see the measured line below the table |
| `web/scripts/biteproof_next_managed_files.sh` | the Next managed-file guard in `web/src/lib/nextManagedFiles.test.ts`: the `agentRules:false` switch, the `/AGENTS.md` + `/CLAUDE.md` ignore rules, the `tsconfig` include, `web/next-env.d.ts` staying untracked and ignored, and the skip's own premise | 6 | 82s |
| `scripts/refguard_bite.sh` | the `refguard` gate's own suite (`scripts/refguard_test.sh`, 100 assertions): the six sabotages t_dc1200ff claimed as its bite proof but never committed (card t_bac5a2c5) — the hook installed as a no-op, the hook returning at the FIRST violation instead of reading to EOF, the allow-list hardcoding the first remote so a second breaks, `install` leaving the hook non-executable, a DELETE refused too (so the namespace becomes uncleanable), and `scan` reporting a staleness verdict from NO live data instead of returning 2. Each sabotage is applied to a throwaway copy and the committed suite is run against it via `REFGUARD_UNDER_TEST`; the unmodified guard is the control and must be GREEN. A sabotage whose rewrite does not change the text is REFUSED rather than counted — a byte-identical copy would silently test nothing, which is the trap the parent card warned about (the original hand-run harness caught its own `no-siblings` row this way). Value-level sabotages only: no placeholder is deleted, since that breaks argument counts and fails for the wrong reason. On demand only, never a gate | 6 | see the measured line below the table |
| `web/scripts/operator-bite.sh` | the operator-identity guard in `web/src/lib/operatorNotice.test.ts`: the operator block on the legal surfaces, one definition of the registration number, the RTL isolation, the locales, and no platform identity leaking into a merchant's footer | 11 | 14s |
| `web/scripts/biteproof_docs_index_render_sites.sh` | the RENDER-SITE half of `web/src/lib/docsIndex.test.ts`'s vacuity test (card `t_fc8a9ff2`, whose parent is `t_c258915a`): the six `docsSrc.includes(...)` checks that prove the page really renders what the guard asserts were made against the RAW page source, while this file's own rule is that a comment never counts — so one `//` line satisfied the guard's stated answer to its own limit and the whole suite stayed GREEN on a page that renders no slug id at all. The one stripped `docsSrc` made EVERY leg comment-satisfiable at once, so the harness drives each leg, not just the two the card measured (card `t_3b51370d` added the four that were left). **Fifteen driven cases** over `src/app/docs/page.tsx` alone (a "case" here is one `check()` call / one `BITES` line = the fifteen labelled cases: `BASELINE` + `CASE 1..14` + `NEGATIVE CONTROL 12`; the script's sixteenth `==` line is the `GUARD RESTORED` trailer). Broken down: the baseline, four CONTROLS (CASE 1, 3, 8, 10), seven HOLE cases (CASE 2, 4, 5, 6, 9, 11 — each carrying the mutation with a comment form — plus CASE 7, the `GROUPS` parse) and one NEGATIVE control (CASE 12). **Card `t_1f175168` added CASE 13 and 14**, the two render sites the vacuity test never asserted at all: every entry HEADING (`{f.title}`, rendered at both `:430` and `:475`) and the guide LINK (`href={f.href}`, `:436`), so the whole suite stayed GREEN 10/0 while every feature heading on /docs rendered the literal word "Feature" and every "Full guide" link pointed at `/docs` itself. Each is the HOLE shape — GREEN under the base guard, RED naming `[render-site-entry-title]` / `[render-site-guide-link]` under this one, with exactly one failing test. The heading pattern is the JSX TEXT slot `>{f.title}` and needs two lines, because `key={f.title}` is an ATTRIBUTE on the same element and a test accepting a bare `f.title` would pass while the heading rendered a constant; the link pattern is the ATTRIBUTE slot `href={f.href}`. The legs of the same vacuity test this harness still does NOT drive are `export default function DocsIndexPage` (`:406`) and `<T k=` (`:428`); the other six `docsSrc.includes(...)` legs are driven above. The guard's sha256 is re-checked unchanged at the end, and every mutation helper FAILS CLOSED: a token that is no longer on the page sets `fail=1` and says so, instead of silently skipping the case and letting the run print PASS with that case's line absent (stated limit: on today's tree the premise check refuses first for that shape — measured, exit 2 before any case — so this is defence in depth, not a reproduced false PASS). The four CONTROLS (the render sites for `{f.body}`, `id={f.slug}`, `id={g.id}` and `<T k={g.labelKey} />` moved off the page with NO comment) are RED in both columns, so the check is not vacuous in either direction; the six comment-carried HOLE cases drive the same mutation by a `//` line, a `{/* … */}` JSX container and a module-scope `/* … */` block comment (GREEN under `origin/main`'s guard — the defect — and RED, naming the leg it broke, under this tree), plus one HOLE case that is not a render site at all: a whole feature entry commented out of `GROUPS` (card `t_8b8d3365`), which `parseGroups` read from the same raw text, so a commented-out feature entry was still counted and satisfied the docs-parity contract for a feature the page no longer lists. The two legs a reader NOTICES FIRST — the category ANCHORS (`#sales-payments`, `#security`, …) and the TRANSLATED category labels — are the ones card `t_3b51370d` added, each as a CONTROL plus a `//`-carried HOLE, all four RED naming `every category is a named, translated group` with exactly 1 failing test. The NEGATIVE control is that a comment beside a REAL render site stays GREEN under both guards. The attribution rule from the sibling `biteproof_docs_index_comment_anchor.sh` is kept: the NEW red must be **exactly one** failing test, read from node's own summary (`ℹ fail N`, not a `✖` count — node prints each failure twice). The verdict rule is `biteproof_docs_tool_claims.sh`'s `rule_of` (card `t_420ca40c`), self-checked on every run against three real degenerate node runs, and the page is restored byte-identically on every exit through the `trap`. Its base check is no longer just `git cat-file -e`: a base whose reader has CAUGHT UP to this tree is refused by CAPABILITY rather than reported as a `fail=1` full of `does not reproduce the premise` lines — the base guard's text is read for a DECLARATION of `docsSrc`, and the read has three outcomes: a declared post-fix reader is refused, a declared pre-fix reader is measured, and a reader spelled as something else (a longer expression, a helper) is ALSO refused rather than assumed pre-fix (card `t_01df90af`; measured, a guard declaring neither reader used to fall through and measure). Every refusal names its cause and derives a `BITEPROOF_BASE` from git, and is proved on every run against three real fixtures: a post-fix one (must exit 2 with a `BITEPROOF_BASE` suggestion derived from git), an unplaceable-reader one (must exit 2 naming that cause) and a real pre-fix one (must not refuse) — cards `t_858620fc` and `t_01df90af`. **THE SHARED INPUT IS SERIALISED PER WORKTREE** (card `t_03a646e2`), which the harness lacked: every case rewrites the SHARED `src/app/docs/page.tsx` and `run_guard` reads it throughout, so two overlapping runs in ONE worktree printed false verdict lines — reproduced by QA on `f63fa325`, two runs 1s apart printing 7 and 4 `***` lines (a false `baseline: NEW guard wanted GREEN, got RED` among them) against 0 for a solo run, 9 and 11 BITES instead of 13, rc=1 for both (no false PASS reachable). It now takes the sibling's per-worktree `flock` — keyed by `git rev-parse --absolute-git-dir`, under a host-global `/tmp` base, parent takes it on fd 9 and re-execs, the child PROVES it holds it before printing, a contended run WAITS, a wait-out is exit 75 — held from before the premise check, which READS the page. Two overlapping runs in one worktree now print 13 BITES with 0 `***` lines each (measured) while the pre-fix bytes print 12+9 `***` lines under the identical shape, and `web/src/lib/harnessLock.test.ts` drives both harnesses (its D3 leg was silently skipped on every linked worktree, because the second tree it named was hardcoded and absent there; it is now derived from `--git-common-dir`) | 15 | see the measured line below the table |
| `web/scripts/biteproof_docs_tool_claims.sh` | the tool-claim shapes in `web/src/lib/docsToolClaims.test.ts`: the two phrasings the guard MISSED (an underscore-free name in `as the X tool`, and the header-comment parenthetical `… pairs with (propose_write)`, both one-line and wrapped across two comment lines), plus the two it already caught, plus two negative controls (a registered name stays quiet, real prose stays quiet). Each case runs the guard at the branch's BASE (`origin/main`, overridable with `BITEPROOF_BASE`) and requires it GREEN on the mutated tree — that is the "it missed this" column — then the guard in the working tree and requires it RED naming the offender, then restores the page byte-identically (sha256). The base guard must itself be GREEN on the unmutated tree or the script exits 2 without a verdict (a red base would make every case read "does not reproduce"), so it is not a comparison against a known-broken revision. **The verdict rule requires POSITIVE EVIDENCE THE GUARD RAN** (`t_420ca40c`): it used to be `if grep -q '^ℹ fail [1-9]'; then RED; else GREEN` — a decision from the absence of a failure line, so a guard that never started (a missing file, a file that cannot parse, an all-skipped guard) read GREEN, which on the OLD column means "the base guard missed this". GREEN now needs node exit 0 with `ℹ pass [1-9]` **and** a `✔ ` test line; RED needs `fail [1-9]` **and** `pass [1-9]` (a guard that cannot load prints `tests 1 / pass 0 / fail 1`, and calling that RED would print BITES for a case that wanted RED while the instrument was broken); anything else REFUSES with exit 2. The rule is SELF-CHECKED on every run against three real degenerate node runs (all-skipped, cannot-load, missing) and is proven load-bearing by restoring the pre-fix rule: the self-check then refuses, naming `leg 2: an all-skipped guard read as GREEN`. Its measured limit is stated in the script: node counts a test-less file as `tests 1` with a `✔ <file>` line, so an emptied guard still reads GREEN — that case is covered inside the guard by its own "cannot pass vacuously" test. **The verdict channel is PER-RUN** (`t_ff3640e7`): the guard's output used to be written to the fixed host-global `/tmp/bite.last.out` and re-`cat`ed by both call sites, so two runs overlapping in ONE worktree read each other's output and printed four false OLD/NEW verdicts (reproduced); it now travels in a variable set by `run_guard` (which is why `run_guard` is never called in a command substitution), and the in-tree `.ts` copies of the guard are per-run (`$GUARD.oldcheck.$$.ts`) for the same reason. The `trap ... EXIT` restores BOTH mutated pages and removes every per-run temp, so an interrupt cannot leave a mutation on disk (the shipped `cp "$EMBED_BAK" "$EMBED"` sat on the non-error path only). **THE SHARED INPUT IS SERIALISED PER WORKTREE** (`t_ed66094c`): the channel fix removed the shared OUTPUT but the cases still mutate the two SHARED pages, and `run_guard` walks the whole docs tree, so a second run started while the first was mid-case read a page the first had already mutated — reproduced, 3/3 rounds, two runs 2s apart in ONE worktree printing 33 false `***` verdict lines across 6 runs while the exit code stayed 1 (no false PASS is reachable; the harm is four wrong verdict lines). The harness now takes a `flock` on a file keyed by `git rev-parse --absolute-git-dir` (one key per worktree, stable from any CWD, different for the primary tree and each linked worktree; `--show-toplevel` is resolved against the CWD and would not be mutual), under a HOST-GLOBAL base (`/tmp`, never `$TMPDIR` — the run it must exclude usually belongs to another PROFILE, which is how the defect was found). The parent takes it on fd 9 and re-execs; the child inherits the descriptor and PROVES it holds it (a second take on the same file must be refused, a fresh file in that directory must lock fine) before printing anything; a contended run WAITS and says so rather than failing, and a wait-out is exit 75 rather than a red harness. Held from before the premise check, which READS both pages. Removing the take or the child's proof each turns a leg of `web/src/lib/harnessLock.test.ts` red (measured: both sabotages caught, harness restored byte-identical). **THE D3 LEG OF THE FILE ABOVE WAS VACUOUS ON EVERY LINKED WORKTREE UNTIL `t_03a646e2`**: `web/src/lib/harnessLock.test.ts` named a hardcoded sibling worktree path behind an `existsSync` guard, and a linked worktree has no `.worktrees/` inside it, so the leg that proves two worktrees do not serialise each other was silently skipped exactly where the gate runs. The peer tree is now derived from `git rev-parse --path-format=absolute --git-common-dir` (the PRIMARY tree), and the file drives this harness AND `biteproof_docs_index_render_sites.sh`. Run alone on this host: exit 1 with the three `does not reproduce` lines, which is CORRECT on a branch cut from `origin/main` (OLD == NEW there, so no gap to reproduce; on the branch with the guard changed they read `ok … the gap reproduces`), and `git status` clean | 6 | 13s |
| `scripts/biteproof_web_image_gate.sh` | the `webimage` preflight gate (a real cache-only `docker build -f web/Dockerfile`), which is the only gate that builds the SHIPPING web artifact. Three cases, each requiring the gate to go RED **and** the host suite (`npm run test:unit`) to stay GREEN — that second direction is what proves the host never saw the class: a healthy tree at HEAD is green (not a tautology), a `.dockerignore` rule that filters a `COPY` source out of the build context (the MCP-PARITY.md defect one layer out), and a runtime-stage `COPY --from=build` naming a path the build stage never produced (outside `buildContext.test.ts`'s reach, which parses the build stage's COPYs only) | 3 | 1st run on this host, load ~30: see the measured line below the table |
| `web/scripts/biteproof_mcp_tool_inventory.sh` | `web/src/lib/mcpToolInventory.test.ts`, both directions: case 1 runs the guard on the REAL pre-fix page (the revision at the merge-base with `origin/main`) and requires it RED naming all ten tools that page omitted, then the mutation cases — a registered tool whose row is deleted (forward), a new Go tool with no row (forward), a row that is not registered (reverse), a duplicated row, the em-dash placeholder row, a hand-typed count, a note-only group counted as a category, and the CLAIM case (card `t_5f8083eb`): a single FILE named as the registry in the page's metadata description, proven on both legs — the file that shipped the claim (`tools.go`) and the file that falsified it (`parity_issue396.go`) — each RED for its own named reason, the needle derived from the current per-file `&mcp.Tool{` counts. Every file restored byte-identically (sha256 re-checked). **Case 1 is coupled to a merge-base with `origin/main`, so it refuses on a tree cut from current main (there the page at the merge-base is byte-identical to this tree's, and the case would prove nothing). It is run LAST for that reason: it used to sit first and its `exit 2` took every case after it down, which is how case 7 was unreachable on this card's own branch.** | 9 | see the measured line below the table |
| `web/scripts/biteproof_settings_tabs.sh` | the settings-tab-strip guards in `web/e2e/settings-tabs.spec.ts` (card `t_dc06`): the strip class, the reveal wait, the inset focus-ring rule, and the tab-count wait — each mutated one at a time, with the page-side cases REBUILDING the app first so the spec measures this tree's build rather than a previous one. Every case must produce a red at a NAMED assertion, not a crash or a failed build | 4 | see the measured line below the table |
| `web/scripts/biteproof_docs_index_comment_anchor.sh` | the comment hole in `web/src/lib/docsIndex.test.ts`'s `declaresId()` (card `t_7b24b035`): the helper searched the RAW source while every other reader in the file strips comments first, so an anchor id that existed ONLY inside a comment satisfied all four legs — and `/docs` shipped a sidebar row pointing at an anchor no page renders while `test:unit` stayed 516/0 and `next build` exited 0. Seven cases, each a phantom anchor made to look declared by text a reader never sees: the card's own measured mutation (a `//` line behind a `NAV_SECTIONS` row), the same phantom in a `{/* … */}` JSX container and in a module-scope block comment, the same phantom behind an INDEX ENTRY's `href` (a different leg of the same helper), the `slug:` exception for `/docs` itself in both directions (a `// slug:` line must no longer satisfy it; a real `slug:` must still satisfy it), the baseline, and the negative control that a REAL `id:` declaration still satisfies the helper after the strip. Each case runs the BASE guard (`origin/main`) and requires it GREEN — the hole — then this tree's guard and requires it RED naming the phantom **and exactly one failing test**, so a red is attributable to the sabotage rather than a sibling. | 7 | see the measured line below the table |
| `web/scripts/biteproof_forms_bulk_failure.sh` | the /forms BULK-publish failure guard in `web/e2e/forms-bulk-failure.spec.ts` (card `t_c9e37f49`): a refused bulk publish must leave the merchant-facing sentence on screen 3s later, in English, and must not add a list reload. The pre-fix shape is re-created byte-for-byte (the **baseline is a real case, not a preamble**: the unmutated tree must be GREEN first, or a red below could be the harness itself, so the spec would otherwise be a tautology) and then `load()` is moved BACK into the `finally` beside `setBusy(false)` — the shape whose `return` on both failure branches reloads the list and lets `load()`'s `setErr("")` replace the sentence ~200ms after it is set. Both page-side cases rebuild the app first (`npm run build`), so the spec measures THIS tree's build rather than a leftover one, and the red must land at a NAMED assertion (`the bulk refusal must render a message at all` / `the refusal must survive the whole 3s window` / `a refused batch must not reload the list`), not at a crash or a failed build. `src/app/forms/page.tsx` is restored byte-identically after every case (sha256 re-verified) | 2 | 190s |
| `deploy/backup/jomform-backup_bite.sh` | the failure paths the nightly backup's suite asserts (`deploy/backup/jomform-backup_test.sh`, the gated `backup` suite): the fail-closed seam and the legacy refusal paths, sabotaged one at a time in a throwaway copy | 29 | 2110s |
| `scripts/biteproof_api_test_gate_timeout.sh` | the api `test` gate's explicit per-package budget in `scripts/preflight.sh` (the `-timeout` this card added), and the `selftest` check 17 that guards it. Six cases read the **argv `go test` was handed** under a stubbed `go`, never the file's text — because the sharpest case keeps `-timeout` in the gate's printed LABEL and removes it from the command it EXECUTES, which a grep-based guard calls healthy. Cases: the flag removed from the gate line (byte-for-byte the pre-fix `main` shape that produced `panic: test timed out after 10m0s` for a green `internal/httpapi`), a default of `0` (the alarm disabled — must be REFUSED rather than handed to `go`), the flag in the label but not in the executed command, the flag hardcoded so `API_TEST_TIMEOUT` no longer reaches `go test`, and — end to end — the REAL `selftest` gate against the mutated runner, required to go RED with every failure naming the budget (so the red is attributable to this sabotage and not a sibling check). `scripts/preflight.sh` is restored byte-identically after every case (sha256 re-verified) | 6 | 150s |
| `scripts/biteproof_preflight_reuse.sh` | the safety guards of the per-tree gate result record (`scripts/preflight.sh` + `scripts/gate_result.sh`) and the `selftest` checks 31/32/33/34/35/36/37 that back them: the documented red-tree path (`--only <PREFLIGHT_RERUN> --reuse-record`) must RUN rather than die on "vouches for NO gate" (the refusal has to be judged on the RECORD, before the selection filter); one reuse event must ledger exactly ONE line (the consumer and the runner both observe it, and when both wrote, `--report` — the instrument the saving is read from — reported double: 588s read as 1176s, every lane in the host ledger's window counted twice); a record observed under a different NODE must stay usable and be announced as an advisory rather than refused (on this host node is a property of the SHELL — an interactive shell resolves v24.19.0 via nvm, a worker lane resolves v26.7.0 from `.hermes/tools` — so hashing it into the fingerprint refused records on identical bytes and left reuse dead for the shape that runs the gates); the test-DB host must be a fact about the SERVER and not its SPELLING (card `t_7add0cfa`: `127.0.0.1` and `localhost` are one Postgres, but the host string was hashed verbatim, so the gate that wrote the green record for tree `f98b5fcf` was refused by the very next reader that spelled it the other way, silently — "the environment changed since that record" — costing the lane the whole gate, the host-wide flock whose waiter measures 50-86 minutes); the test-DB **credential strip** must cover every shape it OWNS (card `t_cd6136ca`: the pre-fix all-or-nothing expression `s#^[^@/]*@##` let a password containing `@` or `/` reach the record file and its key material, so rotating such a password would silently refuse a valid green record), and a test-DB value the strip CANNOT cover must be **refused** (card `t_99229419`, case 6): PostgreSQL's keyword/value DSN (`host=… password=… user=…`) carries no `scheme://` and no userinfo delimiter at all, so before the scheme guard it was returned by `${url#*://}` **verbatim** with its password, which the API's own driver accepts (measured with this repo's pinned pgx v5.10.0); and the strip's anchor must not ALIAS two servers (card `t_43750cc1`: the old unconditional step-2 strip dropped everything through the last `@` in the WHOLE value, so for a DSN with NO userinfo whose DATABASE name carries an `@` the host and port went with it — measured on the parent, three DSNs naming hostA/db, hostB/db and hostA/other all produced the component `x` with ONE key, and the record written against hostA was REUSED by `gr_reusable` as evidence about hostB, the FALSE-REUSE direction). Case 4 restores the host-speelling defect one-sidedly by renaming the canonicalising function and appending the raw component, and asserts BOTH that the mutation landed and that the two spellings then disagree, since a mutation that never landed is a silent green. Case 5 puts the all-or-nothing expression back, verifies against the card's own pre-fix table (both components, `2@…` and `u1:pw/2@…`) BEFORE it spends a selftest run, and requires the red to name the credential/BLIND/one-server legs. Case 6 removes ONLY the scheme guard, leaving the right-anchor strip in place: that is the one shape that discriminates the two checks, because the keyword/value DSN contains no `@` at all, so check 35 still passes and check 36's component/record leg is the only one that can fire — case 5's wholesale revert reds check 35 as well, so its needle cannot carry check 36's text without a red for case 6 reading as off-topic there. Case 6's own mutation is verified by VALUE before it spends a selftest run (it also requires the URL form to still strip, so it measures the scheme guard and not a broken function). Case 7 covers the OTHER direction: it keeps the right-anchor strip for a URL-form value (so check 35's `@`-in-password shapes still pass) and removes ONLY the address test, verifying by VALUE that it reproduced the collision AND that the URL form still strips, so check 37 is the only failure that can fire. Case 0 is the control (the unmutated tree must pass the whole selftest, so a red below is the sabotage and not this tree); the sabotage cases each run the REAL `selftest` gate end to end and require it RED **with every failure naming that case's property**, so an unattributable red cannot be counted as proof. **Cases 4, 5, 6 and 7 are SURGICAL** (cards `t_43750cc1` and `t_99229419`): all the guards they sabotage live in the one `gr_testdb_component`, so a mutation that restores a whole pre-fix function reds several checks at once and `verdict` then reports the red as "for reasons OTHER than" the case's own property — measured, the raw pre-fix component also returns `2@…` for a password containing `@`, which is check 35's leg. Each of the four now replaces only the inner body that carries its own rule (via the awk the script already used for case 5) and asserts by VALUE that the other rules still answer correctly, so a non-surgical mutation fails the case's premise instead of being counted. Case 4 drops only the canonicalising sed (checks 35/36/37 unchanged), case 5 swaps only the anchor block for the all-or-nothing expression (checks 34/36 unchanged), case 6 removes only the scheme guard (checks 34/35/37 unchanged), and case 7 swaps only the anchor block for the unconditional step-2 strip (checks 34/35/36 unchanged). No case removes the scheme guard except case 6's own, which is what keeps every other case's red attributable. | 8 | see the measured line below the table |

Three rows above (`scripts/biteproof_web_image_gate.sh`,
`web/scripts/biteproof_mcp_tool_inventory.sh`, `web/scripts/biteproof_settings_tabs.sh`)
were added by the card that found them unregistered: all three files had been on
main since #758 / #766 / #787 without a row, and none of those three changes
touched this file — so `TestEveryBiteHarnessIsRegisteredInTheInventory` failed the
`api test` gate on a pristine main.

`api/scripts/biteproof_model_default_guard.py` is the same shape one card later:
the guard it covers (`api/internal/llm/model_default_guard_test.go`) had no row
because no harness had ever backed it, which is why widening its registry scope
could only be proved by writing the first one. **Measured on this host (card
`t_8853fc29`, load ~25 on 6 cores): 2m43s, ALL 7 CASES BITES, EXIT=0.** Each case
printed its two legs at their own named needle and the unmutated tree was green
under BOTH readers first (the control), so no case's red is this tree being
broken. The three sabotaged/created files came back byte-identical (`tools.go`
`fdc1caf043712ce6`, `parity_issue396.go` `27eca0bf9ce259c7`, the guard
`2a09eec123dacaca`), the throwaway `api/internal/mcpsrv/sub/` directory was
removed, and `git status` held **3 entries before and 3 after, 0 introduced by the
run** — the tree was already dirty because this harness runs on the branch that
changes the guard, which is why it compares status before/after instead of
demanding a clean tree.

**Re-measured 2026-09-28 by the card that made its refusal a capability test**
(`t_9df01abd`), on PR 841's head tree (`5376463e`, guard `d506376d29bfb991`) with
the card's fix in place:

- **Default base (`origin/main` `72092730`, the caught-up reader): exit 2 and NO
  verdict.** All 7 cases ran, **0/7 disagreed**, and the harness printed the
  refusal naming the revision to use instead (`87040114b980`, the parent of
  `ba4a7d10`) — it did not print a bite count at all. The pre-fix harness on the
  same tree printed `BITES: 0/7` / `EXIT=1`, which is the false verdict this card
  exists to remove. The three files came back byte-identical and `git status` held
  **1 entry before and 1 after, 0 introduced**.
- **At the named base (`BITEPROOF_BASE=ca110813`, the single-file reader): 7/7
  bites, EXIT=0**, with `cases where the two readers DISAGREED (the premise
  held): 7/7`.
- **The refusal is itself load-bearing** (self-bite): neutralising only the
  disagreement test and running ONE case at the same caught-up base restored the
  reported shape — `BITES: 0/1`, `EXIT=1` — so the refusal is what turns that into
  an exit-2 refusal rather than a number that reads like a finding.

**Re-measured 2026-09-29 by card `t_34ab7a00`** (the rework that fixed the two
defects above), on the committed tree `449da6cf` at `origin/main` `272fda17`
(guard `64e150f14307f61a`), `git status` **0 entries before and 0 after**:

- **Two defect reproductions, from the PRE-FIX harness, both measured, because a
  claim about what a refusal says has to come from running it.** Default base
  (`origin/main` = the tree itself) printed `base reader: origin/main (272fda17),
  49787 bytes (49593 chars); tree reader: 49788 bytes (49594 chars)` and then the
  refusal — one newline short of byte-identical, so the fast path **did not fire
  at all**, and this is the run that shows the CAUSE previously recorded for that
  was wrong (the two files were never "one byte apart" in the sense the row said;
  the comparison could not hold at any distance). Its suggestion was
  `eda839c1` (the parent of the newest guard-touching commit `0c1c6e9c`) and it
  closed with `at eda839c153ab the same tree is 7/7` — fed back in verbatim that
  revision **REFUSES AGAIN** (`base reader … 22327 bytes … tree reader: 49788
  bytes`), so the remedy looped, and the `7/7` was false for it.
- **Default base, POST-FIX: the byte fast path fires — `exit 2` in 0.17s, before
  any `go test` runs**, with `REFUSING: the guard at origin/main (272fda17) is
  BYTE-IDENTICAL to this tree's (64e150f14307)`. That is the path item 4 asked
  about, proven by running it on a tree whose guard IS byte-identical to the base's
  (the default run on the tree the base points at).
- **The named revision, fed back in verbatim, is a WORKING base:
  `BITEPROOF_BASE=87040114b980d9c2c9db377de0046644910f4f8c` → `BASE RED (bites)` /
  `BRANCH GREEN as expected` on the two move cases, `BASE GREEN as expected` /
  `BRANCH RED (bites)` on the stale-copy controls, **`cases where the two readers
  DISAGREED (the premise held): 7/7`**, `MUTATIONS THAT DID NOT BITE: 0 []`,
  **`BITES: 7/7`**, `EXIT=0`, 257.87s.** The three sabotaged files came back
  byte-identical (`tools.go` `cfca3ae5f1025c63`, `parity_issue396.go`
  `27eca0bf9ce259c7`, the guard `64e150f14307f61a`), `api/internal/mcpsrv/sub/`
  was removed, and `git status` held **0 entries before and 0 after, 0
  introduced**. The refusal's suggestion was checked by re-reading the revision's
  guard out of git (`mcpRegistryDir` absent) before it is printed, so a suggestion
  that is a caught-up reader is reported as unattributable instead of being handed
  over — the loop the reproduction above walked into.

What was measured on this host for those three rows, and what was not — stated
apart, because the two are not the same claim:

- `web/scripts/biteproof_docs_index_comment_anchor.sh` (card `t_7b24b035`, the row
  added last): **65s, ALL 7 CASES BITES, EXIT=0.** Every case's base leg
  (`origin/main` `7d0489b3`) read GREEN on the mutated tree and every new-guard leg
  read RED naming its own phantom with exactly 1 failing test — case 1 verbatim:
  `/docs/security#qa-fake-anchor (Guides) — /docs/security declares no id
  "qa-fake-anchor"`, case 4 (a different leg, the index entry's `href`):
  `Trusted devices (2FA) → /docs/security#qa-fake-anchor`. The two negative controls
  (a REAL `id:` declaration, and `/docs#form-analytics`, a real `slug:`) stayed GREEN
  in both columns, so the strip did not blind the helper. All three sabotaged files
  came back byte-identical and `docsIndex.test.ts` was never written to
  (sha256 unchanged). Two harness bugs were found and fixed while building it, both
  recorded in the script: the backup names were keyed by BASENAME, so the two
  `page.tsx` files overwrote each other's backup; and three cases inserted their nav
  row at line 1, ABOVE the `const NAV_SECTIONS` anchor the guard slices from, so they
  read GREEN for a reason that had nothing to do with the hole. That second one is
  why the cases now resolve their anchor line and refuse if it moved.

- `scripts/biteproof_preflight_reuse.sh` (the union re-cut of PR 865, card
  `t_77f24357`): **re-measured on the union's head tree `570d87b8` (tree
  `f4df2f3d`) by the verify card `t_3ab1cddb` — 16 checks passed, 0 failed,
  `EXIT=0`.** Case 0 is the control, `preflight self-test: 71 passed, 0 failed`,
  with checks 34, 35, 36 and 37 all green simultaneously, so a red below is the
  sabotage and not this tree; cases 1-7 each turned the REAL `selftest` RED with
  every failure naming that case's own property (case 6 naming check 36, case 7
  naming check 37), and both mutated sources came back byte-identical after every
  case with this tree's `git status` unchanged. The per-shape half was measured on
  the union's bytes as well: the credential is absent from every written record,
  the three loopback spellings share ONE key, and a rotated `@`/`/` password does
  not move it (the record drive below). The
  case mutations themselves need a frozen tree — the harness rewrites
  `scripts/gate_result.sh` in place, so it cannot run while this lane's gate is
  queued — and each case costs one full standalone `selftest`.

  The last full runs, kept because they are what the cases were cut against and
  what the union's cases are derived from: the branch's own run before the union
  (card `t_99229419`: `ALL SIX CASES BITE (0, 1, 2, 3, 5, 6), EXIT=0`, control
  `preflight self-test: 67 passed, 0 failed`), and the host-spelling card's run
  (card `t_7add0cfa`, tree `7c68c986`: cases 0, 1, 2, 3 and 4, control
  `preflight self-test: 67 passed, 0 failed`, case 4 red on two failures naming
  the component leg and the reader leg). Both are pre-union: neither carried check
  36, and the union's case bodies are the surgical versions of those cases with
  the scheme guard kept in each, so their `EXIT=0` is evidence about the cases as
  they stood, not about these bytes.

  The union's own case count is **8** (`case 0` the control, plus cases 1-7): this
  branch contributes case 6, and cases 4, 5, 6 and 7 are each surgical — each
  replaces only the inner body carrying its own rule and asserts by VALUE that the
  other rules still answer as this tree answers them, including the scheme guard,
  which no case removes. That is what keeps a red attributable now that four
  guards share the one `gr_testdb_component`.

  Two traps measured while adding case 5, both now in the script: a needle that
  does not match the check's own CASE (check 35 writes `CREDENTIAL`, a lowercase
  needle reported the case's own red as "for OTHER reasons" — a case that refuses
  its own red can never pass), and a `git` write landing MID-RUN (a commit made
  during case 5's selftest is reported by the `git status` leg as "something was
  left behind"; do the bookkeeping after the run).

  **Re-measured 2026-09-29 by the card that added check 34 and case 4**
  (`t_7add0cfa`, tree `7c68c986`): **10 checks passed, 0 failed, EXIT=0** — case 0
  `preflight self-test: 67 passed, 0 failed` (the 65 before the card plus check 34's
  two `ok` lines), and cases 1, 2, 3 and 4 each turned the REAL selftest RED with
  every failure naming that property (one failure for cases 1-3, two for case 4:
  the component leg and the reader leg). Case 4's sabotage restored the reported
  defect — the test-DB host hashed as the URL's raw HOST STRING — and the red named
  **the reader leg**, `the SAME server and database spelled the other way was NOT
  reused (rc=1) … testdb=localhost:5432/jomform_test`, which is the defect stated
  end to end rather than only at the component. Two files were corrected in the same
  card because the FIRST run of case 4 failed the harness's own attribution rule
  (its needle list did not name that branch) — the case was right and the verdict
  regex was too narrow. Both sources byte-identical after every case, `git status`
  unchanged. Wall time ~45 min for all five cases at load 14-44 on 6 cores, each
  case ~6-8 min because each one is a full standalone `selftest`.

- `web/scripts/biteproof_docs_index_render_sites.sh` (card `t_fc8a9ff2`, the row
  added here; **re-measured on the re-cut tree by `t_8b8d3365`** — the case count
  corrected from 8 to 9 when the `GROUPS` case the row had claimed without the
  harness driving it was actually added; **extended and re-measured again by
  `t_3b51370d`** — the four remaining legs of the one stripped `docsSrc` are now
  driven, so the count is 13): **59.9s, 13 of 13 CASES BITE, EXIT=0** (`t_3b51370d`
  run; 43.2s for the 9-case run `t_8b8d3365` measured). **The base on current
  `main` is no longer `origin/main`**: the render-site fix landed on
  main with PR #860 (`cd5027b5`), so `origin/main`'s guard IS this tree's guard and
  the seven HOLE cases read `OLD guard wanted GREEN, got RED — the case does not
  reproduce the premise` (measured: `BITEPROOF FAIL (fail=1)`), which is the
  harness being honest rather than a regression. **That same trap has since caught
  the recipe this line used to give** (card `t_858620fc`): it named `origin/main^`,
  which was pre-fix only while PR #860 was the tip — PR #862 then landed on main, so
  that base had already become `f63fa325963d`, the POST-fix reader too, and the
  recorded recipe
  reproduced the same `BITEPROOF FAIL (fail=1)` with seven `does not reproduce the
  premise` lines (re-measured on `f63fa325`, 2026-09-30). **A `^`-relative base is
  not a base at all** — it drifts with `origin/main` and silently reproduces this
  card's own defect — so this line now names the immutable sha `8ffae6457be5`
  instead. **The harness refuses a base whose reader has CAUGHT UP** instead of
  reporting that false FAIL: it reads the base guard's text for a DECLARATION of the
  reader (never a byte distance) and has three outcomes — it REFUSES a base that
  DECLARES `stripUnrendered(read(DOCS_PAGE))`, and it ALSO REFUSES a base whose
  `docsSrc` it cannot place (a reader spelled as a longer expression matches neither
  declaration, and assuming "pre-fix" for a shape it does not recognise is exactly how
  a caught-up base reads as the "before" column — the hole card `t_01df90af` closed,
  measured: a post-fix guard carrying the old `const docsSrc = read(DOCS_PAGE)` line
  verbatim in a comment classified as "pre-fix" before that card and as UNKNOWN,
  refused, after it). Each refusal prints
  `BITEPROOF REFUSED (VERDICT=REFUSED, exit 2)` and names a `BITEPROOF_BASE`
  resolved from git — the parent of the commit that first declares the post-fix
  reader, offered only after re-reading that revision from git and showing its guard
  still declares the pre-fix reader (measured: it names `8ffae6457be5`). The refusal
  is itself driven ON EVERY RUN, in ALL THREE directions: a self-check runs the
  real script against a post-fix fixture (must exit 2 with the refusal, and print a
  `BITEPROOF_BASE` suggestion), against a fixture whose reader it cannot place (must
  exit 2 naming that cause) and against a pre-fix one (must NOT refuse), so a
  deaf refusal is caught rather than looking like a fix. **Measured on this tree
  (2026-09-30), the driven cases are UNCHANGED at 13**: `BITEPROOF_BASE=8ffae645`
  (`8ffae6457be5`, the last revision whose guard still declares `const docsSrc =
  read(DOCS_PAGE)`) → **BITEPROOF PASS, EXIT=0, 13 of 13 BITES**; the same on
  `272fda17`; and the DEFAULT base → **EXIT=2, `VERDICT=REFUSED`**, zero `fail=1`.
  The self-check BITES, measured by sabotage: with `base_reader_is_pre` forced to
  answer "pre-fix" on every revision, the run refuses with `the caught-up-base
  refusal DID NOT BITE … came back exit 3, not 2`. Before that merge the
  base was `origin/main` `272fda17447c` (the parent card's `t_7b24b035` fix is
  already there, so its guard is GREEN on the unmutated tree and the premise holds:
  the baseline case reads `old guard GREEN, as expected`). The six HOLE cases read
  GREEN under the base guard — the hole — and RED, naming the leg, under this tree:
  `does not render {f.body}` (×2: a `//` line and a module-scope `/* … */`), `does
  not render the slug as an id` (×2: a `//` line and a `{/* … */}` container), and
  `every category is a named, translated group` (×2: `id={g.id}` carried by a `//`
  line, and `<T k={g.labelKey} />` carried by a `//` line — the category anchors
  and the translated category labels, added by `t_3b51370d`). All six carry
  **exactly 1 failing test** each, at 10 tests total on this tree
  (the parent's comment-anchor harness measures 9 — this guard has since grown a
  tenth). The seventh red is the `GROUPS` leg (`feature entry commented out of
  GROUPS` → RED naming `no feature was lost in the reorganisation`), also exactly
  1 failing test. All four CONTROLS (the render sites moved off the page with no
  comment at all) read RED in BOTH columns, so the check is not vacuous in either
  direction, and the NEGATIVE control (a comment naming every token beside a page
  that really renders them) stayed GREEN in both — so the strip was not
  over-eager. `src/app/docs/page.tsx` came back byte-identical (`00d832a1fca4`)
  after every one of the thirteen cases, `git status --porcelain` on it was empty
  at the end, and `docsIndex.test.ts` was never written to (sha256 unchanged
  `06d32d0d2f1d`) — this harness mutates the page only. Its verdict rule is
  `biteproof_docs_tool_claims.sh`'s `rule_of` (`t_420ca40c`), self-checked on this
  run against three real degenerate node runs. **The harness bites the fix**
  (re-driven by `t_3b51370d`): with `docsSrc` neutralised back to the raw
  `read(DOCS_PAGE)` in the guard, ALL SIX comment-carried HOLE cases read GREEN
  under the NEW column and the run reports `BITEPROOF FAIL (fail=1)` — the two
  HOLE legs this card added included, so they are load-bearing and not just green.
  The row's own inventory was corrected in the same pass: it had called the
  script's `BASELINE` a case yet excluded it from the count, said `five HOLE
  cases` against a real six, and named `CASE 1..7` + `NEGATIVE CONTROL 8` for a
  twelve-label script (measured, `grep -cE '^echo "== '` = 14 = 13 cases + the
  `GUARD RESTORED` trailer) — the same over-read the `GROUPS` fix was about.
  The mutation helpers now fail closed (`t_3b51370d`): a token no longer on the
  page sets `fail=1` with `MUTATION DID NOT APPLY … re-anchor this case` instead of
  silently skipping the case and letting the run print PASS with that case's line
  absent. Honest limit: on today's tree the premise check refuses first for that
  shape (measured: exit 2 before any case), so this is defence in depth — the
  premise only covers tokens that also redden the BASE column — and it is not
  claimed to be a reproduced false PASS.
  **Re-measured 2026-09-30 by the card that added the per-worktree lock**
  (`t_03a646e2`), on this branch (`bc5efed8` + the lock, guard sha `06d32d0d2f1d`):
  `BITEPROOF_BASE=8ffae645` → **BITEPROOF PASS, EXIT=0, 13 of 13 BITES, 92s**, page
  restored byte-identical `00d832a1fca4`, `git status` unchanged. The LOCK is
  measured the same day, and it bites: this harness's own bytes and the PRE-fix
  bytes (`origin/main`, the 518-line script with `grep -c flock` = 0) were each run
  as two overlapping runs 1s apart in ONE worktree at `8ffae645` — the fixed bytes
  printed **13 BITES and 0 `***` lines each, rc=0 for both**, the pre-fix bytes
  printed **7 and 6 BITES with 12 and 9 `***` lines, rc=1 for both** (a false
  `baseline: NEW guard wanted GREEN, got RED`, `MUTATION DID NOT APPLY`,
  `RESTORE MISMATCH`), and the contended fixed run printed the `lock: another run
  of this harness holds this worktree's lock — waiting up to 1800s` line. Removing
  the lock take and the child's held-check each reds a leg of
  `web/src/lib/harnessLock.test.ts` (measured: `must exit 75 … Got 2` and
  `expected: /was not held here/`), and the restored bytes are green again — that
  file now drives BOTH docs harnesses. The same pass also folded in the `webimage`
  blocker that file caused (card `t_10065a1e`): its guard now SKIPS on POSITIVE
  image-build evidence (no `.git` AND no checkout-only root entry — the conjunction
  `nextManagedFiles.test.ts` uses), so `docker build -f web/Dockerfile
  --output type=cacheonly .` completes (measured rc=0, `ℹ skipped 1`, the skip's
  reason printed) where a main BEFORE `c5427aed` (PR 866, the same skip landing)
  died on `spawnSync git ENOENT` — `877512ed` measured rc=1; `origin/main` @
  `c5427aed` builds rc=0; the skip's own premise is asserted against synthetic
  trees on every run, and the `flock`-absent refusal — previously gated behind
  `PATH=/usr/bin:/bin` and therefore skipped on every host — is now driven with
  a scratch PATH (measured to bite: removing the harness's refusal reds it).
- `web/scripts/biteproof_settings_tabs.sh`: **1088s, ALL CASES OK.** All four cases
  went red at their own NAMED assertion (the strip wrapping to 5 rows at 320px, the
  selected tab scrolled out of the strip's window, the focus ring clipped at the
  left edge, and the count wait never reaching the 12 tabs) and the three mutated
  files came back byte-identical (sha256 re-checked).
- `web/scripts/biteproof_mcp_tool_inventory.sh`: **exit 2, 1s** — not a pass and not
  a failure of the guard. Case 1 resolves the pre-fix page from `git merge-base HEAD
  origin/main`, and this branch is cut from current main, so that revision is
  byte-identical to the tree's own page; the harness refuses to run a case that
  would prove nothing rather than reporting green. On a branch with the page
  changed, or on the primary tree, it runs all nine.
  **Re-measured 2026-09-27 by the card that added case 7** (`t_5f8083eb`), on a branch
  cut from `08854c2c`: **all 9 mutation cases BITES, 41s, then exit 2 on case 1** —
  case 1 is now run LAST, because while it sat first its refusal called `exit 2` and
  took the whole mutation set down with it (which is how case 7 was unreachable). The
  seven pre-existing cases kept their verdicts, case 7's two legs went RED naming
  `holds 176 of the 177` and `holds 1 of the 177` respectively, and the page came back
  byte-identical (`76c5c3889823`).
- `scripts/biteproof_web_image_gate.sh`: **case 0 completed GREEN** (the healthy
  tree at HEAD passed the real `docker build` gate, so the gate is not a
  tautology), then cases 1-2 did not complete on this host: each case runs
  `./scripts/preflight.sh --only webimage`, which takes the same host-wide
  preflight lock that the merge and QA lanes were queuing on (~25 load average on 6
  cores), and the run was still waiting on it when this card had to hand off. Its
  wall time is therefore **unmeasured**, and this cell says so rather than quoting a
  number nobody produced.

**None of the three carries a wall-time figure that was not actually run on this
host**, which is the one thing this document exists to prevent.

`api/scripts/biteproof_model_id_scan.py` was re-measured by card `t_492976fe` on
this host (PR 841's head tree, load ~17 on 6 cores):

- **13/13 cases, EXIT=0, tree restored byte-identically.** The three new colon
  cases and the ten legacy ones all printed their two legs at their own named
  needle: `case 11` BRANCH RED naming the hyphenless-family probe file and the
  `mistral` family's `:7b` id with BASE GREEN, `case 12` (the `gpt-oss` exemplar,
  family spelled rather than the id, for the reason the harness's constants are)
  BRANCH RED and BASE RED at the pinned `5376463e`, the two negative controls (a
  colon followed by a SPACE, and bare family words) GREEN in both columns.
- **Its run-wide base had gone stale and now derives itself.** The row used to name
  `origin/main` as the pre-widening reader; PR 841 MERGED on 2026-09-28 22:53Z, so
  `origin/main` IS the widened reader and the first full run of this card measured
  **5 of the 10 legacy cases failing their BASE leg** (cases 1-5, each expecting
  the pre-fix reader to stay GREEN and getting a RED that named the mutation). No
  case was wrong about the guard — the instrument's premise had gone stale, the
  same failure the sibling harness records under card `t_9df01abd`. The default is
  now derived as the parent of the newest commit at `origin/main` that touched the
  guard (`eda839c1`, the pre-widening reader) and capability-checked against a
  colon-shape probe: a base whose reader has caught up is REFUSED with exit 2 and
  no verdict rather than reported as a bite count.
- **The first cut of the new probes was itself a stray mention, found by running
  it.** The harness docstring spelled two full ids, and the scan reads this `.py`
  file — so cases 10 and 14 read RED naming the harness. Fixed by assembling them
  from parts like every other id in the file, which is the rule the existing
  constants already carried.

The table carries one row more than the tree does: `biteproof_ses_replyto.py`
ships with PR 772, and its row is registered here (and in the guard's
`notYetOnMain` allowlist) so that landing it needs no doc edit. The guard consults
that allowlist only for a file that is ABSENT, and refuses an allowlisted name the
table does not carry — so it cannot be used to excuse a harness missing from both.

"Wall time here" is my own measurement on the agent host at load average 15-21 on 6
cores (2026-09-26), from a run I executed for EVERY row — none of these figures is
quoted from another document. Load matters: the same host measures one warm
incremental `go build ./...` at 35s where the gate header records 13s, so treat
these as *this host under load*, not as a hardware-independent constant. Each
harness was run against the tree as it stood on this file's branch, and each one
that completed ended with its sources restored byte-identically and `git status`
clean.

Rows go stale when the source a harness sabotages moves, which is exactly what this
file exists to make visible — and it has already happened once, **measured**: at
this file's first cut `biteproof_menu_variant_gaps.py` had 14 cases and the backup
proof had 20; by the time the branch was re-cut onto current main they had 23 and
29, because landings took their subjects (and their case lists) with them. Both
were re-run against the new tree and their rows re-derived above. The
`biteproof_menu_variation.py` row carries the other outcome of the same check: its
anchor no longer matches, so it now aborts part-way and its row says so rather than
quoting a stale pass.

Two documented figures in the repo are out of date, both caught by running the
things instead of reading about them:

- `deploy/backup/README.md` documents the backup bite proof as `~8 minutes`; the
  measured run is **35m10s** (29/29 caught). That README was not edited — it is the
  backup lane's page and its "so it is not a gate" conclusion still holds — but the
  register carries the measured number.
- `biteproof_menu_variation.py`'s own output still advertises 8 cases; on this tree
  3 of them run and it exits 2. Filed as its own card, since re-anchoring it is a
  code change to a file a different landing moved.

## Why they are not gates

The card that created this file asked for the wall time before deciding. Measured,
on this host, at load ~15 (6 cores):

| Unit of work | Measured |
|---|---|
| one warm incremental `go build ./...` | 35s |
| one `go test ./internal/config/ -count=1` (the SES harness's unit) | 1.3s |
| one named `httpapi` test, warm test binary | 7.2s |
| the same, after touching a source file in the package | 8.7s |

`deploy/backup/jomform-backup_test.sh` — the *suite*, not its bite proof — is
already a per-run gate, and its bite proof is deliberately left on demand for the
same reason: `deploy/backup/README.md` records ~8 minutes, "8 minutes on every
merge is how a gate gets skipped". The bite proofs above are the same shape and
strictly more expensive per case, because each case costs a `go test` (or `npm
run test:unit`) that has to re-run the guard from a mutated source:

- the three `api` harnesses are **3m20s – 5m** each, and they are the cheap end:
  each mutation runs one full package or a named test, and the `api test` gate they
  would sit next to is 2m35s on an idle host;
- the two `web` harnesses need an installed `web/node_modules` (the `web` gate's
  own prerequisite) and re-run `node --test`;
- the SES harness only exists once PR 772 lands, and is the cheapest at 2m02s —
  still over the cost of the gate it would duplicate.

Wiring all of them would roughly double a full gate, and the merge lane already
waits on a serialised gate lock. So: **option 2 of the card — declared manual, and
made machine-checkable** rather than silently absent. What was actually wrong was
not that they were manual; it was that nothing said so, and nothing noticed when
the set changed.

## Who re-runs them, and when

The author of a change that either (a) touches a guard named in the table above, or
(b) touches the source that guard pins — for the `api` harnesses that is
`api/internal/httpapi/menu.go`, `render_blocks.go`, `formLang.go`, `order.go`,
`verify.go`, `public.go` and `server.go` (the unlock class guard walks the served
route table `Server.routes()` there, so a route added to it moves that harness's
subject), `api/cmd/jomform-api/main.go`, `api/internal/config/config.go`,
`api/internal/llm/llm.go` and the tool registry `api/internal/mcpsrv/*.go`; for
the web ones
`web/next.config.mjs`, `web/.gitignore`, `web/tsconfig.json`,
`web/next-env.d.ts`; for the backup one the seam and failure paths in
`deploy/backup/jomform-backup.sh`.

**When:** in the same PR, before handing it to verify. Re-run the harness whose
subject you touched, and quote its summary line (every harness prints a per-case
verdict and a final restored/`BITES` line). A harness whose subject moved and was
not re-run is the exact shape this file exists to make visible — the guard still
runs on every gate, but nothing has checked that it still fails when it should.

The merge lane runs them when a PR's diff touches one of those subjects and the
handoff does not already quote a run. They are on demand, never a merge blocker on
their own: a bite proof that was not re-run is a *gap in evidence*, not a red gate.

## Adding a harness

**THE NAMING RULE, stated here because the guard enforces it (card t_e6c6d214).** A
harness is a file whose **basename contains `bite`** (case-insensitive), excluding Go
`_test.go` files and the inventory file itself. Directory names never trigger it, which is
why the guard can live in a package called `biteharness` without matching itself.

That rule is load-bearing in the strict direction: **any** tracked file whose basename
contains `bite` must have a row here, or the `api test` gate fails by name. So a plain
helper called `bite_util.py` reds the build even though it is not a harness — either give
it a row and say what it backs, or name it something that does not claim to be one. The
reverse direction is checked too: a row whose file is gone fails.

Add the file, add its row here in the same change, and name the guard it backs — the
`api test` gate refuses a `*bite*` file with no row (`api/internal/biteharness`),
and a row whose file is gone. Do not add a row for anything that is not a runnable
bite harness, and do not wire one as a gate without also correcting its row here:
`GATE-INVENTORY.md` is the list of what a green run executed, and this file is the
list of what it did not.

See also: `GATE-INVENTORY.md` (the gate table the `selftest` gate parses),
`deploy/backup/README.md` (the backup suite and its bite proof),
`api/internal/mcpsrv/mcp_parity_test.go` and `MCP-PARITY.md` (the same
gated-data-file split, for MCP tool parity).
