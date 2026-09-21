import type { Metadata } from "next";
import Link from "next/link";

// AGENTS.md DOCUMENTATION PARITY rule: #652 changed how a block is edited on a
// phone (the Settings tab is gone; a tap on the block opens the panel) and
// replaced the card's four buttons with ↑↓ + one ⋯ menu. Docs that described
// the old flow would be wrong, so the flow is documented here and the builder
// links to this page with an inline "?" (rule 4).
export const metadata: Metadata = {
  title: "JomForm Docs — the form builder",
  description:
    "How the JomForm form builder works: the field palette, the canvas, editing a block's settings on desktop and on a phone, and the per-block actions.",
  alternates: { canonical: "https://jomform.com/docs/builder" },
};

export default function BuilderDocsPage() {
  return (
    <article className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">The form builder</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        The builder is where a form&apos;s fields are laid out. It is the same surface for a form and
        for one of your own templates (see{" "}
        <Link href="/docs/customize#templates" className="underline">
          templates
        </Link>
        ).
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">The three areas</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        On a desktop (≥768px) the builder is three columns: the <strong>field palette</strong> on the
        left (every field type, grouped Standard / Advanced / Pricing), the <strong>canvas</strong> in
        the middle (the fields in the order a buyer sees them), and the{" "}
        <strong>block settings</strong> panel on the right, which edits whichever block is selected.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Adding a field</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Drag a palette item onto the canvas, or tap it — tapping appends the field to the end. On a
        phone the palette lives behind the <strong>Add</strong> tab; adding a field returns you to the
        canvas with the new block highlighted, so you can add several fields in a row without
        leaving the canvas.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Editing a block&apos;s settings</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Every block has settings: its label/text, description, placeholder, CSS class, inline style,
        whether it is required, its validation message, and — for the field types that need one — a
        choices editor, a product picker or a conditional-logic rule. Which settings appear depends on
        the field type.
      </p>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>On a desktop</strong>, tap a block on the canvas and its settings appear in the
          right-hand panel.
        </li>
        <li>
          <strong>On a phone</strong>, tap a block on the canvas and the settings panel replaces the
          canvas. The row at the top of the panel — <strong>← Canvas</strong> next to the block&apos;s
          type — returns you to the canvas. The block stays highlighted after you close the panel, so
          you can see where you were, and tapping it again reopens its settings.
        </li>
        <li>
          The mobile tab bar has two tabs, <strong>Add</strong> and <strong>Canvas</strong>. There is
          no separate settings tab: editing a block is a tap on the block itself.
        </li>
      </ul>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Reordering, duplicating, removing</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Each block card carries <strong>↑</strong> and <strong>↓</strong> to move it one position (the
        arrows are disabled at the ends of the list) and a <strong>⋯</strong> button whose menu holds{" "}
        <strong>Duplicate</strong> and <strong>Remove</strong>. You can also drag a block by its card
        to drop it anywhere on the canvas. None of these controls opens the settings panel — they stop
        the tap from reaching the card, so arranging a form never swaps you into an editor by accident.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">
        The canvas preview is drawn, not interactive
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The canvas shows what a block <em>looks like</em>, so that you can arrange the form without
        being able to type into it: checkboxes and radio choices are drawn as flat grey marks, the
        product quantity is a fixed &ldquo;1&rdquo;, and the totals are placeholders. They are
        deliberately not tappable — the real controls belong to the buyer on the published form. An
        image block with no image yet shows a caption naming the setting to fill in; set the URL (or
        upload a file) in that block&apos;s settings.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Form mode and everything else</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The panel also carries the form-level <strong>mode</strong>: <em>payment</em> (the default —
        the form takes payment and its entries are sales) or <em>response</em> (no products, no
        payment; its entries are stored as responses). Everything else about a form — status,
        confirmation, restrictions, notifications, the per-form CHIP feed and the restaurant menu —
        lives in{" "}
        <strong>Form Settings</strong>, not the builder.
      </p>
    </article>
  );
}
