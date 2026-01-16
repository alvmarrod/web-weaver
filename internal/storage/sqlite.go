package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Storage handles all database operations
type Storage struct {
	db *sql.DB
}

// NewStorage creates a new Storage instance, opening/creating the DB and initializing schema
func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage := &Storage{db: db}

	// Initialize schema
	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// initSchema creates tables and indices if they don't exist
func (s *Storage) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS nodes (
		node_id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain_name TEXT UNIQUE NOT NULL,
		description TEXT,
		crawl_count INTEGER DEFAULT 0,
		last_depth INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS edges (
		edge_id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_node_id INTEGER NOT NULL,
		to_node_id INTEGER NOT NULL,
		weight INTEGER DEFAULT 1,
		FOREIGN KEY (from_node_id) REFERENCES nodes(node_id),
		FOREIGN KEY (to_node_id) REFERENCES nodes(node_id),
		UNIQUE(from_node_id, to_node_id)
	);

	CREATE TABLE IF NOT EXISTS queue_state (
		entry_id INTEGER PRIMARY KEY AUTOINCREMENT,
		node_id INTEGER NOT NULL,
		domain_name TEXT NOT NULL,
		depth INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_nodes_domain ON nodes(domain_name);
	CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_node_id);
	CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_node_id);
	CREATE INDEX IF NOT EXISTS idx_queue_state_node ON queue_state(node_id);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: Add last_depth column if it doesn't exist (for existing databases)
	_, err = s.db.Exec(`
		ALTER TABLE nodes ADD COLUMN last_depth INTEGER DEFAULT 0;
	`)
	// Ignore error if column already exists
	// SQLite will return "duplicate column name" error which we can safely ignore

	return nil
}

// UpsertNode inserts a new node or updates description if domain exists
// Returns the node_id of the inserted/existing node
func (s *Storage) UpsertNode(domain, description string) (int, error) {
	return s.UpsertNodeWithDepth(domain, description, 0)
}

// UpsertNodeWithDepth inserts a new node or updates description and depth if domain exists
// Returns the node_id of the inserted/existing node
func (s *Storage) UpsertNodeWithDepth(domain, description string, depth int) (int, error) {
	// Insert or update
	_, err := s.db.Exec(`
		INSERT INTO nodes (domain_name, description, crawl_count, last_depth)
		VALUES (?, ?, 0, ?)
		ON CONFLICT(domain_name) DO UPDATE SET
			description = COALESCE(EXCLUDED.description, nodes.description),
			last_depth = EXCLUDED.last_depth
	`, domain, description, depth)

	if err != nil {
		return 0, fmt.Errorf("failed to upsert node: %w", err)
	}

	// Get the node_id
	var nodeID int
	err = s.db.QueryRow("SELECT node_id FROM nodes WHERE domain_name = ?", domain).Scan(&nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve node_id: %w", err)
	}

	return nodeID, nil
}

// IncrementCrawlCount atomically increments the crawl_count for a node
func (s *Storage) IncrementCrawlCount(nodeID int) error {
	_, err := s.db.Exec("UPDATE nodes SET crawl_count = crawl_count + 1 WHERE node_id = ?", nodeID)
	if err != nil {
		return fmt.Errorf("failed to increment crawl count: %w", err)
	}
	return nil
}

// ResetCrawlCount resets the crawl_count to 0 for a node
func (s *Storage) ResetCrawlCount(nodeID int) error {
	_, err := s.db.Exec("UPDATE nodes SET crawl_count = 0 WHERE node_id = ?", nodeID)
	if err != nil {
		return fmt.Errorf("failed to reset crawl count: %w", err)
	}
	return nil
}

// GetNode retrieves a node by domain name, returns nil if not found
func (s *Storage) GetNode(domain string) (*Node, error) {
	var node Node
	err := s.db.QueryRow(`
		SELECT node_id, domain_name, description, crawl_count, created_at
		FROM nodes
		WHERE domain_name = ?
	`, domain).Scan(&node.NodeID, &node.DomainName, &node.Description, &node.CrawlCount, &node.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	return &node, nil
}

// GetNodeWithDepth retrieves a node with depth info by domain name, returns nil if not found
func (s *Storage) GetNodeWithDepth(domain string) (*Node, int, error) {
	var node Node
	var lastDepth int
	err := s.db.QueryRow(`
		SELECT node_id, domain_name, description, crawl_count, created_at, last_depth
		FROM nodes
		WHERE domain_name = ?
	`, domain).Scan(&node.NodeID, &node.DomainName, &node.Description, &node.CrawlCount, &node.CreatedAt, &lastDepth)

	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get node: %w", err)
	}

	return &node, lastDepth, nil
}

// UpsertEdge inserts a new edge or increments weight if it exists
func (s *Storage) UpsertEdge(fromID, toID int) error {
	_, err := s.db.Exec(`
		INSERT INTO edges (from_node_id, to_node_id, weight)
		VALUES (?, ?, 1)
		ON CONFLICT(from_node_id, to_node_id) DO UPDATE SET
			weight = weight + 1
	`, fromID, toID)

	if err != nil {
		return fmt.Errorf("failed to upsert edge: %w", err)
	}
	return nil
}

// LoadResumableNodes returns all nodes with crawl_count < maxCrawls
func (s *Storage) LoadResumableNodes(maxCrawls int) ([]*Node, error) {
	rows, err := s.db.Query(`
		SELECT node_id, domain_name, description, crawl_count, created_at, last_depth
		FROM nodes
		WHERE crawl_count < ?
		ORDER BY created_at ASC
	`, maxCrawls)

	if err != nil {
		return nil, fmt.Errorf("failed to load resumable nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.NodeID, &node.DomainName, &node.Description, &node.CrawlCount, &node.CreatedAt, &node.LastDepth); err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}
		nodes = append(nodes, &node)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating nodes: %w", err)
	}

	return nodes, nil
}

// Close closes the database connection
func (s *Storage) Close() error {
	return s.db.Close()
}

// SaveQueueEntry saves a queue entry to persist crawl state
func (s *Storage) SaveQueueEntry(nodeID int, domain string, depth int) error {
	_, err := s.db.Exec(`
		INSERT INTO queue_state (node_id, domain_name, depth)
		VALUES (?, ?, ?)
	`, nodeID, domain, depth)

	if err != nil {
		return fmt.Errorf("failed to save queue entry: %w", err)
	}
	return nil
}

// LoadQueueEntries loads all saved queue entries for resume
func (s *Storage) LoadQueueEntries() ([]*QueueEntry, error) {
	rows, err := s.db.Query(`
		SELECT node_id, domain_name, depth
		FROM queue_state
		ORDER BY entry_id ASC
	`)

	if err != nil {
		return nil, fmt.Errorf("failed to load queue entries: %w", err)
	}
	defer rows.Close()

	var entries []*QueueEntry
	for rows.Next() {
		var entry QueueEntry
		if err := rows.Scan(&entry.NodeID, &entry.DomainName, &entry.Depth); err != nil {
			return nil, fmt.Errorf("failed to scan queue entry: %w", err)
		}
		entries = append(entries, &entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating queue entries: %w", err)
	}

	return entries, nil
}

// ClearQueueEntries removes all saved queue entries (called on successful completion)
func (s *Storage) ClearQueueEntries() error {
	_, err := s.db.Exec("DELETE FROM queue_state")
	if err != nil {
		return fmt.Errorf("failed to clear queue entries: %w", err)
	}
	return nil
}
