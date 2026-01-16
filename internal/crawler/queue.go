package crawler

import (
	"fmt"
	"sync"

	"github.com/alvmarrod/web-weaver/internal/storage"
)

// Queue implements a thread-safe BFS queue with deduplication
type Queue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []storage.QueueEntry
	visited map[string]bool // key: domain_depth
	stopped bool
}

// NewQueue creates a new BFS queue
func NewQueue() *Queue {
	q := &Queue{
		items:   make([]storage.QueueEntry, 0),
		visited: make(map[string]bool),
		stopped: false,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push adds an entry to the queue if not already visited at this depth
// Returns true if added, false if duplicate
func (q *Queue) Push(entry storage.QueueEntry) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Don't accept new entries if stopped
	if q.stopped {
		return false
	}

	// Create deduplication key: domain@depth
	key := makeKey(entry.DomainName, entry.Depth)

	// Check if already visited
	if q.visited[key] {
		return false
	}

	// Mark as visited and enqueue
	q.visited[key] = true
	q.items = append(q.items, entry)

	// Signal waiting workers
	q.cond.Signal()

	return true
}

// Pop removes and returns the first entry from the queue
// Blocks if queue is empty and not stopped
// Returns (entry, true) if successful, (empty, false) if stopped and empty
func (q *Queue) Pop() (storage.QueueEntry, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for {
		// If we have items, return the first one
		if len(q.items) > 0 {
			entry := q.items[0]
			q.items = q.items[1:]
			return entry, true
		}

		// Queue is empty - check if stopped
		if q.stopped {
			return storage.QueueEntry{}, false
		}

		// Queue is empty but not stopped - wait for new items
		q.cond.Wait()
	}
}

// IsEmpty returns true if the queue has no items
func (q *Queue) IsEmpty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) == 0
}

// Size returns the current number of items in the queue
func (q *Queue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Stop signals the queue to stop accepting new entries
// Workers blocked on Pop() will drain remaining items, then receive false
func (q *Queue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.stopped = true
	// Broadcast to wake all waiting workers
	q.cond.Broadcast()
}

// GetAllEntries returns a snapshot of all current queue entries
// Used for persisting queue state on checkpoint/shutdown
func (q *Queue) GetAllEntries() []storage.QueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Return a copy of the current items
	entries := make([]storage.QueueEntry, len(q.items))
	copy(entries, q.items)
	return entries
}

// makeKey creates a deduplication key from domain and depth
func makeKey(domain string, depth int) string {
	return fmt.Sprintf("%s@%d", domain, depth)
}
