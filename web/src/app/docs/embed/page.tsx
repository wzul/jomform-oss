import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Embed a form on your site — JomForm Docs",
  description:
    "Embed a JomForm form on any website with an iframe. Auto-height via postMessage, the merchant listener snippet, and the 640px fallback.",
  path: "/docs/embed",
});

// The iframe embed guide (issue #456, follow-up from #60 and #395).
//
// THE SNIPPETS ARE MODULE CONSTANTS, NOT BODY STRINGS. Each one is a code
// sample the renderer prints verbatim, and a merchant copies the rendered text,
// so the addresses are written out literally rather than interpolated from a
// shared constant: a guard that reads this source has to see the same concrete
// address the reader does.
//
// THE SNIPPET USES /embed/f/{workspace}/{slug}, NOT THE PLAIN FORM URL. Measured
// on prod and locally: the plain public form URL answers
// `X-Frame-Options: SAMEORIGIN`, which a browser refuses to frame cross-origin,
// while /embed/f/ answers `CSP: frame-ancestors *` + `X-Frame-Options: ALLOWALL`
// and does load. So the plain URL is the one src a merchant must NOT paste.
//
// The auto-height contract: the embedded form posts a message whose type is
// "jomform:height" and whose height is the form's content height to the parent,
// on load, on resize and on DOM mutation. Without the listener the iframe keeps
// the fixed 640px height.

const EMBED_SNIPPET = `<iframe src="https://yourworkspace.jomform.com/embed/f/your-workspace/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>`;

const MERCHANT_SNIPPET = `<iframe src="https://yourworkspace.jomform.com/embed/f/your-workspace/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>

<script>
addEventListener("message", e => {
  if (e.data?.type === "jform:height") document.querySelector("iframe").style.height = e.data.height + "px";
});
</script>`;

const PLAIN_IFRAME = `<iframe src="https://yourworkspace.jomform.com/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>`;

const SECTIONS: Section[] = [
  {
    id: "embed",
    title: "Embed a form",
    icon: "🧩",
    body: [
      "A JomForm form drops into any website with an **iframe**. It renders the full form: fields, validation and payment.",
      "A submit lands in the form's **submissions** and **sales**, exactly as it would on the form's own page.",
      "The buyer reaches **CHIP checkout** and the thank-you page in the top frame, so payment works inside the embed.",
      {
        warn: "Paste the **embed URL**, not the form's public URL. The public URL is same-origin only and will not frame.",
      },
    ],
  },
  {
    id: "snippet",
    title: "1. Paste the embed snippet",
    icon: "📋",
    body: [
      "Turn on **Allow embedding (iframe)** in the form's settings, then paste this on your page.",
      "Replace `your-workspace` and `your-form-slug` with your workspace slug and the form's slug.",
      { code: EMBED_SNIPPET },
      "With embedding switched off, that address answers **403** instead of rendering the form. Turn the setting on first.",
    ],
  },
  {
    id: "auto-height",
    title: "2. Auto-height (recommended)",
    icon: "📐",
    body: [
      "A fixed height causes **inner scrollbars** on longer forms.",
      "The form posts its height over **postMessage** on load, on resize, and whenever its content changes: conditional fields, product selection, the confirmation message.",
      "Listen for `jomform:height` and set the iframe's height. That is the whole listener.",
      { code: MERCHANT_SNIPPET },
      "Without the listener nothing breaks: the frame keeps the **640px** you set, and longer forms scroll inside it.",
      {
        note: "The form only posts when it is inside a frame. A direct visit to the form URL is unaffected.",
      },
    ],
  },
  {
    id: "message-contract",
    title: "The message contract",
    icon: "📨",
    body: [
      "Two fields arrive with every message. Filter on **type** before you touch the DOM.",
      {
        table: {
          label: "postMessage fields",
          head: ["Field", "Meaning"],
          rows: [
            ["type", 'Always `"jomform:height"`. Filter on it so your other listeners ignore the message.'],
            ["height", "The form's content height in pixels. Set it as the iframe's CSS height."],
          ],
        },
      },
    ],
  },
  {
    id: "sandboxed",
    title: "Sandboxed iframes",
    icon: "🔒",
    body: [
      "Plain iframes work as they are. A `sandbox` attribute needs **four tokens**: `allow-forms`, `allow-scripts`, `allow-same-origin` and `allow-top-navigation`.",
      "Those let the form submit and the buyer reach checkout in the top frame.",
      {
        tip: "Without `allow-forms` the form cannot submit. Without `allow-top-navigation` the buyer cannot reach checkout.",
      },
    ],
  },
  {
    id: "cors",
    title: "CORS",
    icon: "🌐",
    body: [
      'No CORS configuration is needed. The form\'s JavaScript posts to `location.pathname + "/order"`.',
      "That is same-origin: the iframe document **is** JomForm, so the browser never makes a cross-origin request.",
    ],
  },
  {
    id: "cookies",
    title: "Private forms and cookies",
    icon: "🍪",
    body: [
      "Rendering and submitting a **published** form are cookieless.",
      'A **private** form is the exception. The buyer enters an access password, and the gate posts to `location.pathname + "/unlock"`.',
      "The gate sets a signed **access cookie**, so the frame can render the form instead of asking again.",
      "That cookie is Partitioned: stored per top-level site, not per page.",
      "So the unlock is scoped to the top-level site, and it does not unlock it on anybody else's.",
      "Partitioning is what keeps that unlock working when a visitor's browser blocks third-party cookies.",
      "It also means that while a second page of your own site that embeds the same form does not ask for the password again, another merchant's page stays locked.",
      "A browser without partitioned-cookie support treats it as an ordinary third-party cookie and may refuse it; the buyer can open the form's own URL.",
    ],
  },
  {
    id: "plain-url",
    title: "Why not the plain form URL?",
    icon: "🚧",
    body: [
      "The ordinary public URL is meant to be opened directly, so JomForm sends it `X-Frame-Options: SAMEORIGIN`.",
      "A browser will not render it inside a frame on another domain. You get an empty frame or a refusal.",
      "That is a deliberate guard against a page wrapping JomForm in an invisible frame to trick a buyer. Do not work around it.",
      { code: PLAIN_IFRAME },
      "The **embed URL** exists for exactly this case and is the only address framable from your site.",
      "Related: the [customization guide](/docs/customize) covers your own colours, CSS and JS.",
    ],
  },
];

export default function EmbedDocsPage() {
  return (
    <DocsPage
      title="Embed a form on your site"
      lead="Drop a form into any website with an iframe, with auto-height and a working payment."
      sections={SECTIONS}
    />
  );
}
