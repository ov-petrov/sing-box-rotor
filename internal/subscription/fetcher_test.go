package subscription

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/config"
)

// fakeDoer implements HTTPDoer and records calls.
type fakeDoer struct {
	responses []*http.Response
	errs      []error
	callIdx   int
	calls     []*http.Request
}

func (f *fakeDoer) Do(r *http.Request) (*http.Response, error) {
	f.calls = append(f.calls, r)
	if f.errs != nil && f.callIdx < len(f.errs) && f.errs[f.callIdx] != nil {
		err := f.errs[f.callIdx]
		f.callIdx++
		return nil, err
	}
	if f.callIdx >= len(f.responses) {
		return nil, errors.New("no more responses")
	}
	resp := f.responses[f.callIdx]
	f.callIdx++
	return resp, nil
}

func newJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newBase64Response(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newErrorResponse(code int) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("error")),
	}
}

// stub parsers
type stubJSON struct{ out []CandidateConfig; err error }

func (s stubJSON) Parse(name, source string, body []byte) ([]CandidateConfig, error) {
	return s.out, s.err
}

type stubB64 struct{ out []CandidateConfig; err error }

func (s stubB64) Parse(name, source string, body []byte) ([]CandidateConfig, error) {
	return s.out, s.err
}

type parserFuncJSON func(name, src string, body []byte) ([]CandidateConfig, error)

func (p parserFuncJSON) Parse(n, s string, b []byte) ([]CandidateConfig, error) { return p(n, s, b) }

type parserFuncB64 func(name, src string, body []byte) ([]CandidateConfig, error)

func (p parserFuncB64) Parse(n, s string, b []byte) ([]CandidateConfig, error) { return p(n, s, b) }

func newFetcher(d HTTPDoer, jp JSONParser, bp Base64Parser) *Fetcher {
	return NewFetcher(d, 100*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)), jp, bp)
}

func TestFetch_Empty(t *testing.T) {
	f := newFetcher(&fakeDoer{}, stubJSON{}, stubB64{})
	got, err := f.Fetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFetch_DispatchesByType(t *testing.T) {
	jsonCalled := atomic.Int32{}
	b64Called := atomic.Int32{}
	jp := parserFuncJSON(func(name, src string, body []byte) ([]CandidateConfig, error) {
		jsonCalled.Add(1)
		return []CandidateConfig{{Name: name + "-j", Source: src, Raw: body, Parsed: map[string]any{}}}, nil
	})
	bp := parserFuncB64(func(name, src string, body []byte) ([]CandidateConfig, error) {
		b64Called.Add(1)
		return []CandidateConfig{{Name: name + "-b", Source: src, Raw: body, Parsed: map[string]any{}}}, nil
	})
	d := &fakeDoer{responses: []*http.Response{
		newJSONResponse(`{"outbounds":[]}`),
		newBase64Response("base64body"),
	}}
	f := newFetcher(d, jp, bp)
	subs := []config.Subscription{
		{Name: "a", URL: "https://e.com/a", Type: "sing-box-json"},
		{Name: "b", URL: "https://e.com/b", Type: "base64"},
	}
	got, err := f.Fetch(context.Background(), subs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}
	if jsonCalled.Load() != 1 || b64Called.Load() != 1 {
		t.Fatalf("parsers not called: json=%d b64=%d", jsonCalled.Load(), b64Called.Load())
	}
}

func TestFetch_SkipsFailedDownload(t *testing.T) {
	d := &fakeDoer{errs: []error{errors.New("boom")}}
	jp := stubJSON{}
	bp := stubB64{}
	f := newFetcher(d, jp, bp)
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "base64"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error when all fail")
	}
}

func TestFetch_SkipsFailedParse(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{newJSONResponse("x")}}
	jp := stubJSON{err: errors.New("parse fail")}
	bp := stubB64{}
	f := newFetcher(d, jp, bp)
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "sing-box-json"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected all-failed error")
	}
}

func TestFetch_PartialSuccess(t *testing.T) {
	d := &fakeDoer{
		responses: []*http.Response{
			newJSONResponse(`{"outbounds":[]}`),
		},
		errs: []error{nil, errors.New("boom")},
	}
	jp := stubJSON{out: []CandidateConfig{{Name: "ok", Raw: []byte("{}")}}}
	f := newFetcher(d, jp, stubB64{})
	subs := []config.Subscription{
		{Name: "ok", URL: "https://e.com/ok", Type: "sing-box-json"},
		{Name: "bad", URL: "https://e.com/bad", Type: "sing-box-json"},
	}
	got, err := f.Fetch(context.Background(), subs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
}

func TestFetch_SetsUserAgent(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{newJSONResponse("{}")}}
	jp := stubJSON{out: []CandidateConfig{{Name: "x", Raw: []byte("{}")}}}
	f := newFetcher(d, jp, stubB64{})
	_, _ = f.Fetch(context.Background(), []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "sing-box-json"}})
	if len(d.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(d.calls))
	}
	if got := d.calls[0].Header.Get("User-Agent"); got == "" {
		t.Fatal("missing User-Agent")
	}
}

func TestFetch_ContextCanceled(t *testing.T) {
	d := &fakeDoer{}
	f := newFetcher(d, stubJSON{}, stubB64{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Fetch(ctx, []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "base64"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetch_UnknownType(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{newJSONResponse("{}")}}
	f := newFetcher(d, stubJSON{}, stubB64{})
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "yaml"}}
	got, err := f.Fetch(context.Background(), subs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 candidates for unknown type, got %d", len(got))
	}
}

func TestFetch_Non2xxStatus(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{newErrorResponse(http.StatusServiceUnavailable)}}
	f := newFetcher(d, stubJSON{}, stubB64{})
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "sing-box-json"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
}

func TestFetch_BodyTooLarge(t *testing.T) {
	huge := strings.Repeat("x", MaxBodySize+100)
	d := &fakeDoer{responses: []*http.Response{newBase64Response(huge)}}
	f := newFetcher(d, stubJSON{}, stubB64{})
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "base64"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
}

func TestFetch_NilJSONParser(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{newJSONResponse("{}")}}
	f := newFetcher(d, nil, stubB64{})
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "sing-box-json"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error when JSON parser is nil")
	}
}

func TestFetch_NilBase64Parser(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{newBase64Response("eHh4")}}
	f := newFetcher(d, stubJSON{}, nil)
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "base64"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error when base64 parser is nil")
	}
}

func TestNewFetcher_Defaults(t *testing.T) {
	f := NewFetcher(nil, 0, nil, stubJSON{}, stubB64{})
	if f.client == nil {
		t.Fatal("expected default client")
	}
	if f.timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", f.timeout)
	}
	if f.log == nil {
		t.Fatal("expected default logger")
	}
}

func TestNewFetcher_NegativeTimeout(t *testing.T) {
	f := NewFetcher(&fakeDoer{}, -5*time.Second, nil, stubJSON{}, stubB64{})
	if f.timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", f.timeout)
	}
}

func TestFetch_RealHTTP(t *testing.T) {
	called := atomic.Int32{}
	 srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"outbounds":[]}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	jp := stubJSON{out: []CandidateConfig{{Name: "real", Raw: []byte(`{"outbounds":[]}`)}}}
	f := NewFetcher(client, 5*time.Second, nil, jp, stubB64{})
	subs := []config.Subscription{{Name: "real", URL: srv.URL, Type: "sing-box-json"}}
	got, err := f.Fetch(context.Background(), subs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	if called.Load() != 1 {
		t.Fatalf("server called %d times", called.Load())
	}
}

func TestFetch_InvalidURL(t *testing.T) {
	f := newFetcher(&fakeDoer{}, stubJSON{}, stubB64{})
	subs := []config.Subscription{{Name: "a", URL: "://invalid", Type: "sing-box-json"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// errReader is an io.ReadCloser that always returns an error on Read.
type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }
func (e errReader) Close() error                { return nil }

func TestFetch_ReadBodyError(t *testing.T) {
	d := &fakeDoer{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       errReader{err: errors.New("read error")},
	}}}
	f := newFetcher(d, stubJSON{}, stubB64{})
	subs := []config.Subscription{{Name: "a", URL: "https://e.com/a", Type: "sing-box-json"}}
	_, err := f.Fetch(context.Background(), subs)
	if err == nil {
		t.Fatal("expected error for body read error")
	}
}
