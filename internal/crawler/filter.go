package crawler

import (
	"net/url"
	"regexp"
	"strings"
)

// Excluded domain patterns (social media, ads, analytics)
var excludedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(facebook|fb)\.com`),
	regexp.MustCompile(`(?i)twitter\.com`),
	regexp.MustCompile(`(?i)instagram\.com`),
	regexp.MustCompile(`(?i)linkedin\.com`),
	regexp.MustCompile(`(?i)youtube\.com`),
	regexp.MustCompile(`(?i)google-analytics\.com`),
	regexp.MustCompile(`(?i)doubleclick\.net`),
	regexp.MustCompile(`(?i)^ads?\.`),
	regexp.MustCompile(`(?i)^analytics?\.`),
	regexp.MustCompile(`(?i)googletagmanager\.com`),
	regexp.MustCompile(`(?i)googleapis\.com`),
}

// ExtractDomain extracts the hostname (domain/subdomain) from a URL string
func ExtractDomain(urlStr string) (string, error) {
	// Handle protocol-relative URLs
	if strings.HasPrefix(urlStr, "//") {
		urlStr = "https:" + urlStr
	}

	// Handle relative URLs (no scheme)
	if !strings.Contains(urlStr, "://") {
		return "", nil // Skip relative URLs
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", nil
	}

	return strings.ToLower(hostname), nil
}

// ExtractRootDomain extracts the root domain from a subdomain
// Example: blog.example.com -> example.com
func ExtractRootDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return domain
}

// IsExcluded checks if a domain matches any excluded pattern
func IsExcluded(domain string) bool {
	for _, pattern := range excludedPatterns {
		if pattern.MatchString(domain) {
			return true
		}
	}
	return false
}

// FilterLinks extracts, filters, and selects up to maxLinks cross-domain links
// Returns a list of target domains (not full URLs)
func FilterLinks(sourceURL string, links []string, maxLinks int) []string {
	sourceDomain, err := ExtractDomain(sourceURL)
	if err != nil || sourceDomain == "" {
		return []string{}
	}

	seen := make(map[string]bool)
	var filtered []string

	for _, link := range links {
		// Skip empty links
		if strings.TrimSpace(link) == "" {
			continue
		}

		// Extract target domain
		targetDomain, err := ExtractDomain(link)
		if err != nil || targetDomain == "" {
			continue
		}

		// Skip same-domain links (not cross-domain)
		if targetDomain == sourceDomain {
			continue
		}

		// Skip excluded domains
		if IsExcluded(targetDomain) {
			continue
		}

		// Skip duplicates
		if seen[targetDomain] {
			continue
		}

		seen[targetDomain] = true
		filtered = append(filtered, targetDomain)

		// Stop at max links
		if len(filtered) >= maxLinks {
			break
		}
	}

	return filtered
}
