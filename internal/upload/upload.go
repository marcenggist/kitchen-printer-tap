package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcenggist/kitchen-printer-tap/internal/config"
	"github.com/marcenggist/kitchen-printer-tap/internal/job"
)

// UploadStatus represents the upload status for a job.
type UploadStatus struct {
	JobID       string    `json:"job_id"`
	Status      string    `json:"status"` // pending, uploaded, failed
	Attempts    int       `json:"attempts"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at,omitempty"`
}

// Uploader handles uploading jobs to the webhook.
type Uploader struct {
	cfg        *config.UploadConfig
	basePath   string
	logger     *slog.Logger
	client     *http.Client
	queue      chan string
	queueSize  atomic.Int64
	done       chan struct{}
	wg         sync.WaitGroup
}

// New creates a new uploader.
func New(cfg *config.UploadConfig, basePath string, logger *slog.Logger) *Uploader {
	return &Uploader{
		cfg:      cfg,
		basePath: basePath,
		logger:   logger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		queue: make(chan string, 1000),
		done:  make(chan struct{}),
	}
}

// Start begins the upload worker.
func (u *Uploader) Start() {
	if !u.cfg.Enabled {
		u.logger.Info("upload disabled")
		return
	}

	u.wg.Add(1)
	go u.worker()

	// Scan for pending uploads
	u.wg.Add(1)
	go u.scanPending()

	u.logger.Info("upload worker started",
		"webhook_url", u.cfg.WebhookURL)
}

// Stop halts the upload worker.
func (u *Uploader) Stop() {
	close(u.done)
	u.wg.Wait()
}

// Enqueue adds a job to the upload queue.
func (u *Uploader) Enqueue(jobPath string) {
	if !u.cfg.Enabled {
		return
	}

	select {
	case u.queue <- jobPath:
		u.queueSize.Add(1)
	default:
		u.logger.Warn("upload queue full, dropping job",
			"path", jobPath)
	}
}

// QueueSize returns the current queue size.
func (u *Uploader) QueueSize() int64 {
	return u.queueSize.Load()
}

func (u *Uploader) worker() {
	defer u.wg.Done()

	for {
		select {
		case <-u.done:
			return
		case jobPath := <-u.queue:
			u.queueSize.Add(-1)
			u.processJob(jobPath)
		}
	}
}

func (u *Uploader) processJob(basePath string) {
	binPath := basePath + ".bin"
	jsonPath := basePath + ".json"
	statusPath := basePath + ".upload.json"

	// Load existing status or create new
	status := u.loadOrCreateStatus(statusPath, filepath.Base(basePath))

	if status.Status == "uploaded" {
		return
	}

	// Read metadata
	metaBytes, err := os.ReadFile(jsonPath)
	if err != nil {
		u.logger.Error("failed to read metadata",
			"path", jsonPath,
			"error", err)
		return
	}

	var meta job.Metadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		u.logger.Error("failed to parse metadata",
			"path", jsonPath,
			"error", err)
		return
	}

	// Read binary data
	binData, err := os.ReadFile(binPath)
	if err != nil {
		u.logger.Error("failed to read binary",
			"path", binPath,
			"error", err)
		return
	}

	// Attempt upload with retries
	var lastErr error
	for attempt := 1; attempt <= u.cfg.MaxRetries; attempt++ {
		status.Attempts = attempt
		status.LastAttempt = time.Now().UTC()

		err := u.upload(meta, binData)
		if err == nil {
			status.Status = "uploaded"
			status.UploadedAt = time.Now().UTC()
			u.saveStatus(statusPath, status)
			u.logger.Info("job uploaded",
				"job_id", meta.JobID,
				"attempts", attempt)
			return
		}

		lastErr = err
		status.LastError = err.Error()
		u.saveStatus(statusPath, status)

		if attempt < u.cfg.MaxRetries {
			backoff := time.Duration(attempt) * u.cfg.RetryBackoff
			select {
			case <-u.done:
				return
			case <-time.After(backoff):
			}
		}
	}

	status.Status = "failed"
	u.saveStatus(statusPath, status)
	u.logger.Error("job upload failed",
		"job_id", meta.JobID,
		"attempts", status.Attempts,
		"error", lastErr)
}

func (u *Uploader) upload(meta job.Metadata, binData []byte) error {
	// Build multipart request
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add metadata JSON
	metaBytes, _ := json.Marshal(meta)
	metaPart, err := writer.CreateFormField("metadata")
	if err != nil {
		return fmt.Errorf("creating metadata field: %w", err)
	}
	metaPart.Write(metaBytes)

	// Add binary file
	binPart, err := writer.CreateFormFile("payload", meta.JobID+".bin")
	if err != nil {
		return fmt.Errorf("creating payload field: %w", err)
	}
	binPart.Write(binData)

	writer.Close()

	// Create request
	ctx, cancel := context.WithTimeout(context.Background(), u.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", u.cfg.WebhookURL, &buf)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if u.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+u.cfg.AuthToken)
	}

	// Send request
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("upload failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (u *Uploader) loadOrCreateStatus(path, jobID string) *UploadStatus {
	data, err := os.ReadFile(path)
	if err == nil {
		var status UploadStatus
		if json.Unmarshal(data, &status) == nil {
			return &status
		}
	}

	return &UploadStatus{
		JobID:  jobID,
		Status: "pending",
	}
}

func (u *Uploader) saveStatus(path string, status *UploadStatus) {
	data, _ := json.MarshalIndent(status, "", "  ")
	os.WriteFile(path, data, 0640)
}

func (u *Uploader) scanPending() {
	defer u.wg.Done()

	// Scan for jobs that need uploading
	err := filepath.Walk(u.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-u.done:
			return filepath.SkipAll
		default:
		}

		if !info.IsDir() && strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".upload.json") {
			// Check if already uploaded
			basePath := strings.TrimSuffix(path, ".json")
			statusPath := basePath + ".upload.json"

			status := u.loadOrCreateStatus(statusPath, filepath.Base(basePath))
			if status.Status != "uploaded" {
				u.Enqueue(basePath)
			}
		}

		return nil
	})

	if err != nil {
		u.logger.Error("scan pending failed",
			"error", err)
	}
}
