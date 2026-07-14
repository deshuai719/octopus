package sitesync

import (
	"context"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

type accountAuthFailureTransition struct {
	Status      model.SiteAuthStatus
	Failures    int
	ShouldWrite bool
}

func nextAccountAuthFailureState(account *model.SiteAccount, err error) accountAuthFailureTransition {
	if account == nil || err == nil {
		return accountAuthFailureTransition{}
	}

	code := apperror.Code(err)
	statusCode := siteErrorStatusCode(err)
	failures := account.ConsecutiveAuthFailures + 1

	switch code {
	case CodeSiteAuthCredentialRevoked, CodeSiteAuthRefreshTerminal, CodeSiteAuthReauthRequired:
		return accountAuthFailureTransition{Status: model.SiteAuthStatusReauthRequired, Failures: failures, ShouldWrite: true}
	case CodeSiteAuthCredentialExpired:
		if strings.TrimSpace(account.RefreshToken) != "" || failures >= 2 {
			return accountAuthFailureTransition{Status: model.SiteAuthStatusReauthRequired, Failures: failures, ShouldWrite: true}
		}
		return accountAuthFailureTransition{Status: model.SiteAuthStatusSuspectedExpired, Failures: failures, ShouldWrite: true}
	case CodeSiteAuthRefreshRetryable,
		CodeSiteUpstreamPermissionDenied,
		CodeSiteUpstreamRateLimited,
		CodeSiteUpstreamNetworkError,
		CodeSiteUpstreamServerError,
		CodeSiteUpstreamCloudflareChallenge:
		return accountAuthFailureTransition{}
	}

	if statusCode == 401 {
		if strings.TrimSpace(account.RefreshToken) != "" || failures >= 2 {
			return accountAuthFailureTransition{Status: model.SiteAuthStatusReauthRequired, Failures: failures, ShouldWrite: true}
		}
		return accountAuthFailureTransition{Status: model.SiteAuthStatusSuspectedExpired, Failures: failures, ShouldWrite: true}
	}
	return accountAuthFailureTransition{}
}

func recordAccountAuthFailure(ctx context.Context, account *model.SiteAccount, stage string, err error) error {
	transition := nextAccountAuthFailureState(account, err)
	if !transition.ShouldWrite {
		return nil
	}
	now := time.Now()
	message := sanitizeSiteStatusMessage(err)
	if message == "" {
		message = "site authentication failed"
	}
	stage = strings.TrimSpace(stage)
	if len(stage) > 32 {
		stage = stage[:32]
	}

	database := db.GetDB()
	if database == nil {
		return apperror.New(apperror.CodeCommonDatabaseError, "database is not initialized")
	}
	updates := map[string]any{
		"auth_status":               transition.Status,
		"auth_failure_code":         apperror.Code(err),
		"auth_failure_message":      message,
		"auth_failure_stage":        stage,
		"consecutive_auth_failures": transition.Failures,
		"last_auth_failure_at":      &now,
	}
	if err := database.WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(updates).Error; err != nil {
		return err
	}
	account.AuthStatus = transition.Status
	account.AuthFailureCode = apperror.Code(err)
	account.AuthFailureMessage = message
	account.AuthFailureStage = stage
	account.ConsecutiveAuthFailures = transition.Failures
	account.LastAuthFailureAt = &now
	return nil
}

func recordAccountAuthSuccess(ctx context.Context, account *model.SiteAccount) error {
	if account == nil {
		return nil
	}
	database := db.GetDB()
	if database == nil {
		return apperror.New(apperror.CodeCommonDatabaseError, "database is not initialized")
	}
	now := time.Now()
	if err := database.WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(accountAuthSuccessUpdates(now)).Error; err != nil {
		return err
	}
	applyAccountAuthSuccess(account, now)
	return nil
}

func accountAuthSuccessUpdates(now time.Time) map[string]any {
	return map[string]any{
		"auth_status":               model.SiteAuthStatusValid,
		"auth_failure_code":         "",
		"auth_failure_message":      "",
		"auth_failure_stage":        "",
		"consecutive_auth_failures": 0,
		"last_auth_success_at":      &now,
		"reauth_notified_at":        nil,
	}
}

func applyAccountAuthSuccess(account *model.SiteAccount, now time.Time) {
	if account == nil {
		return
	}
	account.AuthStatus = model.SiteAuthStatusValid
	account.AuthFailureCode = ""
	account.AuthFailureMessage = ""
	account.AuthFailureStage = ""
	account.ConsecutiveAuthFailures = 0
	account.LastAuthSuccessAt = &now
	account.ReauthNotifiedAt = nil
}
