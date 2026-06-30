# sing-box-rotor

A Linux daemon that automatically selects the fastest available sing-box configuration from a list of subscriptions and switches the running sing-box service with minimal disruption.

## What it does

- Fetches sing-box configurations from subscription URLs.
- Tests each configuration by running a temporary sing-box instance on a random port.
- Measures latency to a configurable test URL through the proxy.
- Applies hysteresis (failure threshold + switch cooldown) to avoid flapping.
- Writes the best configuration to the main sing-box config path and restarts the sing-box systemd service only when a switch is needed.

## Documentation

- [Design Specification](docs/superpowers/specs/2026-06-30-sing-box-rotor-design.md)
- [Implementation Plan](docs/superpowers/plans/2026-06-30-sing-box-rotor.md)

## Quick start

```bash
# Build
go build -o sing-box-rotor ./cmd/sing-box-rotor

# Run once for testing
sudo ./sing-box-rotor --config /etc/sing-box-rotor/config.yaml --once

# Run as a service
sudo systemctl start sing-box-rotor
```

## Configuration example

See [`contrib/config.example.yaml`](contrib/config.example.yaml).

## Current implementation status

The repository now contains the v1 daemon skeleton described in the plan:

- YAML config loading and validation.
- Subscription fetching for `sing-box-json` and base64 proxy-list sources.
- Basic VMess, VLESS, Shadowsocks, and Trojan link parsing.
- Candidate sing-box config generation.
- Temporary sing-box runner with isolated local SOCKS inbound.
- HTTP latency checker with HTTP and SOCKS5 proxy support.
- Selector hysteresis state machine.
- Atomic config write, backup, and systemd restart manager.
- CLI entry point with `--config`, `--once`, `--version`, and verbose logging.
- systemd unit and install script under `contrib/` and `scripts/`.

Unit tests are included for the implemented packages. They are designed to avoid
real subscription URLs, real `systemctl`, and real sing-box processes unless a
caller explicitly runs the daemon.

## License

MIT
