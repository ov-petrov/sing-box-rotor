package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/checker"
	"github.com/ov-petrov/sing-box-rotor/internal/config"
	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

type fakeFetcher struct {
	candidates []subscription.CandidateConfig
	err        error
	calls      int
}

func (f *fakeFetcher) Fetch(context.Context, []config.Subscription) ([]subscription.CandidateConfig, error) {
	f.calls++
	return f.candidates, f.err
}

type fakeApplier struct {
	calls   int
	applied string
}

func (a *fakeApplier) Apply(_ context.Context, c subscription.CandidateConfig) error {
	a.calls++
	a.applied = c.Name
	return nil
}

type fakeEvaluator struct {
	results []checker.Result
	err     error
	calls   int
}

func (e *fakeEvaluator) Evaluate(context.Context, []subscription.CandidateConfig) ([]checker.Result, error) {
	e.calls++
	return e.results, e.err
}

type fakeProbe struct {
	err error
}

func (p fakeProbe) Probe(context.Context) error {
	return p.err
}

func TestRunOnceNoCandidates(t *testing.T) {
	cfg := &config.Config{}
	r := New(cfg, &fakeFetcher{}, &fakeApplier{}, nil)
	if err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("expected no candidates error")
	}
}

func TestRunOnceAppliesBestCandidate(t *testing.T) {
	cfg := &config.Config{}
	applier := &fakeApplier{}
	evaluator := &fakeEvaluator{results: []checker.Result{
		{CandidateName: "slow", Latency: 50 * time.Millisecond},
		{CandidateName: "fast", Latency: 10 * time.Millisecond},
	}}
	r := New(cfg, &fakeFetcher{candidates: []subscription.CandidateConfig{
		{Name: "slow", Raw: []byte(`{"outbounds":[]}`)},
		{Name: "fast", Raw: []byte(`{"outbounds":[]}`)},
	}}, applier, nil)
	r.Evaluator = evaluator
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applier.calls != 1 || applier.applied != "fast" {
		t.Fatalf("applied calls=%d name=%q", applier.calls, applier.applied)
	}
}

func TestRunOnceKeepsSelectorStateBetweenRechecks(t *testing.T) {
	cfg := &config.Config{SwitchCooldown: time.Minute}
	applier := &fakeApplier{}
	fetcher := &fakeFetcher{candidates: []subscription.CandidateConfig{{Name: "fast", Raw: []byte(`{"outbounds":[]}`)}}}
	evaluator := &fakeEvaluator{results: []checker.Result{{CandidateName: "fast", Latency: time.Millisecond}}}
	r := New(cfg, fetcher, applier, nil)
	r.Evaluator = evaluator

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applier.calls != 1 {
		t.Fatalf("same current candidate should only be applied once, got %d applies", applier.calls)
	}
}

func TestCheckCurrentEvaluatesAfterFailureThreshold(t *testing.T) {
	cfg := &config.Config{FailThreshold: 2}
	applier := &fakeApplier{}
	fetcher := &fakeFetcher{candidates: []subscription.CandidateConfig{{Name: "recovered", Raw: []byte(`{"outbounds":[]}`)}}}
	evaluator := &fakeEvaluator{results: []checker.Result{{CandidateName: "recovered", Latency: time.Millisecond}}}
	r := New(cfg, fetcher, applier, nil)
	r.Evaluator = evaluator
	r.Probe = fakeProbe{err: errors.New("down")}

	if err := r.checkCurrent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if evaluator.calls != 0 {
		t.Fatalf("first failure should not evaluate, got %d calls", evaluator.calls)
	}
	if err := r.checkCurrent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if evaluator.calls != 1 || applier.calls != 1 {
		t.Fatalf("threshold should evaluate and apply, evaluator=%d applier=%d", evaluator.calls, applier.calls)
	}
}
