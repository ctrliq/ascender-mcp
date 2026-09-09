package tools

import "testing"

func TestSetJSONDistinguishesOmittedFromEmpty(t *testing.T) {
	p := map[string]any{}
	setJSON(p, "omitted", nil)
	setJSON(p, "cleared", map[string]any{})
	setJSON(p, "set", map[string]any{"a": 1})
	if _, ok := p["omitted"]; ok {
		t.Fatal("nil map should leave the field alone")
	}
	if p["cleared"] != "{}" {
		t.Fatalf("empty map should clear the field, got %v", p["cleared"])
	}
	if p["set"] != `{"a":1}` {
		t.Fatalf("got %v", p["set"])
	}
}
