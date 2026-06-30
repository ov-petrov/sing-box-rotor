package selector

import (
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/checker"
)

type DecisionKind int

const (
	KeepCurrent DecisionKind = iota
	EvaluateCandidates
	SwitchCandidate
)

type Decision struct {
	Kind   DecisionKind
	Target string
	Reason string
}

type Selector struct {
	currentConfig  string
	currentFails   int
	lastSwitchTime time.Time
	failThreshold  int
	switchCooldown time.Duration
}

func New(current string, failThreshold int, switchCooldown time.Duration, now time.Time) *Selector {
	if failThreshold < 1 {
		failThreshold = 1
	}
	return &Selector{currentConfig: current, failThreshold: failThreshold, switchCooldown: switchCooldown, lastSwitchTime: now.Add(-switchCooldown)}
}

func (s *Selector) CurrentCheck(success bool) Decision {
	if success {
		s.currentFails = 0
		return Decision{Kind: KeepCurrent, Reason: "current config healthy"}
	}
	s.currentFails++
	if s.currentFails < s.failThreshold {
		return Decision{Kind: KeepCurrent, Reason: "failure threshold not reached"}
	}
	return Decision{Kind: EvaluateCandidates, Reason: "failure threshold reached"}
}

func (s *Selector) Choose(results []checker.Result, now time.Time) Decision {
	var best *checker.Result
	for i := range results {
		r := &results[i]
		if r.Error != nil {
			continue
		}
		if best == nil || r.Latency < best.Latency {
			best = r
		}
	}
	if best == nil {
		return Decision{Kind: KeepCurrent, Reason: "no healthy candidates"}
	}
	if best.CandidateName == s.currentConfig {
		s.currentFails = 0
		return Decision{Kind: KeepCurrent, Target: best.CandidateName, Reason: "best is current"}
	}
	if now.Sub(s.lastSwitchTime) < s.switchCooldown {
		return Decision{Kind: KeepCurrent, Target: best.CandidateName, Reason: "switch cooldown active"}
	}
	return Decision{Kind: SwitchCandidate, Target: best.CandidateName, Reason: "better candidate selected"}
}

func (s *Selector) MarkSwitched(name string, now time.Time) {
	s.currentConfig = name
	s.currentFails = 0
	s.lastSwitchTime = now
}
