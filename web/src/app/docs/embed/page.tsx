import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Embed a form on your site — JomForm Docs",
  description:
    "Embed a JomForm form on any website with an iframe. Auto-height via postMessage, the merchant listener snippet, and the 640px fallback.",
  alternates: { canonical: "https://jomform.com/docs/embed" },
};

// Issue #456: iframe embed docs. Follow-up from #60 (embed payment flow) and
// #395 (knowledge base). Documents the auto-height postMessage contract: the
// embedded form posts {type:"jomform:height", height} to the parent on load,
// on resize and on DOM mutation; the 3-line listener below makes the iframe
// track the form's height. Without the listener the iframe keeps the fixed
// 640px height (inner scrollbars for longer forms).
//
// THE SNIPPET USES /embed/f/{workspace}/{slug}, NOT THE PLAIN FORM URL.
// Measured on prod and locally: the plain public form URL answers
// `X-Frame-Options: SAMEORIGIN`, which a browser refuses to frame cross-origin
// (read from the CDP frame tree: the frame resolves to chrome-error://chromewebdata/),
// while /embed/f/ answers `CSP: frame-ancestors *` + `X-Frame-Options: ALLOWALL`
// and does load. So the plain URL is the one src a merchant must NOT paste, and
// this page previously handed them exactly that. The plain URL's same-origin-only
// framing is documented below rather than left as a surprise.
//
// The addresses are written out LITERALLY in the snippets rather than built from
// a shared constant. A merchant copies the rendered text, and a guard that reads
// this source has to see the same concrete address — an interpolated `${...}`
// hides the URL from both.

const MERCHANT_SNIPPET = `<iframe src="https://yourworkspace.jomform.com/embed/f/your-workspace/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>

<script>
addEventListener("message", e => {
  if (e.data?.type === "jomform:height") document.querySelector("iframe").style.height = e.data.height + "px";
});
</script>`;

const PLAIN_IFRAME = `<iframe src="https://yourworkspace.jomform.com/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>`;

const MESSAGE_CONTRACT: { field: string; meaning: string }[] = [
  { field: "type", meaning: 'Always "jomform:height" — filter on this so your other postMessage listeners ignore it.' },
  { field: "height", meaning: "The form's content height in pixels. Set it as the iframe's CSS height." },
];

export default function EmbedDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Embed a form on your site</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Drop a JomForm form into any website with an <code className="rounded bg-slate-100 px-1">iframe</code>. The
        embed renders the full form (fields, validation and payment) and submits straight into your form&apos;s
        submissions and sales. The buyer is taken to the top frame for CHIP checkout and the thank-you page — so
        payment works inside the embed.
      </p>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Use the <strong>embed URL</strong> below — not the form&apos;s ordinary public URL. The public URL is
        same-origin only: it answers{" "}
        <code className="rounded bg-slate-100 px-1">X-Frame-Options: SAMEORIGIN</code>, so a browser refuses to frame
        it on your site and shows an empty or broken frame. The embed URL is the one that is allowed to be framed
        from your domain.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">1. Paste the embed snippet</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Turn on <strong>Allow embedding (iframe)</strong> in the form&apos;s settings, then paste this on your page.
        Replace <code className="rounded bg-slate-100 px-1">your-workspace</code> and{" "}
        <code className="rounded bg-slate-100 px-1">your-form-slug</code> with your workspace slug and the form&apos;s
        slug.
      </p>
      <pre
        tabIndex={0}
        className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"
      >
{`<iframe src="https://yourworkspace.jomform.com/embed/f/your-workspace/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>`}
      </pre>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        With embedding switched off, that address answers{" "}
        <code className="rounded bg-slate-100 px-1">403</code> instead of rendering the form — the setting is what
        makes the iframe work, so turn it on before you paste.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">2. Auto-height (recommended)</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A fixed height causes inner scrollbars on longer forms. The embedded form posts its height to your page on
        load, on resize and whenever its content changes (conditional fields, product selection, the confirmation
        message after submit). Add this 3-line listener so the iframe grows and shrinks with the form — no inner
        scrollbars:
      </p>
      <pre
        tabIndex={0}
        className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"
      >
{MERCHANT_SNIPPET}
      </pre>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Without the listener nothing breaks: the iframe keeps whatever height you set in CSS (
        <code className="rounded bg-slate-100 px-1">640px</code> in the snippet above), and longer forms scroll
        inside the frame.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">The message contract</h2>
      <div className="mt-4 overflow-hidden rounded-lg border border-slate-200">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-50 text-slate-700">
            <tr>
              <th className="px-4 py-2 font-semibold">Field</th>
              <th className="px-4 py-2 font-semibold">Meaning</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 text-slate-600">
            {MESSAGE_CONTRACT.map((m) => (
              <tr key={m.field}>
                <td className="px-4 py-2 font-mono text-xs text-slate-800">{m.field}</td>
                <td className="px-4 py-2">{m.meaning}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        The form only posts when it is actually inside a frame — a direct visit to the form URL is unaffected.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Sandboxed iframes</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Plain iframes like the snippet above work as-is. If you add a{" "}
        <code className="rounded bg-slate-100 px-1">sandbox</code> attribute, include{" "}
        <code className="rounded bg-slate-100 px-1">allow-forms</code>,{" "}
        <code className="rounded bg-slate-100 px-1">allow-scripts</code>,{" "}
        <code className="rounded bg-slate-100 px-1">allow-same-origin</code> and{" "}
        <code className="rounded bg-slate-100 px-1">allow-top-navigation</code> so the form can submit and the
        buyer can reach CHIP checkout in the top frame.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">CORS</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        No CORS configuration is needed. The form&apos;s JavaScript posts to{" "}
        <code className="rounded bg-slate-100 px-1">location.pathname + &quot;/order&quot;</code> — same-origin
        inside the iframe (the iframe document <em>is</em> JomForm), so the browser never makes a cross-origin
        request.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Private forms and cookies</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Rendering and submitting a <strong>published</strong> form are cookieless. A{" "}
        <strong>private</strong> form is the one exception: its buyer enters an access password, and the gate
        (which also posts to{" "}
        <code className="rounded bg-slate-100 px-1">location.pathname + &quot;/unlock&quot;</code>, so it works
        inside the embed just like the button does) sets a signed access cookie so the frame can render the form
        instead of asking again. That cookie is scoped to the top-level site it was entered on, not to the single
        page — it is a{" "}
        <code className="rounded bg-slate-100 px-1">Partitioned</code> third-party cookie, so unlocking the
        embedded form on your site does not unlock it on anybody else&apos;s, while a second page of your own site
        that embeds the same form does not ask for the password again. Partitioning is also what keeps that
        unlock working when a visitor&apos;s browser blocks third-party cookies: the cookie is stored per top-level
        site, so your page keeps it and the buyer is not asked for the password again. A browser that does not
        support partitioned cookies yet ignores the attribute and treats it as an ordinary third-party cookie,
        which such a browser may refuse to store — a buyer who is asked again can always open the form&apos;s own
        URL directly.
      </p>

      <p className="mt-8 text-sm leading-relaxed text-slate-600">
        Related: see the{" "}
        <a
          href="/docs/customize"
          className="text-[var(--jf-primary)] underline underline-offset-2 hover:underline"
        >
          customization guide
        </a>{" "}
        to brand the embedded form with your own colours, CSS and JS.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Why not the plain form URL?</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The form&apos;s ordinary public URL is meant to be opened directly, so JomForm sends it the standard{" "}
        <code className="rounded bg-slate-100 px-1">X-Frame-Options: SAMEORIGIN</code> header — a browser will not
        render it inside a frame on a different domain, and depending on the browser you get an empty frame or a
        refusal. That is a deliberate protection against a page wrapping JomForm in an invisible frame to trick a
        buyer into clicking something, so it is not something to work around by other means:
      </p>
      <pre
        tabIndex={0}
        className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"
      >
{PLAIN_IFRAME}
      </pre>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        The embed URL above exists for exactly this case and is the only address that is framable from your site. If
        you need your form on your own page, always use the embed URL.
      </p>
    </div>
  );
}
