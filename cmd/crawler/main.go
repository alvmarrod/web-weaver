package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alvmarrod/web-weaver/internal/config"
	"github.com/alvmarrod/web-weaver/internal/crawler"
	"github.com/alvmarrod/web-weaver/internal/metrics"
	"github.com/alvmarrod/web-weaver/internal/storage"
	"github.com/alvmarrod/web-weaver/internal/version"
	"github.com/sirupsen/logrus"
)

func main() {
	// Configure logging
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	logrus.Infof("Web Weaver v%s starting...", version.Version)

	// Load configuration
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		logrus.Fatalf("Failed to load config: %v", err)
	}

	logrus.Infof("Configuration loaded: seed=%s, depth=%d, workers=%d",
		cfg.SeedURL, cfg.MaxDepth, cfg.ConcurrentWorkers)

	// Initialize storage
	store, err := storage.NewStorage(cfg.DBPath)
	if err != nil {
		logrus.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	logrus.Infof("Database initialized: %s", cfg.DBPath)

	// Initialize metrics tracker
	tracker := metrics.NewTracker()

	// Metrics callback for crawler
	metricsCallback := func(nodesCrawled, nodesDiscovered, edgesRecorded, pagesFetched, pagesFailed int) {
		if nodesCrawled > 0 {
			tracker.IncrementNodesCrawled()
		}
		if nodesDiscovered > 0 {
			tracker.IncrementNodesDiscovered()
		}
		if edgesRecorded > 0 {
			tracker.IncrementEdgesRecorded()
		}
		if pagesFetched > 0 {
			tracker.IncrementPagesFetched()
		}
		if pagesFailed > 0 {
			tracker.IncrementPagesFailed()
		}
	}

	// Initialize crawler
	c := crawler.NewCrawler(cfg, store, metricsCallback)

	// Handle resume logic - load resumable nodes into memory
	resumableNodes, err := store.LoadResumableNodes(cfg.MaxCrawlsPerNode)
	if err != nil {
		logrus.Fatalf("Failed to load resumable nodes: %v", err)
	}

	if len(resumableNodes) > 0 {
		logrus.Infof("Resuming crawl: found %d resumable nodes, loading into memory...", len(resumableNodes))

		// Load nodes from storage into memory graph
		if err := c.LoadFromStorage(); err != nil {
			logrus.Fatalf("Failed to load nodes into memory: %v", err)
		}

		// Re-queue all resumable nodes at depth 0
		for _, node := range resumableNodes {
			entry := storage.QueueEntry{
				NodeID:     node.NodeID,
				DomainName: node.DomainName,
				Depth:      0,
			}
			c.Enqueue(entry)
			tracker.IncrementNodesDiscovered()
		}
	} else {
		// No resumable nodes - start fresh with seed
		logrus.Info("No resumable nodes found, starting fresh crawl with seed")

		// Extract seed domain
		seedDomain, err := crawler.ExtractDomain(cfg.SeedURL)
		if err != nil {
			logrus.Fatalf("Invalid seed URL: %v", err)
		}

		// Check if seed exists and reset crawl_count if needed
		existingSeed, err := store.GetNode(seedDomain)
		if err != nil {
			logrus.Fatalf("Failed to check for existing seed: %v", err)
		}

		if existingSeed != nil && existingSeed.CrawlCount >= cfg.MaxCrawlsPerNode {
			logrus.Infof("Seed %s exists with crawl_count=%d, resetting to 0", seedDomain, existingSeed.CrawlCount)
			if err := store.ResetCrawlCount(existingSeed.NodeID); err != nil {
				logrus.Fatalf("Failed to reset crawl count: %v", err)
			}
		}

		// Enqueue seed URL (will create node in memory if doesn't exist)
		if _, err := c.EnqueueSeed(cfg.SeedURL); err != nil {
			logrus.Fatalf("Failed to enqueue seed: %v", err)
		}
		tracker.IncrementNodesDiscovered()
	}

	// Start crawler workers
	c.Start()

	// Setup signal handler for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Track termination reason
	var terminationReason string
	var wg sync.WaitGroup
	shutdownComplete := make(chan struct{})

	// Handle force quit on second signal
	forceQuitChan := make(chan os.Signal, 1)
	signal.Notify(forceQuitChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-forceQuitChan        // First signal (consumed by main handler)
		sig := <-forceQuitChan // Second signal = force quit
		logrus.Warnf("Received second signal (%v) - forcing immediate exit!", sig)
		logrus.Warn("Attempting emergency save...")

		// Emergency flush of memory graph
		if err := c.FlushToStorage(); err != nil {
			logrus.Errorf("Emergency memory flush failed: %v", err)
		} else {
			logrus.Info("Emergency memory flush succeeded")
		}

		// Emergency metrics save
		if err := tracker.WriteToFile(cfg.MetricsPath, "forced_exit"); err != nil {
			logrus.Errorf("Emergency metrics save failed: %v", err)
		}
		os.Exit(1)
	}()

	// Monitor queue for natural termination
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.WaitUntilEmpty()
		terminationReason = "queue_empty"
		// Signal main goroutine
		select {
		case <-shutdownComplete:
			// Already shutting down
		default:
			sigChan <- syscall.SIGTERM
		}
	}()

	// Start progress logger
	stopProgress := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				logrus.Info(tracker.LogProgress())
			case <-stopProgress:
				return
			}
		}
	}()

	// Wait for signal (SIGTERM or natural completion)
	sig := <-sigChan
	logrus.Infof("Received signal: %v", sig)

	// Mark shutdown in progress
	close(shutdownComplete)

	// Stop progress logger first
	close(stopProgress)

	// Determine termination reason if not already set
	if terminationReason == "" {
		terminationReason = "signal"
	}

	logrus.Info("Initiating graceful shutdown...")
	logrus.Info("Step 1/5: Stopping crawler workers...")

	// Stop crawler (with timeouts built-in)
	c.Stop()

	logrus.Info("Step 2/5: Waiting for background goroutines...")

	// Wait for background goroutines with timeout
	bgDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(bgDone)
	}()

	select {
	case <-bgDone:
		logrus.Info("All background tasks completed")
	case <-time.After(5 * time.Second):
		logrus.Warn("Background tasks timeout (5s), continuing with shutdown")
	}

	logrus.Info("Step 3/5: Flushing in-memory graph to database...")

	// Flush memory graph to database
	if err := c.FlushToStorage(); err != nil {
		logrus.Errorf("Failed to flush memory graph: %v", err)
	} else {
		logrus.Info("Memory graph flushed successfully")
	}

	logrus.Info("Step 4/5: Writing final metrics...")

	// Final progress log
	logrus.Info("Final stats: " + tracker.LogProgress())

	// Write metrics to file
	if err := tracker.WriteToFile(cfg.MetricsPath, terminationReason); err != nil {
		logrus.Errorf("Failed to write metrics: %v", err)
	} else {
		logrus.Infof("Metrics written to %s", cfg.MetricsPath)
	}

	logrus.Info("Step 5/5: Closing database connection...")

	// Database is closed via defer store.Close()

	logrus.Info("Graceful shutdown complete. Goodbye!")
}
