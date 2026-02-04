package job

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// Metadata represents the JSON metadata for a captured print job.
type Metadata struct {
	JobID          string    `json:"job_id"`
	DeviceID       string    `json:"device_id"`
	SiteID         string    `json:"site_id"`
	PrinterIP      string    `json:"printer_ip"`
	PrinterPort    uint16    `json:"printer_port"`
	SrcIP          string    `json:"src_ip"`
	CaptureStartTS time.Time `json:"capture_start_ts"`
	CaptureEndTS   time.Time `json:"capture_end_ts"`
	ByteLen        int       `json:"byte_len"`
	SHA256         string    `json:"sha256"`
	Transport      string    `json:"transport"`
	Tags           []string  `json:"tags,omitempty"`
	ReprintOfJobID string    `json:"reprint_of_job_id,omitempty"`
}

// Job represents an in-progress or completed print job capture.
type Job struct {
	mu       sync.Mutex
	Metadata Metadata
	Data     []byte
	closed   bool
}

// New creates a new job with the given parameters.
func New(deviceID, siteID, printerIP string, printerPort uint16, srcIP, transport string) *Job {
	return &Job{
		Metadata: Metadata{
			JobID:          uuid.New().String(),
			DeviceID:       deviceID,
			SiteID:         siteID,
			PrinterIP:      printerIP,
			PrinterPort:    printerPort,
			SrcIP:          srcIP,
			CaptureStartTS: time.Now().UTC(),
			Transport:      transport,
			Tags:           []string{},
		},
		Data: make([]byte, 0, 4096),
	}
}

// Append adds data to the job. Returns false if job is already closed.
func (j *Job) Append(data []byte) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return false
	}

	j.Data = append(j.Data, data...)
	return true
}

// Close finalizes the job, computing hash and setting end timestamp.
func (j *Job) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return
	}

	j.closed = true
	j.Metadata.CaptureEndTS = time.Now().UTC()
	j.Metadata.ByteLen = len(j.Data)

	hash := sha256.Sum256(j.Data)
	j.Metadata.SHA256 = hex.EncodeToString(hash[:])
}

// IsClosed returns whether the job is closed.
func (j *Job) IsClosed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closed
}

// GetHash returns the SHA256 hash of the job data.
func (j *Job) GetHash() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Metadata.SHA256
}

// AddTag adds a tag to the job.
func (j *Job) AddTag(tag string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Metadata.Tags = append(j.Metadata.Tags, tag)
}

// SetReprintOf marks this job as a reprint of another job.
func (j *Job) SetReprintOf(jobID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Metadata.ReprintOfJobID = jobID
	for _, tag := range j.Metadata.Tags {
		if tag == "reprint" {
			return
		}
	}
	j.Metadata.Tags = append(j.Metadata.Tags, "reprint")
}

// Store represents the job storage backend.
type Store struct {
	basePath  string
	minFreeMB int
	mu        sync.Mutex
}

// NewStore creates a new job store.
func NewStore(basePath string, minFreeMB int) (*Store, error) {
	if err := os.MkdirAll(basePath, 0750); err != nil {
		return nil, fmt.Errorf("creating base path: %w", err)
	}

	return &Store{
		basePath:  basePath,
		minFreeMB: minFreeMB,
	}, nil
}

// Save writes a job to disk atomically.
func (s *Store) Save(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !job.IsClosed() {
		return fmt.Errorf("cannot save unclosed job")
	}

	// Check disk space
	if !s.hasEnoughSpace() {
		return fmt.Errorf("insufficient disk space (min %d MB required)", s.minFreeMB)
	}

	// Create date-based directory structure
	ts := job.Metadata.CaptureStartTS
	dir := filepath.Join(s.basePath, ts.Format("2006"), ts.Format("01"), ts.Format("02"))
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating job directory: %w", err)
	}

	baseName := filepath.Join(dir, job.Metadata.JobID)
	binPath := baseName + ".bin"
	jsonPath := baseName + ".json"
	tmpBinPath := binPath + ".tmp"
	tmpJSONPath := jsonPath + ".tmp"

	// Write binary data atomically
	if err := s.writeFileAtomic(tmpBinPath, binPath, job.Data); err != nil {
		return fmt.Errorf("writing binary file: %w", err)
	}

	// Write metadata JSON atomically
	metaBytes, err := json.MarshalIndent(job.Metadata, "", "  ")
	if err != nil {
		os.Remove(binPath)
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	if err := s.writeFileAtomic(tmpJSONPath, jsonPath, metaBytes); err != nil {
		os.Remove(binPath)
		return fmt.Errorf("writing metadata file: %w", err)
	}

	return nil
}

func (s *Store) writeFileAtomic(tmpPath, finalPath string, data []byte) error {
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func (s *Store) hasEnoughSpace() bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.basePath, &stat); err != nil {
		// If we can't check, assume we have space
		return true
	}

	// Calculate available space in MB
	availMB := (stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024)
	return availMB >= uint64(s.minFreeMB)
}

// GetJobPath returns the path where a job would be stored.
func (s *Store) GetJobPath(jobID string, ts time.Time) string {
	dir := filepath.Join(s.basePath, ts.Format("2006"), ts.Format("01"), ts.Format("02"))
	return filepath.Join(dir, jobID)
}
