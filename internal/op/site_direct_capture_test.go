package op

import (
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func directCaptureInt(value int) *int { return &value }

func createDirectCaptureSite(t *testing.T, ctx context.Context, origin string) *model.Site {
	t.Helper()
	site := &model.Site{Name: "managed-" + origin, Platform: model.SitePlatformNewAPI, BaseURL: origin, Enabled: true}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate() error = %v", err)
	}
	return site
}

func createDirectCaptureAccount(t *testing.T, ctx context.Context, account model.SiteAccount) *model.SiteAccount {
	t.Helper()
	if err := SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate() error = %v", err)
	}
	return &account
}

func TestConfirmDirectCaptureCreatesSiteAndFirstAccountAtomically(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	identity := DirectCaptureIdentity{CanonicalOrigin: "https://new.example", Platform: model.SitePlatformNewAPI, PlatformUserID: directCaptureInt(42)}
	match, err := MatchDirectCapture(ctx, identity, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if match.Action != DirectCaptureActionCreateSite {
		t.Fatalf("action = %q", match.Action)
	}

	result, err := ConfirmDirectCapture(ctx, DirectCapturePersistInput{
		Match: *match, CanonicalOrigin: identity.CanonicalOrigin, Platform: identity.Platform,
		SiteName: "New Site", AccountName: "Admin", AccessToken: "ephemeral-test-token", PlatformUserID: identity.PlatformUserID,
	})
	if err != nil {
		t.Fatalf("ConfirmDirectCapture() error = %v", err)
	}
	if result.SiteID == 0 || result.AccountID == 0 {
		t.Fatalf("invalid result: %#v", result)
	}
	site, err := SiteGet(result.SiteID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if site.CanonicalOrigin == nil || *site.CanonicalOrigin != identity.CanonicalOrigin || len(site.Accounts) != 1 {
		t.Fatalf("created site = %#v", site)
	}
	account := site.Accounts[0]
	if account.AccessToken != "ephemeral-test-token" || account.PlatformUserID == nil || *account.PlatformUserID != 42 || !account.AutoSync {
		t.Fatalf("created account = %#v", account)
	}
}

func TestConfirmDirectCaptureMigratesCredentialAndPreservesDisabledState(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	site := createDirectCaptureSite(t, ctx, "https://disabled.example")
	account := createDirectCaptureAccount(t, ctx, model.SiteAccount{
		SiteID: site.ID, Name: "legacy", CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username: "legacy-user", Password: "legacy-password", Enabled: true,
	})
	if err := SiteEnabled(site.ID, false, ctx); err != nil {
		t.Fatal(err)
	}
	if err := SiteAccountEnabled(account.ID, false, ctx); err != nil {
		t.Fatal(err)
	}
	identity := DirectCaptureIdentity{CanonicalOrigin: "https://disabled.example", Platform: model.SitePlatformNewAPI, PlatformUserID: directCaptureInt(77)}
	match, err := MatchDirectCapture(ctx, identity, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if match.Action != DirectCaptureActionUpdateAccount || !match.CredentialMigration || match.SiteEnabled || match.AccountEnabled {
		t.Fatalf("match = %#v", match)
	}
	if _, err := ConfirmDirectCapture(ctx, DirectCapturePersistInput{
		Match: *match, CanonicalOrigin: identity.CanonicalOrigin, Platform: identity.Platform,
		AccessToken: "replacement-token", PlatformUserID: identity.PlatformUserID,
	}); err != nil {
		t.Fatal(err)
	}
	reloadedSite, _ := SiteGet(site.ID, ctx)
	reloaded, _ := SiteAccountGet(account.ID, ctx)
	if reloadedSite.Enabled || reloaded.Enabled {
		t.Fatal("disabled site/account was unexpectedly enabled")
	}
	if reloaded.CredentialType != model.SiteCredentialTypeAccessToken || reloaded.Username != "" || reloaded.Password != "" || reloaded.AccessToken != "replacement-token" {
		t.Fatalf("migrated account = %#v", reloaded)
	}
	if reloaded.AuthStatus != model.SiteAuthStatusValid || reloaded.PlatformUserID == nil || *reloaded.PlatformUserID != 77 {
		t.Fatalf("authentication state = %#v", reloaded)
	}
}

func TestMatchDirectCaptureCreatesNewAccountForDifferentPlatformUser(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	site := createDirectCaptureSite(t, ctx, "https://multi.example")
	createDirectCaptureAccount(t, ctx, model.SiteAccount{
		SiteID: site.ID, Name: "first", CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken: "first-token", PlatformUserID: directCaptureInt(1), Enabled: true,
	})
	identity := DirectCaptureIdentity{CanonicalOrigin: "https://multi.example", Platform: model.SitePlatformNewAPI, PlatformUserID: directCaptureInt(2)}
	match, err := MatchDirectCapture(ctx, identity, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if match.Action != DirectCaptureActionCreateAccount {
		t.Fatalf("action = %q, want create_account", match.Action)
	}
	result, err := ConfirmDirectCapture(ctx, DirectCapturePersistInput{
		Match: *match, CanonicalOrigin: identity.CanonicalOrigin, Platform: identity.Platform,
		AccountName: "second", AccessToken: "second-token", PlatformUserID: identity.PlatformUserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, _ := SiteAccountGet(result.AccountID, ctx)
	if created.PlatformUserID == nil || *created.PlatformUserID != 2 {
		t.Fatalf("created account = %#v", created)
	}
}

func TestMatchDirectCaptureExcludesAPIKeyAndRequiresResolutionForMultipleManagementAccounts(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	site := createDirectCaptureSite(t, ctx, "https://resolution.example")
	apiKey := createDirectCaptureAccount(t, ctx, model.SiteAccount{SiteID: site.ID, Name: "api", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "api-key", Enabled: true})
	first := createDirectCaptureAccount(t, ctx, model.SiteAccount{SiteID: site.ID, Name: "first", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "one", Enabled: true})
	createDirectCaptureAccount(t, ctx, model.SiteAccount{SiteID: site.ID, Name: "second", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "two", Enabled: true})

	match, err := MatchDirectCapture(ctx, DirectCaptureIdentity{CanonicalOrigin: "https://resolution.example", Platform: model.SitePlatformNewAPI}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !match.ResolutionRequired || len(match.AccountOptions) != 2 {
		t.Fatalf("match = %#v", match)
	}
	for _, option := range match.AccountOptions {
		if option.ID == apiKey.ID {
			t.Fatal("API key account was offered as a management account")
		}
	}
	selected, err := MatchDirectCapture(ctx, DirectCaptureIdentity{CanonicalOrigin: "https://resolution.example", Platform: model.SitePlatformNewAPI}, &first.ID, false)
	if err != nil || selected.AccountID != first.ID || selected.Action != DirectCaptureActionUpdateAccount {
		t.Fatalf("selected match = %#v, err = %v", selected, err)
	}
}

func TestConfirmDirectCaptureRejectsVersionConflictWithoutOverwritingCredential(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	site := createDirectCaptureSite(t, ctx, "https://conflict.example")
	account := createDirectCaptureAccount(t, ctx, model.SiteAccount{SiteID: site.ID, Name: "account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "original", Enabled: true})
	match, err := MatchDirectCapture(ctx, DirectCaptureIdentity{CanonicalOrigin: "https://conflict.example", Platform: model.SitePlatformNewAPI}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Where("id = ?", account.ID).Update("name", "changed-after-preview").Error; err != nil {
		t.Fatal(err)
	}
	_, err = ConfirmDirectCapture(ctx, DirectCapturePersistInput{
		Match: *match, CanonicalOrigin: "https://conflict.example", Platform: model.SitePlatformNewAPI, AccessToken: "must-not-win",
	})
	if !apperror.IsCode(err, "direct_capture.conflict") {
		t.Fatalf("error = %v, code = %q", err, apperror.Code(err))
	}
	reloaded, _ := SiteAccountGet(account.ID, ctx)
	if reloaded.Name != "changed-after-preview" || reloaded.AccessToken != "original" {
		t.Fatalf("newer account state was overwritten: name=%q token=%q", reloaded.Name, reloaded.AccessToken)
	}
}

func TestConfirmDirectCaptureUpdatesNameAndCredentialAndFeedsNextPreviewAndUI(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	site := createDirectCaptureSite(t, ctx, "https://rename.example")
	account := createDirectCaptureAccount(t, ctx, model.SiteAccount{
		SiteID: site.ID, Name: "Octopus 保存名", CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken: "old-token", PlatformUserID: directCaptureInt(88), Enabled: true,
	})
	identity := DirectCaptureIdentity{CanonicalOrigin: "https://rename.example", Platform: model.SitePlatformNewAPI, PlatformUserID: directCaptureInt(88)}
	match, err := MatchDirectCapture(ctx, identity, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("名", 128)
	if _, err := ConfirmDirectCapture(ctx, DirectCapturePersistInput{
		Match: *match, CanonicalOrigin: identity.CanonicalOrigin, Platform: identity.Platform,
		AccountName: name, AccessToken: "new-token", PlatformUserID: identity.PlatformUserID,
	}); err != nil {
		t.Fatalf("ConfirmDirectCapture() error = %v", err)
	}

	reloaded, err := SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Name != name || reloaded.AccessToken != "new-token" {
		t.Fatalf("account update was not atomic: name=%q token=%q", reloaded.Name, reloaded.AccessToken)
	}
	nextMatch, err := MatchDirectCapture(ctx, identity, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	channelView, err := SiteChannelAccountGet(site.ID, account.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nextMatch.AccountName != name || channelView.AccountName != name {
		t.Fatalf("name drifted across next preview/UI: preview=%q ui=%q", nextMatch.AccountName, channelView.AccountName)
	}
}

func TestConfirmDirectCaptureRejectsAccountNameOver128Characters(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	site := createDirectCaptureSite(t, ctx, "https://long-name.example")
	account := createDirectCaptureAccount(t, ctx, model.SiteAccount{SiteID: site.ID, Name: "original", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "old-token", Enabled: true})
	identity := DirectCaptureIdentity{CanonicalOrigin: "https://long-name.example", Platform: model.SitePlatformNewAPI}
	match, err := MatchDirectCapture(ctx, identity, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ConfirmDirectCapture(ctx, DirectCapturePersistInput{
		Match: *match, CanonicalOrigin: identity.CanonicalOrigin, Platform: identity.Platform,
		AccountName: strings.Repeat("a", 129), AccessToken: "must-not-win",
	})
	if !apperror.IsCode(err, "direct_capture.account_name.invalid") {
		t.Fatalf("error = %v, code = %q", err, apperror.Code(err))
	}
	reloaded, _ := SiteAccountGet(account.ID, ctx)
	if reloaded.Name != "original" || reloaded.AccessToken != "old-token" {
		t.Fatalf("invalid name changed the account: name=%q token=%q", reloaded.Name, reloaded.AccessToken)
	}
}

func TestSiteCreateEnforcesManagedCanonicalOriginButKeepsAPIPathsIndependent(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	first := &model.Site{Name: "first-managed", Platform: model.SitePlatformNewAPI, BaseURL: "HTTPS://Example.com:443/a", Enabled: true}
	if err := SiteCreate(first, ctx); err != nil {
		t.Fatal(err)
	}
	duplicate := &model.Site{Name: "second-managed", Platform: model.SitePlatformOneHub, BaseURL: "https://example.com/b", Enabled: true}
	if err := SiteCreate(duplicate, ctx); !apperror.IsCode(err, "site.origin.conflict") {
		t.Fatalf("managed duplicate error = %v, code = %q", err, apperror.Code(err))
	}

	apiOne := &model.Site{Name: "api-one", Platform: model.SitePlatformAPI, BaseURL: "https://api.example/v1", Enabled: true}
	apiTwo := &model.Site{Name: "api-two", Platform: model.SitePlatformAPI, BaseURL: "https://api.example/compatible/v1", Enabled: true}
	if err := SiteCreate(apiOne, ctx); err != nil {
		t.Fatal(err)
	}
	if err := SiteCreate(apiTwo, ctx); err != nil {
		t.Fatal(err)
	}
	if apiOne.CanonicalOrigin != nil || apiTwo.CanonicalOrigin != nil {
		t.Fatal("ordinary API sites unexpectedly received a canonical origin")
	}
}
