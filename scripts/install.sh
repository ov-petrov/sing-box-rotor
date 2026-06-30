#!/bin/sh
set -eu

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sing-box-rotor ./cmd/sing-box-rotor
install -m 0755 sing-box-rotor /usr/local/bin/sing-box-rotor
install -d -m 0700 /etc/sing-box-rotor
if [ ! -f /etc/sing-box-rotor/config.yaml ]; then
  install -m 0600 contrib/config.example.yaml /etc/sing-box-rotor/config.yaml
fi
install -m 0644 contrib/systemd/sing-box-rotor.service /etc/systemd/system/sing-box-rotor.service
systemctl daemon-reload
systemctl enable sing-box-rotor.service
