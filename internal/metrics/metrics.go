// Package metrics holds the Prometheus collectors. Values never include
// tool arguments, tokens, or Ascender response content: only names,
// methods, status codes, and timings.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	toolCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ascender_mcp_tool_calls_total",
		Help: "MCP tool calls by tool name and outcome (ok or error).",
	}, []string{"tool", "outcome"})

	toolDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ascender_mcp_tool_call_duration_seconds",
		Help:    "MCP tool call latency by tool name, including upstream Ascender round trips.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tool"})

	upstreamRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ascender_mcp_upstream_requests_total",
		Help: "Requests forwarded to the Ascender API by method and status code (code 0 is a transport failure).",
	}, []string{"method", "code"})

	upstreamDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ascender_mcp_upstream_request_duration_seconds",
		Help:    "Ascender API round-trip latency by method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})
)

// ObserveToolCall records one completed MCP tool call.
func ObserveToolCall(tool string, d time.Duration, isError bool) {
	outcome := "ok"
	if isError {
		outcome = "error"
	}
	toolCalls.WithLabelValues(tool, outcome).Inc()
	toolDuration.WithLabelValues(tool).Observe(d.Seconds())
}

// Handler serves the Prometheus exposition endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Transport wraps base (nil means http.DefaultTransport) with upstream
// request counting and latency observation.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		start := time.Now()
		resp, err := base.RoundTrip(req)
		code := 0
		if err == nil {
			code = resp.StatusCode
		}
		upstreamRequests.WithLabelValues(req.Method, strconv.Itoa(code)).Inc()
		upstreamDuration.WithLabelValues(req.Method).Observe(time.Since(start).Seconds())
		return resp, err
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
