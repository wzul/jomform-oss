"use client";

// Issue #151: dedicated product editor page (replaces the inline add row and
// the edit drawer). Supports Simple / Variable products. A Simple product uses
// product-level price/stock/images; a Variable product carries per-variation
// name/price/stock/image (reusing the #61 image upload). Mobile-friendly.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";

export type Variation = {
  id?: string;
  name: string;
  price_sen: number;
  stock?: number | null;
  images?: string[];
  sort_order?: number;
};

export type ProductForm = {
  id?: string;
  name: string;
  description: string;
  price_sen: number;
  images: string[];
  type: string; // one_time | subscription
  billing_period: string; // month | year
  trial_days: number;
  stock?: number | null;
  active: boolean;
  variations: Variation[];
  // Issue #152: subscription billing count — null = infinite, N = fixed charges.
  subscription_charges?: number | null;
};

const emptyForm = (): ProductForm => ({
  name: "",
  description: "",
  price_sen: 0,
  images: [],
  type: "one_time",
  billing_period: "month",
  trial_days: 0,
  stock: null,
  active: true,
  variations: [],
  subscription_charges: null,
});

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

export default function ProductEditor({ productId }: { productId?: string }) {
  const { t } = useI18n();
  const { anon } = useSession();
  const router = useRouter();
  const isNew = !productId;

  const [form, setForm] = useState<ProductForm>(emptyForm);
  const [loading, setLoading] = useState(!isNew);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [savedFlash, setSavedFlash] = useState("");
  const [uploadingIdx, setUploadingIdx] = useState<number | null>(null);

  // product type: simple (zero variations) vs variable (>=1)
  const isVariable = form.variations.length > 0;

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // Load the product (edit mode) with its variations for the editor.
  useEffect(() => {
    if (isNew) return;
    let cancelled = false;
    (async () => {
      try {
        const r = await fetch(`/api/rest/products/${productId}`);
        if (!r.ok) {
          if (!cancelled) setError(t("prod.notFound"));
          return;
        }
        const p = await r.json();
        if (cancelled) return;
        setForm({
          id: p.id,
          name: p.name || "",
          description: p.description || "",
          price_sen: p.price_sen || 0,
          images: Array.isArray(p.images) ? p.images : [],
          type: p.type || "one_time",
          billing_period: p.billing_period || "month",
          trial_days: p.trial_days || 0,
          stock: typeof p.stock === "number" ? p.stock : null,
          active: p.active !== false,
          subscription_charges: typeof p.subscription_charges === "number" ? p.subscription_charges : null,
          variations: Array.isArray(p.variations)
            ? p.variations.map((v: Variation) => ({
                id: v.id,
                name: v.name || "",
                price_sen: v.price_sen || 0,
                stock: typeof v.stock === "number" ? v.stock : null,
                images: Array.isArray(v.images) ? v.images : [],
                sort_order: v.sort_order || 0,
              }))
            : [],
        });
      } catch {
        if (!cancelled) setError(t("prod.loadFail"));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [productId, isNew]);

  // ── image upload (reuse #61 upload endpoint) ─────────────────────────────
  async function uploadImage(file: File, variationIdx: number | null) {
    if (!file) return;
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setError(t("prod.uploadFail"));
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setError(t("prod.uploadFail"));
      return;
    }
    setUploadingIdx(variationIdx);
    setError("");
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/image", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.url) {
        setError(t("prod.uploadFail"));
        return;
      }
      if (variationIdx === null) {
        // product-level image (simple)
        setForm((f) => ({ ...f, images: [...f.images, d.url] }));
      } else {
        // per-variation image
        setForm((f) => {
          const variations = f.variations.map((v, i) =>
            i === variationIdx ? { ...v, images: [...(v.images || []), d.url] } : v
          );
          return { ...f, variations };
        });
      }
    } finally {
      setUploadingIdx(null);
    }
  }

  function removeImage(variationIdx: number | null, imgIdx: number) {
    if (variationIdx === null) {
      setForm((f) => ({ ...f, images: f.images.filter((_, k) => k !== imgIdx) }));
    } else {
      setForm((f) => {
        const variations = f.variations.map((v, i) =>
          i === variationIdx
            ? { ...v, images: (v.images || []).filter((_, k) => k !== imgIdx) }
            : v
        );
        return { ...f, variations };
      });
    }
  }

  // ── variations editor ────────────────────────────────────────────────────
  function addVariation() {
    setForm((f) => ({
      ...f,
      variations: [
        ...f.variations,
        { name: "", price_sen: 0, stock: null, images: [], sort_order: f.variations.length },
      ],
    }));
  }

  function updateVariation(idx: number, patch: Partial<Variation>) {
    setForm((f) => ({
      ...f,
      variations: f.variations.map((v, i) => (i === idx ? { ...v, ...patch } : v)),
    }));
  }

  function removeVariation(idx: number) {
    setForm((f) => ({
      ...f,
      variations: f.variations.filter((_, i) => i !== idx),
    }));
  }

  function moveVariation(idx: number, dir: -1 | 1) {
    setForm((f) => {
      const variations = [...f.variations];
      const j = idx + dir;
      if (j < 0 || j >= variations.length) return f;
      [variations[idx], variations[j]] = [variations[j], variations[idx]];
      return { ...f, variations };
    });
  }

  // ── save ─────────────────────────────────────────────────────────────────
  const canSave = useMemo(() => {
    if (!form.name.trim()) return false;
    if (isVariable) {
      return form.variations.every((v) => v.name.trim() && v.price_sen >= 0);
    }
    return form.price_sen >= 0;
  }, [form, isVariable]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!canSave || saving) return;
    setSaving(true);
    setError("");
    setSavedFlash("");
    const body: Record<string, unknown> = {
      name: form.name.trim(),
      description: form.description,
      type: form.type,
      billing_period: form.type === "subscription" ? form.billing_period : undefined,
      trial_days: form.type === "subscription" ? Math.max(0, form.trial_days) : undefined,
      subscription_charges:
        form.type === "subscription" && typeof form.subscription_charges === "number"
          ? Math.max(1, form.subscription_charges)
          : null,
      active: form.active,
    };
    if (isVariable) {
      // Variable: per-variation price/stock/images; product-level ignored.
      body.variations = form.variations.map((v, i) => ({
        name: v.name.trim(),
        price_sen: Math.round(parseFloat(String(v.price_sen)) * 100),
        stock: typeof v.stock === "number" ? v.stock : null,
        images: v.images || [],
        sort_order: i,
      }));
    } else {
      // Simple: product-level price/stock/images.
      body.price_sen = Math.round(parseFloat(String(form.price_sen)) * 100);
      body.stock = typeof form.stock === "number" ? form.stock : null;
      body.images = form.images;
      body.variations = [];
    }
    try {
      const url = isNew ? "/api/rest/products" : `/api/rest/products/${productId}`;
      const r = await fetch(url, {
        method: isNew ? "POST" : "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setError(d.error || t("prod.saveFail"));
        return;
      }
      const saved = await r.json().catch(() => ({}));
      setSavedFlash(t("prod.saved"));
      if (isNew && saved.id) {
        // Navigate to the edit page so the user can continue editing.
        router.replace(`/products/${saved.id}/edit`);
        return;
      }
    } finally {
      setSaving(false);
    }
  }

  if (loading) {
    return (
      <Shell active="products">
        <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>
      </Shell>
    );
  }

  const priceInput = (value: number, onChange: (n: number) => void) => (
    <input
      type="number"
      step="0.01"
      min="0"
      value={value === 0 ? "" : (value / 100).toFixed(2)}
      onChange={(e) => onChange(Math.round(parseFloat(e.target.value || "0") * 100))}
      placeholder="0.00"
      className="jf-input mt-1"
    />
  );

  const stockInput = (value: number | null | undefined, onChange: (n: number | null) => void) => (
    <input
      type="number"
      min="0"
      value={typeof value === "number" ? String(value) : ""}
      onChange={(e) => onChange(e.target.value === "" ? null : Math.max(0, parseInt(e.target.value, 10)))}
      placeholder="∞"
      className="jf-input mt-1"
    />
  );

  const imagePicker = (variationIdx: number | null, images: string[]) => (
    <div>
      <div className="flex flex-wrap gap-3">
        {images.map((url, i) => (
          <div key={`${i}-${url}`} className="group relative">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={url}
              alt={t("prod.images")}
              className="h-20 w-20 rounded-lg border border-[var(--jf-border)] object-cover"
            />
            <button
              type="button"
              onClick={() => removeImage(variationIdx, i)}
              className="absolute bottom-1 left-1/2 -translate-x-1/2 rounded bg-black/60 px-2 py-0.5 text-[10px] text-white opacity-0 transition group-hover:opacity-100"
            >
              {t("prod.removeImage")}
            </button>
          </div>
        ))}
      </div>
      <label className="mt-2 inline-flex cursor-pointer items-center gap-2 rounded-lg border border-dashed border-[var(--jf-border)] px-3 py-1.5 text-xs text-[var(--jf-muted)] hover:border-[var(--jf-primary)] hover:text-[var(--jf-primary)]">
        {uploadingIdx === variationIdx ? t("prod.uploading") : t("prod.addImage")}
        <input
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          className="hidden"
          disabled={uploadingIdx !== null}
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = "";
            if (f) uploadImage(f, variationIdx);
          }}
        />
      </label>
    </div>
  );

  return (
    <Shell active="products">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <button
              onClick={() => router.push("/products")}
              className="text-xs text-[var(--jf-muted)] hover:text-[var(--jf-primary)]"
            >
              {t("prod.backToProducts")}
            </button>
            <h1 className="mt-1 text-xl font-semibold tracking-tight">
              {isNew ? t("prod.newTitle") : t("prod.editTitle")}
            </h1>
          </div>
        </div>

        {savedFlash && (
          <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700">{savedFlash}</p>
        )}
        {error && <p className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">{error}</p>}

        <form onSubmit={save} className="jf-card mt-6 max-w-2xl space-y-5 p-6">
          {/* name + description */}
          <label className="block text-sm font-medium">
            {t("prod.name")}
            <input
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              required
              className="jf-input mt-1"
            />
          </label>
          <label className="block text-sm font-medium">
            {t("prod.description")}
            <textarea
              value={form.description}
              onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              rows={3}
              placeholder={t("prod.descriptionPh")}
              className="jf-input mt-1 resize-y"
            />
          </label>

          {/* type: one_time | subscription */}
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="block text-sm font-medium">
              {t("prod.type")}
              <select
                value={form.type}
                onChange={(e) => setForm((f) => ({ ...f, type: e.target.value }))}
                className="jf-input mt-1"
              >
                <option value="one_time">{t("prod.oneTime")}</option>
                <option value="subscription">{t("prod.subscription")}</option>
              </select>
            </label>
            {form.type === "subscription" && (
              <>
                <label className="block text-sm font-medium">
                  {t("prod.billingPeriod")}
                  <select
                    value={form.billing_period}
                    onChange={(e) => setForm((f) => ({ ...f, billing_period: e.target.value }))}
                    className="jf-input mt-1"
                  >
                    <option value="month">{t("prod.monthly")}</option>
                    <option value="year">{t("prod.yearly")}</option>
                  </select>
                </label>
                <label className="block text-sm font-medium">
                  {t("prod.trial")}
                  <input
                    value={String(form.trial_days)}
                    onChange={(e) => setForm((f) => ({ ...f, trial_days: Math.max(0, parseInt(e.target.value || "0", 10)) }))}
                    type="number"
                    min="0"
                    step="1"
                    className="jf-input mt-1"
                  />
                  <span className="mt-1 block text-xs text-[var(--jf-muted)]">{t("prod.trialHint")}</span>
                </label>
                <label className="block text-sm font-medium">
                  {t("prod.billingCount")}
                  <select
                    value={typeof form.subscription_charges === "number" ? "fixed" : "infinite"}
                    onChange={(e) =>
                      setForm((f) => ({
                        ...f,
                        subscription_charges: e.target.value === "fixed" ? 1 : null,
                      }))
                    }
                    className="jf-input mt-1"
                  >
                    <option value="infinite">{t("prod.chargesInfinite")}</option>
                    <option value="fixed">{t("prod.chargesFixed")}</option>
                  </select>
                </label>
                {typeof form.subscription_charges === "number" && (
                  <label className="block text-sm font-medium">
                    {t("prod.chargesN")}
                    <input
                      value={String(form.subscription_charges)}
                      onChange={(e) =>
                        setForm((f) => ({
                          ...f,
                          subscription_charges: Math.max(1, parseInt(e.target.value || "1", 10)),
                        }))
                      }
                      type="number"
                      min="1"
                      step="1"
                      className="jf-input mt-1"
                    />
                  </label>
                )}
              </>
            )}
          </div>

          {/* Simple / Variable type */}
          <div className="rounded-lg border border-[var(--jf-border)] p-4">
            <div className="text-sm font-medium">{t("prod.productType")}</div>
            <div className="mt-2 flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => setForm((f) => ({ ...f, variations: [] }))}
                className={`rounded-lg border px-4 py-2 text-sm font-medium ${
                  !isVariable
                    ? "border-[var(--jf-primary)] bg-indigo-50 text-[var(--jf-primary)]"
                    : "border-[var(--jf-border)] text-[var(--jf-muted)] hover:border-[var(--jf-primary)]"
                }`}
              >
                {t("prod.simple")}
              </button>
              <button
                type="button"
                onClick={() =>
                  setForm((f) =>
                    f.variations.length === 0
                      ? { ...f, variations: [{ name: "", price_sen: 0, stock: null, images: [], sort_order: 0 }] }
                      : f
                  )
                }
                className={`rounded-lg border px-4 py-2 text-sm font-medium ${
                  isVariable
                    ? "border-[var(--jf-primary)] bg-indigo-50 text-[var(--jf-primary)]"
                    : "border-[var(--jf-border)] text-[var(--jf-muted)] hover:border-[var(--jf-primary)]"
                }`}
              >
                {t("prod.variable")}
              </button>
            </div>
            <p className="mt-2 text-xs text-[var(--jf-muted)]">
              {isVariable ? t("prod.variableHint") : t("prod.simpleHint")}
            </p>
          </div>

          {/* Simple: product-level price/stock/images */}
          {!isVariable && (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <label className="block text-sm font-medium">
                  {t("prod.simplePrice")}
                  {priceInput(form.price_sen, (n) => setForm((f) => ({ ...f, price_sen: n })))}
                </label>
                <label className="block text-sm font-medium">
                  {t("prod.simpleStock")}
                  {stockInput(form.stock, (n) => setForm((f) => ({ ...f, stock: n })))}
                  <span className="mt-1 block text-xs text-[var(--jf-muted)]">{t("prod.noStockNote")}</span>
                </label>
              </div>
              <div className="block text-sm font-medium">
                <div className="mb-2">{t("prod.simpleImages")}</div>
                {imagePicker(null, form.images)}
              </div>
            </>
          )}

          {/* Variable: variations editor */}
          {isVariable && (
            <div>
              <div className="mb-2 flex items-center justify-between">
                <div className="text-sm font-medium">
                  {t("prod.variations")}{" "}
                  <span className="text-xs font-normal text-[var(--jf-muted)]">
                    {t("prod.variationCount", { n: form.variations.length })}
                  </span>
                </div>
                <button
                  type="button"
                  onClick={addVariation}
                  className="jf-btn-ghost px-3 py-1.5 text-xs"
                >
                  + {t("prod.addVariation")}
                </button>
              </div>
              <p className="mb-3 text-xs text-[var(--jf-muted)]">{t("prod.variationTypeNote")}</p>
              <div className="space-y-3">
                {form.variations.map((v, idx) => (
                  <div key={idx} className="rounded-lg border border-[var(--jf-border)] p-3">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                        {t("prod.variationCount", { n: idx + 1 })}
                      </span>
                      <div className="flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => moveVariation(idx, -1)}
                          disabled={idx === 0}
                          className="rounded-full bg-white px-1.5 text-xs shadow ring-1 ring-slate-200 disabled:opacity-30"
                          title={t("prod.moveUp")}
                        >
                          ↑
                        </button>
                        <button
                          type="button"
                          onClick={() => moveVariation(idx, 1)}
                          disabled={idx === form.variations.length - 1}
                          className="rounded-full bg-white px-1.5 text-xs shadow ring-1 ring-slate-200 disabled:opacity-30"
                          title={t("prod.moveDown")}
                        >
                          ↓
                        </button>
                        <button
                          type="button"
                          onClick={() => removeVariation(idx)}
                          className="rounded-full bg-white px-1.5 text-xs text-rose-600 shadow ring-1 ring-slate-200"
                          title={t("prod.removeVariation")}
                        >
                          ✕
                        </button>
                      </div>
                    </div>
                    <div className="mt-2 grid gap-3 sm:grid-cols-2">
                      <label className="block text-sm font-medium">
                        {t("prod.variationName")}
                        <input
                          value={v.name}
                          onChange={(e) => updateVariation(idx, { name: e.target.value })}
                          placeholder={t("prod.variationNamePh")}
                          className="jf-input mt-1"
                        />
                      </label>
                      <label className="block text-sm font-medium">
                        {t("prod.variationPrice")}
                        {priceInput(v.price_sen, (n) => updateVariation(idx, { price_sen: n }))}
                      </label>
                      <label className="block text-sm font-medium">
                        {t("prod.variationStock")}
                        {stockInput(v.stock, (n) => updateVariation(idx, { stock: n }))}
                      </label>
                      <div className="block text-sm font-medium">
                        <div className="mb-1">{t("prod.variationImage")}</div>
                        {imagePicker(idx, v.images || [])}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* status / visibility */}
          <div className="rounded-lg border border-[var(--jf-border)] p-3">
            <div className="mb-1 text-sm font-medium">{t("prod.status")}</div>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={form.active}
                onChange={(e) => setForm((f) => ({ ...f, active: e.target.checked }))}
                className="h-4 w-4 accent-[var(--jf-primary)]"
              />
              {t("prod.active")}
            </label>
            {!form.active && <p className="mt-1 text-xs text-amber-600">{t("prod.inactiveNote")}</p>}
          </div>

          {!canSave && (
            <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700">
              {isVariable ? t("prod.variationRequired") : t("prod.name")}
            </p>
          )}

          <div className="flex items-center gap-2 pt-2">
            <button type="submit" disabled={!canSave || saving} className="jf-btn-primary disabled:opacity-50">
              {saving
                ? isNew
                  ? t("prod.creating")
                  : t("prod.saving")
                : isNew
                ? t("prod.createProduct")
                : t("prod.saveProduct")}
            </button>
            <button type="button" onClick={() => router.push("/products")} className="jf-btn-ghost">
              {t("prod.cancel")}
            </button>
          </div>
        </form>
      </div>
    </Shell>
  );
}
