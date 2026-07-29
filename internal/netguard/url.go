package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidatePublicURL rejects non-HTTP targets and hosts which resolve to local,
// private, link-local, multicast or otherwise non-public addresses.
func ValidatePublicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("only valid http:// and https:// URLs are supported")
	}
	if u.User != nil {
		return fmt.Errorf("URLs containing credentials are not supported")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("local and private hosts are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return fmt.Errorf("local and private IP addresses are not allowed")
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve URL host: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("URL host resolved to no addresses")
	}
	for _, addr := range addrs {
		if !IsPublicIP(addr.IP) {
			return fmt.Errorf("URL host resolves to a local or private address")
		}
	}
	return nil
}

func IsPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

// PublicHTTPClient validates every redirect target as well as the original
// request URL. This closes the redirect hop left open by one-time URL checks.
func PublicHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("parse destination address: %w", err)
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve destination: %w", err)
			}
			var lastErr error
			for _, address := range addresses {
				if !IsPublicIP(address.IP) {
					return nil, fmt.Errorf("destination resolves to a local or private address")
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("destination resolved to no addresses")
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return ValidatePublicURL(req.Context(), req.URL.String())
		},
	}
}
