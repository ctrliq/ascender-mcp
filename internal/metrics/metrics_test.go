package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestObserveToolCall(t *testing.T) {
	before := testutil.ToFloat64(toolCalls.WithLabelValues("test_tool", "ok"))
	ObserveToolCall("test_tool", 5*time.Millisecond, false)
	if got := testutil.ToFloat64(toolCalls.WithLabelValues("test_tool", "ok")); got != before+1 {
		t.Fatalf("ok counter = %v, want %v", got, before+1)
	}

	beforeErr := testutil.ToFloat64(toolCalls.WithLabelValues("test_tool", "error"))
	ObserveToolCall("test_tool", 5*time.Millisecond, true)
	if got := testutil.ToFloat64(toolCalls.WithLabelValues("test_tool", "error")); got != beforeErr+1 {
		t.Fatalf("error counter = %v, want %v", got, beforeErr+1)
	}
}

type staticRoundTripper struct {
	resp *http.Response
	err  error
}

func (s staticRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return s.resp, s.err
}

func TestTransport(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "http://ascender.invalid/api/v2/ping/", nil)

	before := testutil.ToFloat64(upstreamRequests.WithLabelValues(http.MethodPatch, "200"))
	if _, err := Transport(staticRoundTripper{resp: &http.Response{StatusCode: 200}}).RoundTrip(req); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if got := testutil.ToFloat64(upstreamRequests.WithLabelValues(http.MethodPatch, "200")); got != before+1 {
		t.Fatalf("200 counter = %v, want %v", got, before+1)
	}

	beforeFail := testutil.ToFloat64(upstreamRequests.WithLabelValues(http.MethodPatch, "0"))
	if _, err := Transport(staticRoundTripper{err: errors.New("boom")}).RoundTrip(req); err == nil {
		t.Fatal("expected transport error to propagate")
	}
	if got := testutil.ToFloat64(upstreamRequests.WithLabelValues(http.MethodPatch, "0")); got != beforeFail+1 {
		t.Fatalf("transport-failure counter = %v, want %v", got, beforeFail+1)
	}
}

func TestHandlerServesExposition(t *testing.T) {
	ObserveToolCall("exposition_probe", time.Millisecond, false)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "exposition_probe") {
		t.Fatal("exposition missing the observed tool")
	}
}
