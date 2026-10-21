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

      <h2 className="mt-8 text-xl font-semibold text-slate-900">What each setting does</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Every one of these reaches the published form. Where a setting appears depends on the field
        type, and each one shows up like this:
      </p>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>Label / text</strong> — the field&apos;s own label. The text block is a
          single-line answer field: its label is the text you type, and it is the palette&apos;s
          only short-answer field.
        </li>
        <li>
          <strong>Description</strong> — helper text printed under the field, on every block type.
        </li>
        <li>
          <strong>Placeholder</strong> — the grey hint inside the field, for text, note, number,
          email, phone, address, website, captcha, list and repeater fields. A date or time field
          has no placeholder: the picker prints its own format hint instead.
        </li>
        <li>
          <strong>Max length</strong> — how many characters a text or note field accepts.
        </li>
        <li>
          <strong>Default value</strong> — the value a text, note, quantity or hidden field starts
          with.
        </li>
        <li>
          <strong>CSS class</strong> — your own class name on the field&apos;s control (on the
          block&apos;s own element for text, section, HTML, image and divider blocks), so a custom
          CSS rule can target it.
        </li>
        <li>
          <strong>Inline style</strong> — safe CSS only (under Advanced; no background images or
          external URLs).
        </li>
        <li>
          <strong>Required message</strong> — the words a buyer reads when a required field is left
          empty, in place of the browser&apos;s own wording.
        </li>
        <li>
          <strong>Validation pattern</strong> — a regular expression the buyer&apos;s answer must
          match, for text, email, phone and website fields. Browsers ignore a pattern on a number
          or note field, so it is not offered there.
        </li>
        <li>
          <strong>Format</strong> — on a date or time block: whether the buyer gets a date picker, a
          date-and-time picker or a time picker.
        </li>
        <li>
          <strong>Choices</strong>, the <strong>product</strong> picker and{" "}
          <strong>conditional logic</strong> — as before: a dropdown, radio, image-choice, option or
          shipping block edits its own list of choices, a product block picks the product it sells,
          and a rule shows or hides a field depending on an earlier answer.
        </li>
      </ul>

      <h2 id="url-fields" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        What a website field accepts
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A website block is the browser&apos;s own URL input, so it accepts any absolute URL —{" "}
        <span className="font-mono text-xs">https://…</span>,{" "}
        <span className="font-mono text-xs">ftp://…</span>,{" "}
        <span className="font-mono text-xs">mailto:…</span> — and refuses only what is not a URL at
        all, such as <span className="font-mono text-xs">yourname.com</span> with no scheme.
        JomForm applies the same rule again when the form is submitted, so a buyer is never told one
        thing by the page and another by the server when they press send. There is one deliberate
        exception: a script-bearing scheme (<span className="font-mono text-xs">javascript:</span>,{" "}
        <span className="font-mono text-xs">data:</span>,{" "}
        <span className="font-mono text-xs">vbscript:</span>) is refused even though the browser
        accepts the string, because the answer is stored and read back later. A custom-HTML
        response form&apos;s field registered as{" "}
        <span className="font-mono text-xs">type:&quot;url&quot;</span> follows exactly the same
        rule.
      </p>

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
