package crawler

import (
	"fmt"
	"sync"
	"time"

	"github.com/alvmarrod/web-weaver/internal/config"
	"github.com/alvmarrod/web-weaver/internal/storage"
	"github.com/gocolly/colly/v2"
	"github.com/sirupsen/logrus"
)

// Crawler orchestrates the web crawling process
type Crawler struct {
	cfg             *config.Config
	storage         *storage.Storage
	queue           *Queue
	limiter         *SubdomainLimiter
	collector       *colly.Collector
	contextMap      map[string]storage.QueueEntry
	contextMu       sync.RWMutex
	wg              sync.WaitGroup
	stopChan        chan struct{}
	stopOnce        sync.Once
	inFlightMu      sync.Mutex
	inFlight        int
	metricsCallback func(nodesCrawled, nodesDiscovered, edgesRecorded, pagesFetched, pagesFailed int)
}

// NewCrawler creates a new crawler instance
func NewCrawler(cfg *config.Config, store *storage.Storage, metricsCallback func(int, int, int, int, int)) *Crawler {
	c := &Crawler{
		cfg:             cfg,
		storage:         store,
		queue:           NewQueue(),
		limiter:         NewSubdomainLimiter(cfg.MaxSubdomainsPerRoot),
		contextMap:      make(map[string]storage.QueueEntry),
		stopChan:        make(chan struct{}),
		metricsCallback: metricsCallback,
	}

	c.setupColly()
	return c
}

// setupColly configures the Colly collector with callbacks
func (c *Crawler) setupColly() {
	c.collector = colly.NewCollector(
		colly.Async(true),
		colly.MaxDepth(0), // Managed manually via queue depth
	)

	// Set request timeout
	c.collector.SetRequestTimeout(time.Duration(c.cfg.RequestTimeoutMs) * time.Millisecond)

	// Limit parallelism
	c.collector.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: c.cfg.ConcurrentWorkers,
		Delay:       0,
	})

	// Extract title
	c.collector.OnHTML("title", func(e *colly.HTMLElement) {
		domain, err := ExtractDomain(e.Request.URL.String())
		if err != nil || domain == "" {
			return
		}

		ctx := c.getContextWithFallback(domain)
		if ctx == nil {
			return
		}

		title := e.Text
		if len(title) > 60 {
			title = title[:60]
		}

		// Update node description with title
		_, err = c.storage.UpsertNode(ctx.DomainName, title)
		if err != nil {
			logrus.Warnf("Failed to update node description: %v", err)
		}
	})

	// Extract meta description as fallback
	c.collector.OnHTML("meta[name=description]", func(e *colly.HTMLElement) {
		domain, err := ExtractDomain(e.Request.URL.String())
		if err != nil || domain == "" {
			return
		}

		ctx := c.getContextWithFallback(domain)
		if ctx == nil {
			return
		}

		// Only use meta description if title hasn't been set
		node, err := c.storage.GetNode(ctx.DomainName)
		if err != nil || node == nil || node.Description != "" {
			return
		}

		description := e.Attr("content")
		if len(description) > 60 {
			description = description[:60]
		}

		_, err = c.storage.UpsertNode(ctx.DomainName, description)
		if err != nil {
			logrus.Warnf("Failed to update node description: %v", err)
		}
	})

	// Extract links
	c.collector.OnHTML("a[href]", func(e *colly.HTMLElement) {
		domain, err := ExtractDomain(e.Request.URL.String())
		if err != nil || domain == "" {
			return
		}

		ctx := c.getContextWithFallback(domain)
		if ctx == nil {
			// Silently skip - likely a redirect to an untracked domain
			return
		}

		link := e.Attr("href")
		c.handleLink(ctx, link)
	})

	// Handle successful response
	c.collector.OnResponse(func(r *colly.Response) {
		defer c.decrementInFlight()

		// Extract domain from response URL
		domain, err := ExtractDomain(r.Request.URL.String())
		if err != nil || domain == "" {
			return
		}

		ctx := c.getContextWithFallback(domain)
		if ctx == nil {
			// Silently skip - likely a redirect outside our crawl scope
			return
		}

		logrus.Infof("Worker fetched %s (depth=%d, status=%d)", ctx.DomainName, ctx.Depth, r.StatusCode)
		if c.metricsCallback != nil {
			c.metricsCallback(0, 0, 0, 1, 0) // pagesFetched++
		}
	})

	// Handle errors with retry logic
	c.collector.OnError(func(r *colly.Response, err error) {
		defer c.decrementInFlight()

		// Log even if context is missing
		if r != nil && r.Request != nil {
			logrus.Errorf("OnError called for %s: %v (status: %d)", r.Request.URL, err, r.StatusCode)

			// Extract domain and delete context
			domain, extractErr := ExtractDomain(r.Request.URL.String())
			if extractErr == nil && domain != "" {
				c.deleteContext(domain)

				if c.metricsCallback != nil {
					c.metricsCallback(0, 0, 0, 0, 1) // pagesFailed++
				}
			}
		} else {
			logrus.Errorf("OnError called with nil response: %v", err)
		}
	})
}

// EnqueueSeed enqueues the initial seed URL
func (c *Crawler) EnqueueSeed(seedURL string) (int, error) {
	// Extract seed domain and create initial node
	seedDomain, err := ExtractDomain(seedURL)
	if err != nil || seedDomain == "" {
		return 0, fmt.Errorf("invalid seed URL: %w", err)
	}

	// Upsert seed node
	nodeID, err := c.storage.UpsertNode(seedDomain, "")
	if err != nil {
		return 0, fmt.Errorf("failed to create seed node: %w", err)
	}

	// Enqueue seed
	c.Enqueue(storage.QueueEntry{
		NodeID:     nodeID,
		DomainName: seedDomain,
		Depth:      0,
	})

	return nodeID, nil
}

// Start begins the crawler workers
func (c *Crawler) Start() {
	logrus.Infof("Starting %d crawler workers", c.cfg.ConcurrentWorkers)

	// Start workers
	for i := 0; i < c.cfg.ConcurrentWorkers; i++ {
		c.wg.Add(1)
		go c.worker(i + 1)
	}
}

// worker processes queue entries
func (c *Crawler) worker(id int) {
	defer c.wg.Done()

	logrus.Infof("Worker %d started", id)

	for {
		select {
		case <-c.stopChan:
			logrus.Infof("Worker %d received stop signal", id)
			return
		default:
		}

		// Pop next entry (blocks if empty)
		entry, ok := c.queue.Pop()
		if !ok {
			// Queue stopped and empty
			logrus.Infof("Worker %d: queue stopped, exiting", id)
			return
		}

		logrus.Debugf("Worker %d: popped %s (depth=%d)", id, entry.DomainName, entry.Depth)

		// Check crawl count limit
		node, err := c.storage.GetNode(entry.DomainName)
		if err != nil {
			logrus.Warnf("Worker %d: failed to get node %s: %v", id, entry.DomainName, err)
			continue
		}

		if node == nil {
			logrus.Warnf("Worker %d: node not found for %s, skipping", id, entry.DomainName)
			continue
		}

		if node.CrawlCount >= c.cfg.MaxCrawlsPerNode {
			logrus.Debugf("Worker %d: node %s at max crawls, skipping", id, entry.DomainName)
			continue
		}

		// Construct URL and fetch
		targetURL := "https://" + entry.DomainName
		c.setContext(entry.DomainName, entry)

		// Increment crawl count
		if err := c.storage.IncrementCrawlCount(entry.NodeID); err != nil {
			logrus.Warnf("Worker %d: failed to increment crawl count: %v", id, err)
		}

		if c.metricsCallback != nil {
			c.metricsCallback(1, 0, 0, 0, 0) // nodesCrawled++
		}

		// Increment in-flight counter before async visit
		c.incrementInFlight()

		// Visit URL
		if err := c.collector.Visit(targetURL); err != nil {
			c.decrementInFlight() // Decrement on immediate failure
			logrus.Warnf("Worker %d: visit failed for %s: %v", id, targetURL, err)
			c.deleteContext(entry.DomainName)
		} else {
			logrus.Infof("Worker %d: scheduled visit to %s (depth=%d)", id, targetURL, entry.Depth)
		}
	}
}

// handleLink processes a single extracted link
func (c *Crawler) handleLink(sourceCtx *storage.QueueEntry, link string) {
	targetDomain, err := ExtractDomain(link)
	if err != nil || targetDomain == "" {
		return
	}

	// Skip same-domain links
	if targetDomain == sourceCtx.DomainName {
		return
	}

	// Skip excluded domains
	if IsExcluded(targetDomain) {
		return
	}

	// Check subdomain limit
	if !c.limiter.CanAdd(targetDomain) {
		return
	}

	// Upsert target node
	targetNodeID, err := c.storage.UpsertNode(targetDomain, "")
	if err != nil {
		logrus.Warnf("Failed to upsert target node %s: %v", targetDomain, err)
		return
	}

	// Increment nodes discovered (new node found via link)
	if c.metricsCallback != nil {
		c.metricsCallback(0, 1, 0, 0, 0) // nodesDiscovered++
	}

	// Record edge
	if err := c.storage.UpsertEdge(sourceCtx.NodeID, targetNodeID); err != nil {
		logrus.Warnf("Failed to upsert edge %s -> %s: %v", sourceCtx.DomainName, targetDomain, err)
		return
	}

	// Increment edges metric
	if c.metricsCallback != nil {
		c.metricsCallback(0, 0, 1, 0, 0) // edgesRecorded++
	}

	logrus.Infof("Edge: %s -> %s (depth %d->%d)", sourceCtx.DomainName, targetDomain, sourceCtx.Depth, sourceCtx.Depth+1)

	// Check depth limit
	nextDepth := sourceCtx.Depth + 1
	if nextDepth > c.cfg.MaxDepth {
		return
	}

	// Add to subdomain limiter
	c.limiter.Add(targetDomain)

	// Enqueue target
	c.queue.Push(storage.QueueEntry{
		NodeID:     targetNodeID,
		DomainName: targetDomain,
		Depth:      nextDepth,
	})
}

// Stop gracefully stops the crawler (safe to call multiple times)
func (c *Crawler) Stop() {
	c.stopOnce.Do(func() {
		logrus.Info("Stopping crawler...")

		// Stop queue and signal workers
		logrus.Debug("Stopping queue...")
		c.queue.Stop()

		logrus.Debug("Signaling workers to stop...")
		close(c.stopChan)

		// Wait for workers to finish with timeout
		logrus.Debug("Waiting for workers to finish...")
		workersDone := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(workersDone)
		}()

		select {
		case <-workersDone:
			logrus.Debug("All workers stopped")
		case <-time.After(5 * time.Second):
			logrus.Warn("Workers timeout (5s) - some workers may still be running")
		}

		// Wait for collector to finish in-flight requests (with aggressive timeout)
		inFlight := c.getInFlight()
		if inFlight > 0 {
			logrus.Infof("Waiting for %d in-flight requests (max 10s)...", inFlight)
			collectorDone := make(chan struct{})
			go func() {
				c.collector.Wait()
				close(collectorDone)
			}()

			select {
			case <-collectorDone:
				logrus.Info("All in-flight requests completed")
			case <-time.After(10 * time.Second):
				remaining := c.getInFlight()
				logrus.Warnf("Timeout waiting for requests - abandoning %d in-flight requests", remaining)
			}
		}

		logrus.Info("Crawler stopped")
	})
}

// Enqueue adds a node to the crawl queue
func (c *Crawler) Enqueue(entry storage.QueueEntry) bool {
	// Add to subdomain limiter
	c.limiter.Add(entry.DomainName)

	// Push to queue
	return c.queue.Push(entry)
}

// WaitUntilEmpty blocks until the queue is empty AND no requests are in-flight
func (c *Crawler) WaitUntilEmpty() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			size := c.queue.Size()
			inFlight := c.getInFlight()
			logrus.Infof("Queue status: %d items, %d in-flight requests", size, inFlight)
		default:
		}

		time.Sleep(1 * time.Second)

		queueEmpty := c.queue.IsEmpty()
		inFlight := c.getInFlight()

		if queueEmpty && inFlight == 0 {
			// Double-check after a short delay
			logrus.Infof("Queue and in-flight both zero, double-checking...")
			time.Sleep(2 * time.Second)

			if c.queue.IsEmpty() && c.getInFlight() == 0 {
				logrus.Info("Queue confirmed empty with no in-flight requests, initiating natural shutdown")
				c.Stop()
				return
			}
		}
	}
}

// Helper methods for in-flight request tracking
func (c *Crawler) incrementInFlight() {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	c.inFlight++
}

func (c *Crawler) decrementInFlight() {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	c.inFlight--
}

func (c *Crawler) getInFlight() int {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	return c.inFlight
}

// Context management helpers (use domain as key, not full URL)
func (c *Crawler) setContext(domain string, entry storage.QueueEntry) {
	c.contextMu.Lock()
	defer c.contextMu.Unlock()
	c.contextMap[domain] = entry
}

func (c *Crawler) getContext(domain string) *storage.QueueEntry {
	c.contextMu.RLock()
	defer c.contextMu.RUnlock()
	if entry, ok := c.contextMap[domain]; ok {
		return &entry
	}
	return nil
}

func (c *Crawler) getContextWithFallback(domain string) *storage.QueueEntry {
	// Try exact match first
	ctx := c.getContext(domain)
	if ctx != nil {
		return ctx
	}

	// Try root domain match (handles www.example.com vs example.com redirects)
	c.contextMu.RLock()
	defer c.contextMu.RUnlock()

	rootDomain := ExtractRootDomain(domain)
	for key, entry := range c.contextMap {
		if ExtractRootDomain(key) == rootDomain {
			// Only log this at trace level to reduce spam
			entryCopy := entry
			return &entryCopy
		}
	}

	return nil
}

func (c *Crawler) deleteContext(domain string) {
	c.contextMu.Lock()
	defer c.contextMu.Unlock()
	delete(c.contextMap, domain)
}
