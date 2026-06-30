package subscription

import (
	"encoding/json"
	"fmt"
)

func BuildCandidate(name, source string, link Link) (CandidateConfig, error) {
	outbound := map[string]any{
		"type":        link.Type,
		"tag":         "proxy",
		"server":      link.Server,
		"server_port": link.Port,
	}
	switch link.Type {
	case "vmess", "vless":
		outbound["uuid"] = link.UUID
	case "trojan":
		outbound["password"] = link.Password
	case "shadowsocks":
		outbound["method"] = link.Method
		outbound["password"] = link.Password
	default:
		return CandidateConfig{}, fmt.Errorf("unsupported link type %q", link.Type)
	}
	parsed := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"outbounds": []any{outbound, map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "proxy"},
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		return CandidateConfig{}, err
	}
	if link.Tag != "" {
		name = name + "/" + link.Tag
	}
	return CandidateConfig{Name: name, Source: source, Raw: raw, Parsed: parsed}, nil
}
