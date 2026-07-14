package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
)

func TestDetectPlatformDetailedGenericStatusRequiresConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"name":"unrelated"}}`))
			return
		}
		_, _ = w.Write([]byte(`<html><title>Portal</title></html>`))
	}))
	defer server.Close()

	result, err := DetectPlatformDetailed(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DetectPlatformDetailed() error = %v", err)
	}
	if result.Platform != model.SitePlatformNewAPI || !result.RequiresConfirmation {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Confidence != detectionConfidenceLow {
		t.Fatalf("confidence = %v, want %v", result.Confidence, detectionConfidenceLow)
	}
}

func TestDetectPlatformDetailedStatusKeywordIsHighConfidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"system_name":"Sub2API"}}`))
			return
		}
		_, _ = w.Write([]byte(`<html><title>Portal</title></html>`))
	}))
	defer server.Close()

	result, err := DetectPlatformDetailed(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DetectPlatformDetailed() error = %v", err)
	}
	if result.Platform != model.SitePlatformSub2API || result.RequiresConfirmation {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Confidence < detectionConfidenceHigh {
		t.Fatalf("confidence = %v, want >= %v", result.Confidence, detectionConfidenceHigh)
	}
}

func TestDetectPlatformDetailedCloudflareIsNotPlatform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("CF-Ray", "test-ray")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html><title>Just a moment...</title><p>Cloudflare</p></html>`))
	}))
	defer server.Close()

	_, err := DetectPlatformDetailed(context.Background(), server.URL)
	if got := apperror.Code(err); got != CodeSiteUpstreamCloudflareChallenge {
		t.Fatalf("error code = %q, want %q (err=%v)", got, CodeSiteUpstreamCloudflareChallenge, err)
	}
}

func TestDetectPlatformDetailedLoginPageIsInconclusive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`<html><title>Login</title></html>`))
	}))
	defer server.Close()

	_, err := DetectPlatformDetailed(context.Background(), server.URL)
	if got := apperror.Code(err); got != CodeSitePlatformDetectInconclusive {
		t.Fatalf("error code = %q, want %q (err=%v)", got, CodeSitePlatformDetectInconclusive, err)
	}
}

func TestPlatformAuthCapabilityRejectsUnknownField(t *testing.T) {
	err := ValidateCredentialFields(model.SitePlatformAnyRouter, []CredentialField{CredentialFieldRefreshToken})
	if got := apperror.Code(err); got != CodeSitePlatformIncompatible {
		t.Fatalf("error code = %q, want %q", got, CodeSitePlatformIncompatible)
	}
}
