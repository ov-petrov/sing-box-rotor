package selector

import (
	"errors"
	"testing"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/checker"
)

func TestCurrentCheckThreshold(t *testing.T) {
	s := New("current", 2, time.Minute, time.Unix(0, 0))
	if d := s.CurrentCheck(false); d.Kind != KeepCurrent {
		t.Fatalf("first failure should keep current: %+v", d)
	}
	if d := s.CurrentCheck(false); d.Kind != EvaluateCandidates {
		t.Fatalf("second failure should evaluate: %+v", d)
	}
	if d := s.CurrentCheck(true); d.Kind != KeepCurrent {
		t.Fatalf("success should keep current: %+v", d)
	}
}

func TestChooseBestAndCooldown(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New("a", 1, time.Minute, now)
	results := []checker.Result{
		{CandidateName: "bad", Error: errors.New("down")},
		{CandidateName: "b", Latency: 20 * time.Millisecond},
		{CandidateName: "c", Latency: 10 * time.Millisecond},
	}
	d := s.Choose(results, now)
	if d.Kind != SwitchCandidate || d.Target != "c" {
		t.Fatalf("bad decision: %+v", d)
	}
	s.MarkSwitched("c", now)
	if d := s.Choose(results, now.Add(time.Second)); d.Kind != KeepCurrent {
		t.Fatalf("cooldown/current should keep: %+v", d)
	}
}
