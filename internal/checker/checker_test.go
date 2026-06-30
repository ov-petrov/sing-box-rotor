package checker

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProbeSuccessAndStatusFailure(t *testing.T) {
	c := NewWithClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusNoContent
		if r.URL.Path == "/bad" {
			status = http.StatusTeapot
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}, http.MethodHead)
	if latency, err := c.Probe(context.Background(), "https://example.com/ok"); err != nil || latency <= 0 {
		t.Fatalf("latency=%s err=%v", latency, err)
	}
	if _, err := c.Probe(context.Background(), "https://example.com/bad"); err == nil {
		t.Fatal("expected status error")
	}
}

func TestNewRejectsUnsupportedProxy(t *testing.T) {
	if _, err := New("ftp://127.0.0.1:1", time.Second, http.MethodGet); err == nil {
		t.Fatal("expected unsupported proxy error")
	}
}
