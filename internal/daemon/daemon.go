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

type CurrentProbe interface {
	Probe(context.Context) error
}

type Rotor struct {
	Config    *config.Config
	Fetcher   Fetcher
	Applier   Applier
	Evaluator CandidateEvaluator
	Probe     CurrentProbe
	Selector  *selector.Selector
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
	return &Rotor{
		Config:    cfg,
		Fetcher:   f,
		Applier:   a,
		Evaluator: liveEvaluator{cfg: cfg},
		Probe:     liveCurrentProbe{cfg: cfg},
		Selector:  selector.New("", cfg.FailThreshold, cfg.SwitchCooldown, time.Now()),
		Log:       log,
	}
}

func (r *Rotor) RunOnce(ctx context.Context) error {
	return r.evaluateAndApply(ctx)
}

func (r *Rotor) evaluateAndApply(ctx context.Context) error {
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
	now := time.Now()
	decision := r.Selector.Choose(results, now)
	if decision.Kind != selector.SwitchCandidate {
		if decision.Reason == "no healthy candidates" {
			return fmt.Errorf("no healthy candidates")
		}
		r.Log.Debug("keeping current candidate", "reason", decision.Reason, "target", decision.Target)
		return nil
	}
	candidate, ok := byName[decision.Target]
	if !ok {
		return fmt.Errorf("selected candidate %q was not found", decision.Target)
	}
	if err := r.Applier.Apply(ctx, candidate); err != nil {
		return err
	}
	r.Selector.MarkSwitched(decision.Target, now)
	return nil
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

type liveCurrentProbe struct {
	cfg *config.Config
}

func (p liveCurrentProbe) Probe(ctx context.Context) error {
	chk, err := checker.New("socks5://"+p.cfg.SingBox.Inbound, p.cfg.TestTimeout, p.cfg.RequestMethod)
	if err != nil {
		return err
	}
	_, err = chk.Probe(ctx, p.cfg.TestURL)
	return err
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
			if err := r.checkCurrent(ctx); err != nil {
				r.Log.Warn("current check handling failed", "error", err)
			}
		case <-recheck.C:
			if err := r.evaluateAndApply(ctx); err != nil {
				r.Log.Warn("recheck failed", "error", err)
			}
		}
	}
}

func (r *Rotor) checkCurrent(ctx context.Context) error {
	err := r.Probe.Probe(ctx)
	decision := r.Selector.CurrentCheck(err == nil)
	if err != nil {
		r.Log.Warn("current config probe failed", "error", err, "decision", decision.Reason)
	}
	if decision.Kind != selector.EvaluateCandidates {
		return nil
	}
	return r.evaluateAndApply(ctx)
}
