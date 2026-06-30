package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/config"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

type parserFunc func(string, string, []byte) ([]CandidateConfig, error)

func (f parserFunc) Parse(n, s string, b []byte) ([]CandidateConfig, error) {
	return f(n, s, b)
}

func response(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestFetcherDispatchesByType(t *testing.T) {
	var seen []string
	f := NewFetcher(doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("missing user agent")
		}
		return response("body"), nil
	}), time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)),
		parserFunc(func(name, source string, body []byte) ([]CandidateConfig, error) {
			seen = append(seen, "json:"+name)
			return []CandidateConfig{{Name: name, Source: source, Raw: body}}, nil
		}),
		parserFunc(func(name, source string, body []byte) ([]CandidateConfig, error) {
			seen = append(seen, "base64:"+name)
			return []CandidateConfig{{Name: name, Source: source, Raw: body}}, nil
		}),
	)
	got, err := f.Fetch(context.Background(), []config.Subscription{
		{Name: "a", URL: "https://example.com/a", Type: "sing-box-json"},
		{Name: "b", URL: "https://example.com/b", Type: "base64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || strings.Join(seen, ",") != "json:a,base64:b" {
		t.Fatalf("bad dispatch: got=%v seen=%v", got, seen)
	}
}

func TestJSONParser(t *testing.T) {
	got, err := (JSON{}).Parse("sub", "https://example.com", []byte(`{"outbounds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "sub" || !json.Valid(got[0].Raw) {
		t.Fatalf("bad candidate: %+v", got[0])
	}
	if _, err := (JSON{}).Parse("bad", "", []byte(`{"dns":{}}`)); err == nil {
		t.Fatal("expected missing route/outbounds error")
	}
}

func TestBase64ParserBuildsCandidates(t *testing.T) {
	link := "trojan://secret@example.com:443#node"
	body := base64.StdEncoding.EncodeToString([]byte(link))
	got, err := (Base64{}).Parse("sub", "https://example.com", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Name, "node") || !json.Valid(got[0].Raw) {
		t.Fatalf("bad candidates: %+v", got)
	}
}

func TestParseLinks(t *testing.T) {
	cases := []string{
		"vless://uuid@example.com:443#vless-node",
		"trojan://secret@example.com:443#trojan-node",
		"ss://aes-128-gcm:secret@example.com:8388#ss-node",
	}
	for _, tc := range cases {
		if _, err := ParseLink(tc); err != nil {
			t.Fatalf("%s: %v", tc, err)
		}
	}
}
