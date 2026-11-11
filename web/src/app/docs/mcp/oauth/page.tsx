import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "MCP OAuth & tokens — JomForm Docs",
  description:
    "How an MCP client discovers JomForm's authorization server, the read and readwrite scopes, and how API-key scopes differ from a connector grant.",
  path: "/docs/mcp/oauth",
});

// Card t_0356ed4c: the OAuth half of the old /docs/mcp page, moved here unchanged
// — discovery, connector scopes and API-key scopes are one task ("what may this
// credential do"), and a reader debugging a 403 lands on exactly this page.

const SECTIONS: Section[] = [
  {
    id: "oauth-discovery",
    title: "OAuth discovery",
    icon: "🔐",
    body: [
      "A client discovers how to authenticate from the server itself. Every step below is a document or a challenge it can read.",
      {
        steps: [
          "An unauthenticated call to `https://mcp.jomform.com/mcp` is refused with `401` and `WWW-Authenticate: Bearer resource_metadata=\"…\"`.",
          "That header names the **protected-resource metadata** URL.",
          "The client fetches `/.well-known/oauth-protected-resource`, defined by RFC 9728.",
          "The document names the resource, the authorization server, the scopes and the token header.",
          "It then fetches `/.well-known/oauth-authorization-server`, RFC 8414, for the authorize, token, revoke and registration endpoints.",
          "It registers itself dynamically and opens the authorize URL in the browser, where you sign in and grant access.",
          "It exchanges the code for an access token, sending the `resource` parameter on both requests.",
          "JomForm rejects a `resource` that is not this MCP endpoint, and issues no token.",
          "Every later call to `/mcp` carries the token as a Bearer credential.",
        ],
      },
      "The resource is `https://mcp.jomform.com/mcp`. The authorization server is `https://mcp.jomform.com`, and the scopes are `read` and `readwrite`.",
    ],
  },
  {
    id: "connector-scopes",
    title: "OAuth connector scopes",
    icon: "🎫",
    body: [
      "A connector's grant carries one of two **scopes**. The MCP client asks for the one it wants on the authorize request.",
      "The request carries `?scope=…`, space-delimited. JomForm grants exactly what was asked for and records it on the token.",
      "Both scopes are advertised in the protected-resource and authorization-server metadata, beside the other fields a client needs.",
      {
        list: [
          "`read`: read-only. Every listing and getter works. Every tool that creates, updates, deletes, archives, cancels, refunds or generates a stored key is refused with a scope error.",
          "A `read` grant cannot upgrade itself. `create_api_key` is a write, so a read-only connector can never mint a wider credential.",
          "`readwrite`: read and write. A request that omits `scope` defaults to readwrite, so an existing client is never silently downgraded.",
        ],
      },
      "The scope is **fixed for the life of the connection**. Refreshing an access token returns the same grant, so a read grant never widens.",
      "A request that names a scope the server does not advertise is refused with `invalid_scope`.",
      "It is never silently widened to readwrite, or narrowed to read.",
      "The token response echoes the **issued grant**, not the advertised list, so a client can read back what it holds.",
    ],
  },
  {
    id: "headless",
    title: "Headless onboarding (no browser)",
    icon: "🖥️",
    body: [
      "An AI harness with no browser can connect without one. JomForm implements the **device authorization grant**, RFC 8628, and advertises it in the authorization-server metadata.",
      {
        steps: [
          "The harness posts `client_id`, plus optional `scope` and `login_hint` (your account email), to `/oauth/device_authorization`.",
          "It receives a `device_code`, a short `user_code`, a `verification_uri`, `expires_in` of 600 seconds and `interval` of 5 seconds.",
          "JomForm emails the `user_code` to the address on the account. That email is the consent screen.",
          "The email names the client, the access in plain words, the address and city, the time and the code.",
          "You approve or deny from the link, or by entering the code on Profile → MCP → Connected apps.",
          "The next poll returns an access token and a refresh token.",
        ],
      },
      "The device grant starts at `read`. Write access is granted only when you approve an email that names it.",
      "Both secrets are stored hashed. The email goes only to the account's registered address.",
      "The response is identical whether or not an address has an account, so it cannot be used to discover who is registered.",
    ],
  },
  {
    id: "device-polling",
    title: "Polling and limits",
    icon: "⏱️",
    body: [
      "The harness polls `/oauth/token` with the **device grant type** until you decide.",
      {
        list: [
          "`authorization_pending`: you have not approved or denied yet.",
          "`slow_down`: the harness polled faster than `interval`. The interval grows by five seconds.",
          "`access_denied`: you refused. The harness must stop.",
          "`expired_token`: ten minutes passed. Start again.",
        ],
      },
      "An approved `device_code` is single-use. A second exchange is refused.",
      "Three authorisations an hour are allowed per account and per address. Beyond that the request is refused rather than mailed.",
      "Platform-admin accounts cannot use this flow. An emailed code must not be the only thing before a platform-admin token.",
    ],
  },
  {
    id: "scope-changes",
    title: "Changing a scope",
    icon: "🔁",
    body: [
      "A grant never widens on refresh. Refreshing returns the same scope, and a `scope` parameter on a refresh request is ignored.",
      "A wider grant is always a **new grant**. The old credential stops working in the same action.",
      {
        list: [
          "**Profile → MCP → Connected apps**. You are already signed in, so one confirmation is enough. Copy the new credential from the page.",
          "A new device authorisation from the harness. The email is the consent screen again.",
          "The browser authorize flow with the higher scope.",
        ],
      },
      "Narrowing a grant needs no confirmation, takes effect immediately, and also rotates the credential.",
      "Every change records an audit event with both scopes and the route used.",
      "Over MCP, `list_connected_apps`, `revoke_connected_app` and `set_connected_app_scope` expose the same surface. The last one can only lower a grant.",
    ],
  },
  {
    id: "api-key-scopes",
    title: "API-key scopes",
    icon: "🔑",
    body: [
      "An API key carries a **scope**, which says what that credential may do at all.",
      {
        list: [
          "`readonly`: read tools only. Every read works.",
          "Every tool that creates, updates, deletes, archives, cancels, revokes or generates a stored key is refused with a scope error.",
          "A readonly key cannot mint another key, so it cannot escalate itself to readwrite.",
          "`readwrite`: read and write. It is the default grant for an OAuth connector unless the client asks for `read`.",
          "`admin`: kept for existing keys. It satisfies every scope, so it behaves as readwrite.",
        ],
      },
      "Scope is a separate **axis** from your workspace role: `read_only`, `manage` or `admin`.",
      "A write has to satisfy both. A `readwrite` key held by a `read_only` member is still refused, and so is a `readonly` key held by the owner.",
      "Reading never requires the write scope.",
      "One tool looks wider than it is. `get_webhook_signing_key` needs `readwrite` only when it must create the account's keypair on first use.",
      "Reading an existing key does not.",
    ],
  },
];

export default function McpOAuthPage() {
  return (
    <DocsPage
      title="OAuth & tokens"
      lead="How a client finds the authorization server, and what a read or readwrite grant may do."
      sections={SECTIONS}
    />
  );
}
