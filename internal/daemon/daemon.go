package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ov-petrov/sing-box-rotor/internal/checker"
	"github.com/ov-petrov/sing-box-rotor/internal/config"
	"github.com/ov-petrov/sing-box-rotor/internal/runner"
	"github.com/ov-petrov/sing-box-rotor/internal/selector"
	"github.com/ov-petrov/sing-box-rotor/internal/subscription"
	"github.com/ov-petrov/sing-box-rotor/internal/systemd"
)

type Fetcher interface {
	Fetch(context.Context, []config.Subscription) ([]subscription.CandidateConfig, error)
}

type Applier interface {
	Apply(context.Context, subscription.CandidateConfig) error
}

type CandidateEvaluator interface {
	Evaluate(context.Context, []subscription.CandidateConfig) ([]checker.Result, error)
}

type Rotor struct {
	Config    *config.Config
	Fetcher   Fetcher
	Applier   Applier
	Evaluator CandidateEvaluator
	Log       *slog.Logger
}

func New(cfg *config.Config, f Fetcher, a Applier, log *slog.Logger) *Rotor {
	if log == nil {
		log = slog.Default()
	}
	if f == nil {
		f = subscription.NewFetcher(nil, 30*time.Second, log, nil, nil)
	}
	if a == nil {
		a = systemd.New(cfg.SingBox, nil)
	}
	return &Rotor{Config: cfg, Fetcher: f, Applier: a, Evaluator: liveEvaluator{cfg: cfg}, Log: log}
}

func (r *Rotor) RunOnce(ctx context.Context) error {
	candidates, err := r.Fetcher.Fetch(ctx, r.Config.Subscriptions)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no candidates produced")
	}
	byName := make(map[string]subscription.CandidateConfig, len(candidates))
	for _, candidate := range candidates {
		byName[candidate.Name] = candidate
	}
	results, err := r.Evaluator.Evaluate(ctx, candidates)
	if err != nil {
		return err
	}
	sel := selector.New("", 1, 0, time.Now())
	decision := sel.Choose(results, time.Now())
	if decision.Kind != selector.SwitchCandidate {
		return fmt.Errorf("no healthy candidates")
	}
	return r.Applier.Apply(ctx, byName[decision.Target])
}

type liveEvaluator struct {
	cfg *config.Config
}

func (e liveEvaluator) Evaluate(ctx context.Context, candidates []subscription.CandidateConfig) ([]checker.Result, error) {
	results := make([]checker.Result, 0, len(candidates))
	for _, candidate := range candidates {
		handle, err := runner.Start(ctx, e.cfg.SingBox.Binary, candidate)
		if err != nil {
			results = append(results, checker.Result{CandidateName: candidate.Name, Error: err})
			continue
		}
		chk, err := checker.New(handle.ProxyURL, e.cfg.TestTimeout, e.cfg.RequestMethod)
		if err != nil {
			_ = handle.Close()
			results = append(results, checker.Result{CandidateName: candidate.Name, Error: err})
			continue
		}
		latency, err := chk.Probe(ctx, e.cfg.TestURL)
		_ = handle.Close()
		results = append(results, checker.Result{CandidateName: candidate.Name, Latency: latency, Error: err})
	}
	return results, nil
}

func (r *Rotor) Run(ctx context.Context) error {
	if err := r.RunOnce(ctx); err != nil {
		return err
	}
	check := time.NewTicker(r.Config.CheckInterval)
	recheck := time.NewTicker(r.Config.RecheckInterval)
	defer check.Stop()
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-check.C:
			r.Log.Debug("periodic current check tick")
		case <-recheck.C:
			if err := r.RunOnce(ctx); err != nil {
				r.Log.Warn("recheck failed", "error", err)
			}
		}
	}
}
