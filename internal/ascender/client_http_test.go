package ascender

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// recorded is one upstream request as the stub saw it.
type recorded struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

// newStub returns a Client pointed at an httptest server and a pointer
// to the slice of requests it has received. handler writes the
// response; if nil, the stub answers 200 {}.
func newStub(t *testing.T, handler http.HandlerFunc) (*Client, *[]recorded) {
	t.Helper()
	var seen []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		if r.Body != nil {
			b := make([]byte, 1<<16)
			n, _ := r.Body.Read(b)
			body.Write(b[:n])
		}
		seen = append(seen, recorded{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Header: r.Header.Clone(), Body: body.String(),
		})
		if handler == nil {
			w.Write([]byte(`{}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, false), &seen
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestRequestContract(t *testing.T) {
	t.Run("path gets api prefix and trailing slash", func(t *testing.T) {
		c, seen := newStub(t, nil)
		if _, err := c.Get(context.Background(), "jobs/7", nil); err != nil {
			t.Fatal(err)
		}
		if got := (*seen)[0].Path; got != "/api/v2/jobs/7/" {
			t.Fatalf("path = %q, want /api/v2/jobs/7/", got)
		}
	})

	t.Run("host trailing slash is trimmed", func(t *testing.T) {
		c, seen := newStub(t, nil)
		c = New(c.host+"/", false)
		if _, err := c.Get(context.Background(), "ping", nil); err != nil {
			t.Fatal(err)
		}
		if got := (*seen)[0].Path; got != "/api/v2/ping/" {
			t.Fatalf("path = %q, want /api/v2/ping/", got)
		}
	})

	t.Run("query params are encoded", func(t *testing.T) {
		c, seen := newStub(t, nil)
		c.Get(context.Background(), "jobs", map[string][]string{"name__exact": {"a b"}, "page_size": {"5"}})
		if got := (*seen)[0].Query; got != "name__exact=a+b&page_size=5" {
			t.Fatalf("query = %q", got)
		}
	})

	t.Run("bearer only when token set", func(t *testing.T) {
		c, seen := newStub(t, nil)
		c.Get(context.Background(), "ping", nil)
		if got := (*seen)[0].Header.Get("Authorization"); got != "" {
			t.Fatalf("anonymous client sent Authorization %q", got)
		}
		c.WithToken("tok").Get(context.Background(), "me", nil)
		if got := (*seen)[1].Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("Authorization = %q, want Bearer tok", got)
		}
		// WithToken must not mutate the original.
		c.Get(context.Background(), "ping", nil)
		if got := (*seen)[2].Header.Get("Authorization"); got != "" {
			t.Fatalf("WithToken leaked token into original client: %q", got)
		}
	})

	t.Run("json body sets content type and accept", func(t *testing.T) {
		c, seen := newStub(t, nil)
		c.Post(context.Background(), "jobs", map[string]any{"a": 1})
		r := (*seen)[0]
		if r.Method != http.MethodPost || r.Body != `{"a":1}` {
			t.Fatalf("got %s %q", r.Method, r.Body)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers = %v", r.Header)
		}
		c.Patch(context.Background(), "jobs/1", map[string]any{"b": true})
		if (*seen)[1].Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", (*seen)[1].Method)
		}
		c.Post(context.Background(), "jobs/1/cancel", nil)
		if r := (*seen)[2]; r.Body != "" || r.Header.Get("Content-Type") != "" {
			t.Fatalf("nil body should send no body or content type: %+v", r)
		}
	})

	t.Run("unmarshalable body errors before sending", func(t *testing.T) {
		c, seen := newStub(t, nil)
		_, err := c.Post(context.Background(), "jobs", map[string]any{"f": func() {}})
		if err == nil || len(*seen) != 0 {
			t.Fatalf("err = %v, requests = %d", err, len(*seen))
		}
	})
}

func TestResponseHandling(t *testing.T) {
	t.Run("204 and empty body decode to empty map", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		out, err := c.Get(context.Background(), "x", nil)
		if err != nil || out == nil || len(out) != 0 {
			t.Fatalf("out=%v err=%v", out, err)
		}
	})

	t.Run("non-JSON 200 is an Error not a panic", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) })
		_, err := c.Get(context.Background(), "x", nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Message != "non-JSON response" || ae.Status != 200 {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("4xx becomes Error with detail", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"detail":"You do not have permission"}`))
		})
		_, err := c.Get(context.Background(), "x", nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 403 || ae.Detail != "You do not have permission" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("context cancel and timeout map to transport errors", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Get(ctx, "x", nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Message != "request canceled" {
			t.Fatalf("canceled: err = %v", err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err = c.Get(ctx, "x", nil)
		if !errors.As(err, &ae) || ae.Message != "request timed out" {
			t.Fatalf("timeout: err = %v", err)
		}
	})

	t.Run("unreachable host is a generic transport error", func(t *testing.T) {
		c := New("http://127.0.0.1:1", false)
		_, err := c.Get(context.Background(), "x", nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Message != "transport error" || ae.Status != 0 {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("404 is success", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
		if err := c.Delete(context.Background(), "jobs/1"); err != nil {
			t.Fatalf("404 on delete should be nil, got %v", err)
		}
	})
	t.Run("other errors propagate", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) })
		var ae *Error
		if err := c.Delete(context.Background(), "jobs/1"); !errors.As(err, &ae) || ae.Status != 409 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("sends DELETE", func(t *testing.T) {
		c, seen := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		c.Delete(context.Background(), "jobs/1")
		if r := (*seen)[0]; r.Method != http.MethodDelete || r.Path != "/api/v2/jobs/1/" {
			t.Fatalf("got %s %s", r.Method, r.Path)
		}
	})
}

func TestMe(t *testing.T) {
	t.Run("unwraps envelope", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"count": 1, "results": []map[string]any{{"id": 3, "username": "admin"}}})
		})
		me, err := c.Me(context.Background())
		if err != nil || me["username"] != "admin" {
			t.Fatalf("me=%v err=%v", me, err)
		}
	})
	t.Run("empty results is 404", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"count": 0, "results": []any{}})
		})
		_, err := c.Me(context.Background())
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 404 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad shape is 500", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"results": []any{"not-an-object"}})
		})
		_, err := c.Me(context.Background())
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 500 {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGetText(t *testing.T) {
	t.Run("sets text accept and returns body", func(t *testing.T) {
		c, seen := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("PLAY [all]")) })
		text, trunc, err := c.GetText(context.Background(), "jobs/1/stdout", map[string][]string{"format": {"txt"}})
		if err != nil || trunc || text != "PLAY [all]" {
			t.Fatalf("text=%q trunc=%v err=%v", text, trunc, err)
		}
		if got := (*seen)[0].Header.Get("Accept"); got != "text/plain" {
			t.Fatalf("Accept = %q", got)
		}
	})
	t.Run("truncates at TextCap", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(strings.Repeat("x", TextCap+100)))
		})
		text, trunc, err := c.GetText(context.Background(), "jobs/1/stdout", nil)
		if err != nil || !trunc || len(text) != TextCap {
			t.Fatalf("len=%d trunc=%v err=%v", len(text), trunc, err)
		}
	})
	t.Run("exactly TextCap is not truncated", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(strings.Repeat("x", TextCap)))
		})
		_, trunc, err := c.GetText(context.Background(), "jobs/1/stdout", nil)
		if err != nil || trunc {
			t.Fatalf("trunc=%v err=%v", trunc, err)
		}
	})
	t.Run("4xx is an Error", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"detail":"Not found."}`))
		})
		_, _, err := c.GetText(context.Background(), "jobs/999/stdout", nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 404 {
			t.Fatalf("err = %v", err)
		}
	})
}

// pagedStub serves n pages of one result each, with "next" set on all
// but the last.
func pagedStub(t *testing.T, n int) (*Client, *[]recorded) {
	t.Helper()
	return newStub(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var next any
		if page != fmt.Sprint(n) {
			next = "/api/v2/x/?page=" + page + "1"
		}
		jsonOK(w, map[string]any{
			"count":   n,
			"next":    next,
			"results": []map[string]any{{"id": page}},
		})
	})
}

func TestList(t *testing.T) {
	t.Run("follows next until nil", func(t *testing.T) {
		c, seen := pagedStub(t, 3)
		page, err := c.List(context.Background(), "x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Results) != 3 || page.Total != 3 || page.Truncated {
			t.Fatalf("page = %+v", page)
		}
		if len(*seen) != 3 {
			t.Fatalf("made %d requests, want 3", len(*seen))
		}
		for i, r := range *seen {
			if want := fmt.Sprintf("page=%d", i+1); !strings.Contains(r.Query, want) {
				t.Fatalf("request %d query %q missing %s", i, r.Query, want)
			}
		}
	})

	t.Run("caps at five pages and flags truncation", func(t *testing.T) {
		c, seen := pagedStub(t, 20)
		page, err := c.List(context.Background(), "x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(*seen) != 5 || len(page.Results) != 5 || !page.Truncated || page.Total != 20 {
			t.Fatalf("requests=%d page=%+v", len(*seen), page)
		}
	})

	t.Run("does not mutate caller params", func(t *testing.T) {
		c, seen := pagedStub(t, 1)
		params := map[string][]string{"search": {"web"}}
		if _, err := c.List(context.Background(), "x", params); err != nil {
			t.Fatal(err)
		}
		if _, ok := params["page"]; ok {
			t.Fatal("List wrote page into the caller's url.Values")
		}
		if !strings.Contains((*seen)[0].Query, "search=web") {
			t.Fatalf("query = %q, caller params dropped", (*seen)[0].Query)
		}
	})

	t.Run("skips non-object results", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"count": 2, "next": nil, "results": []any{"junk", map[string]any{"id": 1}}})
		})
		page, err := c.List(context.Background(), "x", nil)
		if err != nil || len(page.Results) != 1 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
	})

	t.Run("upstream error aborts", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
		_, err := c.List(context.Background(), "x", nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 502 {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestResolve(t *testing.T) {
	t.Run("numeric id hits detail path", func(t *testing.T) {
		c, seen := newStub(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, map[string]any{"id": 42}) })
		rec, err := c.Resolve(context.Background(), "projects", "42")
		if err != nil || rec["id"] != float64(42) {
			t.Fatalf("rec=%v err=%v", rec, err)
		}
		if r := (*seen)[0]; r.Path != "/api/v2/projects/42/" || r.Query != "" {
			t.Fatalf("got %s?%s", r.Path, r.Query)
		}
	})

	t.Run("name uses exact filter and returns first", func(t *testing.T) {
		c, seen := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"count": 2, "results": []map[string]any{{"id": 1, "name": "site"}, {"id": 2, "name": "site"}}})
		})
		rec, err := c.Resolve(context.Background(), "projects", "site")
		if err != nil || rec["id"] != float64(1) {
			t.Fatalf("rec=%v err=%v", rec, err)
		}
		if r := (*seen)[0]; r.Path != "/api/v2/projects/" || r.Query != "name__exact=site" {
			t.Fatalf("got %s?%s", r.Path, r.Query)
		}
	})

	t.Run("no match is 404 naming the endpoint", func(t *testing.T) {
		c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"count": 0, "results": []any{}})
		})
		_, err := c.Resolve(context.Background(), "projects", "nope")
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 404 || !strings.Contains(ae.Detail, `projects "nope"`) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNewInsecure(t *testing.T) {
	// A TLS stub with a self-signed cert: the strict client must fail,
	// the insecure one must succeed.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	if _, err := New(srv.URL, false).Ping(context.Background()); err == nil {
		t.Fatal("strict client accepted a self-signed certificate")
	}
	if _, err := New(srv.URL, true).Ping(context.Background()); err != nil {
		t.Fatalf("insecure client rejected self-signed certificate: %v", err)
	}
}
