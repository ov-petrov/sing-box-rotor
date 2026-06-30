package subscription

import (
	"strings"
	"testing"
)

func TestJSON_Valid(t *testing.T) {
	body := []byte(`{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[]}}`)
	p := NewJSONSubscriptionParser()
	candidates, err := p.Parse("sub", "https://e.com/sub", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].Name != "sub" {
		t.Errorf("Name = %q", candidates[0].Name)
	}
	if candidates[0].Source != "https://e.com/sub" {
		t.Errorf("Source = %q", candidates[0].Source)
	}
	if len(candidates[0].Raw) == 0 {
		t.Error("Raw is empty")
	}
	if candidates[0].Parsed == nil {
		t.Error("Parsed is nil")
	}
}

func TestJSON_ValidRouteOnly(t *testing.T) {
	body := []byte(`{"route":{"rules":[]}}`)
	p := NewJSONSubscriptionParser()
	candidates, err := p.Parse("sub", "https://e.com/sub", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
}

func TestJSON_InvalidJSON(t *testing.T) {
	body := []byte(`{not json`)
	p := NewJSONSubscriptionParser()
	_, err := p.Parse("sub", "https://e.com/sub", body)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse JSON") {
		t.Errorf("error %q should mention parse JSON", err)
	}
}

func TestJSON_Array(t *testing.T) {
	body := []byte(`[1,2,3]`)
	p := NewJSONSubscriptionParser()
	_, err := p.Parse("sub", "https://e.com/sub", body)
	if err == nil {
		t.Fatal("expected error for array JSON")
	}
	if !strings.Contains(err.Error(), "parse JSON") {
		t.Errorf("error %q should mention parse JSON", err)
	}
}

func TestJSON_Null(t *testing.T) {
	body := []byte(`null`)
	p := NewJSONSubscriptionParser()
	_, err := p.Parse("sub", "https://e.com/sub", body)
	if err == nil {
		t.Fatal("expected error for null JSON")
	}
	if !strings.Contains(err.Error(), "not an object") {
		t.Errorf("error %q should mention not an object", err)
	}
}

func TestJSON_MissingRequiredKeys(t *testing.T) {
	body := []byte(`{"log":{"level":"info"}}`)
	p := NewJSONSubscriptionParser()
	_, err := p.Parse("sub", "https://e.com/sub", body)
	if err == nil {
		t.Fatal("expected error for missing outbounds/route")
	}
	if !strings.Contains(err.Error(), "missing outbounds or route") {
		t.Errorf("error %q should mention missing outbounds or route", err)
	}
}
