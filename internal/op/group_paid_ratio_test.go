package op

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestGroupGetEnabledMapHydratesPaidSiteLowRatioMetadata(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	paidSite, paidAccount := createGroupPaidRatioSiteAccount(t, ctx, "paid-ratio-site", []string{model.SiteTagPaid})
	publicSite, publicAccount := createGroupPaidRatioSiteAccount(t, ctx, "public-ratio-site", []string{model.SiteTagPublic})

	paidChannel := createGroupPaidRatioChannel(t, ctx, "paid-ratio-channel")
	publicChannel := createGroupPaidRatioChannel(t, ctx, "public-ratio-channel")

	paidRatio := 0.75
	paidGroup := createGroupPaidRatioSiteUserGroup(t, ctx, paidAccount.ID, "vip", &paidRatio)
	publicRatio := 0.5
	publicGroup := createGroupPaidRatioSiteUserGroup(t, ctx, publicAccount.ID, "vip", &publicRatio)

	createGroupPaidRatioBinding(t, ctx, paidSite.ID, paidAccount.ID, paidGroup.ID, "vip", paidChannel.ID)
	createGroupPaidRatioBinding(t, ctx, publicSite.ID, publicAccount.ID, publicGroup.ID, "vip", publicChannel.ID)

	group := &model.Group{
		Name:                  "paid-ratio-group",
		Mode:                  model.GroupModeFailover,
		PaidSiteLowRatioFirst: true,
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: paidChannel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd paid failed: %v", err)
	}
	if err := GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: publicChannel.ID, ModelName: "gpt-4o", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd public failed: %v", err)
	}

	got, err := GroupGetEnabledMap(group.Name, ctx)
	if err != nil {
		t.Fatalf("GroupGetEnabledMap failed: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 enabled items, got %d", len(got.Items))
	}

	byChannelID := map[int]model.GroupItem{}
	for _, item := range got.Items {
		byChannelID[item.ChannelID] = item
	}

	paidItem := byChannelID[paidChannel.ID]
	if !paidItem.PaidSite {
		t.Fatalf("expected paid channel to be marked as paid site")
	}
	if paidItem.SiteGroupRatio == nil || *paidItem.SiteGroupRatio != paidRatio {
		t.Fatalf("paid ratio = %v, want %v", paidItem.SiteGroupRatio, paidRatio)
	}

	publicItem := byChannelID[publicChannel.ID]
	if publicItem.PaidSite {
		t.Fatalf("expected public channel not to be marked as paid site")
	}
	if publicItem.SiteGroupRatio != nil {
		t.Fatalf("public ratio metadata = %v, want nil", *publicItem.SiteGroupRatio)
	}
}

func createGroupPaidRatioSiteAccount(t *testing.T, ctx context.Context, name string, tags []string) (*model.Site, *model.SiteAccount) {
	t.Helper()

	site := &model.Site{
		Name:     name,
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://" + name + ".example.com",
		Enabled:  true,
		Tags:     tags,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           name + "-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	return site, account
}

func createGroupPaidRatioChannel(t *testing.T, ctx context.Context, name string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://" + name + ".example.com/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	return channel
}

func createGroupPaidRatioSiteUserGroup(t *testing.T, ctx context.Context, accountID int, groupKey string, ratio *float64) model.SiteUserGroup {
	t.Helper()
	siteGroup := model.SiteUserGroup{
		SiteAccountID: accountID,
		GroupKey:      groupKey,
		Name:          groupKey,
		Ratio:         ratio,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteGroup).Error; err != nil {
		t.Fatalf("create SiteUserGroup failed: %v", err)
	}
	return siteGroup
}

func createGroupPaidRatioBinding(t *testing.T, ctx context.Context, siteID, accountID, siteGroupID int, groupKey string, channelID int) {
	t.Helper()
	binding := model.SiteChannelBinding{
		SiteID:          siteID,
		SiteAccountID:   accountID,
		SiteUserGroupID: &siteGroupID,
		GroupKey:        groupKey,
		ChannelID:       channelID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create SiteChannelBinding failed: %v", err)
	}
}
