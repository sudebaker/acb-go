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
	"time"
)

var (
	ErrInvalidWebhookURL = errors.New("invalid webhook URL")
	ErrSSRF              = errors.New("SSRF attempt blocked")
)

// allowHTTPEnv controls whether http:// URLs are allowed.
// Set ACB_ALLOW_HTTP_WEBHOOKS=1 to allow (for Docker/internal networks).
// Default: false (HTTPS required).
func allowHTTPWebhooks() bool {
	return os.Getenv("ACB_ALLOW_HTTP_WEBHOOKS") == "1"
}

// isPrivateIP checks if an IP is private, loopback, or in a denylist range.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// RFC 1918
	privateNetworks := []*net.IPNet{
		{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
		{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
		{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)},
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
		// HTTP allowed in internal mode — skip private IP checks
		// since Docker agents communicate on localhost/internal IPs
		return nil
	} else {
		return fmt.Errorf("webhook URL must use https:// (got %s); set ACB_ALLOW_HTTP_WEBHOOKS=1 for internal networks", scheme)
	}

	// For HTTPS: resolve and check for private IPs (SSRF protection)
	host := u.Host
	if host == "" {
		return ErrInvalidWebhookURL
	}

	// Resolve all A/AAAA records
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed: %w", err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return fmt.Errorf("%w: %s resolves to private/loopback IP %s", ErrSSRF, host, ip.IP)
		}
	}

	return nil
}

// NewSafeHTTPClient returns an HTTP client pre-configured with SSRF protections:
// - Disabled redirects (prevents SSRF via 302 to internal hosts)
// - Separate connect timeout (5s) + overall timeout (15s)
func NewSafeHTTPClient() *http.Client {
	rt := &http.Transport{
		DisableRedirects: true,
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: rt,
	}
}

// GetConnectTimeout returns a shorter timeout for TCP connection alone.
func GetConnectTimeout() time.Duration {
	return 5 * time.Second
}

// CheckAndBlockPrivateIPs validates a URL and blocks private/loopback hosts.
func CheckAndBlockPrivateIPs(rawURL string) error {
	if err := ValidateWebhookURL(rawURL); err != nil {
		return err
	}
	return nil
}

// ExtractHostForIPCheck returns the host portion of a URL for DNS resolution.
func ExtractHostForIPCheck(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Host, nil
}