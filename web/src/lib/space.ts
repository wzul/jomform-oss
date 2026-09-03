// Shared space-scope helper (issue #158). The active space is persisted in
// localStorage under "jf_space" by the Shell's SpaceSwitcher. Every dashboard
// fetch must append ?space=<slug> so the backend resolves the account from
// the selected space (each space owns its own account) — never the user's
// first account. Empty string = no space selected yet.
export function spaceScope(): string {
  if (typeof window === "undefined") return "";
  try {
    return localStorage.getItem("jf_space") || "";
  } catch {
    return "";
  }
}

// spaceQuery returns the "?space=<slug>" query string (or "") to append to a
// dashboard fetch URL.
export function spaceQuery(): string {
  const sp = spaceScope();
  return sp ? `?space=${encodeURIComponent(sp)}` : "";
}
