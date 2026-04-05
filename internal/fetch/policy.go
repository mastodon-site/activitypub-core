package fetch

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mastodon-site/activitypub-core/internal/config"
)

// Policy constrains outbound federation HTTP (SSRF mitigation).
type Policy struct {
	AllowHTTP bool
	// relaxLocalFetch disables private/loopback/link-local IP blocking and uses a
	// standard hostname dial (no pin). Only TestingPolicy sets this for unit/integration tests.
	relaxLocalFetch bool
}

// PolicyFromConfig derives a fetch policy from process config (nil cfg → strict defaults).
func PolicyFromConfig(c *config.Config) *Policy {
	if c == nil {
		return &Policy{}
	}
	if c.FetchRelaxLocal {
		return TestingPolicy()
	}
	return &Policy{
		AllowHTTP: c.FetchAllowHTTP,
	}
}

// TestingPolicy allows http:// and loopback/private targets. It is for tests (httptest,
// integration fixtures) only; production callers should use PolicyFromConfig.
func TestingPolicy() *Policy {
	return &Policy{AllowHTTP: true, relaxLocalFetch: true}
}

// LaxPolicyForTests is an alias for TestingPolicy (prefer TestingPolicy in new code).
func LaxPolicyForTests() *Policy {
	return TestingPolicy()
}

// CheckURL parses raw and applies PreDialCheck (scheme, http gate, literal IPs only).
func (p *Policy) CheckURL(ctx context.Context, raw string) error {
	_ = ctx
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("fetch: parse url: %w", err)
	}
	return p.PreDialCheck(u)
}

// PreDialCheck validates scheme, HTTP allowance, and literal-IP hosts.
// Hostnames are not resolved here; under strict policy they are resolved exactly once
// in the transport DialContext and every returned address is checked before connect.
func (p *Policy) PreDialCheck(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("fetch: nil url")
	}
	if u.Host == "" {
		return fmt.Errorf("fetch: missing host")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("fetch: unsupported scheme %q", u.Scheme)
	}
	if scheme == "http" && !p.AllowHTTP {
		return fmt.Errorf("fetch: http disabled (set AP_FETCH_ALLOW_HTTP=1 for development)")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("fetch: missing host")
	}
	if p.relaxLocalFetch {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return p.checkIP(ip)
	}
	return nil
}

// CheckParsedURL validates u using PreDialCheck. The context is unused (kept for API stability).
func (p *Policy) CheckParsedURL(ctx context.Context, u *url.URL) error {
	_ = ctx
	return p.PreDialCheck(u)
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return ip
}

func (p *Policy) checkIP(ip net.IP) error {
	if p.relaxLocalFetch {
		return nil
	}
	if ip == nil {
		return fmt.Errorf("nil IP")
	}
	ip = normalizeIP(ip)
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("disallowed address %s", ip)
	}
	return nil
}

// validateAllResolvedIPs ensures every address from a single DNS lookup is allowed.
func (p *Policy) validateAllResolvedIPs(host string, ips []net.IP) error {
	if p.relaxLocalFetch {
		return nil
	}
	if len(ips) == 0 {
		return fmt.Errorf("fetch: no addresses for %q", host)
	}
	for _, ip := range ips {
		if err := p.checkIP(ip); err != nil {
			return fmt.Errorf("fetch: host %q: %w", host, err)
		}
	}
	return nil
}

func sortIPsPreferIPv4(ips []net.IP) []net.IP {
	seen := make(map[string]struct{})
	var v4, v6 []net.IP
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		ip = normalizeIP(ip)
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if ip.To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	out := make([]net.IP, 0, len(v4)+len(v6))
	out = append(out, v4...)
	out = append(out, v6...)
	return out
}

func (p *Policy) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("fetch: dial %q: %w", addr, err)
	}
	d := &net.Dialer{Timeout: 10 * time.Second}

	if p.relaxLocalFetch {
		return d.DialContext(ctx, network, net.JoinHostPort(host, port))
	}

	if ip := net.ParseIP(host); ip != nil {
		if err := p.checkIP(ip); err != nil {
			return nil, err
		}
		return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("fetch: resolve %q: %w", host, err)
	}
	uniq := make([]net.IP, 0, len(addrs))
	seen := make(map[string]struct{})
	for _, a := range addrs {
		if a.IP == nil {
			continue
		}
		ip := normalizeIP(a.IP)
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, ip)
	}
	if err := p.validateAllResolvedIPs(host, uniq); err != nil {
		return nil, err
	}

	var firstErr error
	for _, ip := range sortIPsPreferIPv4(uniq) {
		conn, dialErr := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		firstErr = dialErr
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("fetch: could not connect to %q", host)
}

type checkingTransport struct {
	p     *Policy
	inner http.RoundTripper
}

func (t *checkingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.p != nil && req.URL != nil {
		if err := t.p.PreDialCheck(req.URL); err != nil {
			return nil, err
		}
	}
	return t.inner.RoundTrip(req)
}

// NewHTTPClientForPolicy builds a client with the given policy (nil uses strict defaults).
func NewHTTPClientForPolicy(p *Policy, timeout time.Duration) *http.Client {
	if p == nil {
		p = &Policy{}
	}
	base := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		IdleConnTimeout:       90 * time.Second,
		DialContext:           p.dialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &checkingTransport{p: p, inner: base},
	}
}

// NewHTTPClient builds a client from config-derived policy (production).
func NewHTTPClient(cfg *config.Config, timeout time.Duration) *http.Client {
	return NewHTTPClientForPolicy(PolicyFromConfig(cfg), timeout)
}
