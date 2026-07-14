package siteorigin

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b:1::/48", "100::/64", "2001:db8::/32",
	"fc00::/7", "fe80::/10", "ff00::/8",
)

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}

func ValidatePublicHost(ctx context.Context, origin string, resolver Resolver) error {
	normalized, err := Normalize(origin)
	if err != nil {
		return err
	}
	parsedHost := strings.TrimPrefix(strings.TrimPrefix(normalized, "https://"), "http://")
	if host, _, splitErr := net.SplitHostPort(parsedHost); splitErr == nil {
		parsedHost = host
	} else {
		parsedHost = strings.Trim(parsedHost, "[]")
	}
	host := strings.ToLower(strings.TrimSuffix(parsedHost, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("direct capture target must be public")
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !IsPublicIP(ip) {
			return fmt.Errorf("direct capture target must be public")
		}
		return nil
	}
	if resolver == nil {
		return fmt.Errorf("public target resolver is unavailable")
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return fmt.Errorf("resolve direct capture target: %w", err)
	}
	for _, address := range addresses {
		if !IsPublicIP(address) {
			return fmt.Errorf("direct capture target resolved to a restricted address")
		}
	}
	return nil
}

func IsPublicIP(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func NewPublicHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	return newPublicHTTPClient(timeout, net.DefaultResolver, dialer.DialContext)
}

type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

func newPublicHTTPClient(timeout time.Duration, resolver Resolver, dial dialContextFunc) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dial == nil {
		dialer := &net.Dialer{Timeout: timeout}
		dial = dialer.DialContext
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve public target: %w", err)
		}
		for _, candidate := range addresses {
			if !IsPublicIP(candidate) {
				return nil, fmt.Errorf("target resolved to a restricted address")
			}
		}
		return dial(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			origin, err := Normalize(req.URL.String())
			if err != nil {
				return err
			}
			return ValidatePublicHost(req.Context(), origin, resolver)
		},
	}
}
