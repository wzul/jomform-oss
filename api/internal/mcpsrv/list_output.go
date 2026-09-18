// Shared output shape for list-style MCP tools.
//
// The go-sdk derives a tool's outputSchema by reflecting on the handler's `Out`
// type. A handler that returns a slice therefore advertises
//
//	{"items": {...}, "type": ["null","array"]}
//
// and a client that validates ListToolsResult against the MCP schema rejects
// the WHOLE result, which takes every tool down — not just the offending one.
// Wrapping the slice in this object keeps the schema an object:
//
//	{"properties": {"items": {"type": ["null","array"]}}, "type": "object"}
//
// This is the shape the repo already used by hand in admin_mcp.go,
// email_deliveries_mcp.go and events_mcp.go ({items: [...], page, limit,
// total}); listOutput makes it the only way to return a list, so a new list
// tool cannot reintroduce the slice shape.
// TestMCPToolsHaveObjectOutputSchema guards it over the real tools/list.
package mcpsrv

// listOutput is the object wrapper for a list-returning tool. Handlers return
// it by pointer (*listOutput[T]) so the ordinary `return nil, nil, err` early
// exits stay valid.
type listOutput[T any] struct {
	Items []T `json:"items"`
}

// newListOut wraps items in a listOutput, normalising a nil slice to an empty
// array so `items` is always present as a JSON array.
func newListOut[T any](items []T) *listOutput[T] {
	if items == nil {
		items = []T{}
	}
	return &listOutput[T]{Items: items}
}

// listPageOutput is the object wrapper for a list-returning tool that also
// reports its pagination window, matching the shape admin_mcp.go / media_mcp.go
// / events_mcp.go already return by hand ({items, page, limit, total}).
type listPageOutput[T any] struct {
	Items []T   `json:"items"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

// newListPageOut wraps a page of items, normalising a nil slice to an empty
// array so `items` is always present as a JSON array.
func newListPageOut[T any](items []T, page, limit int, total int64) *listPageOutput[T] {
	if items == nil {
		items = []T{}
	}
	return &listPageOutput[T]{Items: items, Page: page, Limit: limit, Total: total}
}
