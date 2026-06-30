package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ov-petrov/sing-box-rotor/internal/config"
	"github.com/ov-petrov/sing-box-rotor/internal/daemon"
)

const version = "0.1.0"

func main() {
	var configPath string
	var once bool
	var verbose bool
	var showVersion bool
	flag.StringVar(&configPath, "config", envOr("SINGBOX_ROTOR_CONFIG", "/etc/sing-box-rotor/config.yaml"), "Path to config file")
	flag.BoolVar(&once, "once", false, "Run one evaluation cycle and exit")
	flag.BoolVar(&verbose, "v", false, "Enable debug logging")
	flag.BoolVar(&verbose, "verbose", false, "Enable debug logging")
	flag.BoolVar(&showVersion, "version", false, "Print version")
	flag.Parse()
	if showVersion {
		fmt.Println(version)
		return
	}
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := daemon.New(cfg, nil, nil, log)
	if once {
		err = r.RunOnce(ctx)
	} else {
		err = r.Run(ctx)
	}
	if err != nil && ctx.Err() == nil {
		log.Error("rotor failed", "error", err)
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
