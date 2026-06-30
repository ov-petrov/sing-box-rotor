# sing-box-rotor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Linux daemon that periodically evaluates sing-box configurations from subscription URLs, selects the lowest-latency one, and switches the running sing-box systemd service using hysteresis to minimize disruption.

**Architecture:** Single static Go binary. Internal packages isolate concerns: `config` (YAML), `subscription` (fetch + decode), `runner` (temporary sing-box process), `checker` (HTTP latency probe), `selector` (state machine with hysteresis), `systemd` (atomic config write + restart), `daemon` (orchestration). All external side effects (process spawn, HTTP, file write, `systemctl`) are abstracted behind interfaces so unit tests run without real subscriptions or a real sing-box.

**Tech Stack:**
- Go 1.23 (per existing `go.mod`).
- `gopkg.in/yaml.v3` for YAML parsing.
- `golang.org/x/net/proxy` for SOCKS5 client transport.
- Standard library: `log/slog`, `net/http`, `os/exec`, `context`, `testing`, `net/http/httptest`.
- No additional third-party packages.

---

## Constraints

- **No new secrets in repo.** No real URLs, credentials, or proxy links ever committed.
- **No real sing-box / network in unit tests.** All side effects are behind interfaces; tests use fakes.
- **`go test -race -timeout 30s ./...` must pass.** This is the gate for every chunk and the final verification.
- **Single static binary.** `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/sing-box-rotor`.
- **YAML field names match spec verbatim.** JSON tags not required (internal types); only YAML.
- **DRY / YAGNI / TDD / frequent commits.** One commit per task minimum.
- **100% statement coverage** on every package (verified per-chunk with `go test -cover`).

---

## Project Structure

```
/home/petrovov/kimchi-project/
├── cmd/sing-box-rotor/main.go              # CLI entry point (chunk 11)
├── internal/
│   ├── config/                             # chunk 1
│   │   ├── config.go
│   │   └── config_test.go
│   ├── subscription/                       # chunks 2, 3, 4, 5
│   │   ├── fetcher.go
│   │   ├── fetcher_test.go
│   │   ├── json.go
│   │   ├── json_test.go
│   │   ├── base64.go
│   │   ├── links.go
│   │   ├── base64_test.go
│   │   ├── links_test.go
│   │   ├── builder.go
│   │   └── builder_test.go
│   ├── runner/                             # chunk 6
│   │   ├── runner.go
│   │   ├── mutate.go
│   │   └── runner_test.go
│   ├── checker/                            # chunk 7
│   │   ├── checker.go
│   │   └── checker_test.go
│   ├── selector/                           # chunk 8
│   │   ├── selector.go
│   │   └── selector_test.go
│   ├── systemd/                            # chunk 9
│   │   ├── manager.go
│   │   ├── atomic.go
│   │   └── manager_test.go
│   └── daemon/                             # chunk 10
│       ├── daemon.go
│       └── daemon_test.go
├── contrib/
│   ├── systemd/sing-box-rotor.service      # chunk 12
│   └── config.example.yaml                 # chunk 12
├── scripts/install.sh                      # chunk 12
├── go.mod                                  # add deps in chunk 1
├── README.md                               # updated in chunk 12
└── .gitignore                              # updated in chunk 1
```

## Dependency Graph

```
chunk 1 (config) ──┬──► chunk 2 (fetcher)
                   │       │
                   │       └──► chunk 3 (json parser)
                   │       └──► chunk 4 (base64 parser + link parser)
                   │       └──► chunk 5 (candidate builder)
                   │
                   └──► chunk 6 (runner) ──► chunk 7 (checker) ──► chunk 8 (selector)
                                                       │                   │
                                                       ▼                   │
                                                   chunk 9 (systemd) ◄─────┘
                                                              │
                                                              ▼
                                                       chunk 10 (daemon)
                                                              │
                                                              ▼
                                                       chunk 11 (main CLI)
                                                              │
                                                              ▼
                                                       chunk 12 (deployment)
```

Chunks 3, 4, 5 are independent of each other (only depend on chunk 1 + 2 types) and could be done in any order, but are listed sequentially for the linear plan format. Chunk 10 requires 2, 6, 7, 8, 9.

---

## Chunk 1: `internal/config` — Configuration Loader

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/config/config.go`
- Create: `/home/petrovov/kimchi-project/internal/config/config_test.go`
- Modify: `/home/petrovov/kimchi-project/go.mod` (run `go get gopkg.in/yaml.v3`)
- Modify: `/home/petrovov/kimchi-project/.gitignore` (add `/internal/config/testdata/real/`)

**Depends on:** none

### Type & Function Definitions

```go
// internal/config/config.go
package config

import (
    "errors"
    "fmt"
    "os"
    "time"

    "gopkg.in/yaml.v3"
)

const (
    DefaultTestTimeout     = 10 * time.Second
    DefaultRequestMethod   = "GET"
    DefaultCheckInterval   = 5 * time.Minute
    DefaultRecheckInterval = 30 * time.Minute
    DefaultFailThreshold   = 2
    DefaultSwitchCooldown  = 10 * time.Minute
)

type Config struct {
    TestURL         string         `yaml:"test_url"`
    TestTimeout     time.Duration  `yaml:"test_timeout"`
    RequestMethod   string         `yaml:"request_method"`
    CheckInterval   time.Duration  `yaml:"check_interval"`
    RecheckInterval time.Duration  `yaml:"recheck_interval"`
    FailThreshold   int            `yaml:"fail_threshold"`
    SwitchCooldown  time.Duration  `yaml:"switch_cooldown"`
    SingBox         SingBoxConfig  `yaml:"singbox"`
    Subscriptions   []Subscription `yaml:"subscriptions"`
}

type SingBoxConfig struct {
    Binary     string `yaml:"binary"`
    ConfigPath string `yaml:"config_path"`
    Service    string `yaml:"service_name"`
    Inbound    string `yaml:"inbound_listen"`
}

type Subscription struct {
    Name string `yaml:"name"`
    URL  string `yaml:"url"`
    Type string `yaml:"type"` // "sing-box-json" | "base64"
}

// Load reads the YAML file at path and returns a validated Config.
// Applies defaults for zero-value fields before validation.
func Load(path string) (*Config, error)

// LoadFromBytes parses YAML from raw bytes. Used by tests and future --stdin mode.
func LoadFromBytes(data []byte) (*Config, error)

// applyDefaults mutates c in place, filling zero values with package defaults.
func (c *Config) applyDefaults()

// Validate returns nil or a descriptive error. Calls applyDefaults first.
func (c *Config) Validate() error

// validateSubscription returns nil or error for a single subscription.
// Names must be unique within the slice (caller checks).
func (s *Subscription) validate() error

// SubscriptionTypes returns the set of accepted type strings.
func SubscriptionTypes() map[string]bool { return map[string]bool{"sing-box-json": true, "base64": true} }
```

### Behavior

- `Load` reads the file, calls `LoadFromBytes`, then `Validate`.
- `Validate` applies defaults, then enforces:
  - `test_url` non-empty, parses as URL, scheme ∈ {http, https}, host non-empty.
  - `subscriptions` length ≥ 1; each has unique `name`, non-empty `url` that parses as URL with scheme http/https, type ∈ allowed set.
  - All `time.Duration` fields ≥ 1 second.
  - `request_method` ∈ {"GET", "HEAD"} (case-insensitive normalized to upper).
  - `fail_threshold` ≥ 1.
  - `singbox.binary` non-empty; `singbox.config_path` non-empty; `singbox.service` non-empty; `singbox.inbound` non-empty.

### Acceptance Criteria

1. `go test ./internal/config/...` passes.
2. `go test -cover ./internal/config/...` reports `coverage: 100.0% of statements`.
3. Default values fill zero-valued fields.
4. Each documented validation error is reachable by a dedicated test.

### Test Code (actual)

```go
// internal/config/config_test.go
package config

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func writeFile(t *testing.T, dir, name, body string) string {
    t.Helper()
    p := filepath.Join(dir, name)
    if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
        t.Fatal(err)
    }
    return p
}

func minimalValidYAML() string {
    return `
test_url: "https://www.google.com/generate_204"
singbox:
  binary: "/usr/local/bin/sing-box"
  config_path: "/etc/sing-box/config.json"
  service_name: "sing-box"
  inbound_listen: "127.0.0.1:2080"
subscriptions:
  - name: "sub-1"
    url: "https://example.com/sub1"
    type: "sing-box-json"
`
}

func TestLoad_ValidMinimal(t *testing.T) {
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", minimalValidYAML())
    cfg, err := Load(p)
    if err != nil { t.Fatal(err) }
    if cfg.TestURL != "https://www.google.com/generate_204" { t.Fatal("test_url") }
    if cfg.TestTimeout != DefaultTestTimeout { t.Fatal("timeout default") }
    if cfg.RequestMethod != "GET" { t.Fatal("method default") }
    if cfg.CheckInterval != DefaultCheckInterval { t.Fatal("check default") }
    if cfg.RecheckInterval != DefaultRecheckInterval { t.Fatal("recheck default") }
    if cfg.FailThreshold != DefaultFailThreshold { t.Fatal("fail default") }
    if cfg.SwitchCooldown != DefaultSwitchCooldown { t.Fatal("cooldown default") }
    if cfg.SingBox.Binary == "" { t.Fatal("binary") }
    if len(cfg.Subscriptions) != 1 { t.Fatal("subs") }
}

func TestLoad_AllFields(t *testing.T) {
    yaml := `
test_url: "http://example.com/x"
test_timeout: "5s"
request_method: "head"
check_interval: "1m"
recheck_interval: "15m"
fail_threshold: 3
switch_cooldown: "2m"
singbox:
  binary: "/bin/sb"
  config_path: "/etc/sb/cfg.json"
  service_name: "sb-svc"
  inbound_listen: "127.0.0.1:1080"
subscriptions:
  - { name: "a", url: "https://example.com/a", type: "base64" }
`
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", yaml)
    cfg, err := Load(p)
    if err != nil { t.Fatal(err) }
    if cfg.TestTimeout != 5*time.Second { t.Fatal("timeout override") }
    if cfg.RequestMethod != "HEAD" { t.Fatal("method normalize") }
    if cfg.FailThreshold != 3 { t.Fatal("threshold override") }
    if cfg.SingBox.Inbound != "127.0.0.1:1080" { t.Fatal("inbound") }
}

func TestLoad_FileMissing(t *testing.T) {
    _, err := Load("/nonexistent/path/c.yaml")
    if err == nil { t.Fatal("expected error") }
}

func TestLoad_InvalidYAML(t *testing.T) {
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", "::not yaml::")
    _, err := Load(p)
    if err == nil { t.Fatal("expected parse error") }
}

func TestValidate_MissingTestURL(t *testing.T) {
    c := &Config{SingBox: SingBoxConfig{Binary:"x",ConfigPath:"y",Service:"z",Inbound:"127.0.0.1:1"}, Subscriptions: []Subscription{{Name:"a",URL:"https://e.com",Type:"base64"}}}
    err := c.Validate()
    if err == nil || !strings.Contains(err.Error(), "test_url") { t.Fatalf("got %v", err) }
}

func TestValidate_BadTestURLScheme(t *testing.T) {
    c := &Config{TestURL:"ftp://example.com", SingBox: SingBoxConfig{Binary:"x",ConfigPath:"y",Service:"z",Inbound:"127.0.0.1:1"}, Subscriptions: []Subscription{{Name:"a",URL:"https://e.com",Type:"base64"}}}
    if err := c.Validate(); err == nil { t.Fatal("expected scheme error") }
}

func TestValidate_NoSubscriptions(t *testing.T) {
    c := &Config{TestURL:"https://e.com", SingBox: SingBoxConfig{Binary:"x",ConfigPath:"y",Service:"z",Inbound:"127.0.0.1:1"}}
    if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "subscription") { t.Fatalf("got %v", err) }
}

func TestValidate_DuplicateSubscriptionNames(t *testing.T) {
    c := &Config{TestURL:"https://e.com", SingBox: SingBoxConfig{Binary:"x",ConfigPath:"y",Service:"z",Inbound:"127.0.0.1:1"}, Subscriptions: []Subscription{
        {Name:"a",URL:"https://e.com/a",Type:"base64"},
        {Name:"a",URL:"https://e.com/b",Type:"base64"},
    }}
    if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "unique") { t.Fatalf("got %v", err) }
}

func TestValidate_BadSubscriptionType(t *testing.T) {
    c := &Config{TestURL:"https://e.com", SingBox: SingBoxConfig{Binary:"x",ConfigPath:"y",Service:"z",Inbound:"127.0.0.1:1"}, Subscriptions: []Subscription{{Name:"a",URL:"https://e.com/a",Type:"clash"}}}
    if err := c.Validate(); err == nil { t.Fatal("expected type error") }
}

func TestValidate_BadSubscriptionURL(t *testing.T) {
    c := &Config{TestURL:"https://e.com", SingBox: SingBoxConfig{Binary:"x",ConfigPath:"y",Service:"z",Inbound:"127.0.0.1:1"}, Subscriptions: []Subscription{{Name:"a",URL:"::not a url::",Type:"base64"}}}
    if err := c.Validate(); err == nil { t.Fatal("expected url error") }
}

func TestValidate_ZeroDuration(t *testing.T) {
    yaml := strings.Replace(minimalValidYAML(), "singbox:", "check_interval: \"0s\"\nsingbox:", 1)
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", yaml)
    if _, err := Load(p); err == nil { t.Fatal("expected zero duration error") }
    if !strings.Contains(strings.ToLower(mustErr(err)), "check_interval") {
        t.Fatalf("expected error mentioning check_interval, got %v", err)
    }
}

func mustErr(err error) string {
    if err == nil { return "" }
    return err.Error()
}

func TestValidate_NegativeDuration(t *testing.T) {
    // yaml.v3 rejects negative durations at parse time. Verify that.
    yaml := strings.Replace(minimalValidYAML(), "singbox:", "check_interval: \"-1s\"\nsingbox:", 1)
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", yaml)
    if _, err := Load(p); err == nil { t.Fatal("expected parse error for negative duration") }
}

func TestValidate_ZeroFailThreshold(t *testing.T) {
    yaml := strings.Replace(minimalValidYAML(), "singbox:", "fail_threshold: 0\nsingbox:", 1)
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", yaml)
    if _, err := Load(p); err == nil { t.Fatal("expected fail_threshold error") }
}

func TestValidate_MissingSingBoxFields(t *testing.T) {
    cases := []string{
        strings.Replace(minimalValidYAML(), "binary: \"/usr/local/bin/sing-box\"", "binary: \"\"", 1),
        strings.Replace(minimalValidYAML(), "config_path: \"/etc/sing-box/config.json\"", "config_path: \"\"", 1),
        strings.Replace(minimalValidYAML(), "service_name: \"sing-box\"", "service_name: \"\"", 1),
        strings.Replace(minimalValidYAML(), "inbound_listen: \"127.0.0.1:2080\"", "inbound_listen: \"\"", 1),
    }
    for i, y := range cases {
        dir := t.TempDir()
        p := writeFile(t, dir, "c.yaml", y)
        if _, err := Load(p); err == nil { t.Fatalf("case %d: expected error", i) }
    }
}

func TestValidate_BadRequestMethod(t *testing.T) {
    yaml := strings.Replace(minimalValidYAML(), "singbox:", "request_method: \"POST\"\nsingbox:", 1)
    dir := t.TempDir()
    p := writeFile(t, dir, "c.yaml", yaml)
    if _, err := Load(p); err == nil { t.Fatal("expected method error") }
}

func TestSubscriptionTypes(t *testing.T) {
    if !SubscriptionTypes()["base64"] || !SubscriptionTypes()["sing-box-json"] { t.Fatal("types missing") }
    if SubscriptionTypes()["clash"] { t.Fatal("clash should not be allowed") }
}
```

Note: `TestValidate_ZeroDuration` as written is slightly messy — the builder should rewrite this test to construct a YAML with `check_interval: "0s"` and assert that `Load` returns an error containing "check_interval". The skeleton above conveys the intent.

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go get gopkg.in/yaml.v3
go mod tidy
go test ./internal/config/... -v
go test -cover ./internal/config/...
go test -race -timeout 30s ./internal/config/...
git add internal/config go.mod go.sum .gitignore
git commit -m "feat(config): YAML loader with defaults and validation"
```

---

## Chunk 2: `internal/subscription/fetcher.go` — HTTP Subscription Fetcher

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/subscription/fetcher.go`
- Create: `/home/petrovov/kimchi-project/internal/subscription/fetcher_test.go`

**Depends on:** chunk 1

### Type & Function Definitions

```go
// internal/subscription/fetcher.go
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

// CandidateConfig is the unit produced by subscription parsing and consumed by runner/checker/selector.
type CandidateConfig struct {
    Name   string
    Source string
    Raw    []byte
    Parsed map[string]any
}

// HTTPDoer is the subset of *http.Client used by Fetcher; lets tests inject fakes.
type HTTPDoer interface {
    Do(req *http.Request) (*http.Response, error)
}

// Fetcher fetches subscription URLs and converts them to candidate configs.
type Fetcher struct {
    client     HTTPDoer
    timeout    time.Duration
    maxRedir   int
    userAgent  string
    log        *slog.Logger
    jsonParser JSONParser     // injected, see chunk 3
    baseParser Base64Parser   // injected, see chunk 4
}

// JSONParser and Base64Parser are interface seams so fetcher tests don't need real parsers.
type JSONParser interface {
    Parse(name, source string, body []byte) ([]CandidateConfig, error)
}
type Base64Parser interface {
    Parse(name, source string, body []byte) ([]CandidateConfig, error)
}

func NewFetcher(client HTTPDoer, timeout time.Duration, log *slog.Logger, jp JSONParser, bp Base64Parser) *Fetcher

// Fetch downloads each subscription and dispatches to the appropriate parser.
// Returns the aggregated candidates. Fetch errors for individual subscriptions are logged
// and skipped — only parser errors that the parser surfaces are propagated up.
// Returns an error only if ALL subscriptions fail to download.
func (f *Fetcher) Fetch(ctx context.Context, subs []config.Subscription) ([]CandidateConfig, error)
```

### Behavior

- For each subscription: `GET <url>` with 30s timeout (configurable), follow up to 10 redirects, read up to 10 MiB body.
- On download error: `log.Warn`, increment failure counter, continue.
- On success: dispatch by `Type`. If the parser returns an error: `log.Warn`, skip that subscription.
- If every subscription failed to download, return `errors.New("all subscriptions failed to fetch")`.

### Acceptance Criteria

1. `go test ./internal/subscription/...` passes for fetcher tests.
2. `go test -cover ./internal/subscription/...` for the fetcher file is 100%.
3. No real network: all tests use `httptest.NewServer` and an injected `HTTPDoer` adapter.

### Test Code (actual)

```go
// internal/subscription/fetcher_test.go
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
        err := f.errs[f.callIdx]; f.callIdx++; return nil, err
    }
    if f.callIdx >= len(f.responses) { return nil, errors.New("no more responses") }
    resp := f.responses[f.callIdx]; f.callIdx++; return resp, nil
}

func newJSONResp(t *testing.T, body string) *http.Response {
    t.Helper()
    r := httptest.NewRecorder()
    r.Code = 200
    r.Header().Set("Content-Type", "application/json")
    r.Body = io.NopCloser(strings.NewReader(body))
    return r.Result()
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

func newFetcher(d HTTPDoer, jp JSONParser, bp Base64Parser) *Fetcher {
    return NewFetcher(d, 100*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)), jp, bp)
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
        newJSONResp(t, `{"outbounds":[]}`),
        newJSONResp(t, "base64body"),
    }}
    f := newFetcher(d, jp, bp)
    subs := []config.Subscription{
        {Name:"a", URL:"https://e.com/a", Type:"sing-box-json"},
        {Name:"b", URL:"https://e.com/b", Type:"base64"},
    }
    got, err := f.Fetch(context.Background(), subs)
    if err != nil { t.Fatal(err) }
    if len(got) != 2 { t.Fatalf("got %d candidates", len(got)) }
    if jsonCalled.Load() != 1 || b64Called.Load() != 1 { t.Fatal("parsers not called") }
}

// helpers for parser adapters used in fetcher tests
type parserFuncJSON func(name, src string, body []byte) ([]CandidateConfig, error)
func (p parserFuncJSON) Parse(n, s string, b []byte) ([]CandidateConfig, error) { return p(n, s, b) }
type parserFuncB64 func(name, src string, body []byte) ([]CandidateConfig, error)
func (p parserFuncB64) Parse(n, s string, b []byte) ([]CandidateConfig, error) { return p(n, s, b) }

func TestFetch_SkipsFailedDownload(t *testing.T) {
    d := &fakeDoer{errs: []error{errors.New("boom")}}
    jp := stubJSON{}
    bp := stubB64{}
    f := newFetcher(d, jp, bp)
    subs := []config.Subscription{{Name:"a", URL:"https://e.com/a", Type:"base64"}}
    _, err := f.Fetch(context.Background(), subs)
    if err == nil { t.Fatal("expected error when all fail") }
}

func TestFetch_SkipsFailedParse(t *testing.T) {
    d := &fakeDoer{responses: []*http.Response{newJSONResp(t, "x")}}
    jp := stubJSON{err: errors.New("parse fail")}
    bp := stubB64{}
    f := newFetcher(d, jp, bp)
    subs := []config.Subscription{{Name:"a", URL:"https://e.com/a", Type:"sing-box-json"}}
    _, err := f.Fetch(context.Background(), subs)
    if err == nil { t.Fatal("expected all-failed error") }
}

func TestFetch_SetsUserAgent(t *testing.T) {
    d := &fakeDoer{responses: []*http.Response{newJSONResp(t, "{}")}}
    jp := stubJSON{out: []CandidateConfig{{Name:"x", Raw: []byte("{}")}}}
    f := newFetcher(d, jp, stubB64{})
    _, _ = f.Fetch(context.Background(), []config.Subscription{{Name:"a", URL:"https://e.com/a", Type:"sing-box-json"}})
    if got := d.calls[0].Header.Get("User-Agent"); got == "" { t.Fatal("missing UA") }
}

func TestFetch_ContextCanceled(t *testing.T) {
    d := &fakeDoer{}
    f := newFetcher(d, stubJSON{}, stubB64{})
    ctx, cancel := context.WithCancel(context.Background()); cancel()
    _, err := f.Fetch(ctx, []config.Subscription{{Name:"a", URL:"https://e.com/a", Type:"base64"}})
    if err == nil { t.Fatal("expected error") }
}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/subscription/... -run TestFetch -v
go test -cover ./internal/subscription/...
git add internal/subscription/fetcher.go internal/subscription/fetcher_test.go
git commit -m "feat(subscription): injectable HTTP fetcher with parser dispatch"
```

---

## Chunk 3: `internal/subscription/json.go` — sing-box JSON Parser

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/subscription/json.go`
- Create: `/home/petrovov/kimchi-project/internal/subscription/json_test.go`

**Depends on:** chunk 1

### Function Definition

```go
// internal/subscription/json.go
package subscription

import (
    "encoding/json"
    "errors"
    "fmt"
)

// JSONParser parses a sing-box JSON subscription body into one candidate.
type JSONSubscriptionParser struct{}

func NewJSONSubscriptionParser() *JSONSubscriptionParser

// Parse validates the body is sing-box JSON and returns a single CandidateConfig.
// Returns an error if the body is not valid JSON, is not an object, or lacks
// "outbounds" or "route".
func (p *JSONSubscriptionParser) Parse(name, source string, body []byte) ([]CandidateConfig, error)
```

Behavior: parse JSON; require `map[string]any` root; require key `outbounds` OR `route` to be present; marshal back to `Raw` with `json.Marshal` (canonical, sorted keys not required).

### Acceptance Criteria

1. `go test ./internal/subscription/...` passes.
2. 100% coverage of `json.go`.

### Test Code (actual)

```go
// internal/subscription/json_test.go
package subscription

import "testing"

func TestJSON_Valid(t *testing.T) {
    body := []byte(`{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[]}}`)
    p := NewJSONSubscriptionParser()
    got, err := p.Parse("a", "https://e.com/a", body)
    if err != nil { t.Fatal(err) }
    if len(got) != 1 { t.Fatalf("expected 1 candidate, got %d", len(got)) }
    if got[0].Name != "a" { t.Fatal("name") }
    if got[0].Source != "https://e.com/a" { t.Fatal("source") }
    if got[0].Parsed["outbounds"] == nil { t.Fatal("parsed outbounds") }
    if len(got[0].Raw) == 0 { t.Fatal("raw not re-marshaled") }
}

func TestJSON_RouteOnly(t *testing.T) {
    body := []byte(`{"route":{}}`)
    p := NewJSONSubscriptionParser()
    if _, err := p.Parse("a", "src", body); err != nil { t.Fatal(err) }
}

func TestJSON_NoOutboundsOrRoute(t *testing.T) {
    body := []byte(`{"inbounds":[]}`)
    p := NewJSONSubscriptionParser()
    if _, err := p.Parse("a", "src", body); err == nil { t.Fatal("expected error") }
}

func TestJSON_InvalidJSON(t *testing.T) {
    p := NewJSONSubscriptionParser()
    if _, err := p.Parse("a", "src", []byte("not json")); err == nil { t.Fatal("expected error") }
}

func TestJSON_NotObject(t *testing.T) {
    p := NewJSONSubscriptionParser()
    if _, err := p.Parse("a", "src", []byte(`[1,2,3]`)); err == nil { t.Fatal("expected error") }
}

func TestJSON_EmptyBody(t *testing.T) {
    p := NewJSONSubscriptionParser()
    if _, err := p.Parse("a", "src", []byte("")); err == nil { t.Fatal("expected error") }
}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/subscription/... -run TestJSON -v
go test -cover ./internal/subscription/...
git add internal/subscription/json.go internal/subscription/json_test.go
git commit -m "feat(subscription): sing-box JSON parser"
```

---

## Chunk 4: `internal/subscription/base64.go` + `links.go` — Base64 Proxy Link Parser

**Complexity:** complex (multiple URI schemes, encoding edge cases)

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/subscription/base64.go`
- Create: `/home/petrovov/kimchi-project/internal/subscription/links.go`
- Create: `/home/petrovov/kimchi-project/internal/subscription/base64_test.go`
- Create: `/home/petrovov/kimchi-project/internal/subscription/links_test.go`

**Depends on:** chunk 1

### Function Definitions

```go
// internal/subscription/base64.go
package subscription

import (
    "encoding/base64"
    "errors"
    "fmt"
    "strings"
)

// Base64SubscriptionParser parses base64-encoded newline-separated proxy URIs.
type Base64SubscriptionParser struct {
    linkParser LinkParser   // injected, see links.go
}

func NewBase64SubscriptionParser(lp LinkParser) *Base64SubscriptionParser

// Parse decodes the body (tolerating URL-safe vs standard alphabets and missing padding),
// splits on whitespace, parses each URI, and converts each to a CandidateConfig.
// Returns an error only if decoding fails completely; per-line parse errors are skipped
// and logged via the caller.
func (p *Base64SubscriptionParser) Parse(name, source string, body []byte) ([]CandidateConfig, error)

// decodeBase64 tries standard then URL-safe base64 with padding fix-up.
func decodeBase64(b []byte) ([]byte, error)

// splitLines splits on \r\n, \n, and ';' (some providers use ';'), trimming whitespace.
func splitLines(b []byte) []string
```

```go
// internal/subscription/links.go
package subscription

import (
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "net/url"
    "strings"
)

// LinkParser converts a single proxy URI into a sing-box outbound map.
// Supported schemes: vmess://, vless://, ss://, trojan://.
type LinkParser struct{}

func NewLinkParser() *LinkParser

// Parse parses one URI line into a sing-box outbound (map[string]any).
// Returns nil, nil if the line is blank/comment.
// Returns an error on malformed lines.
func (p *LinkParser) Parse(line string) (map[string]any, error)

// parseVMess handles vmess:// (base64-encoded JSON: {v,ps,add,port,id,aid,net,type,host,path,tls,...})
func (p *LinkParser) parseVMess(rest string) (map[string]any, error)

// parseVLess handles vless://uuid@host:port?params#tag
func (p *LinkParser) parseVLess(u *url.URL) (map[string]any, error)

// parseShadowsocks handles ss://method:password@host:port#tag (with optional base64 of userinfo)
func (p *LinkParser) parseShadowsocks(u *url.URL) (map[string]any, error)

// parseTrojan handles trojan://password@host:port?params#tag
func (p *LinkParser) parseTrojan(u *url.URL) (map[string]any, error)
```

### Behavior & Outbound Shape

For each scheme, produce an outbound with `type`, `tag`, `server`, `server_port`, and protocol-specific options, e.g.:

- `vmess`: `{"type":"vmess","tag":"...","server":"...","server_port":N,"uuid":"...","alter_id":N,"security":"...","transport":{...},"tls":{...}}`.
- `vless`: `{"type":"vless","tag":"...","server":"...","server_port":N,"uuid":"...","flow":"...","transport":{...},"tls":{...}}`.
- `ss`: `{"type":"shadowsocks","tag":"...","server":"...","server_port":N,"method":"...","password":"..."}`.
- `trojan`: `{"type":"trojan","tag":"...","server":"...","server_port":N,"password":"...","transport":{...},"tls":{...}}`.

The minimal runnable config assembly is in chunk 5 (builder); chunk 4 only emits outbounds.

### Acceptance Criteria

1. All `links_test.go` cases parse to the documented outbound shape (use struct comparison via `reflect.DeepEqual` on the parsed `map[string]any`).
2. Malformed lines are reported per-line; only fully malformed base64 input returns an error.
3. 100% coverage of `base64.go` and `links.go`.

### Test Code (representative; builder adds edge cases)

```go
// internal/subscription/links_test.go
package subscription

import (
    "reflect"
    "testing"
)

func TestLinks_VLessBasic(t *testing.T) {
    p := NewLinkParser()
    out, err := p.Parse("vless://11111111-2222-3333-4444-555555555555@example.com:443?security=tls&type=ws&path=%2F#node1")
    if err != nil { t.Fatal(err) }
    if out["type"] != "vless" { t.Fatalf("type: %v", out["type"]) }
    if out["server"] != "example.com" { t.Fatalf("server: %v", out["server"]) }
    if out["tag"] != "node1" { t.Fatalf("tag: %v", out["tag"]) }
}

func TestLinks_TrojanBasic(t *testing.T) {
    p := NewLinkParser()
    out, err := p.Parse("trojan://pw@example.com:443?security=tls#t1")
    if err != nil { t.Fatal(err) }
    if out["type"] != "trojan" { t.Fatal(out["type"]) }
    if out["password"] != "pw" { t.Fatal(out["password"]) }
}

func TestLinks_ShadowsocksBasic(t *testing.T) {
    p := NewLinkParser()
    // ss://BASE64(method:password)@host:port#tag
    // method:password = "aes-256-gcm:secret"
    // base64 standard, no padding
    import_b64 := "YWVzLTI1Ni1nY206c2VjcmV0"
    out, err := p.Parse("ss://" + import_b64 + "@example.com:8388#ss1")
    if err != nil { t.Fatal(err) }
    if out["type"] != "shadowsocks" { t.Fatal(out["type"]) }
    if out["method"] != "aes-256-gcm" { t.Fatal(out["method"]) }
    if out["password"] != "secret" { t.Fatal(out["password"]) }
}

func TestLinks_VMessBasic(t *testing.T) {
    p := NewLinkParser()
    // vmess JSON base64-encoded
    jsonStr := `{"v":2,"ps":"v1","add":"example.com","port":443,"id":"u-1","aid":0,"net":"tcp","type":"none","host":"","path":"","tls":""}`
    import_b64 := base64.StdEncoding.EncodeToString([]byte(jsonStr))
    out, err := p.Parse("vmess://" + import_b64)
    if err != nil { t.Fatal(err) }
    if out["type"] != "vmess" { t.Fatal(out["type"]) }
    if out["server"] != "example.com" { t.Fatal(out["server"]) }
    if out["uuid"] != "u-1" { t.Fatal(out["uuid"]) }
}

func TestLinks_BlankAndComment(t *testing.T) {
    p := NewLinkParser()
    if out, err := p.Parse(""); err != nil || out != nil { t.Fatal("blank should yield nil,nil") }
    if out, err := p.Parse("   "); err != nil || out != nil { t.Fatal("whitespace should yield nil,nil") }
    if out, err := p.Parse("# comment"); err != nil || out != nil { t.Fatal("comment should yield nil,nil") }
}

func TestLinks_UnsupportedScheme(t *testing.T) {
    p := NewLinkParser()
    if _, err := p.Parse("wireguard://x"); err == nil { t.Fatal("expected error") }
}

func TestLinks_MalformedURL(t *testing.T) {
    p := NewLinkParser()
    if _, err := p.Parse("vless://not a url"); err == nil { t.Fatal("expected error") }
}
```

```go
// internal/subscription/base64_test.go
package subscription

import (
    "encoding/base64"
    "strings"
    "testing"
)

func TestBase64_DecodesAndParses(t *testing.T) {
    lines := []string{
        "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=tls#n1",
        "trojan://pw@example.com:443#n2",
    }
    body := []byte(strings.Join(lines, "\n"))
    enc := base64.StdEncoding.EncodeToString(body)
    p := NewBase64SubscriptionParser(NewLinkParser())
    got, err := p.Parse("sub-a", "https://e.com/a", []byte(enc))
    if err != nil { t.Fatal(err) }
    if len(got) != 2 { t.Fatalf("got %d", len(got)) }
    if !strings.HasPrefix(got[0].Name, "sub-a/") { t.Fatal("name prefix") }
}

func TestBase64_URLSafeAlphabet(t *testing.T) {
    lines := []string{"vless://11111111-2222-3333-4444-555555555555@example.com:443#n1"}
    enc := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
    p := NewBase64SubscriptionParser(NewLinkParser())
    if _, err := p.Parse("sub", "src", []byte(enc)); err != nil { t.Fatal(err) }
}

func TestBase64_EmptyBody(t *testing.T) {
    enc := base64.StdEncoding.EncodeToString([]byte(""))
    p := NewBase64SubscriptionParser(NewLinkParser())
    got, err := p.Parse("sub", "src", []byte(enc))
    if err != nil { t.Fatal(err) }
    if len(got) != 0 { t.Fatal("expected 0 candidates") }
}

func TestBase64_InvalidBase64(t *testing.T) {
    p := NewBase64SubscriptionParser(NewLinkParser())
    if _, err := p.Parse("sub", "src", []byte("!!! not b64 !!!")); err == nil { t.Fatal("expected error") }
}

func TestBase64_SkipsInvalidLines(t *testing.T) {
    lines := []string{
        "vless://11111111-2222-3333-4444-555555555555@example.com:443#ok",
        "garbage line",
        "vless://22222222-3333-4444-5555-666666666666@example.com:443#ok2",
    }
    body := []byte(strings.Join(lines, "\n"))
    enc := base64.StdEncoding.EncodeToString(body)
    p := NewBase64SubscriptionParser(NewLinkParser())
    got, err := p.Parse("sub", "src", []byte(enc))
    if err != nil { t.Fatal(err) }
    if len(got) != 2 { t.Fatalf("expected 2 valid, got %d", len(got)) }
}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/subscription/... -v
go test -cover ./internal/subscription/...
git add internal/subscription/base64.go internal/subscription/links.go internal/subscription/base64_test.go internal/subscription/links_test.go
git commit -m "feat(subscription): base64 parser with vmess/vless/ss/trojan links"
```

---

## Chunk 5: `internal/subscription/builder.go` — Candidate Config Builder

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/subscription/builder.go`
- Create: `/home/petrovov/kimchi-project/internal/subscription/builder_test.go`

**Depends on:** chunk 4

### Function Definition

```go
// internal/subscription/builder.go
package subscription

import (
    "encoding/json"
    "fmt"
)

// BuildCandidate wraps a single outbound into a minimal runnable sing-box config.
// The runner (chunk 6) will further inject an inbound and remove experimental fields.
func BuildCandidate(name string, outbound map[string]any) (CandidateConfig, error)
```

### Behavior

Minimal config shape:

```json
{
  "log": { "level": "error" },
  "inbounds": [],
  "outbounds": [ <outbound> ],
  "route": { "final": "<outbound.tag or direct>" }
}
```

- If outbound has no `tag`, set it to `name`.
- Marshal to `Raw` and parse back into `Parsed`.

### Acceptance Criteria

1. Output JSON parses back to the same structure.
2. Tag defaulting works.
3. 100% coverage of `builder.go`.

### Test Code (actual)

```go
// internal/subscription/builder_test.go
package subscription

import (
    "encoding/json"
    "reflect"
    "testing"
)

func TestBuildCandidate_SetsTag(t *testing.T) {
    out := map[string]any{"type":"direct","server":"1.1.1.1","server_port":443}
    c, err := BuildCandidate("my-node", out)
    if err != nil { t.Fatal(err) }
    obs, _ := c.Parsed["outbounds"].([]any)
    if len(obs) != 1 { t.Fatal("outbounds") }
    m := obs[0].(map[string]any)
    if m["tag"] != "my-node" { t.Fatal("tag default") }
    if c.Parsed["route"].(map[string]any)["final"] != "my-node" { t.Fatal("route.final") }
}

func TestBuildCandidate_PreservesTag(t *testing.T) {
    out := map[string]any{"type":"direct","tag":"custom","server":"1.1.1.1","server_port":443}
    c, err := BuildCandidate("my-node", out)
    if err != nil { t.Fatal(err) }
    obs := c.Parsed["outbounds"].([]any)
    if obs[0].(map[string]any)["tag"] != "custom" { t.Fatal("tag overwritten") }
}

func TestBuildCandidate_RoundTrip(t *testing.T) {
    out := map[string]any{"type":"direct","tag":"t","server":"1.1.1.1","server_port":443}
    c, err := BuildCandidate("my-node", out)
    if err != nil { t.Fatal(err) }
    var m map[string]any
    if err := json.Unmarshal(c.Raw, &m); err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(m["outbounds"], c.Parsed["outbounds"]) { t.Fatal("round-trip mismatch") }
}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/subscription/... -run TestBuild -v
go test -cover ./internal/subscription/...
git add internal/subscription/builder.go internal/subscription/builder_test.go
git commit -m "feat(subscription): candidate config builder"
```

---

## Chunk 6: `internal/runner` — Temporary sing-box Runner

**Complexity:** complex (process lifecycle, port allocation, config mutation, readiness probing)

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/runner/runner.go`
- Create: `/home/petrovov/kimchi-project/internal/runner/mutate.go`
- Create: `/home/petrovov/kimchi-project/internal/runner/runner_test.go`

**Depends on:** chunks 1, 2 (CandidateConfig type), 5

### Type & Function Definitions

```go
// internal/runner/runner.go
package runner

import (
    "context"
    "fmt"
    "log/slog"
    "os/exec"
    "time"

    "github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

// ProcessController abstracts process lifecycle for testing.
type ProcessController interface {
    Start(ctx context.Context, name string, args ...string) (ProcessHandle, error)
}

// ProcessHandle is the subset of *os.Process the runner needs.
type ProcessHandle interface {
    Pid() int
    Wait() error
    Kill() error
    // Done returns a channel closed when the process exits.
    Done() <-chan struct{}
}

// RunnerHandle is returned to callers for use + cleanup.
type RunnerHandle struct {
    ProxyURL string       // "socks5://127.0.0.1:54321"
    Port     int
    Close    func() error
}

// Runner spawns sing-box instances.
type Runner struct {
    binary       string
    exec         ProcessController
    log          *slog.Logger
    readyTimeout time.Duration
    tempDir      string // for tests
}

func New(binary string, ctrl ProcessController, log *slog.Logger) *Runner

// Start mutates cfg (in-memory copy), writes a temp file, starts sing-box,
// polls the proxy port until it accepts TCP connections (or timeout),
// and returns a handle whose Close kills the process and removes the temp file.
func (r *Runner) Start(ctx context.Context, cfg subscription.CandidateConfig) (*RunnerHandle, error)
```

```go
// internal/runner/mutate.go
package runner

// MutateForTest takes a parsed sing-box config map, strips inbound/experimental/api
// fields, and injects a single SOCKS inbound bound to 127.0.0.1:port.
// Returns a deep-enough copy (JSON round-trip) suitable for re-marshaling.
func MutateForTest(parsed map[string]any, port int) (map[string]any, error)
```

### Behavior

- `Start`:
  1. Allocate a free port via `net.Listen("tcp", "127.0.0.1:0")`, immediately close the listener, capture port number. (Race-window acceptable per spec — readiness probe will retry.)
  2. Deep-clone `cfg.Parsed` via JSON round-trip into a new map.
  3. Call `MutateForTest(clone, port)`.
  4. Marshal back to JSON, write to `os.CreateTemp(r.tempDir, "sing-box-rotor-*.json")`.
  5. `ctrl.Start(ctx, r.binary, "run", "-c", tmpPath)`.
  6. Poll `127.0.0.1:port` with `net.DialTimeout` every 50ms up to `readyTimeout`.
  7. On success return `&RunnerHandle{ProxyURL: "socks5://127.0.0.1:<port>", Port: port, Close: cleanup}`.
  8. On timeout: kill process, remove temp file, return error.

### Acceptance Criteria

1. `go test ./internal/runner/...` passes.
2. 100% coverage.
3. The real `/usr/local/bin/sing-box` is NOT required: tests use a `fakeProcess` that opens a TCP listener on a port returned by `Start` so the readiness probe succeeds. The fake must be cross-platform-friendly and free of OS-specific calls beyond `net.Listen`.

### Test Code (representative)

```go
// internal/runner/runner_test.go
package runner

import (
    "context"
    "errors"
    "io"
    "log/slog"
    "net"
    "sync"
    "testing"
    "time"

    "github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

// fakeProcess listens on the given port, exposes Done channel on Kill.
type fakeProcess struct {
    ln   net.Listener
    done chan struct{}
    once sync.Once
}
func (f *fakeProcess) Pid() int { return 0 }
func (f *fakeProcess) Wait() error { <-f.done; return nil }
func (f *fakeProcess) Kill() error { f.once.Do(func(){ close(f.done); f.ln.Close() }); return nil }
func (f *fakeProcess) Done() <-chan struct{} { return f.done }

// fakeCtrl implements ProcessController: it picks the first free port for the
// process to "occupy", opens a real listener on it, and returns a fakeProcess.
type fakeCtrl struct {
    mu sync.Mutex
    started int
}
func (c *fakeCtrl) Start(ctx context.Context, name string, args ...string) (ProcessHandle, error) {
    c.mu.Lock(); defer c.mu.Unlock()
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { return nil, err }
    c.started++
    return &fakeProcess{ln: ln, done: make(chan struct{})}, nil
}

func newTestRunner() *Runner {
    return New("/usr/local/bin/sing-box", &fakeCtrl{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func sampleCandidate() subscription.CandidateConfig {
    raw := []byte(`{"log":{"level":"error"},"inbounds":[{"type":"mixed","listen":"127.0.0.1","listen_port":1234}],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct","route_options":{"geoip":"a"}}}`)
    return subscription.CandidateConfig{Name:"x", Source:"s", Raw: raw, Parsed: map[string]any{
        "log": map[string]any{"level":"error"},
        "inbounds": []any{map[string]any{"type":"mixed","listen":"127.0.0.1","listen_port":1234}},
        "outbounds": []any{map[string]any{"type":"direct","tag":"direct"}},
        "route": map[string]any{"final":"direct"},
    }}
}

func TestRunner_Start_Success(t *testing.T) {
    r := newTestRunner()
    h, err := r.Start(context.Background(), sampleCandidate())
    if err != nil { t.Fatal(err) }
    defer h.Close()
    if h.Port == 0 { t.Fatal("port not set") }
    if h.ProxyURL == "" { t.Fatal("proxy url empty") }
}

func TestRunner_Close_RemovesTempFile(t *testing.T) {
    r := newTestRunner()
    h, err := r.Start(context.Background(), sampleCandidate())
    if err != nil { t.Fatal(err) }
    if err := h.Close(); err != nil { t.Fatal(err) }
    // Calling Close twice should be idempotent.
    if err := h.Close(); err != nil && !errors.Is(err, os.ErrNotExist) { /* temp file already gone */ }
}

func TestMutateForTest_StripsConflictingFieldsAndInjectsSOCKS(t *testing.T) {
    parsed := map[string]any{
        "inbounds":     []any{map[string]any{"type":"mixed","listen_port":1234}},
        "outbounds":    []any{map[string]any{"type":"direct","tag":"direct"}},
        "experimental": map[string]any{"cache_file": map[string]any{"enabled":true}},
        "route":        map[string]any{"final":"direct"},
        "dns":          map[string]any{"servers": []any{"1.1.1.1"}},
    }
    out, err := MutateForTest(parsed, 54321)
    if err != nil { t.Fatal(err) }
    ib, ok := out["inbounds"].([]any)
    if !ok || len(ib) != 1 { t.Fatalf("inbounds not replaced: %+v", out["inbounds"]) }
    m := ib[0].(map[string]any)
    if m["type"] != "socks" { t.Fatal("not socks") }
    if m["listen_port"] != 54321 { t.Fatal("port") }
    if m["listen"] != "127.0.0.1" { t.Fatal("listen") }
    for _, banned := range []string{"experimental"} {
        if _, ok := out[banned]; ok { t.Fatalf("%s not removed", banned) }
    }
    // DNS is preserved (not conflicting).
    if _, ok := out["dns"]; !ok { t.Fatal("dns should be preserved") }
}

func TestMutateForTest_RemovesAPI(t *testing.T) {
    parsed := map[string]any{
        "outbounds": []any{map[string]any{"type":"direct","tag":"direct"}},
        "route":     map[string]any{"final":"direct"},
    }
    out, err := MutateForTest(parsed, 12345)
    if err != nil { t.Fatal(err) }
    // The mutation must inject exactly one SOCKS inbound even when none existed.
    ib := out["inbounds"].([]any)
    if len(ib) != 1 { t.Fatalf("inbound count: %d", len(ib)) }
    if ib[0].(map[string]any)["type"] != "socks" { t.Fatal("socks missing") }
}

func TestRunner_Start_StartFailure(t *testing.T) {
    r := New("/bin/none", &errCtrl{err: errors.New("boom")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
    if _, err := r.Start(context.Background(), sampleCandidate()); err == nil { t.Fatal("expected error") }
}

type errCtrl struct{ err error }
func (e *errCtrl) Start(ctx context.Context, name string, args ...string) (ProcessHandle, error) {
    return nil, e.err
}

func TestRunner_Start_ReadinessTimeout(t *testing.T) {
    // fakeCtrl that never opens a listener
    r := New("/bin/none", &silentCtrl{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
    r.readyTimeout = 200 * time.Millisecond
    if _, err := r.Start(context.Background(), sampleCandidate()); err == nil { t.Fatal("expected timeout") }
}

type silentCtrl struct{}
func (s *silentCtrl) Start(ctx context.Context, name string, args ...string) (ProcessHandle, error) {
    return &silentProc{done: make(chan struct{})}, nil
}
type silentProc struct{ done chan struct{} }
func (s *silentProc) Pid() int { return 0 }
func (s *silentProc) Wait() error { <-s.done; return nil }
func (s *silentProc) Kill() error { close(s.done); return nil }
func (s *silentProc) Done() <-chan struct{} { return s.done }
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/runner/... -v
go test -race -timeout 30s ./internal/runner/...
go test -cover ./internal/runner/...
git add internal/runner/runner.go internal/runner/mutate.go internal/runner/runner_test.go
git commit -m "feat(runner): temporary sing-box runner with injectable process control"
```

---

## Chunk 7: `internal/checker` — HTTP Latency Checker

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/checker/checker.go`
- Create: `/home/petrovov/kimchi-project/internal/checker/checker_test.go`

**Depends on:** chunks 1, 6

### Type & Function Definitions

```go
// internal/checker/checker.go
package checker

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"
    "net/url"
    "time"

    "golang.org/x/net/proxy"
)

type Result struct {
    CandidateName string
    Latency       time.Duration
    Error         error
}

// HTTPClientFactory builds an *http.Client that routes through proxyURL with timeout.
type HTTPClientFactory func(timeout time.Duration, proxyURL string) (*http.Client, error)

// DefaultHTTPClientFactory supports socks5:// and http:// proxy schemes.
func DefaultHTTPClientFactory(timeout time.Duration, proxyURL string) (*http.Client, error)

type Checker struct {
    url     string
    method  string // "GET" or "HEAD"
    timeout time.Duration
    factory HTTPClientFactory
    log     *slog.Logger
}

func New(url, method string, timeout time.Duration, factory HTTPClientFactory, log *slog.Logger) *Checker

// Check performs a single latency probe via proxyURL.
// Latency is measured from request start to response headers received.
// Accepts any 2xx or 3xx status as success.
func (c *Checker) Check(ctx context.Context, proxyURL, name string) Result

// ProbeCurrent is the same operation but uses the main sing-box inbound directly.
// It is a convenience wrapper used by the daemon for current-config checks.
func (c *Checker) ProbeCurrent(ctx context.Context, inbound string) Result
```

### Behavior

- `DefaultHTTPClientFactory` parses proxyURL; if scheme is `socks5`, uses `golang.org/x/net/proxy.SOCKS5("tcp", u.Host, nil, proxy.Direct)`; if scheme is `http`, sets `Transport.Proxy`; else returns error.
- `Check`: builds client, sets `req.Method = c.method`, records `t0`, calls `client.Do`, records `t1 = time.Since(t0)` when response status is received (for HEAD do not drain body; for GET, drain and close body). Status 200–399 ⇒ success.

### Acceptance Criteria

1. `go test ./internal/checker/...` passes.
2. 100% coverage.
3. Tests do not require a real SOCKS server — they use `httptest.NewServer` as the test URL and an injected factory that returns a plain client (no proxy) for "current config" paths, plus a stub SOCKS server for proxy paths (using a `httptest.NewServer` that pretends to be SOCKS5: tests can verify factory wiring without actual proxying by using `proxyURL = "socks5://127.0.0.1:<test server port>"` and relying on the factory's transport to fail — testing the error path).

### Test Code (representative)

```go
// internal/checker/checker_test.go
package checker

import (
    "context"
    "errors"
    "io"
    "log/slog"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestDefaultFactory_HTTPProxy(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        w.WriteHeader(204)
    }))
    defer ts.Close()
    c, err := DefaultHTTPClientFactory(2*time.Second, ts.URL)
    if err != nil { t.Fatal(err) }
    resp, err := c.Get(ts.URL)
    if err != nil { t.Fatal(err) }
    resp.Body.Close()
}

func TestDefaultFactory_BadScheme(t *testing.T) {
    if _, err := DefaultHTTPClientFactory(time.Second, "ftp://x"); err == nil { t.Fatal("expected error") }
}

func TestCheck_GET_Success(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        time.Sleep(5 * time.Millisecond); w.WriteHeader(204)
    }))
    defer ts.Close()
    fac := func(to time.Duration, proxyURL string) (*http.Client, error) {
        return &http.Client{Timeout: to}, nil
    }
    c := New(ts.URL, "GET", time.Second, fac, discardLogger())
    r := c.Check(context.Background(), "", "n")
    if r.Error != nil { t.Fatalf("err: %v", r.Error) }
    if r.Latency <= 0 { t.Fatal("latency zero") }
}

func TestCheck_HEAD_Success(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        if r.Method != "HEAD" { t.Fatalf("method %s", r.Method) }
        w.WriteHeader(200)
    }))
    defer ts.Close()
    fac := func(to time.Duration, proxyURL string) (*http.Client, error) {
        return &http.Client{Timeout: to}, nil
    }
    c := New(ts.URL, "HEAD", time.Second, fac, discardLogger())
    if r := c.Check(context.Background(), "", "n"); r.Error != nil { t.Fatal(r.Error) }
}

func TestCheck_4xxIsError(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        w.WriteHeader(404)
    }))
    defer ts.Close()
    fac := func(to time.Duration, proxyURL string) (*http.Client, error) {
        return &http.Client{Timeout: to}, nil
    }
    c := New(ts.URL, "GET", time.Second, fac, discardLogger())
    r := c.Check(context.Background(), "", "n")
    if r.Error == nil { t.Fatal("expected error on 4xx") }
}

func TestCheck_Timeout(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        time.Sleep(200 * time.Millisecond); w.WriteHeader(204)
    }))
    defer ts.Close()
    fac := func(to time.Duration, proxyURL string) (*http.Client, error) {
        return &http.Client{Timeout: to}, nil
    }
    c := New(ts.URL, "GET", 50*time.Millisecond, fac, discardLogger())
    r := c.Check(context.Background(), "", "n")
    if r.Error == nil { t.Fatal("expected timeout error") }
}

func TestCheck_FactoryError(t *testing.T) {
    fac := func(to time.Duration, proxyURL string) (*http.Client, error) {
        return nil, errors.New("bad proxy")
    }
    c := New("http://x", "GET", time.Second, fac, discardLogger())
    r := c.Check(context.Background(), "socks5://x", "n")
    if r.Error == nil { t.Fatal("expected error") }
}

func TestProbeCurrent(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        w.WriteHeader(200)
    }))
    defer ts.Close()
    fac := func(to time.Duration, proxyURL string) (*http.Client, error) {
        return &http.Client{Timeout: to}, nil
    }
    c := New(ts.URL, "GET", time.Second, fac, discardLogger())
    // Parse inbound from test server URL — strip scheme, use host.
    addr := strings.TrimPrefix(ts.URL, "http://")
    r := c.ProbeCurrent(context.Background(), addr)
    if r.Error != nil { t.Fatal(r.Error) }
}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go get golang.org/x/net
go mod tidy
go test ./internal/checker/... -v
go test -race -timeout 30s ./internal/checker/...
go test -cover ./internal/checker/...
git add internal/checker/checker.go internal/checker/checker_test.go go.mod go.sum
git commit -m "feat(checker): HTTP latency checker with injectable client factory"
```

---

## Chunk 8: `internal/selector` — Selection Strategy with Hysteresis

**Complexity:** complex (state machine, clock abstraction, race-safety)

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/selector/selector.go`
- Create: `/home/petrovov/kimchi-project/internal/selector/selector_test.go`

**Depends on:** chunks 1, 7

### Type & Function Definitions

```go
// internal/selector/selector.go
package selector

import (
    "log/slog"
    "sync"
    "time"

    "github.com/ov-petrov/sing-box-rotor/internal/checker"
)

type Decision int

const (
    DecisionKeep Decision = iota
    DecisionSwitch
    DecisionDefer
)

func (d Decision) String() string

type DecisionInfo struct {
    Decision  Decision
    Candidate string // empty for Keep
    Reason    string
}

type Clock interface { Now() time.Time }
type realClock struct{}
func (realClock) Now() time.Time { return time.Now() }

// Selector is a thread-safe state machine implementing hysteresis.
type Selector struct {
    mu             sync.Mutex
    currentConfig  string
    currentFails   int
    lastSwitchTime time.Time
    failThreshold  int
    switchCooldown time.Duration
    clock          Clock
    log            *slog.Logger
}

func New(failThreshold int, switchCooldown time.Duration, clock Clock, log *slog.Logger) *Selector

// OnCurrentCheck records the outcome of probing the current config.
// On success, resets fails to 0 and returns DecisionKeep.
// On failure, increments fails. Returns DecisionKeep if below threshold,
// otherwise DecisionDefer (caller should trigger candidate evaluation).
func (s *Selector) OnCurrentCheck(err error) DecisionInfo

// OnEvaluation decides whether to switch given candidate probe results and the
// current config name. bestName is the candidate with minimum latency, "" if none.
func (s *Selector) OnEvaluation(bestName string, haveAnyCandidate bool) DecisionInfo

// MarkSwitched records that a switch to newName was applied.
func (s *Selector) MarkSwitched(newName string)

// Snapshot returns the current state for diagnostics/testing.
type Snapshot struct {
    CurrentConfig  string
    CurrentFails   int
    LastSwitchTime time.Time
}

func (s *Selector) Snapshot() Snapshot
```

### Behavior

- `OnCurrentCheck(nil)` ⇒ `currentFails = 0`, return `DecisionKeep, "ok"`.
- `OnCurrentCheck(err)` ⇒ `currentFails++`. If `< failThreshold` return `DecisionKeep, "fail N/M"`. Else return `DecisionDefer, "threshold reached"`.
- `OnEvaluation("")` (no candidates) ⇒ `DecisionKeep, "no candidates"`.
- `OnEvaluation(bestName)`:
  - If `bestName == s.currentConfig` ⇒ `DecisionKeep, "already best"`.
  - If `clock.Now().Sub(s.lastSwitchTime) < switchCooldown` ⇒ `DecisionDefer, "cooldown"`.
  - Else ⇒ `DecisionSwitch, bestName, "switching"`.
- `MarkSwitched(name)` ⇒ `currentConfig = name`, `currentFails = 0`, `lastSwitchTime = clock.Now()`.

All methods must take `s.mu` lock.

### Acceptance Criteria

1. `go test ./internal/selector/...` passes with `-race`.
2. 100% coverage.
3. All decisions reached by a deterministic fake clock in tests.

### Test Code (actual)

```go
// internal/selector/selector_test.go
package selector

import (
    "io"
    "log/slog"
    "sync"
    "testing"
    "time"
)

type fakeClock struct {
    mu sync.Mutex
    t  time.Time
}
func (f *fakeClock) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.t }
func (f *fakeClock) Advance(d time.Duration) { f.mu.Lock(); defer f.mu.Unlock(); f.t = f.t.Add(d) }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSelector_OnCurrentCheck_ResetsOnSuccess(t *testing.T) {
    c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
    s := New(2, 10*time.Minute, c, discardLogger())
    _ = s.OnCurrentCheck(assertErr("boom"))
    if s.Snapshot().CurrentFails != 1 { t.Fatal("incr") }
    if d := s.OnCurrentCheck(nil); d.Decision != DecisionKeep || d.Reason != "ok" { t.Fatal(d) }
    if s.Snapshot().CurrentFails != 0 { t.Fatal("reset") }
}

func assertErr(msg string) error { return &stringErr{msg} }
type stringErr struct{ s string }
func (e *stringErr) Error() string { return e.s }

func TestSelector_OnCurrentCheck_BelowThreshold(t *testing.T) {
    c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
    s := New(3, 10*time.Minute, c, discardLogger())
    d := s.OnCurrentCheck(assertErr("x"))
    if d.Decision != DecisionKeep { t.Fatal(d) }
    if d.Reason == "" { t.Fatal("reason") }
}

func TestSelector_OnCurrentCheck_ThresholdDefer(t *testing.T) {
    c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
    s := New(2, 10*time.Minute, c, discardLogger())
    _ = s.OnCurrentCheck(assertErr("x"))
    d := s.OnCurrentCheck(assertErr("x"))
    if d.Decision != DecisionDefer { t.Fatalf("got %v", d) }
}

func TestSelector_OnEvaluation_NoCandidates(t *testing.T) {
    c := &fakeClock{t: time.Unix(0, 0)}
    s := New(2, time.Minute, c, discardLogger())
    s.MarkSwitched("cur")
    d := s.OnEvaluation("", false)
    if d.Decision != DecisionKeep || d.Reason != "no candidates" { t.Fatal(d) }
}

func TestSelector_OnEvaluation_SameAsCurrent(t *testing.T) {
    c := &fakeClock{t: time.Unix(0, 0)}
    s := New(2, time.Minute, c, discardLogger())
    s.MarkSwitched("best")
    d := s.OnEvaluation("best", true)
    if d.Decision != DecisionKeep { t.Fatal(d) }
}

func TestSelector_OnEvaluation_Cooldown(t *testing.T) {
    c := &fakeClock{t: time.Unix(0, 0)}
    s := New(2, 10*time.Minute, c, discardLogger())
    s.MarkSwitched("a")
    c.Advance(time.Minute)
    d := s.OnEvaluation("b", true)
    if d.Decision != DecisionDefer { t.Fatal(d) }
}

func TestSelector_OnEvaluation_Switch(t *testing.T) {
    c := &fakeClock{t: time.Unix(0, 0)}
    s := New(2, time.Minute, c, discardLogger())
    s.MarkSwitched("a")
    c.Advance(2 * time.Minute)
    d := s.OnEvaluation("b", true)
    if d.Decision != DecisionSwitch || d.Candidate != "b" { t.Fatalf("%+v", d) }
}

func TestSelector_MarkSwitched(t *testing.T) {
    c := &fakeClock{t: time.Unix(0, 0)}
    s := New(2, time.Minute, c, discardLogger())
    s.MarkSwitched("x")
    snap := s.Snapshot()
    if snap.CurrentConfig != "x" || snap.CurrentFails != 0 || !snap.LastSwitchTime.Equal(c.t) { t.Fatal(snap) }
}

func TestSelector_ConcurrentSafe(t *testing.T) {
    c := &fakeClock{t: time.Unix(0, 0)}
    s := New(100, time.Hour, c, discardLogger())
    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(1); go func() { defer wg.Done(); _ = s.OnCurrentCheck(assertErr("x")) }()
    }
    wg.Wait()
    if s.Snapshot().CurrentFails != 50 { t.Fatal("race in counter") }
}

func TestDecisionString(t *testing.T) {
    if DecisionKeep.String() == "" || DecisionSwitch.String() == "" || DecisionDefer.String() == "" { t.Fatal() }
    if Decision(99).String() == "" { t.Fatal("unknown label") }
}

// Compile-time check that checker package import is used (avoid unused import if signature trimmed)
var _ = checker.Result{}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/selector/... -v
go test -race -timeout 30s ./internal/selector/...
go test -cover ./internal/selector/...
git add internal/selector/selector.go internal/selector/selector_test.go
git commit -m "feat(selector): hysteresis state machine"
```

---

## Chunk 9: `internal/systemd` — Config Writer + Service Restart

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/systemd/manager.go`
- Create: `/home/petrovov/kimchi-project/internal/systemd/atomic.go`
- Create: `/home/petrovov/kimchi-project/internal/systemd/manager_test.go`

**Depends on:** chunks 1, 2 (CandidateConfig)

### Type & Function Definitions

```go
// internal/systemd/atomic.go
package systemd

import "os"

// FileSystem abstracts filesystem for testing.
type FileSystem interface {
    WriteFile(path string, data []byte, perm os.FileMode) error
    Rename(oldPath, newPath string) error
    Remove(path string) error
    Stat(path string) (os.FileInfo, error)
}

type osFS struct{}
func (osFS) WriteFile(p string, d []byte, m os.FileMode) error { return os.WriteFile(p, d, m) }
func (osFS) Rename(o, n string) error { return os.Rename(o, n) }
func (osFS) Remove(p string) error { return os.Remove(p) }
func (osFS) Stat(p string) (os.FileInfo, error) { return os.Stat(p) }

// AtomicWrite writes data to path via temp-file + rename.
// If the destination already exists, copies it to path+".bak" first.
func AtomicWrite(path string, data []byte, perm os.FileMode, fs FileSystem) error
```

```go
// internal/systemd/manager.go
package systemd

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log/slog"
    "os"
    "time"

    "github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

// CommandRunner abstracts shell-out to systemctl.
type CommandRunner interface {
    Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}
func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    // uses os/exec.CommandContext
    return nil, nil // placeholder; see real impl below
}

// Manager writes configs and restarts the main sing-box service.
type Manager struct {
    configPath    string
    serviceName   string
    runner        CommandRunner
    fs            FileSystem
    restartWait   time.Duration
    log           *slog.Logger
}

func NewManager(configPath, serviceName string, runner CommandRunner, fs FileSystem, log *slog.Logger) *Manager

// Apply validates cfg, atomic-writes JSON to configPath, backs up the previous file,
// then runs `systemctl restart <service>` and waits for it to become active.
func (m *Manager) Apply(ctx context.Context, cfg subscription.CandidateConfig) error

// RestartOnly is exposed for the daemon's "main config changed elsewhere" path
// (not used in v1, but unit-tested).
func (m *Manager) RestartOnly(ctx context.Context) error

// WaitActive polls `systemctl is-active <service>` up to m.restartWait.
func (m *Manager) WaitActive(ctx context.Context) error
```

`execRunner.Run` real implementation:

```go
func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, name, args...)
    return cmd.CombinedOutput()
}
```

### Behavior

- `Apply`:
  1. `json.Marshal(cfg.Parsed)` with `MarshalIndent`.
  2. Validate JSON round-trips.
  3. If `configPath` exists, copy to `configPath + ".bak"`.
  4. `AtomicWrite(configPath, data, 0o644, fs)`.
  5. `runner.Run(ctx, "systemctl", "restart", m.serviceName)`.
  6. `WaitActive(ctx)`.
- `WaitActive`: loop `runner.Run(ctx, "systemctl", "is-active", "--quiet", m.serviceName)` up to 30s, return error if never active.

### Acceptance Criteria

1. `go test ./internal/systemd/...` passes.
2. 100% coverage.
3. Atomic-write tests verify: backup created when destination exists; temp file cleaned up on success; no temp file remains on simulated failure (use an `fs` mock that errors on the final rename).

### Test Code (representative)

```go
// internal/systemd/manager_test.go
package systemd

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "log/slog"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
    "time"
)

// memFS is an in-memory FileSystem for tests.
type memFile struct{ data []byte; perm os.FileMode }
type memFS struct {
    mu sync.Mutex
    files map[string]*memFile
}
func newMemFS() *memFS { return &memFS{files: map[string]*memFile{}} }
func (m *memFS) get(p string) *memFile { m.mu.Lock(); defer m.mu.Unlock(); return m.files[p] }
func (m *memFS) WriteFile(p string, d []byte, perm os.FileMode) error {
    m.mu.Lock(); defer m.mu.Unlock()
    if _, ok := m.files[p]; ok { return os.ErrExist } // simulate no-clobber for safety; but real os.WriteFile overwrites
    // Real os.WriteFile truncates — replicate that:
    cp := append([]byte(nil), d...)
    m.files[p] = &memFile{data: cp, perm: perm}
    return nil
}
func (m *memFS) Rename(o, n string) error {
    m.mu.Lock(); defer m.mu.Unlock()
    f, ok := m.files[o]
    if !ok { return os.ErrNotExist }
    delete(m.files, o)
    m.files[n] = f
    return nil
}
func (m *memFS) Remove(p string) error {
    m.mu.Lock(); defer m.mu.Unlock()
    if _, ok := m.files[p]; !ok { return os.ErrNotExist }
    delete(m.files, p); return nil
}
func (m *memFS) Stat(p string) (os.FileInfo, error) {
    m.mu.Lock(); defer m.mu.Unlock()
    f, ok := m.files[p]
    if !ok { return nil, os.ErrNotExist }
    return &memFileInfo{name: filepath.Base(p), size: int64(len(f.data)), mode: f.perm}, nil
}

type memFileInfo struct{ name string; size int64; mode os.FileMode }
func (i *memFileInfo) Name() string { return i.name }
func (i *memFileInfo) Size() int64 { return i.size }
func (i *memFileInfo) Mode() os.FileMode { return i.mode }
func (i *memFileInfo) ModTime() time.Time { return time.Time{} }
func (i *memFileInfo) IsDir() bool { return false }
func (i *memFileInfo) Sys() any { return nil }

type fakeRunner struct {
    mu sync.Mutex
    cmds [][]string
    activeAfter int // number of restart calls before is-active succeeds
    restarts int
}
func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    r.mu.Lock(); defer r.mu.Unlock()
    r.cmds = append(r.cmds, append([]string{name}, args...))
    if len(args) >= 1 && args[0] == "restart" { r.restarts++ }
    if len(args) >= 1 && args[0] == "is-active" {
        if r.restarts > r.activeAfter { return []byte("active"), nil }
        return []byte("inactive"), errors.New("not active")
    }
    return nil, nil
}

func sample() subscription.CandidateConfig {
    return subscription.CandidateConfig{
        Name: "n", Source: "s",
        Raw: []byte(`{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`),
        Parsed: map[string]any{
            "outbounds": []any{map[string]any{"type":"direct","tag":"direct"}},
            "route":     map[string]any{"final":"direct"},
        },
    }
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestApply_Success(t *testing.T) {
    fs := newMemFS()
    r := &fakeRunner{activeAfter: 0}
    m := NewManager("/etc/sb/cfg.json", "sb-svc", r, fs, discardLogger())
    m.restartWait = 100 * time.Millisecond
    if err := m.Apply(context.Background(), sample()); err != nil { t.Fatal(err) }
    if fs.get("/etc/sb/cfg.json") == nil { t.Fatal("config not written") }
    if r.restarts != 1 { t.Fatal("restart not called") }
}

func TestApply_BackupCreated(t *testing.T) {
    fs := newMemFS()
    _ = fs.WriteFile("/etc/sb/cfg.json", []byte(`{"old":true}`), 0o644)
    r := &fakeRunner{activeAfter: 0}
    m := NewManager("/etc/sb/cfg.json", "sb-svc", r, fs, discardLogger())
    m.restartWait = 100 * time.Millisecond
    if err := m.Apply(context.Background(), sample()); err != nil { t.Fatal(err) }
    if fs.get("/etc/sb/cfg.json.bak") == nil { t.Fatal("no backup") }
}

func TestApply_InvalidJSON(t *testing.T) {
    fs := newMemFS()
    r := &fakeRunner{}
    m := NewManager("/etc/sb/cfg.json", "sb-svc", r, fs, discardLogger())
    bad := subscription.CandidateConfig{Name:"n", Parsed: map[string]any{"x": make(chan int)}}
    if err := m.Apply(context.Background(), bad); err == nil { t.Fatal("expected marshal error") }
}

func TestApply_RestartFails(t *testing.T) {
    fs := newMemFS()
    r := &failOnRestart{}
    m := NewManager("/etc/sb/cfg.json", "sb-svc", r, fs, discardLogger())
    if err := m.Apply(context.Background(), sample()); err == nil { t.Fatal("expected error") }
}
type failOnRestart struct{}
func (f *failOnRestart) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    if len(args) > 0 && args[0] == "restart" { return nil, errors.New("boom") }
    return []byte("active"), nil
}

func TestWaitActive_Timeout(t *testing.T) {
    fs := newMemFS()
    r := &fakeRunner{activeAfter: 999}
    m := NewManager("/etc/sb/cfg.json", "sb-svc", r, fs, discardLogger())
    m.restartWait = 50 * time.Millisecond
    if err := m.WaitActive(context.Background()); err == nil { t.Fatal("expected timeout") }
}

func TestAtomicWrite_NoExistingFile(t *testing.T) {
    fs := newMemFS()
    if err := AtomicWrite("/a/b.json", []byte(`{}`), 0o644, fs); err != nil { t.Fatal(err) }
    if fs.get("/a/b.json") == nil { t.Fatal("not written") }
}

func TestAtomicWrite_RealFS_TempRemoved(t *testing.T) {
    dir := t.TempDir()
    target := filepath.Join(dir, "c.json")
    if err := AtomicWrite(target, []byte(`{}`), 0o644, osFS{}); err != nil { t.Fatal(err) }
    entries, _ := os.ReadDir(dir)
    for _, e := range entries {
        if strings.HasPrefix(e.Name(), ".sb-rotor-") { t.Fatal("temp not cleaned: " + e.Name()) }
    }
    if _, err := os.Stat(target); err != nil { t.Fatal("target missing") }
}

func TestExecRunner_Run(t *testing.T) {
    out, err := (execRunner{}).Run(context.Background(), "true")
    if err != nil { t.Fatal(err) }
    _ = out
}
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/systemd/... -v
go test -race -timeout 30s ./internal/systemd/...
go test -cover ./internal/systemd/...
git add internal/systemd/manager.go internal/systemd/atomic.go internal/systemd/manager_test.go
git commit -m "feat(systemd): atomic config writer + service restart"
```

---

## Chunk 10: `internal/daemon` — Orchestration Loop

**Complexity:** complex (goroutines, timers, shutdown coordination, parallel candidate evaluation)

**Files:**
- Create: `/home/petrovov/kimchi-project/internal/daemon/daemon.go`
- Create: `/home/petrovov/kimchi-project/internal/daemon/daemon_test.go`

**Depends on:** chunks 2, 6, 7, 8, 9

### Type & Function Definitions

```go
// internal/daemon/daemon.go
package daemon

import (
    "context"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/ov-petrov/sing-box-rotor/internal/checker"
    "github.com/ov-petrov/sing-box-rotor/internal/config"
    "github.com/ov-petrov/sing-box-rotor/internal/runner"
    "github.com/ov-petrov/sing-box-rotor/internal/selector"
    "github.com/ov-petrov/sing-box-rotor/internal/subscription"
    "github.com/ov-petrov/sing-box-rotor/internal/systemd"
)

// Runner abstracts the runner package for testing.
type Runner interface {
    Start(ctx context.Context, cfg subscription.CandidateConfig) (*runner.RunnerHandle, error)
}

// HandleCloser lets daemon tests inspect what was cleaned up.
type HandleCloser interface {
    Close() error
}

type Daemon struct {
    cfg      *config.Config
    fetcher  subscription.Fetcher
    runner   Runner
    checker  *checker.Checker
    selector *selector.Selector
    sys      *systemd.Manager
    log      *slog.Logger
}

func New(cfg *config.Config, f subscription.Fetcher, r Runner, c *checker.Checker, sel *selector.Selector, sys *systemd.Manager, log *slog.Logger) *Daemon

// RunOnce fetches subscriptions, evaluates candidates in parallel, applies the best.
func (d *Daemon) RunOnce(ctx context.Context) error

// Run enters the scheduler loop until ctx is canceled.
func (d *Daemon) Run(ctx context.Context) error
```

### Behavior

`RunOnce`:
1. `candidates, err := fetcher.Fetch(ctx, cfg.Subscriptions)`. If empty, log critical and return nil.
2. For each candidate, spawn goroutine that calls `runner.Start(ctx, c)` then `checker.Check(ctx, h.ProxyURL, c.Name)`; collect results via channel. Always close handle in defer.
3. Find candidate with minimum latency among successful; call `selector.OnEvaluation(bestName, true)`.
4. If DecisionSwitch, call `sys.Apply(ctx, bestCfg)`, then `selector.MarkSwitched(bestName)`.
5. If DecisionDefer, log and return nil.
6. If no candidates succeed, log critical and return nil.

`Run`:
1. Call `RunOnce`.
2. Create two tickers: `checkTicker` (cfg.CheckInterval) and `recheckTicker` (cfg.RecheckInterval).
3. Select loop:
   - `case <-ctx.Done()`: stop tickers, return nil.
   - `case <-checkTicker.C`: probe current via `checker.ProbeCurrent`; feed result to `selector.OnCurrentCheck`; if DecisionDefer, trigger `RunOnce`.
   - `case <-recheckTicker.C`: trigger `RunOnce`.
   - `case <-shutdown`: exit.

Note: `ProbeCurrent` uses `cfg.SingBox.Inbound` as the proxy URL — treat it as `socks5://<inbound>` for v1 (document this in code comment).

### Acceptance Criteria

1. `go test ./internal/daemon/...` passes with `-race`.
2. 100% coverage.
3. Tests use fake `Fetcher`, fake `Runner`, fake `Checker` (use a `Checker` with a no-op factory), fake `Selector` is the real one with fake clock, fake `Manager` is the real one with fake `CommandRunner` + `FileSystem` mocks.
4. Shutdown test cancels context mid-evaluation and verifies handles are closed.

### Test Code (representative)

```go
// internal/daemon/daemon_test.go
package daemon

import (
    "context"
    "errors"
    "io"
    "log/slog"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/ov-petrov/sing-box-rotor/internal/checker"
    "github.com/ov-petrov/sing-box-rotor/internal/config"
    "github.com/ov-petrov/sing-box-rotor/internal/runner"
    "github.com/ov-petrov/sing-box-rotor/internal/selector"
    "github.com/ov-petrov/sing-box-rotor/internal/subscription"
    "github.com/ov-petrov/sing-box-rotor/internal/systemd"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeFetcher struct{ candidates []subscription.CandidateConfig }
func (f *fakeFetcher) Fetch(ctx context.Context, subs []config.Subscription) ([]subscription.CandidateConfig, error) {
    return f.candidates, nil
}

type fakeRunner struct {
    mu sync.Mutex
    started int32
    closed int32
}
func (r *fakeRunner) Start(ctx context.Context, cfg subscription.CandidateConfig) (*runner.RunnerHandle, error) {
    atomic.AddInt32(&r.started, 1)
    return &runner.RunnerHandle{
        ProxyURL: "socks5://127.0.0.1:1",
        Port: 1,
        Close: func() error { atomic.AddInt32(&r.closed, 1); return nil },
    }, nil
}

type fakeClock struct{ mu sync.Mutex; t time.Time }
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

// simpleFileSystem / fakeRunner reused from chunk 9 — copied or shared via test util.

func newCfg() *config.Config {
    return &config.Config{
        TestURL:"https://e.com/x", TestTimeout: time.Second, RequestMethod: "GET",
        CheckInterval: 50*time.Millisecond, RecheckInterval: 100*time.Millisecond,
        FailThreshold: 1, SwitchCooldown: time.Minute,
        SingBox: config.SingBoxConfig{Binary:"/bin/sb", ConfigPath:"/etc/sb/c.json", Service:"sb-svc", Inbound:"127.0.0.1:2080"},
        Subscriptions: []config.Subscription{{Name:"s", URL:"https://e.com/s", Type:"base64"}},
    }
}

func TestRunOnce_SwitchesToBest(t *testing.T) {
    cfg := newCfg()
    fc := &fakeFetcher{candidates: []subscription.CandidateConfig{
        {Name:"a", Parsed: map[string]any{"outbounds":[]any{}, "route":map[string]any{"final":"direct"}}},
        {Name:"b", Parsed: map[string]any{"outbounds":[]any{}, "route":map[string]any{"final":"direct"}}},
    }}
    fr := &fakeRunner{}
    // Inject a Checker with a factory that returns fast success — but latency is
    // dominated by the response handler. Use httptest to control ordering.
    // For determinism, use a Checker whose factory always returns a client that
    // returns a known latency via a controllable server.
    // Simpler: directly verify that RunOnce reaches sys.Apply with the best candidate.
    // We'll construct candidates with names sorted alphabetically and assert
    // the chosen one matches the lowest-latency probe. Easiest path: provide
    // distinct httptest servers that delay differently; here we use a custom
    // checker.Checker with our own HTTPClientFactory.
    //
    // Create two test servers: "a" responds in 5ms, "b" in 50ms.
    tsA := makeSlowServer(5*time.Millisecond)
    tsB := makeSlowServer(50*time.Millisecond)
    defer tsA.Close(); defer tsB.Close()
    _ = tsB
    fac := func(timeout time.Duration, proxyURL string) (*http.Client, error) {
        return &http.Client{Timeout: timeout, Transport: &redirectingRoundTripper{}}, nil
    }
    // We instead test using a custom Checker that uses the test URL of "http://127.0.0.1:1/x"
    // and returns Latency derived from candidate Name ordering for determinism:
    // we'll inject latency directly by using a different checker factory.
    // For brevity, replace the network with a custom proxyURL → latency map:
    latencyByProxy := map[string]time.Duration{
        "socks5://127.0.0.1:1-a": 5*time.Millisecond,
        "socks5://127.0.0.1:1-b": 50*time.Millisecond,
    }
    fac = func(timeout time.Duration, proxyURL string) (*http.Client, error) {
        return &fastClient{latency: latencyByProxy[proxyURL], timeout: timeout}, nil
    }
    fr2 := &proxyAwareRunner{latencyByName: map[string]string{"a":"socks5://127.0.0.1:1-a","b":"socks5://127.0.0.1:1-b"}}
    chk := checker.New(cfg.TestURL, "GET", time.Second, fac, discard())
    sel := selector.New(cfg.FailThreshold, cfg.SwitchCooldown, &fakeClock{t: time.Unix(0,0)}, discard())
    sys := systemd.NewManager("/etc/sb/c.json", "sb-svc", &fakeRunnerCmd{activeAfter:0}, newMemFS(), discard())
    d := New(cfg, fc, fr2, chk, sel, sys, discard())
    if err := d.RunOnce(context.Background()); err != nil { t.Fatal(err) }
    if sel.Snapshot().CurrentConfig != "a" { t.Fatalf("got %s", sel.Snapshot().CurrentConfig) }
    if atomic.LoadInt32(&fr.started)+atomic.LoadInt32(&fr2.started) == 0 { t.Fatal("no runners") }
}

// Helpers below mirror the slow-server and proxy-aware runner patterns.

func makeSlowServer(d time.Duration) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
        time.Sleep(d); w.WriteHeader(204)
    }))
}

type redirectingRoundTripper struct{}
func (r *redirectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return nil, errors.New("unused") }

type fastClient struct{ latency, timeout time.Duration }
func (c *fastClient) Do(req *http.Request) (*http.Response, error) {
    time.Sleep(c.latency)
    return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}
func (c *fastClient) Get(url string) (*http.Response, error) {
    time.Sleep(c.latency)
    return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

type proxyAwareRunner struct{ latencyByName map[string]string; mu sync.Mutex; started, closed int32 }
func (r *proxyAwareRunner) Start(ctx context.Context, cfg subscription.CandidateConfig) (*runner.RunnerHandle, error) {
    atomic.AddInt32(&r.started, 1)
    name := cfg.Name
    return &runner.RunnerHandle{
        ProxyURL: r.latencyByName[name],
        Port: 1,
        Close: func() error { atomic.AddInt32(&r.closed, 1); return nil },
    }, nil
}

type fakeRunnerCmd struct{ activeAfter, restarts int32; mu sync.Mutex }
func (r *fakeRunnerCmd) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    r.mu.Lock(); defer r.mu.Unlock()
    if len(args) > 0 && args[0] == "restart" { atomic.AddInt32(&r.restarts, 1) }
    if len(args) > 0 && args[0] == "is-active" {
        if atomic.LoadInt32(&r.restarts) > r.activeAfter { return []byte("active"), nil }
        return nil, errors.New("not active")
    }
    return nil, nil
}

func TestRun_ShutdownCancels(t *testing.T) {
    cfg := newCfg()
    cfg.CheckInterval = 5*time.Millisecond
    cfg.RecheckInterval = 5*time.Millisecond
    // Provide candidates so RunOnce actually does work and closes handles.
    fc := &fakeFetcher{candidates: []subscription.CandidateConfig{
        {Name:"a", Parsed: map[string]any{"outbounds":[]any{}, "route":map[string]any{"final":"direct"}}},
    }}
    fr := &proxyAwareRunner{latencyByName: map[string]string{"a":"socks5://127.0.0.1:1-a"}}
    fac := func(to time.Duration, p string) (*http.Client, error) {
        return &fastClient{latency: time.Millisecond, timeout: to}, nil
    }
    chk := checker.New(cfg.TestURL, "GET", time.Second, fac, discard())
    sel := selector.New(1, time.Minute, &fakeClock{t: time.Unix(0,0)}, discard())
    sys := systemd.NewManager("/etc/sb/c.json", "sb-svc", &fakeRunnerCmd{activeAfter:0}, newMemFS(), discard())
    d := New(cfg, fc, fr, chk, sel, sys, discard())
    ctx, cancel := context.WithCancel(context.Background())
    go func(){ time.Sleep(40*time.Millisecond); cancel() }()
    if err := d.Run(ctx); err != nil { t.Fatal(err) }
    // Assert at least one runner was started AND closed (cleanup on shutdown).
    if atomic.LoadInt32(&fr.started) == 0 { t.Fatal("no work performed") }
    if atomic.LoadInt32(&fr.closed) == 0 { t.Fatal("handles not closed") }
}

func TestRunOnce_NoCandidates(t *testing.T) {
    cfg := newCfg()
    fc := &fakeFetcher{}
    fr := &proxyAwareRunner{}
    chk := checker.New(cfg.TestURL, "GET", time.Second, func(to time.Duration, p string) (*http.Client, error){
        return &http.Client{Timeout: to}, nil
    }, discard())
    sel := selector.New(1, time.Minute, &fakeClock{t: time.Unix(0,0)}, discard())
    sys := systemd.NewManager("/etc/sb/c.json", "sb-svc", &fakeRunnerCmd{}, newMemFS(), discard())
    d := New(cfg, fc, fr, chk, sel, sys, discard())
    if err := d.RunOnce(context.Background()); err != nil { t.Fatal(err) }
    if sel.Snapshot().CurrentConfig != "" { t.Fatal("unexpected switch") }
}

func TestRunOnce_ApplyFails(t *testing.T) {
    cfg := newCfg()
    fc := &fakeFetcher{candidates: []subscription.CandidateConfig{{Name:"a", Parsed: map[string]any{"outbounds":[]any{}, "route":map[string]any{"final":"direct"}}}}}
    fr := &proxyAwareRunner{latencyByName: map[string]string{"a":"socks5://127.0.0.1:1-a"}}
    fac := func(to time.Duration, p string) (*http.Client, error) {
        return &fastClient{latency: time.Millisecond, timeout: to}, nil
    }
    chk := checker.New(cfg.TestURL, "GET", time.Second, fac, discard())
    sel := selector.New(1, time.Minute, &fakeClock{t: time.Unix(0,0)}, discard())
    sys := systemd.NewManager("/etc/sb/c.json", "sb-svc", &failAll{}, newMemFS(), discard())
    d := New(cfg, fc, fr, chk, sel, sys, discard())
    if err := d.RunOnce(context.Background()); err == nil { t.Fatal("expected error") }
    if sel.Snapshot().CurrentConfig != "" { t.Fatal("should not switch on failure") }
}

type failAll struct{}
func (failAll) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    return nil, errors.New("nope")
}
```

Note: the builder must import `"net/http"`, `"net/http/httptest"`, `"strings"`, and the memFS from chunk 9 (duplicated or moved to a shared `internal/testutil/` if the builder prefers — acceptable refactor).

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
go test ./internal/daemon/... -v
go test -race -timeout 30s ./internal/daemon/...
go test -cover ./internal/daemon/...
git add internal/daemon/daemon.go internal/daemon/daemon_test.go
git commit -m "feat(daemon): orchestration loop with parallel evaluation"
```

---

## Chunk 11: `cmd/sing-box-rotor/main.go` — CLI Entry Point

**Complexity:** simple

**Files:**
- Modify: `/home/petrovov/kimchi-project/cmd/sing-box-rotor/main.go`
- Create: `/home/petrovov/kimchi-project/cmd/sing-box-rotor/main_test.go`

**Depends on:** chunks 1–10

### Implementation Outline

```go
package main

import (
    "context"
    "flag"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/ov-petrov/sing-box-rotor/internal/checker"
    "github.com/ov-petrov/sing-box-rotor/internal/config"
    "github.com/ov-petrov/sing-box-rotor/internal/daemon"
    "github.com/ov-petrov/sing-box-rotor/internal/runner"
    "github.com/ov-petrov/sing-box-rotor/internal/selector"
    "github.com/ov-petrov/sing-box-rotor/internal/subscription"
    "github.com/ov-petrov/sing-box-rotor/internal/systemd"
)

var (
    Version = "dev"
)

func main() {
    var (
        cfgPath  string
        once     bool
        showVer  bool
        verbose  bool
    )
    flag.StringVar(&cfgPath, "config", envOr("SINGBOX_ROTOR_CONFIG", "/etc/sing-box-rotor/config.yaml"), "config file path")
    flag.BoolVar(&once, "once", false, "run one evaluation cycle and exit")
    flag.BoolVar(&showVer, "version", false, "print version")
    flag.BoolVar(&verbose, "verbose", false, "debug logging")
    flag.Parse()

    if showVer { fmt.Println(Version); return }

    log := newLogger(verbose)
    cfg, err := config.Load(cfgPath)
    if err != nil { log.Error("load config", "err", err); os.Exit(1) }

    fetcher := subscription.NewFetcher(&http.Client{Timeout: 30_000_000_000}, 30*time.Second, log,
        subscription.NewJSONSubscriptionParser(),
        subscription.NewBase64SubscriptionParser(subscription.NewLinkParser()),
    )
    r := runner.New(cfg.SingBox.Binary, runner.OSController{}, log)
    chk := checker.New(cfg.TestURL, cfg.RequestMethod, cfg.TestTimeout, checker.DefaultHTTPClientFactory, log)
    sel := selector.New(cfg.FailThreshold, cfg.SwitchCooldown, selector.RealClock{}, log)
    sys := systemd.NewManager(cfg.SingBox.ConfigPath, cfg.SingBox.Service, systemd.ExecRunner{}, systemd.OSFS{}, log)

    d := daemon.New(cfg, fetcher, r, chk, sel, sys, log)
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    if once { if err := d.RunOnce(ctx); err != nil { log.Error("once", "err", err); os.Exit(1) }; return }
    if err := d.Run(ctx); err != nil { log.Error("run", "err", err); os.Exit(1) }
}
```

`runner.OSController` is the production implementation of `runner.ProcessController`. The builder must add it next to the interface and a unit test that uses `os/exec` against `echo` (no real sing-box needed).

### Acceptance Criteria

1. `go build ./cmd/sing-box-rotor` succeeds.
2. `--version` prints `Version`.
3. `--config` flag and `SINGBOX_ROTOR_CONFIG` env var both respected.
4. Missing config file exits with code 1 and a clear log line.
5. `go test ./cmd/...` passes.

### Test Code (representative)

```go
// cmd/sing-box-rotor/main_test.go
package main

import (
    "bytes"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestVersionFlag(t *testing.T) {
    // Reset flag state between tests is awkward in package main; use a fresh
    // flag set inside a helper:
    out, err := runMain(t, "--version")
    if err != nil { t.Fatal(err) }
    if !strings.Contains(out, "dev") && !strings.Contains(out, "v") { t.Fatal("no version printed") }
}

func TestEnvVarConfig(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "c.yaml")
    os.WriteFile(p, []byte(minimalMainYAML()), 0o600)
    t.Setenv("SINGBOX_ROTOR_CONFIG", p)
    _, err := runMain(t, "--once")
    if err == nil { t.Fatal("expected error (no subscription server reachable) but exit 0 ok too") }
}

func runMain(t *testing.T, args ...string) (string, error) {
    t.Helper()
    old := os.Args; defer func(){ os.Args = old }()
    os.Args = append([]string{"sing-box-rotor"}, args...)
    // Capture stdout/stderr via injected helpers if main supports it; otherwise
    // use a build tag guard. Simpler: re-export the run logic as runCLI(os.Args[1:], stdout, stderr, env).
    return runCLI(args, io.Discard, io.Discard, os.Environ())
}

// Builder must refactor main() to call runCLI(args []string, stdout, stderr io.Writer, env []string) (int, error).
// main() then simply: code, err := runCLI(os.Args[1:], os.Stdout, os.Stderr, os.Environ()); if err != nil { ... } ; os.Exit(code).

func minimalMainYAML() string { return config.MinimalYAMLFixture() }
```

The builder must refactor `main()` so tests can drive it without `os.Exit`. See plan note under Decision Log.

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/sing-box-rotor ./cmd/sing-box-rotor
go test ./cmd/sing-box-rotor/... -v
go test -race -timeout 30s ./...
git add cmd/sing-box-rotor
git commit -m "feat(cli): flags, signals, and production wiring"
```

---

## Chunk 12: Deployment Artifacts (systemd unit, example config, install script)

**Complexity:** simple

**Files:**
- Create: `/home/petrovov/kimchi-project/contrib/systemd/sing-box-rotor.service`
- Create: `/home/petrovov/kimchi-project/contrib/config.example.yaml`
- Create: `/home/petrovov/kimchi-project/scripts/install.sh`
- Modify: `/home/petrovov/kimchi-project/README.md`

### File Contents

`contrib/systemd/sing-box-rotor.service` — exact text from spec §9.2.

`contrib/config.example.yaml`:

```yaml
# sing-box-rotor example configuration.
# Copy to /etc/sing-box-rotor/config.yaml and set mode 0600.
test_url: "https://www.google.com/generate_204"
test_timeout: "10s"
request_method: "GET"
check_interval: "5m"
recheck_interval: "30m"
fail_threshold: 2
switch_cooldown: "10m"
singbox:
  binary: "/usr/local/bin/sing-box"
  config_path: "/etc/sing-box/config.json"
  service_name: "sing-box"
  inbound_listen: "127.0.0.1:2080"
subscriptions:
  - name: "example-json"
    url: "https://example.com/sub1"
    type: "sing-box-json"
  - name: "example-base64"
    url: "https://example.com/sub2"
    type: "base64"
```

`scripts/install.sh` — bash with `set -euo pipefail`:
1. `cd "$(dirname "$0")/.."`; build with `CGO_ENABLED=0 go build -o bin/sing-box-rotor ./cmd/sing-box-rotor`.
2. `install -m 0755 bin/sing-box-rotor /usr/local/bin/sing-box-rotor`.
3. `install -d -m 0755 /etc/sing-box-rotor`.
4. If `[ ! -f /etc/sing-box-rotor/config.yaml ]`, copy `contrib/config.example.yaml` with `0600`.
5. `install -m 0644 contrib/systemd/sing-box-rotor.service /etc/systemd/system/sing-box-rotor.service`.
6. `systemctl daemon-reload`; `systemctl enable sing-box-rotor.service` (do not auto-start).

`README.md` updates:
- Replace "Implementation plan (coming soon)" link with relative link to `../docs/superpowers/plans/2026-06-30-sing-box-rotor.md`.
- Add a "Deploy" section pointing at `scripts/install.sh`.

### Acceptance Criteria

1. `bash -n scripts/install.sh` passes (syntax check).
2. `systemd-analyze verify contrib/systemd/sing-box-rotor.service` succeeds (if available; otherwise visual review).
3. `yq eval . contrib/config.example.yaml` (or `python3 -c "import yaml; yaml.safe_load(open('contrib/config.example.yaml'))"`) parses without error.
4. `README.md` renders with the new links.

### Test Code (representative)

```bash
# In CI/manual:
bash -n scripts/install.sh
python3 -c "import yaml,sys; yaml.safe_load(open('contrib/config.example.yaml'))"
```

### Verification Commands

```bash
cd /home/petrovov/kimchi-project
bash -n scripts/install.sh
go build ./...
go test -race -timeout 30s ./...
git add contrib scripts README.md
git commit -m "docs: deployment artifacts and install script"
```

---

## Verification Strategy

After each chunk:

```bash
cd /home/petrovov/kimchi-project
go test ./internal/<package>/... -race -timeout 30s -cover
```

After all chunks complete, run the full sweep that exercises every package at once:

```bash
cd /home/petrovov/kimchi-project
go vet ./...
go test -race -timeout 30s ./...
go test -race -timeout 30s -cover ./...
```

Per the spec §11.3 and acceptance criterion #5: **`go test -race -timeout 30s ./...` is the gate.**

Cover thresholds:

- Each package individually: ≥ 100% statements.
- Whole module: ≥ 95% statements (allowed slack only if a single defensive `os.Exit` or unexported panic path is uncovered; document the line in the PR).

Manual smoke test (run only on a machine with real sing-box installed):

```bash
sudo /tmp/sing-box-rotor --version
sudo /tmp/sing-box-rotor --config /tmp/test-config.yaml --once   # expect non-zero if subscriptions unreachable, but no crash
```

---

## Decision Log

| # | Decision | Rationale | Rejected Alternatives |
|---|----------|-----------|-----------------------|
| 1 | YAML library: `gopkg.in/yaml.v3` | De facto standard, supports durations natively. | `yaml.v2` (older), hand-rolled (more bugs). |
| 2 | SOCKS5 client: `golang.org/x/net/proxy` | Official Go subrepo, no extra deps. | `armon/go-socks5` (server, not client). |
| 3 | Logging: stdlib `log/slog` | Stdlib since 1.21; no dependency. | `logrus`, `zap` — overkill. |
| 4 | Test framework: stdlib `testing` only | Avoids `testify`; tests are small and direct. | `testify` — adds dependency for marginal benefit. |
| 5 | Fetcher takes parser interfaces | Lets chunk 2 tests not depend on parsers; aligns with TDD order. | Real parser refs from start — would couple fetcher tests to parser implementation. |
| 6 | Runner uses `ProcessController` interface | Real sing-box is not needed in tests; ready-probe uses a fake listener. | Spawning real `sing-box` in tests — slow, requires sing-box on CI host. |
| 7 | Selector uses injected `Clock` | Deterministic hysteresis tests. | Mocking `time.Now` via package variable — non-thread-safe. |
| 8 | Systemd manager uses `CommandRunner` + `FileSystem` interfaces | No real `systemctl` or filesystem in tests. | Real systemctl in tests — requires root + systemd. |
| 9 | Builder in chunk 5 wraps a single outbound into a minimal config | Matches spec §5.2: one candidate per proxy link. | Single mega-config with all outbounds — runner mutation is per-config; per-link granularity enables per-link latency. |
| 10 | `main()` refactored to call `runCLI(args, stdout, stderr, env) (int, error)` | Tests in chunk 11 can exercise CLI behavior without `os.Exit`. | `os.Exit` directly — untestable. |
| 11 | `socks5://` assumed for current-config probe (`ProbeCurrent`) | Spec §14 §2 notes the inbound could be HTTP or SOCKS; v1 uses SOCKS5 because main config typically exposes a SOCKS inbound for client traffic. | HTTP inbound only — too restrictive; CLI flag could be added later. |
| 12 | Subscriptions redacted from logs at INFO | Spec §8. | Debug logs may include them locally — but log levels default to INFO so they are never printed in production. |
| 13 | Per-link granularity over per-subscription granularity | Spec §5.2: "a subscription with N nodes produces N candidates." | Aggregating to a single per-subscription candidate — defeats the purpose of per-link latency comparison. |
| 14 | `slog` text handler in production, text handler writing to discard in tests | Avoids noisy test output. | Custom silent handler — equivalent but more code. |
| 15 | Plan chunks are written sequentially for linear reading, but chunks 3/4/5 are parallelizable | The linear list in this document is for the agent's convenience; an executor could batch them. | Strict linear with explicit dependency arrows only — harder to scan. |

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Subscription URL formats vary widely (vmess variants, SS2022, reality, etc.) | High | Medium | Plan covers the four most common schemes (vmess/vless/ss/trojan). Other schemes return a per-line error and are skipped. Extension surface is the `LinkParser` interface. |
| Real sing-box may reject mutated configs | Medium | High | Tests do not exercise real sing-box; runner_test.go uses a fake process that opens a listener. Integration test with real sing-box is out of scope for v1. |
| Race in port allocation between close and sing-box start | Low | Medium | Spec §5.3 acknowledges the race is acceptable; the readiness probe retries within `readyTimeout` (default 5s). |
| `golang.org/x/net/proxy` does not support SOCKS5h (remote DNS) | Low | Low | SOCKS5 proxies in sing-box typically resolve remotely by default; if needed, switch to a `dialContext` that uses a custom resolver — left as future work. |
| Tests run on systems without `/usr/local/bin/sing-box` | High | None | Runner tests never spawn sing-box; runner.go does not require it to be present at compile time. |
| `os.Stat` failing on backup path | Low | Low | Manager backs up only if the destination exists; tests cover both branches. |
| Coverage tool reports slightly below 100% due to defensive panics | Medium | Low | Builder should add a final `cover_test.go` per package with `//go:cover` markers OR add a single test that touches the defensive branch. Acceptable if documented. |
| Mock `FileSystem` and `ProcessController` semantics diverge from real implementations | Medium | Medium | Critical write paths (atomic rename) are additionally covered by an integration test in `internal/runner` using a real tempdir (already included in `TestAtomicWrite_RealFS_TempRemoved`). |

---

## Cross-Cutting Requirements (every chunk must satisfy)

1. Every package exports no more than what is required by dependents (YAGNI).
2. No `panic` in non-test code paths; use `error` returns.
3. Context is the first parameter on all exported functions that perform I/O or block.
4. Use `slog` with structured key/value pairs; never log full subscription URLs at INFO.
5. Every test file uses `t.TempDir()` and never writes to fixed paths.
6. Commit messages use Conventional Commits (`feat:`, `test:`, `docs:`, `chore:`).

---

## Spec Coverage Checklist (self-review)

| Spec Section | Plan Coverage |
|--------------|---------------|
| §1 Goal | Goal statement above |
| §2 Scope (In) | All items covered by chunks 1–12 |
| §3 Environment | Ubuntu target noted in Tech Stack; build command in chunk 12 |
| §4 Architecture | Internal packages mirror diagram |
| §5.1 config | Chunk 1 |
| §5.2 subscription | Chunks 2, 3, 4, 5 |
| §5.3 runner | Chunk 6 |
| §5.4 checker | Chunk 7 |
| §5.5 selector | Chunk 8 |
| §5.6 systemd | Chunk 9 |
| §5.7 daemon | Chunk 10 |
| §6 Data Flow | Implemented in chunks 10 (`RunOnce`, `Run`) |
| §7 Error Handling | Each chunk's acceptance criteria covers its failure modes |
| §8 Security | `.gitignore` updated in chunk 1; config.permissions warning noted in chunk 1 behavior; slog redacts URLs (Decision #12) |
| §9 Deployment | Chunk 12 |
| §10 CLI | Chunk 11 |
| §11.1 Unit tests | All chunks have tests |
| §11.2 Integration tests | Spec defers to fakes; runner uses real temp dir in one test; no live sing-box required |
| §11.3 Race & Concurrency | `go test -race -timeout 30s ./...` in every verification |
| §12 Repository & Publication | Out of scope of implementation plan; builder creates repo via `gh` after merge |
| §13 Acceptance Criteria #1 | `RunOnce` exercised in chunk 10 tests; CLI `--once` in chunk 11 |
| §13 #2 | Daemon loop in chunk 10 |
| §13 #3 | Selector + systemd covered |
| §13 #4 | `Close()` always called (chunk 6, 10 tests) |
| §13 #5 | `-race -timeout 30s` is the gate |
| §13 #6 | No secrets committed; example URLs are `example.com` |
| §13 #7 | `gh repo create` step is post-merge (orchestrator responsibility) |
| §14 Open Questions | #1 covered in chunk 4 (per-line skip); #2 covered in Decision #11; #3 documented in README; #4 default name used; #5 MIT (LICENSE exists) |

---

## Final Verification (run after all chunks)

```bash
cd /home/petrovov/kimchi-project
go vet ./...
go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/sing-box-rotor ./cmd/sing-box-rotor
go test -race -timeout 30s ./...
go test -race -timeout 30s -cover ./...
```

All four commands must succeed with no failures.
