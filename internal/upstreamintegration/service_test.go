package upstreamintegration

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupIntegrationServiceDB(t *testing.T) {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "upstream-integration.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
}

func createIntegrationSite(t *testing.T, platform model.SitePlatform, accounts ...model.SiteAccount) model.Site {
	return createIntegrationSiteWithTags(t, "site", platform, []string{model.SiteTagPaid}, accounts...)
}

func createIntegrationSiteWithTags(t *testing.T, name string, platform model.SitePlatform, tags []string, accounts ...model.SiteAccount) model.Site {
	t.Helper()
	site := model.Site{Name: name, Platform: platform, BaseURL: "https://" + name + ".example.com", Tags: tags, Enabled: true}
	if err := dbpkg.GetDB().Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	for i := range accounts {
		accounts[i].SiteID = site.ID
		if err := dbpkg.GetDB().Create(&accounts[i]).Error; err != nil {
			t.Fatalf("create account: %v", err)
		}
	}
	return site
}

func TestPreviewDefaultsOnlyPaidSitesWithCompleteCredentials(t *testing.T) {
	setupIntegrationServiceDB(t)
	expiry := time.Now().Add(time.Hour).UnixMilli()
	paid := createIntegrationSiteWithTags(t, "paid", model.SitePlatformSub2API, []string{model.SiteTagPaid},
		model.SiteAccount{Name: "paid-full", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "paid-at", RefreshToken: "paid-rt", TokenExpiresAt: expiry, Enabled: true},
	)
	public := createIntegrationSiteWithTags(t, "public", model.SitePlatformSub2API, []string{model.SiteTagPublic},
		model.SiteAccount{Name: "public-full", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "public-at", RefreshToken: "public-rt", TokenExpiresAt: expiry, Enabled: true},
	)

	preview, err := BuildPreview(t.Context())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	items := make(map[int]PreviewSite, len(preview.Items))
	for _, item := range preview.Items {
		items[item.SiteID] = item
	}
	paidItem := items[paid.ID]
	if !paidItem.DefaultSelected || paidItem.DefaultAccountID == nil || !paidItem.Accounts[0].DefaultSelected {
		t.Fatalf("paid site should be selected by default: %+v", paidItem)
	}
	publicItem := items[public.ID]
	if publicItem.DefaultSelected || publicItem.DefaultAccountID != nil || publicItem.Accounts[0].DefaultSelected {
		t.Fatalf("public site must require explicit selection: %+v", publicItem)
	}
}

func TestPreviewClassifiesSub2APICompletenessWithoutSecrets(t *testing.T) {
	setupIntegrationServiceDB(t)
	expiry := time.Now().Add(time.Hour).UnixMilli()
	createIntegrationSite(t, model.SitePlatformSub2API,
		model.SiteAccount{Name: "full", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "full-at-secret", RefreshToken: "full-rt-secret", TokenExpiresAt: expiry, Enabled: true},
		model.SiteAccount{Name: "no-expiry", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "degraded-at-secret", RefreshToken: "degraded-rt-secret", Enabled: true},
		model.SiteAccount{Name: "at-only", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "at-only-secret", Enabled: true},
	)
	preview, err := BuildPreview(t.Context())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	accounts := preview.Items[0].Accounts
	if accounts[0].CredentialState != CredentialStateComplete || !accounts[0].DefaultSelected || accounts[0].AuthOwner != AuthOwnerOctopus {
		t.Fatalf("full account classification = %+v", accounts[0])
	}
	if accounts[1].CredentialState != CredentialStateRefreshableDegraded || accounts[1].DefaultSelected || accounts[1].AuthOwner != AuthOwnerOctopus {
		t.Fatalf("refreshable degraded classification = %+v", accounts[1])
	}
	if accounts[2].CredentialState != CredentialStateNonrenewable || accounts[2].DefaultSelected || accounts[2].AuthOwner != AuthOwnerNone {
		t.Fatalf("at-only classification = %+v", accounts[2])
	}
	encoded, _ := json.Marshal(preview)
	for _, secret := range []string{"full-at-secret", "full-rt-secret", "degraded-at-secret", "degraded-rt-secret", "at-only-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("preview leaked %q: %s", secret, encoded)
		}
	}
}

func TestResolveSub2APINeverReturnsRefreshToken(t *testing.T) {
	setupIntegrationServiceDB(t)
	site := createIntegrationSite(t, model.SitePlatformSub2API,
		model.SiteAccount{Name: "full", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "access-secret", RefreshToken: "refresh-secret", TokenExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Enabled: true},
	)
	var account model.SiteAccount
	if err := dbpkg.GetDB().Where("site_id = ?", site.ID).First(&account).Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	resolved, err := Resolve(t.Context(), ResolveRequest{Items: []Selection{{SiteID: site.ID, AccountID: account.ID}}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	encoded, _ := json.Marshal(resolved)
	if !strings.Contains(string(encoded), "access-secret") || strings.Contains(string(encoded), "refresh-secret") || strings.Contains(string(encoded), "refresh_token") {
		t.Fatalf("resolve secret boundary violated: %s", encoded)
	}
	if resolved.Items[0].AuthOwner != AuthOwnerOctopus {
		t.Fatalf("auth owner = %q", resolved.Items[0].AuthOwner)
	}
}

func TestLeaseReturnsCurrentTokenWithoutConsumingRefreshTokenWhenHashIsStale(t *testing.T) {
	setupIntegrationServiceDB(t)
	site := createIntegrationSite(t, model.SitePlatformSub2API,
		model.SiteAccount{Name: "full", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "current-access", RefreshToken: "rotating-refresh-secret", TokenExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Enabled: true},
	)
	var account model.SiteAccount
	if err := dbpkg.GetDB().Where("site_id = ?", site.ID).First(&account).Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	lease, err := Lease(t.Context(), LeaseRequest{SiteID: site.ID, AccountID: account.ID, KnownAccessTokenHash: AccessTokenHash("older-access")})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	encoded, _ := json.Marshal(lease)
	if lease.AccessToken != "current-access" || !lease.Changed || strings.Contains(string(encoded), "refresh") {
		t.Fatalf("lease boundary violated: %+v JSON=%s", lease, encoded)
	}
}
