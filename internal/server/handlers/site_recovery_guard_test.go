package handlers

import (
	"testing"
	"time"
)

func TestRecoveryCandidateLimiterResetsAfterWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := newRecoveryCandidateLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.allow("client"); !allowed {
		t.Fatal("first request should be allowed")
	}
	if allowed, _ := limiter.allow("client"); !allowed {
		t.Fatal("second request should be allowed")
	}
	if allowed, retryAfter := limiter.allow("client"); allowed || retryAfter != 60 {
		t.Fatalf("third request = allowed %v, retryAfter %d; want false, 60", allowed, retryAfter)
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.allow("client"); !allowed {
		t.Fatal("request after the fixed window should be allowed")
	}
}

func TestRecoveryCandidateLimiterKeepsClientsIndependent(t *testing.T) {
	limiter := newRecoveryCandidateLimiter(1, time.Minute)
	limiter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	if allowed, _ := limiter.allow("client-a"); !allowed {
		t.Fatal("first client request should be allowed")
	}
	if allowed, _ := limiter.allow("client-a"); allowed {
		t.Fatal("second client-a request should be limited")
	}
	if allowed, _ := limiter.allow("client-b"); !allowed {
		t.Fatal("client-b should have an independent window")
	}
}

func TestRecoveryRateLimitKeyIgnoresEphemeralPort(t *testing.T) {
	if got := recoveryRateLimitKey("203.0.113.7:54321"); got != "203.0.113.7" {
		t.Fatalf("IPv4 rate-limit key = %q", got)
	}
	if got := recoveryRateLimitKey("[2001:db8::1]:443"); got != "2001:db8::1" {
		t.Fatalf("IPv6 rate-limit key = %q", got)
	}
}
