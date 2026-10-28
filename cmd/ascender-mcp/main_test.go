package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("ASCENDER_TEST_SET", "x")
	t.Setenv("ASCENDER_TEST_EMPTY", "")
	if got := envOr("ASCENDER_TEST_SET", "fb"); got != "x" {
		t.Errorf("set: got %q", got)
	}
	if got := envOr("ASCENDER_TEST_EMPTY", "fb"); got != "fb" {
		t.Errorf("empty should use fallback: got %q", got)
	}
	if got := envOr("ASCENDER_TEST_UNSET_zz", "fb"); got != "fb" {
		t.Errorf("unset should use fallback: got %q", got)
	}
}

func TestToolsetsForLog(t *testing.T) {
	if got := toolsetsForLog(nil); got != "all" {
		t.Errorf("nil: got %q", got)
	}
	if got := toolsetsForLog([]string{"jobs", "templates"}); got != "jobs,templates" {
		t.Errorf("got %q", got)
	}
}

func TestReadyz(t *testing.T) {
	var pings atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pings.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"detail":"db down"}`))
			return
		}
		w.Write([]byte(`{"ha":false}`))
	}))
	defer upstream.Close()

	probe := func(h http.Handler) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return rec
	}

	t.Run("ok when ascender answers", func(t *testing.T) {
		h := readyz(ascender.New(upstream.URL, false))
		rec := probe(h)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ascender: ok" {
			t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("503 with error text when ascender fails", func(t *testing.T) {
		fail.Store(true)
		defer fail.Store(false)
		h := readyz(ascender.New(upstream.URL, false))
		rec := probe(h)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "db down") {
			t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("result is cached across rapid probes", func(t *testing.T) {
		h := readyz(ascender.New(upstream.URL, false))
		before := pings.Load()
		for range 5 {
			probe(h)
		}
		if n := pings.Load() - before; n != 1 {
			t.Fatalf("5 probes hit upstream %d times, want 1", n)
		}
	})

	t.Run("unreachable host is 503", func(t *testing.T) {
		h := readyz(ascender.New("http://127.0.0.1:1", false))
		if rec := probe(h); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("code=%d", rec.Code)
		}
	})
}
