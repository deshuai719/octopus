package siteorigin

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// Normalize returns the canonical HTTP(S) origin for rawURL.
func Normalize(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse site URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("site URL must use http or https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("site URL must not include user information")
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "" {
		return "", fmt.Errorf("site URL must include a host")
	}
	if ip := net.ParseIP(hostname); ip != nil {
		hostname = ip.String()
	} else {
		hostname, err = idna.Lookup.ToASCII(hostname)
		if err != nil {
			return "", fmt.Errorf("normalize internationalized host: %w", err)
		}
		hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	}

	port := parsed.Port()
	if port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("site URL port is invalid")
		}
		if (scheme == "http" && value == 80) || (scheme == "https" && value == 443) {
			port = ""
		}
	}

	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return scheme + "://" + host, nil
}

func Pattern(origin string) (string, error) {
	normalized, err := Normalize(origin)
	if err != nil {
		return "", err
	}
	return normalized + "/*", nil
}
