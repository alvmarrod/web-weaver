# Web Weaver - Implementation Plan

## Module Overview & Responsibilities

### Phase 1: Foundation (Data & Config)

#### 1. `internal/storage/models.go`

**Responsibility:** Data structures

- `Node` struct (ID, DomainName, Description, CrawlCount)
- `Edge` struct (FromNodeID, ToNodeID, Weight)
- `QueueEntry` struct (NodeID, DomainName, Depth)
- `Metrics` struct (all metrics fields)

#### 2. `internal/config/config.go`

**Responsibility:** Configuration management

- `Config` struct matching JSON schema
- `LoadConfig(path string) (*Config, error)` - load & validate JSON
- Default values for missing fields

#### 3. `internal/storage/sqlite.go`

**Responsibility:** Database operations

- `NewStorage(dbPath string) (*Storage, error)` - open/create DB, run schema
- `UpsertNode(domain, description string) (nodeID int, err error)`
- `IncrementCrawlCount(nodeID int) error`
- `GetNode(domain string) (*Node, error)`
- `UpsertEdge(fromID, toID int) error` - increment weight
- `LoadResumableNodes(maxCrawls int) ([]*Node, error)` - nodes with crawl_count < max
- `Close() error`
- SQL schema creation in `initSchema()`

---

### Phase 2: Crawling Logic

#### 4. `internal/crawler/queue.go`

**Responsibility:** Thread-safe BFS queue

- `Queue` struct with mutex, condition variable, visited map
- `NewQueue() *Queue`
- `Push(entry QueueEntry) bool` - returns false if already visited
- `Pop() (QueueEntry, bool)` - blocking, returns false when empty & stopped
- `IsEmpty() bool`
- `Size() int`
- `Stop()` - signal queue to stop accepting entries

#### 5. `internal/crawler/filter.go`

**Responsibility:** Link filtering & selection

- `excludedDomains` - compiled regex list (social, ads, analytics)
- `ExtractDomain(urlStr string) (string, error)` - parse hostname
- `ExtractRootDomain(domain string) string` - get root from subdomain
- `IsExcluded(domain string) bool` - check against blacklist
- `FilterLinks(sourceURL string, links []string) []string` - dedupe, filter, limit to 10

#### 6. `internal/crawler/subdomain_limiter.go`

**Responsibility:** Subdomain count enforcement

- `SubdomainLimiter` struct with mutex and `map[rootDomain][]subdomain`
- `NewSubdomainLimiter(maxPerRoot int) *SubdomainLimiter`
- `CanAdd(domain string) bool` - check if under limit
- `Add(domain string) bool` - register subdomain, returns false if limit exceeded

#### 7. `internal/crawler/crawler.go`

**Responsibility:** Core crawl orchestration

- `Crawler` struct (config, storage, queue, limiter, colly collector, metrics)
- `NewCrawler(cfg *Config, storage *Storage) *Crawler` - init colly with callbacks
- `Start(seedURL string)` - enqueue seed, start workers
- `worker(id int)` - worker loop: pop → fetch → extract → enqueue
- `setupColly()` - configure colly callbacks (OnHTML, OnError)
- `handlePage(nodeID int, depth int, doc *colly.HTMLElement)` - extract links/title
- `Stop()` - graceful worker shutdown

---

### Phase 3: Observability

#### 8. `internal/metrics/metrics.go`

**Responsibility:** Metrics tracking & export

- `Metrics` struct (thread-safe counters)
- `NewMetrics() *Metrics`
- `IncrementNodesDiscovered()`
- `IncrementNodesCrawled()`
- `IncrementEdgesRecorded()`
- `IncrementPagesFetched()`
- `IncrementPagesFailed()`
- `RecordFetchTime(duration time.Duration)`
- `WriteToFile(path, reason string) error` - export JSON on exit

---

### Phase 4: Entry Point & Orchestration

#### 9. `cmd/crawler/main.go`

**Responsibility:** CLI entry point & lifecycle

- Load config from `config.json`
- Initialize storage (SQLite)
- Check for resume: load existing nodes or insert seed
- Initialize crawler
- Start crawler workers
- Setup signal handler (SIGINT, SIGTERM)
- Wait for completion (queue empty or signal)
- Graceful shutdown: stop workers, flush DB, write metrics
- Log progress periodically

---

## Interface Contracts & Design Decisions

### Decision 1: Colly Context Passing

**Problem:** Colly callbacks have fixed signatures, but we need to pass `(nodeID, depth)` context.

**Solution:** Use thread-safe map `map[string]QueueEntry` keyed by URL

- Before `c.Visit(url)`, store context: `contextMap[url] = QueueEntry{NodeID, Domain, Depth}`
- In callback, retrieve: `entry := contextMap[url]`
- After callback, delete: `delete(contextMap, url)`
- Protected by `sync.RWMutex`

### Decision 2: Queue Stop Behavior

**Problem:** What happens when `Stop()` is called while workers are blocked on `Pop()`?

**Solution:** Drain-then-stop semantics

- `Stop()` sets `stopped = true` flag
- `Pop()` continues returning items while queue has entries
- `Pop()` returns `(QueueEntry{}, false)` only when **both** stopped=true AND queue is empty
- `Stop()` broadcasts to condition variable to wake all blocked workers

### Decision 3: Resume Depth Strategy

**Problem:** What depth should re-queued nodes have on resume?

**Solution:** Always resume at depth=0

- `LoadResumableNodes()` returns nodes with `crawl_count < max`
- All resumed nodes enqueued at depth=0
- Crawl count limit prevents infinite work
- Simpler than storing/tracking depths in DB

### Decision 4: Metrics Thread Safety

**Problem:** How to safely update metrics from multiple workers?

**Solution:** Single mutex for all fields

- `Metrics` struct has embedded `sync.Mutex`
- All increment/record methods lock before updating
- Average fetch time computed on read (in `WriteToFile`)
- Simple and sufficient for non-performance-critical updates

### Decision 5: Error Handling Policy

**Problem:** When to stop vs retry vs skip on errors?

**Solution:** Three-tier policy

- **FATAL** (stop worker, propagate to main):
  - DB connection lost (`sqlite.ErrBusy` persistent)
  - Storage initialization failure
- **RETRY** (up to 3 attempts with 5s delay):
  - HTTP timeout
  - HTTP 5xx errors
  - Increase timeout by 2s each retry (5s → 7s → 9s)
- **SKIP** (log warning, continue):
  - DNS resolution failure
  - HTTP 4xx errors
  - Single node/edge DB update failure
  - Malformed URLs

Worker logs error and continues for SKIP/RETRY exhausted.

### Decision 6: Filter Return Type

**Problem:** What should `FilterLinks()` return?

**Solution:** Return domain strings `[]string`

- Input: source URL + raw `<a href>` links
- Output: filtered, deduped domain list (max 10)
- Caller (crawler) constructs visit URLs by prepending `https://`
- Keeps filter as pure function with no URL construction logic

### Decision 7: Subdomain Limit Scope

**Problem:** When is subdomain limit checked?

**Solution:** Check before enqueue, not before fetch

- `CanAdd(domain)` called before `queue.Push()`
- If limit exceeded, skip enqueue (log debug message)
- Already-enqueued subdomains are crawled normally
- Prevents queue pollution with rejected subdomains

### Decision 8: Database Transaction Strategy

**Problem:** When to batch operations?

**Solution:** No explicit batching initially

- SQLite handles concurrent writes with WAL mode
- Each UPSERT/UPDATE is auto-committed
- Future optimization: batch every 50 ops if performance issues
- Simplifies initial implementation

---

## Implementation Order

```text
Phase 1: Foundation
  1. models.go          (structs only, no dependencies)
  2. config.go          (JSON loading)
  3. sqlite.go          (DB operations)

Phase 2: Crawling
  4. filter.go          (pure functions)
  5. subdomain_limiter.go (isolated component)
  6. queue.go           (thread-safe queue)
  7. crawler.go         (integrates 4-6 + colly)

Phase 3: Observability
  8. metrics.go         (metrics tracking)

Phase 4: Integration
  9. main.go            (ties everything together)
```

---

## Dependency Graph

```text
main.go
  ├─> config.go
  ├─> storage/sqlite.go
  │     └─> models.go
  ├─> crawler/crawler.go
  │     ├─> queue.go
  │     ├─> filter.go
  │     ├─> subdomain_limiter.go
  │     ├─> storage/sqlite.go
  │     └─> models.go
  └─> metrics/metrics.go
        └─> models.go
```

---

## Testing Strategy (per module)

| Module | Test Type | Focus |
|--------|-----------|-------|
| `models.go` | Unit | Struct validation |
| `config.go` | Unit | JSON parsing, defaults |
| `sqlite.go` | Integration | UPSERT, transactions, schema |
| `filter.go` | Unit | Domain extraction, exclusion rules |
| `subdomain_limiter.go` | Unit | Limit enforcement |
| `queue.go` | Unit | Concurrency, FIFO order, deduplication |
| `crawler.go` | Integration | Mock colly responses, worker flow |
| `metrics.go` | Unit | Counter accuracy, JSON export |
| `main.go` | E2E | Full crawl on small test graph |

---

## Implementation Notes

### Cross-Module Contracts

1. **Storage ↔ Crawler**
   - Crawler calls `UpsertNode()` before `IncrementCrawlCount()`
   - Crawler never assumes node existence, always UPSERT

2. **Queue ↔ Crawler**
   - Queue is stopped via `Stop()`, then all workers exit on `Pop() == false`
   - Queue tracks visited nodes to prevent duplicates

3. **Metrics ↔ Main**
   - Metrics are written only on exit (signal or natural)
   - `WriteToFile()` includes termination reason

### Concurrency Patterns

- **Storage**: All methods are goroutine-safe (SQLite handles locking)
- **Queue**: Mutex + condition variable for blocking pop
- **SubdomainLimiter**: Mutex-protected map
- **Metrics**: Atomic counters or mutex-protected increments

### Error Handling

- **Fatal errors**: DB init failure, config missing → exit immediately
- **Retriable errors**: HTTP timeout → retry with backoff
- **Skip errors**: DNS failure → log and continue

---

## Next Steps

Choose a module to implement first, or proceed in order 1→9. Each module can be implemented and tested independently before integration.

Ready to start with **`internal/storage/models.go`**?
