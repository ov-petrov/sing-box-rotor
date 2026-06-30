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
	if len(rr.calls) != 2 || !strings.Contains(rr.calls[0], "restart sing-box") {
		t.Fatalf("bad calls: %v", rr.calls)
	}
}
