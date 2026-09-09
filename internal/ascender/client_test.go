package ascender

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"under cap unchanged", "hello", 10, "hello"},
		{"exactly cap unchanged", "hello", 5, "hello"},
		{"over cap appends ellipsis", "hello world", 5, "hello…"},
		{"zero cap", "abc", 0, "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.in, tc.max); got != tc.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// truncate must count runes, not bytes, so multi-byte sequences are
// never split into invalid UTF-8.
func TestTruncateRuneSafe(t *testing.T) {
	// 5 three-byte runes; cap at 3 must keep 3 whole runes + ellipsis.
	in := strings.Repeat("世", 5)
	got := truncate(in, 3)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate split a multi-byte rune: %q is not valid UTF-8", got)
	}
	if n := utf8.RuneCountInString(got); n != 4 { // 3 kept + 1 ellipsis
		t.Fatalf("rune count = %d, want 4 (3 kept + ellipsis)", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want ellipsis suffix, got %q", got)
	}
}

func TestID(t *testing.T) {
	cases := []struct {
		name    string
		record  map[string]any
		want    int
		wantErr bool
	}{
		{"float64 (json default)", map[string]any{"id": float64(42)}, 42, false},
		{"large id no sci-notation", map[string]any{"id": float64(2000000000)}, 2000000000, false},
		{"missing id", map[string]any{"name": "x"}, 0, true},
		{"non-numeric id", map[string]any{"id": "12"}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ID(tc.record)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ID(%v) = %d, want error", tc.record, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ID(%v) unexpected error: %v", tc.record, err)
			}
			if got != tc.want {
				t.Fatalf("ID(%v) = %d, want %d", tc.record, got, tc.want)
			}
		})
	}
}

func mkResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestReadAPIError(t *testing.T) {
	t.Run("lifts detail field", func(t *testing.T) {
		err := readAPIError(mkResp(400, `{"detail":"inventory is required"}`))
		ae, ok := err.(*Error)
		if !ok {
			t.Fatalf("want *Error, got %T", err)
		}
		if ae.Status != 400 || ae.Detail != "inventory is required" {
			t.Fatalf("got status=%d detail=%q", ae.Status, ae.Detail)
		}
	})

	t.Run("lifts first field error", func(t *testing.T) {
		err := readAPIError(mkResp(400, `{"name":["already exists"]}`))
		ae := err.(*Error)
		if ae.Detail != "name: already exists" {
			t.Fatalf("detail = %q, want %q", ae.Detail, "name: already exists")
		}
	})

	t.Run("drops 5xx non-JSON body", func(t *testing.T) {
		err := readAPIError(mkResp(500, "<html>stack trace leak</html>"))
		ae := err.(*Error)
		if ae.Detail != "" {
			t.Fatalf("5xx body should not leak into Detail, got %q", ae.Detail)
		}
		if ae.Message != http.StatusText(500) {
			t.Fatalf("message = %q, want %q", ae.Message, http.StatusText(500))
		}
	})

	t.Run("empty body", func(t *testing.T) {
		err := readAPIError(mkResp(404, ""))
		ae := err.(*Error)
		if ae.Detail != "" || ae.Status != 404 {
			t.Fatalf("got status=%d detail=%q", ae.Status, ae.Detail)
		}
	})

	t.Run("caps long detail at 500 runes", func(t *testing.T) {
		body := `{"detail":"` + strings.Repeat("a", 600) + `"}`
		err := readAPIError(mkResp(400, body))
		ae := err.(*Error)
		if n := utf8.RuneCountInString(ae.Detail); n != 501 { // 500 + ellipsis
			t.Fatalf("detail rune count = %d, want 501", n)
		}
		if !strings.HasSuffix(ae.Detail, "…") {
			t.Fatalf("long detail should be ellipsis-truncated")
		}
	})
}

func TestErrorString(t *testing.T) {
	withDetail := (&Error{Status: 400, Message: "Bad Request", Detail: "x required"}).Error()
	if withDetail != "[400] Bad Request: x required" {
		t.Fatalf("got %q", withDetail)
	}
	noDetail := (&Error{Status: 404, Message: "Not Found"}).Error()
	if noDetail != "[404] Not Found" {
		t.Fatalf("got %q", noDetail)
	}
}
