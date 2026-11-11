// Which Profile tab to open on arrival — the pure half (#1703 follow-up).
//
// THE DEFECT THIS RESOLVES. The RFC 8628 device-flow consent email links to
// `/profile?tab=mcp[&user_code=…]` (api/internal/httpapi/oauth_device.go). The
// page honoured `?tab=` for exactly ONE value — `connected` — and otherwise
// read `localStorage.jf_profile_tab`, defaulting to `identity`. So a first-time
// user (no stored tab yet) who clicked the email's "Approve or deny at …" link
// landed on Identity: no code field, no Approve/Deny card, because that card is
// rendered only inside `tab === "mcp"`. Measured on prod v1.37.0 with cleared
// storage: active tab "Identity", `#prof-device-code` absent; after one manual
// visit to MCP agents the same URL worked, which is why the design-time
// evidence (taken with `jf_profile_tab=mcp` already stored) never saw it.
//
// THE RULE, in precedence order:
//   1. `?linked=` present  → `connected`. That is the social-login callback
//      (issue #310): the user just linked an account and must see it. It carries
//      no `tab=` of its own, so this can only disagree if a link is hand-built;
//      the callback's landing stays the more specific intent.
//   2. `?tab=<id>` valid   → that tab, even when a stored tab says otherwise.
//      A link that names a tab is an explicit instruction from the page that
//      sent the user here (the consent email), and it must beat the tab this
//      browser happened to view last.
//   3. `?tab=` unknown     → the stored tab / `identity`, unchanged from today:
//      a typo or a stale link must still land on a tab the page actually
//      renders, which is what keeps exactly one `aria-selected` in the strip.
//   4. stored tab valid    → that tab (the page persists the last visited one).
//   5. nothing anywhere    → `identity`.
//
// This module is pure (no React, no DOM) because src/app/profile/page.tsx is a
// .tsx file the node unit runner cannot load — the same reason lib/stepUp.ts
// exists. The page MUST call this rather than re-stating the order inline; a
// second copy is how the two drift apart, and this file's test asserts the
// call site as well as the rule.

export type ProfileTab = "identity" | "security" | "passkeys" | "connected" | "mcp";

// The tab ids the page actually renders, in strip order. Keeping this list in
// one place is what makes an unknown `?tab=` fall back instead of selecting a
// tab that has no panel.
export const PROFILE_TABS: readonly ProfileTab[] = [
  "identity",
  "security",
  "passkeys",
  "connected",
  "mcp",
];

export function isProfileTab(v: string | null | undefined): v is ProfileTab {
  return typeof v === "string" && (PROFILE_TABS as readonly string[]).includes(v);
}

/**
 * Resolve the tab to open from the page's query string and the stored tab.
 *
 * `search` is `window.location.search` (`"?tab=mcp"`, or `""`), `stored` is
 * `localStorage.getItem("jf_profile_tab")` (or null when unset/unreadable).
 */
export function resolveProfileTab(search: string, stored: string | null): ProfileTab {
  let params: URLSearchParams;
  try {
    params = new URLSearchParams(search);
  } catch {
    params = new URLSearchParams("");
  }

  // 1. Social-connect callback (issue #310).
  if (params.get("linked") !== null) return "connected";

  // 2. An explicit, valid tab in the link — the device-flow email's `tab=mcp`.
  const requested = params.get("tab");
  if (isProfileTab(requested)) return requested;

  // 3. An unknown `?tab=` falls through to the stored tab, as it always has.
  // 4. The last tab this browser visited.
  if (isProfileTab(stored)) return stored;

  // 5. The landing tab.
  return "identity";
}
