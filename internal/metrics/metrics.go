package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/alvmarrod/web-weaver/internal/storage"
)

// Tracker holds and manages crawl metrics
type Tracker struct {
	mu               sync.Mutex
	data             storage.Metrics
	totalFetchTimeMs int64
	fetchCount       int
}

// NewTracker creates a new metrics tracker
func NewTracker() *Tracker {
	return &Tracker{
		data: storage.Metrics{
			StartTime: time.Now(),
		},
	}
}

// IncrementNodesDiscovered increments the discovered nodes counter
func (t *Tracker) IncrementNodesDiscovered() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.NodesDiscovered++
}

// IncrementNodesCrawled increments the crawled nodes counter
func (t *Tracker) IncrementNodesCrawled() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.NodesCrawled++
}

// IncrementEdgesRecorded increments the edges counter
func (t *Tracker) IncrementEdgesRecorded() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.EdgesRecorded++
}

// IncrementPagesFetched increments the successful fetch counter
func (t *Tracker) IncrementPagesFetched() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.PagesFetched++
}

// IncrementPagesFailed increments the failed fetch counter
func (t *Tracker) IncrementPagesFailed() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.PagesFailed++
}

// RecordFetchTime records a page fetch duration
func (t *Tracker) RecordFetchTime(duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalFetchTimeMs += duration.Milliseconds()
	t.fetchCount++
}

// GetSnapshot returns a copy of current metrics
func (t *Tracker) GetSnapshot() storage.Metrics {
	t.mu.Lock()
	defer t.mu.Unlock()

	snapshot := t.data
	snapshot.TotalFetchTimeMs = t.totalFetchTimeMs

	// Calculate average fetch time
	if t.fetchCount > 0 {
		snapshot.AvgFetchTimeMs = t.totalFetchTimeMs / int64(t.fetchCount)
	}

	return snapshot
}

// WriteToFile exports metrics to a JSON file
func (t *Tracker) WriteToFile(path, reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Finalize metrics
	t.data.EndTime = time.Now()
	t.data.TerminationReason = reason
	t.data.TotalFetchTimeMs = t.totalFetchTimeMs

	// Calculate average
	if t.fetchCount > 0 {
		t.data.AvgFetchTimeMs = t.totalFetchTimeMs / int64(t.fetchCount)
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write metrics file: %w", err)
	}

	return nil
}

// LogProgress prints current metrics to console (for periodic updates)
func (t *Tracker) LogProgress() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return fmt.Sprintf("Nodes: %d discovered, %d crawled | Edges: %d | Pages: %d fetched, %d failed",
		t.data.NodesDiscovered,
		t.data.NodesCrawled,
		t.data.EdgesRecorded,
		t.data.PagesFetched,
		t.data.PagesFailed,
	)
}
