// Package config loads, parses, and validates the sing-box-rotor YAML
// configuration file. It is the single source of truth for runtime defaults
// and is consumed by every other internal package.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Package-level defaults applied to zero-valued Config fields by
// (*Config).applyDefaults.
const (
	DefaultTestTimeout     = 10 * time.Second
	DefaultRequestMethod   = "GET"
	DefaultCheckInterval   = 5 * time.Minute
	DefaultRecheckInterval = 30 * time.Minute
	DefaultFailThreshold   = 2
	DefaultSwitchCooldown  = 10 * time.Minute
)

// allowedSubscriptionTypes enumerates the supported subscription source
// formats. Exposed via SubscriptionTypes so other packages can validate
// without re-declaring the list.
var allowedSubscriptionTypes = map[string]bool{
	"sing-box-json": true,
	"base64":        true,
}

// SubscriptionTypes returns a copy of the allowed subscription type set.
// The returned map is safe for the caller to mutate.
func SubscriptionTypes() map[string]bool {
	out := make(map[string]bool, len(allowedSubscriptionTypes))
	for k, v := range allowedSubscriptionTypes {
		out[k] = v
	}
	return out
}

// Config is the top-level YAML configuration for the sing-box-rotor daemon.
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

// SingBoxConfig describes the local sing-box installation that sing-box-rotor
// drives: where the binary lives, where its main config is written, and which
// systemd service to restart on switch.
type SingBoxConfig struct {
	Binary     string `yaml:"binary"`
	ConfigPath string `yaml:"config_path"`
	Service    string `yaml:"service_name"`
	Inbound    string `yaml:"inbound_listen"`
}

// Subscription describes a single source of candidate proxy configurations.
type Subscription struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	Type string `yaml:"type"`
}

// Load reads the YAML configuration from path and returns a fully validated
// *Config with defaults applied.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	cfg, err := LoadFromBytes(data)
	if err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadFromBytes parses raw YAML bytes into a *Config. It does NOT apply
// defaults or validate; callers that want a fully usable config should use
// Load instead. This split exists so tests and other packages can inspect a
// partially populated config if they need to.
func LoadFromBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse YAML: %w", err)
	}
	return &cfg, nil
}

// applyDefaults fills zero-valued fields with the package defaults so the
// rest of the system can rely on every duration and counter being non-zero.
func (c *Config) applyDefaults() {
	if c.TestTimeout == 0 {
		c.TestTimeout = DefaultTestTimeout
	}
	if c.RequestMethod == "" {
		c.RequestMethod = DefaultRequestMethod
	}
	if c.CheckInterval == 0 {
		c.CheckInterval = DefaultCheckInterval
	}
	if c.RecheckInterval == 0 {
		c.RecheckInterval = DefaultRecheckInterval
	}
	if c.FailThreshold == 0 {
		c.FailThreshold = DefaultFailThreshold
	}
	if c.SwitchCooldown == 0 {
		c.SwitchCooldown = DefaultSwitchCooldown
	}
}

// Validate enforces every invariant required for a usable configuration.
// It returns the first violation encountered with a message that names the
// offending field. Callers are expected to have run applyDefaults first so
// that zero-valued durations don't masquerade as user-supplied values.
func (c *Config) Validate() error {
	if c.TestURL == "" {
		return errors.New("config: test_url is required")
	}
	if err := validateHTTPURL("test_url", c.TestURL); err != nil {
		return err
	}

	if err := validateDuration("test_timeout", c.TestTimeout); err != nil {
		return err
	}
	if err := validateDuration("check_interval", c.CheckInterval); err != nil {
		return err
	}
	if err := validateDuration("recheck_interval", c.RecheckInterval); err != nil {
		return err
	}
	if err := validateDuration("switch_cooldown", c.SwitchCooldown); err != nil {
		return err
	}

	if c.FailThreshold < 1 {
		return fmt.Errorf("config: fail_threshold must be >= 1, got %d", c.FailThreshold)
	}

	method := strings.ToUpper(strings.TrimSpace(c.RequestMethod))
	if method != "GET" && method != "HEAD" {
		return fmt.Errorf("config: request_method must be GET or HEAD, got %q", c.RequestMethod)
	}
	c.RequestMethod = method

	if c.SingBox.Binary == "" {
		return errors.New("config: singbox.binary is required")
	}
	if c.SingBox.ConfigPath == "" {
		return errors.New("config: singbox.config_path is required")
	}
	if c.SingBox.Service == "" {
		return errors.New("config: singbox.service_name is required")
	}
	if c.SingBox.Inbound == "" {
		return errors.New("config: singbox.inbound_listen is required")
	}

	if len(c.Subscriptions) < 1 {
		return errors.New("config: at least one subscription is required")
	}
	seen := make(map[string]struct{}, len(c.Subscriptions))
	for i := range c.Subscriptions {
		sub := &c.Subscriptions[i]
		if _, dup := seen[sub.Name]; dup {
			return fmt.Errorf("config: duplicate subscription name %q", sub.Name)
		}
		seen[sub.Name] = struct{}{}
		if err := sub.validate(); err != nil {
			return fmt.Errorf("config: subscriptions[%d] (%s): %w", i, sub.Name, err)
		}
	}
	return nil
}

// validate checks a single Subscription for required and well-formed fields.
// Field-level errors wrap the field name so the caller can pinpoint the
// problem.
func (s *Subscription) validate() error {
	if s.Name == "" {
		return errors.New("name is required")
	}
	if err := validateHTTPURL("url", s.URL); err != nil {
		return err
	}
	if !allowedSubscriptionTypes[s.Type] {
		return fmt.Errorf("type %q is not one of %v", s.Type, allowedSubscriptionKeys())
	}
	return nil
}

// validateHTTPURL ensures v is a syntactically valid http(s) URL with a
// non-empty host. The field name is included in any error so the caller
// knows which field of the parent struct failed.
func validateHTTPURL(field, v string) error {
	if v == "" {
		return fmt.Errorf("config: %s is required", field)
	}
	u, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("config: %s: invalid URL: %w", field, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("config: %s: scheme must be http or https, got %q", field, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("config: %s: host is required", field)
	}
	return nil
}

// validateDuration rejects zero or negative durations. We require at least
// one second because sub-second intervals break the health-check loop in
// practice and almost certainly indicate a typo.
func validateDuration(field string, d time.Duration) error {
	if d < time.Second {
		return fmt.Errorf("config: %s must be >= 1s, got %s", field, d)
	}
	return nil
}

// allowedSubscriptionKeys returns the allowed subscription types in a
// stable, sorted slice for deterministic error messages.
func allowedSubscriptionKeys() []string {
	out := make([]string, 0, len(allowedSubscriptionTypes))
	for k := range allowedSubscriptionTypes {
		out = append(out, k)
	}
	// Tiny fixed sort keeps error messages deterministic without dragging in
	// the sort package for two elements.
	if len(out) == 2 && out[0] > out[1] {
		out[0], out[1] = out[1], out[0]
	}
	return out
}
