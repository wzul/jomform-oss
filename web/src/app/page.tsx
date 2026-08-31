"use client";

import { useEffect, useState } from "react";

type Product = {
  id: string;
  name: string;
  price_sen: number;
  type: string;
  billing_period?: string;
  active: boolean;
};

type FormRow = {
  id: string;
  slug: string;
  title: string;
  status: string;
  url: string;
  updated_at: string;
};

type Summary = {
  counts: Record<string, number>;
  revenue_sen: number;
};

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("ms-MY", { minimumFractionDigits: 2 })}`;

export default function Dashboard() {
  const [authed, setAuthed] = useState(false);
  const [checking, setChecking] = useState(true);
  const [tab, setTab] = useState<"produk" | "borang" | "keys">("produk");

  const [products, setProducts] = useState<Product[]>([]);
  const [forms, setForms] = useState<FormRow[]>([]);
  const [summary, setSummary] = useState<Summary | null>(null);

  const [pName, setPName] = useState("");
  const [pPrice, setPPrice] = useState("");
  const [pType, setPType] = useState("one_time");
  const [pPeriod, setPPeriod] = useState("month");

  const [fSlug, setFSlug] = useState("");
  const [fTitle, setFTitle] = useState("");

  const [error, setError] = useState("");
  const [newKey, setNewKey] = useState("");

  async function api<T>(path: string, init?: RequestInit): Promise<T> {
    const isForm = init?.body instanceof FormData;
    const r = await fetch(path, {
      ...init,
      headers: isForm
        ? init?.headers
        : { "Content-Type": "application/json", ...(init?.headers || {}) },
    });
    if (r.status === 401) {
      setAuthed(false);
      throw new Error("unauthorized");
    }
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`);
    return data;
  }

  async function loadAll() {
    try {
      const [p, f, s] = await Promise.all([
        api<Product[]>("/api/rest/products"),
        api<FormRow[]>("/api/rest/forms"),
        api<Summary>("/api/rest/summary"),
      ]);
      setProducts(p);
      setForms(f);
      setSummary(s);
      setAuthed(true);
    } catch {
      setAuthed(false);
    } finally {
      setChecking(false);
    }
  }

  useEffect(() => {
    loadAll();
  }, []);

  async function login(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setError("");
    const fd = new FormData(e.currentTarget);
    const r = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: fd.get("email"), password: fd.get("password") }),
    });
    if (r.ok || r.status === 401) {
      const d = await r.json().catch(() => ({}));
      if (d.next_step === "totp_required") {
        setError("Masukkan kod 2FA sebagai medan totp_code.");
        return;
      }
    }
    if (r.ok) {
      setAuthed(true);
      setChecking(true);
      await loadAll();
    } else {
      setError("Log masuk gagal. Semak emel & kata laluan.");
    }
  }

  async function register(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setError("");
    const fd = new FormData(e.currentTarget);
    const r = await fetch("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: fd.get("email"), password: fd.get("password") }),
    });
    if (r.status === 201) {
      setAuthed(true);
      setChecking(true);
      await loadAll();
    } else {
      const d = await r.json().catch(() => ({}));
      setError(d.error || "Pendaftaran gagal");
    }
  }

  async function createProduct(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api("/api/rest/products", {
        method: "POST",
        body: JSON.stringify({
          name: pName,
          price_sen: Math.round(parseFloat(pPrice) * 100),
          type: pType,
          billing_period: pType === "subscription" ? pPeriod : undefined,
        }),
      });
      setPName("");
      setPPrice("");
      await loadAll();
    } catch (err) {
      setError(String(err));
    }
  }

  async function createForm(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    try {
      await api("/api/rest/forms", {
        method: "POST",
        body: JSON.stringify({
          slug: fSlug.toLowerCase().replace(/[^a-z0-9-]/g, "-"),
          title: fTitle,
          config: { products: products.map((p) => ({ id: p.id, name: p.name, price_sen: p.price_sen })), description: fTitle },
        }),
      });
      setFSlug("");
      setFTitle("");
      await loadAll();
      setTab("borang");
    } catch (err) {
      setError(String(err));
    }
  }

  async function publish(id: string) {
    await api(`/api/rest/forms/${id}/publish`, { method: "POST" });
    await loadAll();
  }

  async function createKey() {
    setError("");
    setNewKey("");
    try {
      const d = await api<{ key: string }>("/api/rest/keys", {
        method: "POST",
        body: JSON.stringify({ name: "AI Agent Key", scopes: ["readwrite"] }),
      });
      setNewKey(d.key);
      await loadAll();
    } catch (err) {
      setError(String(err));
    }
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">
        Memuatkan…
      </main>
    );
  }

  if (!authed) {
    return (
      <main className="flex min-h-screen items-center justify-center px-6">
        <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-[#12151f] p-8">
          <h1 className="text-2xl font-bold">
            Jom<span className="text-indigo-400">Form</span>
          </h1>
          <p className="mt-1 text-sm text-slate-400">Log masuk atau daftar — borang jualan anda siap dalam 5 minit.</p>
          {error && <p className="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-400">{error}</p>}
          <form onSubmit={login} className="mt-6 space-y-3">
            <input name="email" type="email" required placeholder="emel@anda.my" className="w-full rounded-xl border border-slate-700 bg-[#0e1119] px-4 py-2.5 text-sm outline-none focus:border-indigo-500" />
            <input name="password" type="password" required placeholder="Kata laluan" className="w-full rounded-xl border border-slate-700 bg-[#0e1119] px-4 py-2.5 text-sm outline-none focus:border-indigo-500" />
            <button className="w-full rounded-xl bg-gradient-to-r from-indigo-500 to-violet-500 py-2.5 font-semibold">Log Masuk</button>
          </form>
          <form onSubmit={register} className="mt-3 space-y-3">
            <input name="email" type="email" required placeholder="Baharu? Emel untuk daftar" className="w-full rounded-xl border border-slate-700 bg-[#0e1119] px-4 py-2.5 text-sm outline-none focus:border-violet-500" />
            <input name="password" type="password" required minLength={10} placeholder="Kata laluan (min. 10 aksara)" className="w-full rounded-xl border border-slate-700 bg-[#0e1119] px-4 py-2.5 text-sm outline-none focus:border-violet-500" />
            <button className="w-full rounded-xl border border-slate-600 py-2.5 text-sm font-semibold text-slate-200">Daftar Pereka</button>
          </form>
        </div>
      </main>
    );
  }

  return (
    <main className="mx-auto min-h-screen max-w-4xl px-6 py-10">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">
            Jom<span className="text-indigo-400">Form</span>
          </h1>
          <a href="/settings" className="text-xs text-slate-500 hover:text-indigo-400">
            Tetapan →
          </a>
        </div>
        {summary && (
          <div className="text-right text-sm text-slate-400">
            Hasil: <strong className="text-emerald-400">{money(summary.revenue_sen)}</strong>
            <span className="ml-3">Menunggu: {summary.counts.pending ?? 0}</span>
          </div>
        )}
      </header>

      <nav className="mt-6 flex gap-2">
        {(["produk", "borang", "keys"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`rounded-full px-4 py-1.5 text-sm font-medium ${
              tab === t ? "bg-indigo-500 text-white" : "border border-slate-700 text-slate-300"
            }`}
          >
            {t.toUpperCase()}
          </button>
        ))}
      </nav>

      {error && <p className="mt-4 rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-400">{error}</p>}

      {tab === "produk" && (
        <section className="mt-6 space-y-4">
          <form onSubmit={createProduct} className="flex flex-wrap gap-2 rounded-2xl border border-slate-800 bg-[#12151f] p-4">
            <input value={pName} onChange={(e) => setPName(e.target.value)} required placeholder="Nama produk" className="flex-1 rounded-xl border border-slate-700 bg-[#0e1119] px-3 py-2 text-sm" />
            <input value={pPrice} onChange={(e) => setPPrice(e.target.value)} required type="number" step="0.01" min="0" placeholder="Harga RM" className="w-28 rounded-xl border border-slate-700 bg-[#0e1119] px-3 py-2 text-sm" />
            <select value={pType} onChange={(e) => setPType(e.target.value)} className="rounded-xl border border-slate-700 bg-[#0e1119] px-3 py-2 text-sm">
              <option value="one_time">One-time</option>
              <option value="subscription">Langganan</option>
            </select>
            {pType === "subscription" && (
              <select value={pPeriod} onChange={(e) => setPPeriod(e.target.value)} className="rounded-xl border border-slate-700 bg-[#0e1119] px-3 py-2 text-sm">
                <option value="month">Bulanan</option>
                <option value="year">Tahunan</option>
              </select>
            )}
            <button className="rounded-xl bg-indigo-500 px-5 py-2 text-sm font-semibold">Tambah</button>
          </form>
          <ul className="space-y-2">
            {products.map((p) => (
              <li key={p.id} className="flex items-center justify-between rounded-xl border border-slate-800 bg-[#12151f] px-4 py-3">
                <div>
                  <p className="font-medium">{p.name}</p>
                  <p className="text-xs text-slate-500">
                    {p.type === "subscription" ? `Langganan ${p.billing_period === "year" ? "tahunan" : "bulanan"}` : "One-time"}
                  </p>
                </div>
                <span className="font-semibold text-indigo-300">{money(p.price_sen)}</span>
              </li>
            ))}
            {products.length === 0 && <li className="text-sm text-slate-500">Belum ada produk. Tambah yang pertama di atas.</li>}
          </ul>
        </section>
      )}

      {tab === "borang" && (
        <section className="mt-6 space-y-4">
          <form onSubmit={createForm} className="flex flex-wrap gap-2 rounded-2xl border border-slate-800 bg-[#12151f] p-4">
            <input value={fTitle} onChange={(e) => setFTitle(e.target.value)} required placeholder="Tajuk borang (cth: Beli Biskut Raya)" className="flex-1 rounded-xl border border-slate-700 bg-[#0e1119] px-3 py-2 text-sm" />
            <input value={fSlug} onChange={(e) => setFSlug(e.target.value)} required placeholder="slug-url" className="w-40 rounded-xl border border-slate-700 bg-[#0e1119] px-3 py-2 text-sm" />
            <button className="rounded-xl bg-violet-500 px-5 py-2 text-sm font-semibold">Bina</button>
          </form>
          <ul className="space-y-2">
            {forms.map((f) => (
              <li key={f.id} className="flex items-center justify-between rounded-xl border border-slate-800 bg-[#12151f] px-4 py-3">
                <div>
                  <p className="font-medium">{f.title}</p>
                  <a href={`https://jomform.com${f.url}`} className="text-xs text-indigo-400 underline" target="_blank">
                    {f.url}
                  </a>
                </div>
                {f.status === "published" ? (
                  <span className="rounded-full bg-emerald-500/15 px-3 py-1 text-xs font-semibold text-emerald-400">LIVE</span>
                ) : (
                  <button onClick={() => publish(f.id)} className="rounded-full border border-slate-600 px-4 py-1 text-xs font-semibold">
                    Publish
                  </button>
                )}
              </li>
            ))}
            {forms.length === 0 && <li className="text-sm text-slate-500">Belum ada borang. Bina yang pertama.</li>}
          </ul>
        </section>
      )}

      {tab === "keys" && (
        <section className="mt-6 space-y-4">
          <div className="rounded-2xl border border-slate-800 bg-[#12151f] p-4">
            <p className="text-sm text-slate-300">
              Pengurusan kunci API telah berpindah ke{" "}
              <a href="/settings" className="text-indigo-400 underline">
                halaman Tetapan
              </a>
              .
            </p>
            <button
              onClick={createKey}
              className="mt-3 rounded-xl bg-emerald-500/90 px-5 py-2 text-sm font-semibold text-emerald-950"
            >
              Jana Key Baru
            </button>
            {newKey && (
              <div className="mt-3 rounded-xl border border-emerald-600/40 bg-emerald-500/10 p-3">
                <p className="text-xs text-emerald-300">Simpan sekarang — takkan dipapar lagi:</p>
                <code className="mt-1 block break-all font-mono text-sm text-emerald-200">{newKey}</code>
              </div>
            )}
          </div>
        </section>
      )}
    </main>
  );
}
