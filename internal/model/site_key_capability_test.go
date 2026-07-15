package model

import "testing"

func TestSiteKeyCreateCapabilityFor(t *testing.T) {
	userID := 7
	tests := []struct {
		name       string
		platform   SitePlatform
		account    SiteAccount
		supported  bool
		writable   bool
		reasonCode SiteKeyCreateReasonCode
	}{
		{name: "new api", platform: SitePlatformNewAPI, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token", PlatformUserID: &userID}, supported: true, writable: true},
		{name: "new api missing user id", platform: SitePlatformNewAPI, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token"}, supported: true, reasonCode: SiteKeyCreateReasonPlatformUserIDMissing},
		{name: "one api", platform: SitePlatformOneAPI, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token"}, reasonCode: SiteKeyCreateReasonGroupBindingNotSupported},
		{name: "one hub", platform: SitePlatformOneHub, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeUsernamePassword, Username: "user", Password: "pass"}, supported: true, writable: true},
		{name: "done hub", platform: SitePlatformDoneHub, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token"}, supported: true, writable: true},
		{name: "done hub api key", platform: SitePlatformDoneHub, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAPIKey, APIKey: "key"}, supported: true, reasonCode: SiteKeyCreateReasonCredentialAPIKeyReadOnly},
		{name: "anyrouter", platform: SitePlatformAnyRouter, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token"}, supported: true, writable: true},
		{name: "sub2api", platform: SitePlatformSub2API, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token"}, supported: true, writable: true},
		{name: "sub2api password", platform: SitePlatformSub2API, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeUsernamePassword, Username: "user", Password: "pass"}, supported: true, reasonCode: SiteKeyCreateReasonCredentialTypeNotSupported},
		{name: "direct api", platform: SitePlatformAPI, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAPIKey, APIKey: "key"}, reasonCode: SiteKeyCreateReasonPlatformNotSupported},
		{name: "disabled", platform: SitePlatformDoneHub, account: SiteAccount{Enabled: false, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token"}, supported: true, reasonCode: SiteKeyCreateReasonAccountDisabled},
		{name: "reauth required", platform: SitePlatformDoneHub, account: SiteAccount{Enabled: true, CredentialType: SiteCredentialTypeAccessToken, AccessToken: "token", AuthStatus: SiteAuthStatusReauthRequired}, supported: true, reasonCode: SiteKeyCreateReasonReauthRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capability := SiteKeyCreateCapabilityFor(&Site{Platform: tt.platform, Enabled: true}, &tt.account)
			if capability.Supported != tt.supported || capability.Writable != tt.writable {
				t.Fatalf("capability = %+v, want supported=%t writable=%t", capability, tt.supported, tt.writable)
			}
			if capability.CanCreateSingle != tt.writable || capability.CanCreateAll != tt.writable {
				t.Fatalf("create flags = single:%t all:%t, want %t", capability.CanCreateSingle, capability.CanCreateAll, tt.writable)
			}
			if capability.ReasonCode != tt.reasonCode {
				t.Fatalf("reason code = %q, want %q", capability.ReasonCode, tt.reasonCode)
			}
			if !tt.writable && capability.Reason == "" {
				t.Fatal("unavailable capability must include a display reason")
			}
		})
	}
}
