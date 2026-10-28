package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ctrliq/ascender-mcp/internal/ascender"
)

// stub is a scriptable Ascender. Handlers are keyed by "METHOD /path"
// (path without the /api/v2 prefix or trailing slash) and may be
// replaced mid-test. Every request is recorded.
type stub struct {
	t        *testing.T
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	calls    []string // "METHOD /path body"
	client   *ascender.Client
}

func newStubAscender(t *testing.T) *stub {
	t.Helper()
	s := &stub{t: t, handlers: map[string]http.HandlerFunc{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v2"), "/")
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		body := string(raw)
		key := r.Method + " " + path
		s.mu.Lock()
		s.calls = append(s.calls, strings.TrimSpace(key+" "+body))
		h := s.handlers[key]
		s.mu.Unlock()
		if h == nil {
			t.Logf("stub: unexpected %s", key)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"detail":"Not found."}`))
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	s.client = ascender.New(srv.URL, false)
	return s
}

func (s *stub) on(key string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[key] = h
}

func (s *stub) reply(key string, status int, v any) {
	s.on(key, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	})
}

func (s *stub) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func page(items ...map[string]any) map[string]any {
	results := []map[string]any{}
	results = append(results, items...)
	return map[string]any{"count": len(results), "next": nil, "results": results}
}

func TestWaitForJob(t *testing.T) {
	t.Run("returns once status is terminal", func(t *testing.T) {
		s := newStubAscender(t)
		statuses := []string{"pending", "running", "successful"}
		var n int
		s.on("GET /jobs/5", func(w http.ResponseWriter, r *http.Request) {
			st := statuses[min(n, len(statuses)-1)]
			n++
			json.NewEncoder(w).Encode(map[string]any{"id": 5, "status": st})
		})
		job, err := waitForJob(context.Background(), s.client, "jobs/5", 30*time.Second)
		if err != nil || job["status"] != "successful" {
			t.Fatalf("job=%v err=%v", job, err)
		}
		if n != 3 {
			t.Fatalf("polled %d times, want 3", n)
		}
	})

	t.Run("every terminal status ends the wait", func(t *testing.T) {
		// Listed explicitly, not read from terminalJobStatuses, so dropping
		// a status from that map fails here instead of shrinking the test.
		for _, status := range []string{"successful", "failed", "error", "canceled"} {
			s := newStubAscender(t)
			s.reply("GET /jobs/1", 200, map[string]any{"status": status})
			job, err := waitForJob(context.Background(), s.client, "jobs/1", time.Second)
			if err != nil || job["status"] != status {
				t.Fatalf("%s: job=%v err=%v", status, job, err)
			}
		}
	})

	t.Run("timeout reports non-terminal", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /jobs/1", 200, map[string]any{"status": "running"})
		start := time.Now()
		_, err := waitForJob(context.Background(), s.client, "jobs/1", 100*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "still non-terminal") {
			t.Fatalf("err = %v", err)
		}
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("took %v; sleep should be clamped to remaining timeout", el)
		}
	})

	t.Run("context cancel stops polling", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /jobs/1", 200, map[string]any{"status": "running"})
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		_, err := waitForJob(ctx, s.client, "jobs/1", time.Minute)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("transient upstream error is retried", func(t *testing.T) {
		s := newStubAscender(t)
		var n int
		s.on("GET /jobs/1", func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "failed"})
		})
		job, err := waitForJob(context.Background(), s.client, "jobs/1", 30*time.Second)
		if err != nil || job["status"] != "failed" {
			t.Fatalf("job=%v err=%v", job, err)
		}
		if n != 2 {
			t.Fatalf("polled %d times, want 2", n)
		}
	})

	t.Run("upstream error at deadline surfaces timeout", func(t *testing.T) {
		// Errors inside the grace window never win over the overall
		// timeout: a job that is unreachable for the whole wait still
		// ends at the deadline.
		s := newStubAscender(t)
		s.on("GET /jobs/1", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
		_, err := waitForJob(context.Background(), s.client, "jobs/1", 50*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "timeout after") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCancel(t *testing.T) {
	t.Run("posts cancel then returns fresh record", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("POST /jobs/9/cancel", 202, map[string]any{})
		s.reply("GET /jobs/9", 200, map[string]any{"id": 9, "status": "canceled"})
		job, err := cancel(context.Background(), s.client, "jobs/9")
		if err != nil || job["status"] != "canceled" {
			t.Fatalf("job=%v err=%v", job, err)
		}
		if calls := s.seen(); len(calls) != 2 || calls[0] != "POST /jobs/9/cancel" || calls[1] != "GET /jobs/9" {
			t.Fatalf("calls = %v", calls)
		}
	})

	t.Run("405 on terminal job is success", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("POST /jobs/9/cancel", 405, map[string]any{"detail": "Method not allowed"})
		s.reply("GET /jobs/9", 200, map[string]any{"id": 9, "status": "successful"})
		job, err := cancel(context.Background(), s.client, "jobs/9")
		if err != nil || job["status"] != "successful" {
			t.Fatalf("job=%v err=%v", job, err)
		}
	})

	t.Run("other errors propagate", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("POST /jobs/9/cancel", 403, map[string]any{"detail": "forbidden"})
		_, err := cancel(context.Background(), s.client, "jobs/9")
		var ae *ascender.Error
		if !errors.As(err, &ae) || ae.Status != 403 {
			t.Fatalf("err = %v", err)
		}
		if calls := s.seen(); len(calls) != 1 {
			t.Fatalf("should not GET after a hard failure: %v", calls)
		}
	})
}

func TestReplaceRelation(t *testing.T) {
	const path = "job_templates/3/instance_groups"

	t.Run("no-op when already equal", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /"+path, 200, page(map[string]any{"id": 1}, map[string]any{"id": 2}))
		out, err := replaceRelation(context.Background(), s.client, path, []int{1, 2})
		if err != nil || len(out.Results) != 2 {
			t.Fatalf("out=%+v err=%v", out, err)
		}
		if calls := s.seen(); len(calls) != 1 {
			t.Fatalf("expected a single GET, got %v", calls)
		}
	})

	t.Run("order matters so a reorder is applied", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /"+path, 200, page(map[string]any{"id": 1}, map[string]any{"id": 2}))
		s.reply("POST /"+path, 204, nil)
		if _, err := replaceRelation(context.Background(), s.client, path, []int{2, 1}); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"GET /" + path,
			`POST /` + path + ` {"disassociate":true,"id":1}`,
			`POST /` + path + ` {"disassociate":true,"id":2}`,
			`POST /` + path + ` {"id":2}`,
			`POST /` + path + ` {"id":1}`,
			"GET /" + path,
		}
		got := s.seen()
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("empty ids clears the relation", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /"+path, 200, page(map[string]any{"id": 7}))
		s.reply("POST /"+path, 204, nil)
		if _, err := replaceRelation(context.Background(), s.client, path, nil); err != nil {
			t.Fatal(err)
		}
		if calls := s.seen(); len(calls) != 3 || !strings.Contains(calls[1], `"disassociate":true,"id":7`) {
			t.Fatalf("calls = %v", calls)
		}
	})

	t.Run("mid-sequence failure names the id", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /"+path, 200, page())
		s.on("POST /"+path, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["id"] == float64(99) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"detail":"no such instance group"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		_, err := replaceRelation(context.Background(), s.client, path, []int{1, 99, 2})
		if err == nil || !strings.Contains(err.Error(), "associate 99 failed") {
			t.Fatalf("err = %v", err)
		}
		var ae *ascender.Error
		if !errors.As(err, &ae) {
			t.Fatalf("underlying ascender.Error not wrapped: %v", err)
		}
	})
}

func TestLaunch(t *testing.T) {
	t.Run("resolves by name and sends only supplied overrides", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /job_templates", 200, page(map[string]any{"id": 12, "name": "deploy"}))
		s.reply("POST /job_templates/12/launch", 201, map[string]any{"job": 500})
		out, err := launch("job_templates")(context.Background(), s.client, launchArgs{
			IDOrName: "deploy", Limit: "web*", ExtraVars: map[string]any{"v": 1},
		})
		if err != nil || out["job"] != float64(500) {
			t.Fatalf("out=%v err=%v", out, err)
		}
		calls := s.seen()
		if !strings.Contains(calls[0], "GET /job_templates") {
			t.Fatalf("calls = %v", calls)
		}
		var body map[string]any
		json.Unmarshal([]byte(strings.TrimPrefix(calls[1], "POST /job_templates/12/launch ")), &body)
		if body["limit"] != "web*" || body["extra_vars"].(map[string]any)["v"] != float64(1) {
			t.Fatalf("body = %v", body)
		}
		for _, absent := range []string{"inventory", "instance_groups"} {
			if _, ok := body[absent]; ok {
				t.Fatalf("unset override %q sent as %v", absent, body[absent])
			}
		}
	})

	t.Run("numeric id skips the lookup", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /job_templates/12", 200, map[string]any{"id": 12})
		s.reply("POST /job_templates/12/launch", 201, map[string]any{"job": 1})
		_, err := launch("job_templates")(context.Background(), s.client, launchArgs{IDOrName: "12", InventoryID: 4, InstanceGroups: []int{2}})
		if err != nil {
			t.Fatal(err)
		}
		if calls := s.seen(); len(calls) != 2 || calls[1] != `POST /job_templates/12/launch {"instance_groups":[2],"inventory":4}` {
			t.Fatalf("calls = %v", calls)
		}
	})

	t.Run("unknown name does not launch", func(t *testing.T) {
		s := newStubAscender(t)
		s.reply("GET /job_templates", 200, page())
		_, err := launch("job_templates")(context.Background(), s.client, launchArgs{IDOrName: "missing"})
		var ae *ascender.Error
		if !errors.As(err, &ae) || ae.Status != 404 {
			t.Fatalf("err = %v", err)
		}
		if calls := s.seen(); len(calls) != 1 {
			t.Fatalf("launch POST sent despite failed resolve: %v", calls)
		}
	})
}

func TestEndpointKinds(t *testing.T) {
	for _, tc := range []struct {
		fn      func(string) (string, error)
		in      string
		want    string
		wantErr bool
	}{
		{templateEndpoint, "", "job_templates", false},
		{templateEndpoint, "template", "job_templates", false},
		{templateEndpoint, "workflow", "workflow_job_templates", false},
		{templateEndpoint, "Workflow", "", true},
		{jobEndpoint, "", "jobs", false},
		{jobEndpoint, "job", "jobs", false},
		{jobEndpoint, "workflow", "workflow_jobs", false},
		{jobEndpoint, "bogus", "", true},
	} {
		got, err := tc.fn(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("kind %q: got %q err=%v, want %q wantErr=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestQueryClamp(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 20}, {-5, 20}, {1, 1}, {200, 200}, {201, 200}, {9999, 200}} {
		if got := query(tc.in, 20).Get("page_size"); got != strconv.Itoa(tc.want) {
			t.Errorf("query(%d) page_size = %s, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSetHelpers(t *testing.T) {
	p := map[string]any{}
	set(p, "nil", (*string)(nil))
	empty := ""
	set(p, "empty", &empty)
	setIf(p, "zero", 0)
	setIf(p, "nonzero", 3)
	setIf(p, "false", false)
	if _, ok := p["nil"]; ok {
		t.Error("nil pointer should be omitted")
	}
	if v, ok := p["empty"]; !ok || v != "" {
		t.Error("pointer to empty string should be sent so the field can be cleared")
	}
	if _, ok := p["zero"]; ok {
		t.Error("setIf should omit zero values")
	}
	if _, ok := p["false"]; ok {
		t.Error("setIf should omit false")
	}
	if p["nonzero"] != 3 {
		t.Errorf("nonzero = %v", p["nonzero"])
	}
}
