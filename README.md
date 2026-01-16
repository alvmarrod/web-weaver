# Web Weaver

A lightweight web crawler that discovers domains/subdomains and their cross-domain links, storing exploration data for later graph visualization.

---

## Prerequisites

- Go 1.23+ ([install](https://go.dev/dl/))
- GCC (for SQLite CGO compilation)

  ```bash
  # Debian/Ubuntu/Raspbian
  sudo apt-get install build-essential

  # macOS
  xcode-select --install
  ```

---

## Project Setup

### 1. Initialize Project

```bash
mkdir web-weaver
cd web-weaver

# Initialize Go module
go mod init github.com/yourusername/web-weaver

# Create directory structure
mkdir -p cmd/crawler internal/{config,storage,crawler,metrics}
```

### 2. Install Dependencies

```bash
# Core dependencies
go get github.com/gocolly/colly/v2
go get github.com/mattn/go-sqlite3

# Logging
go get github.com/sirupsen/logrus

# CLI (optional, for future flags support)
go get github.com/spf13/cobra
```

### 3. Create Configuration File

Create `config.json` in project root:

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

## Build

```bash
# Download dependencies if missing
go mod tidy

# Development build
go build -o web_weaver ./cmd/crawler

# Production build (optimized for RPi)
go build -ldflags="-s -w" -o web_weaver ./cmd/crawler
```

**Build flags explained:**

- `-s`: Strip debug symbols
- `-w`: Strip DWARF symbols
- Reduces binary size by ~30%

---

## Usage

### First Run (New Crawl)

```bash
./web_weaver
```

- Reads `config.json`
- Creates `crawler.db` if missing
- Starts crawling from `seed_url`
- Press `Ctrl+C` for graceful shutdown

### Resume Crawl

Update `seed_url` in `config.json` to add new starting point, then:

```bash
./web_weaver
```

- Loads existing `crawler.db`
- Re-queues nodes with `crawl_count < max`
- Continues crawling, appending results

### Clean Start

```bash
rm crawler.db metrics.log
./web_weaver
```

---

## Output Files

| File | Description |
|------|-------------|
| `crawler.db` | SQLite database with nodes and edges |
| `metrics.log` | JSON metrics written on exit |

### Inspecting Results

```bash
# View nodes
sqlite3 crawler.db "SELECT * FROM nodes LIMIT 10;"

# View edges
sqlite3 crawler.db "SELECT * FROM edges LIMIT 10;"

# Count statistics
sqlite3 crawler.db "SELECT COUNT(*) FROM nodes;"
sqlite3 crawler.db "SELECT COUNT(*) FROM edges;"

# View metrics
cat metrics.log | jq '.'
```

---

## Logs

**Console Output:**

```bash
INFO[0000] Starting crawl from example.com
INFO[0001] Worker 1: fetched blog.example.com (depth=1, 8 links)
WARN[0003] Worker 2: timeout on slow.example.com (retry 1/3)
INFO[0005] Queue: 45 | Nodes: 120 | Edges: 340
^C
INFO[0010] Shutdown signal received
INFO[0010] Flushing 12 in-memory entries to DB
INFO[0011] Metrics written to metrics.log
INFO[0011] Crawl complete
```

---

## Configuration Reference

| Parameter | Type | Description |
|-----------|------|-------------|
| `seed_url` | string | Starting URL for crawl |
| `max_depth` | int | Maximum BFS depth (default: 5) |
| `max_crawls_per_node` | int | Times to crawl each node (default: 3) |
| `max_subdomains_per_root` | int | Subdomain limit per root domain (default: 3) |
| `max_outbound_links` | int | Links to extract per page (default: 10) |
| `concurrent_workers` | int | Parallel crawlers (default: 3) |
| `request_timeout_ms` | int | HTTP timeout in ms (default: 5000) |
| `retry_attempts` | int | Max retries on failure (default: 3) |
| `retry_delay_ms` | int | Delay between retries (default: 5000) |
| `db_path` | string | SQLite database file path |
| `metrics_path` | string | Metrics output file path |

---

## Troubleshooting

### SQLite CGO Errors

```bash
# Ensure GCC is installed
gcc --version

# Rebuild with verbose output
go build -x -o web_weaver ./cmd/crawler
```

### Permission Errors

```bash
# Ensure write permissions
chmod 644 config.json
chmod 755 .
```

### Memory Issues on RPi

Reduce `concurrent_workers` to 1-2 in `config.json`:

```json
{
  "concurrent_workers": 2
}
```

### Rate Limiting / Blocked Requests

Add delay in future version or manually throttle by reducing workers.

---

## Development

### Run Tests

```bash
# All tests
go test ./...

# Specific package
go test ./internal/crawler -v

# With coverage
go test ./... -cover
```

### Format Code

```bash
go fmt ./...
```

### Lint

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run
```

---

## Project Structure

```text
web-weaver/
├── cmd/
│   └── crawler/
│       └── main.go              # Entry point
├── internal/
│   ├── config/
│   │   └── config.go            # Config loader
│   ├── storage/
│   │   ├── sqlite.go            # DB operations
│   │   └── models.go            # Node/Edge structs
│   ├── crawler/
│   │   ├── crawler.go           # Core logic
│   │   ├── queue.go             # BFS queue
│   │   └── filter.go            # Link filtering
│   └── metrics/
│       └── metrics.go           # Metrics tracking
├── config.json                  # Runtime config
├── crawler.db                   # Generated DB
├── metrics.log                  # Generated metrics
├── go.mod
├── go.sum
└── README.md
```

---

## Contribute

### Setting up pre-commit hooks

**Pre-commit hooks** run automatically when you try to commit changes (`git commit`). They check and reformat any staged files that do not conform to our style guides.

1. To set up the hooks, you first need to install `pre-commit`. On **macOS**, you can use Homebrew:

   ```bash
   brew install pre-commit
   ```

   Alternatively, install it via pip:

   ```bash
   pip install pre-commit
   ```

2. Once `pre-commit` is installed, set up the git hooks by running the following command from the project¡s root directory:

   ```bash
   pre-commit install
   ```

   On success, it will display:

   ```bash
   pre-commit installed at .git/hooks/pre-commit
   ```

Now, the hooks will automatically check your staged files every time you commit.

### Manually run pre-commit hooks

To run all configured hooks on every file in the repository:

```bash
pre-commit run --all-files
```

You can select a specific rule instead of all hooks:

```bash
pre-commit run ruff --all-files
```

## License

MIT
