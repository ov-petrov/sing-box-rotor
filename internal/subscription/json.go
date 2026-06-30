package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
)

// JSONSubscriptionParser validates and repackages a sing-box JSON subscription.
type JSONSubscriptionParser struct{}

// NewJSONSubscriptionParser creates a new JSONSubscriptionParser.
func NewJSONSubscriptionParser() *JSONSubscriptionParser {
	return &JSONSubscriptionParser{}
}

// Parse validates that body is a JSON object containing at least one of the
// required sing-box top-level keys ("outbounds" or "route") and returns it as
// a single CandidateConfig.
func (p *JSONSubscriptionParser) Parse(name, source string, body []byte) ([]CandidateConfig, error) {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if parsed == nil {
		return nil, errors.New("JSON body is not an object")
	}
	if _, hasOutbounds := parsed["outbounds"]; !hasOutbounds {
		if _, hasRoute := parsed["route"]; !hasRoute {
			return nil, errors.New("sing-box JSON missing outbounds or route")
		}
	}

	raw, _ := json.Marshal(parsed) // parsed was just successfully unmarshaled, cannot fail

	return []CandidateConfig{{
		Name:   name,
		Source: source,
		Raw:    raw,
		Parsed: parsed,
	}}, nil
}
