# sing-box-rotor Design Specification

> **Version:** 1.0  
> **Date:** 2026-06-30  
> **Status:** Draft — awaiting implementation plan

---

## 1. Goal

Build a Linux daemon (`sing-box-rotor`) that manages multiple sing-box configurations obtained from subscription URLs, periodically tests their performance through a proxy, and automatically switches to the configuration with the lowest latency while keeping service disruption to a minimum.

---

## 2. Scope

### In scope
- Daemon executable written in Go.
- YAML configuration file for the utility itself.
- Fetching subscription URLs at runtime.
- Supporting two subscription formats:
  - `sing-box-json` — direct sing-box JSON configuration.
  - `base64` — base64-encoded proxy list (Clash/Shadowrocket style) that must be converted to a sing-box config.
- Testing each candidate configuration by running a temporary sing-box instance on a random free port.
- Measuring HTTP latency to a global test URL through the temporary proxy.
- Selecting the best available configuration based on latency.
- Writing the active configuration to a known path and restarting the main sing-box systemd service only when switching.
- Hysteresis: consecutive-failure threshold and cooldown between switches.
- Deployment as a systemd service with a periodic check loop.
- Comprehensive unit and integration tests.
- Initial public GitHub repository created and pushed via `gh`.

### Out of scope (for v1)
- Web UI or metrics endpoint.
- Multiple active configurations / load balancing.
- Encrypted or authenticated subscription formats beyond standard base64.
- IPv6-specific handling beyond what sing-box already provides.
- Windows or macOS support.

---

## 3. Environment & Constraints

- **OS:** Ubuntu 22.04–24.04.
- **sing-box:** already installed and running separately (tested with v1.12.20).
- **Privileges:** the daemon needs permission to write the main sing-box config file and to run `systemctl restart <sing-box-service>`.
- **Security:** no real API keys, subscription URLs, or actual proxy credentials may be committed to the public repository. All secrets live only in local config files that are explicitly excluded from git.
- **Deployment:** the utility is packaged as a single static binary and runs as its own systemd service.

---

## 4. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         sing-box-rotor daemon                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────┐ │
│  │ config       │  │ subscription │  │ runner       │  │ checker  │ │
│  │ loader       │──▶│ fetcher      │──▶│ (temp        │──▶│ (latency │ │
│  └──────────────┘  └──────────────┘  │  sing-box)   │  │  probe)  │ │
│                                      └──────────────┘  └────┬─────┘ │
│                                                             │       │
│                                      ┌──────────────────────┘       │
│                                      ▼                               │
│                          ┌──────────────────────┐                    │
│                          │   selector/strategy  │                    │
│                          │  + hysteresis logic  │                    │
│                          └──────────┬───────────┘                    │
│                                     │                                │
│                                     ▼                                │
│                          ┌──────────────────────┐                    │
│                          │  systemd manager     │                    │
│                          │ (write + restart)    │                    │
│                          └──────────┬───────────┘                    │
│                                     │                                │
└─────────────────────────────────────┼────────────────────────────────┘
                                      ▼
                           ┌─────────────────────┐
                           │ main sing-box       │
                           │ systemd service     │
                           └─────────────────────┘
```

The daemon never modifies the running sing-box service while testing candidates. It spawns temporary sing-box child processes, probes them, selects the best one, and only then applies the change to the main service.

---

## 5. Components

### 5.1. `config` — Configuration Loader

**Responsibility:** Load, parse, and validate the utility's YAML configuration file.

**Configuration file location:** `/etc/sing-box-rotor/config.yaml` (default), overridable via `--config` flag or `SINGBOX_ROTOR_CONFIG` env var.

**Schema:**

```yaml
# Test target
test_url: "https://www.google.com/generate_204"  # required
test_timeout: "10s"                               # default 10s
request_method: "GET"                             # GET or HEAD; default GET

# Scheduling
check_interval: "5m"      # how often to check current config; default 5m
recheck_interval: "30m"   # how often to re-test all candidates; default 30m

# Hysteresis
fail_threshold: 2         # consecutive failures before considering a switch; default 2
switch_cooldown: "10m"    # minimum time between actual switches; default 10m

# sing-box integration
singbox:
  binary: "/usr/local/bin/sing-box"        # required if not on PATH
  config_path: "/etc/sing-box/config.json" # main config file the service uses
  service_name: "sing-box"                 # systemd service name
  inbound_listen: "127.0.0.1:2080"         # main service inbound (for current-config checks)

# Subscriptions
subscriptions:
  - name: "sub-1"
    url: "https://example.com/sub1"
    type: "sing-box-json"
  - name: "sub-2"
    url: "https://example.com/sub2"
    type: "base64"
```

**Validation rules:**
- `test_url` must be a valid HTTPS or HTTP URL.
- `subscriptions` must contain at least one entry.
- Each subscription must have a unique `name`, a valid `url`, and `type` ∈ {`sing-box-json`, `base64`}.
- All duration fields must be positive and at least 1 second.

**Go type (sketch):**

```go
type Config struct {
    TestURL         string          `yaml:"test_url"`
    TestTimeout     time.Duration   `yaml:"test_timeout"`
    RequestMethod   string          `yaml:"request_method"`
    CheckInterval   time.Duration   `yaml:"check_interval"`
    RecheckInterval time.Duration   `yaml:"recheck_interval"`
    FailThreshold   int             `yaml:"fail_threshold"`
    SwitchCooldown  time.Duration   `yaml:"switch_cooldown"`
    SingBox         SingBoxConfig   `yaml:"singbox"`
    Subscriptions   []Subscription  `yaml:"subscriptions"`
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
    Type string `yaml:"type"`
}
```

### 5.2. `subscription` — Subscription Fetcher

**Responsibility:** Download subscription URLs and convert them into sing-box JSON configurations.

**Behavior:**
- For each subscription:
  - Perform HTTP GET with a reasonable timeout (e.g., 30s).
  - Follow up to 10 redirects.
  - If download fails, log a warning and skip the subscription.
- If `type == "sing-box-json"`:
  - Validate that the body is valid JSON and contains required sing-box fields (`outbounds` or `route`).
  - Return the config as-is.
- If `type == "base64"`:
  - Decode base64.
  - Parse proxy links (VMess, VLESS, Shadowsocks, Trojan, etc.) — support at least the four most common: `vmess://`, `vless://`, `ss://`, `trojan://`.
  - Convert each link to a sing-box outbound.
  - For each outbound, build a minimal runnable sing-box config containing that outbound plus a placeholder route.
  - Return one candidate config per proxy link (so a subscription with N nodes produces N candidates).

**Output:** A slice of `CandidateConfig`:

```go
type CandidateConfig struct {
    Name   string          // candidate display name (e.g. "sub-1/node-3")
    Source string          // subscription URL (used only in local error messages, never committed)
    Raw    []byte          // raw sing-box JSON
    Parsed map[string]any  // parsed JSON for manipulation
}
```

**Security note:** subscription URLs and credentials are runtime data. They must never be written to repository files or logs committed anywhere.

### 5.3. `runner` — Temporary sing-box Runner

**Responsibility:** Run a sing-box process with a candidate configuration on a random free local port for latency testing.

**Behavior:**
- Allocate a random free TCP port on `127.0.0.1` (`:0`).
- Mutate the candidate config:
  - Remove any existing inbounds, experimental fields, API, or server bindings that might conflict.
  - Inject a single SOCKS inbound bound to `127.0.0.1:<random-port>`.
  - Keep the original outbounds and route intact.
- Write the mutated config to a temporary file in `/tmp/sing-box-rotor-*`.
- Run `sing-box run -c <temp-file>` as a child process.
- Wait for the process to be ready by attempting to connect to the SOCKS/HTTP port with retries and timeout (e.g., 5s).
- On success, return a `RunnerHandle` containing the process, temp file path, and proxy address.
- On failure, kill the process, clean up the temp file, and return an error.
- Provide a `Close()` method that always kills the process and removes the temp file.

**Concurrency:** Multiple runners can run in parallel. Each uses its own port and temp file.

**Go type (sketch):**

```go
type RunnerHandle struct {
    ProxyURL string       // e.g., "socks5://127.0.0.1:54321"
    Close    func() error // cleanup
}

func Start(ctx context.Context, binary string, cfg CandidateConfig) (*RunnerHandle, error)
```

### 5.4. `checker` — Latency Checker

**Responsibility:** Measure HTTP latency to the global test URL through a proxy.

**Behavior:**
- Build an HTTP client that uses the proxy URL (SOCKS5 or HTTP) from the runner.
- Set timeout from config (`test_timeout`).
- Use configured request method (`GET` or `HEAD`).
- Record `time.Since(start)` when the response headers are received (do not wait for body unless GET).
- Accept any HTTP status code 200–399 as success.
- Return `(latency, nil)` on success, `(0, err)` on failure.

**Output:**

```go
type Result struct {
    CandidateName string
    Latency       time.Duration
    Error         error
}
```

### 5.5. `selector` — Selection Strategy

**Responsibility:** Decide when and to which configuration to switch, applying hysteresis.

**State:**

```go
type Selector struct {
    currentConfig    string
    currentFails     int
    lastSwitchTime   time.Time
    failThreshold    int
    switchCooldown   time.Duration
}
```

**Algorithm:**

1. **Current-config check (every `check_interval`):**
   - Probe latency through main sing-box inbound.
   - If success:
     - Reset `currentFails` to 0.
     - Return "keep current".
   - If failure:
     - Increment `currentFails`.
     - If `currentFails < failThreshold`, return "keep current".
     - Else return "evaluate candidates".

2. **Candidate evaluation (triggered by threshold or `recheck_interval`):**
   - Start temporary runners for all candidate configs in parallel.
   - Probe latency through each.
   - Filter out failures.
   - If no candidate succeeds, keep current and log critical.
   - Pick candidate with minimum latency.
   - If picked candidate == current, return "keep current".
   - If picked candidate != current:
     - If `time.Since(lastSwitchTime) < switchCooldown`, return "keep current" and log that switch is deferred.
     - Else return "switch to <candidate>".

3. **After a switch:**
   - Update `currentConfig`.
   - Reset `currentFails` to 0.
   - Set `lastSwitchTime = now`.

### 5.6. `systemd` — Service Manager

**Responsibility:** Write the active configuration and restart the main sing-box systemd service.

**Behavior:**
- `Apply(cfg CandidateConfig) error`:
  - Marshal config to JSON with indentation.
  - Atomically write to `singbox.config_path` (write temp + rename).
  - Set file permissions to `0644` (or `0600` if containing secrets — see Security).
  - Run `systemctl restart <service_name>`.
  - Wait for service to become active (poll `systemctl is-active` up to 30s).
  - Return error if restart fails.

**Safety:**
- Validate that the new JSON is valid before writing.
- Keep a backup of the previous config at `<config_path>.bak` before replacing.

### 5.7. `daemon` — Main Loop

**Responsibility:** Orchestrate all components on a scheduler and handle shutdown.

**Behavior:**
- On startup:
  1. Load config.
  2. Fetch subscriptions.
  3. Run full candidate evaluation and apply best config.
  4. Enter main loop.
- Main loop:
  - Maintain two timers:
    - `checkTicker` fires every `check_interval`.
    - `recheckTicker` fires every `recheck_interval`.
  - On `checkTicker`:
    - Probe current config through main inbound.
    - If it fails threshold, trigger candidate evaluation.
  - On `recheckTicker`:
    - Always trigger candidate evaluation to discover if a better config recovered.
  - On shutdown signal (SIGINT/SIGTERM):
    - Stop timers.
    - Wait for in-flight checks to finish.
    - Clean up any temporary runners.
    - Exit.

---

## 6. Data Flow

### 6.1. Startup

```
Load config
    │
    ▼
Fetch subscriptions ──▶ convert to CandidateConfig[]
    │
    ▼
Evaluate all candidates (parallel temp runners + latency probes)
    │
    ▼
Select best ──▶ Apply to main sing-box service (write + restart)
    │
    ▼
Enter scheduler loop
```

### 6.2. Periodic Check

```
checkTicker fires
    │
    ▼
Probe current config via main inbound
    │
    ├── success ──▶ reset fail counter
    │
    └── failure ──▶ increment fail counter
              │
              ▼
        fail >= threshold?
              │
              ├── no ──▶ keep current
              │
              └── yes ──▶ evaluate candidates
                        │
                        ▼
                  best == current?
                        │
                        ├── yes ──▶ keep current
                        │
                        └── no ──▶ cooldown elapsed?
                                  │
                                  ├── no ──▶ defer switch
                                  │
                                  └── yes ──▶ switch
```

### 6.3. Periodic Recheck

```
recheckTicker fires
    │
    ▼
Evaluate all candidates
    │
    ▼
If a better config is found and cooldown elapsed, switch
```

---

## 7. Error Handling

| Scenario | Handling |
|---|---|
| Config file missing/invalid | Fatal on startup with clear error message and exit code 1. |
| Subscription fetch fails | Log warning, skip subscription, continue with others. |
| Subscription decode/convert fails | Log warning, skip that subscription. |
| Temp runner fails to start | Treat candidate as unavailable. |
| Latency probe times out | Treat candidate as unavailable. |
| All candidates unavailable | Keep current config, log critical error. |
| Main service restart fails | Log error, keep previous config backup, do not update selector state. |
| Shutdown during check | Finish in-flight probe, cleanup temp processes, exit cleanly. |

---

## 8. Security & Secrets Management

- **No secrets in repository:** The public repository contains only source code, tests, documentation, and example config files. Example config files must use placeholder URLs like `https://example.com/sub`.
- **Runtime-only credentials:** Subscription URLs and proxy credentials are loaded at runtime from `/etc/sing-box-rotor/config.yaml`.
- **File permissions:** The utility config should be readable only by the service user (mode `0600`). The daemon should warn if permissions are too permissive.
- **Logs:** Never log full subscription URLs or decoded proxy credentials at INFO level. DEBUG logs may include them locally but must not be shipped.
- **.gitignore:** Add entries for `/config.yaml`, `/etc/`, `*.key`, `*.secret`, and any local test fixtures containing real data.

---

## 9. Deployment

### 9.1. Binary

Build as a static Linux binary:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sing-box-rotor ./cmd/sing-box-rotor
```

### 9.2. systemd Service

Create `/etc/systemd/system/sing-box-rotor.service`:

```ini
[Unit]
Description=sing-box-rotor — automatic sing-box config selector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sing-box-rotor --config /etc/sing-box-rotor/config.yaml
Restart=on-failure
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
```

> Running as root is required to restart `sing-box.service` and write to `/etc/sing-box/config.json`. In a hardened setup, this can be replaced with passwordless sudo rules or D-Bus polkit rules.

### 9.3. Installation Script

Provide a minimal install script (for implementation plan) that:
1. Builds the binary.
2. Installs it to `/usr/local/bin/sing-box-rotor`.
3. Creates `/etc/sing-box-rotor/`.
4. Copies an example config.
5. Installs and enables the systemd service.

---

## 10. CLI Interface

```
sing-box-rotor [flags]

Flags:
  --config string   Path to config file (default "/etc/sing-box-rotor/config.yaml")
  --once            Run one evaluation cycle and exit (useful for testing)
  --version         Print version
  -v, --verbose     Enable debug logging
```

---

## 11. Testing Strategy

### 11.1. Unit Tests

- **config:** parse valid/invalid YAML, default values, validation errors.
- **selector:** hysteresis logic with deterministic clock, threshold, cooldown, no-candidate scenarios.
- **checker:** HTTP latency measurement using a local test server and a mock SOCKS/HTTP proxy.
- **subscription:** base64 decoding, link parsing, JSON validation.

### 11.2. Integration Tests

- Use a fake `sing-box` binary that listens on a configurable port and acts as a proxy.
- Test full daemon flow with mock subscription server and fake sing-box.
- Verify that the main config is written and service restart command is invoked.

### 11.3. Race & Concurrency

- Run all tests with `go test -race -timeout 30s ./...`.
- Ensure temp runners are cleaned up even on panic/cancellation.

---

## 12. Repository & Publication

- Initialize a new public GitHub repository via `gh repo create`.
- Repository name: `sing-box-rotor` (or as requested).
- Push the initial commit containing:
  - This design spec.
  - `README.md` with overview and usage.
  - `.gitignore` excluding secrets and build artifacts.
  - `LICENSE` (MIT by default unless specified otherwise).
  - Empty Go module skeleton.

Before creating the repo, verify `gh auth status` and required scopes.

---

## 13. Acceptance Criteria

1. `sing-box-rotor --once` loads subscriptions, evaluates candidates, and applies the best config successfully.
2. The daemon runs continuously, checks the current config every 5 minutes, and switches only after `fail_threshold` consecutive failures.
3. Switching respects `switch_cooldown` and causes only one restart of the main sing-box service per switch.
4. Temporary sing-box processes are cleaned up after every evaluation.
5. All tests pass with `go test -race -timeout 30s ./...`.
6. No real subscription URLs or credentials are present in the repository.
7. The project is pushed to a public GitHub repository created via `gh`.

---

## 14. Open Questions / Assumptions

1. **Subscription format `base64`:** Assumed to be a newline-separated list of proxy URIs (`vmess://`, `vless://`, `ss://`, `trojan://`). If other schemes are needed, they can be added later.
2. **Main sing-box inbound:** The utility assumes the main sing-box config exposes an HTTP/SOCKS inbound at `singbox.inbound_listen` for current-config probes.
3. **Privileges:** The daemon is expected to run as root or with equivalent permissions to restart the sing-box service and write its config.
4. **Initial repository name:** Assumed `sing-box-rotor`. The user may override.
5. **License:** Assumed MIT unless the user specifies otherwise.
