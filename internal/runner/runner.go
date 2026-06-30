package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

type RunnerHandle struct {
	ProxyURL string
	Close    func() error
}

func Start(ctx context.Context, binary string, cfg subscription.CandidateConfig) (*RunnerHandle, error) {
	if binary == "" {
		return nil, errors.New("sing-box binary is required")
	}
	mutated, port, err := MutateForTest(cfg)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "sing-box-rotor-*.json")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(mutated); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, "run", "-c", tmpPath)
	if err := cmd.Start(); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := waitTCP(ctx, addr, 5*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		os.Remove(tmpPath)
		return nil, err
	}
	closed := false
	return &RunnerHandle{
		ProxyURL: "socks5://" + addr,
		Close: func() error {
			if closed {
				return nil
			}
			closed = true
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
			return os.Remove(tmpPath)
		},
	}, nil
}

func MutateForTest(cfg subscription.CandidateConfig) ([]byte, int, error) {
	port, err := freePort()
	if err != nil {
		return nil, 0, err
	}
	raw, err := mutateForTestPort(cfg, port)
	return raw, port, err
}

func mutateForTestPort(cfg subscription.CandidateConfig, port int) ([]byte, error) {
	parsed := cfg.Parsed
	if parsed == nil {
		if err := json.Unmarshal(cfg.Raw, &parsed); err != nil {
			return nil, err
		}
	}
	delete(parsed, "experimental")
	delete(parsed, "api")
	parsed["inbounds"] = []any{map[string]any{
		"type":        "socks",
		"tag":         "rotor-test-in",
		"listen":      "127.0.0.1",
		"listen_port": port,
	}}
	return json.MarshalIndent(parsed, "", "  ")
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitTCP(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("runner did not become ready at %s: %w", addr, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
