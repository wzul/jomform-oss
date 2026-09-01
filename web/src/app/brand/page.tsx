"use client";

// Brand assets — publicly served at https://jomform.com/brand
import Image from "next/image";
import { useEffect, useState } from "react";

const BASE = "https://jomform.com";

const ASSETS = [
  { file: "icon.svg", label: "Logo mark (SVG)", note: "Vector — use this wherever possible" },
  { file: "icon-512.png", label: "Logo mark 512×512", note: "OAuth app avatar, profile pictures" },
  { file: "icon-192.png", label: "Logo mark 192×192", note: "PWA icon, small avatars" },
  { file: "icon-64.png", label: "Logo mark 64×64", note: "Favicon-scale uses" },
  { file: "banner.svg", label: "Banner (SVG)", note: "Full lockup, vector" },
  { file: "banner-1200x360.png", label: "Banner 1200×360", note: "Open Graph / social preview" },
];

const COLORS = [
  { name: "Indigo 500", hex: "#6366f1" },
  { name: "Violet 500", hex: "#8b5cf6" },
  { name: "Indigo 600", hex: "#4f46e5" },
  { name: "Ink 900", hex: "#17181c" },
  { name: "Paper", hex: "#f7f8f8" },
];

function CopyBtn({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      onClick={() => {
        navigator.clipboard.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }}
      className="jf-btn-ghost"
    >
      {done ? "Copied ✓" : label}
    </button>
  );
}

export default function BrandPage() {
  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <h1 className="text-3xl font-bold">Brand assets</h1>
      <p className="mt-2 text-sm text-slate-500">
        Logos and colors for JomForm — free to use when referring to the product.
        Please don&apos;t modify the mark or use it as your own logo.
      </p>

      {/* Primary mark */}
      <section className="jf-card mt-8 p-6">
        <h2 className="text-lg font-semibold">Logo mark</h2>
        <div className="mt-4 flex items-center gap-8">
          <Image src="/brand/icon-512.png" alt="JomForm logo" width={160} height={160} className="rounded-2xl" />
          <div className="space-y-2 text-sm">
            <p><strong>JF</strong> monogram on a white form sheet, indigo→violet gradient tile.</p>
            <p className="text-slate-400">Rounded-square, safe area = 12% of width.</p>
          </div>
        </div>
      </section>

      <h2 className="mt-10 text-lg font-semibold">Downloads</h2>
      <ul className="mt-3 space-y-2">
        {ASSETS.map((a) => (
          <li key={a.file} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{a.label}</p>
              <p className="text-[11px] text-slate-400">{a.note}</p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <CopyBtn text={`${BASE}/brand/${a.file}`} label="Copy URL" />
              <a href={`/brand/${a.file}`} download className="jf-btn-primary">Download</a>
            </div>
          </li>
        ))}
      </ul>

      <h2 className="mt-10 text-lg font-semibold">Colors</h2>
      <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-5">
        {COLORS.map((c) => (
          <div key={c.hex} className="rounded-lg border border-[var(--jf-border)] p-3">
            <div className="h-12 w-full rounded-md" style={{ background: c.hex }} />
            <p className="mt-2 text-xs font-medium">{c.name}</p>
            <p className="font-mono text-[11px] text-slate-400">{c.hex}</p>
          </div>
        ))}
      </div>

      <h2 className="mt-10 text-lg font-semibold">Banner</h2>
      <img src="/brand/banner-1200x360.png" alt="JomForm banner" className="mt-3 w-full rounded-xl border border-[var(--jf-border)]" />

      <h2 className="mt-10 text-lg font-semibold">Usage</h2>
      <ul className="mt-3 list-disc space-y-1.5 pl-5 text-sm text-slate-600">
        <li>Minimum mark size: 32×32 px.</li>
        <li>Clear space around the mark = 12% of its width.</li>
        <li>On dark backgrounds use the white wordmark; on light backgrounds use indigo #4f46e5.</li>
        <li>Don&apos;t rotate, recolor, add effects, or rearrange the monogram.</li>
      </ul>

      <p className="mt-8 text-xs text-slate-400">
        Questions or custom sizes? Email contact@jomform.com.
      </p>
    </main>
  );
}