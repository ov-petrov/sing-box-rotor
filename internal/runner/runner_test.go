package runner

import (
	"encoding/json"
	"testing"

	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

func TestMutateForTestInjectsOnlyLocalSocksInbound(t *testing.T) {
	raw := []byte(`{"inbounds":[{"type":"mixed"}],"experimental":{},"api":{},"outbounds":[{"type":"direct"}]}`)
	got, err := mutateForTestPort(subscription.CandidateConfig{Raw: raw}, 28080)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["experimental"]; ok {
		t.Fatal("experimental should be removed")
	}
	inbounds := parsed["inbounds"].([]any)
	first := inbounds[0].(map[string]any)
	if first["type"] != "socks" || first["listen"] != "127.0.0.1" {
		t.Fatalf("bad inbound: %+v", first)
	}
}
