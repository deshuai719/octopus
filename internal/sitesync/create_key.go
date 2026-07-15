package sitesync

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func CreateAccountToken(ctx context.Context, siteID int, accountID int, req model.SiteChannelKeyCreateRequest) (*model.SiteKeyCreateResult, error) {
	siteRecord, account, err := loadWritableKeyCreateAccount(ctx, siteID, accountID)
	if err != nil {
		return nil, err
	}

	groupKey, err := normalizeRequestedGroupKey(req.GroupKey)
	if err != nil {
		return nil, err
	}
	if _, err := SyncAccount(ctx, accountID); err != nil && !isMissingKeySyncError(err) {
		return nil, fmt.Errorf("failed to refresh site account before key creation: %w", err)
	}

	siteRecord, account, err = loadWritableKeyCreateAccount(ctx, siteID, accountID)
	if err != nil {
		return nil, err
	}
	accountView, err := op.SiteChannelAccountGet(siteID, accountID, ctx)
	if err != nil {
		return nil, err
	}
	group, ok := findSiteChannelGroup(accountView.Groups, groupKey)
	if !ok {
		return nil, fmt.Errorf("site group %s not found after refresh", groupKey)
	}
	if group.HasKeys {
		return &model.SiteKeyCreateResult{
			Status:        model.SiteKeyCreateStatusAlreadyExists,
			RemoteApplied: false,
			Message:       "该分组已存在 Key，未重复创建",
			Account:       accountView,
		}, nil
	}

	name := firstNonEmptyString(strings.TrimSpace(req.Name), strings.TrimSpace(group.GroupName), groupKey)
	if err := createRemoteAccountToken(ctx, siteRecord, account, groupKey, name); err != nil {
		recordKeyCreateAuthFailure(ctx, account, err)
		return nil, sanitizeSiteError(err)
	}

	if _, err := SyncAccount(ctx, accountID); err != nil && !isMissingKeySyncError(err) {
		staleView, _ := op.SiteChannelAccountGet(siteID, accountID, ctx)
		return &model.SiteKeyCreateResult{
			Status:        model.SiteKeyCreateStatusRemoteCreatedSyncFailed,
			RemoteApplied: true,
			SyncPending:   true,
			Message:       "上游已创建 Key，但本地同步失败；请先重新同步，不要重复创建",
			Account:       staleView,
		}, nil
	}

	accountView, err = op.SiteChannelAccountGet(siteID, accountID, ctx)
	if err != nil {
		return &model.SiteKeyCreateResult{
			Status:        model.SiteKeyCreateStatusRemoteCreatedSyncFailed,
			RemoteApplied: true,
			SyncPending:   true,
			Message:       "上游已创建 Key，但本地账号视图刷新失败；请先重新同步，不要重复创建",
		}, nil
	}
	refreshedGroup, ok := findSiteChannelGroup(accountView.Groups, groupKey)
	if !ok || !refreshedGroup.HasKeys {
		return &model.SiteKeyCreateResult{
			Status:        model.SiteKeyCreateStatusRemoteCreatedSyncFailed,
			RemoteApplied: true,
			SyncPending:   true,
			Message:       "上游已接受创建请求，但同步后仍未读取到目标 Key；请先重新同步，不要重复创建",
			Account:       accountView,
		}, nil
	}
	return &model.SiteKeyCreateResult{
		Status:        model.SiteKeyCreateStatusCreated,
		RemoteApplied: true,
		Message:       "Key 已创建并完成同步",
		Account:       accountView,
	}, nil
}

func CreateAllMissingAccountTokens(ctx context.Context, siteID int, accountID int) (*model.SiteKeyCreateBatchResult, error) {
	if _, _, err := loadWritableKeyCreateAccount(ctx, siteID, accountID); err != nil {
		return nil, err
	}
	if _, err := SyncAccount(ctx, accountID); err != nil && !isMissingKeySyncError(err) {
		return nil, fmt.Errorf("failed to refresh site account before batch key creation: %w", err)
	}

	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if siteRecord.ID != siteID {
		return nil, fmt.Errorf("site account not found")
	}
	if err := requireWritableKeyCreateCapability(siteRecord, account); err != nil {
		return nil, err
	}

	accountView, err := op.SiteChannelAccountGet(siteID, accountID, ctx)
	if err != nil {
		return nil, err
	}
	pendingGroups := make([]model.SiteChannelGroup, 0)
	alreadyExistsCount := 0
	for _, group := range accountView.Groups {
		if group.HasKeys {
			alreadyExistsCount++
			continue
		}
		pendingGroups = append(pendingGroups, group)
	}
	sort.SliceStable(pendingGroups, func(i, j int) bool {
		return pendingGroups[i].GroupKey < pendingGroups[j].GroupKey
	})

	result := &model.SiteKeyCreateBatchResult{
		AttemptedCount:     len(pendingGroups),
		AlreadyExistsCount: alreadyExistsCount,
		Failures:           make([]model.SiteKeyCreateBatchFailure, 0),
		Account:            accountView,
	}
	if len(pendingGroups) == 0 {
		return result, nil
	}
	for _, group := range pendingGroups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		name := firstNonEmptyString(strings.TrimSpace(group.GroupName), groupKey)
		if createErr := createRemoteAccountToken(ctx, siteRecord, account, groupKey, name); createErr != nil {
			recordKeyCreateAuthFailure(ctx, account, createErr)
			result.Failures = append(result.Failures, model.SiteKeyCreateBatchFailure{
				GroupKey:  groupKey,
				GroupName: name,
				Message:   sanitizeSiteStatusMessage(createErr),
			})
			continue
		}
		result.CreatedCount++
	}
	result.FailedCount = len(result.Failures)

	if _, err := SyncAccount(ctx, accountID); err != nil && !isMissingKeySyncError(err) {
		result.SyncPending = result.CreatedCount > 0
		return result, nil
	}
	if refreshed, err := op.SiteChannelAccountGet(siteID, accountID, ctx); err == nil {
		result.Account = refreshed
		createdGroups := make(map[string]struct{}, result.CreatedCount)
		failedGroups := make(map[string]struct{}, len(result.Failures))
		for _, failure := range result.Failures {
			failedGroups[failure.GroupKey] = struct{}{}
		}
		for _, group := range pendingGroups {
			groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
			if _, failed := failedGroups[groupKey]; !failed {
				createdGroups[groupKey] = struct{}{}
			}
		}
		for _, group := range refreshed.Groups {
			key := model.NormalizeSiteGroupKey(group.GroupKey)
			if _, expected := createdGroups[key]; expected && group.HasKeys {
				delete(createdGroups, key)
			}
		}
		if len(createdGroups) > 0 {
			result.SyncPending = true
		}
	} else if result.CreatedCount > 0 {
		result.SyncPending = true
	}
	return result, nil
}

func loadWritableKeyCreateAccount(ctx context.Context, siteID int, accountID int) (*model.Site, *model.SiteAccount, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	if siteRecord == nil || account == nil || siteRecord.ID != siteID || account.SiteID != siteID {
		return nil, nil, fmt.Errorf("site account not found")
	}
	if err := requireWritableKeyCreateCapability(siteRecord, account); err != nil {
		return nil, nil, err
	}
	return siteRecord, account, nil
}

func requireWritableKeyCreateCapability(siteRecord *model.Site, account *model.SiteAccount) error {
	capability := model.SiteKeyCreateCapabilityFor(siteRecord, account)
	if capability.CanCreateSingle && capability.CanCreateAll {
		return nil
	}
	return fmt.Errorf("%s: %s", capability.ReasonCode, capability.Reason)
}

func normalizeRequestedGroupKey(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("group key is required")
	}
	return model.NormalizeSiteGroupKey(value), nil
}

func isMissingKeySyncError(err error) bool {
	if err == nil {
		return false
	}
	code := apperror.Code(err)
	return code == CodeSiteSyncMissingGroupKey || code == apperror.CodeSiteSub2APIAPIKeyRequired
}

func findSiteChannelGroup(groups []model.SiteChannelGroup, groupKey string) (model.SiteChannelGroup, bool) {
	for _, group := range groups {
		if model.NormalizeSiteGroupKey(group.GroupKey) == groupKey {
			return group, true
		}
	}
	return model.SiteChannelGroup{}, false
}

func recordKeyCreateAuthFailure(ctx context.Context, account *model.SiteAccount, err error) {
	if authErr := recordAccountAuthFailure(ctx, account, "create_key", err); authErr != nil {
		log.Warnf("failed to update site account auth state after key creation failure (account=%d): %v", account.ID, authErr)
	}
}

func createRemoteAccountToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if err := requireWritableKeyCreateCapability(siteRecord, account); err != nil {
		return err
	}
	switch siteRecord.Platform {
	case model.SitePlatformAnyRouter:
		return createAnyRouterToken(ctx, siteRecord, account, groupKey, name)
	case model.SitePlatformNewAPI, model.SitePlatformOneHub, model.SitePlatformDoneHub:
		return createManagementPlatformToken(ctx, siteRecord, account, groupKey, name)
	case model.SitePlatformSub2API:
		return createSub2APIToken(ctx, siteRecord, account, groupKey, name)
	default:
		return fmt.Errorf("site platform %s does not support group key creation", siteRecord.Platform)
	}
}

func createManagementPlatformToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return fmt.Errorf("API key credential account does not support quick site key creation")
	}

	accessToken, err := resolveManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return err
	}

	payload, err := requestJSONWithManagedAccessToken(
		ctx,
		siteRecord,
		http.MethodPost,
		buildSiteURL(siteRecord.BaseURL, "/api/token/"),
		buildManagedTokenCreatePayload(siteRecord.Platform, groupKey, name),
		accessToken,
		account,
	)
	if err != nil {
		return err
	}
	if !siteTokenCreateSucceeded(payload) {
		return fmt.Errorf("%s", firstNonEmptyString(extractSiteResponseMessage(payload), "site token creation failed"))
	}
	return nil
}

func createAnyRouterToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return fmt.Errorf("API key credential account does not support quick site key creation")
	}

	accessToken, err := resolveAnyRouterManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return err
	}

	payloadBody := buildManagedTokenCreatePayload(siteRecord.Platform, groupKey, name)
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/token/")

	userID, _ := anyRouterDiscoverUserID(ctx, siteRecord, account, accessToken)
	payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodPost,
		requestURL,
		payloadBody,
		anyRouterAuthHeaders(accessToken, userID),
		account,
	)
	if err == nil && siteTokenCreateSucceeded(payload) {
		return nil
	}
	if err != nil && !shouldTryAlternativeManagedAuth(err) {
		return err
	}

	tryUserIDs := []int{userID}
	if alternateUserID, probeErr := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID); probeErr == nil && alternateUserID > 0 {
		tryUserIDs = append(tryUserIDs, alternateUserID)
	}
	if userID <= 0 {
		if probedUserID, probeErr := anyRouterProbeUserIDByCookie(ctx, siteRecord, account, accessToken); probeErr == nil && probedUserID > 0 {
			tryUserIDs = append(tryUserIDs, probedUserID)
		}
	}
	tryUserIDs = slicesCompactInts(tryUserIDs)

	for _, candidateUserID := range tryUserIDs {
		for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
			headers := map[string]string{"Cookie": cookie}
			anyRouterAddUserIDHeaders(headers, candidateUserID)
			payload, _, requestErr := anyRouterRequestJSONWithCookies(
				ctx,
				siteRecord,
				http.MethodPost,
				requestURL,
				payloadBody,
				headers,
				account,
			)
			if requestErr != nil {
				if !shouldTryAlternativeManagedAuth(requestErr) {
					return requestErr
				}
				if err == nil {
					err = requestErr
				}
				continue
			}
			if siteTokenCreateSucceeded(payload) {
				return nil
			}
			if message := strings.TrimSpace(extractSiteResponseMessage(payload)); message != "" {
				err = fmt.Errorf("%s", message)
			}
		}
	}

	if err != nil {
		return err
	}
	return fmt.Errorf("site token creation failed")
}

func createSub2APIToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return fmt.Errorf("API key credential account does not support quick site key creation")
	}

	groupID, err := strconv.Atoi(model.NormalizeSiteGroupKey(groupKey))
	if err != nil || groupID <= 0 {
		return fmt.Errorf("sub2api group key %q is not a positive group id", groupKey)
	}

	accessToken, err := ensureFreshSub2APIAccessToken(ctx, siteRecord, account, false)
	if err != nil {
		return err
	}

	requestBody := buildSub2APITokenCreatePayload(groupID, name)
	headers := map[string]string{"Authorization": ensureBearer(accessToken)}
	endpoints := []string{"/api/v1/keys", "/api/v1/api-keys"}

	for index, endpoint := range endpoints {
		payload, err := requestJSON(
			ctx,
			siteRecord,
			http.MethodPost,
			buildSiteURL(siteRecord.BaseURL, endpoint),
			requestBody,
			headers,
			account,
		)
		if err != nil && shouldRetrySub2APIAfterRefresh(err, account) {
			refreshedToken, refreshErr := ensureFreshSub2APIAccessToken(ctx, siteRecord, account, true)
			if refreshErr == nil && stripBearerPrefix(refreshedToken) != stripBearerPrefix(accessToken) {
				accessToken = refreshedToken
				headers = map[string]string{"Authorization": ensureBearer(refreshedToken)}
				payload, err = requestJSON(ctx, siteRecord, http.MethodPost, buildSiteURL(siteRecord.BaseURL, endpoint), requestBody, headers, account)
			}
		}
		if err != nil {
			if index == 0 && isSub2APIEndpointNotFound(err) {
				continue
			}
			return err
		}
		if err := validateSub2APITokenCreateResponse(payload, endpoint); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("sub2api key creation endpoint not found")
}

func isSub2APIEndpointNotFound(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "http 404")
}

func validateSub2APITokenCreateResponse(payload map[string]any, endpoint string) error {
	if _, hasCode := payload["code"]; hasCode {
		data, err := unwrapSub2APIData(payload, endpoint)
		if err != nil {
			return err
		}
		if data == nil {
			return fmt.Errorf("sub2api %s response missing created key data", endpoint)
		}
		return nil
	}
	if siteTokenCreateSucceeded(payload) {
		return nil
	}
	return fmt.Errorf("%s", firstNonEmptyString(extractSiteResponseMessage(payload), "site token creation failed"))
}

func buildManagedTokenCreatePayload(platform model.SitePlatform, groupKey string, name string) map[string]any {
	return map[string]any{
		"name":                 defaultSiteTokenCreateName(platform, groupKey, name),
		"unlimited_quota":      true,
		"expired_time":         -1,
		"remain_quota":         0,
		"allow_ips":            "",
		"model_limits_enabled": false,
		"model_limits":         "",
		"group":                model.NormalizeSiteGroupKey(groupKey),
	}
}

func buildSub2APITokenCreatePayload(groupID int, name string) map[string]any {
	return map[string]any{
		"name":     defaultSiteTokenCreateName(model.SitePlatformSub2API, strconv.Itoa(groupID), name),
		"group_id": groupID,
	}
}

func defaultSiteTokenCreateName(platform model.SitePlatform, groupKey string, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return normalizeSiteTokenCreateNameForPlatform(trimmed, platform)
	}

	return normalizeSiteTokenCreateNameForPlatform(firstNonEmptyString(strings.TrimSpace(groupKey), model.SiteDefaultGroupKey), platform)
}

func normalizeSiteTokenCreateNameForPlatform(value string, platform model.SitePlatform) string {
	value = normalizeSiteTokenCreateName(value)
	maxRunes := 50
	if platform == model.SitePlatformOneHub || platform == model.SitePlatformDoneHub {
		maxRunes = 30
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		return strings.TrimSpace(string(runes[:maxRunes]))
	}
	return value
}

func normalizeSiteTokenCreateName(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		switch r {
		case '/', '\\':
			return '-'
		default:
			return r
		}
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, " -_")
	if value == "" {
		value = model.SiteDefaultGroupKey
	}
	runes := []rune(value)
	if len(runes) > 50 {
		value = strings.TrimSpace(string(runes[:50]))
	}
	return value
}

func siteTokenCreateSucceeded(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	return siteTokenCreateSucceededFromAny(payload)
}

func siteTokenCreateSucceededFromAny(value any) bool {
	payload, ok := value.(map[string]any)
	if !ok {
		succeeded, ok := value.(bool)
		return ok && succeeded
	}
	if raw, ok := payload["success"]; ok {
		switch typed := raw.(type) {
		case bool:
			return typed
		case float64:
			return typed != 0
		case int:
			return typed != 0
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "1", "true", "ok", "success":
				return true
			case "0", "false", "fail", "failed", "error":
				return false
			}
		}
		return false
	}
	return false
}

func slicesCompactInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
