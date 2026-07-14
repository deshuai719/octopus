package sitesync

import (
	"net/http"
	"strings"
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
