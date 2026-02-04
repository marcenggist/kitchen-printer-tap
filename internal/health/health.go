package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/marcenggist/kitchen-printer-tap/internal/capture"
	"github.com/marcenggist/kitchen-printer-tap/internal/config"
)

// Status represents the health status response.
type Status struct {
	Status         string    `json:"status"`
	Timestamp      time.Time `json:"timestamp"`
	Uptime         string    `json:"uptime"`
	JobsCaptured   int64     `json:"jobs_captured"`
	BytesCaptured  int64     `json:"bytes_captured"`
	ActiveSessions int       `json:"active_sessions"`
	UploadQueue    int64     `json:"upload_queue"`
	ParseErrors    int64     `json:"parse_errors"`
}

// Server provides the health endpoint.
type Server struct {
	cfg         *config.HealthConfig
	startTime   time.Time
	stats       *capture.Stats
	getQueue    func() int64
	getSessions func() int
	logger      *slog.Logger
	server      *http.Server
}

// New creates a new health server.
func New(cfg *config.HealthConfig, stats *capture.Stats, getQueue func() int64, getSessions func() int, logger *slog.Logger) *Server {
	return &Server{
		cfg:         cfg,
		startTime:   time.Now(),
		stats:       stats,
		getQueue:    getQueue,
		getSessions: getSessions,
		logger:      logger,
	}
}

// Start begins the health server.
func (s *Server) Start() error {
	if !s.cfg.Enabled {
		s.logger.Info("health endpoint disabled")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Addr:         s.cfg.Address,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		s.logger.Info("health server started",
			"address", s.cfg.Address)
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("health server error",
				"error", err)
		}
	}()

	return nil
}

// Stop halts the health server.
func (s *Server) Stop() {
	if s.server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := Status{
		Status:         "ok",
		Timestamp:      time.Now().UTC(),
		Uptime:         time.Since(s.startTime).Round(time.Second).String(),
		JobsCaptured:   s.stats.JobsCaptured.Load(),
		BytesCaptured:  s.stats.BytesCaptured.Load(),
		ActiveSessions: s.getSessions(),
		UploadQueue:    s.getQueue(),
		ParseErrors:    s.stats.ParseErrors.Load(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// GetStatus returns the current health status.
func (s *Server) GetStatus() Status {
	return Status{
		Status:         "ok",
		Timestamp:      time.Now().UTC(),
		Uptime:         time.Since(s.startTime).Round(time.Second).String(),
		JobsCaptured:   s.stats.JobsCaptured.Load(),
		BytesCaptured:  s.stats.BytesCaptured.Load(),
		ActiveSessions: s.getSessions(),
		UploadQueue:    s.getQueue(),
		ParseErrors:    s.stats.ParseErrors.Load(),
	}
}
