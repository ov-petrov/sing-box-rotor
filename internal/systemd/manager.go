package systemd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/config"
	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

type Manager struct {
	Config config.SingBoxConfig
	Runner CommandRunner
}

func New(cfg config.SingBoxConfig, r CommandRunner) *Manager {
	if r == nil {
		r = ExecRunner{}
	}
	return &Manager{Config: cfg, Runner: r}
}

func (m *Manager) Apply(ctx context.Context, candidate subscription.CandidateConfig) error {
	var parsed any
	if err := json.Unmarshal(candidate.Raw, &parsed); err != nil {
		return fmt.Errorf("candidate JSON invalid: %w", err)
	}
	raw, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return err
	}
	if err := backupIfExists(m.Config.ConfigPath); err != nil {
		return err
	}
	mode, err := privateModeFor(m.Config.ConfigPath)
	if err != nil {
		return err
	}
	if err := atomicWrite(m.Config.ConfigPath, raw, mode); err != nil {
		return err
	}
	if err := m.Runner.Run(ctx, "systemctl", "restart", m.Config.Service); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := m.Runner.Run(ctx, "systemctl", "is-active", "--quiet", m.Config.Service)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %s did not become active: %w", m.Config.Service, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func backupIfExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	input, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".bak", input, privateFileMode(info.Mode().Perm()))
}

func privateModeFor(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0o600, nil
		}
		return 0, err
	}
	return privateFileMode(info.Mode().Perm()), nil
}

func privateFileMode(mode os.FileMode) os.FileMode {
	if mode == 0 || mode&0o077 != 0 {
		return 0o600
	}
	return mode
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sing-box-rotor-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
