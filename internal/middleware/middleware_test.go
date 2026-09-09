package middleware

import (
	"encoding/json"
	"testing"
)

func TestFingerprint(t *testing.T) {
	const token = "secret-bearer-token"
	fp := fingerprint(token)
	if len(fp) != 32 {
		t.Fatalf("fingerprint len = %d, want 32 hex chars", len(fp))
	}
	if fingerprint(token) != fp {
		t.Fatal("fingerprint is not deterministic")
	}
	if fingerprint("different-token") == fp {
		t.Fatal("distinct tokens collided")
	}
}

func TestRedactArgs(t *testing.T) {
	if got := redactArgs(nil); got != nil {
		t.Fatalf("redactArgs(nil) = %v, want nil", got)
	}
	if got := redactArgs(json.RawMessage(`{}`)); got != nil {
		t.Fatalf("redactArgs(empty) = %v, want nil", got)
	}
	out := redactArgs(json.RawMessage(`{"name":"deploy","password":"hunter2","extra_vars":{"k":"v"},"page_size":20}`))
	if out["name"] != "deploy" || out["page_size"] != float64(20) {
		t.Fatalf("non-sensitive args altered: %v", out)
	}
	for _, k := range []string{"password", "extra_vars"} {
		if out[k] != "[redacted]" {
			t.Fatalf("sensitive key %q = %v, want [redacted]", k, out[k])
		}
	}
}
