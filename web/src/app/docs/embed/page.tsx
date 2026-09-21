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

const MERCHANT_SNIPPET = `<iframe src="https://yourworkspace.jomform.com/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>

<script>
addEventListener("message", e => {
  if (e.data?.type === "jomform:height") document.querySelector("iframe").style.height = e.data.height + "px";
});
</script>`;

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
        submissions and sales. Framing is allowed for embedded forms, and the buyer is taken to the top frame for
        CHIP checkout and the thank-you page — so payment works inside the embed.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">1. Paste the embed snippet</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Turn on <strong>Allow embedding (iframe)</strong> in the form&apos;s settings, then paste this on your page.
        Replace the <code className="rounded bg-slate-100 px-1">src</code> with your form&apos;s public URL.
      </p>
      <pre
        tabIndex={0}
        className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"
      >
{`<iframe src="https://yourworkspace.jomform.com/your-form-slug"
        style="width:100%;height:640px;border:0"
        title="Your form"></iframe>`}
      </pre>

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
        request. Rendering and submitting are cookieless.
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
    </div>
  );
}
