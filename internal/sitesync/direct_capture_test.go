package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
)

func TestClassifyDirectCaptureStatus(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want model.SitePlatform
	}{
		{name: "new api", data: map[string]any{"quota_display_type": "USD", "passkey_login": true, "setup": true}, want: model.SitePlatformNewAPI},
		{name: "done hub", data: map[string]any{"linuxDo_oauth": true, "user_agreement_enabled": true, "max_log_query_days": 30}, want: model.SitePlatformDoneHub},
		{name: "one hub", data: map[string]any{"oidc_auth": true, "language": "zh", "EnableSafe": true, "UptimeDomain": ""}, want: model.SitePlatformOneHub},
		{name: "one api", data: map[string]any{"oidc": true, "oidc_well_known": "", "oidc_token_endpoint": ""}, want: model.SitePlatformOneAPI},
		{name: "generic family is inconclusive", data: map[string]any{"version": "1", "system_name": "custom"}, want: ""},
		{name: "conflicting schemas are inconclusive", data: map[string]any{"quota_display_type": "USD", "passkey_login": true, "setup": true, "linuxDo_oauth": true, "user_agreement_enabled": true, "max_log_query_days": 30}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyDirectCaptureStatus(map[string]any{"success": true, "data": test.data})
			if got != test.want {
				t.Fatalf("classifyDirectCaptureStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDirectCaptureHTTPClientRejectsCredentialRedirectAcrossOrigin(t *testing.T) {
	client := directCaptureHTTPClient("https://relay.example")
	request, err := http.NewRequest(http.MethodGet, "https://other.example/api/user/self", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, nil); err == nil || !strings.Contains(err.Error(), "changed origin") {
		t.Fatalf("cross-origin redirect error = %v", err)
	}
}

func TestProbeDirectCaptureStatusPlatformRecognizesDoneHubVariant(t *testing.T) {
	var groupRequests atomic.Int32
	var unsafeGroupRequest atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"linuxDo_oauth":true,"max_log_query_days":30}}`))
		case "/api/user_group_map":
			groupRequests.Add(1)
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				unsafeGroupRequest.Store(true)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"default":{"name":"default","symbol":"default","ratio":1,"dynamic_ratio":false}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	platform, err := probeDirectCaptureStatusPlatformWithClient(context.Background(), server.URL, server.Client())
	if err != nil {
		t.Fatalf("probeDirectCaptureStatusPlatformWithClient() error = %v", err)
	}
	if platform != model.SitePlatformDoneHub {
		t.Fatalf("platform = %q, want %q", platform, model.SitePlatformDoneHub)
	}
	if got := groupRequests.Load(); got != 1 {
		t.Fatalf("groupRequests = %d, want 1", got)
	}
	if unsafeGroupRequest.Load() {
		t.Fatal("public group probe used a write method or carried credentials")
	}
}

func TestProbeDirectCaptureStatusPlatformRejectsMalformedDoneHubVariant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"linuxDo_oauth":true,"max_log_query_days":30}}`))
		case "/api/user_group_map":
			_, _ = w.Write([]byte(`{"success":true,"data":{"default":{"name":"default"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	platform, err := probeDirectCaptureStatusPlatformWithClient(context.Background(), server.URL, server.Client())
	if err != nil {
		t.Fatalf("probeDirectCaptureStatusPlatformWithClient() error = %v", err)
	}
	if platform != "" {
		t.Fatalf("platform = %q, want inconclusive", platform)
	}
}

func TestProbeDirectCaptureStatusPlatformRejectsUnsuccessfulDoneHubStatus(t *testing.T) {
	groupRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":false,"data":{"linuxDo_oauth":true,"max_log_query_days":30}}`))
		case "/api/user_group_map":
			groupRequests++
			_, _ = w.Write([]byte(`{"success":true,"data":{"default":{"name":"default","symbol":"default","ratio":1}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	platform, err := probeDirectCaptureStatusPlatformWithClient(context.Background(), server.URL, server.Client())
	if err != nil {
		t.Fatalf("probeDirectCaptureStatusPlatformWithClient() error = %v", err)
	}
	if platform != "" {
		t.Fatalf("platform = %q, want inconclusive", platform)
	}
	if groupRequests != 0 {
		t.Fatalf("groupRequests = %d, want 0", groupRequests)
	}
}

func TestProbeDirectCaptureProfileRequiresAuthenticatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/self" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := probeDirectCaptureProfileWithClient(
		context.Background(),
		server.URL,
		model.SitePlatformDoneHub,
		"candidate-token",
		nil,
		[]string{"/api/user/self"},
		server.Client(),
	)
	if !apperror.IsCode(err, "direct_capture.auth.invalid") {
		t.Fatalf("error code = %q, want direct_capture.auth.invalid", apperror.Code(err))
	}
}

func TestProbeDirectCaptureProfileContinuesAfterOversizedEndpoint(t *testing.T) {
	var selfRequests atomic.Int32
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self":
			selfRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"padding":"` + strings.Repeat("x", 64*1024) + `"}}`))
		case "/api/user/token":
			tokenRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":719,"username":"tester"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	profile, err := probeDirectCaptureProfileWithClient(
		context.Background(),
		server.URL,
		model.SitePlatformDoneHub,
		"candidate-token",
		nil,
		[]string{"/api/user/self", "/api/user/token"},
		server.Client(),
	)
	if err != nil {
		t.Fatalf("probeDirectCaptureProfileWithClient() error = %v", err)
	}
	if profile.Path != "/api/user/token" {
		t.Fatalf("profile path = %q, want fallback /api/user/token", profile.Path)
	}
	if got := selfRequests.Load(); got != 1 {
		t.Fatalf("self requests = %d, want 1", got)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestValidateDirectCaptureEvidenceRequiresStrongOrTwoMediumSignals(t *testing.T) {
	tests := []struct {
		name     string
		platform model.SitePlatform
		evidence []DirectCaptureEvidence
		wantCode string
	}{
		{
			name:     "new api structural signature",
			platform: model.SitePlatformNewAPI,
			evidence: []DirectCaptureEvidence{{Code: "browser.strong.status_schema.new-api"}},
		},
		{
			name:     "done hub status and group structural signature",
			platform: model.SitePlatformDoneHub,
			evidence: []DirectCaptureEvidence{{Code: "browser.strong.status_group_schema.done-hub"}},
		},
		{
			name:     "brand is weak",
			platform: model.SitePlatformOneHub,
			evidence: []DirectCaptureEvidence{{Code: "browser.weak.brand.one-hub"}},
			wantCode: "platform.variant.inconclusive",
		},
		{
			name:     "sub2api two medium signals",
			platform: model.SitePlatformSub2API,
			evidence: []DirectCaptureEvidence{{Code: "browser.medium.storage.sub2api_token_pair"}, {Code: "browser.medium.auth_profile.sub2api"}},
		},
		{
			name:     "sub2api one medium signal",
			platform: model.SitePlatformSub2API,
			evidence: []DirectCaptureEvidence{{Code: "browser.medium.storage.sub2api_token_pair"}},
			wantCode: "platform.variant.inconclusive",
		},
		{
			name:     "explicit conflict",
			platform: model.SitePlatformNewAPI,
			evidence: []DirectCaptureEvidence{{Code: "browser.conflict.status_schema.new-api"}},
			wantCode: "platform.evidence.conflict",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateDirectCaptureEvidence(test.platform, test.evidence)
			if test.wantCode == "" && err != nil {
				t.Fatalf("validateDirectCaptureEvidence() error = %v", err)
			}
			if test.wantCode != "" && !apperror.IsCode(err, test.wantCode) {
				t.Fatalf("error code = %q, want %q", apperror.Code(err), test.wantCode)
			}
		})
	}
}
