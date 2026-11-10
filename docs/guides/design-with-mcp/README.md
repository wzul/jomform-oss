# Design with an AI agent (MCP): setup guide

Connect your agent to JomForm once, then drive the store with plain language.

**What this does:** JomForm speaks the Model Context Protocol, so a connected agent can add
products, edit forms, read sales and refund a sale for you.

## 1. Open the connect page and copy the server URL

![The MCP connect page with the Copy button beside https://mcp.jomform.com/mcp boxed](01-server-url.png)

The **MCP** page (`/mcp` on your JomForm address) prints the one thing you need,
`https://mcp.jomform.com/mcp`, and the `/mcp` path is required. Press **Copy**.

## 2. Paste it into your MCP client

![The Claude Desktop JSON snippet boxed](02-client-config.png)

The page prints the exact `claude_desktop_config.json` entry, and any client that takes a remote MCP
server over HTTP uses the same URL. Restart the client after saving.

## 3. Authorise

The client finds the OAuth 2.1 (PKCE) endpoints itself and opens a JomForm login page. Sign in and
grant access — there is no API key to copy or rotate, because the grant is the credential.

## 4. Pick the scope

![The Read-only and Read & write scope cards boxed](03-scope.png)

The grant is enforced on every tool call:

| scope | what the agent can do |
|---|---|
| **read** | look at forms, products, sales and analytics; every tool that changes anything is refused |
| **readwrite** | do all of that **and** change the store: add a product, edit a form, refund a sale |

A client that asks for no scope gets **readwrite**, and a read-only grant can never upgrade itself.

## 5. Ask for what you want

![The example commands card boxed](04-example-commands.png)

*"add product RM25.00"*, *"how many sales today?"*, *"refund sale JF-xxxx"*: the agent calls the
matching tool and answers, and anything that would change the store has to pass the scope above.

## Notes

- **Discovery is automatic.** The server advertises `/.well-known/oauth-protected-resource` and
  `/.well-known/oauth-authorization-server`, and an unauthenticated call to `/mcp` answers `401`
  with a `WWW-Authenticate` header pointing at them.
- **Connecting from Canva, or any agent that is not a coding client:** it must allow a remote MCP
  server with HTTP OAuth; paste the URL into the field it calls *remote MCP server*.
- **The agent acts inside one workspace.** Tools take a workspace, so one connection can drive every
  workspace your account manages and nothing outside them.
