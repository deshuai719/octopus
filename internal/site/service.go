package site

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/sitesync"
)

func SyncAccount(ctx context.Context, accountID int) (*model.SiteSyncResult, error) {
	return sitesync.SyncAccount(ctx, accountID)
}

func CheckinAccount(ctx context.Context, accountID int) (*model.SiteCheckinResult, error) {
	return sitesync.CheckinAccount(ctx, accountID)
}

func ProjectAccount(ctx context.Context, accountID int) ([]int, error) {
	return sitesync.ProjectAccount(ctx, accountID)
}

func ProjectSite(ctx context.Context, siteID int) error {
	return sitesync.ProjectSite(ctx, siteID)
}

func SyncAll(ctx context.Context) {
	sitesync.SyncAll(ctx)
}

func SyncAllWithOptions(ctx context.Context, opts sitesync.SiteBatchOptions) sitesync.SiteBatchSummary {
	return sitesync.SyncAllWithOptions(ctx, opts)
}

func SyncAccountsWithOptions(ctx context.Context, accountIDs []int, opts sitesync.SiteBatchOptions) sitesync.SiteBatchSummary {
	return sitesync.SyncAccountsWithOptions(ctx, accountIDs, opts)
}

func CheckinAll(ctx context.Context) {
	sitesync.CheckinAll(ctx)
}

func CheckinAllWithOptions(ctx context.Context, opts sitesync.SiteBatchOptions) sitesync.SiteBatchSummary {
	return sitesync.CheckinAllWithOptions(ctx, opts)
}

func LastSyncAllTime() time.Time {
	return sitesync.LastSyncAllTime()
}

func LastCheckinAllTime() time.Time {
	return sitesync.LastCheckinAllTime()
}

func RefreshAccountRandomCheckinSchedule(ctx context.Context, accountID int) error {
	return sitesync.RefreshAccountRandomCheckinSchedule(ctx, accountID)
}

func UpdateAccountRemoteToken(ctx context.Context, accountID int, tokenID int, req model.SiteRemoteKeyUpdateRequest) (*model.SiteRemoteKeyMutationResult, error) {
	return sitesync.UpdateAccountRemoteToken(ctx, accountID, tokenID, req)
}

func DeleteAccountRemoteToken(ctx context.Context, accountID int, tokenID int) (*model.SiteRemoteKeyMutationResult, error) {
	return sitesync.DeleteAccountRemoteToken(ctx, accountID, tokenID)
}

func DeleteSite(ctx context.Context, siteID int) error {
	return sitesync.DeleteSite(ctx, siteID)
}

func ArchiveSite(ctx context.Context, siteID int) error {
	return sitesync.ArchiveSite(ctx, siteID)
}

func RestoreSite(ctx context.Context, siteID int) error {
	return sitesync.RestoreSite(ctx, siteID)
}

func ListArchivedSites(ctx context.Context) ([]model.Site, error) {
	return sitesync.ListArchivedSites(ctx)
}

func DeleteSiteAccount(ctx context.Context, accountID int) error {
	return sitesync.DeleteSiteAccount(ctx, accountID)
}

func DetectPlatform(ctx context.Context, rawURL string) (model.SitePlatform, model.SiteModelRouteType, error) {
	return sitesync.DetectPlatform(ctx, rawURL)
}

func DetectPlatformDetailed(ctx context.Context, rawURL string) (sitesync.PlatformDetection, error) {
	return sitesync.DetectPlatformDetailed(ctx, rawURL)
}

func PlatformAuthCapabilities() []sitesync.PlatformAuthCapability {
	return sitesync.PlatformAuthCapabilities()
}

func CreateRecoverySession(ctx context.Context, accountID int, ownerToken string) (sitesync.RecoverySessionView, error) {
	return sitesync.CreateRecoverySession(ctx, accountID, ownerToken)
}

func GetRecoverySession(ctx context.Context, sessionID string, ownerToken string) (sitesync.RecoverySessionView, error) {
	return sitesync.GetRecoverySession(ctx, sessionID, ownerToken)
}

func SubmitRecoveryCandidate(ctx context.Context, sessionID string, capability string, input sitesync.RecoveryCandidateInput) (sitesync.RecoverySessionView, error) {
	return sitesync.SubmitRecoveryCandidate(ctx, sessionID, capability, input)
}

func ConfirmRecoverySession(ctx context.Context, sessionID string, ownerToken string) (sitesync.RecoverySessionView, error) {
	return sitesync.ConfirmRecoverySession(ctx, sessionID, ownerToken)
}

func CancelRecoverySession(ctx context.Context, sessionID string, ownerToken string) (sitesync.RecoverySessionView, error) {
	return sitesync.CancelRecoverySession(ctx, sessionID, ownerToken)
}

func CreateAccountToken(ctx context.Context, accountID int, req model.SiteChannelKeyCreateRequest) (*model.SiteSyncResult, error) {
	return sitesync.CreateAccountToken(ctx, accountID, req)
}
