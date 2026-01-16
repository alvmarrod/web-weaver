package memory

import (
	"fmt"
	"sync"
	"time"

	"github.com/alvmarrod/web-weaver/internal/storage"
	"github.com/sirupsen/logrus"
)

// MemoryGraph holds graph data in memory for fast access
type MemoryGraph struct {
	nodes       map[string]*storage.Node // domain -> node
	nodesById   map[int]*storage.Node    // nodeID -> node
	edges       map[string]int           // "fromID-toID" -> weight
	nodeCounter int                      // auto-increment for node IDs
	mu          sync.RWMutex
}

// NewMemoryGraph creates a new in-memory graph
func NewMemoryGraph() *MemoryGraph {
	return &MemoryGraph{
		nodes:       make(map[string]*storage.Node),
		nodesById:   make(map[int]*storage.Node),
		edges:       make(map[string]int),
		nodeCounter: 0,
	}
}

// UpsertNode inserts or updates a node in memory
// Returns the node_id of the inserted/existing node
func (mg *MemoryGraph) UpsertNode(domain, description string) (int, error) {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// Check if node exists
	if node, exists := mg.nodes[domain]; exists {
		// Update description if provided and current is empty
		if description != "" && node.Description == "" {
			node.Description = description
		}
		return node.NodeID, nil
	}

	// Create new node
	mg.nodeCounter++
	node := &storage.Node{
		NodeID:      mg.nodeCounter,
		DomainName:  domain,
		Description: description,
		CrawlCount:  0,
		CreatedAt:   time.Now(),
	}

	mg.nodes[domain] = node
	mg.nodesById[node.NodeID] = node

	return node.NodeID, nil
}

// GetNode retrieves a node by domain name
func (mg *MemoryGraph) GetNode(domain string) (*storage.Node, error) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	if node, exists := mg.nodes[domain]; exists {
		// Return a copy to prevent external modifications
		nodeCopy := *node
		return &nodeCopy, nil
	}

	return nil, nil // Not found (matches storage behavior)
}

// IncrementCrawlCount atomically increments the crawl count for a node
func (mg *MemoryGraph) IncrementCrawlCount(nodeID int) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	node, exists := mg.nodesById[nodeID]
	if !exists {
		return fmt.Errorf("node with ID %d not found", nodeID)
	}

	node.CrawlCount++
	return nil
}

// UpsertEdge inserts a new edge or increments weight if it exists
func (mg *MemoryGraph) UpsertEdge(fromID, toID int) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// Verify nodes exist
	if _, exists := mg.nodesById[fromID]; !exists {
		return fmt.Errorf("source node %d not found", fromID)
	}
	if _, exists := mg.nodesById[toID]; !exists {
		return fmt.Errorf("target node %d not found", toID)
	}

	// Create or increment edge
	edgeKey := fmt.Sprintf("%d-%d", fromID, toID)
	mg.edges[edgeKey]++

	return nil
}

// GetStats returns current graph statistics
func (mg *MemoryGraph) GetStats() (nodeCount, edgeCount int) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	return len(mg.nodes), len(mg.edges)
}

// Flush writes all in-memory data to SQLite storage
func (mg *MemoryGraph) Flush(store *storage.Storage) error {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	startTime := time.Now()
	logrus.Info("Starting flush to database...")

	// Track statistics
	nodesWritten := 0
	edgesWritten := 0
	var firstErr error

	// Flush nodes
	for _, node := range mg.nodes {
		// Upsert node with current description
		_, err := store.UpsertNode(node.DomainName, node.Description)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logrus.Warnf("Failed to flush node %s: %v", node.DomainName, err)
			continue
		}

		// Get the node from DB to get its actual ID
		dbNode, err := store.GetNode(node.DomainName)
		if err != nil {
			logrus.Warnf("Failed to retrieve node %s after upsert: %v", node.DomainName, err)
			continue
		}

		// Update crawl count in DB (direct SQL update to match memory)
		if err := store.ResetCrawlCount(dbNode.NodeID); err != nil {
			logrus.Warnf("Failed to reset crawl count for %s: %v", node.DomainName, err)
		}

		// Set to actual crawl count
		for i := 0; i < node.CrawlCount; i++ {
			if err := store.IncrementCrawlCount(dbNode.NodeID); err != nil {
				logrus.Warnf("Failed to set crawl count for %s: %v", node.DomainName, err)
				break
			}
		}

		nodesWritten++
	}

	// Flush edges (need to map memory IDs to DB IDs)
	// Build ID mapping: memory ID -> DB ID
	idMap := make(map[int]int)
	for domain, memNode := range mg.nodes {
		dbNode, err := store.GetNode(domain)
		if err != nil || dbNode == nil {
			continue
		}
		idMap[memNode.NodeID] = dbNode.NodeID
	}

	// Write edges with mapped IDs
	for edgeKey, weight := range mg.edges {
		var memFromID, memToID int
		fmt.Sscanf(edgeKey, "%d-%d", &memFromID, &memToID)

		dbFromID, fromExists := idMap[memFromID]
		dbToID, toExists := idMap[memToID]

		if !fromExists || !toExists {
			logrus.Warnf("Skipping edge %s: node ID mapping not found", edgeKey)
			continue
		}

		// Write edge with weight times
		for i := 0; i < weight; i++ {
			if err := store.UpsertEdge(dbFromID, dbToID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				logrus.Warnf("Failed to flush edge %d->%d: %v", dbFromID, dbToID, err)
				break
			}
		}

		edgesWritten++
	}

	duration := time.Since(startTime)
	logrus.Infof("Flush complete: %d nodes, %d edges written in %v", nodesWritten, edgesWritten, duration)

	return firstErr
}

// LoadFromStorage populates in-memory graph from SQLite (for resume)
func (mg *MemoryGraph) LoadFromStorage(store *storage.Storage, maxCrawls int) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	logrus.Info("Loading resumable nodes from database into memory...")

	// Load resumable nodes
	nodes, err := store.LoadResumableNodes(maxCrawls)
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	// Populate memory graph
	for _, node := range nodes {
		// Use DB node ID directly
		mg.nodes[node.DomainName] = node
		mg.nodesById[node.NodeID] = node

		// Update counter to avoid ID conflicts
		if node.NodeID > mg.nodeCounter {
			mg.nodeCounter = node.NodeID
		}
	}

	logrus.Infof("Loaded %d nodes into memory", len(nodes))
	return nil
}

// ResetCrawlCount resets crawl count for a node (used on startup)
func (mg *MemoryGraph) ResetCrawlCount(nodeID int) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	node, exists := mg.nodesById[nodeID]
	if !exists {
		return fmt.Errorf("node with ID %d not found", nodeID)
	}

	node.CrawlCount = 0
	return nil
}
