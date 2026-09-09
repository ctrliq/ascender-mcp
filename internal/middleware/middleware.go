// Package middleware holds the HTTP bearer gate and the MCP audit and
// metrics middleware that wrap the server.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctrliq/ascender-mcp/internal/metrics"
)

// RequireBearer rejects requests without a bearer token. The token is
// not validated here; Ascender does that when tool calls land. The
// fingerprint in UserID activates the SDK's session-hijack guard, and
// the raw token rides along in Extra for tools to forward. sessionTTL
// bounds how long a token is trusted for the session it opened.
func RequireBearer(sessionTTL time.Duration, next http.Handler) http.Handler {
	verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, fmt.Errorf("empty bearer: %w", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{
			UserID:     fingerprint(token),
			Expiration: time.Now().Add(sessionTTL),
			Extra:      map[string]any{"bearer": token},
		}, nil
	}
	return auth.RequireBearerToken(verify, nil)(next)
}

// fingerprint is 128 bits of SHA-256 over the token, hex encoded. Wide
// enough that a collision in the session-user compare is not a concern.
func fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// sensitiveArgKeys are redacted from audit logs.
var sensitiveArgKeys = map[string]bool{
	"password":          true,
	"inputs":            true,
	"extra_vars":        true,
	"extra_data":        true,
	"module_args":       true,
	"source_vars":       true,
	"variables":         true,
	"pod_spec_override": true,
}

// Observe logs every tool call with redacted arguments and records its
// latency and outcome. An IsError result counts as an error even though
// it is not a JSON-RPC failure.
func Observe(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return next(ctx, method, req)
			}
			start := time.Now()
			res, err := next(ctx, method, req)
			result, _ := res.(*mcp.CallToolResult)
			failed := err != nil || (result != nil && result.IsError)
			metrics.ObserveToolCall(params.Name, time.Since(start), failed)

			tokenFP, forwardedFor := "", ""
			if extra := req.GetExtra(); extra != nil {
				if extra.TokenInfo != nil {
					tokenFP = extra.TokenInfo.UserID
				}
				forwardedFor = extra.Header.Get("X-Forwarded-For")
			}
			logger.Info("tool call",
				"tool", params.Name,
				"args", redactArgs(params.Arguments),
				"ok", !failed,
				"duration_ms", time.Since(start).Milliseconds(),
				"token_fp", tokenFP,
				"forwarded_for", forwardedFor)
			return res, err
		}
	}
}

func redactArgs(raw json.RawMessage) map[string]any {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil || len(args) == 0 {
		return nil
	}
	for k := range args {
		if sensitiveArgKeys[k] {
			args[k] = "[redacted]"
		}
	}
	return args
}
