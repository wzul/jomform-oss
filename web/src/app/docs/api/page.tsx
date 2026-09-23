import type { Metadata } from "next";

import {
  API_BASE,
  accessLevels,
  apiExamples,
  endpointCount,
  endpointsByTag,
  rateLimitedEndpoints,
  writeGatedReads,
} from "../../../lib/openapi.ts";

export const metadata: Metadata = {
  title: "REST API reference — JomForm Docs",
  description:
    "The JomForm REST API: create a key, make your first call, and the full endpoint reference. Server-to-server only — the API sends no CORS headers and does not answer browser preflight requests.",
  alternates: { canonical: "https://jomform.com/docs/api" },
};

// The REST API reference (issue #395 docs structure).
//
// EVERYTHING BELOW THAT NAMES AN ENDPOINT COMES FROM THE CONTRACT.
//   The endpoint tables are rendered from lib/openapi.ts, which reads
//   web/src/lib/openapiSpec.gen.ts — the generated copy of
//   api/internal/httpapi/openapi_routes.go. The api test suite
//   (openapi_parity_test.go) fails when that registry and the real route table in
//   rest.go disagree in either direction, so this page cannot list an endpoint
//   the API does not serve, and cannot silently miss one it does.
//
//   The worked examples come from web/src/lib/apiExamples.gen.ts and each one is
//   EXECUTED against a live server by openapi_examples_test.go. The page does not
//   print an example nobody has run.
//
// THE PROSE IS THE PART THIS PAGE OWNS, and it is deliberately narrow: what the
// API is for, how to authenticate, what the scopes mean, the error shape, and
// the two facts a reader must not have to discover by experiment — there are no
// CORS headers, and almost nothing is rate limited. Those two sentences are
// asserted by web/src/lib/docsApi.test.ts, so a later edit cannot quietly drop
// them.

const ACCESS_LABELS: Record<string, string> = {
  read: "Any authenticated key (readonly or readwrite)",
  write: "readwrite key; manage/admin role inside a ?workspace= scope",
  admin: "readwrite key; admin role in the scoped workspace",
  session: "Browser session only — an API key gets 401",
};

// The in-page nav. It is a list rather than nine hand-written <li> elements so
// that web/src/lib/docsApi.test.ts can require it to cover EVERY <section id> on
// the page — the hand-written version silently listed 7 of the 9 sections
// ("Versioning & changelog" and "Building and testing against the API" were
// reachable only by scrolling).
const PAGE_SECTIONS: { id: string; title: string }[] = [
  { id: "quickstart", title: "Start in 5 minutes" },
  { id: "auth", title: "Authentication" },
  { id: "scopes", title: "Scopes" },
  { id: "errors", title: "Errors" },
  { id: "limits", title: "Rate limits" },
  { id: "cors", title: "No CORS" },
  { id: "endpoints", title: "Endpoint reference" },
  { id: "versioning", title: "Versioning & changelog" },
  { id: "testing", title: "Building and testing against the API" },
];

export default function ApiDocsPage() {
  const groups = endpointsByTag();
  const total = endpointCount();
  const limited = rateLimitedEndpoints();

  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">REST API</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        The JomForm REST API lets your own server create forms, read sales and responses, manage
        products, subscriptions, webhooks and domains. It is served from{" "}
        <code className="rounded bg-slate-100 px-1">{API_BASE}</code> and currently exposes{" "}
        <strong>{total} operations</strong>, every one of them listed on this page.
      </p>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A machine-readable OpenAPI 3.1 contract for the same surface is published at{" "}
        <a
          href="https://github.com/wzul/jomform/blob/main/api/openapi/openapi.json"
          className="text-[var(--jf-primary)] underline underline-offset-2"
        >
          api/openapi/openapi.json
        </a>
        . It is generated from the API&apos;s own route table and checked against it on every test
        run, so it cannot drift from the endpoints you actually have.
      </p>

      <nav className="mt-6 rounded-lg border border-slate-200 bg-slate-50 p-4">
        <div className="text-xs font-medium uppercase tracking-wide text-slate-500">On this page</div>
        <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm">
          {/*
            text-sky-700, not the --jf-primary token: on this nav's bg-slate-50
            background #5e6ad2 measures 4.49:1 against the 4.5:1 WCAG AA floor,
            which the axe gate reports as a serious violation. sky-700 is the
            colour every other /docs page's "On this page" nav already uses.
          */}
          {PAGE_SECTIONS.map((s) => (
            <li key={s.id}>
              <a href={`#${s.id}`} className="text-sky-700 hover:underline">
                {s.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>

      {/* ── Start here ─────────────────────────────────────────────────────── */}
      <section id="quickstart" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Start in 5 minutes</h2>
        <ol className="mt-3 list-decimal space-y-2 pl-5 text-sm leading-6 text-slate-600">
          <li>
            Sign in and open <strong>Settings → API keys</strong>, then create a key. Choose{" "}
            <code className="rounded bg-slate-100 px-1">readwrite</code> if it needs to create or
            change anything. The raw key is shown <strong>once</strong>; copy it then. You can also
            create one over the API itself with <code className="rounded bg-slate-100 px-1">POST /v1/rest/keys</code>
            .
          </li>
          <li>
            Export it in your shell — the API never takes a key in the query string, and this page
            never prints a real one:
            <pre tabIndex={0} className="mt-2 overflow-x-auto rounded-lg bg-slate-900 p-3 text-xs leading-5 text-slate-100">
              <code>{`export JOMFORM_API_KEY="jfk_your_key_here"`}</code>
            </pre>
          </li>
          <li>
            Make your first call. Every example on this page is executed against a live server by
            the test suite, so these commands are known to work.
          </li>
        </ol>

        <div className="mt-6 space-y-8">
          {apiExamples.map((e) => (
            <div key={e.id} id={`example-${e.id}`} className="scroll-mt-24">
              <h3 className="text-sm font-semibold text-slate-900">{e.title}</h3>
              <p className="mt-1 text-sm leading-relaxed text-slate-600">{e.why}</p>
              <pre tabIndex={0} className="mt-2 overflow-x-auto rounded-lg bg-slate-900 p-3 text-xs leading-5 text-slate-100">
                <code>{e.curl}</code>
              </pre>
              <p className="mt-2 text-xs leading-5 text-slate-600">
                Expect HTTP <strong>{e.expectStatus}</strong>, and a body containing{" "}
                <code className="rounded bg-slate-100 px-1">{e.expectContains}</code>. {e.note}
              </p>
            </div>
          ))}
        </div>
        <p className="mt-4 text-sm leading-relaxed text-slate-600">
          That run is the whole flow: authenticate, create a form, publish it, read it back, read the
          records it produced.
        </p>
      </section>

      {/* ── Authentication ─────────────────────────────────────────────────── */}
      <section id="auth" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Authentication</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Send the key as a bearer token:
        </p>
        <pre tabIndex={0} className="mt-2 overflow-x-auto rounded-lg bg-slate-900 p-3 text-xs leading-5 text-slate-100">
          <code>{`Authorization: Bearer $JOMFORM_API_KEY`}</code>
        </pre>
        <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-6 text-slate-600">
          <li>
            A key belongs to a <strong>workspace</strong> (account), not to a person. Everything it
            reads and writes is that workspace&apos;s data.
          </li>
          <li>
            The dashboard also accepts a browser session cookie on these same routes. A script should
            use a key.
          </li>
          <li>
            <strong>The same key drives the MCP server.</strong> There is one credential for both
            surfaces — create it once and use it for <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>{" "}
            and for MCP (see the{" "}
            <a href="/docs/mcp" className="text-[var(--jf-primary)] underline underline-offset-2">
              MCP tool reference
            </a>
            ).
          </li>
          <li>
            A missing or revoked key is answered with <strong>401</strong> before any workspace is
            resolved, so a broken client fails loudly rather than reading data it should not.
          </li>
        </ul>
      </section>

      {/* ── Scopes ─────────────────────────────────────────────────────────── */}
      <section id="scopes" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Scopes</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          A key carries a scope that decides what it may do at all, independently of who created it:
        </p>
        <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-6 text-slate-600">
          <li>
            <code className="rounded bg-slate-100 px-1">readonly</code> — read only. It is{" "}
            <strong>refused on every write with 403</strong>, and also on the reads whose handler
            writes anyway — an editor flow that saves through a write gate, or a value minted on
            first use. Those are the {writeGatedReads().length} routes below; the list comes from
            the contract, so it cannot go stale:
            <ul className="mt-2 list-disc space-y-1 pl-5">
              {writeGatedReads().map((e) => (
                <li key={`${e.method} ${e.path}`}>
                  <code className="rounded bg-slate-100 px-1">
                    {e.method} {e.path}
                  </code>
                  <span className="text-slate-500"> — {e.summary}</span>
                </li>
              ))}
            </ul>
          </li>
          <li>
            <code className="rounded bg-slate-100 px-1">readwrite</code> — read and write.
          </li>
        </ul>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Inside a <code className="rounded bg-slate-100 px-1">?workspace=&lt;slug&gt;</code> scope
          there is a second, independent axis: your <strong>role in that workspace</strong>. A write
          needs both a readwrite key and the manage-or-admin role, and the payment-credential routes
          need admin. Scope is a property of the credential; role is a property of the person. The
          reference below states, per endpoint, which gate applies.
        </p>
      </section>

      {/* ── Errors ─────────────────────────────────────────────────────────── */}
      <section id="errors" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Errors</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Failures use the HTTP status for the class and a JSON body that says what went wrong:
        </p>
        <pre tabIndex={0} className="mt-2 overflow-x-auto rounded-lg bg-slate-900 p-3 text-xs leading-5 text-slate-100">
          <code>{`{ "error": "auth diperlukan" }`}</code>
        </pre>
        <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-6 text-slate-600">
          <li>
            <strong>400</strong> — the request is invalid. The message is written for the caller
            (&ldquo;nama dan price_sen wajib&rdquo;, &ldquo;tidak dijumpai&rdquo;) rather than being a
            fixed catalogue, so it may change wording without a version bump.
          </li>
          <li>
            <strong>401</strong> — missing, unknown or revoked credential.
          </li>
          <li>
            <strong>403</strong> — the credential is not allowed to do this: a readonly key on a
            write, or no role in the <code className="rounded bg-slate-100 px-1">?workspace=</code>{" "}
            you asked for. The body names the reason, and it always contains the word{" "}
            <code className="rounded bg-slate-100 px-1">scope</code> when a scope is the cause.
          </li>
          <li>
            <strong>404</strong> — no such object <em>in your workspace</em>.
          </li>
          <li>
            <strong>409</strong> — the request conflicts with the object&apos;s state. Publishing a
            payment form that has no products returns 409 with{" "}
            <code className="rounded bg-slate-100 px-1">requires_ack: true</code>, because that form
            could never take money. Send{" "}
            <code className="rounded bg-slate-100 px-1">{`{"acknowledge":true}`}</code> to publish it
            anyway.
          </li>
          <li>
            <strong>429</strong> — rate limited (see below).
          </li>
          <li>
            <strong>500</strong> — an internal error, with the detail logged server-side and never
            returned to you.
          </li>
          <li>
            <strong>503</strong> — the feature needs something this deployment does not have (no
            language model configured, no media storage, and so on). Retrying will not help.
          </li>
        </ul>
      </section>

      {/* ── Rate limits ────────────────────────────────────────────────────── */}
      <section id="limits" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Rate limits</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          <strong>Most of this API has no rate limit.</strong> Do not design a client around a
          uniform quota that does not exist. Exactly {limited.length} of the {total} operations are
          throttled, and they are:
        </p>
        <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-6 text-slate-600">
          {limited.map((e) => (
            <li key={`${e.method} ${e.path}`}>
              <code className="rounded bg-slate-100 px-1">
                {e.method} {e.path}
              </code>{" "}
              — {e.rateLimit} ({e.summary})
            </li>
          ))}
        </ul>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          The upload and AI routes are limited per client IP. The in-app assistant is limited per
          user, counted inside the handler, and it is session-only. A throttled response is{" "}
          <strong>429</strong>; the per-IP limiter also sends{" "}
          <code className="rounded bg-slate-100 px-1">Retry-After: 30</code>. Every other route —
          including the ones that write — is currently unmetered, so apply your own backoff and treat
          a 5xx as retryable rather than assuming the server will protect you from a runaway loop.
        </p>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Traffic reaches the API through Cloudflare, which is where edge protections live. Any
          per-key quota would be a separate, deliberate addition; none is promised here.
        </p>
      </section>

      {/* ── No CORS ────────────────────────────────────────────────────────── */}
      <section id="cors" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">No CORS — server-to-server only</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          This API is built for <strong>server-to-server</strong> calls. It sends{" "}
          <strong>no CORS headers at all</strong>: there is no{" "}
          <code className="rounded bg-slate-100 px-1">Access-Control-Allow-Origin</code> and no{" "}
          <code className="rounded bg-slate-100 px-1">Access-Control-Allow-Methods</code>, and a
          browser&apos;s preflight <code className="rounded bg-slate-100 px-1">OPTIONS</code> request
          is not answered with them. A <code className="rounded bg-slate-100 px-1">fetch()</code> from
          a page on another origin will therefore be blocked by the browser, and the request may not
          even be sent.
        </p>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          That is deliberate, not an oversight. Call the API from your own backend, where you can keep
          the key out of the browser: that is the only place it is safe, because an API key shipped
          in client-side JavaScript is a key you have published. If your front end needs data, have
          it call your server, and have your server call JomForm.
        </p>
      </section>

      {/* ── Endpoint reference ─────────────────────────────────────────────── */}
      <section id="endpoints" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Endpoint reference</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          All {total} operations, generated from the OpenAPI contract. The <em>Access</em> column is
          the gate the API&apos;s code enforces:
        </p>
        <dl className="mt-3 space-y-1 text-xs leading-5 text-slate-600">
          {accessLevels().map((level) => (
            <div key={level} className="flex gap-2">
              <dt className="shrink-0 font-mono font-semibold text-slate-900">{level}</dt>
              <dd>{ACCESS_LABELS[level] ?? ""}</dd>
            </div>
          ))}
        </dl>

        {groups.map((group) => (
          <div key={group.tag} id={`endpoints-${group.tag.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`} className="mt-8 scroll-mt-24">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-[var(--jf-primary)]">
              {group.tag}
            </h3>
            <div className="mt-3 space-y-4">
              {group.endpoints.map((e) => (
                <div key={`${e.method} ${e.path}`}>
                  <div className="flex flex-wrap items-baseline gap-2">
                    <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-slate-700">
                      {e.method}
                    </span>
                    <code className="break-all font-mono text-xs text-slate-900">{e.path}</code>
                    <span className="text-[11px] uppercase tracking-wide text-slate-500">
                      {e.access}
                    </span>
                    {e.rateLimit ? (
                      <span className="text-[11px] text-slate-500">rate limited: {e.rateLimit}</span>
                    ) : null}
                  </div>
                  <p className="mt-1 text-sm leading-relaxed text-slate-600">{e.summary}</p>
                  {e.description ? (
                    <p className="mt-1 whitespace-pre-line text-xs leading-5 text-slate-600">
                      {e.description}
                    </p>
                  ) : null}
                </div>
              ))}
            </div>
          </div>
        ))}
      </section>

      {/* ── Versioning ─────────────────────────────────────────────────────── */}
      <section id="versioning" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Versioning &amp; changelog</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Everything here lives under <code className="rounded bg-slate-100 px-1">/v1</code>, and{" "}
          <strong>/v1 is stable</strong>: existing endpoints keep their paths, their request fields
          and their response field names. Additive changes — a new endpoint, a new optional field, a
          new event type — may land under <code className="rounded bg-slate-100 px-1">/v1</code> at
          any time, so tolerate unknown fields rather than rejecting a response that has grown one.
          A change that would break an existing integration means a new prefix (
          <code className="rounded bg-slate-100 px-1">/v2</code>), with the old one kept running
          alongside it.
        </p>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Error message <em>wording</em> is not covered by that promise — match on the status code
          (and on the stable <code className="rounded bg-slate-100 px-1">code</code> field where one
          is returned, such as{" "}
          <code className="rounded bg-slate-100 px-1">requires_ack</code>), not on the text.
        </p>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Changes to this contract show up in two places you can watch: the published{" "}
          <a
            href="https://github.com/wzul/jomform/blob/main/api/openapi/openapi.json"
            className="text-[var(--jf-primary)] underline underline-offset-2"
          >
            openapi.json
          </a>{" "}
          (which is regenerated with every route change, and fails the test suite when it is out of
          date) and the release notes for each version tag.
        </p>
      </section>

      {/* ── Testing against your own workspace ─────────────────────────────── */}
      <section id="testing" className="mt-10 scroll-mt-24">
        <h2 className="text-lg font-semibold text-slate-900">Building and testing against the API</h2>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          There is <strong>no separate sandbox or test key</strong>. A key acts on your real
          workspace, so an integration test that runs against production data is an integration test
          that may email a customer. Instead, build against a workspace you are willing to throw
          away: create one for the integration (the dashboard&apos;s workspace switcher does this in
          a click, or <code className="rounded bg-slate-100 px-1">POST /v1/workspaces</code>), point
          its CHIP credentials at CHIP&apos;s own sandbox if you are exercising payments, and create
          a key scoped to that workspace. When you are done, delete the workspace rather than leaving
          it to rot — a throwaway workspace you can delete is what makes testing without a sandbox
          safe.
        </p>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Use <code className="rounded bg-slate-100 px-1">readonly</code> for anything that only
          reads. It is the cheapest safety net in this API: a readonly key cannot damage real data
          even if your test harness has a bug.
        </p>
      </section>
    </div>
  );
}
