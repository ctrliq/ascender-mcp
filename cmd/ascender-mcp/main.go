// Ascender MCP exposes Ascender operations to LLMs over the Model
// Context Protocol. Bearer tokens from MCP clients are forwarded to
// Ascender verbatim; this server holds no credentials and Ascender
// enforces RBAC.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
	"github.com/ctrliq/ascender-mcp/internal/metrics"
	"github.com/ctrliq/ascender-mcp/internal/middleware"
	"github.com/ctrliq/ascender-mcp/internal/tools"
)

// version is set at link time: go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

// sessionTimeout caps idle MCP sessions so unauthenticated initialize
// calls cannot accumulate until OOM. It is also how long a revoked
// Ascender token keeps working here, since tokens are not re-checked
// between calls.
const sessionTimeout = 5 * time.Minute

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	host := os.Getenv("ASCENDER_HOST")
	if host == "" {
		logger.Error("ASCENDER_HOST is required (e.g. https://ascender.example.com)")
		os.Exit(2)
	}
	listen := envOr("MCP_LISTEN", ":8080")
	path := envOr("MCP_PATH", "/mcp")
	metricsListen := os.Getenv("MCP_METRICS_LISTEN")
	insecure := os.Getenv("ASCENDER_INSECURE_SKIP_VERIFY") == "true"
	readOnly := os.Getenv("ASCENDER_READ_ONLY") == "true"
	toolsets, err := tools.ParseToolsets(os.Getenv("ASCENDER_TOOLSETS"))
	if err != nil {
		logger.Error("bad ASCENDER_TOOLSETS", "err", err)
		os.Exit(2)
	}

	client := ascender.New(host, insecure)
	server := mcp.NewServer(&mcp.Implementation{Name: "ascender-mcp", Version: version}, nil)
	server.AddReceivingMiddleware(middleware.Observe(logger))
	tools.Register(server, client, readOnly, toolsets)

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{SessionTimeout: sessionTimeout},
	)

	mux := http.NewServeMux()
	mux.Handle(path, middleware.RequireBearer(sessionTimeout, mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/readyz", readyz(client))

	servers := []*http.Server{{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}}
	if metricsListen != "" {
		// Metrics get their own listener so per-tool usage counters are
		// never reachable through the ingress that serves /mcp.
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", metrics.Handler())
		servers = append(servers, &http.Server{Addr: metricsListen, Handler: metricsMux, ReadHeaderTimeout: 10 * time.Second})
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("listening",
		"addr", listen, "path", path, "ascender", host,
		"insecure_skip_verify", insecure, "read_only", readOnly,
		"toolsets", toolsetsForLog(toolsets), "metrics", metricsListen)
	for _, s := range servers {
		go func() {
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("listen failed", "addr", s.Addr, "err", err)
				stop()
			}
		}()
	}

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "addr", s.Addr, "err", err)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func toolsetsForLog(enabled []string) string {
	if len(enabled) == 0 {
		return "all"
	}
	return strings.Join(enabled, ",")
}

// readyz reports whether Ascender is reachable, caching the answer for
// 5s so probes do not amplify upstream load.
func readyz(client *ascender.Client) http.Handler {
	const cacheTTL = 5 * time.Second
	var (
		mu       sync.Mutex
		checked  time.Time
		lastErr  error
		lastBody string
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(checked) >= cacheTTL {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			_, lastErr = client.Ping(ctx)
			cancel()
			checked = time.Now()
			lastBody = "ascender: ok"
			if lastErr != nil {
				lastBody = "ascender: " + lastErr.Error()
			}
		}
		if lastErr != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintln(w, lastBody)
	})
}
