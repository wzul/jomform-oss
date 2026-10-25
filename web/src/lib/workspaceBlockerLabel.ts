// The Settings -> Workspace Danger zone's census labels (card t_088521a4).
//
// THE DEFECT. `GET /v1/workspaces/{id}/deletion-check` answers each blocker with a
// stable machine `kind` AND a human `label`, and the label is HARDCODED MALAY in
// the API (`blockerLabels` in api/internal/dbstore/workspace.go: "borang (termasuk
// yang dalam tong sampah)", "produk", "jualan", "domain tersuai", "kunci API", …).
// `WorkspaceTab.tsx` rendered `{b.label}` verbatim, so the sentence above the list
// was English — it goes through `t("ws.notEmpty")` — while every row inside it was
// Malay. Measured on prod 2026-10-16 with the UI locale `en`
// (`document.documentElement.lang` = "en"), on a workspace whose forms/products/
// sales/domains/webhooks/keys counts made every category render.
//
// THE RULE. The VISIBLE name comes from the dictionary, keyed on `kind`:
// `ws.blocker.<kind>`. That is the same source as every other string on the card,
// so it follows the five locales the user's profile selects.
//
// WHY THE KEY IS DERIVED AND NOT LOOKED UP IN A TABLE. A hand-written
// kind -> key map here would be a SECOND list of server categories, and it is
// precisely that shape which drifts: the server gains "archived_forms", the map
// has no row, and the row falls back to the Malay label on an English page again.
// Deriving the key means the ONLY list is the dictionary. The cost of deriving is
// that a missing entry is invisible to the type checker, so
// `src/lib/workspaceBlockerLabel.test.ts` pins the two sets against each other —
// parsing the Go map for the kinds the server can emit and failing in BOTH
// directions (a kind with no dictionary entry, a dictionary entry with no kind).
import { DICT } from "../components/i18nDict.ts";

/** A key of the dashboard dictionary — the same type `t()` accepts. */
export type DictKey = keyof typeof DICT;

/** Prefix every blocker-label key carries; the `kind` completes it. */
export const BLOCKER_LABEL_PREFIX = "ws.blocker.";

/**
 * The dictionary key that names a blocker `kind`, or null when the dictionary has
 * no name for it.
 *
 * NULL IS THE HONEST ANSWER, not the key itself: `t()` falls back to the raw key
 * when an entry is missing, which would print `ws.blocker.archived_forms` to the
 * user. The caller decides what to show instead (the server's own label, which is
 * the only name we have for a category this build does not know), and the guard
 * named above is what stops that fallback from being reached by a shipped kind.
 */
export function blockerLabelKey(kind: string): DictKey | null {
  if (!kind) return null;
  const key = `${BLOCKER_LABEL_PREFIX}${kind}`;
  // hasOwnProperty, not `in`: `key in DICT` is also true for inherited members
  // ("toString", "constructor"), so a bogus kind could resolve to a prototype
  // property and be handed to `t()` as a key.
  return Object.prototype.hasOwnProperty.call(DICT, key) ? (key as DictKey) : null;
}

/**
 * Every blocker-label key the dictionary carries, in dictionary order.
 *
 * Used by the guard to check the REVERSE direction (a dictionary entry whose
 * server kind is gone is a dead translation, and a rename would otherwise leave
 * one behind silently).
 */
export function blockerLabelKeys(): DictKey[] {
  return (Object.keys(DICT) as DictKey[]).filter((k) => k.startsWith(BLOCKER_LABEL_PREFIX));
}
