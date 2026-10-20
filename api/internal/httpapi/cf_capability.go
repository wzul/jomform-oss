package httpapi

// CFOAuthConfigured is the export seam for the fact cfOAuthConfigured owns:
// "THIS DEPLOYMENT has the Cloudflare OAuth app configured" (the client id is
// set — see cf_oauth.go for why the id alone is the test).
//
// Two surfaces publish the same word and must therefore answer from the same
// rule:
//
//	GET /v1/rest/cf/status        -> {"configured": cfOAuthConfigured()}
//	MCP get_cloudflare_status     -> {"configured": deps.CFOAuthConfigured()}
//
// cmd/jomform-api/main.go wires this function into mcpsrv.Deps.CFOAuthConfigured
// (mcpsrv importing httpapi would be an import cycle). The MCP tool used to
// hard-code false instead — meaning "the OAuth link is browser-only", which is
// true but is NOT what `configured` means anywhere else — so a deployment with
// the OAuth app configured answered false over MCP and true over REST, and no
// configuration change could make them agree.
//
// Keep this a DELEGATE. A second reading of the env here (or in mcpsrv) would
// drift back into that disagreement the first time the rule changes.
func CFOAuthConfigured() bool { return cfOAuthConfigured() }
