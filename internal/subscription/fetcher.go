// Package subscription fetches proxy subscriptions and converts them into
// runnable sing-box candidate configurations.
package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/config"
)

// MaxBodySize limits how much data we will read from a subscription URL to
// protect against misbehaving or malicious endpoints.
const MaxBodySize = 10 << 20 // 10 MiB

// CandidateConfig is a single runnable sing-box configuration produced from a
// subscription. It is the unit consumed by runner, checker, and selector.
type CandidateConfig struct {
	Name   string
	Source string
	Raw    []byte
	Parsed map[string]any
}

// HTTPDoer is the subset of *http.Client used by Fetcher. Tests inject a fake
// implementation so no real network traffic is required.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// JSONParser converts a sing-box JSON subscription body into candidates.
type JSONParser interface {
	Parse(name, source string, body []byte) ([]CandidateConfig, error)
}

// Base64Parser converts a base64-encoded proxy list into candidates.
type Base64Parser interface {
	Parse(name, source string, body []byte) ([]CandidateConfig, error)
}

// Fetcher downloads subscription URLs and dispatches them to parsers.
type Fetcher struct {
	client    HTTPDoer
	timeout   time.Duration
	maxRedir  int
	userAgent string
	log       *slog.Logger
	json      JSONParser
	b64       Base64Parser
}

// NewFetcher creates a Fetcher. If client is nil, an http.Client with sensible
// defaults is used. If timeout is zero, 30s is used.
func NewFetcher(client HTTPDoer, timeout time.Duration, log *slog.Logger, jp JSONParser, bp Base64Parser) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Fetcher{
		client:    client,
		timeout:   timeout,
		maxRedir:  10,
		userAgent: "sing-box-rotor/1.0",
		log:       log,
		json:      jp,
		b64:       bp,
	}
}

// Fetch downloads every subscription and converts it to CandidateConfigs.
// Download failures and parser failures for individual subscriptions are
// logged and skipped. An error is returned only if every subscription fails
// to download.
func (f *Fetcher) Fetch(ctx context.Context, subs []config.Subscription) ([]CandidateConfig, error) {
	if len(subs) == 0 {
		return nil, nil
	}

	var all []CandidateConfig
	failures := 0

	for _, sub := range subs {
		candidates, err := f.fetchOne(ctx, sub)
		if err != nil {
			failures++
			f.log.Warn("subscription fetch failed",
				slog.String("name", sub.Name),
				slog.String("type", sub.Type),
				slog.Any("error", err),
			)
			continue
		}
		all = append(all, candidates...)
	}

	if failures > 0 && failures == len(subs) {
		return all, errors.New("all subscriptions failed to fetch")
	}
	return all, nil
}

func (f *Fetcher) fetchOne(ctx context.Context, sub config.Subscription) ([]CandidateConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) > MaxBodySize {
		return nil, fmt.Errorf("body exceeds %d bytes", MaxBodySize)
	}

	switch sub.Type {
	case "sing-box-json":
		if f.json == nil {
			return nil, errors.New("no JSON parser configured")
		}
		return f.json.Parse(sub.Name, sub.URL, body)
	case "base64":
		if f.b64 == nil {
			return nil, errors.New("no base64 parser configured")
		}
		return f.b64.Parse(sub.Name, sub.URL, body)
	default:
		// Config validation should prevent this, but treat it as a skipped
		// subscription rather than failing the whole run.
		f.log.Warn("unknown subscription type, skipping",
			slog.String("name", sub.Name),
			slog.String("type", sub.Type),
		)
		return nil, nil
	}
}
