package op

import (
	"context"
	"errors"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type SiteChannelHealthMetadata struct {
	SiteID          int
	SiteName        string
	SiteTags        []string
	SiteGroupName   string
	SiteGroupRatio  *float64
	RatioLastSeenAt *time.Time
}

func SiteChannelBindingGetByChannelID(channelID int, ctx context.Context) (*model.SiteChannelBinding, error) {
	var binding model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Where("channel_id = ?", channelID).First(&binding).Error; err != nil {
		return nil, err
	}
	return &binding, nil
}

func SiteChannelBindingMapByChannelIDs(channelIDs []int, ctx context.Context) (map[int]model.SiteChannelBinding, error) {
	result := make(map[int]model.SiteChannelBinding)
	if len(channelIDs) == 0 {
		return result, nil
	}

	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		result[binding.ChannelID] = binding
	}
	return result, nil
}

func SiteChannelHealthMetadataMapByChannelIDs(channelIDs []int, ctx context.Context) (map[int]SiteChannelHealthMetadata, error) {
	result := make(map[int]SiteChannelHealthMetadata)
	bindings, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil || len(bindings) == 0 {
		return result, err
	}

	siteIDs := make([]int, 0, len(bindings))
	groupIDs := make([]int, 0, len(bindings))
	accountIDs := make([]int, 0, len(bindings))
	groupKeys := make([]string, 0, len(bindings))
	seenSiteIDs := make(map[int]struct{}, len(bindings))
	seenGroupIDs := make(map[int]struct{}, len(bindings))
	seenAccountIDs := make(map[int]struct{}, len(bindings))
	seenGroupKeys := make(map[string]struct{}, len(bindings))

	for _, binding := range bindings {
		if _, seen := seenSiteIDs[binding.SiteID]; !seen {
			seenSiteIDs[binding.SiteID] = struct{}{}
			siteIDs = append(siteIDs, binding.SiteID)
		}
		if binding.SiteUserGroupID != nil && *binding.SiteUserGroupID > 0 {
			if _, seen := seenGroupIDs[*binding.SiteUserGroupID]; !seen {
				seenGroupIDs[*binding.SiteUserGroupID] = struct{}{}
				groupIDs = append(groupIDs, *binding.SiteUserGroupID)
			}
			continue
		}
		if _, seen := seenAccountIDs[binding.SiteAccountID]; !seen {
			seenAccountIDs[binding.SiteAccountID] = struct{}{}
			accountIDs = append(accountIDs, binding.SiteAccountID)
		}
		baseGroupKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
		if _, seen := seenGroupKeys[baseGroupKey]; !seen {
			seenGroupKeys[baseGroupKey] = struct{}{}
			groupKeys = append(groupKeys, baseGroupKey)
		}
	}

	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).Select("id", "name", "tags").Where("id IN ?", siteIDs).Find(&sites).Error; err != nil {
		return nil, err
	}
	siteByID := make(map[int]model.Site, len(sites))
	for _, site := range sites {
		siteByID[site.ID] = site
	}

	groupByID := make(map[int]model.SiteUserGroup, len(groupIDs))
	if len(groupIDs) > 0 {
		var groups []model.SiteUserGroup
		if err := db.GetDB().WithContext(ctx).
			Select("id", "site_account_id", "group_key", "name", "ratio", "ratio_last_seen_at").
			Where("id IN ?", groupIDs).
			Find(&groups).Error; err != nil {
			return nil, err
		}
		for _, group := range groups {
			groupByID[group.ID] = group
		}
	}

	groupByAccountKey := make(map[string]model.SiteUserGroup)
	if len(accountIDs) > 0 && len(groupKeys) > 0 {
		var groups []model.SiteUserGroup
		if err := db.GetDB().WithContext(ctx).
			Select("id", "site_account_id", "group_key", "name", "ratio", "ratio_last_seen_at").
			Where("site_account_id IN ? AND group_key IN ?", accountIDs, groupKeys).
			Find(&groups).Error; err != nil {
			return nil, err
		}
		for _, group := range groups {
			groupByAccountKey[siteUserGroupAccountKey(group.SiteAccountID, group.GroupKey)] = group
		}
	}

	for channelID, binding := range bindings {
		site, exists := siteByID[binding.SiteID]
		if !exists {
			continue
		}
		metadata := SiteChannelHealthMetadata{
			SiteID:   site.ID,
			SiteName: site.Name,
			SiteTags: append([]string(nil), site.Tags...),
		}
		var siteGroup model.SiteUserGroup
		var hasGroup bool
		if binding.SiteUserGroupID != nil {
			siteGroup, hasGroup = groupByID[*binding.SiteUserGroupID]
		} else {
			baseGroupKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
			siteGroup, hasGroup = groupByAccountKey[siteUserGroupAccountKey(binding.SiteAccountID, baseGroupKey)]
			if !hasGroup {
				metadata.SiteGroupName = baseGroupKey
			}
		}
		if hasGroup {
			metadata.SiteGroupName = siteGroup.Name
			if metadata.SiteGroupName == "" {
				metadata.SiteGroupName = siteGroup.GroupKey
			}
			metadata.SiteGroupRatio = siteGroup.Ratio
			metadata.RatioLastSeenAt = siteGroup.RatioLastSeenAt
		}
		result[channelID] = metadata
	}
	return result, nil
}

// SiteChannelBindingListAll 返回全部站点投影渠道绑定，供 POR 任务按 siteAccountID 分组兄弟渠道。
func SiteChannelBindingListAll(ctx context.Context) ([]model.SiteChannelBinding, error) {
	var bindings []model.SiteChannelBinding
	err := db.GetDB().WithContext(ctx).Find(&bindings).Error
	return bindings, err
}

func ChannelManagedBinding(channelID int, ctx context.Context) (*model.SiteChannelBinding, bool, error) {
	binding, err := SiteChannelBindingGetByChannelID(channelID, ctx)
	if err == nil {
		return binding, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}
