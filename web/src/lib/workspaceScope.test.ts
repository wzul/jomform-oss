// The stale-scope class: a NEW user on a BROWSER THAT HAS BEEN USED BEFORE must
// not inherit the previous user's workspace, and must not be dropped on a
// dashboard with no workspace at all.
//
// THE REPORTED BUG (Wan, 2026-09-20). Fresh register → try to create a form →
// "workspace not found". The sequence from prod:
//
//   login                 -> 200
//   GET  /v1/workspaces   -> 200     (the list answered fine: zero workspaces)
//   GET  /v1/rest/forms   -> 200     (resolveSessionAccount falls back to the
//                                     caller's own account, so authed() passes)
//   POST /v1/templates/use -> 403    <- the create-form attempt
//
// `grep -rn 'removeItem("jf_workspace"' web/src` returned ZERO hits: the key was
// never cleared anywhere, and Shell.signOut() only called /api/auth/logout. So
// the new user's every scoped write carried ?workspace=<the previous user's
// slug>, which resolveAccountScope correctly rejects (rest_scope.go → 403),
// while the unscoped reads kept answering 200. The user then read that 403 in the
// template picker (TemplatePicker.tsx:103) as the reason they could not create a
// form — a workspace they had never had.
//
// There were TWO independent halves, and each one alone leaves the bug reachable:
//   A. Nothing cleared the per-user cache on sign-out (the browser-level leak).
//   B. A slug that is not in the workspace list was still SENT. Shell half-fixed
//      this in memory but wrote localStorage only when the list was non-empty
//      (`if (!ws.length) return;`), so with zero workspaces — exactly the new
//      user's case — the foreign slug survived and kept going out.
//
// WHAT THIS FILE PINS, and why each is needed
//   1. EXECUTED: clearUnknownWorkspaceScope() against an EMPTY list drops a
//      stored foreign slug, and the very next workspaceQuery() therefore carries
//      no ?workspace= at all. This is the reported 403's trigger, executed.
//   2. EXECUTED: it KEEPS a slug that IS in the list, and it is a NO-OP when no
//      list was returned (a failed fetch), so a network blip cannot wipe a valid
//      selection — that would be a worse bug than the one being fixed.
//   3. EXECUTED: clearPerUserStorage() drops EVERY per-user key, and the sweep is
//      driven by PER_USER_STORAGE_KEYS — so a sibling key cannot be inherited the
//      same way while the one that bit is fixed.
//   4. SOURCE: Shell.signOut() actually CALLS it. A perfectly good clearer nobody
//      invokes is the original bug with a nicer name.
//   5. SOURCE: the resolver sends a workspace-less user to the first-run ask
//      (/workspace-picker) instead of "/", and reconciles the stored slug against
//      the list the server returned.
//   6. SOURCE: the first-run ask has a WORKING create action — the page that used
//      to render only t("workspace.empty") + Retry must carry the create form,
//      call the shared createWorkspace, and enter the workspace it just made.
//   7. COVERAGE: every jf_* key any storage call site in web/src names is either
//      swept by PER_USER_STORAGE_KEYS or is a documented non-storage key
//      (jf_session / jf_trust are COOKIE names). This is the "sweep the class,
//      not the instance" assertion: add a new per-user localStorage key anywhere
//      and this fails until it is either swept or declared.
//   8. CROSS-LANGUAGE: the create path the ask flow uses is the one the Go side
//      actually serves (POST /v1/workspaces) and the server's status → message
//      mapping is the one the client tests.
//   9. DOCUMENTATION PARITY: the ask's inline "?" link points at a docs section
//      that exists and is reachable from the docs nav.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   This repo has no JSX/DOM runner (see src/lib/i18n.test.ts), so this file does
//   not click the button or observe the rendered page: the rendered proof of the
//   two-user journey is a browser run, recorded in the card's handoff. The
//   component wiring is pinned from source, and the pure decisions are executed.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import {
  PER_USER_STORAGE_KEYS,
  clearPerUserStorage,
  clearUnknownWorkspaceScope,
  createWorkspace,
  dropCurrentWorkspaceScope,
  dropWorkspaceScopeIfCurrent,
  enterWorkspace,
  publishWorkspaceScope,
  resetConfirmedWorkspaceScope,
  setConfirmedWorkspaceScope,
  workspaceQuery,
  workspaceScope,
} from "./workspace.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";
import { pickerCopyKeys } from "./workspacePickerState.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const shell = read("../components/Shell.tsx");
const switcher = read("../components/WorkspaceSwitcher.tsx");
const picker = read("../app/workspace-picker/page.tsx");
const postLogin = read("./postLogin.ts");
const workspaceLib = read("./workspace.ts");
const docsPage = read("../app/docs/page.tsx");
const docsLayout = read("../app/docs/layout.tsx");

// The Go half of the same contract, read cross-language on purpose: the create
// path is ONE endpoint with two implementations, and a rename on either side
// makes the ask form fail without anything else noticing.
const goRestWorkspaces = read("../../../api/internal/httpapi/rest_workspaces.go");
const goRestErrors = read("../../../api/internal/httpapi/errors.go");
const goRestRoutes = read("../../../api/internal/httpapi/rest.go");
const goWorkspaceStore = read("../../../api/internal/dbstore/workspace.go");

const WEB_DIR = fileURLToPath(new URL("..", import.meta.url));
const DOCS_ANCHOR = "/docs#workspaces";

/** A minimal localStorage stand-in; workspace.ts only touches it inside functions. */
function withStore<T>(
  initial: Record<string, string>,
  fn: () => T,
): { result: T; store: Map<string, string>; events: string[] } {
  const store = new Map<string, string>(Object.entries(initial));
  const events: string[] = [];
  const g = globalThis as unknown as Record<string, unknown>;
  const prev = { window: g.window, localStorage: g.localStorage };
  // A stand-in `window`, not `globalThis`: the module guards on
  // `typeof window !== "undefined"` and then calls dispatchEvent, which Node's
  // globalThis does not have. The dispatched types are recorded so the tests can
  // assert the session context is actually notified.
  g.window = { dispatchEvent: (e: unknown) => void events.push((e as { type: string }).type) };
  g.localStorage = {
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  };
  // Every case starts with NO confirmed scope, so workspaceQuery() falls back to
  // the value this case just stored. It is module-level state, so a value left by
  // an earlier case would otherwise mask what this one asserts (t_da4b2031).
  resetConfirmedWorkspaceScope();
  try {
    return { result: fn(), store, events };
  } finally {
    resetConfirmedWorkspaceScope();
    g.window = prev.window;
    g.localStorage = prev.localStorage;
  }
}

/** Every .ts/.tsx file under web/src that is not itself a test. */
function sourceFiles(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const abs = join(dir, entry);
      if (statSync(abs).isDirectory()) {
        walk(abs);
        continue;
      }
      if (!/\.(ts|tsx)$/.test(entry)) continue;
      if (/\.test\.tsx?$/.test(entry)) continue;
      out.push(abs);
    }
  };
  walk(WEB_DIR);
  return out;
}

test("the guard is reading real files (it cannot pass vacuously)", () => {
  assert.ok(
    shell.includes("export function Shell("),
    "components/Shell.tsx is not the app shell source — the guard's anchor moved",
  );
  assert.ok(
    picker.includes("export default function WorkspacePickerPage"),
    "app/workspace-picker/page.tsx is not the picker source — the guard's anchor moved",
  );
  assert.ok(
    workspaceLib.includes("export function clearUnknownWorkspaceScope"),
    "lib/workspace.ts no longer exports clearUnknownWorkspaceScope",
  );
  assert.ok(
    existsSync(path("../app/docs/page.tsx")),
    "the docs page this guard checks does not exist",
  );
  assert.ok(sourceFiles().length > 50, `expected to scan the app's source, got ${sourceFiles().length} files`);
});

// ── 1 & 2. The EXECUTED decision: an unknown slug is never sent ──────────────

test("a foreign slug is dropped when the caller has NO workspace, and never sent as ?workspace=", () => {
  // The exact reported state: the browser holds user A's slug, and user B (who
  // has no workspace at all) is the one making the request.
  const { result, store } = withStore({ jf_workspace: "previous-tenant" }, () =>
    clearUnknownWorkspaceScope([]),
  );

  assert.equal(
    result,
    "",
    "clearUnknownWorkspaceScope kept a slug that is not in the workspace list — " +
      "that slug is what the API rejects with 403 \"workspace not found\"",
  );
  assert.equal(
    store.get("jf_workspace"),
    undefined,
    "the foreign slug is still in localStorage after reconciliation",
  );

  // And the consequence that actually broke the user: the next request carries no
  // scope at all, so the API resolves the caller's own account (200) instead of
  // rejecting a workspace they never had (403).
  const sent = withStore({ jf_workspace: "previous-tenant" }, () => {
    clearUnknownWorkspaceScope([]);
    return workspaceQuery();
  });
  assert.equal(
    sent.result,
    "",
    "a scoped request was still built with a foreign slug — this IS the reported 403",
  );
  assert.equal(sent.store.get("jf_workspace"), undefined);
});

// ── The CONFIRMED scope: the same rule, enforced one layer lower (t_da4b2031) ─
//
// THE SECOND PATH TO THE SAME 403. The reconciliation above only runs once the
// session bundle has ANSWERED. `workspaceQuery()` used to read localStorage
// directly, so every page's FIRST fetch — and the bundle's own request — carried
// the raw stored slug before that answer existed; a stale slug therefore still
// reached the server, and a 403 from it was read as a dead session. These
// EXECUTED cases pin the fix: once the session has confirmed a scope, that scope
// is what goes on the wire, and a slug the server has rejected can never be sent
// again.

test("once the session confirms a scope, THAT scope is what every fetch sends", () => {
  // The reported production shape: storage holds a slug the session cannot
  // resolve, and the bundle has now answered with the workspace the caller
  // really has. The next request must carry the REAL one.
  const { result, store } = withStore({ jf_workspace: "ws-gone" }, () => {
    setConfirmedWorkspaceScope("ws-real");
    return workspaceQuery();
  });
  assert.equal(
    result,
    "?workspace=ws-real",
    "workspaceQuery() sent the stale slug instead of the session-confirmed one — the request would be " +
      "refused with 403 again, which is the defect",
  );
  // Confirming a scope does NOT silently rewrite storage: the Shell owns that
  // write, and a reader of jf_workspace must still see what the user's browser
  // actually holds until the reconciler runs.
  assert.equal(store.get("jf_workspace"), "ws-gone");
});

test("publishWorkspaceScope moves BOTH the stored key and the confirmed scope together", () => {
  // The reconcile paths (Shell's effect, clearUnknownWorkspaceScope) publish,
  // because writing only localStorage would leave workspaceQuery serving the slug
  // they just rejected.
  const { store } = withStore({ jf_workspace: "ws-gone" }, () => publishWorkspaceScope("ws-real"));
  assert.equal(store.get("jf_workspace"), "ws-real");
  assert.equal(
    withStore({ jf_workspace: "ws-gone" }, () => {
      publishWorkspaceScope("ws-real");
      return workspaceQuery();
    }).result,
    "?workspace=ws-real",
    "publishWorkspaceScope did not reach workspaceQuery — the two halves of the scope drifted",
  );

  // Publishing "" drops the STORED key as well as the in-memory scope, so a
  // reload cannot resurrect a slug the server has not confirmed.
  const cleared = withStore({ jf_workspace: "ws-gone" }, () => publishWorkspaceScope(""));
  assert.equal(cleared.store.get("jf_workspace"), undefined);
  assert.equal(
    withStore({ jf_workspace: "ws-gone" }, () => {
      publishWorkspaceScope("");
      return workspaceQuery();
    }).result,
    "",
    "an empty publish still left a scope on the request",
  );
});

test("dropCurrentWorkspaceScope unsends the rejected slug and tells the session", () => {
  // Called by runLoad when the server refuses a `?workspace=` as not the
  // caller's. Both effects are load-bearing: the slug must stop being sent, and
  // the jf:workspace-changed event is what makes the page re-fetch for the
  // workspace the Shell then selects instead of sitting on the error.
  const { store, events } = withStore({ jf_workspace: "ws-gone" }, () => {
    dropCurrentWorkspaceScope();
  });
  assert.equal(store.get("jf_workspace"), undefined, "the rejected slug is still stored");
  assert.equal(
    withStore({ jf_workspace: "ws-gone" }, () => {
      dropCurrentWorkspaceScope();
      return workspaceQuery();
    }).result,
    "",
    "a rejected slug is still sent on the next request — the refusal would repeat forever",
  );
  assert.deepEqual(
    events,
    ["jf:workspace-changed"],
    "dropping the scope no longer notifies the session context, so the page would never re-fetch for the " +
      "workspace the Shell selects next",
  );
});

test("enterWorkspace publishes a scope that every fetch then uses, and notifies the session", () => {
  // A user-initiated switch: the slug came from the server's own list, so it is
  // confirmed by definition — and it must reach the wire immediately, not only
  // after the next bundle.
  const { store, events } = withStore({ jf_workspace: "ws-a" }, () => {
    enterWorkspace("ws-b");
  });
  assert.equal(store.get("jf_workspace"), "ws-b");
  assert.equal(
    withStore({ jf_workspace: "ws-a" }, () => {
      enterWorkspace("ws-b");
      return workspaceQuery();
    }).result,
    "?workspace=ws-b",
    "after a switch the next fetch still carried the PREVIOUS workspace's slug",
  );
  assert.deepEqual(events, ["jf:workspace-changed"]);
});

test("dropWorkspaceScopeIfCurrent does NOT drop a slug that has already been replaced (t_1cfb4044)", () => {
  // THE RACE THIS CARD CHARACTERISES. A page fires its first scoped request on
  // mount; the session bundle (UNSCOPED, so never refused) answers while that
  // request is still in flight and the `[bundle]` effect reconciles the scope
  // onto a workspace the caller really has; the refusal for the ORIGINAL slug
  // then resolves. The drop must be a no-op by then — the refused slug is not the
  // scope in use any more, and acting on it wipes the value that was just
  // published, leaving `jf_workspace` unset for good.
  //
  // Measured on this card's tree: `/chat` with a stale slug polled
  // localStorage.getItem("jf_workspace") and read null at 30s (12+ times the
  // spec's 10s budget), so this is the mechanism, not a tight timeout.
  const { store, events } = withStore({ jf_workspace: "ws-a" }, () => {
    setConfirmedWorkspaceScope("ws-a");
    dropWorkspaceScopeIfCurrent("ws-gone");
  });
  assert.equal(
    store.get("jf_workspace"),
    "ws-a",
    "a refusal for a slug that had ALREADY been replaced wiped the reconciled workspace — this is the " +
      "stale-slug race (t_1cfb4044): nothing re-publishes the value, so every later request goes unscoped",
  );
  assert.deepEqual(
    events,
    [],
    "an out-of-date refusal still notified the session context, so the page re-fetches for a scope it " +
      "did not lose",
  );
  assert.equal(
    withStore({ jf_workspace: "ws-a" }, () => {
      setConfirmedWorkspaceScope("ws-a");
      dropWorkspaceScopeIfCurrent("ws-gone");
      return workspaceQuery();
    }).result,
    "?workspace=ws-a",
    "the reconciled scope stopped reaching the wire — the page would load with no workspace at all",
  );
});

test("dropWorkspaceScopeIfCurrent still drops the slug the refusal IS about", () => {
  // The other direction, and the pre-fix behaviour it must not lose: while the
  // refused slug is still the one in use, the refusal is current information and
  // the slug must stop being sent — or the very next request repeats the 403.
  const { store, events } = withStore({ jf_workspace: "ws-gone" }, () => {
    setConfirmedWorkspaceScope("ws-gone");
    dropWorkspaceScopeIfCurrent("ws-gone");
  });
  assert.equal(store.get("jf_workspace"), undefined, "the current, refusing slug is still stored");
  assert.deepEqual(events, ["jf:workspace-changed"]);
  assert.equal(
    withStore({ jf_workspace: "ws-gone" }, () => {
      setConfirmedWorkspaceScope("ws-gone");
      dropWorkspaceScopeIfCurrent("ws-gone");
      return workspaceQuery();
    }).result,
    "",
    "a rejected slug is still sent on the next request — the refusal would repeat forever",
  );
});

test("dropWorkspaceScopeIfCurrent treats an UNATTRIBUTED refusal conservatively", () => {
  // "" is not "no scope" — it is "the caller cannot say which slug was refused",
  // and the safe reading of that is "it might be the current one". A fix that
  // treated a missing attribution as a licence to act would silently stop
  // un-sending a slug that IS stale, which is the original sign-out defect.
  const { store } = withStore({ jf_workspace: "ws-gone" }, () => {
    setConfirmedWorkspaceScope("ws-gone");
    dropWorkspaceScopeIfCurrent("");
  });
  assert.equal(
    store.get("jf_workspace"),
    undefined,
    "an unattributed refusal no longer dropped the current slug — a stale scope would keep being sent",
  );
});

test("dropCurrentWorkspaceScope stays the unconditional form", () => {
  // It is the wrapper with no slug to attribute (WorkspaceTab's rename path holds
  // no refusal body, and the slug there is the one the user just acted on), so it
  // must keep meaning exactly what it meant before.
  const { store } = withStore({ jf_workspace: "ws-a" }, () => {
    setConfirmedWorkspaceScope("ws-a");
    dropCurrentWorkspaceScope();
  });
  assert.equal(store.get("jf_workspace"), undefined);
});

test("clearUnknownWorkspaceScope empties the confirmed scope too, not only storage", () => {
  // The Shell's reconciler calls this with the server's list. Dropping only the
  // key would leave workspaceQuery() serving the foreign slug from memory, so the
  // very next fetch would reproduce the 403 the reconciliation exists to prevent.
  const { result, store } = withStore({ jf_workspace: "gone" }, () => {
    setConfirmedWorkspaceScope("gone");
    return clearUnknownWorkspaceScope([{ slug: "acme" }]);
  });
  assert.equal(result, "");
  assert.equal(store.get("jf_workspace"), undefined);
  assert.equal(
    withStore({ jf_workspace: "gone" }, () => {
      setConfirmedWorkspaceScope("gone");
      clearUnknownWorkspaceScope([{ slug: "acme" }]);
      return workspaceQuery();
    }).result,
    "",
    "the confirmed scope survived reconciliation — the stale slug would be sent again",
  );
});

// ── The class guard: the scope is BUILT in one place (t_da4b2031) ────────────
//
// `workspaceQuery()` is the one builder of `?workspace=`, and it reads the
// session-confirmed scope. A page that hand-builds the same query from
// `workspaceScope()` reaches localStorage DIRECTLY — so it sends a slug the
// server may already have rejected and re-creates the refusal this card fixes
// (`fetchDeliveries`, `retryDelivery` and four form-settings sites did exactly
// that). This census fails when a source file outside lib/workspace.ts does it
// again, in either spelling.
test("only lib/workspace.ts builds ?workspace= from the raw stored slug", () => {
  const offenders: string[] = [];
  for (const abs of sourceFiles()) {
    const rel = relative(WEB_DIR, abs);
    if (rel === "lib/workspace.ts") continue; // the module that OWNS the rule
    // String/template contents are blanked so prose in a comment or a sentence is
    // not read as code; the URLs here are template literals, so the marker is the
    // `${workspaceScope()` interpolation rather than the literal text.
    const code = readFileSync(abs, "utf8")
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .split("\n")
      .filter((l) => !/^\s*(\/\/|\*)/.test(l))
      .join("\n");
    if (/\$\{\s*workspaceScope\(\)/.test(code) || /[?&]workspace=\$\{\s*encodeURIComponent\(\s*workspaceScope\(\)/.test(code)) {
      offenders.push(rel);
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "these files build a ?workspace= scope from the RAW stored slug instead of the session-confirmed one " +
      "(workspaceQuery()/activeWorkspaceScope() in lib/workspace.ts). A raw slug can be one the server " +
      "has already rejected, so the request re-creates the 403 that used to sign the user out " +
      "(t_da4b2031): " +
      offenders.join(", "),
  );
  // And the census is looking at the real app: it must have seen the module that
  // owns the rule.
  assert.ok(
    sourceFiles().some((f) => f.endsWith(join("lib", "workspace.ts"))),
    "the walk did not include lib/workspace.ts — this guard would pass vacuously",
  );
});

test("a slug that IS in the list survives, and an absent list is a no-op", () => {
  // The valid case must not be collateral damage: this is the setting that makes
  // the dashboard show the right workspace's forms/products/sales.
  //
  // The module-level CONFIRMED scope is reset per case: workspaceQuery prefers it
  // over the stored value once the session has answered (t_da4b2031), so a value
  // left by an earlier test would mask what these cases are asserting.
  const kept = withStore({ jf_workspace: "acme" }, () => clearUnknownWorkspaceScope([{ slug: "acme" }, { slug: "beta" }]));
  assert.equal(kept.result, "acme", "a valid stored workspace was dropped — the user's selection would reset");
  assert.equal(kept.store.get("jf_workspace"), "acme");
  assert.equal(
    withStore({ jf_workspace: "acme" }, () => workspaceQuery()).result,
    "?workspace=acme",
    "the valid scope no longer reaches the request URL",
  );

  // A REMOVED workspace (renamed/deleted server-side) is also dropped, not sent.
  const gone = withStore({ jf_workspace: "gone" }, () => clearUnknownWorkspaceScope([{ slug: "acme" }]));
  assert.equal(gone.result, "", "a workspace the caller no longer has was still sent as a scope");

  // NO list at all (the fetch failed): the decision has no information, so it must
  // change nothing. Wiping a valid selection on a network blip would be worse than
  // the bug being fixed.
  const unknown = withStore({ jf_workspace: "acme" }, () => clearUnknownWorkspaceScope(undefined));
  assert.equal(unknown.result, "acme", "an absent workspace list was treated as an empty one");
  assert.equal(unknown.store.get("jf_workspace"), "acme");

  // Nothing stored, nothing to reconcile.
  assert.equal(withStore({}, () => clearUnknownWorkspaceScope([{ slug: "acme" }])).result, "");
});

// ── 3. The sweep is driven by a list, so the whole class is covered ──────────

test("clearPerUserStorage drops EVERY per-user key, not just the one that bit", () => {
  const seeded: Record<string, string> = {};
  for (const k of PER_USER_STORAGE_KEYS) seeded[k] = "set-by-the-previous-user";
  seeded["jf_unrelated_visitor_pref"] = "keep-me";

  const { store } = withStore(seeded, () => clearPerUserStorage());

  for (const k of PER_USER_STORAGE_KEYS) {
    assert.equal(
      store.get(k),
      undefined,
      `${k} survived sign-out — the next user on this browser inherits it, which is ` +
        "the reported defect for jf_workspace and the same shape for its siblings",
    );
  }
  // It clears the per-user set and nothing else: a visitor-scoped preference is
  // not another user's data.
  assert.equal(store.get("jf_unrelated_visitor_pref"), "keep-me");

  // The scope key really is in the set — otherwise the sweep would be cosmetic.
  assert.ok(
    (PER_USER_STORAGE_KEYS as readonly string[]).includes("jf_workspace"),
    "jf_workspace is not in PER_USER_STORAGE_KEYS — sign-out would stop clearing the scope",
  );
});

test("every jf_* key a storage call site names is swept or declared non-storage", () => {
  // The class guard. `jf_workspace` was the instance; the sweep list is the fix
  // for the class, and this assertion is what keeps the list honest when somebody
  // adds the next per-user key.
  const COOKIE_ONLY = new Set(["jf_session", "jf_trust"]); // HttpOnly/session cookies, never localStorage
  const swept = new Set<string>(PER_USER_STORAGE_KEYS);

  const offenders: string[] = [];
  for (const abs of sourceFiles()) {
    const src = readFileSync(abs, "utf8");
    // Only actual storage CALLS count: a comment may mention a key name.
    for (const m of src.matchAll(/localStorage\s*\.\s*(?:get|set|remove)Item\(\s*["'](jf_[a-z0-9_]+)["']/gi)) {
      const key = m[1].toLowerCase();
      if (swept.has(key) || COOKIE_ONLY.has(key)) continue;
      offenders.push(`${relative(WEB_DIR, abs)} stores ${key}`);
    }
  }

  assert.deepEqual(
    offenders,
    [],
    "a per-user localStorage key is outside PER_USER_STORAGE_KEYS — sign-out would " +
      "leave it for the next user, the same leak as jf_workspace: " +
      offenders.join("; "),
  );

  // And the scan is looking at real files: it must have seen jf_workspace, which
  // is stored by the Shell and the picker in this tree.
  const scanned = sourceFiles().filter((f) => readFileSync(f, "utf8").includes("jf_workspace"));
  assert.ok(
    scanned.length >= 4,
    `the storage scan found jf_workspace in only ${scanned.length} files — the walk is not reading the app`,
  );
});

// ── 4-6. The wiring: sign-out calls it, the resolver asks, the ask creates ───

test("Shell.signOut() CLEARS the per-user scope (a clearer nobody calls is not a fix)", () => {
  const signOut = shell.slice(shell.indexOf("async function signOut"));
  assert.ok(signOut.length > 0, "Shell.tsx no longer defines signOut");
  const body = signOut.slice(0, signOut.indexOf("window.location.href"));
  assert.match(
    body,
    /clearPerUserStorage\(\)/,
    "Shell.signOut() does not clear the per-user storage — this IS the reported bug: " +
      "jf_workspace was never cleared anywhere, so the next user inherits the previous " +
      "user's workspace slug and every scoped write 403s",
  );
  // Clearing must happen on the way OUT, before the session is torn down and the
  // page navigates — not after a redirect that may never complete.
  assert.ok(
    body.indexOf("clearPerUserStorage()") < body.indexOf(`fetch("/api/auth/logout"`),
    "the scope is cleared after the logout request — a failed/navigated-away logout would " +
      "leave the previous user's slug behind",
  );
});

test("Shell reconciles the stored slug against the list, including when it is EMPTY", () => {
  const effectSrc = shell.slice(shell.indexOf("const ws = bundle"), shell.indexOf("}, [workspaces"));
  assert.ok(effectSrc.length > 0, "could not locate the workspace-selection effect in Shell.tsx");
  // Strip comments first: the fix's own comment QUOTES the old guard verbatim
  // (`if (!ws.length) return;`), and scanning raw source let that quoted copy be
  // matched as if it were the live statement.
  const effect = effectSrc.replace(/\/\/[^\n]*/g, "").replace(/\/\*[\s\S]*?\*\//g, "");
  assert.match(
    effect,
    /clearUnknownWorkspaceScope\(ws\)/,
    "the Shell does not reconcile the stored slug against the fetched list — its own " +
      "fallback kept a foreign slug when the list was empty (`if (!ws.length) return;`)",
  );
  // The list must only be reconciled once it is an ANSWER, never while the bundle
  // is still loading. `useSession()` derives `workspaces` from the bundle, so an
  // empty array means BOTH "the server said you have none" and "the fetch has not
  // settled"; reconciling against the second erased a VALID slug on first paint
  // (role then resolved to "" and the role-gated surfaces disappeared). The
  // NO-OP branch of clearUnknownWorkspaceScope exists for exactly this and was
  // unreachable while the effect passed a list unconditionally.
  //
  // Match ANY form that feeds a list straight through — the bare
  // `Array.isArray(workspaces) ? workspaces : []` the effect used before this
  // rule, and a direct `clearUnknownWorkspaceScope(workspaces)`. Both reconcile
  // against the loading state.
  assert.doesNotMatch(
    effect,
    /clearUnknownWorkspaceScope\(workspaces\)|Array\.isArray\(workspaces\)\s*\?\s*workspaces\s*:\s*\[\s*\]/,
    "the Shell passes the workspace list to clearUnknownWorkspaceScope WITHOUT checking " +
      "whether the bundle has settled — while the bundle is still loading the list is " +
      "empty, so a valid stored slug is dropped on first paint and the role-gated " +
      "surfaces (Settings → Members, CHIP) render as a non-member",
  );
  // ...and the settled-vs-loading distinction must be driven by the bundle.
  assert.match(
    effect,
    /bundle/,
    "the effect no longer consults the session bundle, so it cannot tell \"you have no " +
      "workspaces\" from \"the list has not loaded yet\"",
  );
  // The empty list must NOT early-return before the reconciliation: that early
  // return is precisely how the foreign slug survived in the reported journey.
  // Match ANY form of the guard statement (`if (!ws.length) return;` and
  // `if (ws.length === 0) { ... }` both break the fix the same way), and take the
  // FIRST occurrence: an earlier return short-circuits before the reconcile, and
  // a brace-only match would miss the unbraced one — which is how the bite test
  // for this rule initially passed while the defect was reinstated.
  const guard = /if\s*\(\s*!\s*ws\.length\s*\)|if\s*\(\s*ws\.length\s*===\s*0\s*\)/.exec(effect);
  const reconcileAt = effect.indexOf("clearUnknownWorkspaceScope(ws)");
  assert.ok(reconcileAt !== -1, "the Shell does not call clearUnknownWorkspaceScope(ws) at all");
  assert.ok(guard, "the empty-list guard is gone from the Shell's effect — re-check this rule");
  assert.ok(
    reconcileAt < guard.index,
    "the empty-list guard runs BEFORE the reconciliation — with zero workspaces the " +
      "foreign slug survives and is sent on every scoped write (the reported 403)",
  );
});

test("the login resolver ASKS a workspace-less user to create one instead of dropping them on the dashboard", () => {
  // The signup half of the card: a brand-new user has zero workspaces, and this
  // line used to return "/", which is a dashboard with no workspace and no
  // prompt — the first thing they tried then failed.
  assert.match(
    postLogin,
    /if \(workspaces\.length === 0\) return "\/workspace-picker";/,
    'resolvePostLogin still sends a workspace-less user to "/" — they are never asked ' +
      "to create a workspace, which is the signup half of the defect",
  );
  // And the resolver also reconciles the scope it is about to rely on.
  assert.match(
    postLogin,
    /clearUnknownWorkspaceScope\(workspaces\)/,
    "the resolver does not reconcile the stored slug — the first page load after login " +
      "could still carry the previous user's scope",
  );
  // The reconcile must sit AFTER the authoritative list arrived and BEFORE the
  // default-workspace write, so it can never drop the slug it is about to set.
  //
  // The write is `enterWorkspace(slug)` now (t_da4b2031): the resolver runs
  // before the first dashboard page mounts, so it has to publish the CONFIRMED
  // scope every scoped fetch reads, not only the stored key — a raw setItem left
  // the page sending the previous slug.
  const reconcileAt = postLogin.indexOf("clearUnknownWorkspaceScope(workspaces)");
  const writeAt = postLogin.indexOf("enterWorkspace(def.slug)");
  assert.ok(
    writeAt !== -1,
    "the login resolver no longer publishes the default workspace via enterWorkspace — a raw " +
      "localStorage write leaves the CONFIRMED scope (what every scoped fetch reads) pointing at the " +
      "previous workspace (t_da4b2031)",
  );
  assert.ok(
    reconcileAt < writeAt,
    "the stored slug is written before it is reconciled — the reconciliation could undo it",
  );
});

test("the first-run ask carries a WORKING create action and enters the new workspace", () => {
  // The page with zero workspaces used to be t("workspace.empty") + Retry and
  // nothing else: no create action at all, which is why the only way in was the
  // hidden "+ New workspace" inside the topbar switcher.
  assert.match(
    picker,
    /onSubmit=\{askFirstCreate\}/,
    "the workspace picker's empty state has no create form — a workspace-less user " +
      "cannot get past it (the dead end in the reported journey)",
  );
  assert.match(
    picker,
    /const r = await createWorkspace\(\{ name: newName, slug: newSlug \}\)/,
    "the ask does not use the shared createWorkspace helper — its status→message " +
      "handling would drift from the switcher's",
  );
  assert.match(
    picker,
    /if \(!r\.ok\) \{[\s\S]{0,700}?setCreateError\(t\(r\.errorKey\)\)/,
    "the ask renders something other than the dictionary key — a server-side Malay " +
      "string would be shown to an English user",
  );
  // A successful create must LAND the user in the new workspace, or they are still
  // scoped to nothing and the next create surface fails the same way.
  const fn = picker.slice(picker.indexOf("async function askFirstCreate"));
  assert.match(
    fn,
    /enterWorkspace\(r\.slug\)/,
    "the ask does not enter the workspace it just created — the user would still have no " +
      "active scope and the next create would 403",
  );
  // And the switcher must share the same helper rather than POST its own copy.
  assert.match(
    switcher,
    /const r = await createWorkspace\(\{ name, slug \}\)/,
    "the topbar switcher posts /api/workspaces itself again — two create paths will drift",
  );
});

// ── 8. Cross-language: one endpoint, one status mapping ─────────────────────

test("the ask's create path is the endpoint the Go side serves, with the same status mapping", () => {
  assert.ok(
    workspaceLib.includes('fetch("/api/workspaces"'),
    "lib/workspace.createWorkspace no longer POSTs /api/workspaces",
  );
  // The proxy maps /api/* onto /v1/*, so the route must still be registered.
  assert.ok(
    goRestRoutes.includes('mux.Handle("POST /v1/workspaces"'),
    "POST /v1/workspaces is not registered — the ask form would 404 and the user could " +
      "never get a workspace",
  );
  // The statuses the client maps are the statuses the handler writes.
  assert.ok(
    goRestWorkspaces.includes('writeErr(w, http.StatusConflict, "slug already in use")'),
    "the handler no longer answers 409 for a taken slug — the client's slugTaken branch is dead",
  );
  // The handler still answers 400 for a bad slug — written from the package's
  // own literal (workspaceSlugLengthMessage) rather than inline, because the
  // message→code table in errcode.go keys on the exact string and an inline
  // duplicate is how the wire wording and the table drift apart. The client's
  // slugInvalid branch is live as long as the LITERAL is what reaches the wire,
  // which is what this checks.
  assert.ok(
    goRestWorkspaces.includes('writeErr(w, http.StatusBadRequest, workspaceSlugLengthMessage)'),
    "the handler no longer answers 400 with the slug-length message — the client's slugInvalid " +
      "branch is dead",
  );
  // The literal itself is declared in errors.go (that file owns the wire
  // wording), and errcode.go's message→code table keys on its EXACT text. Pin the
  // declaration, or a re-word would silently drop the code and the dashboard
  // would fall back to its generic 400 sentence (#6177).
  assert.ok(
    goRestErrors.includes('workspaceSlugLengthMessage = "slug 3-40 characters"'),
    "the slug-length literal moved or changed — the message→code table in errcode.go keys on this " +
      "exact string, so a rename silently drops the code and the dashboard falls back to its " +
      "generic 400 sentence",
  );
  assert.match(
    workspaceLib,
    /res\.status === 409[\s\S]{0,120}?workspace\.slugTaken/,
    "the client stopped mapping 409 to the slugTaken message",
  );
  assert.match(
    workspaceLib,
    /res\.status === 400[\s\S]{0,120}?workspace\.slugInvalid/,
    "the client stopped mapping 400 to the slugInvalid message",
  );
});

// ── The EXECUTED create call, against a fake server ─────────────────────────

test("createWorkspace maps each real response to the right key, and normalises the slug", async () => {
  type Sent = { url: string; method: string; body: { name: string; slug: string } };
  const calls: Sent[] = [];
  const originalFetch = globalThis.fetch;
  const stub = (status: number, body: unknown = {}) =>
    (async (url: string, init?: { method?: string; body?: string }) => {
      calls.push({ url, method: init?.method || "GET", body: JSON.parse(init?.body || "{}") });
      return { ok: status >= 200 && status < 300, status, json: async () => body };
    }) as unknown as typeof fetch;
  try {
    globalThis.fetch = stub(201, { id: "ws-new", slug: "my-shop" });
    const created = await createWorkspace({ name: "  My Shop  ", slug: "My Shop" });
    assert.deepEqual(created, { ok: true, id: "ws-new", slug: "my-shop" });
    assert.deepEqual(
      calls.at(-1),
      { url: "/api/workspaces", method: "POST", body: { name: "My Shop", slug: "myshop" } },
      "the create request is not a POST to /api/workspaces with a normalised slug",
    );
    // The switcher's input normalises as the user types with the SAME character
    // class, so the value shown and the value sent can never disagree (a plain
    // substring check — no regex escaping games).
    assert.ok(
      switcher.includes('setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))'),
      "the switcher's slug input no longer normalises with the same class as " +
        "createWorkspace — the value shown to the user and the value sent would disagree",
    );
    assert.ok(
      picker.includes('setNewSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))'),
      "the first-run ask's slug input no longer normalises like the switcher's",
    );

    globalThis.fetch = stub(409);
    assert.deepEqual(await createWorkspace({ name: "x", slug: "taken" }), {
      ok: false,
      errorKey: "workspace.slugTaken",
    });

    globalThis.fetch = stub(400);
    assert.deepEqual(await createWorkspace({ name: "x", slug: "ab" }), {
      ok: false,
      errorKey: "workspace.slugInvalid",
    });

    globalThis.fetch = stub(500);
    assert.deepEqual(await createWorkspace({ name: "x", slug: "ok-slug" }), {
      ok: false,
      errorKey: "workspace.createFail",
    });

    // A network failure must produce the generic key, not throw out of a submit
    // handler and leave the button spinning.
    globalThis.fetch = (async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch;
    assert.deepEqual(await createWorkspace({ name: "x", slug: "ok-slug" }), {
      ok: false,
      errorKey: "workspace.createFail",
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

// ── 9. i18n + docs parity ───────────────────────────────────────────────────

test("the ask's and the picker's strings exist in all five locales and are actually rendered", () => {
  const keys = [
    "workspace.askPageTitle",
    "workspace.askPageSubtitle",
    "workspace.askTitle",
    "workspace.askBody",
    "workspace.askHost",
    "workspace.docs",
    // The picker's list subtitle is selected by the ROW COUNT, so all three
    // sentences must exist in every locale: a missing one renders the raw key
    // above a list of real workspaces.
    "workspace.pickerSubtitle",
    "workspace.pickerSingleSubtitle",
    "workspace.pickerNeutralSubtitle",
  ] as const;
  for (const key of keys) {
    const entry = DICT[key] as Record<string, unknown> | undefined;
    assert.ok(entry, `the dictionary has no ${key} — the ask would render a raw key`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — a user on that locale would silently see English`,
      );
    }
  }
  for (const key of ["workspace.askTitle", "workspace.askBody"] as const) {
    assert.ok(picker.includes(`t("${key}")`), `app/workspace-picker/page.tsx does not render t("${key}")`);
  }
  // The heading/subtitle keys are no longer literal at the call site (card
  // t_6c0badbe): the page renders `t(copy.title)` / `t(copy.subtitle)` from
  // pickerCopyKeys, so the copy is selected from the SAME mode as the body. What
  // must still hold is that the ask-first mode resolves to these two keys and the
  // page renders them.
  assert.deepEqual(pickerCopyKeys("ask", 0), {
    title: "workspace.askPageTitle",
    subtitle: "workspace.askPageSubtitle",
  });
  assert.ok(picker.includes("t(copy.title as Key)"), "the ask heading is not rendered from pickerCopyKeys(mode)");
  assert.ok(picker.includes("t(copy.subtitle as Key)"), "the ask subtitle is not rendered from pickerCopyKeys(mode)");
  // The page must hand the ROW COUNT to the copy selector. Measured defect: a
  // user with exactly one workspace read "You have multiple workspaces. Pick
  // which one to enter." above the single row.
  assert.ok(
    picker.includes("pickerCopyKeys(mode, workspaces.length)"),
    "the picker page selects its subtitle without the row count — one workspace would " +
      "again be told it has multiple",
  );
  // The branch itself, executed: one row is its own sentence, two rows keeps
  // the "multiple" wording the card requires, and an unread list claims neither.
  assert.equal(
    pickerCopyKeys("list", 1).subtitle,
    "workspace.pickerSingleSubtitle",
    "a one-workspace list still renders the multiple-workspaces sentence",
  );
  assert.equal(pickerCopyKeys("list", 2).subtitle, "workspace.pickerSubtitle");
  assert.equal(pickerCopyKeys("loading", 0).subtitle, "workspace.pickerNeutralSubtitle");
  assert.equal(pickerCopyKeys("loadError", 0).subtitle, "workspace.pickerNeutralSubtitle");
  // And the English sentences say different things, so the swap above is real
  // copy and not two keys pointing at one string.
  const en = (k: "workspace.pickerSingleSubtitle" | "workspace.pickerSubtitle") =>
    (DICT[k] as Record<string, string>).en;
  assert.notEqual(en("workspace.pickerSingleSubtitle"), en("workspace.pickerSubtitle"));
  assert.ok(
    !/multiple/i.test(en("workspace.pickerSingleSubtitle")),
    "the one-workspace subtitle still claims multiple workspaces",
  );
  // The host hint carries the address placeholder in every locale, not just en.
  // Card t_48a93084: it is {host}, not {slug}, because the address a workspace
  // is published at depends on the DEPLOYMENT (lib/deployment.ts) — a
  // self-hosted install has no per-workspace host to substitute a slug into.
  const host = DICT["workspace.askHost"] as Record<string, string>;
  for (const lang of LANGS) {
    assert.ok(host[lang].includes("{host}"), `workspace.askHost.${lang} dropped the {host} placeholder`);
    assert.ok(
      !host[lang].includes("jomform.com"),
      `workspace.askHost.${lang} still hardcodes a hosted address — a self-hosted install would print a host it does not have`,
    );
  }
});

test('the ask\'s inline "?" link points at a docs section that exists and is reachable', () => {
  assert.ok(
    picker.includes(`href="${DOCS_ANCHOR}"`),
    `the first-run ask has no inline "?" doc link (${DOCS_ANCHOR}) — AGENTS.md ` +
      "documentation parity requires one next to the feature",
  );
  assert.match(
    docsPage,
    /slug: "workspaces"/,
    `the docs link points at #workspaces but the docs index declares no section with that slug`,
  );
  assert.ok(
    docsPage.includes("id={f.slug}"),
    "the docs page does not render the section ids into the DOM, so the anchor would not scroll",
  );
  assert.ok(
    docsLayout.includes(DOCS_ANCHOR),
    `the docs nav does not link ${DOCS_ANCHOR} — the page would be reachable only by typing the URL`,
  );
  // The section must state the behaviour the defect contradicted: a workspace-less
  // user is ASKED to create one.
  assert.match(
    docsPage,
    /asks you to create one/i,
    "the docs page does not say a workspace-less user is asked to create a workspace",
  );
});

// ── The Go half: one account, not two ──────────────────────────────────────

test("the first-workspace create reuses the signup account instead of minting a second one", () => {
  // Workspaces.Create inserts an account of its own (issue #158), so an ask-first
  // create that used it would leave every new user owning TWO accounts — one of
  // them unreachable, with workspace_id NULL. The store must offer the reuse path
  // and the handler must take it.
  assert.match(
    goWorkspaceStore,
    /func \(s \*WorkspaceStore\) SoleEmptyUnscopedAccount\(/,
    "dbstore no longer offers the sole-empty-unscoped-account lookup — the create path " +
      "has no way to reuse the signup account",
  );
  assert.match(
    goWorkspaceStore,
    /func \(s \*WorkspaceStore\) CreateForAccount\(/,
    "dbstore no longer offers CreateForAccount — the handler cannot link an existing account",
  );
  assert.match(
    goRestWorkspaces,
    /if prior, _ := h\.Workspaces\.ByMember\(r\.Context\(\), userID\); len\(prior\) == 0 \{[\s\S]{0,900}?CreateForAccount\(/,
    "createWorkspace does not reuse the signup account when this is the caller's FIRST " +
      "workspace — the user ends up with two accounts (the reported no-double-account check)",
  );
  // The reuse must be bounded: an account that already holds CONTENT belongs to
  // the legacy data-migration case (its published forms resolve through the
  // account slug), not to a side effect of pressing "create workspace".
  //
  // api_keys is deliberately NOT in this list. A key is a credential, not
  // content: it carries no public host slug and no published URL, and
  // CreateForAccount syncs api_keys.workspace_id with the account. Excluding
  // key holders made the reuse unreachable on /mcp — every MCP caller
  // authenticates through the api_keys table — so create_workspace left the
  // user owning two accounts with their own agent credential stranded on the
  // unreachable one. Asserting the clause back would re-open that defect.
  for (const table of ["forms", "products", "sales"]) {
    assert.match(
      goWorkspaceStore,
      new RegExp(`NOT EXISTS \\(SELECT 1 FROM ${table}\\b`),
      `SoleEmptyUnscopedAccount no longer excludes accounts holding ${table} rows — a ` +
        "legacy workspace-less account would be silently relinked",
    );
  }
  assert.doesNotMatch(
    goWorkspaceStore,
    /NOT EXISTS \(SELECT 1 FROM api_keys\b/,
    "SoleEmptyUnscopedAccount excludes accounts holding an api_keys row again — every " +
      "/mcp caller holds one (auth resolves the Bearer through that table), so the reuse " +
      "branch becomes unreachable on the MCP transport and create_workspace leaves a fresh " +
      "user owning two accounts, with their agent's credential stranded on the unreachable one",
  );
  // The store guard alone is not enough to pin the behaviour: a comment cannot fail
  // a test, so the executed half lives next to it as
  // TestSoleEmptyUnscopedAccountKeepsTheCallersKey (dbstore) and
  // TestRESTCreateWorkspaceReusesAKeyHoldingSignupAccount (httpapi).
  // Linking must be owner-checked: never link somebody else's account.
  assert.match(
    goWorkspaceStore,
    /UPDATE accounts SET workspace_id=\$1, updated_at=now\(\)[\s\S]{0,200}?owner_user_id=\$3/,
    "CreateForAccount's link is not owner-checked — it could link another user's account",
  );
  // And the dependent rows must be synced explicitly (000045's trigger only fires on
  // INSERT / UPDATE OF account_id on forms).
  for (const table of ["forms", "products", "sales", "api_keys"]) {
    assert.match(
      goWorkspaceStore,
      new RegExp(`UPDATE ${table} SET workspace_id=\\$1 WHERE account_id=\\$2 AND workspace_id IS NULL`),
      `CreateForAccount does not sync ${table}.workspace_id — a relinked account's rows would ` +
        "keep a NULL scope (trg_forms_sync_workspace_id does not fire on an account update)",
    );
  }
});
