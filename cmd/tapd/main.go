package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcenggist/kitchen-printer-tap/internal/capture"
	"github.com/marcenggist/kitchen-printer-tap/internal/config"
	"github.com/marcenggist/kitchen-printer-tap/internal/health"
	"github.com/marcenggist/kitchen-printer-tap/internal/job"
	"github.com/marcenggist/kitchen-printer-tap/internal/upload"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "/etc/kitchen-printer-tap/config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("tapd version %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting tapd",
		"version", version,
		"config", *configPath)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config",
			"error", err)
		os.Exit(1)
	}

	logger.Info("configuration loaded",
		"device_id", cfg.DeviceID,
		"site_id", cfg.SiteID,
		"interface", cfg.Interface,
		"port_9100", cfg.Capture.Port9100Enabled,
		"port_515", cfg.Capture.Port515Enabled)

	// Initialize job store
	store, err := job.NewStore(cfg.Storage.BasePath, cfg.Storage.MinFreeMB)
	if err != nil {
		logger.Error("failed to initialize store",
			"error", err)
		os.Exit(1)
	}

	// Initialize reprint detector
	reprintDetector := job.NewReprintDetector(cfg.Storage.ReprintWindowSec)

	// Initialize statistics
	stats := &capture.Stats{}

	// Initialize uploader
	uploader := upload.New(&cfg.Upload, cfg.Storage.BasePath, logger)
	uploader.Start()

	// Initialize capturer
	capturer := capture.New(cfg, store, reprintDetector, stats, logger)
	if err := capturer.Start(); err != nil {
		logger.Error("failed to start capture",
			"error", err)
		os.Exit(1)
	}

	// Initialize health server
	healthServer := health.New(
		&cfg.Health,
		stats,
		uploader.QueueSize,
		capturer.GetActiveSessions,
		logger,
	)
	if err := healthServer.Start(); err != nil {
		logger.Error("failed to start health server",
			"error", err)
	}

	// Start metrics logger if enabled
	var metricsStop chan struct{}
	if cfg.Metrics.Enabled {
		metricsStop = make(chan struct{})
		go metricsLoop(logger, stats, uploader, capturer, cfg.Metrics.Interval, metricsStop)
	}

	logger.Info("tapd running",
		"health_endpoint", fmt.Sprintf("http://%s/health", cfg.Health.Address))

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	logger.Info("shutdown signal received",
		"signal", sig.String())

	// Graceful shutdown
	if metricsStop != nil {
		close(metricsStop)
	}
	healthServer.Stop()
	capturer.Stop()
	uploader.Stop()

	logger.Info("tapd stopped",
		"jobs_captured", stats.JobsCaptured.Load(),
		"bytes_captured", stats.BytesCaptured.Load())
}

func metricsLoop(logger *slog.Logger, stats *capture.Stats, uploader *upload.Uploader, capturer *capture.Capturer, interval time.Duration, stop chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			logger.Info("metrics",
				"jobs_captured", stats.JobsCaptured.Load(),
				"bytes_captured", stats.BytesCaptured.Load(),
				"upload_queue", uploader.QueueSize(),
				"active_sessions", capturer.GetActiveSessions(),
				"parse_errors", stats.ParseErrors.Load())
		}
	}
}
