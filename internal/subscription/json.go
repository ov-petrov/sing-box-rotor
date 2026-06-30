package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
)

type JSON struct{}

func (JSON) Parse(name, source string, body []byte) ([]CandidateConfig, error) {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("invalid sing-box JSON: %w", err)
	}
	if _, ok := parsed["outbounds"]; !ok {
		if _, ok := parsed["route"]; !ok {
			return nil, errors.New("sing-box JSON must contain outbounds or route")
		}
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	return []CandidateConfig{{Name: name, Source: source, Raw: raw, Parsed: parsed}}, nil
}
