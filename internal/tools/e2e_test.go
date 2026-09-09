package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// TestEndToEnd drives real tools through an in-memory MCP session
// against a stub Ascender, covering the adapter every tool relies on:
// bearer forwarding, page envelopes, and upstream errors surfacing as
// IsError results.
func TestEndToEnd(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v2/projects/":
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1, "next": nil,
				"results": []map[string]any{{"id": 7, "name": "site"}},
			})
		case "/api/v2/projects/7/":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"detail":"nope"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	Register(server, ascender.New(upstream.URL, false), false, []string{"projects"})

	// Simulate what the HTTP bearer middleware attaches to each request.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.ServerRequest[*mcp.CallToolParamsRaw]); ok {
				call.Extra = &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{Extra: map[string]any{"bearer": "tok123"}}}
			}
			return next(ctx, method, req)
		}
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("list_projects failed: err=%v res=%+v", err, res)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("upstream saw Authorization %q, want bearer forwarded", gotAuth)
	}
	page, _ := res.StructuredContent.(map[string]any)
	if page["total"] != float64(1) {
		t.Fatalf("list_projects structured content = %v, want total 1", res.StructuredContent)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_project", Arguments: map[string]any{"id_or_name": "7"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("upstream 400 should be an IsError result, got %+v", res)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "nope") {
		t.Fatalf("error text %q should carry Ascender's detail", text)
	}
}
