package sitesync

import (
	"slices"

	"github.com/bestruirui/octopus/internal/model"
)

type CredentialField string

const (
	CredentialFieldAccessToken    CredentialField = "access_token"
	CredentialFieldRefreshToken   CredentialField = "refresh_token"
	CredentialFieldTokenExpiresAt CredentialField = "token_expires_at"
	CredentialFieldPlatformUserID CredentialField = "platform_user_id"
)

type ValidationProbe struct {
	Method            string   `json:"method"`
	Paths             []string `json:"paths"`
	RequiresUserID    bool     `json:"requires_user_id"`
	CompatibleFamily  string   `json:"compatible_family"`
	RuntimeValidation bool     `json:"runtime_validation"`
}

type RecoveryGuide struct {
	Title          string   `json:"title"`
	Steps          []string `json:"steps"`
	ManualFallback string   `json:"manual_fallback"`
}

type PlatformAuthCapability struct {
	Platform              model.SitePlatform       `json:"platform"`
	CompatibleFamily      string                   `json:"compatible_family"`
	RecommendedCredential model.SiteCredentialType `json:"recommended_credential"`
	RequiredFields        []CredentialField        `json:"required_fields"`
	ExtractableFields     []CredentialField        `json:"extractable_fields"`
	SupportsRefresh       bool                     `json:"supports_refresh"`
	ValidationProbe       ValidationProbe          `json:"validation_probe"`
	RecoveryGuide         RecoveryGuide            `json:"recovery_guide"`
}

var platformAuthCapabilities = map[model.SitePlatform]PlatformAuthCapability{
	model.SitePlatformNewAPI:  newAPICompatibleCapability(model.SitePlatformNewAPI),
	model.SitePlatformOneAPI:  newAPICompatibleCapability(model.SitePlatformOneAPI),
	model.SitePlatformOneHub:  newAPICompatibleCapability(model.SitePlatformOneHub),
	model.SitePlatformDoneHub: newAPICompatibleCapability(model.SitePlatformDoneHub),
	model.SitePlatformSub2API: {
		Platform:              model.SitePlatformSub2API,
		CompatibleFamily:      "sub2api",
		RecommendedCredential: model.SiteCredentialTypeAccessToken,
		RequiredFields:        []CredentialField{CredentialFieldAccessToken},
		ExtractableFields: []CredentialField{
			CredentialFieldAccessToken,
			CredentialFieldRefreshToken,
			CredentialFieldTokenExpiresAt,
		},
		SupportsRefresh: true,
		ValidationProbe: ValidationProbe{
			Method:            "GET",
			Paths:             []string{"/api/v1/profile", "/api/profile"},
			CompatibleFamily:  "sub2api",
			RuntimeValidation: true,
		},
		RecoveryGuide: RecoveryGuide{
			Title: "重新登录 Sub2API",
			Steps: []string{
				"在目标站点完成登录、验证码和二次验证。",
				"登录成功后返回扩展，提取当前授权信息。",
				"确认掩码摘要后再保存；刷新令牌会由 Octopus 受控轮换。",
			},
			ManualFallback: "如果站点不暴露可识别的授权信息，请在账号编辑页手动粘贴 access token 和 refresh token。",
		},
	},
	model.SitePlatformAnyRouter: {
		Platform:              model.SitePlatformAnyRouter,
		CompatibleFamily:      "anyrouter",
		RecommendedCredential: model.SiteCredentialTypeAccessToken,
		RequiredFields: []CredentialField{
			CredentialFieldAccessToken,
			CredentialFieldPlatformUserID,
		},
		ExtractableFields: []CredentialField{
			CredentialFieldAccessToken,
			CredentialFieldPlatformUserID,
		},
		SupportsRefresh: false,
		ValidationProbe: ValidationProbe{
			Method:            "GET",
			Paths:             []string{"/api/user/self", "/api/user/profile"},
			RequiresUserID:    true,
			CompatibleFamily:  "anyrouter",
			RuntimeValidation: true,
		},
		RecoveryGuide: RecoveryGuide{
			Title: "重新登录 AnyRouter",
			Steps: []string{
				"在目标站点完成登录和 Cloudflare 验证。",
				"扩展只读取目标域名的 session Cookie 和必要的用户 ID。",
				"确认掩码摘要与账号身份后再保存。",
			},
			ManualFallback: "如果站点接口已二次开发，请在账号编辑页手动粘贴 session 值和用户 ID；不要粘贴 Cloudflare/WAF Cookie。",
		},
	},
}

func newAPICompatibleCapability(platform model.SitePlatform) PlatformAuthCapability {
	return PlatformAuthCapability{
		Platform:              platform,
		CompatibleFamily:      "new-api",
		RecommendedCredential: model.SiteCredentialTypeAccessToken,
		RequiredFields: []CredentialField{
			CredentialFieldAccessToken,
			CredentialFieldPlatformUserID,
		},
		ExtractableFields: []CredentialField{
			CredentialFieldAccessToken,
			CredentialFieldPlatformUserID,
		},
		SupportsRefresh: false,
		ValidationProbe: ValidationProbe{
			Method:            "GET",
			Paths:             []string{"/api/user/self", "/api/user/token"},
			RequiresUserID:    true,
			CompatibleFamily:  "new-api",
			RuntimeValidation: true,
		},
		RecoveryGuide: RecoveryGuide{
			Title: "重新登录 NewAPI 兼容站点",
			Steps: []string{
				"在目标站点完成登录、验证码、二次验证和 Cloudflare 验证。",
				"打开个人设置或安全设置中的系统访问令牌页面。",
				"如果没有完整令牌，请手动创建或显示一次；扩展不会自动创建，也不会重复尝试。",
				"确认令牌掩码摘要和用户 ID 后再保存。",
			},
			ManualFallback: "如果页面不允许读取完整系统访问令牌，请复制令牌并在 Octopus 账号编辑页手动粘贴。",
		},
	}
}

func PlatformAuthCapabilityFor(platform model.SitePlatform) (PlatformAuthCapability, bool) {
	capability, ok := platformAuthCapabilities[platform]
	return clonePlatformAuthCapability(capability), ok
}

func PlatformAuthCapabilities() []PlatformAuthCapability {
	platforms := []model.SitePlatform{
		model.SitePlatformNewAPI,
		model.SitePlatformOneAPI,
		model.SitePlatformOneHub,
		model.SitePlatformDoneHub,
		model.SitePlatformSub2API,
		model.SitePlatformAnyRouter,
	}
	result := make([]PlatformAuthCapability, 0, len(platforms))
	for _, platform := range platforms {
		capability, _ := PlatformAuthCapabilityFor(platform)
		result = append(result, capability)
	}
	return result
}

func ValidateCredentialFields(platform model.SitePlatform, fields []CredentialField) error {
	capability, ok := PlatformAuthCapabilityFor(platform)
	if !ok {
		return newSitePlatformIncompatibleError(platform)
	}
	for _, field := range fields {
		if !slices.Contains(capability.ExtractableFields, field) {
			return newSitePlatformIncompatibleError(platform).WithParam("field", string(field))
		}
	}
	return nil
}

func clonePlatformAuthCapability(source PlatformAuthCapability) PlatformAuthCapability {
	source.RequiredFields = slices.Clone(source.RequiredFields)
	source.ExtractableFields = slices.Clone(source.ExtractableFields)
	source.ValidationProbe.Paths = slices.Clone(source.ValidationProbe.Paths)
	source.RecoveryGuide.Steps = slices.Clone(source.RecoveryGuide.Steps)
	return source
}
