package crawler

import (
	"sync"
)

// SubdomainLimiter enforces max subdomains per root domain
type SubdomainLimiter struct {
	maxPerRoot int
	mu         sync.RWMutex
	// Map: rootDomain -> set of subdomains
	subdomains map[string]map[string]bool
}

// NewSubdomainLimiter creates a new subdomain limiter
func NewSubdomainLimiter(maxPerRoot int) *SubdomainLimiter {
	return &SubdomainLimiter{
		maxPerRoot: maxPerRoot,
		subdomains: make(map[string]map[string]bool),
	}
}

// CanAdd checks if a domain can be added without exceeding the limit
// Does NOT modify state - use Add() to register the domain
func (sl *SubdomainLimiter) CanAdd(domain string) bool {
	rootDomain := ExtractRootDomain(domain)

	sl.mu.RLock()
	defer sl.mu.RUnlock()

	subdomainSet, exists := sl.subdomains[rootDomain]
	if !exists {
		// First subdomain for this root - always allowed
		return true
	}

	// Check if this exact subdomain is already registered
	if subdomainSet[domain] {
		// Already registered - allowed
		return true
	}

	// Check if we've hit the limit
	return len(subdomainSet) < sl.maxPerRoot
}

// Add registers a domain with the limiter
// Returns true if added successfully, false if limit exceeded
func (sl *SubdomainLimiter) Add(domain string) bool {
	rootDomain := ExtractRootDomain(domain)

	sl.mu.Lock()
	defer sl.mu.Unlock()

	// Initialize map for this root domain if needed
	if sl.subdomains[rootDomain] == nil {
		sl.subdomains[rootDomain] = make(map[string]bool)
	}

	subdomainSet := sl.subdomains[rootDomain]

	// Already registered - success
	if subdomainSet[domain] {
		return true
	}

	// Check limit
	if len(subdomainSet) >= sl.maxPerRoot {
		return false
	}

	// Add the subdomain
	subdomainSet[domain] = true
	return true
}

// Count returns the number of subdomains registered for a root domain
func (sl *SubdomainLimiter) Count(rootDomain string) int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	if subdomainSet, exists := sl.subdomains[rootDomain]; exists {
		return len(subdomainSet)
	}
	return 0
}
