package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// minimalValidYAML is the smallest config that should load successfully and
// have all defaults applied.
const minimalValidYAML = `
test_url: "https://www.gstatic.com/generate_204"
singbox:
  binary: "/usr/local/bin/sing-box"
  config_path: "/etc/sing-box/config.json"
  service_name: "sing-box"
  inbound_listen: "127.0.0.1:2080"
subscriptions:
  - name: "primary"
    url: "https://example.com/sub"
    type: "sing-box-json"
`

// fullValidYAML overrides every field so we can assert the parser preserves
// user-supplied values and does not silently replace them with defaults.
const fullValidYAML = `
test_url: "https://www.example.com/health"
test_timeout: "3s"
request_method: "head"
check_interval: "1m"
recheck_interval: "15m"
fail_threshold: 5
switch_cooldown: "20m"
singbox:
  binary: "/usr/local/bin/sing-box"
  config_path: "/etc/sing-box/config.json"
  service_name: "sing-box.service"
  inbound_listen: "127.0.0.1:2080"
subscriptions:
  - name: "alpha"
    url: "https://a.example.com/sub"
    type: "sing-box-json"
  - name: "beta"
    url: "https://b.example.com/sub"
    type: "base64"
`

func TestLoad_ValidMinimal(t *testing.T) {
	cfg, err := LoadFromBytes([]byte(minimalValidYAML))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.TestURL != "https://www.gstatic.com/generate_204" {
		t.Errorf("TestURL = %q", cfg.TestURL)
	}
	if cfg.TestTimeout != DefaultTestTimeout {
		t.Errorf("TestTimeout = %s, want %s", cfg.TestTimeout, DefaultTestTimeout)
	}
	if cfg.RequestMethod != "GET" {
		t.Errorf("RequestMethod = %q, want GET", cfg.RequestMethod)
	}
	if cfg.CheckInterval != DefaultCheckInterval {
		t.Errorf("CheckInterval = %s", cfg.CheckInterval)
	}
	if cfg.RecheckInterval != DefaultRecheckInterval {
		t.Errorf("RecheckInterval = %s", cfg.RecheckInterval)
	}
	if cfg.FailThreshold != DefaultFailThreshold {
		t.Errorf("FailThreshold = %d", cfg.FailThreshold)
	}
	if cfg.SwitchCooldown != DefaultSwitchCooldown {
		t.Errorf("SwitchCooldown = %s", cfg.SwitchCooldown)
	}
	if cfg.SingBox.Binary != "/usr/local/bin/sing-box" {
		t.Errorf("Binary = %q, want /usr/local/bin/sing-box", cfg.SingBox.Binary)
	}
	if cfg.SingBox.ConfigPath != "/etc/sing-box/config.json" {
		t.Errorf("ConfigPath = %q", cfg.SingBox.ConfigPath)
	}
}

func TestLoad_AllFields(t *testing.T) {
	cfg, err := LoadFromBytes([]byte(fullValidYAML))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.TestURL != "https://www.example.com/health" {
		t.Errorf("TestURL = %q", cfg.TestURL)
	}
	if cfg.TestTimeout != 3*time.Second {
		t.Errorf("TestTimeout = %s", cfg.TestTimeout)
	}
	if cfg.RequestMethod != "HEAD" {
		t.Errorf("RequestMethod = %q, want HEAD (normalized)", cfg.RequestMethod)
	}
	if cfg.CheckInterval != time.Minute {
		t.Errorf("CheckInterval = %s", cfg.CheckInterval)
	}
	if cfg.RecheckInterval != 15*time.Minute {
		t.Errorf("RecheckInterval = %s", cfg.RecheckInterval)
	}
	if cfg.FailThreshold != 5 {
		t.Errorf("FailThreshold = %d", cfg.FailThreshold)
	}
	if cfg.SwitchCooldown != 20*time.Minute {
		t.Errorf("SwitchCooldown = %s", cfg.SwitchCooldown)
	}
	if cfg.SingBox.Binary != "/usr/local/bin/sing-box" {
		t.Errorf("Binary = %q", cfg.SingBox.Binary)
	}
	if cfg.SingBox.ConfigPath != "/etc/sing-box/config.json" {
		t.Errorf("ConfigPath = %q", cfg.SingBox.ConfigPath)
	}
	if cfg.SingBox.Service != "sing-box.service" {
		t.Errorf("Service = %q", cfg.SingBox.Service)
	}
	if cfg.SingBox.Inbound != "127.0.0.1:2080" {
		t.Errorf("Inbound = %q", cfg.SingBox.Inbound)
	}
	if len(cfg.Subscriptions) != 2 {
		t.Fatalf("len(Subscriptions) = %d", len(cfg.Subscriptions))
	}
	if cfg.Subscriptions[0].Name != "alpha" || cfg.Subscriptions[1].Type != "base64" {
		t.Errorf("subscriptions not parsed in order: %+v", cfg.Subscriptions)
	}
}

func TestLoad_FileMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.yaml")
	_, err := Load(missing)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist.yaml") {
		t.Errorf("error %q should mention path", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("this: is: not: valid: yaml: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected YAML parse error, got nil")
	}
	if !strings.Contains(err.Error(), "YAML") {
		t.Errorf("error %q should mention YAML", err)
	}
}

// helper: build a config from a YAML snippet and apply defaults so we only
// test the Validate method.
func mustParse(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	cfg.applyDefaults()
	return cfg
}

// helper: build a config from a YAML snippet WITHOUT applying defaults so we
// can test zero/negative duration rejection.
func mustParseRaw(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	return cfg
}

func TestValidate_MissingTestURL(t *testing.T) {
	cfg := mustParse(t, `
subscriptions:
  - name: "a"
    url: "https://example.com"
    type: "base64"
`)
	cfg.TestURL = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "test_url") {
		t.Fatalf("want error mentioning test_url, got %v", err)
	}
}

func TestValidate_BadTestURLScheme(t *testing.T) {
	cases := []string{
		"ftp://example.com/x",
		"file:///etc/passwd",
		"javascript:alert(1)",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			cfg := mustParse(t, minimalValidYAML)
			cfg.TestURL = raw
			if err := cfg.Validate(); err == nil {
				t.Fatalf("want error for %q, got nil", raw)
			} else if !strings.Contains(err.Error(), "test_url") {
				t.Errorf("error %q should mention test_url", err)
			}
		})
	}
}

func TestValidate_NoSubscriptions(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.Subscriptions = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for empty subscriptions, got nil")
	} else if !strings.Contains(err.Error(), "subscription") {
		t.Errorf("error %q should mention subscriptions", err)
	}
}

func TestValidate_DuplicateSubscriptionNames(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.Subscriptions = append(cfg.Subscriptions, Subscription{
		Name: "primary",
		URL:  "https://duplicate.example.com",
		Type: "base64",
	})
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for duplicate names, got nil")
	} else if !strings.Contains(err.Error(), "primary") {
		t.Errorf("error %q should mention duplicate name", err)
	}
}

func TestValidate_BadSubscriptionType(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.Subscriptions[0].Type = "yaml"
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for unknown type, got nil")
	} else if !strings.Contains(err.Error(), "type") {
		t.Errorf("error %q should mention type", err)
	}
}

func TestValidate_BadSubscriptionURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"not a url", "not a url at all"},
		{"missing scheme", "://broken"},
		{"host empty", "http://"},
		{"parse error", "://[::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustParse(t, minimalValidYAML)
			cfg.Subscriptions[0].URL = tc.raw
			if err := cfg.Validate(); err == nil {
				t.Fatalf("want error for %q, got nil", tc.raw)
			} else if !strings.Contains(err.Error(), "url") {
				t.Errorf("error %q should mention url", err)
			}
		})
	}
}

func TestValidate_EmptySubscriptionName(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.Subscriptions[0].Name = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for empty subscription name, got nil")
	} else if !strings.Contains(err.Error(), "name") {
		t.Errorf("error %q should mention name", err)
	}
}

func TestValidate_EmptySubscriptionURL(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.Subscriptions[0].URL = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for empty subscription URL, got nil")
	} else if !strings.Contains(err.Error(), "url") {
		t.Errorf("error %q should mention url", err)
	}
}

func TestValidate_ZeroDuration(t *testing.T) {
	cases := []struct {
		name  string
		field string
		set   func(*Config)
	}{
		{"test_timeout", "test_timeout", func(c *Config) { c.TestTimeout = 0 }},
		{"check_interval", "check_interval", func(c *Config) { c.CheckInterval = 0 }},
		{"recheck_interval", "recheck_interval", func(c *Config) { c.RecheckInterval = 0 }},
		{"switch_cooldown", "switch_cooldown", func(c *Config) { c.SwitchCooldown = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustParse(t, minimalValidYAML)
			tc.set(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("want error for zero %s, got nil", tc.field)
			} else if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q should mention %s", err, tc.field)
			}
		})
	}
}

func TestValidate_NegativeDuration(t *testing.T) {
	cfg := mustParseRaw(t, `
test_url: "https://example.com"
test_timeout: "-1s"
singbox:
  binary: "/usr/bin/sing-box"
  config_path: "/etc/sb.json"
  service_name: "sb.service"
  inbound_listen: "127.0.0.1:2080"
subscriptions:
  - name: "a"
    url: "https://example.com"
    type: "base64"
`)
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for negative test_timeout, got nil")
	}
}

func TestValidate_ZeroFailThreshold(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.FailThreshold = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for fail_threshold=0, got nil")
	} else if !strings.Contains(err.Error(), "fail_threshold") {
		t.Errorf("error %q should mention fail_threshold", err)
	}
}

func TestValidate_MissingSingBoxFields(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Config)
		wantField string
	}{
		{"binary", func(c *Config) { c.SingBox.Binary = "" }, "binary"},
		{"config_path", func(c *Config) { c.SingBox.ConfigPath = "" }, "config_path"},
		{"service_name", func(c *Config) { c.SingBox.Service = "" }, "service_name"},
		{"inbound_listen", func(c *Config) { c.SingBox.Inbound = "" }, "inbound_listen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustParse(t, minimalValidYAML)
			// Patch in non-empty singbox fields so we isolate the one under test.
			cfg.SingBox.Binary = "/usr/bin/sing-box"
			cfg.SingBox.ConfigPath = "/etc/sb.json"
			cfg.SingBox.Service = "sb.service"
			cfg.SingBox.Inbound = "127.0.0.1:2080"
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("want error for empty %s, got nil", tc.wantField)
			}
			if !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("error %q should mention %s", err, tc.wantField)
			}
		})
	}
}

func TestValidate_BadRequestMethod(t *testing.T) {
	cfg := mustParse(t, minimalValidYAML)
	cfg.RequestMethod = "POST"
	if err := cfg.Validate(); err == nil {
		t.Fatal("want error for POST method, got nil")
	} else if !strings.Contains(err.Error(), "request_method") {
		t.Errorf("error %q should mention request_method", err)
	}
}

func TestSubscriptionTypes(t *testing.T) {
	types := SubscriptionTypes()
	if !types["sing-box-json"] {
		t.Error("sing-box-json should be allowed")
	}
	if !types["base64"] {
		t.Error("base64 should be allowed")
	}
	if types["yaml"] || types["clash"] || types["unknown"] {
		t.Errorf("unexpected types in map: %v", types)
	}
	// Mutating the returned map must not affect subsequent calls.
	types["bogus"] = true
	if SubscriptionTypes()["bogus"] {
		t.Error("SubscriptionTypes returned shared map; mutations leak")
	}
}

func TestApplyDefaults_AllZero(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.TestTimeout != DefaultTestTimeout {
		t.Errorf("TestTimeout = %s", cfg.TestTimeout)
	}
	if cfg.RequestMethod != DefaultRequestMethod {
		t.Errorf("RequestMethod = %s", cfg.RequestMethod)
	}
	if cfg.CheckInterval != DefaultCheckInterval {
		t.Errorf("CheckInterval = %s", cfg.CheckInterval)
	}
	if cfg.RecheckInterval != DefaultRecheckInterval {
		t.Errorf("RecheckInterval = %s", cfg.RecheckInterval)
	}
	if cfg.FailThreshold != DefaultFailThreshold {
		t.Errorf("FailThreshold = %d", cfg.FailThreshold)
	}
	if cfg.SwitchCooldown != DefaultSwitchCooldown {
		t.Errorf("SwitchCooldown = %s", cfg.SwitchCooldown)
	}
}

func TestApplyDefaults_PreservesUserValues(t *testing.T) {
	cfg := &Config{
		TestTimeout:     7 * time.Second,
		RequestMethod:   "HEAD",
		CheckInterval:   2 * time.Minute,
		RecheckInterval: 7 * time.Minute,
		FailThreshold:   9,
		SwitchCooldown:  1 * time.Minute,
	}
	cfg.applyDefaults()
	if cfg.TestTimeout != 7*time.Second {
		t.Errorf("user TestTimeout overwritten: %s", cfg.TestTimeout)
	}
	if cfg.RequestMethod != "HEAD" {
		t.Errorf("user RequestMethod overwritten: %s", cfg.RequestMethod)
	}
	if cfg.CheckInterval != 2*time.Minute {
		t.Errorf("user CheckInterval overwritten: %s", cfg.CheckInterval)
	}
	if cfg.RecheckInterval != 7*time.Minute {
		t.Errorf("user RecheckInterval overwritten: %s", cfg.RecheckInterval)
	}
	if cfg.FailThreshold != 9 {
		t.Errorf("user FailThreshold overwritten: %d", cfg.FailThreshold)
	}
	if cfg.SwitchCooldown != time.Minute {
		t.Errorf("user SwitchCooldown overwritten: %s", cfg.SwitchCooldown)
	}
}

func TestLoad_RoundTripFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(fullValidYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SingBox.Binary != "/usr/local/bin/sing-box" {
		t.Errorf("Binary = %q", cfg.SingBox.Binary)
	}
}

func TestLoad_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(path, []byte(minimalValidYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the config invalid after parsing by removing subscriptions.
	// We cannot easily do that through the file, so write a YAML that is syntactically
	// valid but fails validation (missing test_url).
	body := `
singbox:
  binary: "/usr/bin/sing-box"
  config_path: "/etc/sb.json"
  service_name: "sb"
  inbound_listen: "127.0.0.1:2080"
subscriptions:
  - name: "a"
    url: "https://example.com"
    type: "base64"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "test_url") {
		t.Errorf("error %q should mention test_url", err)
	}
}
