"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { PageLoading } from "../../components/PageLoading";
import { Shell } from "../../components/Shell";
import { MobileRecord, MobileRecordList } from "../../components/MobileRecord";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate } from "../../lib/formatDate";
import { currencyLabel, formatPrice } from "../../lib/money";
import { workspaceQuery, workspaceUrl } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { apiErr, translateApiErr } from "../../lib/apiErr";
import { panelDomId, tabDomId, tabStop, tablistKeyDown } from "../../lib/tablist";
import { matchesTypedValue } from "../../lib/typedConfirm";
import { customAmountBody, limitsToInputs } from "../../lib/productAmounts";

type Product = {
  id: string;
  name: string;
  description?: string;
  price_sen: number;
  images?: string[];
  type: string;
  billing_period?: string;
  // round 13c: from_payment | catch_up | skip_missed
  schedule_mode?: string;
  trial_days?: number;
  subscription_charges?: number | null; // null = infinite
  stock?: number | null;
  // Card t_06851d88: pay-what-you-want (custom_amount) limits, integer sen.
  // min is 0 for every other type; max null = no cap; suggested null = the
  // buyer's field starts empty. `price_sen` of such a product is a DISPLAY
  // value derived from these, never the amount charged.
  min_price_sen?: number;
  max_price_sen?: number | null;
  suggested_price_sen?: number | null;
  active: boolean;
  created_at?: string;
};

type SortKey = "name" | "price_sen" | "created_at";
type SortDir = "asc" | "desc";

/** A sortable column header for the product list.
 *
 *  MODULE SCOPE, DELIBERATELY (react-hooks/static-components). This was declared
 *  inside the page component, so every render produced a NEW component type and
 *  React unmounted/remounted each header cell (three of them) on every state
 *  change — re-creating DOM nodes for a `<th>` that never needed to move. The
 *  rule reports exactly that; deriving `active` from props is the fix it asks for.
 *
 *  IT RENDERS THE `<th>` ITSELF, and that is load-bearing (card t_3fbc6953).
 *  The header cell used to be the caller's `<th>` with this component's
 *  `<button>` inside it, and the sort STATE was written on the button:
 *
 *      <th><button aria-sort="ascending">Name</button></th>
 *
 *  `aria-sort` is supported by the roles `columnheader` and `rowheader` ONLY —
 *  a `<button>` supports neither — so axe-core 4.13.0 reported three
 *  `aria-allowed-attr` CRITICAL nodes on the live `/products`
 *  ("aria-sort is not supported by the role button"), and the attribute was
 *  invisible to assistive technology as a sort state. Because the `<th>` was
 *  rendered by the caller, moving the attribute could only be done by moving
 *  the ownership of the cell — so this component owns it now, and the state,
 *  the icon and the button all live together inside it:
 *
 *      <th aria-sort="ascending"><button>Name <span/></button></th>
 *
 *  Reference: the W3C ARIA APG sortable-table pattern
 *  https://www.w3.org/WAI/ARIA/apg/patterns/table/examples/sortable-table/
 *  and https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Attributes/aria-sort
 *  Both put `aria-sort` on the `<th>` and the `<button>` inside it, and both
 *  scope it to the CURRENTLY sorted column — which is why the inactive columns
 *  omit the attribute instead of carrying `aria-sort="none"` (they had `"none"`
 *  before, on the button, which was wrong twice over).
 *
 *  The direction glyph is `aria-hidden` so it stays out of the button's
 *  accessible name (`"Price (RM)"`, not `"Price (RM) ▼"`), as the APG example
 *  does. */
function SortHeader({
  label,
  k,
  sortKey,
  sortDir,
  onToggle,
  className,
}: {
  label: string;
  k: SortKey;
  sortKey: SortKey;
  sortDir: SortDir;
  onToggle: (k: SortKey) => void;
  className: string;
}) {
  const active = sortKey === k;
  return (
    <th className={className} aria-sort={active ? (sortDir === "asc" ? "ascending" : "descending") : undefined}>
      <button
        onClick={() => onToggle(k)}
        className="inline-flex items-center gap-1 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] hover:text-[var(--jf-primary)]"
      >
        {label}
        <span className="text-jf-3xs" aria-hidden="true">{active ? (sortDir === "asc" ? "▲" : "▼") : "⇅"}</span>
      </button>
    </th>
  );
}

// A row in the Trash view (GET /api/rest/products/trash). `can_purge` is false
// when the product has a sale or a subscription — those are the two obstacles
// the server refuses on, so the button is withheld rather than failing on click.
type TrashedProduct = {
  id: string;
  name: string;
  price_sen: number;
  active: boolean;
  deleted_at: string;
  sales_count: number;
  subscription_count: number;
  can_purge: boolean;
};

const PER_PAGE = 10;

export default function Products() {
  const { t } = useI18n();
  const { anon, bundle, role, workspace, user, currency } = useSession();
  const router = useRouter();

  // Card t_d5f359c7: a product's price_sen has no currency of its own — it is
  // charged in the WORKSPACE's currency (#185), so that is the label. "—" until
  // the session bundle resolves; never the old hardcoded RM.
  const money = (sen: number) => formatPrice(sen, currency);
  // The price FIELD's own label names the same money ("Price (RM)" → "Price (US$)").
  const priceLabel = t("prod.price", { cur: currencyLabel(currency) });
  // Card t_7eb3b623: the pay-what-you-want limits, labelled in the same money.
  const minLabel = t("prod.minAmount", { cur: currencyLabel(currency) });
  const maxLabel = t("prod.maxAmount", { cur: currencyLabel(currency) });
  const sugLabel = t("prod.suggestedAmount", { cur: currencyLabel(currency) });

  // Issue #163: read_only members see no write actions (add/edit/archive).
  // manage + admin keep full write access.
  const canWrite = role !== "read_only";

  const [products, setProducts] = useState<Product[]>([]);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // search + filters
  const [search, setSearch] = useState("");
  const [typeFilter, setTypeFilter] = useState("all");
  const [stockFilter, setStockFilter] = useState("all");

  // sorting
  const [sortKey, setSortKey] = useState<SortKey>("created_at");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  // pagination
  const [page, setPage] = useState(1);

  // selection
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  // Trash view: a second, explicitly-selected view over the SAME page. The
  // default is the catalogue — a new visitor must never land in the trash.
  const [view, setView] = useState<"active" | "trash">("active");
  const [trashed, setTrashed] = useState<TrashedProduct[]>([]);
  const [trashLoaded, setTrashLoaded] = useState(false);
  const [notify, setNotify] = useState("");
  // The TRASH view's OWN refusal channel (card t_602c48f5 — the third surface of
  // the defect t_ab4aa85b fixed on /forms and in the Danger zone):
  //
  //   * Every refusal branch used to write its sentence to `error` and then fire
  //     `loadTrash()`, which clears `error` on a successful reload — so the
  //     sentence was SET and ERASED in the same tick and the merchant never read
  //     why their action was refused. Measured on the built app in a real browser
  //     with a 409 on the restore route: `p[role="alert"]` sampled every 100ms
  //     thirty times was absent at EVERY sample, while the trash listing went from
  //     one call to two (the wipe is the same-tick relist, not a missing string).
  //     `loadTrash()` does not touch this state, so the refetch that REPORTS the
  //     refusal can no longer wipe it.
  //   * `error` is also this page's LOAD-failure line: it carries a Retry that
  //     reloads the listing. A refusal changes nothing about the listing, so the
  //     sentence belongs beside the rows it is about, without a Retry.
  //
  // Cleared at the START of the next action that re-issues it (both handlers open
  // with it) and by the Active/Trash tab switch. THE RULE: a refusal stays
  // readable until the user's next action — never erased by the refetch that
  // reports it.
  const [trashErr, setTrashErr] = useState("");
  const [confirmPurge, setConfirmPurge] = useState<TrashedProduct | null>(null);
  const [purgeTyped, setPurgeTyped] = useState("");
  const [purgeBusy, setPurgeBusy] = useState(false);

  // edit drawer
  const [editing, setEditing] = useState<Product | null>(null);
  const [drawerBusy, setDrawerBusy] = useState(false);
  const [drawerError, setDrawerError] = useState("");

  const load = useCallback(async () => {
    // Issue #29: no serial `await me → fetch` gate — the list fetch runs
    // immediately; a 401 flips `anon` and the auth bounce below handles it.
    // include_archived=1 so the WooCommerce-style admin list shows a status
    // column and lets owners toggle products back to active.
    // Issue #503: a non-ok response used to become an EMPTY list with no error
    // (`r.ok ? r.json() : []`). Route it through the shared helper: 401/403 →
    // /login, anything else → a visible message. Never empty-as-error.
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(workspaceUrl("/api/rest/products?include_archived=1"));
        return Array.isArray(d) ? (d as Product[]) : [];
      },
      { onUnauth: goToLogin, message: t("prod.loadFailList"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setProducts([]);
      setError(r.error);
      setChecking(false);
      return;
    }
    setProducts(r.data);
    setError("");
    setChecking(false);
  }, [t, goToLogin]);

  useEffect(() => {
    // Always fetch the authoritative list (include_archived=1) so archived
    // products show for the status column/toggle. The bundle cache can
    // optimistically paint active products first, but must not suppress the
    // reload that surfaces archived ones.
    if (bundle && Array.isArray(bundle.products) && products.length === 0 && checking) {
      setProducts(bundle.products as Product[]);
    }
    load();
    // Issue #170: `workspace` re-runs load() when the active workspace changes
    // so the list re-fetches for the new workspace without a manual refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bundle, workspace]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // Reset pagination + selection when filters change.
  useEffect(() => {
    setPage(1);
    setSelected(new Set());
  }, [search, typeFilter, stockFilter]);

  // ── filtering + sorting + pagination (client-side for now) ──────────────
  const filtered = useMemo(() => {
    let rows = products;
    if (search.trim()) {
      const q = search.trim().toLowerCase();
      rows = rows.filter((p) => p.name.toLowerCase().includes(q));
    }
    if (typeFilter !== "all") {
      rows = rows.filter((p) => p.type === typeFilter);
    }
    if (stockFilter === "in") {
      rows = rows.filter((p) => typeof p.stock === "number" && p.stock > 0);
    } else if (stockFilter === "out") {
      rows = rows.filter(
        (p) => typeof p.stock === "number" && p.stock === 0
      );
    }
    const dir = sortDir === "asc" ? 1 : -1;
    return [...rows].sort((a, b) => {
      switch (sortKey) {
        case "name":
          return a.name.localeCompare(b.name) * dir;
        case "price_sen":
          return (a.price_sen - b.price_sen) * dir;
        default: {
          const ta = a.created_at || "";
          const tb = b.created_at || "";
          return (ta < tb ? -1 : ta > tb ? 1 : 0) * dir;
        }
      }
    });
  }, [products, search, typeFilter, stockFilter, sortKey, sortDir]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PER_PAGE));
  const safePage = Math.min(page, totalPages);
  const pageRows = useMemo(
    () => filtered.slice((safePage - 1) * PER_PAGE, safePage * PER_PAGE),
    [filtered, safePage]
  );

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir(key === "name" ? "asc" : "desc");
    }
  }

  // ── selection ────────────────────────────────────────────────────────────
  const ids = Array.from(selected);
  const allOnPageSelected =
    pageRows.length > 0 && pageRows.every((p) => selected.has(p.id));

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allOnPageSelected) {
        for (const p of pageRows) next.delete(p.id);
      } else {
        for (const p of pageRows) next.add(p.id);
      }
      return next;
    });
  }

  async function bulkArchive() {
    if (ids.length === 0 || busy) return;
    const ok = window.confirm(t("prod.bulkArchiveConfirm", { n: ids.length }));
    if (!ok) return;
    setBusy(true);
    try {
      await fetch(`/api/rest/products/bulk${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "archive", ids }),
      });
    } finally {
      setBusy(false);
      setSelected(new Set());
      load();
    }
  }

  // ── create ───────────────────────────────────────────────────────────────
  // Card t_bb0d0bee: ONE creation entry point. The quick-create panel below is
  // COLLAPSED by default and the header's "New product" button is what reveals
  // it. Before this, the panel was always open while a second "New product"
  // LINK to /products/new sat above it — two ways to start the same task on one
  // screen, and on a 390x844 phone the always-open panel pushed the first
  // catalogue row to y=781 (measured), i.e. the list began below the fold.
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [price, setPrice] = useState("");
  // Card t_7eb3b623: pay-what-you-want. A custom_amount product has no asking
  // price — the buyer types the amount and the SERVER checks it against these
  // limits (httpapi/custom_amount.go). They are typed in the workspace currency
  // and converted to integer sen in ONE place (lib/productAmounts.ts).
  //
  // The minimum starts BLANK, not at the RM1.00 floor: this row labels its
  // fields with placeholders (there is no room for a visible <label> beside an
  // inline input), and a pre-filled value hides its own placeholder — the
  // merchant would see an unlabelled "1.00". `required` is what stops an empty
  // submit; the floor itself is the server's rule, not a default typed here.
  const [minAmt, setMinAmt] = useState("");
  const [maxAmt, setMaxAmt] = useState("");
  const [sugAmt, setSugAmt] = useState("");
  const [type, setType] = useState("one_time");
  const [period, setPeriod] = useState("month");
  const [trial, setTrial] = useState("0");
  const [stock, setStock] = useState("");
  // Issue #152: billing count — "infinite" (null) or a fixed N charges.
  const [chargesMode, setChargesMode] = useState<"infinite" | "fixed">("infinite");
  const [schedMode, setSchedMode] = useState<string>("from_payment");
  const [charges, setCharges] = useState("");

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    // Card t_7eb3b623: a custom_amount product is created from its LIMITS — the
    // API accepts a request with no `price_sen` for this type and derives the
    // display price from the suggestion (else the floor). Sending one would be a
    // bogus asking price, so the two payloads are built separately.
    let body: Record<string, unknown>;
    if (type === "custom_amount") {
      const parsed = customAmountBody(minAmt, maxAmt, sugAmt);
      if (!parsed.ok) {
        // A value that is not money at all. The LIMITS themselves (the RM1.00
        // floor, min <= max) are the server's rule; this only refuses to send
        // junk — `JSON.stringify(Infinity)` is null, which Go decodes as 0.
        const field =
          parsed.field === "min"
            ? minLabel
            : parsed.field === "max"
              ? maxLabel
              : sugLabel;
        setError(t("prod.priceInvalid", { name: field }));
        return;
      }
      body = {
        name,
        type,
        ...parsed.body,
        stock: stock === "" ? null : Math.max(0, parseInt(stock, 10) || 0),
      };
    } else {
      // Issue #551: this is the ONE place RM (typed) becomes sen. Never rely on
      // Math.max/parseFloat to sanitise — a non-finite result serialises to null,
      // which the server decodes as 0 (a silent RM 0.00 product). Reject it visibly.
      const rm = parseFloat(price);
      if (!Number.isFinite(rm) || rm < 0) {
        setError(t("prod.priceInvalid", { name: name || priceLabel }));
        return;
      }
      const priceSen = Math.round(rm * 100);
      body = {
        name,
        price_sen: priceSen,
        type,
        billing_period: type === "subscription" ? period : undefined,
        trial_days: type === "subscription" ? Math.max(0, parseInt(trial || "0", 10) || 0) : undefined,
        subscription_charges:
          type === "subscription" && chargesMode === "fixed"
            ? Math.max(1, parseInt(charges || "1", 10) || 1)
            : null,
        // round 13c: next-charge date policy.
        schedule_mode: type === "subscription" ? schedMode : undefined,
        stock: stock === "" ? null : Math.max(0, parseInt(stock, 10) || 0),
      };
    }
    const r = await fetch(`/api/rest/products${workspaceQuery()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (r.ok) {
      setName("");
      setPrice("");
      setStock("");
      setTrial("0");
      setCharges("");
      setChargesMode("infinite");
      setMinAmt("");
      setMaxAmt("");
      setSugAmt("");
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      // A refused limit triple surfaces on this row's own failure line. The
      // API's English sentence stays on the wire for REST/MCP clients: the
      // dashboard never renders server prose (lib/apiErr), and a class-code
      // refusal (`request.badRequest`) has no dictionary sentence — so the
      // merchant is told the action failed, not the rule (measured: max below
      // min answers "Failed to add product"). The rule itself is NOT re-typed
      // here — the server is the authority.
      setError(apiErr(d, t("prod.failedAdd"), t));
    }
  }

  // ── toggle status / delete (single row) ─────────────────────────────────
  async function toggleStatus(p: Product) {
    await fetch(`/api/rest/products/${p.id}${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ active: !p.active }),
    });
    load();
  }

  async function archiveOne(p: Product) {
    const ok = window.confirm(t("prod.archiveOneConfirm", { name: p.name }));
    if (!ok) return;
    await fetch(`/api/rest/products/${p.id}${workspaceQuery()}`, { method: "DELETE" });
    setNotify(t("prod.deletedToTrash"));
    load();
  }

  // The trash is fetched only when it is opened. It is a SEPARATE listing
  // (GET /api/rest/products/trash) because the catalogue deliberately excludes
  // trashed rows — an archived product is still listed, a trashed one is not.
  const loadTrash = useCallback(async () => {
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/products/trash${workspaceQuery()}`);
        return Array.isArray(d) ? (d as TrashedProduct[]) : [];
      },
      { onUnauth: goToLogin, message: t("prod.loadTrashFail") },
    );
    if (!r.ok) {
      setTrashed([]);
      setError(r.error);
      return;
    }
    setTrashed(r.data);
    setTrashLoaded(true);
    setError("");
  }, [t, goToLogin]);

  useEffect(() => {
    if (view === "trash") void loadTrash();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, loadTrash, workspace]);

  // Escape closes the purge dialog.
  useEffect(() => {
    if (!confirmPurge) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setConfirmPurge(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [confirmPurge]);

  async function restoreProduct(id: string) {
    setNotify("");
    // The NEXT action on this surface clears the previous refusal — at the START
    // of the action, never as a side effect of the relist below it (card
    // t_602c48f5).
    setTrashErr("");
    let res: Response;
    try {
      res = await fetch(`/api/rest/products/${id}/restore${workspaceQuery()}`, { method: "POST" });
    } catch {
      // A transport failure (offline, aborted, DNS) is an ACTION failure like any
      // other and it must leave a sentence on this surface. Without this branch
      // the rejection escaped through the row's `void restoreProduct(p.id)`
      // handler as an unhandled rejection and the merchant saw NOTHING at all —
      // `purgeProduct` already had its half, this one never did. Same class as the
      // missing catch card t_ab4aa85b found on /forms' restore.
      setTrashErr(t("prod.restoreFail"));
      return;
    }
    if (!res.ok) {
      // The sentence is rendered from the machine `code` in the merchant's OWN
      // language rather than from the server's Malay prose (issue #6177), which is
      // what `purgeProduct` below already does. Today the only refusal this route
      // can answer is the row not being in the bin, whose class code
      // (`request.notFound`) the dictionary deliberately does not translate, so
      // this falls back to the page's own line; routing it here is what makes a
      // coded refusal readable the moment the API grows one.
      //
      // It goes to `trashErr`, NOT `error`: the relist below clears `error` on
      // success, so the sentence used to be set and wiped in the same tick.
      const d = await res.json().catch(() => null);
      setTrashErr(apiErr(d, t("prod.restoreFail"), t));
      void loadTrash();
      return;
    }
    setNotify(t("prod.restored"));
    void loadTrash();
    load();
  }

  async function purgeProduct() {
    if (!confirmPurge || purgeBusy) return;
    // ONE rule, shared with the server (textutil.SameTypedValue): both sides are
    // trimmed. The comparison used to trim ONLY the caller's value and compare it
    // against the raw stored name, which no padded stored name can satisfy —
    // TrimSpace can never return a value with a leading/trailing space, and a
    // product name is stored verbatim — so for such a product this button could
    // NEVER enable and the row could not be purged from anywhere (card
    // t_1ee11d1f). The button below uses the same expression, so the guard and
    // the control cannot disagree. (The pre-fix expression is deliberately NOT
    // spelled out here: lib/typedConfirm.test.ts greps for it, and a comment
    // quoting it would satisfy the regression scan for the wrong reason.)
    if (!matchesTypedValue(purgeTyped, confirmPurge.name)) return;
    setPurgeBusy(true);
    setNotify("");
    // The next purge clears the previous refusal — at the START of the action,
    // never as a side effect of the relist (card t_602c48f5).
    setTrashErr("");
    try {
      const res = await fetch(`/api/rest/products/${confirmPurge.id}/purge${workspaceQuery()}`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ confirm_name: purgeTyped.trim() }),
      });
      if (!res.ok) {
        // A purge refused for a sale or a subscription names the obstacle in its
        // `code`, so the merchant gets that sentence in their OWN language
        // instead of the server's prose (issue #6177). Anything else falls back
        // to the page's own line. It renders beside the trash rows (`trashErr`),
        // so the refusal survives the relist below it.
        const d = await res.json().catch(() => null);
        setTrashErr(apiErr(d, t("prod.purgeFail"), t));
        setConfirmPurge(null);
        setPurgeTyped("");
        void loadTrash();
        return;
      }
      setNotify(t("prod.purged"));
      setConfirmPurge(null);
      setPurgeTyped("");
      void loadTrash();
    } catch {
      // A transport failure (offline, aborted) is an ACTION failure, so it uses
      // the same channel rather than the page's load-failure line — and the dialog
      // is closed with it, because it used to stay open behind a sentence the
      // relist then wiped.
      setTrashErr(t("prod.purgeFail"));
      setConfirmPurge(null);
      setPurgeTyped("");
    } finally {
      setPurgeBusy(false);
    }
  }

  // ── edit drawer save ─────────────────────────────────────────────────────
  const [editForm, setEditForm] = useState({
    name: "",
    description: "",
    price: "",
    type: "one_time",
    period: "month",
    trial: "0",
    stock: "",
    // Card t_7eb3b623: the pay-what-you-want limits as typed text, seeded from
    // the stored product (integer sen) by limitsToInputs below. Only sent when
    // the type is custom_amount — for every other type the server refuses them.
    min: "1.00",
    max: "",
    suggested: "",
    active: true,
    images: [] as string[],
    chargesMode: "infinite" as "infinite" | "fixed",
    scheduleMode: "from_payment",
    charges: "",
  });
  const [editTab, setEditTab] = useState("general");
  const [uploadingImg, setUploadingImg] = useState(false);
  const [savedFlash, setSavedFlash] = useState("");

  const isDirty = useMemo(() => {
    if (!editing) return false;
    const seed = limitsToInputs(editing);
    return (
      editForm.name !== editing.name ||
      editForm.description !== (editing.description || "") ||
      editForm.price !== (editing.price_sen / 100).toFixed(2) ||
      editForm.type !== editing.type ||
      (editForm.type === "custom_amount" &&
        (editForm.min !== seed.min ||
          editForm.max !== seed.max ||
          editForm.suggested !== seed.suggested)) ||
      (editForm.type === "subscription" &&
        editForm.period !== (editing.billing_period || "month")) ||
      editForm.trial !== String(editing.trial_days ?? 0) ||
      editForm.stock !==
        (typeof editing.stock === "number" ? String(editing.stock) : "") ||
      editForm.active !== editing.active ||
      editForm.chargesMode !==
        (typeof editing.subscription_charges === "number" ? "fixed" : "infinite") ||
      (editForm.chargesMode === "fixed" &&
        editForm.charges !== String(editing.subscription_charges ?? 1)) ||
      JSON.stringify(editForm.images) !== JSON.stringify(editing.images || [])
    );
  }, [editForm, editing]);

  function closeEditor() {
    if (isDirty && !window.confirm(t("prod.unsaved"))) return;
    setEditing(null);
    setEditTab("general");
    setSavedFlash("");
  }

  useEffect(() => {
    if (editing) {
      setEditForm({
        name: editing.name,
        description: editing.description || "",
        price: (editing.price_sen / 100).toFixed(2),
        type: editing.type,
        // Card t_7eb3b623: the limits are seeded from the stored product. A
        // product that is not (yet) custom_amount stores min 0, which the server
        // would refuse — limitsToInputs starts such a product at the floor, so
        // switching it to pay-what-you-want begins from an accepted value.
        ...limitsToInputs(editing),
        period: editing.billing_period || "month",
        trial: String(editing.trial_days ?? 0),
        stock: typeof editing.stock === "number" ? String(editing.stock) : "",
        active: editing.active,
        images: editing.images || [],
        chargesMode:
          typeof editing.subscription_charges === "number" ? "fixed" : "infinite",
        charges: String(editing.subscription_charges ?? 1),
        scheduleMode: editing.schedule_mode || "from_payment",
      });
      setEditTab("general");
      setDrawerError("");
      setSavedFlash("");
      setUploadingImg(false);
    }
  }, [editing]);

  async function uploadImage(file: File) {
    if (!file) return;
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setDrawerError(t("prod.uploadFail"));
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setDrawerError(t("prod.uploadFail"));
      return;
    }
    setUploadingImg(true);
    setDrawerError("");
    try {
      const fd = new FormData();
      fd.append("file", file);
      // t_e19368af: the scope must be on the URL, or the upload lands in the
      // session's first owned account instead of the selected workspace.
      const r = await fetch(workspaceUrl("/api/rest/uploads/image"), { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.url) {
        setDrawerError(t("prod.uploadFail"));
        return;
      }
      setEditForm((f) => ({ ...f, images: [...f.images, d.url] }));
      // Reordering/remove marks the form dirty (images changed).
    } finally {
      setUploadingImg(false);
    }
  }

  function moveImage(i: number, dir: -1 | 1) {
    setEditForm((f) => {
      const imgs = [...f.images];
      const j = i + dir;
      if (j < 0 || j >= imgs.length) return f;
      [imgs[i], imgs[j]] = [imgs[j], imgs[i]];
      return { ...f, images: imgs };
    });
  }

  async function saveEdit(e: React.FormEvent) {
    e.preventDefault();
    if (!editing) return;
    setDrawerBusy(true);
    setDrawerError("");
    // Issue #501: a product image whose URL is not https:// would render as
    // the html/template #ZgotmplZ sentinel on the buyer's page (form renderer
    // + restaurant menu). Catch it at the field; the server rejects it too.
    const badImage = editForm.images.find((u) => u.trim() && !u.trim().startsWith("https://"));
    if (badImage) {
      setDrawerError(t("url.notHttps"));
      setDrawerBusy(false);
      return;
    }
    const stockVal = editForm.stock === "" ? null : Math.max(0, parseInt(editForm.stock, 10) || 0);
    const trialVal = Math.max(0, parseInt(editForm.trial || "0", 10) || 0);
    // Card t_7eb3b623: a custom_amount product is saved from its LIMITS, not
    // from a price — the server derives the display price from the suggestion
    // (else the floor) and ignores any price_sen sent alongside the limits. The
    // inline dialog must therefore be able to CONVERT a product to this type,
    // which is only possible if it sends the limits with the type change.
    let body: Record<string, unknown>;
    if (editForm.type === "custom_amount") {
      const parsed = customAmountBody(editForm.min, editForm.max, editForm.suggested);
      if (!parsed.ok) {
        const field =
          parsed.field === "min" ? minLabel : parsed.field === "max" ? maxLabel : sugLabel;
        setDrawerError(t("prod.priceInvalid", { name: field }));
        setDrawerBusy(false);
        return;
      }
      body = {
        name: editForm.name,
        description: editForm.description,
        type: editForm.type,
        stock: stockVal,
        active: editForm.active,
        images: editForm.images,
        ...parsed.body,
        // A custom_amount product cannot carry variations (the server refuses
        // such a product), so converting one clears them — the same wholesale
        // replace ProductEditor's save performs. An empty list is an explicit
        // "replace with none"; omitting the key would KEEP them.
        variations: [],
      };
    } else {
      // Issue #551: same single-conversion rule as the create path — RM typed here
      // becomes sen exactly once, and a non-finite price is refused visibly.
      const rm = parseFloat(editForm.price);
      if (!Number.isFinite(rm) || rm < 0) {
        setDrawerError(t("prod.priceInvalid", { name: editForm.name || priceLabel }));
        setDrawerBusy(false);
        return;
      }
      body = {
        name: editForm.name,
        description: editForm.description,
        price_sen: Math.round(rm * 100),
        type: editForm.type,
        billing_period: editForm.type === "subscription" ? editForm.period : undefined,
        trial_days: editForm.type === "subscription" ? trialVal : undefined,
        subscription_charges:
          editForm.type === "subscription" && editForm.chargesMode === "fixed"
            ? Math.max(1, parseInt(editForm.charges || "1", 10) || 1)
            : null,
        schedule_mode: editForm.type === "subscription" ? editForm.scheduleMode : undefined,
        stock: stockVal,
        active: editForm.active,
        images: editForm.images,
      };
    }
    try {
      const r = await fetch(`/api/rest/products/${editing.id}${workspaceQuery()}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setDrawerError(apiErr(d, t("prod.failedUpdate"), t));
        return;
      }
      setSavedFlash(t("prod.updated"));
      load();
    } finally {
      setDrawerBusy(false);
    }
  }

  // In-flight: render inside the shell (see components/PageLoading.tsx) so the
  // sidebar and top bar stay mounted; the bare `<main>` this replaced dropped
  // the whole app chrome while the products fetch was outstanding.
  if (checking) {
    return <PageLoading active="products" />;
  }

  const countLabel = t("prod.countItems", { n: filtered.length });
  const statusLabel = (p: Product) => (p.active ? t("prod.active") : t("prod.archived"));
  const stockLabel = (p: Product) =>
    typeof p.stock === "number"
      ? p.stock > 0
        ? t("prod.inStock", { n: p.stock })
        : t("prod.soldOut")
      : "∞";
  const typeLabel = (p: Product) => {
    if (p.type === "subscription") {
      return `${t("prod.subscription")} · ${p.billing_period === "year" ? t("prod.yearly") : t("prod.monthly")}${
        typeof p.subscription_charges === "number"
          ? ` · ${t("prod.chargesNLabel", { n: p.subscription_charges })}`
          : ""
      }`;
    }
    // Pay-what-you-want: the range is the useful part of the type, so the list
    // shows the limits the server will enforce — a bare "Pay what you want" row
    // would not tell a merchant which of their products is capped (card
    // t_7eb3b623).
    if (p.type === "custom_amount") {
      return p.max_price_sen != null
        ? `${t("prod.customAmount")} · ${money(p.min_price_sen ?? 0)}–${money(p.max_price_sen)}`
        : t("prod.customAmount");
    }
    return t("prod.oneTime");
  };

  return (
    <Shell active="products">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("prod.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("prod.subtitle")}</p>
          </div>
          {/* Card t_7eb3b623: the full-page editor (/products/new) shipped in
              issue #151 and NOTHING linked to it — `git grep 'products/new'`
              across web/ returned nothing. It is the only surface with the
              product-type switch, variations and images, so an orphaned route
              hid all three.

              Card t_bb0d0bee: ONE entry point. This button used to be a LINK to
              /products/new while a second, always-open inline form sat right
              below it — two ways to create a product on one screen, and the
              inline form pushed the catalogue below the fold on a phone
              (measured at 390px: the first row's top was y=781 on an 844px
              viewport). The button now OPENS the quick-create panel (the reveal
              is `createOpen`), and the full editor stays reachable from inside
              that panel — a secondary "more options" route, not a second
              always-visible creation surface. */}
          {canWrite && (
            <button
              type="button"
              onClick={() => setCreateOpen((v) => !v)}
              aria-expanded={createOpen}
              aria-controls="prod-create"
              className={createOpen ? "jf-btn-ghost" : "jf-btn-primary"}
            >
              {createOpen ? t("common.cancel") : t("prod.newTitle")}
            </button>
          )}
        </div>

        {/* Add form — the reveal behind the header's "New product" button
            (card t_bb0d0bee). Collapsed by default so the catalogue is what a
            phone lands on; `id` matches the button's aria-controls. */}
        {canWrite && createOpen && (
        <form id="prod-create" onSubmit={add} className="jf-card mt-6 flex flex-wrap gap-2 p-4">
          {/* The inputs and selects in this row are named with the SAME shape —
              `aria-label={t(...)}` — and neither half may fall back to its
              placeholder. A `<select>` with only `<option>` children has no
              accessible name at all, so axe reports `select-name` (CRITICAL) on
              the live row (card t_3fbc6953); an INPUT with a placeholder is the
              mirror-image defect axe CANNOT report, because axe accepts a
              placeholder as an accessible name — so name / price / stock stayed
              unnamed through every green scan until the placeholder-refusing
              census in `e2e/controlNames.ts` named all three (card t_becb0f50).
              The name is announced only from `aria-label`, and it survives the
              user typing (the placeholder does not). */}
          <input value={name} onChange={(e) => setName(e.target.value)} required placeholder={t("prod.name")} aria-label={t("prod.name")} className="jf-input min-w-48 flex-1" />
          {/* The row's `<select>`s carry the same `aria-label` treatment for the
              `select-name` reason above (card t_3fbc6953). The TYPE selector sits
              before the money fields because it decides which of them apply — a
              pay-what-you-want product has no asking price, only limits (card
              t_7eb3b623). */}
          <select value={type} onChange={(e) => setType(e.target.value)} className="jf-input w-48" aria-label={t("prod.type")}>
            <option value="one_time">{t("prod.oneTime")}</option>
            <option value="subscription">{t("prod.subscription")}</option>
            <option value="custom_amount">{t("prod.customAmount")}</option>
          </select>
          {type === "custom_amount" ? (
            // Pay what you want: no asking price — the buyer types the amount and
            // the SERVER checks it against these limits (httpapi/custom_amount.go).
            // The minimum is required there, so it is required here; a maximum or
            // a suggestion left blank means "none" (null), not zero.
            <>
              <input
                value={minAmt}
                onChange={(e) => setMinAmt(e.target.value)}
                required
                type="number"
                step="0.01"
                placeholder={minLabel}
                aria-label={minLabel}
                className="jf-input w-48"
              />
              <input
                value={maxAmt}
                onChange={(e) => setMaxAmt(e.target.value)}
                type="number"
                step="0.01"
                min="0"
                placeholder={maxLabel}
                aria-label={maxLabel}
                className="jf-input w-52"
              />
              <input
                value={sugAmt}
                onChange={(e) => setSugAmt(e.target.value)}
                type="number"
                step="0.01"
                min="0"
                placeholder={sugLabel}
                aria-label={sugLabel}
                className="jf-input w-52"
              />
            </>
          ) : (
            <input value={price} onChange={(e) => setPrice(e.target.value)} required type="number" step="0.01" min="0" placeholder={priceLabel} aria-label={priceLabel} className="jf-input w-32" />
          )}
          <input value={stock} onChange={(e) => setStock(e.target.value)} type="number" min="0" placeholder={t("prod.stock")} aria-label={t("prod.stock")} className="jf-input w-40" />
          {type === "subscription" && (
            <>
              <select value={period} onChange={(e) => setPeriod(e.target.value)} className="jf-input w-32" aria-label={t("prod.billingPeriod")}>
                <option value="month">{t("prod.monthly")}</option>
                <option value="year">{t("prod.yearly")}</option>
              </select>
              <input
                value={trial}
                onChange={(e) => setTrial(e.target.value)}
                type="number"
                min="0"
                step="1"
                placeholder={t("prod.trial")}
                title={t("prod.trial")}
                aria-label={t("prod.trial")}
                className="jf-input w-36"
              />
              <select
                value={chargesMode}
                onChange={(e) => setChargesMode(e.target.value as "infinite" | "fixed")}
                className="jf-input w-40"
                title={t("prod.billingCount")}
                aria-label={t("prod.billingCount")}
              >
                <option value="infinite">{t("prod.chargesInfinite")}</option>
                <option value="fixed">{t("prod.chargesFixed")}</option>
              </select>
              <select
                value={schedMode}
                onChange={(e) => setSchedMode(e.target.value)}
                className="jf-input w-56"
                title={t("prod.scheduleMode")}
                aria-label={t("prod.scheduleMode")}
              >
                <option value="from_payment">{t("prod.schedFromPayment")}</option>
                <option value="catch_up">{t("prod.schedCatchUp")}</option>
                <option value="skip_missed">{t("prod.schedSkipMissed")}</option>
              </select>
              {chargesMode === "fixed" && (
                <input
                  value={charges}
                  onChange={(e) => setCharges(e.target.value)}
                  type="number"
                  min="1"
                  step="1"
                  placeholder={t("prod.chargesN")}
                  title={t("prod.chargesN")}
                  aria-label={t("prod.chargesN")}
                  className="jf-input w-24"
                />
              )}
            </>
          )}
          <button className="jf-btn-primary">{t("prod.add")}</button>
          {/* Card t_7eb3b623's route, kept reachable as the SECONDARY path
              (card t_bb0d0bee): /products/new is the only surface with
              variations, images and the payment link, and an orphaned route
              hid all three once already. It is a link inside the one creation
              panel, not a second always-visible entry point. */}
          <Link href="/products/new" className="jf-btn-ghost">
            {t("prod.fullEditor")}
          </Link>
        </form>
        )}
        {error && (
          <p role="alert" className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">
            {error}
            {/* Issue #503: a failed load gets an action, not a dead end. */}
            <button
              type="button"
              onClick={() => void (view === "trash" ? loadTrash() : load())}
              className="ml-2 underline decoration-dotted underline-offset-2"
            >
              {t("common.retry")}
            </button>
          </p>
        )}

        {/* Active / Trash switcher. The catalogue is the DEFAULT: a fresh
            visitor must never land in the trash. */}
        <div
          className="mt-5 flex flex-wrap items-center gap-1 border-b border-[var(--jf-border)]"
          role="tablist"
          onKeyDown={tablistKeyDown}
        >
          {(["active", "trash"] as const).map((v) => (
            <button
              key={v}
              id={tabDomId("prod", v)}
              aria-controls={panelDomId("prod", v)}
              tabIndex={tabStop(v, view)}
              type="button"
              role="tab"
              aria-selected={view === v}
              onClick={() => {
                setView(v);
                setError("");
                setNotify("");
                // Switching views is the user's next action: the refusal about the
                // view being left is cleared with the success notice (card
                // t_602c48f5).
                setTrashErr("");
              }}
              className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium ${
                view === v
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-[var(--jf-muted)] hover:text-slate-800"
              }`}
            >
              {v === "active" ? t("prod.trashTab") : t("prod.trashTabDeleted")}
              {v === "trash" && trashLoaded && trashed.length > 0 ? ` (${trashed.length})` : ""}
            </button>
          ))}
        </div>

        {notify && (
          <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{notify}</p>
        )}

        {/* Trash view: the bin, with restore and delete-for-good. */}
        {view === "trash" && (
          <div
            role="tabpanel"
            id={panelDomId("prod", "trash")}
            aria-labelledby={tabDomId("prod", "trash")}
          >
            <p className="mt-4 max-w-2xl text-sm text-[var(--jf-muted)]">{t("prod.trashNote")}</p>
            {/* The refusal (card t_602c48f5), rendered beside the rows it is about.
                It is a state no loader writes, so the relist that reports the
                refusal cannot erase the sentence — and unlike the page's load-failure
                line above it carries no Retry, because a refusal changes nothing
                about the listing and reloading it is not the remedy. */}
            {trashErr && (
              <p
                role="alert"
                className="mt-3 max-w-2xl rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700"
              >
                {trashErr}
              </p>
            )}
            <ul className="mt-4 space-y-2">
              {trashed.map((p) => (
                <li
                  key={p.id}
                  className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{p.name}</p>
                    <p className="text-jf-2xs text-[var(--jf-muted)]">
                      {money(p.price_sen)} · {t("prod.trashDeletedAt")}{" "}
                      {formatDate(p.deleted_at, user?.timezone)}
                    </p>
                    {/* Withheld rather than offered-and-refused: the server
                        refuses on a sale or a live subscription, and the count
                        says which obstacle to clear. */}
                    {!p.can_purge && (
                      <p className="mt-1 text-jf-2xs text-amber-700">
                        {t("prod.canPurgeFalse")}{" "}
                        {p.sales_count > 0
                          ? `${t("prod.salesCount")}: ${p.sales_count}`
                          : `${t("prod.subsCount")}: ${p.subscription_count}`}
                      </p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                    {canWrite && (
                      <>
                        <button
                          type="button"
                          onClick={() => void restoreProduct(p.id)}
                          className="jf-btn-ghost"
                        >
                          {t("prod.restore")}
                        </button>
                        {p.can_purge && (
                          <button
                            type="button"
                            onClick={() => {
                              setConfirmPurge(p);
                              setPurgeTyped("");
                              setError("");
                            }}
                            className="jf-btn-ghost text-rose-700"
                          >
                            {t("prod.purge")}
                          </button>
                        )}
                      </>
                    )}
                  </div>
                </li>
              ))}
              {trashed.length === 0 && (
                <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("prod.trashEmpty")}</li>
              )}
            </ul>
          </div>
        )}

        {view === "active" && (
        <div
          role="tabpanel"
          id={panelDomId("prod", "active")}
          aria-labelledby={tabDomId("prod", "active")}
        >
        {/* Bulk action bar */}
        {canWrite && ids.length > 0 && (
          <div className="jf-card mt-4 flex flex-wrap items-center gap-2 px-4 py-3">
            <span className="text-sm font-medium">{t("forms.nSelected", { n: ids.length })}</span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                onClick={bulkArchive}
                disabled={busy}
                className="jf-btn-ghost text-rose-700"
              >
                {t("prod.bulkArchive")}
              </button>
            </div>
          </div>
        )}

        {/* Search + filters */}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("prod.searchPlaceholder")}
            aria-label={t("prod.searchPlaceholder")}
            className="jf-input min-w-48 flex-1 sm:max-w-xs"
          />
          <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)} className="jf-input w-40" aria-label={t("prod.filterType")}>
            <option value="all">{t("prod.filterAll")}</option>
            <option value="one_time">{t("prod.oneTime")}</option>
            <option value="subscription">{t("prod.subscription")}</option>
            <option value="custom_amount">{t("prod.customAmount")}</option>
          </select>
          <select value={stockFilter} onChange={(e) => setStockFilter(e.target.value)} className="jf-input w-44" aria-label={t("prod.filterStock")}>
            <option value="all">{t("prod.filterStockAll")}</option>
            <option value="in">{t("prod.inStockShort")}</option>
            <option value="out">{t("prod.outOfStock")}</option>
          </select>
          <span className="ml-auto whitespace-nowrap text-xs text-[var(--jf-muted)]">{countLabel}</span>
        </div>

        {/* PHONE LAYOUT (card t_c83ecf3e): the table below switches OFF its
            select, Type, Stock and Date columns below `sm`, so a phone showed a
            product's name and price with no type, no stock and no date. Below
            `sm` each row is a card — the bulk-select box and the status chip stay
            on the card (the bulk bar above must keep working), and the three
            hidden columns sit one disclosure away. The table above `sm` is
            unchanged. */}
        <div className="jf-card mt-4 overflow-hidden sm:hidden">
          {pageRows.length > 0 && (
            <MobileRecordList>
              {pageRows.map((p) => (
                <MobileRecord
                  key={p.id}
                  leading={
                    canWrite ? (
                      <input
                        type="checkbox"
                        aria-label={`Select ${p.name}`}
                        checked={selected.has(p.id)}
                        onChange={() => toggle(p.id)}
                        className="h-4 w-4 accent-[var(--jf-primary)]"
                      />
                    ) : null
                  }
                  title={p.name}
                  meta={money(p.price_sen)}
                  badges={
                    canWrite ? (
                      <button
                        onClick={() => toggleStatus(p)}
                        className={[
                          "rounded-full px-2.5 py-0.5 text-jf-2xs font-medium",
                          p.active
                            ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                            : "border border-slate-200 bg-slate-50 text-slate-500",
                        ].join(" ")}
                        title={t("prod.toggleStatusTitle")}
                      >
                        {statusLabel(p)}
                      </button>
                    ) : (
                      <span
                        className={[
                          "inline-block rounded-full px-2.5 py-0.5 text-jf-2xs font-medium",
                          p.active
                            ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                            : "border border-slate-200 bg-slate-50 text-slate-500",
                        ].join(" ")}
                      >
                        {statusLabel(p)}
                      </span>
                    )
                  }
                  fields={[
                    { key: "type", label: t("prod.type"), value: typeLabel(p) },
                    { key: "stock", label: t("prod.stockShort"), value: stockLabel(p) },
                    {
                      key: "date",
                      label: t("prod.date"),
                      value: p.created_at ? formatDate(p.created_at, user?.timezone) : "—",
                    },
                  ]}
                  detailsLabel={t("common.details")}
                  actions={
                    canWrite ? (
                      <>
                        <button onClick={() => setEditing(p)} className="jf-btn-ghost">
                          {t("forms.edit")}
                        </button>
                        <Link href={`/products/${p.id}/edit`} className="jf-btn-ghost">
                          {t("prod.openEditor")}
                        </Link>
                        <button
                          onClick={() => archiveOne(p)}
                          className="jf-btn-ghost text-rose-700"
                          title={t("prod.archiveOneTitle")}
                        >
                          ✕
                        </button>
                      </>
                    ) : null
                  }
                />
              ))}
            </MobileRecordList>
          )}
          {pageRows.length === 0 && (
            <p className="px-4 py-8 text-center text-sm text-[var(--jf-muted)]">{t("prod.empty")}</p>
          )}
        </div>

        {/* Table (sm and up — the phone arm above owns everything below that) */}
        <div className="jf-card mt-4 hidden overflow-x-auto sm:block">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left">
                <th className="hidden w-10 px-4 py-3 sm:table-cell">
                  {canWrite && (
                    <input
                      type="checkbox"
                      aria-label={t("forms.selectAll")}
                      checked={allOnPageSelected}
                      onChange={toggleAll}
                      className="h-4 w-4 accent-[var(--jf-primary)]"
                    />
                  )}
                </th>
                <SortHeader label={t("prod.name")} k="name" sortKey={sortKey} sortDir={sortDir} onToggle={toggleSort} className="px-2 py-3" />
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
                  {t("prod.type")}
                </th>
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
                  {t("prod.stockShort")}
                </th>
                <SortHeader label={priceLabel} k="price_sen" sortKey={sortKey} sortDir={sortDir} onToggle={toggleSort} className="px-2 py-3" />
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("dash.status")}
                </th>
                <SortHeader label={t("prod.date")} k="created_at" sortKey={sortKey} sortDir={sortDir} onToggle={toggleSort} className="hidden px-2 py-3 sm:table-cell" />
                <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("bld.actions")}
                </th>
              </tr>
            </thead>
            <tbody>
              {pageRows.map((p) => (
                <tr
                  key={p.id}
                  // NO `opacity-60` ON AN ARCHIVED ROW (card t_3fbc6953). It was
                  // the whole-row "this is archived" signal, and it is a
                  // contrast defect: `opacity` composites the row's text towards
                  // whatever is behind it, so `--jf-primary` (4.70:1 on white)
                  // and `--jf-muted` (4.83:1) both fell to roughly 1.6:1. The
                  // page's own new a11y scan reports it as a serious
                  // `color-contrast` node the moment `/products` is rendered in
                  // a browser — the class of defect a source guard cannot see.
                  // The archived state is already carried by the Status column's
                  // own chip ("Archived", with its own tinted surface) and by
                  // `prod.inactiveNote` in the editor, so the row keeps its
                  // AA-safe tokens and no longer fades.
                  className="border-b border-[var(--jf-border)] last:border-0"
                >
                  <td className="hidden px-4 py-3 sm:table-cell">
                    {canWrite && (
                      <input
                        type="checkbox"
                        aria-label={`Select ${p.name}`}
                        checked={selected.has(p.id)}
                        onChange={() => toggle(p.id)}
                        className="h-4 w-4 accent-[var(--jf-primary)]"
                      />
                    )}
                  </td>
                  <td className="px-2 py-3">
                    {canWrite ? (
                      <button
                        onClick={() => setEditing(p)}
                        className="text-left font-medium text-[var(--jf-primary)] hover:underline"
                      >
                        {p.name}
                      </button>
                    ) : (
                      <span className="font-medium">{p.name}</span>
                    )}
                  </td>
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">{typeLabel(p)}</td>
                  <td className="hidden px-2 py-3 sm:table-cell">{stockLabel(p)}</td>
                  <td className="px-2 py-3 font-semibold">{money(p.price_sen)}</td>
                  <td className="px-2 py-3">
                    {canWrite ? (
                      <button
                        onClick={() => toggleStatus(p)}
                        className={[
                          "rounded-full px-2.5 py-0.5 text-jf-2xs font-medium",
                          p.active
                            ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                            : "border border-slate-200 bg-slate-50 text-slate-500",
                        ].join(" ")}
                        title={t("prod.toggleStatusTitle")}
                      >
                        {statusLabel(p)}
                      </button>
                    ) : (
                      <span
                        className={[
                          "inline-block rounded-full px-2.5 py-0.5 text-jf-2xs font-medium",
                          p.active
                            ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                            : "border border-slate-200 bg-slate-50 text-slate-500",
                        ].join(" ")}
                      >
                        {statusLabel(p)}
                      </span>
                    )}
                  </td>
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">
                    {p.created_at ? formatDate(p.created_at, user?.timezone) : "—"}
                  </td>
                  <td className="px-4 py-3">
                    {canWrite && (
                      <div className="flex items-center justify-end gap-1">
                        <button onClick={() => setEditing(p)} className="jf-btn-ghost">
                          {t("forms.edit")}
                        </button>
                        {/* Card t_7eb3b623: the inline dialog above is the QUICK
                            edit (name/type/money/stock); the full-page editor at
                            /products/<id>/edit is where variations and images
                            live, and it had no link at all. */}
                        <Link href={`/products/${p.id}/edit`} className="jf-btn-ghost">
                          {t("prod.openEditor")}
                        </Link>
                        <button
                          onClick={() => archiveOne(p)}
                          className="jf-btn-ghost text-rose-700"
                          title={t("prod.archiveOneTitle")}
                        >
                          ✕
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
              {pageRows.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-4 py-8 text-center text-sm text-[var(--jf-muted)]">
                    {t("prod.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("prod.pageOf", { page: safePage, total: totalPages })}
            </span>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={safePage <= 1}
                className="jf-btn-ghost"
              >
                {t("common.prev")}
              </button>
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={safePage >= totalPages}
                className="jf-btn-ghost"
              >
                {t("common.next")}
              </button>
            </div>
          </div>
        )}

        {/* Edit drawer / view — WC-style product editor (#124) */}
        {editing && (
          <div className="fixed inset-0 z-50 flex justify-end bg-black/30" onClick={closeEditor}>
            <div
              className="h-full w-full max-w-xl overflow-y-auto bg-white p-6 shadow-xl"
              onClick={(e) => e.stopPropagation()}
              role="dialog"
              aria-label={t("prod.editTitle")}
            >
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">{t("prod.editTitle")}</h2>
                <button onClick={closeEditor} className="text-lg text-[var(--jf-muted)]" aria-label={t("prof.twoFaClose")}>
                  ✕
                </button>
              </div>

              {/* tabs */}
              <div className="mt-4 flex flex-wrap gap-1 border-b border-[var(--jf-border)]">
                {(
                  [
                    ["general", t("prod.general")],
                    ["subscription", t("prod.subscriptionTab")],
                    ["inventory", t("prod.inventory")],
                    ["media", t("prod.media")],
                  ] as const
                ).map(([key, label]) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setEditTab(key)}
                    className={`rounded-t-lg px-4 py-2 text-sm font-medium ${
                      editTab === key
                        ? "border border-b-0 border-[var(--jf-border)] text-[var(--jf-primary)]"
                        : "text-[var(--jf-muted)] hover:text-[var(--jf-primary)]"
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>

              {savedFlash && (
                <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700">
                  {savedFlash}
                </p>
              )}

              <form onSubmit={saveEdit} className="mt-5 space-y-4">
                {editTab === "general" && (
                  <>
                    <label className="block text-sm font-medium">
                      {t("prod.name")}
                      <input
                        value={editForm.name}
                        onChange={(e) => setEditForm((f) => ({ ...f, name: e.target.value }))}
                        required
                        className="jf-input mt-1"
                      />
                    </label>
                    <label className="block text-sm font-medium">
                      {t("prod.description")}
                      <textarea
                        value={editForm.description}
                        onChange={(e) => setEditForm((f) => ({ ...f, description: e.target.value }))}
                        rows={4}
                        placeholder={t("prod.descriptionPh")}
                        className="jf-input mt-1 resize-y"
                      />
                    </label>
                    <label className="block text-sm font-medium">
                      {t("prod.type")}
                      <select
                        value={editForm.type}
                        onChange={(e) => setEditForm((f) => ({ ...f, type: e.target.value }))}
                        className="jf-input mt-1"
                      >
                        <option value="one_time">{t("prod.oneTime")}</option>
                        <option value="subscription">{t("prod.subscription")}</option>
                        <option value="custom_amount">{t("prod.customAmount")}</option>
                      </select>
                    </label>
                    {editForm.type === "custom_amount" ? (
                      // Card t_7eb3b623: pay what you want — no asking price. The
                      // limits ARE the contract the server checks the buyer's
                      // amount against (httpapi/custom_amount.go). The floor and
                      // min <= max stay the server's rule: a refused triple comes
                      // back as this dialog's failure line, never as a second copy
                      // of the rule typed into the client.
                      <>
                        <div className="grid gap-3 sm:grid-cols-3">
                          <label className="block text-sm font-medium">
                            {minLabel}
                            <input
                              value={editForm.min}
                              onChange={(e) => setEditForm((f) => ({ ...f, min: e.target.value }))}
                              required
                              type="number"
                              step="0.01"
                              className="jf-input mt-1"
                            />
                          </label>
                          <label className="block text-sm font-medium">
                            {maxLabel}
                            <input
                              value={editForm.max}
                              onChange={(e) => setEditForm((f) => ({ ...f, max: e.target.value }))}
                              type="number"
                              step="0.01"
                              min="0"
                              className="jf-input mt-1"
                            />
                          </label>
                          <label className="block text-sm font-medium">
                            {sugLabel}
                            <input
                              value={editForm.suggested}
                              onChange={(e) =>
                                setEditForm((f) => ({ ...f, suggested: e.target.value }))
                              }
                              type="number"
                              step="0.01"
                              min="0"
                              className="jf-input mt-1"
                            />
                          </label>
                        </div>
                        <p className="text-xs text-[var(--jf-muted)]">{t("prod.amountHint")}</p>
                      </>
                    ) : (
                      <label className="block text-sm font-medium">
                        {priceLabel}
                        <input
                          value={editForm.price}
                          onChange={(e) => setEditForm((f) => ({ ...f, price: e.target.value }))}
                          required
                          type="number"
                          step="0.01"
                          min="0"
                          className="jf-input mt-1"
                        />
                      </label>
                    )}
                  </>
                )}

                {editTab === "subscription" && (
                  <>
                    {editForm.type !== "subscription" ? (
                      <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700">
                        {t("prod.general")}:{" "}
                        <button
                          type="button"
                          className="font-medium text-[var(--jf-primary)] underline"
                          onClick={() => setEditTab("general")}
                        >
                          {t("prod.subscription")}
                        </button>
                      </p>
                    ) : (
                      <>
                        <label className="block text-sm font-medium">
                          {t("prod.billingPeriod")}
                          <select
                            value={editForm.period}
                            onChange={(e) => setEditForm((f) => ({ ...f, period: e.target.value }))}
                            className="jf-input mt-1"
                          >
                            <option value="month">{t("prod.monthly")}</option>
                            <option value="year">{t("prod.yearly")}</option>
                          </select>
                        </label>
                        <label className="block text-sm font-medium">
                          {t("prod.trial")}
                          <input
                            value={editForm.trial}
                            onChange={(e) => setEditForm((f) => ({ ...f, trial: e.target.value }))}
                            type="number"
                            min="0"
                            step="1"
                            className="jf-input mt-1"
                          />
                          <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                            {t("prod.trialHint")}
                          </span>
                        </label>
                        <label className="block text-sm font-medium">
                          {t("prod.billingCount")}
                          <select
                            value={editForm.chargesMode}
                            onChange={(e) =>
                              setEditForm((f) => ({
                                ...f,
                                chargesMode: e.target.value as "infinite" | "fixed",
                              }))
                            }
                            className="jf-input mt-1"
                          >
                            <option value="infinite">{t("prod.chargesInfinite")}</option>
                            <option value="fixed">{t("prod.chargesFixed")}</option>
                          </select>
                          <select
                            value={editForm.scheduleMode}
                            onChange={(e) =>
                              setEditForm((f) => ({ ...f, scheduleMode: e.target.value }))
                            }
                            className="jf-input mt-1"
                          >
                            <option value="from_payment">{t("prod.schedFromPayment")}</option>
                            <option value="catch_up">{t("prod.schedCatchUp")}</option>
                            <option value="skip_missed">{t("prod.schedSkipMissed")}</option>
                          </select>
                          <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                            {t("prod.scheduleModeHint")}
                          </span>
                          {editForm.chargesMode === "fixed" && (
                            <input
                              value={editForm.charges}
                              onChange={(e) =>
                                setEditForm((f) => ({ ...f, charges: e.target.value }))
                              }
                              type="number"
                              min="1"
                              step="1"
                              className="jf-input mt-1"
                            />
                          )}
                          <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                            {t("prod.chargesHint")}
                          </span>
                        </label>
                      </>
                    )}
                  </>
                )}

                {editTab === "inventory" && (
                  <>
                    <label className="block text-sm font-medium">
                      {t("prod.stock")}
                      <input
                        value={editForm.stock}
                        onChange={(e) => setEditForm((f) => ({ ...f, stock: e.target.value }))}
                        type="number"
                        min="0"
                        className="jf-input mt-1"
                      />
                      <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                        {t("prod.noStockNote")}
                      </span>
                    </label>
                    {typeof editing.stock === "number" && editing.stock === 0 && (
                      <p className="rounded-lg bg-red-50 px-3 py-2 text-xs font-medium text-red-700">
                        {t("prod.soldOut")} — set a stock above to reinstate it.
                      </p>
                    )}
                  </>
                )}

                {editTab === "media" && (
                  <>
                    <div className="block text-sm font-medium">
                      <div className="mb-2">{t("prod.images")}</div>
                      <div className="flex flex-wrap gap-3">
                        {editForm.images.map((url, i) => (
                          <div key={`${i}-${url}`} className="group relative">
                            {/* eslint-disable-next-line @next/next/no-img-element */}
                            <img
                              src={url}
                              alt={t("prod.images")}
                              className="h-24 w-24 rounded-lg border border-[var(--jf-border)] object-cover"
                            />
                            <div className="absolute -top-2 -right-2 flex gap-1">
                              <button
                                type="button"
                                onClick={() => moveImage(i, -1)}
                                disabled={i === 0}
                                className="rounded-full bg-white px-1.5 text-xs shadow ring-1 ring-slate-200 disabled:opacity-30"
                                title={t("prod.moveUp")}
                              >
                                ↑
                              </button>
                              <button
                                type="button"
                                onClick={() => moveImage(i, 1)}
                                disabled={i === editForm.images.length - 1}
                                className="rounded-full bg-white px-1.5 text-xs shadow ring-1 ring-slate-200 disabled:opacity-30"
                                title={t("prod.moveDown")}
                              >
                                ↓
                              </button>
                            </div>
                            <button
                              type="button"
                              onClick={() =>
                                setEditForm((f) => ({
                                  ...f,
                                  images: f.images.filter((_, k) => k !== i),
                                }))
                              }
                              className="absolute bottom-1 left-1/2 -translate-x-1/2 rounded bg-black/60 px-2 py-0.5 text-jf-3xs text-white opacity-0 transition group-hover:opacity-100"
                            >
                              {t("prod.removeImage")}
                            </button>
                          </div>
                        ))}
                      </div>
                      <label className="mt-3 inline-flex cursor-pointer items-center gap-2 rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-2 text-sm text-[var(--jf-muted)] hover:border-[var(--jf-primary)] hover:text-[var(--jf-primary)]">
                        {uploadingImg ? t("prod.uploading") : t("prod.addImage")}
                        <input
                          type="file"
                          accept="image/png,image/jpeg,image/webp,image/gif"
                          className="hidden"
                          disabled={uploadingImg}
                          onChange={(e) => {
                            const f = e.target.files?.[0];
                            e.target.value = "";
                            if (f) uploadImage(f);
                          }}
                        />
                      </label>
                      <span className="mt-2 block text-xs text-[var(--jf-muted)]">
                        {t("prod.imagesNote")}
                      </span>
                    </div>
                  </>
                )}

                {drawerError && (
                  <p className="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">{drawerError}</p>
                )}

                {/* status / visibility */}
                <div className="rounded-lg border border-[var(--jf-border)] p-3">
                  <div className="mb-1 text-sm font-medium">{t("prod.status")}</div>
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={editForm.active}
                      onChange={(e) => setEditForm((f) => ({ ...f, active: e.target.checked }))}
                      className="h-4 w-4 accent-[var(--jf-primary)]"
                    />
                    {t("prod.active")}
                  </label>
                  {!editForm.active && (
                    <p className="mt-1 text-xs text-amber-600">{t("prod.inactiveNote")}</p>
                  )}
                  <p className="mt-2 text-xs text-[var(--jf-muted)]">{t("prod.activeHint")}</p>
                </div>

                <div className="flex items-center gap-2 pt-2">
                  <button type="submit" disabled={drawerBusy} className="jf-btn-primary disabled:opacity-50">
                    {drawerBusy ? t("builder.saving") : t("set.save")}
                  </button>
                  <button type="button" onClick={closeEditor} className="jf-btn-ghost">
                    {t("prod.cancel")}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}
        </div>
        )}

        {/* Purge confirmation. A permanent delete cannot be undone and takes the
            product's variations with it, so it asks the caller to type the name
            back — the same level as the forms bin, using the product's NAME
            because a product has no slug.

            The naming of this dialog is pinned by `e2e/a11y-dialogs.spec.ts`, the
            way the forms bin and the workspace danger zone are: it must name
            itself with `aria-labelledby` pointing at its own heading (an
            `aria-label` copy of the title cannot be checked against a heading, so
            a later edit could move the heading and leave the name behind), and its
            field must be reachable by a real `<label for>` rather than only an
            `aria-label`. Both were missing here (card t_eedd6522) — the same two
            gaps the sibling dialogs had before PR #1251 covered them. */}
        {confirmPurge && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
            onClick={() => setConfirmPurge(null)}
          >
            <div
              role="dialog"
              aria-modal="true"
              aria-labelledby="prod-purge-title"
              className="w-full max-w-md rounded-xl bg-white p-5 shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <p id="prod-purge-title" className="text-sm font-semibold">{t("prod.purgeConfirmTitle")}</p>
              <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("prod.purgeConfirmBody")}</p>
              <label
                htmlFor="prod-purge-confirm-name"
                className="mt-3 block text-xs font-medium text-[var(--jf-muted)]"
              >
                {t("prod.purgeConfirmFieldLabel")}
              </label>
              <input
                id="prod-purge-confirm-name"
                type="text"
                value={purgeTyped}
                autoFocus
                autoComplete="off"
                disabled={purgeBusy}
                onChange={(e) => setPurgeTyped(e.target.value)}
                className="jf-input mt-1 w-full"
              />
              <div className="mt-4 flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setConfirmPurge(null)}
                  className="jf-btn-ghost"
                >
                  {t("prod.cancel")}
                </button>
                <button
                  type="button"
                  onClick={() => void purgeProduct()}
                  disabled={purgeBusy || !matchesTypedValue(purgeTyped, confirmPurge.name)}
                  className="rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 disabled:opacity-40"
                >
                  {/* The in-flight copy, same key as the forms bin and the danger
                      zone: the button must stay READABLE as "working" while the
                      delete is out, not merely go dim (card t_eedd6522). */}
                  {purgeBusy ? t("common.saving") : t("prod.purge")}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}
