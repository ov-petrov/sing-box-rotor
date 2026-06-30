package systemd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ov-petrov/sing-box-rotor/internal/config"
	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return nil
}

func TestApplyWritesBackupAndRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rr := &recordingRunner{}
	m := New(config.SingBoxConfig{ConfigPath: path, Service: "sing-box"}, rr)
	err := m.Apply(context.Background(), subscription.CandidateConfig{Raw: []byte(`{"outbounds":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "outbounds") {
		t.Fatalf("config not written: %s", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	backupInfo, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if got := backupInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("backup mode = %o, want 600", got)
	}
	if len(rr.calls) != 2 || !strings.Contains(rr.calls[0], "restart sing-box") {
		t.Fatalf("bad calls: %v", rr.calls)
	}
}

func TestPrivateFileModePreservesAlreadyPrivateMode(t *testing.T) {
	if got := privateFileMode(0o640); got != 0o600 {
		t.Fatalf("group-readable mode should be tightened to 600, got %o", got)
	}
	if got := privateFileMode(0o400); got != 0o400 {
		t.Fatalf("owner-only mode should be preserved, got %o", got)
	}
}
