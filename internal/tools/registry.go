package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// registry is what every registrar receives. readOnly drops mutating
// tools at registration so they never appear in tools/list; the MCP
// annotations alone are only hints a client may ignore.
type registry struct {
	server   *mcp.Server
	client   *ascender.Client
	readOnly bool
}

// handler is the shape every tool is written in. The client is already
// bound to the caller's bearer token; any returned error becomes an
// IsError tool result.
type handler[In, Out any] func(ctx context.Context, c *ascender.Client, in In) (Out, error)

func add[In, Out any](r *registry, t *mcp.Tool, h handler[In, Out]) {
	if r.readOnly && !t.Annotations.ReadOnlyHint {
		return
	}
	mcp.AddTool(r.server, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := h(ctx, r.client.WithToken(bearer(req)), in)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		return nil, out, nil
	})
}

// bearer returns the token the HTTP auth middleware stashed on the
// request. Empty only if that middleware was bypassed, in which case
// Ascender rejects the unauthenticated call.
func bearer(req *mcp.CallToolRequest) string {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return ""
	}
	t, _ := req.Extra.TokenInfo.Extra["bearer"].(string)
	return t
}

func read(name, desc string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: desc, Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
	}}
}

func write(name, desc string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: desc, Annotations: &mcp.ToolAnnotations{
		DestructiveHint: ptr(true),
		OpenWorldHint:   ptr(true),
	}}
}

// writeIdempotent marks tools whose retry yields the same end state:
// update, delete, set, and add/remove relations.
func writeIdempotent(name, desc string) *mcp.Tool {
	t := write(name, desc)
	t.Annotations.IdempotentHint = true
	return t
}

func ptr[T any](v T) *T { return &v }
