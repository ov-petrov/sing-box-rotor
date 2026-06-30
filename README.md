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
- Implementation plan (coming soon)

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

See `config.example.yaml` (to be added).

## License

MIT
