package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestFetchManagementTokensRevealsMaskedValuesByExternalID(t *testing.T) {
	userID := 73
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer managed-token" || r.Header.Get("New-API-User") != "73" {
			t.Fatalf("unexpected managed headers: Authorization=%q New-API-User=%q", r.Header.Get("Authorization"), r.Header.Get("New-API-User"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":91,"name":"公益组","key":"abcd********wxyz","group":"public","status":1}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/batch/keys":
			var body struct {
				IDs []int64 `json:"ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode batch reveal request: %v", err)
			}
			if !reflect.DeepEqual(body.IDs, []int64{91}) {
				t.Fatalf("unexpected reveal ids: %+v", body.IDs)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"keys":{"91":"abcd-real-secret-wxyz"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokens, err := fetchManagementTokens(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "managed-token",
		PlatformUserID: &userID,
	}, "managed-token")
	if err != nil {
		t.Fatalf("fetchManagementTokens returned error: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected one token, got %+v", tokens)
	}
	if tokens[0].ExternalID != 91 || tokens[0].Token != "abcd-real-secret-wxyz" || tokens[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("unexpected revealed token: %+v", tokens[0])
	}
}

func TestMergePersistedSiteTokensMatchesStableExternalID(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            8,
		SiteAccountID: 3,
		ExternalID:    91,
		Name:          "old-name",
		Token:         "old-full-value",
		GroupKey:      "old-group",
		Enabled:       false,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "sync",
	}}
	incoming := []model.SiteToken{{
		ExternalID:  91,
		Name:        "new-name",
		Token:       "new-full-value",
		GroupKey:    "new-group",
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusReady,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(3, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected one merged token, got %+v", merged)
	}
	if merged[0].ExternalID != 91 || merged[0].Token != "new-full-value" || merged[0].GroupKey != "new-group" {
		t.Fatalf("expected incoming remote state to win for stable external id, got %+v", merged[0])
	}
	if merged[0].Enabled {
		t.Fatalf("expected local enabled preference to be preserved")
	}
}

func TestNormalizeSiteTokenCreateNameUsesReadableBoundedGroupName(t *testing.T) {
	if got := normalizeSiteTokenCreateName("  公益 / 高延迟  "); got != "公益 - 高延迟" {
		t.Fatalf("unexpected normalized name %q", got)
	}
	longName := "这是一个很长的分组名称用于验证密钥名称会按字符而不是按字节安全截断并且不会带上任何奇怪的前后缀或时间戳"
	got := normalizeSiteTokenCreateName(longName)
	if len([]rune(got)) != 50 {
		t.Fatalf("expected 50 runes, got %d in %q", len([]rune(got)), got)
	}
	if got == "" || got[:1] == "-" {
		t.Fatalf("unexpected bounded name %q", got)
	}
}
