package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/checker"
	"github.com/ov-petrov/sing-box-rotor/internal/config"
	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
)

type fakeFetcher struct {
	candidates []subscription.CandidateConfig
	err        error
}

func (f fakeFetcher) Fetch(context.Context, []config.Subscription) ([]subscription.CandidateConfig, error) {
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
}

func (e fakeEvaluator) Evaluate(context.Context, []subscription.CandidateConfig) ([]checker.Result, error) {
	return e.results, e.err
}

func TestRunOnceNoCandidates(t *testing.T) {
	cfg := &config.Config{}
	r := New(cfg, fakeFetcher{}, &fakeApplier{}, nil)
	if err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("expected no candidates error")
	}
}

func TestRunOnceAppliesBestCandidate(t *testing.T) {
	cfg := &config.Config{}
	applier := &fakeApplier{}
	r := New(cfg, fakeFetcher{candidates: []subscription.CandidateConfig{
		{Name: "slow", Raw: []byte(`{"outbounds":[]}`)},
		{Name: "fast", Raw: []byte(`{"outbounds":[]}`)},
	}}, applier, nil)
	r.Evaluator = fakeEvaluator{results: []checker.Result{
		{CandidateName: "slow", Latency: 50 * time.Millisecond},
		{CandidateName: "fast", Latency: 10 * time.Millisecond},
	}}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applier.calls != 1 || applier.applied != "fast" {
		t.Fatalf("applied calls=%d name=%q", applier.calls, applier.applied)
	}
}
