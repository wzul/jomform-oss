"use client";

// Issue #126: dedicated Sales page (Gravity-Forms-Entries-style).
// One page lists every sale across the merchant's forms with a per-form &
// status filter, search, date range, pagination, a row detail drawer
// (buyer / items / amounts / status), confirm/capture/release actions for
// skip-capture sales, and CSV export. Deep-linked from the forms list /
// builder via /sales?form=<id> which pre-applies the form filter.
//
// Mobile-first table approach mirrors #122/#59: the table scrolls
// horizontally inside a card on small screens (min-width), and the controls
// wrap into a clean stack.

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n, type Key } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate, formatDateTime } from "../../lib/formatDate";
import { Switch } from "../../components/Switch";
import { answersPreview, answerText, isResponseForm, isMixedList, responsesList, rowKey, rowKind } from "../../lib/salesRows";
import { workspaceQuery, activeWorkspaceId, soundEnabledFromSettings, soundToggleBody } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { apiErr, translateApiErr } from "../../lib/apiErr";
import { formatMoney, formatTotals, normaliseCurrency } from "../../lib/money";

type Item = {
  product_id: string;
  price_sen: number;
  qty: number;
  name: string;
  // Issue #153: which variation was bought (empty for simple products).
  variation_id?: string;
  variation_name?: string;
};

type Sale = {
  ref_code: string;
  status: string;
  total_sen: number;
  // t_71c67471: the currency this sale was RECORDED in (ISO code). Emitted by
  // toSaleRow for every sale row and absent on a response row (no money). Every
  // amount on the row — total, shipping, discount, refunded, outstanding and
  // each item line — is denominated in it.
  currency?: string;
  payment_method: string;
  buyer: string;
  created_at: string;
  skip_capture?: boolean;
  authorized?: boolean;
  captured_at?: string;
  email_status?: string;
  form_id?: string;
  sale_type?: string;
  invoice_no?: string;
  buyer_email?: string;
  buyer_phone?: string;
  subtotal_sen?: number;
  shipping_sen?: number;
  // Shipping phase 2: WHERE the shipping fee came from, so a disputed fee is
  // explainable without reading the raw record. `shipping_source` is the source
  // that WON ("courier" | "zones") and `shipping_source_detail` names it (the
  // courier provider, or the zone the address matched). When a courier was named
  // but did not price the sale, `shipping_fallback_reason` says which KIND of
  // failure sent it to the zone rate ("unavailable" | "error" | "timeout") — the
  // word "failed" is not enough to argue about a fee.
  shipping_source?: string;
  shipping_source_detail?: string;
  shipping_fallback_reason?: string;
  discount_sen?: number;
  items?: Item[];
  updated_at?: string;
  // Issue #438: QR ordering flow — the table number the QR stand encoded.
  table_number?: string;
  // Issue #518: partial-refund accounting. refunded_sen is the running total
  // already returned, outstanding_sen the balance still refundable, and
  // refund_status the derived "none" | "partial" | "full" label. A partially
  // refunded sale keeps its paid/confirmed status, so the refund badge must
  // come from refund_status — reading `status` alone would show it as paid.
  refunded_sen?: number;
  outstanding_sen?: number;
  refund_status?: string;
  // Issue #171/#604: a response-mode form has no sale at all — its entries are
  // submissions. The API returns those rows in this same envelope (marked
  // sale_type "response"), carrying the submission id and the recorded answers
  // instead of money/status. Without this the table would look up undefined
  // fields and the entry would still not be readable.
  submission_id?: string;
  data?: Record<string, unknown>;
};

type SalesPage = { items: Sale[]; page: number; limit: number; total: number };
// Issue #604: `mode` is what tells the page whether the selected form collects
// payments (read /api/rest/sales) or responses (its entries live in
// submissions, which the same /api/rest/sales call now returns for such a form).
type FormRow = { id: string; slug: string; title: string; status: string; mode?: string };

const PAGE_SIZE = 15;

/* an uncaptured skip_capture authorization (issue #12) — CapturedAt is only
   set once the merchant captures; released sales go cancelled. */
const isCapturePending = (s: Sale) =>
  !!s.skip_capture &&
  !s.captured_at &&
  (s.status === "pending" || s.status === "authorized" || s.authorized === true);

const STATUSES = ["pending", "paid", "confirmed", "failed", "cancelled", "refunded"] as const;

// Issue #518: a partially refunded sale keeps its paid/confirmed status, so the
// partial state is derived from the recorded refunded amount. `refund_status`
// is what the API reports.
const isPartiallyRefunded = (s: Sale) =>
  s.refund_status === "partial" ||
  (typeof s.refunded_sen === "number" &&
    s.refunded_sen > 0 &&
    typeof s.total_sen === "number" &&
    s.refunded_sen < s.total_sen);

const money = (sen: number, currency?: string) => formatMoney(sen, currency);

// The currency a row's amounts are denominated in (t_71c67471). Read from the
// SALE — the API returns the currency each sale was recorded in — never from
// the workspace's current setting, which would relabel history the moment a
// workspace switched. MYR when the row carries none.
const currencyOf = (s?: Sale) => normaliseCurrency(s?.currency);

// Issue #447: play a short audio beep when a new order/response arrives (for
// a kitchen/order screen). Uses the Web Audio API so no asset file is needed;
// a short two-tone chirp is generated on the fly. Best-effort — a browser
// that blocks audio (or an error) is silently ignored.
function playBeep() {
  try {
    const Ctx = window.AudioContext || (window as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctx) return;
    const ctx = new Ctx();
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.setValueAtTime(880, now);
    osc.frequency.setValueAtTime(1320, now + 0.12);
    gain.gain.setValueAtTime(0.0001, now);
    gain.gain.exponentialRampToValueAtTime(0.25, now + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.28);
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start(now);
    osc.stop(now + 0.3);
    osc.onended = () => ctx.close();
  } catch {
    // audio unavailable — ignore
  }
}

/* same status badge colors as the dashboard Recent sales table.
   Card t_10266084: `cancelled` and the FALLBACK both sit on `bg-slate-100`,
   where `text-slate-500` (#62748e) measures 4.349:1 — under the 4.5:1 floor for
   this 10px/12px badge. `--jf-muted-on-tint` (#5b6472) measures 5.460:1 on the
   same tint. Neither class list was visible to `mutedText.test.ts` until the
   shared source reader learned to follow a local helper call (this card). */
const saleBadge = (s: string) =>
  ({
    pending: "bg-amber-50 text-amber-700 border-amber-200",
    paid: "bg-blue-50 text-blue-700 border-blue-200",
    confirmed: "bg-emerald-50 text-emerald-700 border-emerald-200",
    failed: "bg-red-50 text-red-700 border-red-200",
    cancelled: "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200",
    refunded: "bg-orange-50 text-orange-700 border-orange-200",
  })[s] || "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200";

/* Issue #518: the partial-refund chip. A partially refunded sale keeps its
   paid/confirmed status, so it needs its own visible marker — otherwise the
   merchant reads the sale as fully paid while money has already gone back. */
const refundBadge = "bg-amber-50 text-amber-700 border-amber-300";

/* email delivery status badge colors (issue #194).
   Card t_10266084: the FALLBACK (a status the map does not name) sits on
   `bg-slate-100`, where `text-slate-500` (#62748e) measures 4.349:1 — under the
   4.5:1 floor for this 10px badge; `--jf-muted-on-tint` (#5b6472) measures
   5.460:1 on the same tint. Invisible to `mutedText.test.ts` until the shared
   source reader learned to follow a local helper call (this card). */
const emailStatusBadge = (e: string) =>
  ({
    delivered: "bg-emerald-50 text-emerald-700 border-emerald-200",
    bounce: "bg-red-50 text-red-700 border-red-200",
    complaint: "bg-orange-50 text-orange-700 border-orange-200",
    open: "bg-blue-50 text-blue-700 border-blue-200",
    click: "bg-violet-50 text-violet-700 border-violet-200",
  })[e] || "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200";

/* localized label for an email delivery event type (issue #194) */
const emailStatusLabel = (e: string, t: (k: Key) => string) =>
  ({
    delivered: t("dash.emailDelivered"),
    bounce: t("dash.emailBounced"),
    complaint: t("dash.emailComplaint"),
    open: t("dash.emailOpened"),
    click: t("dash.emailClicked"),
  })[e] || e;

function SalesPageInner() {
  const { t } = useI18n();
  // Issue #447: `workspaces` carries the id↔slug map the per-workspace sound
  // setting needs (its REST path is keyed by workspace id).
  const { anon, role, workspace, user, workspaces } = useSession();
  const router = useRouter();
  const search = useSearchParams();
  const deepForm = search.get("form") || "";

  // Issue #163: read_only members see no write actions (capture/confirm).
  // manage + admin keep full write access.
  const canWrite = role !== "read_only";

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // forms for the filter dropdown (fetched from /api/rest/forms)
  const [forms, setForms] = useState<FormRow[]>([]);

  // filters — server-driven (mirror #122 structure but paging is server-side)
  const [formFilter, setFormFilter] = useState<string>(deepForm);
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [dateFrom, setDateFrom] = useState<string>("");
  const [dateTo, setDateTo] = useState<string>("");
  const [q, setQ] = useState<string>("");
  const [page, setPage] = useState(1);

  // data
  const [rows, setRows] = useState<Sale[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");

  // Issue #447: realtime notifications. `live` reflects whether the SSE
  // stream is connected; `soundEnabled` is the per-workspace audio-beep
  // setting (stored on workspace_settings.sound_enabled, off unless the
  // merchant turned it on). When a new sale/response arrives over the stream we
  // re-fetch the list (no manual refresh) and, if sound is on, play a short beep
  // (for a kitchen/order screen).
  //
  // The setting is PER WORKSPACE and must survive a reload: a kitchen screen
  // that goes silent on reload is useless, so the toggle is seeded from the
  // stored value on mount and written back when it changes (see the two effects
  // below). It is deliberately NOT held in localStorage — a per-browser value
  // would show a different answer on the second device in the same workspace.
  const [live, setLive] = useState(false);
  const [soundEnabled, setSoundEnabled] = useState(false);

  // actions
  const [busyRef, setBusyRef] = useState<string | null>(null);
  // The page's one message state, and its TONE. `msg` carries both outcomes —
  // every action below writes a confirmation on success and a failure sentence
  // when the API refuses (the confirm / capture / release / refund / bulk-capture
  // endpoints) — so the tone has to travel WITH the text, exactly as /settings
  // does it (settings's `showMsgOk`/`showMsgFail`, card t_ad2af554). Before this,
  // a failed refund and a successful refund were the same indigo 12px sentence
  // with no fill and no icon: a merchant scanning for a failure saw nothing that
  // read as one. Never inferred from the string, and never left stale — every
  // non-empty writer sets both in one call, which the source guard
  // (src/lib/actionFailureTone.test.ts) enforces.
  const [msg, setMsg] = useState("");
  const [msgOk, setMsgOk] = useState(true);
  const showMsgOk = useCallback((text: string) => {
    setMsgOk(true);
    setMsg(text);
  }, []);
  const showMsgFail = useCallback((text: string) => {
    setMsgOk(false);
    setMsg(text);
  }, []);

  // row detail drawer
  const [detail, setDetail] = useState<Sale | null>(null);
  const drawerRef = useRef<HTMLDivElement>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // form id -> title lookup for the "form" column
  const formName = useMemo(() => {
    const m = new Map<string, string>();
    for (const f of forms) m.set(f.id, f.title);
    return m;
  }, [forms]);

  const loadForms = useCallback(async () => {
    // Issue #503: a non-ok response used to silently become an empty dropdown.
    // A 401 must not be swallowed (an expired session would leave the filter
    // empty with no signal) — 401/403 → /login. A non-auth failure just leaves
    // the dropdown empty; the sales list itself surfaces the load error.
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/forms${workspaceQuery()}`);
        return Array.isArray(d) ? (d as FormRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("forms.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    setForms(r.ok ? r.data : []);
  }, [t, goToLogin]);

  // Load forms on mount (and re-fetch when the active workspace changes, #170).
  useEffect(() => {
    void loadForms();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace]);

  // Reset pagination whenever filters change.
  useEffect(() => {
    setPage(1);
  }, [formFilter, statusFilter, dateFrom, dateTo, q]);

  const loadSales = useCallback(
    async (p: number) => {
      setChecking(true);
      setErr("");
      const qs = new URLSearchParams({ page: String(p), limit: String(PAGE_SIZE) });
      if (formFilter) qs.set("form_id", formFilter);
      // Issue #604: a response has no payment status, so a status filter would
      // silently match nothing. The control is hidden in that view; do not send
      // a stale value either (a merchant can filter by status, then switch the
      // form dropdown to a response form).
      const responseForm = isResponseForm(forms, formFilter);
      if (statusFilter && !responseForm) qs.set("status", statusFilter);
      if (dateFrom) qs.set("date_from", dateFrom);
      if (dateTo) qs.set("date_to", dateTo);
      if (q.trim()) qs.set("q", q.trim());
      const sp = workspaceQuery();
      const sep = sp ? "&" : "";
      // Issue #503: a 401 was flattened into a generic load error (and the
      // expired session then looked like an app bug). Route it through the
      // shared helper: 401/403 → /login, anything else → the visible error.
      const r = await runLoad(
        () => fetchJson<SalesPage>(`/api/rest/sales?${qs.toString()}${sep}${sp.replace("?", "")}`),
        { onUnauth: goToLogin, message: t("bld.loadSalesErr"), errorKey: (b: unknown) => translateApiErr(b, t) },
      );
      if (!r.ok) {
        setRows([]);
        setTotal(0);
        setErr(r.error);
        setChecking(false);
        return;
      }
      setRows(Array.isArray(r.data.items) ? r.data.items : []);
      setTotal(r.data.total || 0);
      setChecking(false);
    },
    [formFilter, statusFilter, dateFrom, dateTo, q, t, goToLogin, forms],
  );

  // Deep-link: when /sales?form=<id>, the form dropdown mirrors it.
  useEffect(() => {
    if (deepForm) setFormFilter(deepForm);
  }, [deepForm]);

  // Fetch whenever filters/page change (server-side paging + filtering).
  // Issue #170: `workspace` re-runs loadSales when the active workspace changes
  // so the sales list re-fetches for the new workspace without a manual refresh.
  useEffect(() => {
    void loadSales(page);
  }, [page, loadSales, workspace]);

  // Issue #447: realtime notifications — subscribe to the authenticated SSE
  // stream (proxied through /api/rest/events/stream) so the list updates live
  // when a new sale/response arrives, with no manual refresh. The stream is
  // account-scoped server-side (no cross-tenant leak). On a sale/response
  // event we re-fetch the current page; if the per-workspace sound toggle is
  // on we also play a short beep (for a kitchen/order screen).
  const pageRef = useRef(page);
  pageRef.current = page;
  const loadRef = useRef(loadSales);
  loadRef.current = loadSales;
  const soundRef = useRef(soundEnabled);
  soundRef.current = soundEnabled;
  useEffect(() => {
    if (anon) return;
    const es = new EventSource(`/api/rest/events/stream${workspaceQuery()}`);
    es.onopen = () => setLive(true);
    es.onerror = () => setLive(false);
    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as { event?: string };
        const evt = ev.event || "";
        // Only re-fetch for events that change the sales list.
        if (
          evt === "sale.paid" ||
          evt === "sale.failed" ||
          evt === "sale.refunded" ||
          evt === "subscription.charged" ||
          evt === "response.submitted"
        ) {
          void loadRef.current(pageRef.current);
          if (soundRef.current) playBeep();
        }
      } catch {
        // ignore malformed frames
      }
    };
    return () => {
      es.close();
      setLive(false);
    };
  }, [anon, workspace]);

  // Issue #447: the workspace whose settings the sound toggle reads/writes. The
  // REST settings endpoint is keyed by workspace ID while every list fetch on
  // this page is scoped by SLUG, so the id has to be resolved from the session's
  // workspace list. It is a plain string, so it is a stable effect dependency
  // (the list itself is a fresh array on some renders and would re-run an effect
  // on every render).
  const soundWorkspaceId = activeWorkspaceId(workspaces);
  // True once the stored value has been read for the current workspace (also on
  // a failed read). The toggle stays DISABLED until then, so it can never render
  // a value that is not the server's — the bug this card fixes — and it can
  // never write the default over a value it has not read yet.
  const [soundLoaded, setSoundLoaded] = useState(false);
  // True when the stored value could NOT be read. The switch then says so (via
  // the inline text below it) instead of presenting the default OFF as if it
  // were the workspace's setting — a kitchen screen whose beep is ON must not
  // silently read "off" after a failed load.
  const [soundReadFailed, setSoundReadFailed] = useState(false);
  // Monotonic write sequence: a slow failed write must not revert the state a
  // newer click already set.
  const soundWriteSeq = useRef(0);

  // Issue #447: read the workspace's STORED sound setting and seed the toggle
  // from it. Before this the toggle was a `useState(false)` that no request ever
  // touched, so enabling the beep on a kitchen screen lasted exactly until the
  // next reload — the reload is the whole point of the feature.
  useEffect(() => {
    setSoundLoaded(false);
    setSoundReadFailed(false);
    if (anon || !soundWorkspaceId) return;
    let cancelled = false;
    fetch(`/api/workspaces/${soundWorkspaceId}/settings`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (cancelled) return;
        if (!d) {
          // The read did not produce a payload, so the control must not render
          // the default as if it were the stored value: a workspace whose beep
          // is ON would show OFF, which is the bug this card is about. Say so.
          setSoundReadFailed(true);
        } else {
          setSoundEnabled(soundEnabledFromSettings(d));
        }
        setSoundLoaded(true);
      })
      .catch(() => {
        // Offline / proxy failure: same rule as above — report the failed read
        // (never a silent default) but leave the control usable, because losing
        // an unrelated request must not cost the merchant the ability to set
        // the value.
        if (!cancelled) {
          setSoundReadFailed(true);
          setSoundLoaded(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [anon, soundWorkspaceId]);

  // Issue #447: apply the toggle. The UI flips IMMEDIATELY (optimistic) and the
  // new value is written through the existing workspace-settings REST path —
  // exactly the one `sound_enabled` field, which saveWorkspaceSettings merges
  // into the stored settings, so no other setting can be clobbered by this
  // request. If the write fails the toggle is REVERTED and the failure is shown:
  // a switch that silently displays a value the server does not hold is the bug
  // being fixed.
  //
  // WHY THE FAILURE GOES TO `showMsgFail` AND NOT TO `err` (t_bfe1cda5)
  //   `err` is the page's LOAD error; both of its render sites sit inside the
  //   empty-table branch (`rows.length === 0` under `!checking`, see the tbody
  //   below) — the load error is exactly the state that leaves the table empty,
  //   so that is a correct place for it. A SAVE failure is not: this switch lives
  //   in the page HEADER, and with any row on the table — the normal state — the
  //   empty branch never renders, so a failed sound save used to reach `err` and
  //   then NOTHING on screen. Measured on the built page (card t_bfe1cda5): the
  //   switch flipped back (`aria-checked` false → false) and the failure sentence
  //   was in ZERO elements. The page's message banner (`msg` + the tone pair)
  //   renders unconditionally, so the failure lands where the merchant can see
  //   it, is announced (`role="alert"`), and wears the failure chrome.
  async function toggleSound(next: boolean) {
    const previous = soundEnabled;
    setMsg("");
    setSoundEnabled(next);
    if (!soundWorkspaceId) {
      // No workspace to write to (session still resolving): leaving the flip in
      // place would be exactly the silent lie this card is about.
      setSoundEnabled(previous);
      showMsgFail(t("sales.soundSaveFail"));
      return;
    }
    const seq = ++soundWriteSeq.current;
    let message = t("sales.soundSaveFail");
    try {
      const r = await fetch(`/api/workspaces/${soundWorkspaceId}/settings`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(soundToggleBody(next)),
      });
      if (r.ok) return;
      const d = (await r.json().catch(() => ({}))) as { error?: string };
      message = apiErr(d, message, t);
    } catch {
      // network failure — the same visible outcome as a rejected write
    }
    // A newer click owns the state now; its own write decides the outcome.
    if (seq !== soundWriteSeq.current) return;
    setSoundEnabled(previous);
    showMsgFail(message);
  }

  // Auth bounce via router (client transition), not a full reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const safePage = Math.min(page, totalPages);

  // ── actions: confirm / capture / release (reuse existing REST endpoints) ──
  async function confirmSale(ref: string) {
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/confirm${workspaceQuery()}`, {
        method: "POST",
      });
      if (r.ok) {
        showMsgOk(`${ref} ✓`);
        await loadSales(safePage);
      } else {
        showMsgFail(apiErr(await r.json().catch(() => ({})), t("sales.confirmFail"), t));
      }
    } finally {
      setBusyRef(null);
    }
  }

  async function captureSale(ref: string) {
    // The amount the merchant types is in the SALE's currency (t_71c67471), so
    // the prompt names the row's own code instead of assuming RM.
    const target = rows.find((r) => r.ref_code === ref) || (detail?.ref_code === ref ? detail : undefined);
    const cur = currencyOf(target);
    const raw = window.prompt(
      `Capture amount in ${cur} for ${ref} (empty = full authorized amount):`,
    );
    if (raw === null) return; // cancelled
    const rm = raw.trim();
    let body = "";
    if (rm !== "") {
      const rmNum = Number(rm);
      if (!Number.isFinite(rmNum) || rmNum <= 0) {
        showMsgFail(t("sales.captureInvalid"));
        return;
      }
      body = JSON.stringify({ amount_sen: Math.round(rmNum * 100) });
    }
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/capture${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        ...(body ? { body } : {}),
      });
      const d = await r.json().catch(() => ({}));
      if (r.ok) {
        showMsgOk(`${t("bld.capture")} ${ref} ✓`);
        await loadSales(safePage);
      } else {
        showMsgFail(apiErr(d, `Capture gagal ${ref}.`, t));
      }
    } finally {
      setBusyRef(null);
    }
  }

  async function releaseSale(ref: string) {
    if (!window.confirm(`Release authorization ${ref}? The buyer's card will NOT be charged.`))
      return;
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/release${workspaceQuery()}`, {
        method: "POST",
      });
      const d = await r.json().catch(() => ({}));
      if (r.ok) {
        showMsgOk(`Authorization ${ref} released. ✓`);
        await loadSales(safePage);
      } else {
        showMsgFail(apiErr(d, `Release gagal ${ref}.`, t));
      }
    } finally {
      setBusyRef(null);
    }
  }

  // Issue #407: full or partial refund against a paid/confirmed sale.
  // Issue #518: the balance (total − already refunded) is enforced server-side;
  // the prompt shows it so a merchant does not have to guess, and the API error
  // (e.g. an over-refund) is surfaced verbatim.
  async function refundSale(ref: string) {
    const target = rows.find((r) => r.ref_code === ref) || (detail?.ref_code === ref ? detail : undefined);
    const outstanding = target?.outstanding_sen ?? target?.total_sen ?? 0;
    const already = target?.refunded_sen ?? 0;
    // t_71c67471: a refund must be issued in the currency the sale was paid in
    // (the workspace's setting may have moved on since), so every figure in the
    // prompt — and the unit the typed amount is in — comes from the row.
    const cur = currencyOf(target);
    const raw = window.prompt(
      already > 0
        ? `Refund amount in ${cur} for ${ref} — remaining balance ${money(outstanding, cur)} (already refunded ${money(already, cur)}). Empty = refund the remaining balance:`
        : `Refund amount in ${cur} for ${ref} (empty = full refund):`,
    );
    if (raw === null) return; // cancelled
    const rm = raw.trim();
    let body = "";
    if (rm !== "") {
      const rmNum = Number(rm);
      if (!Number.isFinite(rmNum) || rmNum <= 0) {
        showMsgFail(t("sales.refundInvalid"));
        return;
      }
      // Client-side guard mirroring the server rule so the merchant gets an
      // immediate answer; the server is still the authority.
      if (Math.round(rmNum * 100) > outstanding) {
        showMsgFail(t("sales.refundInvalid"));
        return;
      }
      body = JSON.stringify({ amount_sen: Math.round(rmNum * 100) });
    }
    if (!window.confirm(`Refund ${ref}? This cannot be undone.`)) return;
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/refund${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        ...(body ? { body } : {}),
      });
      const d = await r.json().catch(() => ({}));
      if (r.ok) {
        showMsgOk(`${t("sales.refund")} ${ref} ✓`);
        await loadSales(safePage);
      } else {
        showMsgFail(apiErr(d, `Refund gagal ${ref}.`, t));
      }
    } finally {
      setBusyRef(null);
    }
  }

  async function captureAllPending() {
    const awaiting = rows.filter(isCapturePending);
    if (awaiting.length === 0) return;
    // t_71c67471: pending authorizations can in principle span currencies (a sale
    // keeps the currency it was recorded in). formatTotals groups per currency
    // and joins them, so the prompt never adds US$ to RM as if they were one
    // number — and a uniform list still reads exactly like one amount.
    const total = formatTotals(awaiting.map((x) => ({ sen: x.total_sen, currency: x.currency })));
    if (!window.confirm(`Capture ALL ${awaiting.length} pending authorization(s) — total ${total}?`))
      return;
    setBusyRef("__all__");
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/bulk-capture${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refs: awaiting.map((s) => s.ref_code) }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        showMsgFail(apiErr(d, t("bld.bulkCaptureFail"), t));
        return;
      }
      // Bulk capture is PARTIAL by design — the response carries how many
      // succeeded and how many did not — so a mixed outcome is reported with the
      // failure tone: some authorizations are still uncaptured and the merchant
      // has to look at each status. Only an all-captured run is a success.
      const summary =
        t("bld.bulkCaptureDone", {
          captured: String(d.captured),
          requested: String(d.requested),
          failed: String(d.failed),
        }) + (d.failed ? t("bld.bulkCaptureCheck") : " ✓");
      if (d.failed) showMsgFail(summary);
      else showMsgOk(summary);
      await loadSales(safePage);
    } finally {
      setBusyRef(null);
    }
  }

  function clearFilters() {
    setFormFilter("");
    setStatusFilter("");
    setDateFrom("");
    setDateTo("");
    setQ("");
    setPage(1);
  }

  // CSV export — same endpoint the builder uses. It must carry the SAME filters
  // the list is showing, because the button sits directly above the list: an
  // export that ignored status/date/q produced a different data set from the
  // page, and (before the server fix) an unfiltered export dropped every
  // response entry the list showed.
  const exportUrl = useMemo(() => {
    const qs = new URLSearchParams();
    if (formFilter) qs.set("form_id", formFilter);
    // Issue #604: a response has no payment status, so the control is hidden in
    // that view — do not send a stale value (mirrors loadSales). Read the form's
    // mode directly: `filteredForm` is derived further down the component and
    // this memo runs during the same render pass (a forward reference here would
    // be a temporal-dead-zone crash, not a stale value).
    if (statusFilter && !isResponseForm(forms, formFilter)) qs.set("status", statusFilter);
    if (dateFrom) qs.set("date_from", dateFrom);
    if (dateTo) qs.set("date_to", dateTo);
    if (q.trim()) qs.set("q", q.trim());
    const sp = workspaceQuery();
    const sep = sp ? "&" : "";
    return `/api/rest/sales/export.csv?${qs.toString()}${sep}${sp.replace("?", "")}`;
  }, [formFilter, statusFilter, dateFrom, dateTo, q, forms]);

  // drawer focus management (mirrors Shell #104: focus + Escape + Tab trap)
  useEffect(() => {
    if (!detail) return;
    closeBtnRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setDetail(null);
        return;
      }
      if (e.key !== "Tab") return;
      const focusables = drawerRef.current?.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), select, input, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (!focusables || focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [detail]);

  const countLabel = total > 0 ? `${total} ${t("dash.records")}` : t("sales.empty");
  const hasFilters = !!(formFilter || statusFilter || dateFrom || dateTo || q.trim());

  // Issue #604: when a response-mode form is selected, the rows are its
  // submissions. Their shape differs (no money, no status, capture/refund do
  // not apply), so the table switches to the answers view and the status filter
  // is hidden — a submission has no status column, so offering the filter would
  // silently return nothing.
  //
  // The header/caption/empty-copy flag is NOT the row-layout flag: it may fall
  // back to the selected form's `mode`, which arrives with the forms list — a
  // DIFFERENT request from the rows. Each row's layout comes from that row alone
  // (rowKind in the tbody), so a row can never be rendered by one branch and
  // then re-keyed by the other while the forms list is still in flight. That
  // ordering is exactly what left stale sales-layout <tr> nodes on the responses
  // view (2 phantom rows over the 3 real ones).
  const filteredForm = isResponseForm(forms, formFilter);
  const responsesView = responsesList(rows, filteredForm);
  // A MIXED list (both kinds on one page) is now producible: with no form
  // filter the API returns the account's sales AND its response entries. The
  // table then has to render BOTH row layouts under one header, so it uses the
  // combined column set below instead of the responses-only one; each row still
  // picks its own layout from rowKind, so no row can be rendered through the
  // wrong branch.
  const mixedView = isMixedList(rows);

  return (
    <Shell active="sales">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">
              {responsesView ? t("bld.responsesThisForm") : t("sales.title")}
            </h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">
              {responsesView
                ? t("bld.responsesSubtitle")
                : mixedView
                  ? t("sales.mixedSubtitle")
                  : t("sales.subtitle")}
            </p>
          </div>
          {/* Issue #447: realtime status + per-workspace sound toggle. The pill
              is STATUS ONLY (it lights when the SSE stream is connected); the
              switch is the stored per-workspace beep setting. */}
          <div className="flex items-center gap-2">
            <span
              className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-jf-2xs font-medium ${
                live
                  ? "border-emerald-200 bg-emerald-50 text-emerald-700"
                  : "border-slate-200 bg-slate-50 text-slate-500"
              }`}
              title={t("sales.liveHint")}
            >
              <span
                className={`h-1.5 w-1.5 rounded-full ${live ? "bg-emerald-500" : "bg-slate-400"}`}
              />
              {live ? t("sales.live") : t("sales.offline")}
            </span>
            {/* Issue #447: the tooltip is RESTORED here (card t_5ca04dc0). The
                hand-rolled pill carried a `title` and the move to the shared
                component dropped it — `label` is the accessible NAME, not a
                tooltip, so a sighted mouse user hovering the DISABLED switch lost
                the one hint that the workspace's stored value is still loading.
                The title keeps the load-dependent copy the raw markup had. */}
            <Switch
              checked={soundEnabled}
              onChange={() => void toggleSound(!soundEnabled)}
              disabled={!soundLoaded}
              label={soundLoaded ? t("sales.soundLabel") : t("sales.soundLoading")}
              title={soundLoaded ? t("sales.soundLabel") : t("sales.soundLoading")}
            />
            {/* AGENTS.md documentation-parity step 4: the inline "?" link next
                to the field, pointing at the section that documents it.
                t_d768342e: that section now lives on the sales guide page.
                t_e3ebe979 (the rebase): the two sides are a UNION, not a choice
                — the branch moved the anchor to /docs/sales (where the full
                text is now) and main carries #738's AA value for the muted
                glyph. Taking either side wholesale loses the other: the
                branch's pre-#738 text-slate-500 is 4.48:1 on --jf-bg (axe
                serious), and main's /docs#realtime-notifications anchor is a
                summary, not the guide the switch is meant to open. */}
            <a
              href="/docs/sales#realtime-notifications"
              className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-slate-300 text-jf-3xs font-semibold text-[var(--jf-muted)] hover:bg-slate-50"
              aria-label={t("sales.soundDocs")}
              title={t("sales.soundDocs")}
            >
              ?
            </a>
            {/* A failed READ must not pass the default off as the stored value:
                say the setting could not be read instead of showing a state the
                workspace may not hold. */}
            {soundReadFailed && (
              <span role="status" className="text-jf-2xs text-amber-700">
                {t("sales.soundReadFail")}
              </span>
            )}
          </div>
        </div>

        {/* Filter / control bar */}
        <div className="jf-card mt-6 flex flex-wrap items-end gap-2 p-4">
          <select
            value={formFilter}
            onChange={(e) => setFormFilter(e.target.value)}
            aria-label={t("sales.filterForm")}
            className="jf-input w-56"
          >
            <option value="">{t("sales.allForms")}</option>
            {forms.map((f) => (
              <option key={f.id} value={f.id}>
                {f.title}
              </option>
            ))}
          </select>
          {/* Issue #604: a response has no payment status, so the filter is not
              rendered at all (rather than hidden) — offering a control that can
              only ever return nothing is worse than not offering it. */}
          {!responsesView && (
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              aria-label={t("sales.filterStatus")}
              className="jf-input w-40"
            >
              <option value="">{t("sales.allStatuses")}</option>
              {STATUSES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          )}
          <label className="text-xs text-[var(--jf-muted)]">
            {t("sales.dateFrom")}
            <input
              type="date"
              value={dateFrom}
              onChange={(e) => setDateFrom(e.target.value)}
              className="jf-input mt-0.5 block w-40"
            />
          </label>
          <label className="text-xs text-[var(--jf-muted)]">
            {t("sales.dateTo")}
            <input
              type="date"
              value={dateTo}
              onChange={(e) => setDateTo(e.target.value)}
              className="jf-input mt-0.5 block w-40"
            />
          </label>
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("sales.search")}
            aria-label={t("sales.search")}
            className="jf-input min-w-40 flex-1 sm:max-w-xs"
          />
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {hasFilters && (
              <button
                onClick={clearFilters}
                className="jf-btn-ghost"
                disabled={checking}
              >
                {t("sales.clearFilters")}
              </button>
            )}
            {canWrite && rows.some(isCapturePending) && (
              <button
                onClick={captureAllPending}
                disabled={busyRef !== null}
                className="rounded-md bg-[var(--jf-primary)] px-3 py-1.5 text-xs font-medium text-white hover:bg-[var(--jf-primary-hover)] disabled:cursor-not-allowed disabled:opacity-40"
              >
                {busyRef === "__all__" ? "…" : `${t("sales.captureAll")} (${rows.filter(isCapturePending).length})`}
              </button>
            )}
            <a
              href={exportUrl}
              className="jf-btn-primary px-3 py-1.5 text-xs"
              title={t("sales.exportNote")}
            >
              {t("common.exportCsv")}
            </a>
          </div>
        </div>

        {/* The page's action feedback. ONE state (`msg`) carries BOTH outcomes,
            so its TONE is painted from `msgOk` — never from the sentence. A
            failed refund and a successful refund used to be the same indigo
            12px line with no fill and no icon (card t_c88c854d). A failure is
            red-filled AND a live `role="alert"`, so it is announced as well as
            seen; a success keeps the app's plain confirmation styling. */}
        {msg && (
          <p
            data-testid="action-msg"
            data-tone={msgOk ? "success" : "failure"}
            role={msgOk ? undefined : "alert"}
            className={`mt-3 inline-block rounded-md px-2.5 py-1.5 text-xs ${
              msgOk
                ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                : "border border-red-200 bg-red-50 font-medium text-red-700"
            }`}
          >
            {msg}
          </p>
        )}

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              {responsesView ? (
                /* Issue #604: a response form's entries have no money and no
                   payment status, so the sale columns would all read "—"/RM0.00.
                   Show the recorded answers instead. */
                <tr className="border-b border-[var(--jf-border)] text-left text-jf-3xs uppercase tracking-wide text-[var(--jf-muted)]">
                  <th className="px-4 py-3 font-medium">{t("bld.responseReceived")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.buyer")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("bld.responseAnswers")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.form")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
                </tr>
              ) : mixedView ? (
                /* A MIXED list: sales and responses on one page. One header has
                   to serve both row layouts, so it is the COMBINED column set —
                   ref/type, buyer, items-or-answers, amount, status, form, date,
                   actions — and every row below emits exactly these 8 cells.
                   Without this branch the responses rendered through the 9-column
                   sales layout with an empty ref and RM0.00. */
                <tr className="border-b border-[var(--jf-border)] text-left text-jf-3xs uppercase tracking-wide text-[var(--jf-muted)]">
                  <th className="px-4 py-3 font-medium">{t("sales.type")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.buyer")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.itemsOrAnswers")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.amount")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.status")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.form")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("prod.date")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
                </tr>
              ) : (
                <tr className="border-b border-[var(--jf-border)] text-left text-jf-3xs uppercase tracking-wide text-[var(--jf-muted)]">
                  <th className="px-4 py-3 font-medium">{t("dash.ref")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.buyer")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.items")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.amount")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.status")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("dash.email")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.form")}</th>
                  <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("prod.date")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
                </tr>
              )}
            </thead>
            <tbody>
              {/* The layout is chosen PER ROW (rowKind), never from a flag that
                  can flip after the rows have already rendered: a response row
                  has no `ref_code`, so rendering it through the sales branch
                  gave every response row the SAME React key ("") and the stale
                  <tr> nodes survived the later flip to the answers view.
                  On a MIXED list each branch emits the 8 combined cells so one
                  header still lines up with both kinds. */}
              {rows.map((s) =>
                mixedView ? (
                  <tr key={rowKey(s)} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                    <td className="whitespace-nowrap px-4 py-3 font-mono text-jf-2xs">
                      {rowKind(s) === "response" ? (
                        <span className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-jf-3xs font-medium text-[var(--jf-primary-on-tint)]">
                          {t("bld.responses")}
                        </span>
                      ) : (
                        s.ref_code
                      )}
                    </td>
                    <td className="max-w-32 truncate px-2 py-3 text-xs">
                      {s.buyer || "—"}
                      {s.buyer_email && (
                        <span className="block truncate text-jf-3xs text-[var(--jf-muted)]">{s.buyer_email}</span>
                      )}
                    </td>
                    <td className="hidden max-w-48 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                      {rowKind(s) === "response"
                        ? answersPreview(s) || "—"
                        : Array.isArray(s.items) && s.items.length
                          ? s.items.map((i) => `${i.qty}× ${i.name}`).join(", ")
                          : "—"}
                    </td>
                    <td className="whitespace-nowrap px-2 py-3 text-xs font-semibold">
                      {rowKind(s) === "response" ? "—" : money(s.total_sen, s.currency)}
                    </td>
                    <td className="whitespace-nowrap px-2 py-3">
                      <span className={`rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${saleBadge(s.status)}`}>
                        {s.status}
                      </span>
                    </td>
                    <td className="hidden max-w-32 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                      {s.form_id ? formName.get(s.form_id) || "—" : "—"}
                    </td>
                    <td className="hidden whitespace-nowrap px-2 py-3 text-jf-2xs text-[var(--jf-muted)] sm:table-cell">
                      {formatDate(s.created_at, user?.timezone)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      <button
                        onClick={() => setDetail(s)}
                        className="jf-btn-ghost"
                      >
                        {t("sales.viewDetail")}
                      </button>
                    </td>
                  </tr>
                ) : rowKind(s) === "response" ? (
                  <tr
                    key={rowKey(s)}
                    className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60"
                  >
                    <td className="whitespace-nowrap px-4 py-3 text-jf-2xs text-[var(--jf-muted)]">
                      {formatDateTime(s.created_at, user?.timezone)}
                    </td>
                    <td className="max-w-32 truncate px-2 py-3 text-xs">
                      {s.buyer || "—"}
                      {s.buyer_email && (
                        <span className="block truncate text-jf-3xs text-[var(--jf-muted)]">{s.buyer_email}</span>
                      )}
                      {s.buyer_phone && (
                        <span className="block truncate text-jf-3xs text-[var(--jf-muted)]">{s.buyer_phone}</span>
                      )}
                    </td>
                    <td className="hidden max-w-64 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                      {answersPreview(s) || "—"}
                    </td>
                    <td className="hidden max-w-32 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                      {s.form_id ? formName.get(s.form_id) || "—" : "—"}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      <button
                        onClick={() => setDetail(s)}
                        className="jf-btn-ghost"
                      >
                        {t("sales.viewDetail")}
                      </button>
                    </td>
                  </tr>
                ) : (
                  <tr key={rowKey(s)} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                  <td className="whitespace-nowrap px-4 py-3 font-mono text-jf-2xs">{s.ref_code}</td>
                  <td className="max-w-32 truncate px-2 py-3 text-xs">
                    {s.buyer || "—"}
                    {s.buyer_email && (
                      <span className="block truncate text-jf-3xs text-[var(--jf-muted)]">{s.buyer_email}</span>
                    )}
                    {s.table_number && (
                      <span className="mt-0.5 inline-block rounded-full bg-indigo-50 px-1.5 py-0.5 text-jf-3xs font-semibold text-[var(--jf-primary-on-tint)]">
                        {t("sales.table")} {s.table_number}
                      </span>
                    )}
                  </td>
                  <td className="hidden max-w-40 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                    {Array.isArray(s.items) && s.items.length
                      ? s.items.map((i) => `${i.qty}× ${i.name}${i.variation_name ? ` (${i.variation_name})` : ""}`).join(", ")
                      : "—"}
                  </td>
                  <td className="whitespace-nowrap px-2 py-3 text-xs font-semibold">{money(s.total_sen, s.currency)}</td>
                  <td className="whitespace-nowrap px-2 py-3">
                    <span className={`rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${saleBadge(s.status)}`}>
                      {s.status}
                    </span>
                    {isPartiallyRefunded(s) && (
                      <span
                        className={`ml-1 inline-block rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${refundBadge}`}
                        title={`${t("sales.refundedLabel")}: ${money(s.refunded_sen || 0, s.currency)} · ${t("sales.outstanding")}: ${money(s.outstanding_sen || 0, s.currency)}`}
                      >
                        {t("sales.partiallyRefunded")}
                      </span>
                    )}
                  </td>
                  <td className="hidden whitespace-nowrap px-2 py-3 sm:table-cell">
                    {s.email_status ? (
                      <span className={`rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${emailStatusBadge(s.email_status)}`}>
                        {emailStatusLabel(s.email_status, t)}
                      </span>
                    ) : (
                      <span className="text-jf-3xs text-[var(--jf-muted)]">{t("dash.emailNotSent")}</span>
                    )}
                  </td>
                  <td className="hidden max-w-32 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                    {s.form_id ? formName.get(s.form_id) || "—" : "—"}
                  </td>
                  <td className="hidden whitespace-nowrap px-2 py-3 text-jf-2xs text-[var(--jf-muted)] sm:table-cell">
                    {formatDate(s.created_at, user?.timezone)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3">
                    <div className="flex items-center justify-end gap-1.5">
                      <button
                        onClick={() => setDetail(s)}
                        className="jf-btn-ghost"
                      >
                        {t("sales.viewDetail")}
                      </button>
                      {isCapturePending(s) ? (
                        <>
                          <button
                            onClick={() => captureSale(s.ref_code)}
                            disabled={busyRef !== null}
                            className="rounded-md bg-[var(--jf-primary)] px-2 py-1 text-jf-3xs font-medium text-white hover:bg-[var(--jf-primary-hover)] disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {busyRef === s.ref_code ? "…" : t("bld.capture")}
                          </button>
                          <button
                            onClick={() => releaseSale(s.ref_code)}
                            disabled={busyRef !== null}
                            className="rounded-md border border-[var(--jf-border)] px-2 py-1 text-jf-3xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {t("bld.release")}
                          </button>
                        </>
                      ) : canWrite && (s.status === "paid" || s.status === "confirmed") ? (
                        <button
                          onClick={() => refundSale(s.ref_code)}
                          disabled={busyRef !== null}
                          className="rounded-md border border-[var(--jf-border)] px-2 py-1 text-jf-3xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                        >
                          {busyRef === s.ref_code ? "…" : t("sales.refund")}
                        </button>
                      ) : (
                        <span className="text-jf-3xs text-[var(--jf-muted)]">—</span>
                      )}
                    </div>
                  </td>
                </tr>
                ),
              )}
              {!checking && rows.length === 0 && (
                <tr>
                  <td colSpan={responsesView ? 5 : mixedView ? 8 : 9} className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
                    {err || (responsesView ? t("bld.responsesEmpty") : t("sales.empty"))}
                    {/* Issue #503: a failed load gets an action, not a dead end. */}
                    {err && (
                      <div className="mt-2">
                        <button
                          type="button"
                          onClick={() => void loadSales(safePage)}
                          className="text-xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
                        >
                          {t("common.retry")}
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
          {checking && (
            <div className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
              {t("sales.loading")}
            </div>
          )}
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("prod.pageOf", { page: safePage, total: totalPages })} · {countLabel}
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

        {/* Row detail drawer */}
        {detail && (
          <div className="fixed inset-0 z-50 flex justify-end bg-black/30" onClick={() => setDetail(null)}>
            <div
              ref={drawerRef}
              className="h-full w-full max-w-lg overflow-y-auto bg-white p-6 shadow-xl md:max-w-xl"
              onClick={(e) => e.stopPropagation()}
              role="dialog"
              aria-label={t("sales.detail")}
            >
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">
                  {t("sales.detail")} ·{" "}
                  <span className="font-mono text-sm">
                    {rowKind(detail) === "response"
                      ? (detail.submission_id || "").slice(0, 8)
                      : detail.ref_code}
                  </span>
                </h2>
                <button
                  ref={closeBtnRef}
                  onClick={() => setDetail(null)}
                  className="text-lg text-[var(--jf-muted)]"
                  aria-label={t("sales.close")}
                >
                  ✕
                </button>
              </div>

              {rowKind(detail) === "response" ? (
                /* Issue #604: a response has no payment, so the sale drawer's
                   status chip / amounts / capture-refund actions are replaced by
                   the recorded answers — the whole point of the entry. */
                <>
                  <section className="mt-5">
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                      {t("sales.buyerInfo")}
                    </h3>
                    <dl className="mt-2 space-y-1 text-sm">
                      <div className="flex justify-between gap-4">
                        <dt className="text-[var(--jf-muted)]">{t("sales.form")}</dt>
                        <dd className="truncate">{detail.form_id ? formName.get(detail.form_id) || "—" : "—"}</dd>
                      </div>
                      <div className="flex justify-between gap-4">
                        <dt className="text-[var(--jf-muted)]">{t("bld.responseSubmittedAt")}</dt>
                        <dd>{formatDateTime(detail.created_at, user?.timezone)}</dd>
                      </div>
                    </dl>
                  </section>

                  <section className="mt-5">
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                      {t("bld.responseAnswers")}
                    </h3>
                    <dl className="mt-2 space-y-1 text-sm">
                      {Object.entries(detail.data || {}).map(([k, v]) => (
                        <div key={k} className="flex justify-between gap-4">
                          <dt className="truncate text-[var(--jf-muted)]">{k}</dt>
                          <dd className="truncate font-medium">{answerText(v) || "—"}</dd>
                        </div>
                      ))}
                    </dl>
                  </section>
                </>
              ) : (
                <>
              <div className="mt-3">
                <span className={`rounded-full border px-2.5 py-0.5 text-xs font-medium ${saleBadge(detail.status)}`}>
                  {detail.status}
                </span>
                {isCapturePending(detail) && (
                  <span className="ml-2 text-xs text-amber-600">{t("sales.unauthorized")}</span>
                )}
                {detail.authorized && detail.captured_at && (
                  <span className="ml-2 text-xs text-emerald-700">{t("sales.authorized")}</span>
                )}
              </div>

              {/* Buyer */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sales.buyerInfo")}
                </h3>
                <dl className="mt-2 space-y-1 text-sm">
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Name</dt>
                    <dd className="font-medium">{detail.buyer || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Email</dt>
                    <dd className="truncate">{detail.buyer_email || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Phone</dt>
                    <dd>{detail.buyer_phone || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("sales.form")}</dt>
                    <dd className="truncate">{detail.form_id ? formName.get(detail.form_id) || "—" : "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("sales.saleType")}</dt>
                    <dd>{detail.sale_type === "subscription" ? t("prod.subscription") : t("prod.oneTime")}</dd>
                  </div>
                  {detail.table_number && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">{t("sales.table")}</dt>
                      <dd className="font-semibold text-[var(--jf-primary)]">{detail.table_number}</dd>
                    </div>
                  )}
                  {detail.invoice_no && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">{t("sales.invoiceNo")}</dt>
                      <dd className="font-mono text-xs">{detail.invoice_no}</dd>
                    </div>
                  )}
                </dl>
              </section>

              {/* Items */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sales.items")}
                </h3>
                {Array.isArray(detail.items) && detail.items.length ? (
                  <table className="mt-2 w-full text-sm">
                    <thead>
                      <tr className="border-b border-[var(--jf-border)] text-left text-jf-3xs uppercase tracking-wide text-[var(--jf-muted)]">
                        <th className="py-1 font-medium">Item</th>
                        <th className="py-1 text-right font-medium">{t("sales.qty")}</th>
                        <th className="py-1 text-right font-medium">{t("dash.amount")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {detail.items.map((i, idx) => (
                        <tr key={idx} className="border-b border-[var(--jf-border)] last:border-0">
                          <td className="py-1.5">
                            {i.name}
                            {i.variation_name ? (
                              <span className="ml-1 text-xs text-[var(--jf-muted)]">({i.variation_name})</span>
                            ) : null}
                          </td>
                          <td className="py-1.5 text-right text-[var(--jf-muted)]">{i.qty}</td>
                          <td className="py-1.5 text-right font-medium">{money(i.price_sen * i.qty, detail.currency)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("sales.noItems")}</p>
                )}
              </section>

              {/* Totals */}
              <section className="mt-5 rounded-lg bg-slate-50 p-3 text-sm">
                <div className="flex justify-between gap-4">
                  <span className="text-[var(--jf-muted)]">{t("dash.amount")}</span>
                  <span>{money(detail.total_sen, detail.currency)}</span>
                </div>
                {typeof detail.shipping_sen === "number" && detail.shipping_sen !== 0 && (
                  <div className="mt-1 flex justify-between gap-4 text-[var(--jf-muted)]">
                    <span>{t("sales.shipping")}</span>
                    <span>{money(detail.shipping_sen, detail.currency)}</span>
                  </div>
                )}
                {/* Shipping phase 2: WHICH source priced this fee. A merchant
                    disputing a delivery charge needs to see, on the sale itself,
                    whether a courier quoted it or the workspace's own zone rate
                    applied — and, when the courier was asked and did not answer,
                    that the zone rate is what charged. Rendered only when the API
                    reports a resolved source, so a pre-phase-1 sale shows nothing. */}
                {typeof detail.shipping_sen === "number" && detail.shipping_sen !== 0 && detail.shipping_source && (
                  <p className="mt-1 text-xs text-[var(--jf-muted)]">
                    {detail.shipping_source === "courier"
                      ? `${t("sales.shipByCourier")}${detail.shipping_source_detail ? ` (${detail.shipping_source_detail})` : ""}`
                      : `${t("sales.shipByZone")}${detail.shipping_source_detail ? ` (${detail.shipping_source_detail})` : ""}`}
                    {/* Asked the courier, charged the zone rate: name the CLASS
                        of failure rather than saying "failed". */}
                    {detail.shipping_source === "zones" && detail.shipping_fallback_reason
                      ? ` — ${t("sales.shipCourierFellBack")} (${detail.shipping_fallback_reason})`
                      : ""}
                  </p>
                )}
                {typeof detail.discount_sen === "number" && detail.discount_sen !== 0 && (
                  <div className="mt-1 flex justify-between gap-4 text-[var(--jf-muted)]">
                    <span>{t("sales.discount")}</span>
                    <span>−{money(detail.discount_sen, detail.currency)}</span>
                  </div>
                )}
                <div className="mt-1 flex justify-between gap-4 font-semibold">
                  <span>{t("sales.total")}</span>
                  <span>{money(detail.total_sen, detail.currency)}</span>
                </div>
                {/* Issue #518: partial-refund accounting in the detail drawer. */}
                {(detail.refunded_sen ?? 0) > 0 && (
                  <>
                    <div className="mt-1 flex justify-between gap-4 text-amber-700">
                      <span>{t("sales.refundBalance")}</span>
                      <span>−{money(detail.refunded_sen || 0, detail.currency)}</span>
                    </div>
                    <div className="mt-1 flex justify-between gap-4 font-semibold">
                      <span>{t("sales.outstanding")}</span>
                      <span>{money(detail.outstanding_sen ?? 0, detail.currency)}</span>
                    </div>
                  </>
                )}
              </section>

              {isPartiallyRefunded(detail) && (
                <p className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800">
                  {t("sales.refundPartialHelp")}
                </p>
              )}

              {/* Payment + status */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sales.payment")}
                </h3>
                <div className="mt-2 space-y-1 text-sm">
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("sales.method")}</dt>
                    <dd>{detail.payment_method || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("dash.ref")}</dt>
                    <dd className="font-mono text-xs">{detail.ref_code}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Created</dt>
                    <dd>{formatDateTime(detail.created_at, user?.timezone)}</dd>
                  </div>
                  {detail.captured_at && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">Captured</dt>
                      <dd>{formatDateTime(detail.captured_at, user?.timezone)}</dd>
                    </div>
                  )}
                </div>
              </section>

              {/* Email delivery timeline (issue #194) */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("del.timeline")}
                </h3>
                {detail.email_status ? (
                  <div className="mt-2 space-y-1 text-sm">
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">{t("dash.email")}</dt>
                      <dd>
                        <span className={`rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${emailStatusBadge(detail.email_status)}`}>
                          {emailStatusLabel(detail.email_status, t)}
                        </span>
                      </dd>
                    </div>
                    <p className="text-xs text-[var(--jf-muted)]">{t("dash.emailStatusNote")}</p>
                  </div>
                ) : (
                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("dash.emailNotSent")}</p>
                )}
              </section>
                </>
              )}

              {/* Actions — issue #604: these are payment actions (capture /
                  release / confirm / refund), so they only render for a sale. A
                  response has none, and its drawer already closed above. */}
              {rowKind(detail) !== "response" && (
              <div className="mt-6 flex justify-end gap-2 border-t border-[var(--jf-border)] pt-4">
                {canWrite && isCapturePending(detail) && (
                  <>
                    <button
                      onClick={() => releaseSale(detail.ref_code)}
                      disabled={busyRef !== null}
                      className="jf-btn-ghost"
                    >
                      {t("bld.release")}
                    </button>
                    <button
                      onClick={() => captureSale(detail.ref_code)}
                      disabled={busyRef !== null}
                      className="jf-btn-primary disabled:opacity-40"
                    >
                      {busyRef === detail.ref_code ? "…" : t("bld.capture")}
                    </button>
                  </>
                )}
                {canWrite && detail.status !== "confirmed" && (
                  <button
                    onClick={() => confirmSale(detail.ref_code)}
                    disabled={busyRef !== null || isCapturePending(detail)}
                    className="jf-btn-ghost"
                  >
                    {t("sales.confirmSale")}
                  </button>
                )}
                {canWrite && (detail.status === "paid" || detail.status === "confirmed") && (
                  <button
                    onClick={() => refundSale(detail.ref_code)}
                    disabled={busyRef !== null}
                    className="jf-btn-ghost"
                  >
                    {busyRef === detail.ref_code ? "…" : t("sales.refund")}
                  </button>
                )}
              </div>
              )}
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}

export default function SalesPage() {
  return (
    <Suspense fallback={<Shell active="sales"><div className="px-6 py-8 text-sm text-[var(--jf-muted)]">…</div></Shell>}>
      <SalesPageInner />
    </Suspense>
  );
}
