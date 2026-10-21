import type { Metadata } from "next";
import Link from "next/link";

// AGENTS.md DOCUMENTATION PARITY: the fully-custom HTML response form (issue
// #255) is reachable from the MCP surface (upsert_form takes custom_html and
// custom_fields) and had no page in the knowledge base — the only description of
// the field contract was the tool schema string in api/internal/mcpsrv/parity.go
// and Go doc comments. This page is that description, and /docs carries a
// one-sentence entry that links here.
//
// The facts below are the code's, not a wish: every claim was read out of
// internal/customform (sanitizer, type vocabulary, Collect), internal/httpapi
// render_custom.go (the iframe, its sandbox, the frame's own CSP, the submit
// bridge) and public.go's handleResponseSubmit (what is validated and stored),
// and the sanitizer claims were measured against SanitizeHTML before they were
// written down.
export const metadata: Metadata = {
  title: "JomForm Docs — custom HTML forms",
  description:
    "The fully-custom HTML response form: what the sanitizer keeps, how the field-registration contract works, the field types and what the server enforces on submit.",
  alternates: { canonical: "https://jomform.com/docs/custom-forms" },
};

export default function CustomFormsDocsPage() {
  return (
    <article className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Custom HTML forms</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A custom HTML form is a response-only form whose body is HTML supplied by an AI agent or by
        you, instead of the blocks the builder lays out. It is the custom_html section of the
        form&apos;s config: html is the markup and fields is the field-registration contract. The MCP
        tool upsert_form takes the two as top-level arguments (custom_html, custom_fields) and is the
        practical way to write one; a REST create or update can carry the same section inside a raw
        config map. There is no dashboard editor for this form type today, so the write surfaces are
        the only way in, and a form with no html in its custom_html section renders through the block
        renderer as usual. The dashboard&apos;s forms list still offers Edit on a custom form and opens
        the block editor, but that editor cannot change or remove the body: its save carries the
        custom_html section forward untouched, and the blocks it writes back are not rendered while
        the section carries html.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">
        A custom form is a response form
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The custom_html section and the form&apos;s mode are two statements of one fact, so you only
        have to write one of them. A form that carries an enabled section is a response-only form —
        it collects answers, it never takes payment — and the write surfaces store
        mode: &quot;response&quot; beside the section for you. That holds for upsert_form&apos;s
        custom_html and custom_fields arguments and for a raw config map on a REST create or update
        alike, so a raw config whose custom_html has a body and no mode key is stored as a response
        form rather than as a payment form: the buyer gets your markup, not the builder or payment
        layout. Writing mode: &quot;payment&quot; next to an enabled section does not win — the
        section is the form&apos;s body, and the payment renderer cannot paint it, so the stored mode
        becomes response.
      </p>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        The reverse direction is unaffected: a config with no section, or with an empty html, is
        stored exactly as you sent it, so setting mode is the only thing that makes an ordinary form
        a response form. A form already stored with an enabled section and no mode key (written
        before this rule, or cloned from an older template) is still treated, rendered and counted
        as a response form — the section alone decides.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">What happens to the HTML</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The markup is stored as you sent it and sanitized again on every render — the stored body is
        never trusted verbatim. The sanitizer keeps the structural and form elements a form needs
        and removes everything that could execute or reach the network:
      </p>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>Scripts and embedded content are dropped with their content.</strong> A script,
          iframe or object element does not survive the sanitizer, so nothing in the body can run
          code or load a second document.
        </li>
        <li>
          <strong>Remote resources are removed.</strong> Only relative URLs, data: URLs and mailto:
          survive, so a stylesheet, font or image hosted elsewhere cannot be loaded — an https image
          is stripped and the element is kept without its src. Inline data: images do work.
        </li>
        <li>
          <strong>Inline styles are kept for a safe allowlist</strong> — colour, background colour,
          typography, spacing, borders and layout. A url() inside a style attribute
          (background-image, for example) is removed. A style element is a different case: it is
          allowed through and its CSS is kept as written, so the property that blocks a remote fetch
          is the frame&apos;s own policy (images limited to data:), not the removal of the rule.
        </li>
        <li>
          <strong>The form tag itself is stripped.</strong> The controls survive, and the page wraps
          the frame&apos;s content in a form at load, so Enter, the required attribute, pattern and
          the browser&apos;s own type checks still apply to the buyer&apos;s answers.
        </li>
      </ul>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        The sanitized body is then written into a sandboxed iframe that has no allow-scripts flag,
        with its own policy: no default source, inline styles only, images limited to data:, and form
        submissions only to this form. The outer page owns the submit button and the hidden
        anti-spam fields; a submit inside the frame is forwarded to the same route. The merchant
        markup is therefore inert — it cannot run script, navigate the page or address another
        origin.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Register the fields</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        custom_fields is an array with one object per answer, and each object has five properties:
      </p>
      <pre
        className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"
        tabIndex={0}
      >
        <code>{`"custom_fields": [
  { "field_id": "nama", "type": "text",   "label": "Nama",   "required": true },
  { "field_id": "emel", "type": "email",  "label": "E-mel",  "required": true },
  { "field_id": "nota", "type": "textarea", "label": "Nota", "required": false,
    "validation": "optional - what the answer should look like" }
]`}</code>
      </pre>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>field_id is the name.</strong> Every control in the markup must carry that exact
          value in its name attribute — that is how a submitted value is matched to the contract.
          Each id must be non-empty and unique; a save with an empty id, a duplicate id or a type
          outside the vocabulary is refused and says which one (field_id wajib, field_id duplikat,
          jenis medan tidak sah).
        </li>
        <li>
          <strong>label is what an error names.</strong> The server&apos;s validation message quotes
          the label; with no label it quotes the field_id instead.
        </li>
        <li>
          <strong>required is enforced twice</strong> — by the browser through the control&apos;s
          attributes, and by the server on the submitted answers, so a direct POST that omits the
          field is refused with 400 rather than stored.
        </li>
        <li>
          <strong>validation is a hint</strong>, kept for whoever maintains the form. Nothing on the
          page or in the server compiles it into a rule, so it cannot refuse an answer the live page
          accepted.
        </li>
      </ul>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A required field also has to exist in the markup. The save is refused when a required
        field_id has no control the page can read — an input, select or textarea carrying that name,
        or a button carrying a non-empty value — because the buyer&apos;s page can only submit
        controls it can find, and a contract with no control behind it would make every submit fail.
        The refusal names the field (medan wajib tiada dalam HTML, then the missing name attributes)
        and comes back from the form and template write paths as well as from upsert_form,
        create_template and update_template. Optional fields need no control at all.
      </p>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        On submit the answers are filtered to the contract: a control whose name is not registered is
        dropped before the submission is stored, so an unregistered name never becomes a field of its
        own. The buyer&apos;s identity is written outside that filter. It is recorded in the same
        stored response as full_name, email and phone, read from the first
        input[type=text], input[type=email] and input[type=tel] the markup carries — registered or
        not. So a text control that is not registered still reaches storage: its value is stored as
        full_name, the field the responses list shows as the buyer. The anti-spam fields the page
        itself emits are consumed and left out too.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Field types</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The type vocabulary is the browser&apos;s input types plus three shapes for longer or
        structured answers:
      </p>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>text, email, tel, number, date, time, url, password</strong> — single-line answers.
          JomForm does not draw the control: your markup carries an input of the matching type, and
          the type name is what the contract records for it.
        </li>
        <li>
          <strong>textarea</strong> — a multi-line answer.
        </li>
        <li>
          <strong>select, radio, checkbox</strong> — the choice controls. A select stores the chosen
          option&apos;s value, a radio stores the checked control&apos;s value, and a checkbox stores
          1 when it is ticked and an empty string when it is not.
        </li>
        <li>
          <strong>hidden</strong> — a value the markup supplies, never shown to the buyer.
        </li>
        <li>
          <strong>consent</strong> — a tick box for agreeing to something, such as a privacy notice.
        </li>
      </ul>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Server-side enforcement is deliberately narrow: required for every type, and a format check
        for email, number and url only. tel, password, date, time and the choice types are
        required-only — the server has no second copy of what a valid date or a matching password
        means for your markup, so it checks presence, not shape. url is re-checked with the same
        rule the builder&apos;s website field uses (see{" "}
        <Link href="/docs/builder" className="underline">
          the form builder guide
        </Link>
        ), so a scheme that rule refuses is answered 400 even where the browser would have accepted
        the string.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">
        Submit, storage and what the buyer sees
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The buyer&apos;s answers are POSTed to the form&apos;s own order route as a client block and a
        fields map. The registered answers are stored as a response — no sale, no payment, no card
        step — and the form&apos;s confirmation (a message or a redirect) is resolved from those
        answers, exactly as for any response form. The stored answers appear in Sales → responses,
        with a detail drawer holding every field value, and agents read the same data through
        list_form_submissions.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Clearing a custom form</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Write an empty html and the custom form is removed: with upsert_form that is an empty
        custom_html string, and through a raw config map it is a custom_html section with no body.
        The form keeps its published URL and renders through the block builder again. Its mode is
        left exactly as it was — an emptied custom form that was response-only stays response-only,
        so set mode yourself if you want those blocks to take payment.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Where to read next</h2>
      <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <Link href="/docs/mcp" className="underline">
            MCP tool reference
          </Link>{" "}
          — upsert_form&apos;s full argument list, and every other tool an agent can call.
        </li>
        <li>
          <Link href="/docs/builder" className="underline">
            The form builder
          </Link>{" "}
          — the block builder a custom form replaces on the published page.
        </li>
        <li>
          <Link href="/docs/sales#responses" className="underline">
            Responses (response-only forms)
          </Link>{" "}
          — where the stored answers appear, and the list_form_submissions tool that reads them.
        </li>
      </ul>
    </article>
  );
}
