package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestManagementRemoteTokenUpdatePreservesExistingFieldsAndDeleteUsesExternalID(t *testing.T) {
	userID := 73
	updated := false
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer managed-token" || r.Header.Get("New-API-User") != "73" {
			t.Fatalf("unexpected managed auth headers")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/91":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":91,"name":"old","group":"default","unlimited_quota":true,"expired_time":-1,"allow_ips":"127.0.0.1"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/token/":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if body["name"] != "付费低倍率" || body["group"] != "vip" || body["allow_ips"] != "127.0.0.1" || body["unlimited_quota"] != true {
				t.Fatalf("update did not preserve and merge fields: %+v", body)
			}
			updated = true
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":91}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/token/91":
			deleted = true
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	siteRecord := &model.Site{Platform: model.SitePlatformNewAPI, BaseURL: server.URL}
	account := &model.SiteAccount{CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "managed-token", PlatformUserID: &userID}
	token := &model.SiteToken{ExternalID: 91}
	name := "付费低倍率"
	groupKey := "vip"
	if err := updateManagementPlatformToken(context.Background(), siteRecord, account, token, model.SiteRemoteKeyUpdateRequest{Name: &name, GroupKey: &groupKey}); err != nil {
		t.Fatalf("updateManagementPlatformToken: %v", err)
	}
	if err := deleteManagementPlatformToken(context.Background(), siteRecord, account, token); err != nil {
		t.Fatalf("deleteManagementPlatformToken: %v", err)
	}
	if !updated || !deleted {
		t.Fatalf("expected update and delete calls, updated=%v deleted=%v", updated, deleted)
	}
}

func TestSub2APIRemoteTokenUpdateAndDelete(t *testing.T) {
	updated := false
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer sub2api-token" {
			t.Fatalf("unexpected authorization %q", r.Header.Get("Authorization"))
		}
		switch r.Method {
		case http.MethodPut:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if body["name"] != "低倍率" || body["group_id"] != float64(7) {
				t.Fatalf("unexpected Sub2API update body: %+v", body)
			}
			updated = true
			_, _ = w.Write([]byte(`{"data":{"id":31}}`))
		case http.MethodDelete:
			deleted = true
			_, _ = w.Write([]byte(`{"data":{"message":"deleted"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	siteRecord := &model.Site{Platform: model.SitePlatformSub2API, BaseURL: server.URL}
	account := &model.SiteAccount{CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "sub2api-token"}
	token := &model.SiteToken{ExternalID: 31}
	name := "低倍率"
	groupKey := "7"
	if err := updateSub2APIToken(context.Background(), siteRecord, account, token, model.SiteRemoteKeyUpdateRequest{Name: &name, GroupKey: &groupKey}); err != nil {
		t.Fatalf("updateSub2APIToken: %v", err)
	}
	if err := deleteSub2APIToken(context.Background(), siteRecord, account, token); err != nil {
		t.Fatalf("deleteSub2APIToken: %v", err)
	}
	if !updated || !deleted {
		t.Fatalf("expected update and delete calls, updated=%v deleted=%v", updated, deleted)
	}
}

func TestSub2APIRemoteTokenDeleteAcceptsSuccessfulEnvelopeMessage(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/api-keys/31" {
			t.Fatalf("unexpected Sub2API delete request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"删除成功"}`))
	}))
	defer server.Close()

	siteRecord := &model.Site{Platform: model.SitePlatformSub2API, BaseURL: server.URL}
	account := &model.SiteAccount{CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "sub2api-token"}
	token := &model.SiteToken{ExternalID: 31}
	if err := deleteSub2APIToken(context.Background(), siteRecord, account, token); err != nil {
		t.Fatalf("expected successful Sub2API envelope to confirm deletion, got %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one DELETE without compatibility replay, got %d", requestCount)
	}
}
