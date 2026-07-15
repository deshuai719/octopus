package upstreamintegration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/sitesync"
	"gorm.io/gorm"
)

type CredentialState string

const (
	CredentialStateComplete            CredentialState = "complete"
	CredentialStateRefreshableDegraded CredentialState = "refreshable_degraded"
	CredentialStateNonrenewable        CredentialState = "nonrenewable"
	CredentialStatePassword            CredentialState = "password"
	CredentialStateUnavailable         CredentialState = "unavailable"

	AuthOwnerOctopus  = "octopus"
	AuthOwnerUpstream = "upstream"
	AuthOwnerNone     = "none"
)

var (
	ErrSiteNotFound          = errors.New("integration site not found")
	ErrAccountNotFound       = errors.New("integration site account not found")
	ErrUnsupportedPlatform   = errors.New("integration site platform is unsupported")
	ErrCredentialUnavailable = errors.New("integration credential is unavailable")
)

type Preview struct {
	Items []PreviewSite `json:"items"`
}

type PreviewSite struct {
	SiteID           int                `json:"site_id"`
	Name             string             `json:"name"`
	BaseURL          string             `json:"base_url"`
	Platform         model.SitePlatform `json:"platform"`
	Tags             []string           `json:"tags,omitempty"`
	Enabled          bool               `json:"enabled"`
	Archived         bool               `json:"archived"`
	Supported        bool               `json:"supported"`
	SupportMessage   string             `json:"support_message,omitempty"`
	DefaultAccountID *int               `json:"default_account_id,omitempty"`
	DefaultSelected  bool               `json:"default_selected"`
	Accounts         []PreviewAccount   `json:"accounts"`
}

type PreviewAccount struct {
	AccountID       int                        `json:"account_id"`
	Name            string                     `json:"name"`
	CredentialType  model.SiteCredentialType   `json:"credential_type"`
	Enabled         bool                       `json:"enabled"`
	AutoSync        bool                       `json:"auto_sync"`
	HasCredential   bool                       `json:"has_credential"`
	CredentialState CredentialState            `json:"credential_state"`
	FieldsPresent   []sitesync.CredentialField `json:"fields_present"`
	SupportsRefresh bool                       `json:"supports_refresh"`
	AuthOwner       string                     `json:"auth_owner"`
	ImportMode      string                     `json:"import_mode,omitempty"`
	MaskedSummary   string                     `json:"masked_summary,omitempty"`
	Warning         string                     `json:"warning,omitempty"`
	SkipReason      string                     `json:"skip_reason,omitempty"`
	DefaultSelected bool                       `json:"default_selected"`
}

type ResolveRequest struct {
	Items []Selection `json:"items"`
}

type Selection struct {
	SiteID    int `json:"site_id"`
	AccountID int `json:"account_id"`
}

type ResolveResponse struct {
	Items []ResolvedAccount `json:"items"`
}

type ResolvedAccount struct {
	SiteID          int                      `json:"site_id"`
	AccountID       int                      `json:"account_id"`
	SiteName        string                   `json:"site_name"`
	AccountName     string                   `json:"account_name"`
	BaseURL         string                   `json:"base_url"`
	Platform        model.SitePlatform       `json:"platform"`
	CredentialType  model.SiteCredentialType `json:"credential_type"`
	CredentialState CredentialState          `json:"credential_state"`
	AuthOwner       string                   `json:"auth_owner"`
	Username        string                   `json:"username,omitempty"`
	Password        string                   `json:"password,omitempty"`
	AccessToken     string                   `json:"access_token,omitempty"`
	TokenExpiresAt  int64                    `json:"token_expires_at,omitempty"`
	PlatformUserID  *int                     `json:"platform_user_id,omitempty"`
	Warning         string                   `json:"warning,omitempty"`
}

type LeaseRequest struct {
	SiteID               int    `json:"site_id"`
	AccountID            int    `json:"account_id"`
	KnownAccessTokenHash string `json:"known_access_token_hash,omitempty"`
	ForceRefresh         bool   `json:"force_refresh,omitempty"`
}

type LeaseResponse struct {
	SiteID         int    `json:"site_id"`
	AccountID      int    `json:"account_id"`
	AccessToken    string `json:"access_token"`
	TokenExpiresAt int64  `json:"token_expires_at,omitempty"`
	Changed        bool   `json:"changed"`
	AuthOwner      string `json:"auth_owner"`
}

func BuildPreview(ctx context.Context) (Preview, error) {
	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).
		Preload("Accounts", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Where("archived = ?", false).
		Order("is_pinned DESC, sort_order ASC, id ASC").
		Find(&sites).Error; err != nil {
		return Preview{}, fmt.Errorf("load integration preview sites: %w", err)
	}
	preview := Preview{Items: make([]PreviewSite, 0, len(sites))}
	for _, site := range sites {
		preview.Items = append(preview.Items, previewSite(site))
	}
	return preview, nil
}

func Resolve(ctx context.Context, request ResolveRequest) (ResolveResponse, error) {
	if len(request.Items) == 0 {
		return ResolveResponse{}, fmt.Errorf("%w: no selections", ErrCredentialUnavailable)
	}
	response := ResolveResponse{Items: make([]ResolvedAccount, 0, len(request.Items))}
	for _, selection := range request.Items {
		site, account, err := loadSiteAccount(ctx, selection.SiteID, selection.AccountID)
		if err != nil {
			return ResolveResponse{}, err
		}
		if site.Archived || !site.Enabled || !account.Enabled {
			return ResolveResponse{}, fmt.Errorf("%w: site or account is disabled", ErrCredentialUnavailable)
		}
		resolved, err := resolveAccount(*site, *account)
		if err != nil {
			return ResolveResponse{}, err
		}
		response.Items = append(response.Items, resolved)
	}
	return response, nil
}

func Lease(ctx context.Context, request LeaseRequest) (LeaseResponse, error) {
	site, account, err := loadSiteAccount(ctx, request.SiteID, request.AccountID)
	if err != nil {
		return LeaseResponse{}, err
	}
	if site.Archived || !site.Enabled || !account.Enabled {
		return LeaseResponse{}, fmt.Errorf("%w: site or account is disabled", ErrCredentialUnavailable)
	}
	if site.Platform != model.SitePlatformSub2API {
		return LeaseResponse{}, ErrUnsupportedPlatform
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		return LeaseResponse{}, fmt.Errorf("%w: sub2api refresh token is missing", ErrCredentialUnavailable)
	}
	lease, err := sitesync.LeaseSub2APIAuth(ctx, site, account, request.KnownAccessTokenHash, request.ForceRefresh)
	if err != nil {
		return LeaseResponse{}, err
	}
	return LeaseResponse{
		SiteID:         site.ID,
		AccountID:      account.ID,
		AccessToken:    lease.AccessToken,
		TokenExpiresAt: lease.TokenExpiresAt,
		Changed:        lease.Changed,
		AuthOwner:      AuthOwnerOctopus,
	}, nil
}

func AccessTokenHash(accessToken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(accessToken, "Bearer "))))
	return fmt.Sprintf("%x", sum)
}

func previewSite(site model.Site) PreviewSite {
	supported := supportsPlatform(site.Platform)
	defaultEligible := supported && site.Enabled && !site.Archived && hasSiteTag(site.Tags, model.SiteTagPaid)
	item := PreviewSite{
		SiteID:    site.ID,
		Name:      site.Name,
		BaseURL:   site.BaseURL,
		Platform:  site.Platform,
		Tags:      append([]string(nil), site.Tags...),
		Enabled:   site.Enabled,
		Archived:  site.Archived,
		Supported: supported,
		Accounts:  make([]PreviewAccount, 0, len(site.Accounts)),
	}
	if !supported {
		item.SupportMessage = "upstream-hub 暂不支持此平台"
	}
	for _, account := range site.Accounts {
		preview := classifyAccount(site.Platform, account)
		preview.DefaultSelected = defaultEligible && account.Enabled && preview.CredentialState == CredentialStateComplete
		item.Accounts = append(item.Accounts, preview)
		if item.DefaultAccountID == nil && preview.DefaultSelected {
			id := account.ID
			item.DefaultAccountID = &id
			item.DefaultSelected = true
		}
	}
	return item
}

func hasSiteTag(tags []string, expected string) bool {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == expected {
			return true
		}
	}
	return false
}

func classifyAccount(platform model.SitePlatform, account model.SiteAccount) PreviewAccount {
	fields := presentCredentialFields(account)
	preview := PreviewAccount{
		AccountID:      account.ID,
		Name:           account.Name,
		CredentialType: account.CredentialType,
		Enabled:        account.Enabled,
		AutoSync:       account.AutoSync,
		FieldsPresent:  fields,
		AuthOwner:      AuthOwnerNone,
	}
	if !supportsPlatform(platform) {
		preview.CredentialState = CredentialStateUnavailable
		preview.SkipReason = "平台暂不支持"
		return preview
	}

	if account.CredentialType == model.SiteCredentialTypeUsernamePassword || strings.TrimSpace(account.AccessToken) == "" {
		if strings.TrimSpace(account.Username) != "" && strings.TrimSpace(account.Password) != "" {
			preview.HasCredential = true
			preview.CredentialState = CredentialStateComplete
			preview.AuthOwner = AuthOwnerUpstream
			preview.ImportMode = "password"
			preview.MaskedSummary = "账号密码"
			return preview
		}
		preview.CredentialState = CredentialStateUnavailable
		preview.SkipReason = "缺少可用的 Access Token 或账号密码"
		return preview
	}

	if account.CredentialType != model.SiteCredentialTypeAccessToken {
		preview.CredentialState = CredentialStateUnavailable
		preview.SkipReason = "凭据类型不支持"
		return preview
	}

	preview.HasCredential = true
	preview.ImportMode = "token"
	switch platform {
	case model.SitePlatformSub2API:
		hasRT := strings.TrimSpace(account.RefreshToken) != ""
		hasExpiry := account.TokenExpiresAt > 0
		preview.SupportsRefresh = hasRT
		if hasRT {
			preview.AuthOwner = AuthOwnerOctopus
		}
		switch {
		case hasRT && hasExpiry:
			preview.CredentialState = CredentialStateComplete
			preview.MaskedSummary = "Access Token + Refresh Token + 过期时间"
		case hasRT:
			preview.CredentialState = CredentialStateRefreshableDegraded
			preview.MaskedSummary = "Access Token + Refresh Token"
			preview.Warning = "缺少过期时间，不会默认勾选；导入后由 Octopus 续期"
		default:
			preview.CredentialState = CredentialStateNonrenewable
			preview.MaskedSummary = "Access Token"
			if hasExpiry {
				preview.MaskedSummary += " + 过期时间"
			}
			preview.Warning = "缺少 Refresh Token，失效后需要重新授权"
		}
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformOneHub, model.SitePlatformDoneHub:
		if account.PlatformUserID == nil {
			preview.HasCredential = false
			preview.CredentialState = CredentialStateUnavailable
			preview.SkipReason = "Access Token 导入需要 Platform User ID"
			return preview
		}
		preview.CredentialState = CredentialStateComplete
		preview.MaskedSummary = "Access Token + User ID"
	}
	return preview
}

func resolveAccount(site model.Site, account model.SiteAccount) (ResolvedAccount, error) {
	if !supportsPlatform(site.Platform) {
		return ResolvedAccount{}, ErrUnsupportedPlatform
	}
	preview := classifyAccount(site.Platform, account)
	if !preview.HasCredential || preview.CredentialState == CredentialStateUnavailable {
		return ResolvedAccount{}, fmt.Errorf("%w: %s", ErrCredentialUnavailable, preview.SkipReason)
	}
	resolved := ResolvedAccount{
		SiteID:          site.ID,
		AccountID:       account.ID,
		SiteName:        site.Name,
		AccountName:     account.Name,
		BaseURL:         site.BaseURL,
		Platform:        site.Platform,
		CredentialType:  account.CredentialType,
		CredentialState: preview.CredentialState,
		AuthOwner:       preview.AuthOwner,
		Warning:         preview.Warning,
	}
	if preview.ImportMode == "password" {
		resolved.CredentialType = model.SiteCredentialTypeUsernamePassword
		resolved.Username = strings.TrimSpace(account.Username)
		resolved.Password = strings.TrimSpace(account.Password)
		return resolved, nil
	}
	resolved.AccessToken = strings.TrimSpace(strings.TrimPrefix(account.AccessToken, "Bearer "))
	resolved.TokenExpiresAt = account.TokenExpiresAt
	resolved.PlatformUserID = account.PlatformUserID
	return resolved, nil
}

func presentCredentialFields(account model.SiteAccount) []sitesync.CredentialField {
	fields := make([]sitesync.CredentialField, 0, 4)
	if strings.TrimSpace(account.AccessToken) != "" {
		fields = append(fields, sitesync.CredentialFieldAccessToken)
	}
	if strings.TrimSpace(account.RefreshToken) != "" {
		fields = append(fields, sitesync.CredentialFieldRefreshToken)
	}
	if account.TokenExpiresAt > 0 {
		fields = append(fields, sitesync.CredentialFieldTokenExpiresAt)
	}
	if account.PlatformUserID != nil {
		fields = append(fields, sitesync.CredentialFieldPlatformUserID)
	}
	return fields
}

func supportsPlatform(platform model.SitePlatform) bool {
	switch platform {
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformOneHub, model.SitePlatformDoneHub, model.SitePlatformSub2API:
		return true
	default:
		return false
	}
}

func loadSiteAccount(ctx context.Context, siteID, accountID int) (*model.Site, *model.SiteAccount, error) {
	var site model.Site
	if err := db.GetDB().WithContext(ctx).First(&site, siteID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrSiteNotFound
		}
		return nil, nil, fmt.Errorf("load integration site: %w", err)
	}
	var account model.SiteAccount
	if err := db.GetDB().WithContext(ctx).Where("id = ? AND site_id = ?", accountID, siteID).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrAccountNotFound
		}
		return nil, nil, fmt.Errorf("load integration site account: %w", err)
	}
	return &site, &account, nil
}
