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
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL")
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

	CREATE INDEX IF NOT EXISTS idx_nodes_domain ON nodes(domain_name);
	CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_node_id);
	CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_node_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// UpsertNode inserts a new node or updates description if domain exists
// Returns the node_id of the inserted/existing node
func (s *Storage) UpsertNode(domain, description string) (int, error) {
	// Insert or ignore (if exists)
	_, err := s.db.Exec(`
		INSERT INTO nodes (domain_name, description, crawl_count)
		VALUES (?, ?, 0)
		ON CONFLICT(domain_name) DO UPDATE SET
			description = COALESCE(EXCLUDED.description, nodes.description)
	`, domain, description)

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
		SELECT node_id, domain_name, description, crawl_count, created_at
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
		if err := rows.Scan(&node.NodeID, &node.DomainName, &node.Description, &node.CrawlCount, &node.CreatedAt); err != nil {
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
