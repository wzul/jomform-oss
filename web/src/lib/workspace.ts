import { isReservedSlugRefusal } from "./workspaceSlug.ts";

// Shared workspace-scope helper (issue #158, renamed from space per #431).
//
// The reserved-name token reader lives in lib/workspaceSlug.ts (it also pins the
// Go constant, so the two spellings of the contract cannot drift). Imported here
// because createWorkspace below is the ONE create path both surfaces share.
// The active workspace is persisted in localStorage under "jf_workspace" by
// the Shell's WorkspaceSwitcher. Every dashboard fetch must append
// ?workspace=<slug> so the backend resolves the account from the selected
// workspace (each workspace owns its own account) — never the user's first
// account. Empty string = no workspace selected yet.
//
// Issue #450: migrate the legacy "jf_space" key (pre-#431 rename) so a user
// who logged in before the rename keeps their active workspace instead of
// falling back to an empty scope (which produced app.jomform.com URLs).
//
// Issue #6177: the machine error `code` on a failed create is decoded with the
// shared helper rather than a second copy of the code→key table: `apiErrorKey`
// is the same lookup the pages use. It is TYPE-free of React, so this module
// stays importable from `node --test`.
//
// The body is read with `res.json()` + `.catch` rather than
// `wsfetch.readErrorBody(res)`: readErrorBody consumes a `clone()`, and the
// #749 reserved-name branch below has to read the body on the same path, so one
// read serves both. `readErrorBody` was the read while this was the only
// consumer of the body; keeping it here would mean two reads of one response.
import { apiErrorKey } from "./apiErr.ts";
// TYPE-ONLY, so the Node test runner (which cannot load `.tsx`) never has to:
// TypeScript erases it, exactly as lib/apiErr.ts does for the same reason. `Key`
// must come from the one definition in components/i18n.tsx rather than being
// restated here.
import type { Key } from "../components/i18n";

function migrateLegacyScope() {
  try {
    const legacy = localStorage.getItem("jf_space");
    if (legacy && !localStorage.getItem("jf_workspace")) {
      localStorage.setItem("jf_workspace", legacy);
    }
    if (legacy) localStorage.removeItem("jf_space");
  } catch {
    // storage unavailable — ignore
  }
}

export function workspaceScope(): string {
  if (typeof window === "undefined") return "";
  try {
    migrateLegacyScope();
    return localStorage.getItem("jf_workspace") || "";
  } catch {
    return "";
  }
}

// workspaceQuery returns the "?workspace=<slug>" query string (or "") to
// append to a dashboard fetch URL.
//
// THE CONFIRMED SCOPE WINS (card t_da4b2031). Every scoped fetch in the
// dashboard builds its URL through this one function, and it used to read
// localStorage directly — so a slug left behind by a workspace that was since
// renamed or deleted went out on the FIRST request of every page, before the
// session bundle had a chance to reconcile it. The server answers that with 403
// "workspace tidak dijumpai" (a TENANCY refusal), and wsfetch.ts's 401/403 rule
// read it as a dead session and navigated to /login: measured on prod, a stale
// jf_workspace signed a merchant out of /chat.
//
// `confirmedScope` is set by SessionProvider once the server has answered with
// the workspace LIST (see setConfirmedWorkspaceScope), so once the session has
// spoken, ?workspace= can only ever carry a workspace the caller actually has.
// Before that first answer it falls back to the stored value, which is the same
// request today's build makes — so first paint is unchanged and the one refusal
// it can produce is harmless: runLoad drops the stale slug instead of logging
// the user out, and the page re-fetches when the reconciled scope publishes.
export function workspaceQuery(): string {
  const sp = confirmedScope ?? workspaceScope();
  return sp ? `?workspace=${encodeURIComponent(sp)}` : "";
}

// workspaceUrl — append the workspace scope to a URL that may ALREADY carry a query
// string (card t_4010be39).
//
// WHY THIS EXISTS. `workspaceQuery()` opens with `?` because most call sites are a bare
// path (`/api/rest/forms${workspaceQuery()}`). Two sites instead build a query of their
// own and concatenate the scope after it:
//
//     /api/rest/products?include_archived=1${workspaceQuery()}
//     /api/rest/media?limit=100${workspaceQuery()}
//
// which produces TWO `?` in one URL. The Go side reads `r.URL.Query()`, and everything
// after the second `?` is part of the FIRST parameter's value — measured against a real
// server:
//
//     RequestURI="/v1/rest/products?include_archived=1?workspace=ws-a"
//         include_archived="1?workspace=ws-a"   workspace=""   <-- scope DROPPED
//
// The scope then silently falls back to the caller's FIRST owned account, so the page
// serves a DIFFERENT workspace than the one the merchant selected, with no error and no
// wrong-looking data. That is the worst shape a tenancy bug can take.
//
// The fix is a helper rather than two `&` edits, because the failure mode is a property
// of string concatenation and will be reintroduced by the next query a call site adds.
// `sep` is what the URL actually needs: `&` when it already has a query, `?` when it
// does not, "" when there is no scope to add. `url` is whatever the caller already had
// (a path plus optional query), so this reads the same at every call site.
export function workspaceUrl(url: string): string {
  const sp = confirmedScope ?? workspaceScope();
  if (!sp) return url;
  const sep = url.includes("?") ? "&" : "?";
  return `${url}${sep}workspace=${encodeURIComponent(sp)}`;
}

// ── The CONFIRMED scope (card t_da4b2031) ────────────────────────────────────
//
// `null` means "the session has not answered yet": workspaceQuery falls back to
// the stored slug, which keeps first paint identical to the pre-fix build.
let confirmedScope: string | null = null;

// setConfirmedWorkspaceScope publishes the slug the SERVER has confirmed for
// this session (a member of the list the bundle returned, or "" when the caller
// has no workspace / the stored one is not in the list). SessionProvider calls
// it with the bundle's own list, so the value can only ever be a workspace the
// caller has.
export function setConfirmedWorkspaceScope(slug: string): void {
  confirmedScope = slug;
}

// publishWorkspaceScope is the ONE place the active scope is written: it persists
// the slug as jf_workspace (or drops the key for "") AND publishes it as the
// CONFIRMED scope every scoped fetch reads. Writing localStorage alone would
// leave workspaceQuery() serving the previous slug (t_da4b2031); publishing alone
// would let a reload resurrect a slug the server has not confirmed.
//
// It NOTIFIES the session context, but only when the value actually CHANGES — so
// a reconciler re-running with the same answer is a no-op and cannot spin. The
// notification is what makes a page re-fetch for the scope it must now use: on a
// stale slug the sequence is "refused → drop (publish "") → session answers with
// a real list → publish that slug", and without the event the page would keep
// sitting on the refusal with data from the workspace that no longer exists.
export function publishWorkspaceScope(slug: string): void {
  const changed = confirmedScope !== slug;
  try {
    if (slug) localStorage.setItem("jf_workspace", slug);
    else localStorage.removeItem("jf_workspace");
  } catch {
    // storage unavailable — the in-memory scope still applies for this page
  }
  confirmedScope = slug;
  if (changed && typeof window !== "undefined") {
    window.dispatchEvent(new Event("jf:workspace-changed"));
  }
}

// activeWorkspaceScope is the scope the dashboard is ACTUALLY using: the
// session-confirmed one once the server has answered, else the stored value. It
// is what the session context's own `workspace` state must track — reading
// localStorage directly there would leave `useSession().workspace` pointing at a
// slug the server has already rejected, so a page keyed on it would never
// re-fetch after the reconciliation (t_da4b2031).
export function activeWorkspaceScope(): string {
  return confirmedScope ?? workspaceScope();
}

// confirmedWorkspaceScope is the value published by setConfirmedWorkspaceScope
// (null until the session answers). Exposed for the guard test.
export function confirmedWorkspaceScope(): string | null {
  return confirmedScope;
}

// dropCurrentWorkspaceScope drops the stored slug because the SERVER just told
// us it is not ours. It is the authoritative form of the reconciliation: it does
// not need the workspace list, so it also works when the request that failed was
// the one fetching that list.
//
// WHY IT TAKES THE REFUSED SLUG (card t_1cfb4044). This used to take nothing and
// drop unconditionally, which is only correct while the refused slug and the
// CURRENT one are the same value. They routinely are not: a page fires its first
// scoped request on mount, the session bundle (an UNSCOPED request, so it is
// never refused) answers while that scoped request is still in flight, and the
// `[bundle]` effect in components/session.tsx reconciles the scope onto the
// workspace the caller really has. The refusal for the ORIGINAL slug then
// resolves — after the reconciliation — and the old unconditional drop wiped the
// freshly published slug, leaving `jf_workspace` unset. Nothing re-publishes it
// (the bundle is cached under the reconciled key and the effect has already
// run), so the value stays unset and every later request goes out UNSCOPED. That
// is the intermittent failure measured on this card: `/chat` with a stale slug
// polled `localStorage.getItem("jf_workspace")` and read `null` at 30s, long past
// the spec's 10s budget, so it was never a timing budget.
//
// So the drop is ROUTED: it applies only while the refused slug is still the one
// in use. By the time it is not, the refusal is stale information about a scope
// the session has already replaced, and acting on it can only destroy a value
// somebody else validated.
//
// `refused` is the slug the refused request WAS SENT WITH (see
// apiErr.workspaceScopeOf for how a body can name it). "" means the caller cannot
// attribute the refusal, and the conservative reading is "it might be the current
// one" — the pre-fix behaviour, kept so the fix cannot silently stop un-sending a
// slug that IS stale.
export function dropWorkspaceScopeIfCurrent(refused: string): void {
  if (refused === "" || refused === activeWorkspaceScope()) {
    publishWorkspaceScope(""); // notifies the session context when it changes
  }
  try {
    localStorage.removeItem("jf_space");
  } catch {
    // storage unavailable — ignore
  }
}

// dropCurrentWorkspaceScope is the unattributed form: the server refused
// whichever scope was in use, so it is dropped unconditionally. Kept for the call
// sites that hold no refusal body to attribute (see WorkspaceTab's rename path —
// there the slug being dropped is the one the user just acted on, so the
// pre-fix reading is the true one).
export function dropCurrentWorkspaceScope(): void {
  dropWorkspaceScopeIfCurrent("");
}

/** Test-only: clear the module-level confirmed scope between cases. */
export function resetConfirmedWorkspaceScope(): void {
  confirmedScope = null;
}

// ── Per-user cached scope: one sweep list, one reconciler ────────────────────
//
// THE REPORTED DEFECT
//   jf_workspace was never cleared anywhere (`grep removeItem("jf_workspace")
//   web/src` used to return zero hits) and Shell.signOut() only called
//   /api/auth/logout. So on a browser where anyone had ever selected a
//   workspace, the NEXT user's session started with the previous user's slug in
//   localStorage, and every scoped write sent it as ?workspace=<other tenant>:
//   the server correctly answered 403 "workspace tidak dijumpai" while the
//   forms list still answered 200 (that is resolveSessionAccount falling back to
//   the caller's own account for authed(), then resolveAccountScope rejecting
//   the foreign slug). A brand-new user's very first action — create a form —
//   failed with a message about a workspace they never had.
//
// THE TWO HALVES OF THE FIX, and why BOTH are needed
//   1. SIGN-OUT CLEARS THE SET. Every localStorage key the dashboard caches
//      about a signed-in user is listed in PER_USER_STORAGE_KEYS and dropped by
//      clearPerUserStorage(). Fixing only jf_workspace would leave its siblings
//      (the settings/profile tab keys) to be inherited the same way, so the
//      sweep is driven by the list — and a guard asserts the list covers every
//      jf_* key any localStorage call site names.
//   2. AN UNKNOWN SLUG IS NEVER SENT. Clearing on sign-out only covers a real
//      sign-out; a session that merely expired, or a key written by an older
//      build, still arrives with a foreign slug. clearUnknownWorkspaceScope()
//      is given the workspace list the server just returned and drops any
//      stored slug that is not in it, so ?workspace= can only ever carry a
//      workspace the caller actually has.
//
// These are the exact keys this dashboard caches per signed-in user. jf_lang is
// included deliberately: it is a per-visitor value that a signed-in user's
// PROFILE supersedes, so a value left by the previous user must not be the next
// user's first paint.
export const PER_USER_STORAGE_KEYS = [
  "jf_workspace",
  "jf_space",
  "jf_lang",
  "jf_profile_tab",
  "jf_settings_tab",
] as const;

// clearPerUserStorage drops every per-user cached key, so nothing about the
// previous user survives a sign-out on this browser.
export function clearPerUserStorage(): void {
  for (const k of PER_USER_STORAGE_KEYS) {
    try {
      localStorage.removeItem(k);
    } catch {
      // storage unavailable — nothing was cached either
    }
  }
}

// clearUnknownWorkspaceScope drops a stored slug that is not in `workspaces`.
//
// Callers pass the list the server returned. `undefined` means "no list was
// returned at all" (a failed fetch, or a payload that carries no list) and is a
// NO-OP on purpose: reconciling against a list we do not have would wipe a
// valid selection on a network blip, which is a worse bug than the one being
// fixed. An EMPTY list is a real answer — the caller has no workspace — and it
// clears the stale slug, which is the case the reported bug hit.
//
// Returns the stored slug that survives ("" when nothing is stored or the
// stored value was dropped).
export function clearUnknownWorkspaceScope(workspaces: WorkspaceRef[] | undefined): string {
  const ws = Array.isArray(workspaces) ? workspaces : undefined;
  if (!ws) return workspaceScope();
  const stored = workspaceScope();
  if (stored === "") return "";
  if (ws.some((w) => w.slug === stored)) return stored;
  // publishWorkspaceScope(""), not a bare removeItem: the CONFIRMED scope every
  // scoped fetch reads must drop the foreign slug in the same step, or
  // workspaceQuery() would keep serving it until the next bundle (t_da4b2031).
  publishWorkspaceScope("");
  return "";
}

// createWorkspaceInput is the create-workspace body (POST /v1/workspaces).
export type CreateWorkspaceInput = { name: string; slug: string };

// CreateWorkspaceErrorKey is a dictionary KEY, never a rendered string: this
// module has no locale, and the call sites (the first-run ask and the switcher)
// must not drift into two different messages for the same refusal.
//
// The type is the SHARED dictionary key union rather than a hand-listed subset,
// because the API-code path can legitimately answer any code `lib/apiErr.ts`
// translates: narrowing to a closed literal list here would silently demote a
// code the table does know (say a role refusal the create endpoint started
// returning) to "Failed to create workspace." instead of the reason. Callers
// only ever `t(key)` it, which any dictionary key satisfies.
//
// The states this path reaches today, for review:
//   apiErr.workspaceSlugTaken / apiErr.workspaceSlugInvalid  ← from the machine code
//   workspace.slugReserved                                  ← from the reserved-name token
//   workspace.slugTaken / workspace.slugInvalid             ← the status fallback
//   workspace.createFail                                    ← everything else
export type CreateWorkspaceErrorKey = Key;

// CreateWorkspaceOutcome is a discriminated result: a failure carries the i18n
// KEY the caller must render, never a rendered string.
export type CreateWorkspaceOutcome =
  | { ok: true; id: string; slug: string }
  | { ok: false; errorKey: CreateWorkspaceErrorKey };

// createWorkspace POSTs a new workspace. ONE implementation shared by the
// first-run ask (app/workspace-picker) and the topbar switcher: the failure →
// key mapping is the part that must not be written twice, because 409 (slug
// taken) and 400 (slug invalid) have distinct, actionable copy.
//
// The slug is normalised to the server's rule (lowercase a-z0-9-dash) before it
// is sent, so a client that types "My Shop" gets the same 400-vs-success
// outcome the server would return for the normalised value rather than a
// rejection the user cannot act on.
//
// Issue #6177: the server names the refusal with a machine `code` and the code
// is read FIRST. The status branch is kept as the fallback for a response that
// carries no code (an older build, a proxy) — not as the primary rule, because
// a branch on the status silently degrades to "Failed to create workspace." the
// moment the refusal moves to a different status, while the API still names the
// exact reason. The server's PROSE is never decoded here: an unknown code is
// not a fallback to it.
export async function createWorkspace(input: CreateWorkspaceInput): Promise<CreateWorkspaceOutcome> {
  const slug = input.slug.trim().toLowerCase().replace(/[^a-z0-9-]/g, "");
  try {
    const res = await fetch("/api/workspaces", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: input.name.trim(), slug }),
    });
    if (!res.ok) {
      // The reserved-name refusal is checked BEFORE the status mapping: it is a
      // 400, so the plain status order would answer `workspace.slugInvalid`
      // ("Slug must be 3-40 chars, a-z, 0-9, dash.") for a name like `billing`
      // that is perfectly well-formed. The merchant would be sent off to retype
      // the same sensitive name, and the sentence would be the API's English
      // prose rendered nowhere (#694's class of defect).
      //
      // The API carries a stable token so this decision never depends on the
      // server's prose, which is a wire message for API-key, curl and MCP
      // callers (see lib/workspaceSlug.ts, which also pins the Go constant).
      //
      // The READ is `res.json()` with a `.catch`: this is the one body read both
      // guards' stubs can execute (the #6177 guard stubs `json` only, the
      // switcher guard stubs `json`+`clone`), and it keeps the #6177 property
      // that a body which cannot be read never throws out of the submit handler
      // — it falls through to the status branch as before.
      const body = await res.json().catch(() => null);
      if (isReservedSlugRefusal(body)) return { ok: false, errorKey: "workspace.slugReserved" };
      const codeKey = apiErrorKey(body);
      if (codeKey !== "") return { ok: false, errorKey: codeKey };
      if (res.status === 409) return { ok: false, errorKey: "workspace.slugTaken" };
      if (res.status === 400) return { ok: false, errorKey: "workspace.slugInvalid" };
      return { ok: false, errorKey: "workspace.createFail" };
    }
    const d = (await res.json().catch(() => ({}))) as { id?: string; slug?: string };
    return { ok: true, id: d.id || "", slug: d.slug || slug };
  } catch {
    return { ok: false, errorKey: "workspace.createFail" };
  }
}

// enterWorkspace makes `slug` the active workspace (the same write Shell and the
// picker perform) and notifies the session context so the workspace-keyed bundle
// re-fetches for it.
//
// It also PUBLISHES the slug as the CONFIRMED scope, because every caller passes
// a slug that came from the server's own workspace list (the switcher, the
// first-run ask, the rename result, the form-settings owner probe). Without
// that, a switch would leave the module-level confirmed scope pointing at the
// previous workspace until the next bundle arrived, and the page's
// `[workspace]`-keyed fetches would go out scoped to the workspace the user just
// left (t_da4b2031).
export function enterWorkspace(slug: string): void {
  publishWorkspaceScope(slug); // notifies the session context when it changes
}

// WorkspaceRef is the minimal shape needed to resolve the ACTIVE workspace: the
// bundle's WorkspaceRow satisfies it, and so does any list of {id, slug}.
export type WorkspaceRef = { id?: string; slug?: string };

// activeWorkspaceId resolves the id of the workspace the caller currently has
// selected, from the workspace list the session bundle carries. Returns "" when
// there is nothing to resolve (no session yet, or an empty list).
//
// WHY THIS EXISTS (issue #447): several dashboard settings are per WORKSPACE,
// but their REST endpoints are keyed by workspace ID while every list fetch is
// scoped by SLUG (?workspace=<slug>). A page that only carries the slug
// therefore cannot read or write that workspace's settings at all — which is
// why the /sales sound toggle never reached the API. The resolution rule is
// shared (rather than hand-rolled per page) because it is the same rule the
// settings page uses for its own per-workspace writes, and it must never pick a
// different workspace: the stored slug wins, and only when there is no match
// (or no selection yet) does it fall back to the first workspace, which is the
// workspace the Shell selects on login.
export function activeWorkspaceId(workspaces: WorkspaceRef[]): string {
  const ws = Array.isArray(workspaces) ? workspaces : [];
  if (ws.length === 0) return "";
  const slug = workspaceScope();
  const active = ws.find((w) => w.slug === slug) || ws[0];
  return active?.id || "";
}

// The GET/PUT body type of the per-workspace realtime sound setting (issue
// #447). `sound_enabled` is the ONE wire field; the server stores it on
// workspace_settings and reports it from the same settings read the settings
// page uses.
export type SoundSettings = { sound_enabled?: boolean };

// soundEnabledFromSettings reads the stored value. The server's contract is
// "muted unless explicitly enabled": the column is NOT NULL DEFAULT FALSE, so a
// missing field means OFF, and only a literal true turns the beep on. Reading
// `!!d.sound_enabled` would be equivalent for a well-formed payload but would
// also treat a string "false" as ON, so the comparison is explicit.
export function soundEnabledFromSettings(settings: SoundSettings | null | undefined): boolean {
  return settings?.sound_enabled === true;
}

// soundToggleBody is the write payload for the toggle: EXACTLY the one field, so
// the request can never carry a stale copy of another workspace setting.
// saveWorkspaceSettings keeps every omitted field at its stored value, so a
// single-field body is safe by construction — whereas posting a whole settings
// object built from page state would clobber the fields that page does not own
// (the same hazard AGENTS.md calls out for spam_judge_enabled).
export function soundToggleBody(next: boolean): SoundSettings {
  return { sound_enabled: next };
}
