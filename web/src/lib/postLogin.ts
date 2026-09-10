// Issue #164: post-login space resolution. After a successful login (password,
// passkey, magic link), decide where to send the user:
//   - default space set  → sync jf_workspace to that space's slug, go to "/"
//   - >1 space, no default → go to "/space-picker" (prompt which space to enter)
//   - single space       → sync jf_workspace to it, go to "/"
//
// `next` is the validated ?next redirect (OAuth authorize) — when present we
// honour it directly (the OAuth flow already scopes to a space) and skip the
// picker entirely.
export async function resolvePostLogin(next: string): Promise<string> {
  if (next) return next;

  let spaces: { id: string; slug: string }[] = [];
  let defaultSpaceId = "";
  try {
    const [spRes, meRes] = await Promise.all([
      fetch("/api/spaces"),
      fetch("/api/auth/me"),
    ]);
    if (spRes.ok) {
      const d = await spRes.json();
      if (Array.isArray(d)) spaces = d;
    }
    if (meRes.ok) {
      const me = await meRes.json();
      defaultSpaceId = me.default_space_id || "";
    }
  } catch {
    // Network hiccup — fall through to the dashboard; the Shell will pick a
    // space from localStorage as before.
    return "/";
  }

  if (spaces.length === 0) return "/";

  // Default set → go straight to that space.
  if (defaultSpaceId) {
    const def = spaces.find((s) => s.id === defaultSpaceId);
    if (def) {
      try {
        localStorage.setItem("jf_workspace", def.slug);
      } catch {}
      return "/";
    }
  }

  // Single space → sync and go straight in (no prompt).
  if (spaces.length === 1) {
    try {
      localStorage.setItem("jf_workspace", spaces[0].slug);
    } catch {}
    return "/";
  }

  // Multi-space, no default → prompt.
  return "/space-picker";
}
