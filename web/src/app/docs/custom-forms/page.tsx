import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// AGENTS.md DOCUMENTATION PARITY: the fully-custom HTML response form (issue
// #255) is reachable from the MCP surface (upsert_form takes custom_html and
// custom_fields), and this page is the only description of the field contract.
//
// The facts below are the code's, not a wish: every claim was read out of
// internal/customform (sanitizer, type vocabulary, Collect), internal/httpapi
// render_custom.go (the iframe, its sandbox, the frame's own CSP, the submit
// bridge) and public.go's handleResponseSubmit (what is validated and stored).
export const metadata: Metadata = docsMetadata({
  title: "JomForm Docs — custom HTML forms",
  description:
    "The fully-custom HTML response form: what the sanitizer keeps, how the field-registration contract works, the field types and what the server enforces on submit.",
  path: "/docs/custom-forms",
});

// The field sample is a module const, not a string inside a body: the style gate
// measures every string a section renders, and a JSON sample is not prose. The
// text is the one this page has always shown.
const FIELD_SAMPLE = `"custom_fields": [
  { "field_id": "full_name", "type": "text",     "label": "Full name", "required": true },
  { "field_id": "email",     "type": "email",    "label": "Email",     "required": true },
  { "field_id": "notes",     "type": "textarea", "label": "Notes",     "required": false,
    "validation": "optional - what the answer should look like" }
]`;

// SHORT ON PURPOSE. Every field name, refusal string and server rule survives,
// because /docs is what a developer greps. The section ids are link targets from
// /docs and from the dashboard's inline "?" links.
const SECTIONS: Section[] = [
  {
    id: "custom-html",
    title: "What a custom HTML form is",
    icon: "🧩",
    body: [
      "A custom HTML form is a **response-only form** whose body is HTML you or an agent supply, instead of builder blocks.",
      "The body sits in the `custom_html` section of the form's config. `html` is the markup and `fields` is the field contract.",
      "The MCP tool `upsert_form` takes the two as top-level arguments: `custom_html` and `custom_fields`. That is the practical way to write one.",
      "A REST create or update can carry the same section inside a raw config map.",
      "There is **no dashboard editor** for this form type, so the write surfaces are the only way in.",
      "A form with no html in its section renders through the block renderer as usual.",
      "The dashboard does offer **Edit** on a custom form, which opens the block editor. That editor cannot change or remove the body.",
      {
        note: "Its save carries the `custom_html` section forward untouched, and the blocks it writes back are not rendered while the section carries html.",
      },
    ],
  },
  {
    id: "response-form",
    title: "A custom form is a response form",
    icon: "📝",
    body: [
      "The `custom_html` section and the form's **mode** are two statements of one fact. You only have to write one of them.",
      "A form that carries an enabled section is **response-only**. It collects answers and never takes payment.",
      "The write surfaces store `mode: \"response\"` beside the section for you, on `upsert_form` and on a raw config map alike.",
      "So a raw config whose `custom_html` has a body and no mode key is stored as a response form, not a payment form.",
      "The buyer gets your markup, not the builder or payment layout.",
      "Writing `mode: \"payment\"` next to an enabled section does not win. The stored mode becomes response.",
      "The reverse holds too: a config with **no section**, or with an empty html, is stored exactly as you sent it.",
      {
        note: "A form already stored with an enabled section and no mode key is still rendered and counted as a response form. The section alone decides.",
      },
    ],
  },
  {
    id: "sanitizer",
    title: "What happens to the HTML",
    icon: "🧼",
    body: [
      "The markup is stored as you sent it and **sanitized again on every render**. The stored body is never trusted verbatim.",
      {
        list: [
          "**Scripts and embedded content are dropped with their content.** A script, iframe or object element does not survive. Nothing in the body can run code or load a second document.",
          "**Remote resources are removed.** Only relative URLs, data: URLs and mailto: survive. A stylesheet, font or image hosted elsewhere cannot load.",
          "An https image is stripped and the element is kept without its src. Inline data: images do work.",
          "**Inline styles are kept** for a safe allowlist: colour, background colour, typography, spacing, borders and layout. A url() inside a style attribute is removed.",
          "A **style element** is allowed through and its CSS is kept as written. The frame's own policy blocks a remote fetch there, not the removal of the rule.",
          "**The form tag itself is stripped.** The controls survive and the page wraps them in a form at load, so Enter, required, pattern and browser type checks still apply.",
        ],
      },
      "The sanitized body goes into a **sandboxed iframe** that has no allow-scripts flag, under its own policy.",
      "That policy allows no default source, inline styles only, images limited to data:, and form submissions only to this form.",
      "The outer page owns the submit button and the hidden anti-spam fields. A submit inside the frame is forwarded to the same route.",
      "The merchant markup is therefore **inert**: it cannot run script, navigate the page or address another origin.",
    ],
  },
  {
    id: "custom-js",
    title: "Your own custom_js still runs",
    icon: "⚡",
    body: [
      "The inert part is the **HTML body**, not the form's theme. A `custom_js` set through `set_form_theme` still runs.",
      "It is served on the outer page, outside the sandbox, in its own script block.",
      "That block carries the same per-response nonce as the page's policy. It is the same JS a block-rendered form serves.",
      "The frame is unchanged: no allow-scripts, and its own nonce-free policy. The JS runs on the page, not inside the body.",
      "It cannot make the body execute. The page's policy is **stricter than a block page's**.",
      "Third-party fetch, XHR and beacons are blocked, so only same-origin requests pass. eval and new Function are refused.",
      "So a self-contained script works and one that loads code at runtime does not.",
      {
        note: "A value that tries to close its own script block is cut off at the attempt. To keep a literal end tag inside a string, write it as <\\/script.",
      },
    ],
  },
  {
    id: "register-the-fields",
    title: "Register the fields",
    icon: "🗂️",
    body: [
      "`custom_fields` is an array with one object per answer. Each object has **five properties**.",
      {
        code: FIELD_SAMPLE,
        caption: "One object per answer, in the custom_fields array.",
      },
      {
        table: {
          label: "The custom_fields contract",
          head: ["Property", "What it does"],
          rows: [
            ["`field_id`", "The name the control must carry. Non-empty and unique."],
            ["`type`", "A browser input type, or textarea, select, radio, checkbox, hidden or consent."],
            ["`label`", "Quoted in a validation error. With no label, the field_id is quoted instead."],
            ["`required`", "Enforced by the browser and again by the server on submit."],
            ["`validation`", "A hint for the form's maintainer. Nothing compiles it into a rule."],
          ],
        },
      },
      {
        list: [
          "**field_id is the name.** Every control must carry that exact value in its `name` attribute. That is how a submitted value is matched.",
          "Each id must be non-empty and unique. A save with an empty id, a duplicate id or an unknown type is refused and says which.",
          "**required is enforced twice**: by the browser through the control's attributes, and by the server on the submitted answers.",
          "A direct POST that omits a required field is refused with **400**, not stored.",
          "**validation is a hint**, kept for whoever maintains the form. Nothing on the page or the server compiles it into a rule.",
          "So it cannot refuse an answer the live page accepted.",
        ],
      },
      "A required field also has to **exist in the markup**. The save is refused when a required `field_id` has no control the page can read.",
      "That means an input, select or textarea carrying the name, or a button with a non-empty value.",
      "The refusal reads `required field missing from the HTML`, then names the missing attributes.",
      {
        note: "It comes back from `upsert_form`, `create_template` and `update_template` alike. Optional fields need no control at all.",
      },
    ],
  },
  {
    id: "submit-and-storage",
    title: "Submit, storage and what the buyer sees",
    icon: "📥",
    body: [
      "The answers are POSTed to the form's **own order route** as a client block and a fields map.",
      "The registered answers are stored as a **response**: no sale, no payment, no card step.",
      "The form's confirmation, a message or a redirect, is resolved from those answers, as for any response form.",
      "The stored answers appear in **Sales → responses**, with a detail drawer holding every field value.",
      "Agents read the same data through `list_form_submissions`.",
    ],
  },
  {
    id: "identity",
    title: "The buyer's identity",
    icon: "👤",
    body: [
      "On submit the answers are **filtered to the contract**. A control whose name is not registered is dropped before storage.",
      "So an unregistered name never becomes a field of its own.",
      "The buyer's identity is written outside that filter. It is recorded in the same stored response as `full_name`, `email` and `phone`.",
      "A control the markup names, with `name=\"full_name\"`, `name=\"email\"` or `name=\"phone\"`, answers for that field whatever its input type.",
      "Naming the control is how you place your name field anywhere in the body.",
      "A body that declares none falls back to the first `input[type=text]`, `input[type=email]` and `input[type=tel]` it carries, registered or not.",
      "So a text control that is not registered still reaches storage: its value is stored as `full_name`, the field the responses list shows as the buyer.",
      {
        note: "The anti-spam fields the page itself emits are consumed and left out too.",
      },
    ],
  },
  {
    id: "field-types",
    title: "Field types",
    icon: "🧾",
    body: [
      "The type vocabulary is the browser's **input types**, plus three shapes for longer or structured answers.",
      {
        list: [
          "**text, email, tel, number, date, time, url, password**: single-line answers. Your markup carries an input of the matching type.",
          "JomForm does not draw the control. The type name is what the contract records.",
          "**textarea**: a multi-line answer.",
          "**select, radio, checkbox**: the choice controls. A select stores the chosen option's value, and a radio the checked control's value.",
          "A checkbox stores **1** when it is ticked and an empty string when it is not.",
          "**hidden**: a value the markup supplies, never shown to the buyer.",
          "**consent**: a tick box for agreeing to something, such as a privacy notice.",
        ],
      },
      "Server-side enforcement is **deliberately narrow**: required for every type, and a format check for email, number and url only.",
      "tel, password, date, time and the choice types are **required-only**.",
      "The server has no second copy of what a valid date or a matching password means for your markup, so it checks presence, not shape.",
      "url is re-checked with the rule the builder's website field uses, so a scheme that rule refuses is answered **400**.",
      "See [the form builder guide](/docs/builder).",
    ],
  },
  {
    id: "clearing",
    title: "Clearing a custom form",
    icon: "🧹",
    body: [
      "Write an **empty html** and the custom form is removed. With `upsert_form` that is an empty `custom_html` string.",
      "Through a raw config map it is a `custom_html` section with no body.",
      "The form keeps its published URL and renders through the **block builder** again.",
      "Its mode is left exactly as it was. An emptied custom form that was response-only stays response-only.",
      "So set mode yourself if you want those blocks to take payment.",
    ],
  },
  {
    id: "read-next",
    title: "Where to read next",
    icon: "📚",
    body: [
      {
        list: [
          "**MCP tool reference**: [open it](/docs/mcp) for `upsert_form`'s full argument list and every other tool an agent can call.",
          "**The form builder**: [open it](/docs/builder) for the block builder a custom form replaces on the published page.",
          "**Responses**: [open them](/docs/sales#responses) for where the stored answers appear.",
        ],
      },
    ],
  },
];

export default function CustomFormsDocsPage() {
  return (
    <DocsPage
      title="Custom HTML forms"
      lead="A response-only form whose body is HTML you supply, with the field contract the server enforces on submit."
      sections={SECTIONS}
    />
  );
}
