package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRequireBearer(t *testing.T) {
	var got *auth.TokenInfo
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = auth.TokenInfoFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := RequireBearer(time.Minute, inner)

	do := func(authz string) *httptest.ResponseRecorder {
		got = nil
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("missing header is 401", func(t *testing.T) {
		rec := do("")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "no bearer token") {
			t.Fatalf("body = %q, want SDK's no-bearer message", rec.Body.String())
		}
		if got != nil {
			t.Fatal("inner handler ran without a token")
		}
	})

	t.Run("non-bearer scheme is 401", func(t *testing.T) {
		if rec := do("Basic dXNlcjpwYXNz"); rec.Code != http.StatusUnauthorized || got != nil {
			t.Fatalf("status = %d, inner ran = %v", rec.Code, got != nil)
		}
	})

	t.Run("bearer with only whitespace is 401", func(t *testing.T) {
		// "Bearer  " splits to one field, so the SDK rejects it before
		// our verifier runs; either way the inner handler must not run.
		if rec := do("Bearer   "); rec.Code != http.StatusUnauthorized || got != nil {
			t.Fatalf("status = %d, inner ran = %v", rec.Code, got != nil)
		}
	})

	t.Run("valid token reaches handler with fingerprint and raw token", func(t *testing.T) {
		rec := do("Bearer abc123")
		if rec.Code != http.StatusOK || got == nil {
			t.Fatalf("status = %d, token info = %v", rec.Code, got)
		}
		if got.UserID != fingerprint("abc123") {
			t.Fatalf("UserID = %q, want fingerprint of token", got.UserID)
		}
		if got.Extra["bearer"] != "abc123" {
			t.Fatalf("Extra[bearer] = %v, want raw token", got.Extra["bearer"])
		}
		if got.UserID == "abc123" || strings.Contains(got.UserID, "abc123") {
			t.Fatal("raw token leaked into UserID")
		}
	})

	t.Run("expiration honours session TTL", func(t *testing.T) {
		rec := do("Bearer abc123")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		remaining := time.Until(got.Expiration)
		if remaining <= 50*time.Second || remaining > time.Minute {
			t.Fatalf("Expiration %v from now, want about 1m", remaining)
		}
	})

	t.Run("zero TTL is rejected as expired", func(t *testing.T) {
		// A TTL of zero yields Expiration == now, which the SDK treats as
		// expired by the time it checks. Guards against a misconfigured
		// sessionTimeout silently admitting every request forever.
		h0 := RequireBearer(-time.Second, inner)
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer abc123")
		rec := httptest.NewRecorder()
		h0.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for expired session", rec.Code)
		}
	})
}

// fakeToolReq builds the request shape Observe inspects.
func fakeToolReq(name string, args string, extra *mcp.RequestExtra) mcp.Request {
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: name, Arguments: json.RawMessage(args)},
	}
	req.Extra = extra
	return req
}

func logLines(buf *bytes.Buffer) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

func TestObserve(t *testing.T) {
	newLogger := func() (*slog.Logger, *bytes.Buffer) {
		var buf bytes.Buffer
		return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
	}

	t.Run("non-tool methods pass through untouched", func(t *testing.T) {
		logger, buf := newLogger()
		called := false
		next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			called = true
			return &mcp.ListToolsResult{}, nil
		}
		req := &mcp.ServerRequest[*mcp.ListToolsParams]{Params: &mcp.ListToolsParams{}}
		if _, err := Observe(logger)(next)(context.Background(), "tools/list", req); err != nil || !called {
			t.Fatalf("err=%v called=%v", err, called)
		}
		if buf.Len() != 0 {
			t.Fatalf("tools/list should not be logged as a tool call: %s", buf.String())
		}
	})

	t.Run("success logs tool, redacted args, ok=true and request metadata", func(t *testing.T) {
		logger, buf := newLogger()
		next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return &mcp.CallToolResult{}, nil
		}
		hdr := http.Header{}
		hdr.Set("X-Forwarded-For", "10.0.0.9")
		extra := &mcp.RequestExtra{Header: hdr, TokenInfo: &auth.TokenInfo{UserID: "fp-1"}}
		req := fakeToolReq("launch_template", `{"id_or_name":"deploy","extra_vars":{"pw":"s3cret"},"password":"hunter2"}`, extra)
		res, err := Observe(logger)(next)(context.Background(), "tools/call", req)
		if err != nil || res == nil {
			t.Fatalf("res=%v err=%v", res, err)
		}
		lines := logLines(buf)
		if len(lines) != 1 {
			t.Fatalf("want 1 log line, got %d: %s", len(lines), buf.String())
		}
		l := lines[0]
		if l["tool"] != "launch_template" || l["ok"] != true || l["token_fp"] != "fp-1" || l["forwarded_for"] != "10.0.0.9" {
			t.Fatalf("log = %v", l)
		}
		args := l["args"].(map[string]any)
		if args["id_or_name"] != "deploy" || args["extra_vars"] != "[redacted]" || args["password"] != "[redacted]" {
			t.Fatalf("args = %v", args)
		}
		if strings.Contains(buf.String(), "s3cret") || strings.Contains(buf.String(), "hunter2") {
			t.Fatalf("secret leaked into log: %s", buf.String())
		}
	})

	t.Run("IsError result logs ok=false", func(t *testing.T) {
		logger, buf := newLogger()
		next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return &mcp.CallToolResult{IsError: true}, nil
		}
		Observe(logger)(next)(context.Background(), "tools/call", fakeToolReq("get_job", `{}`, nil))
		if l := logLines(buf); len(l) != 1 || l[0]["ok"] != false {
			t.Fatalf("log = %v", l)
		}
	})

	t.Run("handler error logs ok=false and propagates", func(t *testing.T) {
		logger, buf := newLogger()
		boom := errors.New("boom")
		next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return nil, boom
		}
		_, err := Observe(logger)(next)(context.Background(), "tools/call", fakeToolReq("get_job", `{}`, nil))
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
		if l := logLines(buf); len(l) != 1 || l[0]["ok"] != false || l[0]["token_fp"] != "" {
			t.Fatalf("log = %v", l)
		}
	})
}
