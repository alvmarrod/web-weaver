# Web Crawler — Low Level Design (LLD)

## 1. Technology Stack

- **Language**: Go 1.21+
- **Crawler**: `github.com/gocolly/colly/v2`
- **Storage**: SQLite3 (`github.com/mattn/go-sqlite3`)
- **Config**: JSON file
- **Concurrency**: 3 goroutines sharing a single queue

---

## 2. Project Structure

```text
web-crawler/
├── cmd/
│   └── crawler/
│       └── main.go           # CLI entry point
├── internal/
│   ├── config/
│   │   └── config.go         # Config loading
│   ├── storage/
│   │   ├── sqlite.go         # SQLite operations
│   │   └── models.go         # Node, Edge structs
│   ├── crawler/
│   │   ├── crawler.go        # Core crawl logic
│   │   ├── queue.go          # Thread-safe BFS queue
│   │   └── filter.go         # Link filtering/selection
│   └── metrics/
│       └── metrics.go        # Progress & metrics tracking
├── config.json               # Runtime configuration
├── crawler.db                # SQLite database
└── metrics.log               # Metrics output file
```

---

## 3. Configuration Schema

**`config.json`**

```json
{
  "seed_url": "https://example.com",
  "max_depth": 5,
  "max_crawls_per_node": 3,
  "max_subdomains_per_root": 3,
  "max_outbound_links": 10,
  "concurrent_workers": 3,
  "request_timeout_ms": 5000,
  "retry_attempts": 3,
  "retry_delay_ms": 5000,
  "db_path": "crawler.db",
  "metrics_path": "metrics.log"
}
```

---

## 4. Database Schema (SQLite)

### 4.1 Tables

```sql
CREATE TABLE nodes (
    node_id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_name TEXT UNIQUE NOT NULL,
    description TEXT,
    crawl_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE edges (
    edge_id INTEGER PRIMARY KEY AUTOINCREMENT,
    from_node_id INTEGER NOT NULL,
    to_node_id INTEGER NOT NULL,
    weight INTEGER DEFAULT 1,
    FOREIGN KEY (from_node_id) REFERENCES nodes(node_id),
    FOREIGN KEY (to_node_id) REFERENCES nodes(node_id),
    UNIQUE(from_node_id, to_node_id)
);

CREATE INDEX idx_nodes_domain ON nodes(domain_name);
CREATE INDEX idx_edges_from ON edges(from_node_id);
CREATE INDEX idx_edges_to ON edges(to_node_id);
```

### 4.2 Operations

- **Insert/Update Node**: UPSERT on `domain_name`
- **Increment Crawl Count**: Atomic UPDATE
- **Insert/Update Edge**: UPSERT on `(from_node_id, to_node_id)`, increment `weight`
- **Resume Logic**: Load all nodes with `crawl_count < max`, re-queue at depth = 0

---

## 5. Core Components

### 5.1 Queue (BFS)

**Type**: Thread-safe FIFO queue with deduplication

**Entry Structure**:

```go
type QueueEntry struct {
    NodeID      int
    DomainName  string
    Depth       int
}
```

**Operations**:

- `Push(entry QueueEntry)` — append if not visited at this depth
- `Pop() (QueueEntry, bool)` — blocking pop, returns false when empty
- `IsEmpty() bool`
- `Size() int`

**Concurrency**: Mutex-protected, condition variable for blocking

---

### 5.2 Crawler Worker

**Lifecycle**:

1. Pop entry from queue (blocks if empty)
2. Check `crawl_count < max_crawls_per_node`
3. Fetch page with Colly
4. Extract title/description → update Node
5. Extract outbound links → filter & select ≤10
6. For each link:
   - Get/create target node
   - Record edge (increment weight)
   - Enqueue if `crawl_count < max` and `depth < max_depth`
7. Increment `crawl_count` for current node
8. Repeat until queue empty or shutdown signal

**Error Handling**:

- Retry HTTP errors 3 times with 5s delay + increased timeout
- Skip DNS failures immediately
- Log errors to stdout

---

### 5.3 Link Filter

**Excluded Domains** (compiled regex):

```text
facebook.com, twitter.com, instagram.com, linkedin.com,
google-analytics.com, doubleclick.net, ads.*, analytics.*
```

**Selection Heuristic**:

1. Parse all `<a href>` from HTML
2. Extract domain/subdomain (strip paths/query/fragment)
3. Keep only cross-domain links (target ≠ source)
4. Deduplicate by target domain
5. Take first 10 in DOM order

---

### 5.4 Subdomain Limiter

**Per Root Domain**:

- Track `map[rootDomain][]subdomain` in memory
- Before enqueuing subdomain:
  - Extract root domain (e.g., `blog.example.com` → `example.com`)
  - Count existing subdomains for root
  - Reject if count ≥ `max_subdomains_per_root`

---

### 5.5 Colly Configuration

```go
c := colly.NewCollector(
    colly.Async(true),
    colly.MaxDepth(0), // managed manually
    colly.IgnoreRobotsTxt(), // optional: set false for compliance
)

c.Limit(&colly.LimitRule{
    DomainGlob:  "*",
    Parallelism: 3,
    Delay:       0, // no rate limit
})

c.SetRequestTimeout(5 * time.Second) // increases on retry
c.OnHTML("a[href]", extractLinks)
c.OnHTML("title", extractTitle)
c.OnHTML("meta[name=description]", extractMeta)
c.OnError(handleError)
```

---

## 6. Graceful Shutdown

**Signal Handling**:

```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

go func() {
    <-sigChan
    log.Println("Shutdown signal received")
    stopCrawlers()   // stop accepting new work
    flushToDB()      // write remaining in-memory data
    writeMetrics()   // write metrics.log
    os.Exit(0)
}()
```

**Natural Termination**:

- Queue returns empty + all workers idle → trigger same shutdown flow

---

## 7. Resume Logic

**On Startup**:

1. Load config
2. Open SQLite DB (create if missing)
3. Parse seed URL → extract domain
4. Check if domain exists in DB:
   - **New**: Insert seed node, enqueue at depth=0
   - **Exists**: Load all nodes with `crawl_count < max`, enqueue at depth=0
5. Start workers

**Idempotency**:

- All DB operations are UPSERT
- Edge weights accumulate
- Crawl counts increment safely

---

## 8. Metrics (Written on Exit)

**`metrics.log` format** (JSON):

```json
{
  "start_time": "2025-01-16T10:00:00Z",
  "end_time": "2025-01-16T10:15:32Z",
  "nodes_discovered": 1523,
  "nodes_crawled": 456,
  "edges_recorded": 3421,
  "pages_fetched": 1368,
  "pages_failed": 34,
  "avg_fetch_time_ms": 234,
  "termination_reason": "signal" // or "queue_empty"
}
```

**Progress Logs** (stdout):

```bash
[INFO] Starting crawl from example.com
[INFO] Worker 1 fetched blog.example.com (depth=1, 8 outbound links)
[WARN] Worker 2 failed to fetch slow.example.com (timeout, retry 1/3)
[INFO] Queue: 45 | Nodes: 120 | Edges: 340
[INFO] Shutdown signal received
[INFO] Flushing 12 in-memory nodes to DB
[INFO] Metrics written to metrics.log
```

---

## 9. Data Flow

```text
Config → Seed → SQLite
              ↓
         Queue (BFS)
              ↓
    ┌─────────┴─────────┐
    ↓         ↓         ↓
Worker-1  Worker-2  Worker-3
    ↓         ↓         ↓
    └─────────┬─────────┘
              ↓
      Colly Fetch + Parse
              ↓
      Filter & Select Links
              ↓
      SQLite (Nodes + Edges)
              ↓
      Enqueue Targets
              ↓
     (Loop until empty/signal)
              ↓
        Graceful Exit
              ↓
      metrics.log
```

---

## 10. Key Implementation Notes

### 10.1 Domain Extraction

```go
func extractDomain(urlStr string) (string, error) {
    u, _ := url.Parse(urlStr)
    return u.Hostname(), nil // includes subdomain
}
```

### 10.2 Root Domain Extraction

```go
func extractRootDomain(domain string) string {
    parts := strings.Split(domain, ".")
    if len(parts) >= 2 {
        return parts[len(parts)-2] + "." + parts[len(parts)-1]
    }
    return domain
}
```

### 10.3 Transaction Batching

- Use SQLite transactions for batch inserts (every 50 nodes/edges)
- Reduces I/O overhead on RPi

### 10.4 Memory Efficiency

- Don't hold full HTML in memory
- Stream parse with Colly callbacks
- Limit in-memory queue size (e.g., 1000 entries)

---

## 11. Testing Strategy

- **Unit Tests**: Queue, filter, domain extraction
- **Integration Tests**: SQLite UPSERT operations, resume logic
- **E2E Test**: Crawl small known graph (e.g., 3-node loop), verify DB state

---

## 12. Future Enhancements (Out of Scope)

- Distributed crawling
- Politeness delays / robots.txt compliance
- Content-based link prioritization
- Graph visualization tool (separate project)
