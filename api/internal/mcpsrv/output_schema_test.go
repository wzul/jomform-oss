package mcpsrv

// MCP output-schema guard.
//
// A client that validates ListToolsResult against the MCP schema rejects the
// WHOLE result when a single tool advertises an outputSchema whose top-level
// `type` is not "object" (e.g. `["null","array"]`), which takes every tool down
// — not just the offending one. That is not hypothetical: 25 of the 149 tools
// returned a bare slice and made https://mcp.jomform.com/mcp unusable for any
// schema-strict client.
//
// The go-sdk derives Tool.OutputSchema by reflecting on the handler's `Out`
// type, so the shape is a property of the handler signature:
//
//   - `Out = []T`            → {"items": …, "type": ["null","array"]}  ← rejected
//   - `Out = *listOutput[T]` → {"properties": {"items": …}, "type": "object"}
//   - `Out = any`            → no outputSchema at all
//
// The `any` case is why this guard checks BOTH properties:
//
//  1. every registered tool advertises an outputSchema, and
//  2. that schema's `type` is "object".
//
// Checking (2) alone would let a new `Out = any` handler return a JSON array at
// call time, unvalidated and undeclared, and reintroduce the same class of
// failure through the missing-schema door.

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPToolsHaveObjectOutputSchema enumerates the registered tools over the
// real MCP protocol (client session + tools/list, paginated to completion) and
// fails if any tool advertises no outputSchema, or one whose `type` is not
// "object". It never reads source, so a newly registered tool is covered the
// moment it is registered.
func TestMCPToolsHaveObjectOutputSchema(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "output-schema-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	var (
		checked        int
		missingSchema  []string
		nonObjectTypes []string
	)
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			checked++
			if tl.OutputSchema == nil {
				missingSchema = append(missingSchema, tl.Name)
				continue
			}
			typ, err := outputSchemaType(tl.OutputSchema)
			if err != nil {
				t.Fatalf("read outputSchema for %s: %v", tl.Name, err)
			}
			if typ != "object" {
				nonObjectTypes = append(nonObjectTypes, tl.Name+": outputSchema.type = "+typ)
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}

	if checked == 0 {
		t.Fatal("no tools registered — registerTools did not run, so the guard checked nothing")
	}
	if len(missingSchema) > 0 {
		sort.Strings(missingSchema)
		t.Errorf("%d of %d tools advertise NO outputSchema (handler `Out` is `any`). "+
			"An undeclared output cannot be validated and can be a JSON array at call time, "+
			"so name the handler's output type (list tools use *listOutput[T]):\n  %s",
			len(missingSchema), checked, strings.Join(missingSchema, "\n  "))
	}
	if len(nonObjectTypes) > 0 {
		sort.Strings(nonObjectTypes)
		t.Errorf("%d of %d tools advertise a non-object outputSchema; a strict MCP client "+
			"rejects the entire ListToolsResult, so ALL tools become unusable. Return an "+
			"object from the handler (list tools use *listOutput[T]):\n  %s",
			len(nonObjectTypes), checked, strings.Join(nonObjectTypes, "\n  "))
	}
}

// outputSchemaType returns the top-level `type` of a rendered outputSchema, or
// "(none)" when the schema declares no type.
func outputSchemaType(schema any) (string, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	var doc struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	switch v := doc.Type.(type) {
	case nil:
		return "(none)", nil
	case string:
		return v, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
