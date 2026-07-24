package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/siteauth"
	"gorm.io/gorm"
)

const (
	DirectCaptureActionCreateSite    = "create_site_account"
	DirectCaptureActionCreateAccount = "create_account"
	DirectCaptureActionUpdateAccount = "update_account"
)

type DirectCaptureIdentity struct {
	CanonicalOrigin string
	Platform        model.SitePlatform
	PlatformUserID  *int
}

type DirectCaptureAccountOption struct {
	ID             int                      `json:"id"`
	Name           string                   `json:"name"`
	CredentialType model.SiteCredentialType `json:"credential_type"`
	PlatformUserID *int                     `json:"platform_user_id,omitempty"`
	Enabled        bool                     `json:"enabled"`
}

type DirectCaptureMatch struct {
	Action              string                       `json:"action"`
	SiteID              int                          `json:"site_id,omitempty"`
	SiteName            string                       `json:"site_name"`
	SiteArchived        bool                         `json:"site_archived"`
	SiteEnabled         bool                         `json:"site_enabled"`
	AccountID           int                          `json:"account_id,omitempty"`
	AccountName         string                       `json:"account_name"`
	AccountEnabled      bool                         `json:"account_enabled"`
	CredentialMigration bool                         `json:"credential_migration"`
	ResolutionRequired  bool                         `json:"resolution_required"`
	AccountOptions      []DirectCaptureAccountOption `json:"account_options,omitempty"`
	SiteTags            []string                     `json:"site_tags,omitempty"`
	SiteVersion         string                       `json:"-"`
	AccountVersion      string                       `json:"-"`
}

type DirectCapturePersistInput struct {
	Match           DirectCaptureMatch
	CanonicalOrigin string
	Platform        model.SitePlatform
	SiteName        string
	AccountName     string
	AccessToken     string
	RefreshToken    string
	TokenExpiresAt  int64
	PlatformUserID  *int
	UserAgent       string
	AddTags         []string
}

type DirectCapturePersistResult struct {
	Action    string `json:"action"`
	SiteID    int    `json:"site_id"`
	AccountID int    `json:"account_id"`
}

func MatchDirectCapture(ctx context.Context, identity DirectCaptureIdentity, selectedAccountID *int, createNew bool) (*DirectCaptureMatch, error) {
	var site model.Site
	err := db.GetDB().WithContext(ctx).Preload("Accounts").Where("canonical_origin = ?", identity.CanonicalOrigin).First(&site).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &DirectCaptureMatch{
			Action:         DirectCaptureActionCreateSite,
			SiteName:       hostNameForDirectCapture(identity.CanonicalOrigin),
			SiteEnabled:    true,
			AccountName:    "默认账号",
			AccountEnabled: true,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	if site.Platform != identity.Platform {
		return nil, directCaptureOpError("site.origin.conflict", "site origin exists with an incompatible platform", http.StatusConflict, "site_match", false, "resolve_origin_conflict")
	}

	match := &DirectCaptureMatch{
		SiteID:       site.ID,
		SiteName:     site.Name,
		SiteArchived: site.Archived,
		SiteEnabled:  site.Enabled,
		SiteVersion:  directCaptureSiteVersion(&site),
		AccountName:  "默认账号",
		SiteTags:     append([]string(nil), site.Tags...),
	}
	management := make([]model.SiteAccount, 0, len(site.Accounts))
	for _, account := range site.Accounts {
		if account.CredentialType != model.SiteCredentialTypeAPIKey {
			management = append(management, account)
			match.AccountOptions = append(match.AccountOptions, directCaptureAccountOption(account))
		}
	}
	if selectedAccountID != nil {
		for i := range management {
			if management[i].ID == *selectedAccountID {
				if identity.PlatformUserID != nil && management[i].PlatformUserID != nil && *identity.PlatformUserID != *management[i].PlatformUserID {
					return nil, directCaptureOpError("direct_capture.identity.conflict", "selected account belongs to a different platform user", http.StatusConflict, "account_match", false, "choose_account")
				}
				return applyDirectCaptureAccountMatch(match, &management[i]), nil
			}
		}
		return nil, directCaptureOpError("direct_capture.account.invalid", "selected account is not a management account on this site", http.StatusBadRequest, "account_match", false, "choose_account")
	}
	if createNew {
		match.Action = DirectCaptureActionCreateAccount
		match.AccountEnabled = true
		return match, nil
	}
	if identity.PlatformUserID != nil {
		for i := range management {
			if management[i].PlatformUserID != nil && *management[i].PlatformUserID == *identity.PlatformUserID {
				return applyDirectCaptureAccountMatch(match, &management[i]), nil
			}
		}
		if len(management) == 1 && management[0].PlatformUserID == nil {
			return applyDirectCaptureAccountMatch(match, &management[0]), nil
		}
		match.Action = DirectCaptureActionCreateAccount
		match.AccountEnabled = true
		return match, nil
	}
	if len(management) == 1 {
		return applyDirectCaptureAccountMatch(match, &management[0]), nil
	}
	if len(management) == 0 {
		match.Action = DirectCaptureActionCreateAccount
		match.AccountEnabled = true
		return match, nil
	}
	match.ResolutionRequired = true
	return match, nil
}

func applyDirectCaptureAccountMatch(match *DirectCaptureMatch, account *model.SiteAccount) *DirectCaptureMatch {
	match.Action = DirectCaptureActionUpdateAccount
	match.AccountID = account.ID
	match.AccountName = account.Name
	match.AccountEnabled = account.Enabled
	match.AccountVersion = directCaptureAccountVersion(account)
	match.CredentialMigration = account.CredentialType == model.SiteCredentialTypeUsernamePassword
	return match
}

func ConfirmDirectCapture(ctx context.Context, input DirectCapturePersistInput) (*DirectCapturePersistResult, error) {
	accountName, err := normalizeDirectCaptureAccountName(firstNonEmptyString(input.AccountName, input.Match.AccountName))
	if err != nil {
		return nil, err
	}
	input.AccountName = accountName
	input.AddTags = model.NormalizeSiteTags(input.AddTags)
	if err := model.ValidateSiteTags(input.AddTags); err != nil {
		return nil, err
	}

	var result DirectCapturePersistResult
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		switch input.Match.Action {
		case DirectCaptureActionCreateSite:
			var count int64
			if err := tx.Model(&model.Site{}).Where("canonical_origin = ?", input.CanonicalOrigin).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return directCaptureOpError("site.origin.conflict", "site origin was created after preview", http.StatusConflict, "persistence", false, "restart_capture")
			}
			site := model.Site{
				Name:             uniqueSiteName(tx, firstNonEmptyString(input.SiteName, hostNameForDirectCapture(input.CanonicalOrigin))),
				Platform:         input.Platform,
				BaseURL:          input.CanonicalOrigin,
				CanonicalOrigin:  &input.CanonicalOrigin,
				Enabled:          true,
				ProxyMode:        model.ProxyUsageModeDirect,
				GlobalWeight:     1,
				DefaultRouteType: model.SiteModelRouteTypeOpenAIChat,
				Tags:             append([]string(nil), input.AddTags...),
				CustomHeader:     upsertSiteUserAgentHeader(nil, input.UserAgent),
			}
			if input.Platform == model.SitePlatformSub2API {
				site.ProxyMode = model.ProxyUsageModeSystem
			}
			if err := site.Validate(); err != nil {
				return err
			}
			if err := tx.Create(&site).Error; err != nil {
				return normalizeDirectCapturePersistError(err)
			}
			account := newDirectCaptureAccount(site.ID, input, now)
			if err := tx.Create(&account).Error; err != nil {
				return err
			}
			result = DirectCapturePersistResult{Action: input.Match.Action, SiteID: site.ID, AccountID: account.ID}
			return nil

		case DirectCaptureActionCreateAccount, DirectCaptureActionUpdateAccount:
			var site model.Site
			if err := tx.First(&site, input.Match.SiteID).Error; err != nil {
				return directCaptureOpError("direct_capture.conflict", "site changed after preview", http.StatusConflict, "persistence", false, "restart_capture")
			}
			if directCaptureSiteVersion(&site) != input.Match.SiteVersion || site.CanonicalOrigin == nil || *site.CanonicalOrigin != input.CanonicalOrigin || site.Platform != input.Platform {
				return directCaptureOpError("direct_capture.conflict", "site changed after preview", http.StatusConflict, "persistence", false, "restart_capture")
			}
			if site.Archived {
				if err := tx.Model(&model.Site{}).Where("id = ?", site.ID).Updates(map[string]any{"archived": false, "archived_at": nil}).Error; err != nil {
					return err
				}
			}
			if len(input.AddTags) > 0 {
				merged := model.NormalizeSiteTags(append(append([]string(nil), site.Tags...), input.AddTags...))
				if err := model.ValidateSiteTags(merged); err != nil {
					return err
				}
				if err := tx.Model(&model.Site{}).Where("id = ?", site.ID).Select("tags").Updates(&model.Site{Tags: merged}).Error; err != nil {
					return err
				}
			}
			if strings.TrimSpace(input.UserAgent) != "" {
				uaHeaders := upsertSiteUserAgentHeader(site.CustomHeader, input.UserAgent)
				if err := tx.Model(&model.Site{}).Where("id = ?", site.ID).Select("custom_header").Updates(&model.Site{CustomHeader: uaHeaders}).Error; err != nil {
					return err
				}
				site.CustomHeader = uaHeaders
			}
			if input.Platform == model.SitePlatformSub2API && site.ProxyMode == model.ProxyUsageModeDirect {
				if err := tx.Model(&model.Site{}).Where("id = ?", site.ID).Update("proxy_mode", model.ProxyUsageModeSystem).Error; err != nil {
					return err
				}
			}
			if input.Match.Action == DirectCaptureActionCreateAccount {
				account := newDirectCaptureAccount(site.ID, input, now)
				if err := tx.Create(&account).Error; err != nil {
					return err
				}
				result = DirectCapturePersistResult{Action: input.Match.Action, SiteID: site.ID, AccountID: account.ID}
				return nil
			}
			var account model.SiteAccount
			if err := tx.First(&account, input.Match.AccountID).Error; err != nil || account.SiteID != site.ID || account.CredentialType == model.SiteCredentialTypeAPIKey {
				return directCaptureOpError("direct_capture.conflict", "account changed after preview", http.StatusConflict, "persistence", false, "restart_capture")
			}
			if directCaptureAccountVersion(&account) != input.Match.AccountVersion {
				return directCaptureOpError("direct_capture.conflict", "account changed after preview", http.StatusConflict, "persistence", false, "restart_capture")
			}
			updates := siteauth.SuccessUpdates(now)
			updates["name"] = input.AccountName
			updates["credential_type"] = model.SiteCredentialTypeAccessToken
			updates["username"] = ""
			updates["password"] = ""
			updates["access_token"] = input.AccessToken
			updates["api_key"] = ""
			updates["refresh_token"] = input.RefreshToken
			updates["token_expires_at"] = input.TokenExpiresAt
			updates["platform_user_id"] = input.PlatformUserID
			if err := tx.Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(updates).Error; err != nil {
				return err
			}
			result = DirectCapturePersistResult{Action: input.Match.Action, SiteID: site.ID, AccountID: account.ID}
			return nil
		default:
			return directCaptureOpError("direct_capture.action.invalid", "direct capture action is invalid", http.StatusBadRequest, "persistence", false, "restart_capture")
		}
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func newDirectCaptureAccount(siteID int, input DirectCapturePersistInput, now time.Time) model.SiteAccount {
	return model.SiteAccount{
		SiteID:               siteID,
		Name:                 firstNonEmptyString(input.AccountName, "默认账号"),
		CredentialType:       model.SiteCredentialTypeAccessToken,
		AccessToken:          input.AccessToken,
		RefreshToken:         input.RefreshToken,
		TokenExpiresAt:       input.TokenExpiresAt,
		PlatformUserID:       cloneDirectCaptureInt(input.PlatformUserID),
		ProxyMode:            model.ProxyUsageModeInherit,
		Enabled:              true,
		AutoSync:             true,
		AutoCheckin:          platformSupportsCheckin(input.Platform),
		CheckinIntervalHours: 24,
		AuthStatus:           model.SiteAuthStatusValid,
		LastAuthSuccessAt:    &now,
	}
}

func directCaptureAccountOption(account model.SiteAccount) DirectCaptureAccountOption {
	return DirectCaptureAccountOption{ID: account.ID, Name: account.Name, CredentialType: account.CredentialType, PlatformUserID: cloneDirectCaptureInt(account.PlatformUserID), Enabled: account.Enabled}
}

func directCaptureSiteVersion(site *model.Site) string {
	if site == nil {
		return ""
	}
	origin := ""
	if site.CanonicalOrigin != nil {
		origin = *site.CanonicalOrigin
	}
	return directCaptureVersion(string(site.Platform), origin, strconv.FormatBool(site.Archived), strconv.FormatBool(site.Enabled), site.Name)
}

func directCaptureAccountVersion(account *model.SiteAccount) string {
	if account == nil {
		return ""
	}
	userID := ""
	if account.PlatformUserID != nil {
		userID = strconv.Itoa(*account.PlatformUserID)
	}
	return directCaptureVersion(strconv.Itoa(account.SiteID), account.Name, string(account.CredentialType), account.Username, account.Password, account.AccessToken, account.APIKey, account.RefreshToken, strconv.FormatInt(account.TokenExpiresAt, 10), userID, strconv.FormatBool(account.Enabled))
}

func normalizeDirectCaptureAccountName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", directCaptureOpError("direct_capture.account_name.invalid", "account name must not be blank", http.StatusBadRequest, "confirmation", false, "edit_account_name")
	}
	if len([]rune(value)) > 128 {
		return "", directCaptureOpError("direct_capture.account_name.invalid", "account name must not exceed 128 characters", http.StatusBadRequest, "confirmation", false, "edit_account_name")
	}
	return value, nil
}

func directCaptureVersion(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func hostNameForDirectCapture(origin string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	if index := strings.IndexByte(trimmed, ':'); index > 0 && !strings.HasPrefix(trimmed, "[") {
		trimmed = trimmed[:index]
	}
	return strings.Trim(trimmed, "[]")
}

func cloneDirectCaptureInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func normalizeDirectCapturePersistError(err error) error {
	if normalized := normalizeSiteOriginConflictError(err); normalized != err {
		return normalized
	}
	return err
}

func directCaptureOpError(code, message string, status int, stage string, retryable bool, action string) *apperror.Error {
	return apperror.New(code, message).WithStatus(status).WithStage(stage).WithRetryable(retryable).WithSuggestedAction(action)
}

func upsertSiteUserAgentHeader(headers []model.CustomHeader, userAgent string) []model.CustomHeader {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return headers
	}
	out := make([]model.CustomHeader, 0, len(headers)+1)
	found := false
	for _, item := range headers {
		key := strings.TrimSpace(item.HeaderKey)
		if strings.EqualFold(key, "User-Agent") {
			if found {
				continue
			}
			out = append(out, model.CustomHeader{HeaderKey: "User-Agent", HeaderValue: userAgent})
			found = true
			continue
		}
		if key == "" {
			continue
		}
		out = append(out, model.CustomHeader{HeaderKey: key, HeaderValue: item.HeaderValue})
	}
	if !found {
		out = append(out, model.CustomHeader{HeaderKey: "User-Agent", HeaderValue: userAgent})
	}
	return out
}
