// The restaurant-menu draft the Form Settings → "Restaurant menu" tab holds
// (card t_4e92c847).
//
// THE DEFECT. The per-form menu config accepts TWO shapes for the best-seller:
// the legacy single id (`best_seller`, #438) and the list (`best_seller_ids`,
// #485). The API is explicit that the list wins when it is non-empty —
// api/internal/dbstore/form_menu.go MenuPage.BestSellerIDs / IsBestSeller /
// BestSellerProductIDs — and the MCP tool `set_form_menu` documents both.
// The tab read and wrote ONLY the legacy field, so:
//
//   read  a menu carrying best_seller_ids rendered "No best-seller selected";
//   write a Save sent `best_seller:"", best_seller_ids:null`, and
//         dbstore.WithMenu REPLACES the whole menu object
//         (`out[MenuConfigKey] = menu`), so the stored ids were erased.
//
// THE RULE. The tab hydrates the list, falling back to the legacy single id —
// the same precedence the renderer applies — and saves the list back. It never
// sends `best_seller`: writing "" over a stored id is what erased the value in
// the first place. "Clear the best sellers" is therefore an empty
// `best_seller_ids` array, and an untouched selection round-trips verbatim.
//
// Both functions are pure so the read/write shape is pinned by a lib test
// (web/src/lib/formMenu.test.ts) instead of only by the page's behaviour.

export type MenuCategoryDraft = { name: string; product_ids: string[] };

export type MenuDraft = {
  enabled: boolean;
  categories: MenuCategoryDraft[];
  /** Product IDs the menu's "Best Seller" section shows, in display order. */
  bestSellerIDs: string[];
  orderFormSlug: string;
};

/** The menu PUT body. The wire names are snake_case, matching the REST/MCP
 *  vocabulary (#485/#497) — this is the shape `putFormMenu` decodes. */
export type MenuSaveBody = {
  enabled: boolean;
  categories: MenuCategoryDraft[];
  best_seller_ids: string[];
  order_form_slug: string;
};

// A product-id list off the wire: strings only, empty and duplicate entries
// dropped. The server applies the same rules on the render path
// (MenuPage.BestSellerProductIDs), so the tab and the menu page agree on what
// "the best sellers" are.
function ids(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const out: string[] = [];
  for (const v of raw) {
    if (typeof v === "string" && v !== "" && !out.includes(v)) out.push(v);
  }
  return out;
}

function categories(raw: unknown): MenuCategoryDraft[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((c) => {
    const o = (c && typeof c === "object" ? c : {}) as {
      name?: unknown;
      product_ids?: unknown;
    };
    return {
      name: typeof o.name === "string" ? o.name : "",
      product_ids: ids(o.product_ids),
    };
  });
}

/**
 * Map the menu config the API returns (`GET /v1/rest/forms/{id}/menu`, a
 * dbstore.MenuPage) into the tab's draft state.
 *
 * `best_seller_ids` wins when it holds an id; otherwise the legacy
 * `best_seller` becomes a one-element list. This is MenuPage.BestSellerProductIDs'
 * precedence, so a menu configured through MCP (#485) hydrates its saved
 * products instead of reading as empty.
 */
export function menuDraftFromWire(raw: unknown): MenuDraft {
  const md = (raw && typeof raw === "object" ? raw : {}) as {
    enabled?: unknown;
    categories?: unknown;
    best_seller?: unknown;
    best_seller_ids?: unknown;
    order_form_slug?: unknown;
  };
  const list = ids(md.best_seller_ids);
  const legacy = typeof md.best_seller === "string" ? md.best_seller : "";
  return {
    enabled: md.enabled === true,
    categories: categories(md.categories),
    bestSellerIDs: list.length > 0 ? list : legacy ? [legacy] : [],
    orderFormSlug: typeof md.order_form_slug === "string" ? md.order_form_slug : "",
  };
}

/**
 * Build the menu PUT body from the tab's draft state.
 *
 * The best sellers travel as `best_seller_ids` and `best_seller` is NEVER sent:
 * the PUT decodes an absent key to "", and `dbstore.WithMenu` replaces the
 * stored menu object wholesale, so sending `best_seller:""` is exactly how a
 * dashboard Save used to drop an MCP-configured selection.
 */
export function menuSaveBody(draft: MenuDraft): MenuSaveBody {
  return {
    enabled: draft.enabled,
    categories: draft.categories.map((c) => ({
      name: c.name,
      product_ids: ids(c.product_ids),
    })),
    best_seller_ids: ids(draft.bestSellerIDs),
    order_form_slug: draft.orderFormSlug,
  };
}
