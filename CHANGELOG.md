# Changelog

All notable changes to Web Weaver will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2026-01-1

### Added

- Queue state persistence - pending work saved on checkpoint/shutdown
- Node depth tracking from original seed
- Smart resume from exact crawl state

### Changed

- Resume behavior (Breaking): Nodes now resume at their last crawl depth instead of resetting to depth=0
- Database schema: added `last_depth` column to nodes and `queue_state` table
- Depth limit now correctly enforced across resume cycles

### Fixed

- Depth reset bug where all resumed nodes were treated as new seeds

## [0.2.0] - 2026-01-16

### Changed

- Switched to `In-Memory` graph, avoiding writing to disk all results.
  - Save checkpoints every 5m + on shutdown.
  - Crash could lose up to those 5m.

## [0.1.1] - 2026-01-16

### Changed

- Updated `sqlite` connection to reduce `fsync` calls.

## [0.1.0] - 2026-01-16

### Added

- **Core Crawler Engine**
  - Breadth-first search (BFS) crawling strategy
  - Configurable depth limit (default: 5)
  - Concurrent workers (default: 3)
  - Cross-domain link discovery

- **Graph Construction**
  - Domain/subdomain nodes
  - Directed edges with weight tracking
  - SQLite storage with WAL mode for concurrency

- **Crawl Limits & Control**
  - Max crawls per node (default: 3)
  - Max subdomains per root domain (default: 3)
  - Max outbound links per page (default: 10)
  - Configurable request timeout and retry logic

- **Resume & Persistence**
  - Automatic resume from interrupted crawls
  - Crawl count reset for exhausted seeds
  - Idempotent database operations (UPSERT)

- **Observability**
  - Real-time progress logging (10s intervals)
  - Comprehensive metrics export (JSON)
  - Detailed worker and fetch logging

- **Graceful Shutdown**
  - SIGTERM/SIGINT signal handling
  - 20-second timeout for in-flight requests
  - Emergency metrics save on force quit (double signal)
  - Natural termination when queue empties

- **Link Filtering**
  - Cross-domain only (excludes same-domain links)
  - Blacklist for social media, ads, and analytics domains
  - Deduplication by target domain

- **Configuration**
  - JSON-based configuration file
  - Default values for all optional parameters
  - Validation on startup

### Technical Details

- **Language**: Go 1.23+
- **Dependencies**:
  - Colly v2 (web scraping)
  - go-sqlite3 (database)
  - logrus (logging)
- **Database Schema**: Nodes and edges with indices
- **Concurrency**: Thread-safe queue, storage, and metrics

### Known Limitations

- No robots.txt compliance
- No rate limiting per domain
- Context lookup fails on some URL redirects (handled gracefully)
- In-flight requests may be abandoned on shutdown timeout
