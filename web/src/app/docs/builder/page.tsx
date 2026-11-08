import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import Link from "next/link";

// AGENTS.md DOCUMENTATION PARITY rule: #652 changed how a block is edited on a
// phone (the Settings tab is gone; a tap on the block opens the panel) and
// replaced the card's four buttons with ↑↓ + one ⋯ menu. Docs that described
// the old flow would be wrong, so the flow is documented here and the builder
// links to this page with an inline "?" (rule 4).
export const metadata: Metadata = docsMetadata({
  title: "JomForm Docs — the form builder",
  description:
    "How the JomForm form builder works: the field palette, the canvas, editing a block's settings on desktop and on a phone, and the per-block actions.",
  path: "/docs/builder",
});

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
          and a rule shows or hides a field depending on an earlier answer. On an option or shipping
          block each choice carries its own price, and that price is exactly what the buyer is
          charged for the choice they pick — so a choice left without a price blocks the save
          instead of quietly costing nothing (enter 0 for a free choice).
        </li>
      </ul>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">
        When a text block holds a sentence
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A text block is a short-answer field, so whatever you put in its label becomes the label of
        an empty input on the published form. Forms built under an older version of JomForm stored
        their introduction sentence in that block, which is how a live page ended up reading
        &quot;Register for our event — cuma rsvp ja&quot; above an empty box instead of showing it as
        an introduction.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The builder now warns you where the block is edited: when a text block&apos;s label reads like
        a sentence, a notice appears under the label and offers one action, Treat as intro line. It
        turns the block into an introduction (a section, in the palette) and keeps the block&apos;s
        id and its text exactly as you wrote them, so the sentence is displayed above the fields
        rather than used as a label. Nothing is changed on its own — short labels such as Abc or
        Nama anda never trigger the notice, and it is only ever your click that converts a block.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Changed your mind? The same panel then offers Undo — make it a field again, which restores
        the exact block you had: the same id, the same text and the same settings. The undo is
        offered for the block you just converted, in the session you converted it; once the change
        is saved and the page reloaded, the introduction is simply the block you chose and can be
        edited like any other.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">
        When a rule&apos;s field no longer exists
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A conditional rule names the field it watches by that field&apos;s internal id. Some older
        versions of the builder gave every block a new id each time a form was opened, so a form
        saved by one of them can still carry a rule that names an id no block has any more. The
        rule picker used to show such a block as if it had no rule at all — the opposite of what
        buyers got: the server still evaluated the rule, it never matched, and the block stayed
        hidden on the live page with every answer to it silently thrown away.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The builder now shows it instead. The block&apos;s card on the canvas carries a
        &quot;rule broken&quot; label, and its settings panel lists the unknown id as Not available with
        a notice saying the rule never matches and that answers to the field are discarded. Nothing
        is changed on your behalf: pick the field the rule should watch, or choose Always show to
        remove the rule. Opening a form and publishing it does not rewrite a rule you have not
        edited — the broken rule is saved as it was, so you can decide what it should do.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The same label appears on a rule that came from a template. A template stores its blocks and
        their rules exactly as it was saved, so a template written by an older version can bring a
        rule naming a field the new form does not have — the notice is there for you to point it at
        a real field, or to clear it.
      </p>

      <h2 id="url-fields" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        What a website field accepts
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A website block is the browser&apos;s own URL input, so it accepts any absolute URL —{" "}
        <span className="font-mono text-xs">https://…</span>,{" "}
        <span className="font-mono text-xs">ftp://…</span>,{" "}
        <span className="font-mono text-xs">mailto:…</span> — and refuses what is not a URL at all,
        such as <span className="font-mono text-xs">yourname.com</span> with no scheme. JomForm
        applies the same rule again when the form is submitted, so the buyer is not told one thing
        by the page and another by the server when they press send — with two narrow exceptions,
        both in the strict direction, where the server refuses a value the page accepted:
      </p>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>A script-bearing scheme</strong> —{" "}
          <span className="font-mono text-xs">javascript:</span>, a{" "}
          <span className="font-mono text-xs">data:</span> URI or{" "}
          <span className="font-mono text-xs">vbscript:</span> — is refused even though the browser
          accepts the string, because the answer is stored and read back later.
        </li>
        <li>
          <strong>A network URL broken in shape</strong> — a missing second slash (
          <span className="font-mono text-xs">http:/example.com</span>) or a space in the host (
          <span className="font-mono text-xs">http://exa mple.com</span>) — is refused even though
          the browser&apos;s lenient parser accepts the string. The server reads the first as an
          address with no host and cannot parse the second at all, so it answers with a corrective
          message instead of storing a link it cannot vouch for.
        </li>
      </ul>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Apart from those two, what the page accepts is what the server accepts —{" "}
        <span className="font-mono text-xs">ftp://</span>,{" "}
        <span className="font-mono text-xs">ws://</span>,{" "}
        <span className="font-mono text-xs">tel:</span>,{" "}
        <span className="font-mono text-xs">mailto:</span> and{" "}
        <span className="font-mono text-xs">file:</span> values are stored exactly as typed. A
        custom-HTML response form&apos;s field registered as{" "}
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
