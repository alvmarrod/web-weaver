package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds all runtime configuration parameters
type Config struct {
	SeedURL              string `json:"seed_url"`
	MaxDepth             int    `json:"max_depth"`
	MaxCrawlsPerNode     int    `json:"max_crawls_per_node"`
	MaxSubdomainsPerRoot int    `json:"max_subdomains_per_root"`
	MaxOutboundLinks     int    `json:"max_outbound_links"`
	ConcurrentWorkers    int    `json:"concurrent_workers"`
	RequestTimeoutMs     int    `json:"request_timeout_ms"`
	RetryAttempts        int    `json:"retry_attempts"`
	RetryDelayMs         int    `json:"retry_delay_ms"`
	DBPath               string `json:"db_path"`
	MetricsPath          string `json:"metrics_path"`
}

// LoadConfig reads and validates configuration from a JSON file
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// Apply defaults for missing values
	applyDefaults(&cfg)

	// Validate configuration
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// applyDefaults sets default values for unspecified fields
func applyDefaults(cfg *Config) {
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 5
	}
	if cfg.MaxCrawlsPerNode == 0 {
		cfg.MaxCrawlsPerNode = 3
	}
	if cfg.MaxSubdomainsPerRoot == 0 {
		cfg.MaxSubdomainsPerRoot = 3
	}
	if cfg.MaxOutboundLinks == 0 {
		cfg.MaxOutboundLinks = 10
	}
	if cfg.ConcurrentWorkers == 0 {
		cfg.ConcurrentWorkers = 3
	}
	if cfg.RequestTimeoutMs == 0 {
		cfg.RequestTimeoutMs = 5000
	}
	if cfg.RetryAttempts == 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryDelayMs == 0 {
		cfg.RetryDelayMs = 5000
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "crawler.db"
	}
	if cfg.MetricsPath == "" {
		cfg.MetricsPath = "metrics.log"
	}
}

// validate checks that required fields are present and values are sensible
func validate(cfg *Config) error {
	if cfg.SeedURL == "" {
		return fmt.Errorf("seed_url is required")
	}
	if cfg.MaxDepth < 1 {
		return fmt.Errorf("max_depth must be >= 1")
	}
	if cfg.MaxCrawlsPerNode < 1 {
		return fmt.Errorf("max_crawls_per_node must be >= 1")
	}
	if cfg.ConcurrentWorkers < 1 {
		return fmt.Errorf("concurrent_workers must be >= 1")
	}
	if cfg.RequestTimeoutMs < 1000 {
		return fmt.Errorf("request_timeout_ms must be >= 1000")
	}
	return nil
}
