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

const maxSubscriptionBody = 10 << 20

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type JSONParser interface {
	Parse(name, source string, body []byte) ([]CandidateConfig, error)
}

type Base64Parser interface {
	Parse(name, source string, body []byte) ([]CandidateConfig, error)
}

type Fetcher struct {
	client    HTTPDoer
	timeout   time.Duration
	userAgent string
	log       *slog.Logger
	json      JSONParser
	base64    Base64Parser
}

func NewFetcher(client HTTPDoer, timeout time.Duration, log *slog.Logger, jp JSONParser, bp Base64Parser) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	if jp == nil {
		jp = JSON{}
	}
	if bp == nil {
		bp = Base64{}
	}
	return &Fetcher{
		client:    client,
		timeout:   timeout,
		userAgent: "sing-box-rotor/0.1",
		log:       log,
		json:      jp,
		base64:    bp,
	}
}

func (f *Fetcher) Fetch(ctx context.Context, subs []config.Subscription) ([]CandidateConfig, error) {
	var candidates []CandidateConfig
	failures := 0
	for _, sub := range subs {
		body, err := f.download(ctx, sub.URL)
		if err != nil {
			failures++
			f.log.Warn("subscription fetch failed", "name", sub.Name, "error", err)
			continue
		}
		var parsed []CandidateConfig
		switch sub.Type {
		case "sing-box-json":
			parsed, err = f.json.Parse(sub.Name, sub.URL, body)
		case "base64":
			parsed, err = f.base64.Parse(sub.Name, sub.URL, body)
		default:
			err = fmt.Errorf("unsupported subscription type %q", sub.Type)
		}
		if err != nil {
			failures++
			f.log.Warn("subscription parse failed", "name", sub.Name, "error", err)
			continue
		}
		candidates = append(candidates, parsed...)
	}
	if len(candidates) == 0 && failures == len(subs) {
		return nil, errors.New("all subscriptions failed")
	}
	return candidates, nil
}

func (f *Fetcher) download(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSubscriptionBody {
		return nil, errors.New("subscription body exceeds 10 MiB")
	}
	return body, nil
}
