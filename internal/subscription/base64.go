package subscription

import (
	"encoding/base64"
	"fmt"
	"strings"
)

type Base64 struct{}

func (Base64) Parse(name, source string, body []byte) ([]CandidateConfig, error) {
	decoded, err := decodeBase64Subscription(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, err
	}
	lines := strings.Fields(decoded)
	var out []CandidateConfig
	for i, line := range lines {
		node, err := ParseLink(line)
		if err != nil {
			continue
		}
		c, err := BuildCandidate(fmt.Sprintf("%s/node-%d", name, i+1), source, node)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no supported proxy links found")
	}
	return out, nil
}

func decodeBase64Subscription(s string) (string, error) {
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range decoders {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("invalid base64 subscription")
}
