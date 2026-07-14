package siteorigin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type fixedResolver map[string][]netip.Addr

func (resolver fixedResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	return resolver[host], nil
}

func TestIsPublicIP(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"8.8.8.8":              true,
		"1.1.1.1":              true,
		"2001:4860:4860::8888": true,
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"169.254.169.254":      false,
		"192.168.1.1":          false,
		"::1":                  false,
		"::ffff:127.0.0.1":     false,
		"fd00::1":              false,
		"2001:db8::1":          false,
	}
	for raw, want := range tests {
		if got := IsPublicIP(netip.MustParseAddr(raw)); got != want {
			t.Errorf("IsPublicIP(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestValidatePublicHostRejectsAnyRestrictedDNSAnswer(t *testing.T) {
	resolver := fixedResolver{"relay.example": {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}}
	if err := ValidatePublicHost(context.Background(), "https://relay.example", resolver); err == nil {
		t.Fatal("mixed public/private DNS answers were accepted")
	}
}

func TestPublicHTTPClientRechecksDNSBeforeDial(t *testing.T) {
	dialed := false
	client := newPublicHTTPClient(time.Second, fixedResolver{"relay.example": {netip.MustParseAddr("127.0.0.1")}}, func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, context.Canceled
	})
	request, _ := http.NewRequest(http.MethodGet, "http://relay.example/", nil)
	_, err := client.Do(request)
	if err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("client.Do() error = %v", err)
	}
	if dialed {
		t.Fatal("network dial occurred after DNS resolved to a restricted address")
	}
}

func TestPublicHTTPClientRejectsRedirectToPrivateAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, "http://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()
	serverAddress := strings.TrimPrefix(server.URL, "http://")
	dialer := &net.Dialer{Timeout: time.Second}
	client := newPublicHTTPClient(time.Second, fixedResolver{"relay.example": {netip.MustParseAddr("8.8.8.8")}}, func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, serverAddress)
	})
	request, _ := http.NewRequest(http.MethodGet, "http://relay.example/", nil)
	_, err := client.Do(request)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("private redirect error = %v", err)
	}
}
