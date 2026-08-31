// Command mcptest exercises the JomForm MCP server end-to-end:
// initialize, tools/list, and a tools/call against a live api.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// authInjector adds the API key to every outgoing request.
type authInjector struct{ key string }

func (a *authInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone since transport reuses the request object.
	r2 := req.Clone(req.Context())
	r2.Header.Set("Authorization", "Bearer "+a.key)
	return http.DefaultTransport.RoundTrip(r2)
}

func main() {
	ctx := context.Background()

	base := os.Getenv("JF_BASE")
	if base == "" {
		base = "http://localhost:8082"
	}
	key := os.Getenv("JF_KEY")
	if key == "" {
		key = "jfk_test_secret_123"
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "0.0.1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: base + "/mcp",
		HTTPClient: &http.Client{
			Transport: &authInjector{key: key},
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// 1. tools/list
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("tools/list: %v", err)
	}
	fmt.Println("TOOLS:")
	for _, t := range tools.Tools {
		fmt.Printf("  - %s: %s\n", t.Name, firstLine(t.Description))
	}

	// 2. tools/call — create_product
	var created map[string]any
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_product",
		Arguments: map[string]any{"name": "Kuih Raya Nyonya", "price_sen": 3500, "type": "one_time"},
	})
	if err != nil {
		log.Fatalf("call create_product: %v", err)
	}
	_ = res
	_ = created

	// 3. tools/call — list_products
	res2, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_products",
		Arguments: map[string]any{},
	})
	if err != nil {
		log.Fatalf("call list_products: %v", err)
	}
	fmt.Printf("LIST_PRODUCTS result: %+v\n", structured(res2))

	// 4. tools/call — get_account
	res3, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_account",
		Arguments: map[string]any{},
	})
	if err != nil {
		log.Fatalf("call get_account: %v", err)
	}
	fmt.Printf("GET_ACCOUNT result: %+v\n", structured(res3))
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

func structured(res *mcp.CallToolResult) any {
	if res == nil {
		return nil
	}
	out := make([]any, 0, len(res.Content))
	for _, c := range res.Content {
		out = append(out, c)
	}
	return out
}
