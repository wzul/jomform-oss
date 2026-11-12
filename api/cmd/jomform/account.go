package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// accountRef is the part of a JomForm account `status` shows. It comes from the
// MCP server's own get_account tool, called with the freshly issued token — the
// same credential the harness will use, so a login that cannot reach the server
// is visible here rather than in the harness later.
type accountRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// lookupAccount performs one MCP initialize + tools/call over the streamable
// HTTP transport.
//
// It is best effort by design: the token is already valid when this runs, and a
// harness that is slow, old or unreachable must never turn an approved login
// into a failed one. The caller drops the error.
func lookupAccount(ctx context.Context, mcpURL, accessToken string) (accountRef, error) {
	var out accountRef
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "jomform-cli", Version: "1.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             mcpURL,
		HTTPClient:           &http.Client{Transport: bearerTransport{token: accessToken}, Timeout: 25 * time.Second},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return out, fmt.Errorf("connect to %s: %w", mcpURL, err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_account", Arguments: map[string]any{}})
	if err != nil {
		return out, fmt.Errorf("get_account: %w", err)
	}
	if res.IsError {
		return out, fmt.Errorf("get_account was refused")
	}
	if res.StructuredContent == nil {
		return out, fmt.Errorf("get_account returned no structured content")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Name == "" && out.Slug == "" {
		return out, fmt.Errorf("get_account returned no account")
	}
	return out, nil
}

// bearerTransport adds the OAuth access token to every MCP request.
type bearerTransport struct {
	token string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
