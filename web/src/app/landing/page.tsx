"use client";

// Marketing landing page — what JomForm is, free forever, CHIP-powered.
const FEATURES = [
  { icon: "✏️", body: "Order form in 5 minutes — drag-and-drop products, fields, images, coupons. No code." },
  { icon: "💳", body: "Buyers pay online the moment they submit. Card, FPX, DuitNow QR, e-wallets." },
  { icon: "🤖", body: "Merchants control their store with AI agents via MCP — no dashboard needed." },
  { icon: "📊", body: "Sales analytics, CSV export, PDF receipts — free forever, no monthly fee." },
];


export default function Landing() {
  return (
    <main className="min-h-screen bg-white">
      {/* nav */}
      <nav className="flex items-center justify-between border-b border-slate-100 px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-[15px] font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <a href="/builder" className="text-sm text-slate-500 hover:text-slate-800">
          Build a form
        </a>
        <a href="/login" className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90">
          Log in
        </a>
      </nav>

      {/* hero */}
      <section className="mx-auto max-w-3xl px-6 pb-16 pt-20 text-center">
        <p className="mb-3 inline-block rounded-full border border-indigo-100 bg-indigo-50 px-3 py-1 text-xs font-medium text-[var(--jf-primary)]">
          100% free · open source · CHIP payments
        </p>
        <h1 className="text-4xl font-bold tracking-tight text-slate-900 sm:text-5xl">
          Jual secara online dengan{" "}
          <span className="text-[var(--jf-primary)]">borang tempahan</span> cantik.
        </h1>
        <p className="mx-auto mt-5 max-w-xl text-lg text-slate-500">
          Bina borang, buyer bayar secara online, anda urus jualan. Untuk penjual
          sosial Malaysia — free forever.
        </p>
        <div className="mt-8 flex items-center justify-center gap-3">
          <a href="/login" className="rounded-xl bg-[var(--jf-primary)] px-6 py-3 text-sm font-semibold text-white hover:opacity-90">
            Mula percuma →
          </a>
          <a href="#how" className="rounded-xl border border-slate-200 px-6 py-3 text-sm font-medium text-slate-600 hover:bg-slate-50">
            Macam mana ia berfungsi
          </a>
        </div>
      </section>

      {/* how it flows */}
      <section id="how" className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="grid grid-cols-1 gap-8 sm:grid-cols-3">
          {[
            ["1. Bina borang", "Tarik produk, medan nama, WhatsApp — publish satu klik."],
            ["2. Buyer bayar", "Checkout selamat oleh CHIP — kad / FPX / DuitNow QR / e-wallet."],
            ["3. Anda auto-update", "Status jualan naik sendiri, resit hantar automatik."],
          ].map(([title, body], i) => (
            <div key={title}>
              <p className="text-sm font-semibold text-[var(--jf-primary)]">{title}</p>
              <p className="mt-2 text-sm text-slate-500">{body}</p>
            </div>
          ))}
        </div>
      </section>

      {/* features */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="grid grid-cols-1 gap-8 sm:grid-cols-2">
          {FEATURES.map((f) => (
            <div key={f.body} className="flex gap-3">
              <span className="text-2xl">{f.icon}</span>
              <p className="text-sm leading-relaxed text-slate-600">{f.body}</p>
            </div>
          ))}
        </div>
      </section>

      {/* footer */}
      <footer className="border-t border-slate-100 px-6 py-8 text-center text-xs text-slate-400">
        <p>
          JomForm — open source (Apache-2.0). Made in Malaysia. Payments by{" "}
          <a href="https://chip-in.asia" className="underline hover:text-slate-600">
            CHIP
          </a>
          .
        </p>
      </footer>
    </main>
  );
}