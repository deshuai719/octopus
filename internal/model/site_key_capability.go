package model

import "strings"

type SiteKeyCreateReasonCode string

const (
	SiteKeyCreateReasonPlatformNotSupported       SiteKeyCreateReasonCode = "platform_not_supported"
	SiteKeyCreateReasonGroupBindingNotSupported   SiteKeyCreateReasonCode = "group_binding_not_supported"
	SiteKeyCreateReasonAccountDisabled            SiteKeyCreateReasonCode = "account_disabled"
	SiteKeyCreateReasonCredentialAPIKeyReadOnly   SiteKeyCreateReasonCode = "credential_api_key_read_only"
	SiteKeyCreateReasonCredentialTypeNotSupported SiteKeyCreateReasonCode = "credential_type_not_supported"
	SiteKeyCreateReasonCredentialMissing          SiteKeyCreateReasonCode = "credential_missing"
	SiteKeyCreateReasonPlatformUserIDMissing      SiteKeyCreateReasonCode = "platform_user_id_missing"
	SiteKeyCreateReasonReauthRequired             SiteKeyCreateReasonCode = "reauth_required"
)

type SiteKeyCreateCapability struct {
	Supported       bool                    `json:"supported"`
	Writable        bool                    `json:"writable"`
	CanCreateSingle bool                    `json:"can_create_single"`
	CanCreateAll    bool                    `json:"can_create_all"`
	ReasonCode      SiteKeyCreateReasonCode `json:"reason_code,omitempty"`
	Reason          string                  `json:"reason,omitempty"`
}

func SiteKeyCreateCapabilityFor(site *Site, account *SiteAccount) SiteKeyCreateCapability {
	unsupported := func(code SiteKeyCreateReasonCode, reason string) SiteKeyCreateCapability {
		return SiteKeyCreateCapability{ReasonCode: code, Reason: reason}
	}
	supportedButUnavailable := func(code SiteKeyCreateReasonCode, reason string) SiteKeyCreateCapability {
		return SiteKeyCreateCapability{Supported: true, ReasonCode: code, Reason: reason}
	}

	if site == nil || account == nil {
		return unsupported(SiteKeyCreateReasonPlatformNotSupported, "站点或账号不存在，无法创建 Key")
	}

	switch site.Platform {
	case SitePlatformOneAPI:
		return unsupported(SiteKeyCreateReasonGroupBindingNotSupported, "One API 原版创建 Token 时不会保存目标分组，无法可靠创建分组 Key")
	case SitePlatformNewAPI, SitePlatformOneHub, SitePlatformDoneHub, SitePlatformAnyRouter, SitePlatformSub2API:
		// Supported platforms continue through the account-level checks below.
	default:
		return unsupported(SiteKeyCreateReasonPlatformNotSupported, "当前平台没有可验证的上游分组 Key 创建接口")
	}

	if !site.Enabled || !account.Enabled {
		return supportedButUnavailable(SiteKeyCreateReasonAccountDisabled, "当前站点账号已禁用，启用后才能创建 Key")
	}
	if account.AuthStatus == SiteAuthStatusReauthRequired {
		return supportedButUnavailable(SiteKeyCreateReasonReauthRequired, "当前账号需要重新认证后才能创建 Key")
	}
	if account.CredentialType == SiteCredentialTypeAPIKey {
		return supportedButUnavailable(SiteKeyCreateReasonCredentialAPIKeyReadOnly, "当前账号只有模型调用 API Key，不能调用上游管理写接口")
	}

	switch site.Platform {
	case SitePlatformSub2API:
		if account.CredentialType != SiteCredentialTypeAccessToken {
			return supportedButUnavailable(SiteKeyCreateReasonCredentialTypeNotSupported, "Sub2API 创建 Key 需要 access_token 凭据")
		}
		if strings.TrimSpace(account.AccessToken) == "" {
			return supportedButUnavailable(SiteKeyCreateReasonCredentialMissing, "当前账号缺少创建 Key 所需的 access_token")
		}
	default:
		switch account.CredentialType {
		case SiteCredentialTypeAccessToken:
			if strings.TrimSpace(account.AccessToken) == "" {
				return supportedButUnavailable(SiteKeyCreateReasonCredentialMissing, "当前账号缺少创建 Key 所需的 access_token")
			}
			if site.Platform == SitePlatformNewAPI && (account.PlatformUserID == nil || *account.PlatformUserID <= 0) {
				return supportedButUnavailable(SiteKeyCreateReasonPlatformUserIDMissing, "New API access_token 账号缺少平台用户 ID，请重新导入或认证")
			}
		case SiteCredentialTypeUsernamePassword:
			if strings.TrimSpace(account.Username) == "" || strings.TrimSpace(account.Password) == "" {
				return supportedButUnavailable(SiteKeyCreateReasonCredentialMissing, "当前账号缺少创建 Key 所需的用户名或密码")
			}
		default:
			return supportedButUnavailable(SiteKeyCreateReasonCredentialTypeNotSupported, "当前凭据类型不支持调用上游 Key 创建接口")
		}
	}

	return SiteKeyCreateCapability{
		Supported:       true,
		Writable:        true,
		CanCreateSingle: true,
		CanCreateAll:    true,
	}
}
