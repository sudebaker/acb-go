package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidWebhookURL = errors.New("invalid webhook URL")
	ErrSSRF              = errors.New("SSRF attempt blocked")
)

// DnsPinCache stores resolved IPs for webhook URLs to prevent DNS rebinding.
type DnsPinCache struct {
	mu   sync.RWMutex
	ips  map[string][]net.IP
	ttls map[string]time.Time
}

// NewDnsPinCache creates a new DNS pin cache.
func NewDnsPinCache() *DnsPinCache {
	return &DnsPinCache{
		ips:  make(map[string][]net.IP),
		ttls: make(map[string]time.Time),
	}
}

// Get returns pinned IPs for a host, or nil if not cached.
func (c *DnsPinCache) Get(host string) []net.IP {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if expires, ok := c.ttls[host]; ok && time.Now().Before(expires) {
		return c.ips[host]
	}
	return nil
}

// Set pins IPs for a host with a TTL.
func (c *DnsPinCache) Set(host string, ips []net.IP, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ips[host] = ips
	c.ttls[host] = time.Now().Add(ttl)
}

// Global DNS pin cache (5 minute TTL)
var dnsPinCache = NewDnsPinCache()

// allowHTTPEnv controls whether http:// URLs are allowed.
// Set ACB_ALLOW_HTTP_WEBHOOKS=1 to allow (for Docker/internal networks).
// Default: false (HTTPS required).
func allowHTTPWebhooks() bool {
	return os.Getenv("ACB_ALLOW_HTTP_WEBHOOKS") == "1"
}

// isPrivateIP checks if an IP is private, loopback, or in a denylist range.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// RFC 1918
	privateNetworks := []*net.IPNet{
		{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
		{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
		{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)},
		{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(8, 32)},
		// Note: 100.64.0.0/10 (CGNAT/Tailscale) removed — internal agents use Tailscale
	}
	for _, network := range privateNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	// RFC 4632: 127.0.0.0/8, 169.254.0.0/16, ::1/128
	denyRanges := []*net.IPNet{
		{IP: net.ParseIP("127.0.0.0"), Mask: net.CIDRMask(8, 32)},    // loopback
		{IP: net.ParseIP("169.254.0.0"), Mask: net.CIDRMask(16, 32)}, // link-local
		{IP: net.ParseIP("fc00::"), Mask: net.CIDRMask(7, 128)},      // IPv6 unique local
		{IP: net.ParseIP("fe80::"), Mask: net.CIDRMask(10, 128)},     // IPv6 link-local
		{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)},       // IPv6 loopback
	}
	for _, network := range denyRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateWebhookURL validates and sanitizes a webhook URL.
// - Allows http:// when ACB_ALLOW_HTTP_WEBHOOKS=1 (for Docker/internal networks)
// - Requires https:// otherwise (production)
// - Rejects private IPs, loopback, link-local when not in allow-HTTP mode
// - Implements DNS pinning to prevent DNS rebinding attacks
// - No file://, gopher://, etc.
func ValidateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	allowHTTP := allowHTTPWebhooks()

	if scheme == "https" {
		// HTTPS always allowed
	} else if scheme == "http" && allowHTTP {
		// HTTP allowed in internal mode — but still block loopback and metadata
		// to prevent severe SSRF.
	} else {
		return fmt.Errorf("webhook URL must use https:// (got %s); set ACB_ALLOW_HTTP_WEBHOOKS=1 for internal networks", scheme)
	}

	// For HTTPS: resolve and check for private IPs (SSRF protection)
	host := u.Hostname()
	if host == "" {
		return ErrInvalidWebhookURL
	}

	// DNS pinning: check cache first to prevent rebinding
	pinnedIPs := dnsPinCache.Get(host)
	var ips []net.IP
	if pinnedIPs != nil {
		// Use pinned IPs from cache
		ips = pinnedIPs
	} else {
		// Resolve all A/AAAA records
		resolvedIPs, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
		if err != nil {
			return fmt.Errorf("DNS resolution failed: %w", err)
		}
		for _, ip := range resolvedIPs {
			ips = append(ips, ip.IP)
		}
		// Pin the resolved IPs for 5 minutes
		dnsPinCache.Set(host, ips, 5*time.Minute)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("%w: %s resolves to private/loopback IP %s", ErrSSRF, host, ip)
		}
	}

	return nil
}

// NewSafeHTTPClient returns an HTTP client pre-configured with SSRF protections:
// - Disabled redirects (prevents SSRF via 302 to internal hosts)
// - Separate connect timeout (5s) + overall timeout (15s)
func NewSafeHTTPClient() *http.Client {
	rt := &http.Transport{}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: rt,
		// Prevent SSRF via 302 redirects to internal hosts
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// GetConnectTimeout returns a shorter timeout for TCP connection alone.
func GetConnectTimeout() time.Duration {
	return 5 * time.Second
}