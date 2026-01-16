package storage

import "time"

// Node represents a domain or subdomain in the crawl graph
type Node struct {
	NodeID      int
	DomainName  string
	Description string
	CrawlCount  int
	CreatedAt   time.Time
}

// Edge represents a directed link between two nodes
type Edge struct {
	EdgeID     int
	FromNodeID int
	ToNodeID   int
	Weight     int
}

// QueueEntry represents an item in the BFS crawl queue
type QueueEntry struct {
	NodeID     int
	DomainName string
	Depth      int
}

// Metrics tracks crawl statistics for export on exit
type Metrics struct {
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	NodesDiscovered   int       `json:"nodes_discovered"`
	NodesCrawled      int       `json:"nodes_crawled"`
	EdgesRecorded     int       `json:"edges_recorded"`
	PagesFetched      int       `json:"pages_fetched"`
	PagesFailed       int       `json:"pages_failed"`
	TotalFetchTimeMs  int64     `json:"total_fetch_time_ms"`
	AvgFetchTimeMs    int64     `json:"avg_fetch_time_ms"`
	TerminationReason string    `json:"termination_reason"`
}
