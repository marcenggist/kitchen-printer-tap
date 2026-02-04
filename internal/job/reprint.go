package job

import (
	"sync"
	"time"
)

// ReprintDetector tracks recent job hashes to detect reprints.
type ReprintDetector struct {
	mu       sync.Mutex
	window   time.Duration
	hashes   map[string][]hashEntry
	cleanTTL time.Duration
}

type hashEntry struct {
	jobID     string
	printerIP string
	timestamp time.Time
}

// NewReprintDetector creates a new reprint detector with the given window.
func NewReprintDetector(windowSeconds int) *ReprintDetector {
	rd := &ReprintDetector{
		window:   time.Duration(windowSeconds) * time.Second,
		hashes:   make(map[string][]hashEntry),
		cleanTTL: time.Duration(windowSeconds*2) * time.Second,
	}
	go rd.cleanupLoop()
	return rd
}

// Check looks for a previous job with the same hash and printer IP.
// Returns the job ID of the original if this is a reprint, empty string otherwise.
func (rd *ReprintDetector) Check(hash, printerIP string) string {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	entries, ok := rd.hashes[hash]
	if !ok {
		return ""
	}

	now := time.Now()
	for _, e := range entries {
		if e.printerIP == printerIP && now.Sub(e.timestamp) <= rd.window {
			return e.jobID
		}
	}

	return ""
}

// Record stores a job hash for reprint detection.
func (rd *ReprintDetector) Record(hash, printerIP, jobID string) {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	entry := hashEntry{
		jobID:     jobID,
		printerIP: printerIP,
		timestamp: time.Now(),
	}

	rd.hashes[hash] = append(rd.hashes[hash], entry)
}

func (rd *ReprintDetector) cleanupLoop() {
	ticker := time.NewTicker(rd.cleanTTL)
	defer ticker.Stop()

	for range ticker.C {
		rd.cleanup()
	}
}

func (rd *ReprintDetector) cleanup() {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	now := time.Now()
	for hash, entries := range rd.hashes {
		var valid []hashEntry
		for _, e := range entries {
			if now.Sub(e.timestamp) <= rd.cleanTTL {
				valid = append(valid, e)
			}
		}
		if len(valid) == 0 {
			delete(rd.hashes, hash)
		} else {
			rd.hashes[hash] = valid
		}
	}
}
