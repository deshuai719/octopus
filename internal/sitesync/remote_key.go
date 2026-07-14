package sitesync

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func UpdateAccountRemoteToken(ctx context.Context, accountID int, tokenID int, req model.SiteRemoteKeyUpdateRequest) (*model.SiteRemoteKeyMutationResult, error) {
	siteRecord, account, token, err := loadRemoteSiteToken(ctx, accountID, tokenID)
	if err != nil {
		return nil, err
	}

	switch siteRecord.Platform {
	case model.SitePlatformNewAPI:
		err = updateManagementPlatformToken(ctx, siteRecord, account, token, req)
	case model.SitePlatformSub2API:
		err = updateSub2APIToken(ctx, siteRecord, account, token, req)
	default:
		return nil, fmt.Errorf("site platform %s does not support remote key updates", siteRecord.Platform)
	}
	if err != nil {
		return nil, sanitizeSiteError(err)
	}
	return finishRemoteKeyMutation(ctx, accountID, "上游 Key 已更新")
}

func DeleteAccountRemoteToken(ctx context.Context, accountID int, tokenID int) (*model.SiteRemoteKeyMutationResult, error) {
	siteRecord, account, token, err := loadRemoteSiteToken(ctx, accountID, tokenID)
	if err != nil {
		return nil, err
	}

	switch siteRecord.Platform {
	case model.SitePlatformNewAPI:
		err = deleteManagementPlatformToken(ctx, siteRecord, account, token)
	case model.SitePlatformSub2API:
		err = deleteSub2APIToken(ctx, siteRecord, account, token)
	default:
		return nil, fmt.Errorf("site platform %s does not support remote key deletion", siteRecord.Platform)
	}
	if err != nil {
		return nil, sanitizeSiteError(err)
	}
	return finishRemoteKeyMutation(ctx, accountID, "上游 Key 已删除")
}

func loadRemoteSiteToken(ctx context.Context, accountID int, tokenID int) (*model.Site, *model.SiteAccount, *model.SiteToken, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, nil, nil, err
	}
	for index := range account.Tokens {
		token := &account.Tokens[index]
		if token.ID != tokenID {
			continue
		}
		if token.ExternalID <= 0 {
			return nil, nil, nil, fmt.Errorf("site token does not have a remote key id")
		}
		return siteRecord, account, token, nil
	}
	return nil, nil, nil, fmt.Errorf("site token not found")
}

func finishRemoteKeyMutation(ctx context.Context, accountID int, message string) (*model.SiteRemoteKeyMutationResult, error) {
	result, err := SyncAccount(ctx, accountID)
	if err != nil {
		return &model.SiteRemoteKeyMutationResult{
			RemoteApplied: true,
			Message:       message + "，但 Octopus 重同步失败；请只重试同步，不要重复执行上游写操作",
		}, nil
	}
	return &model.SiteRemoteKeyMutationResult{RemoteApplied: true, Message: message + "并已完成同步", SyncResult: result}, nil
}

func updateManagementPlatformToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token *model.SiteToken, req model.SiteRemoteKeyUpdateRequest) error {
	accessToken, err := resolveManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return err
	}
	payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, http.MethodGet, buildSiteURL(siteRecord.BaseURL, fmt.Sprintf("/api/token/%d", token.ExternalID)), nil, accessToken, account)
	if err != nil {
		return err
	}
	remote, ok := payload["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("site token detail response is invalid")
	}
	update := make(map[string]any, len(remote)+1)
	for key, value := range remote {
		update[key] = value
	}
	update["id"] = token.ExternalID
	changed := false
	if req.Name != nil {
		name := normalizeSiteTokenCreateName(*req.Name)
		if name == "" {
			return fmt.Errorf("site token name is required")
		}
		update["name"] = name
		changed = true
	}
	if req.GroupKey != nil {
		update["group"] = model.NormalizeSiteGroupKey(*req.GroupKey)
		changed = true
	}
	if !changed {
		return fmt.Errorf("site token update has no changes")
	}
	updated, err := requestJSONWithManagedAccessToken(ctx, siteRecord, http.MethodPut, buildSiteURL(siteRecord.BaseURL, "/api/token/"), update, accessToken, account)
	if err != nil {
		return err
	}
	if !siteTokenCreateSucceeded(updated) {
		return fmt.Errorf("%s", firstNonEmptyString(extractSiteResponseMessage(updated), "site token update failed"))
	}
	return nil
}

func deleteManagementPlatformToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token *model.SiteToken) error {
	accessToken, err := resolveManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return err
	}
	payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, http.MethodDelete, buildSiteURL(siteRecord.BaseURL, fmt.Sprintf("/api/token/%d", token.ExternalID)), nil, accessToken, account)
	if err != nil {
		return err
	}
	if !siteTokenCreateSucceeded(payload) {
		return fmt.Errorf("%s", firstNonEmptyString(extractSiteResponseMessage(payload), "site token deletion failed"))
	}
	return nil
}

func updateSub2APIToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token *model.SiteToken, req model.SiteRemoteKeyUpdateRequest) error {
	body := make(map[string]any, 2)
	if req.Name != nil {
		body["name"] = normalizeSiteTokenCreateName(*req.Name)
	}
	if req.GroupKey != nil {
		groupID, err := strconv.ParseInt(model.NormalizeSiteGroupKey(*req.GroupKey), 10, 64)
		if err != nil || groupID <= 0 {
			return fmt.Errorf("sub2api remote key group must be a numeric group id")
		}
		body["group_id"] = groupID
	}
	if len(body) == 0 {
		return fmt.Errorf("site token update has no changes")
	}
	return mutateSub2APIToken(ctx, siteRecord, account, token.ExternalID, http.MethodPut, body)
}

func deleteSub2APIToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token *model.SiteToken) error {
	return mutateSub2APIToken(ctx, siteRecord, account, token.ExternalID, http.MethodDelete, nil)
}

func mutateSub2APIToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, externalID int64, method string, body any) error {
	accessToken, err := ensureFreshSub2APIAccessToken(ctx, siteRecord, account, false)
	if err != nil {
		return err
	}
	headers := map[string]string{"Authorization": ensureBearer(accessToken)}
	paths := []string{
		fmt.Sprintf("/api/v1/api-keys/%d", externalID),
		fmt.Sprintf("/api/v1/keys/%d", externalID),
	}
	var firstErr error
	for _, path := range paths {
		payload, requestErr := requestJSON(ctx, siteRecord, method, buildSiteURL(siteRecord.BaseURL, path), body, headers, account)
		if requestErr != nil {
			if firstErr == nil {
				firstErr = requestErr
			}
			continue
		}
		if message := strings.TrimSpace(extractSiteResponseMessage(payload)); message != "" && !siteTokenCreateSucceeded(payload) {
			return fmt.Errorf("%s", message)
		}
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	return fmt.Errorf("sub2api remote key mutation failed")
}
