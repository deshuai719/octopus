package sitesync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"golang.org/x/sync/singleflight"
)

const sub2APIAccessTokenRefreshLead = 5 * time.Minute

type sub2APIRefreshedCredentials struct {
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt int64
}

var sub2APIRefreshGroup singleflight.Group

func ensureFreshSub2APIAccessToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, forceRefresh bool) (string, error) {
	if account == nil {
		return "", fmt.Errorf("site account is nil")
	}

	accessToken := stripBearerPrefix(account.AccessToken)
	if accessToken == "" {
		return "", newAccessTokenRequiredError()
	}
	if !forceRefresh && !shouldProactivelyRefreshSub2API(account) {
		return accessToken, nil
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		if forceRefresh {
			return "", newSiteReauthRequiredError("sub2api refresh token is missing")
		}
		return accessToken, nil
	}

	return refreshSub2APIManagedSession(ctx, siteRecord, account, accessToken)
}

func shouldProactivelyRefreshSub2API(account *model.SiteAccount) bool {
	if account == nil {
		return false
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	if account.TokenExpiresAt <= 0 {
		return false
	}
	return time.Until(time.UnixMilli(account.TokenExpiresAt)) <= sub2APIAccessTokenRefreshLead
}

func shouldRetrySub2APIAfterRefresh(err error, account *model.SiteAccount) bool {
	if err == nil || account == nil || strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	if siteErrorStatusCode(err) == 401 {
		return true
	}
	switch apperror.Code(err) {
	case CodeSiteAuthCredentialExpired, CodeSiteAuthReauthRequired:
		return true
	default:
		return false
	}
}

func refreshSub2APIManagedSession(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, currentAccessToken string) (string, error) {
	if siteRecord == nil || account == nil {
		return "", fmt.Errorf("site or account is nil")
	}
	refreshToken := strings.TrimSpace(account.RefreshToken)
	if refreshToken == "" {
		return "", newSiteReauthRequiredError("sub2api refresh token is missing")
	}

	key := sub2APIRefreshKey(account.ID, refreshToken)
	resultCh := sub2APIRefreshGroup.DoChan(key, func() (any, error) {
		return refreshSub2APIManagedSessionOnce(ctx, siteRecord, account, currentAccessToken, refreshToken)
	})

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			return "", result.Err
		}
		refreshed, ok := result.Val.(sub2APIRefreshedCredentials)
		if !ok {
			return "", apperror.New(CodeSiteAuthRefreshTerminal, "sub2api token refresh returned an invalid internal result")
		}
		account.AccessToken = refreshed.AccessToken
		account.RefreshToken = refreshed.RefreshToken
		account.TokenExpiresAt = refreshed.TokenExpiresAt
		return refreshed.AccessToken, nil
	}
}

func sub2APIRefreshKey(accountID int, refreshToken string) string {
	if accountID > 0 {
		return "account:" + strconv.Itoa(accountID)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(refreshToken)))
	return fmt.Sprintf("refresh:%x", sum[:16])
}

func refreshSub2APIManagedSessionOnce(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, currentAccessToken string, expectedRefreshToken string) (sub2APIRefreshedCredentials, error) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if currentAccessToken = stripBearerPrefix(currentAccessToken); currentAccessToken != "" {
		headers["Authorization"] = ensureBearer(currentAccessToken)
	}

	payload, err := requestJSON(
		ctx,
		siteRecord,
		"POST",
		buildSiteURL(siteRecord.BaseURL, "/api/v1/auth/refresh"),
		map[string]any{"refresh_token": expectedRefreshToken},
		headers,
		account,
	)
	if err != nil {
		return sub2APIRefreshedCredentials{}, wrapSub2APIRefreshError(err)
	}

	refreshed, ok := parseSub2APIRefreshPayload(payload)
	if !ok {
		message := firstNonEmptyString(extractSiteResponseMessage(payload), "sub2api token refresh response did not contain complete credentials")
		return sub2APIRefreshedCredentials{}, apperror.New(CodeSiteAuthRefreshTerminal, sanitizeSiteStatusText(message)).
			WithParam("retryable", false).
			WithParam("stage", "refresh")
	}

	return persistSub2APIRefreshedCredentials(ctx, account.ID, expectedRefreshToken, refreshed)
}

func persistSub2APIRefreshedCredentials(ctx context.Context, accountID int, expectedRefreshToken string, refreshed sub2APIRefreshedCredentials) (sub2APIRefreshedCredentials, error) {
	if accountID <= 0 {
		return refreshed, nil
	}
	database := db.GetDB()
	if database == nil {
		return sub2APIRefreshedCredentials{}, fmt.Errorf("database is not initialized")
	}

	result := database.WithContext(ctx).
		Model(&model.SiteAccount{}).
		Where("id = ? AND refresh_token = ?", accountID, expectedRefreshToken).
		Updates(map[string]any{
			"access_token":     refreshed.AccessToken,
			"refresh_token":    refreshed.RefreshToken,
			"token_expires_at": refreshed.TokenExpiresAt,
		})
	if result.Error != nil {
		return sub2APIRefreshedCredentials{}, fmt.Errorf("failed to persist sub2api refreshed session: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return refreshed, nil
	}

	var current model.SiteAccount
	if err := database.WithContext(ctx).
		Select("access_token", "refresh_token", "token_expires_at").
		First(&current, accountID).Error; err != nil {
		return sub2APIRefreshedCredentials{}, fmt.Errorf("failed to reload concurrently refreshed sub2api session: %w", err)
	}
	currentCredentials := sub2APIRefreshedCredentials{
		AccessToken:    stripBearerPrefix(current.AccessToken),
		RefreshToken:   strings.TrimSpace(current.RefreshToken),
		TokenExpiresAt: current.TokenExpiresAt,
	}
	if currentCredentials.AccessToken == "" || currentCredentials.RefreshToken == "" {
		return sub2APIRefreshedCredentials{}, newSiteReauthRequiredError("sub2api credentials changed during refresh and are no longer usable")
	}
	return currentCredentials, nil
}

func wrapSub2APIRefreshError(err error) *apperror.Error {
	retryable := false
	switch apperror.Code(err) {
	case CodeSiteUpstreamNetworkError, CodeSiteUpstreamRateLimited, CodeSiteUpstreamServerError:
		retryable = true
	}
	if params := apperror.Params(err); params != nil {
		if value, ok := params["retryable"].(bool); ok && value {
			retryable = true
		}
	}
	code := CodeSiteAuthRefreshTerminal
	if retryable {
		code = CodeSiteAuthRefreshRetryable
	}
	wrapped := apperror.Wrap(code, "sub2api token refresh failed", err).
		WithStatus(apperror.Status(err)).
		WithParam("retryable", retryable).
		WithParam("stage", "refresh")
	if statusCode := siteErrorStatusCode(err); statusCode > 0 {
		wrapped.WithParam("statusCode", statusCode)
	}
	return wrapped
}

func parseSub2APIRefreshPayload(payload map[string]any) (sub2APIRefreshedCredentials, bool) {
	if payload == nil {
		return sub2APIRefreshedCredentials{}, false
	}

	if rawCode, ok := payload["code"]; ok {
		code := anyToInt64(rawCode)
		if code != 0 {
			return sub2APIRefreshedCredentials{}, false
		}
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		return sub2APIRefreshedCredentials{}, false
	}

	accessToken := stripBearerPrefix(jsonString(data["access_token"]))
	refreshToken := strings.TrimSpace(jsonString(data["refresh_token"]))
	expiresInSeconds := anyToInt64(data["expires_in"])
	if accessToken == "" || refreshToken == "" || expiresInSeconds <= 0 {
		return sub2APIRefreshedCredentials{}, false
	}

	return sub2APIRefreshedCredentials{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenExpiresAt: time.Now().Add(time.Duration(expiresInSeconds) * time.Second).UnixMilli(),
	}, true
}

func anyToInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		var parsed int64
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
