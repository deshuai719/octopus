package grouphealth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

var ErrGroupHealthAlreadyRunning = errors.New("group health check already running")

type Repository interface {
	CreateRunningSnapshot(ctx context.Context, group model.Group, probeMode model.GroupHealthProbeMode) (*model.GroupHealthSnapshot, error)
	AppendAttempt(ctx context.Context, snapshotID int, attempt model.GroupHealthAttempt) error
	FinishSnapshot(ctx context.Context, snapshotID int, status model.GroupHealthStatus, successfulChannelID *int, durationMS int64, message string, finishedAt time.Time) error
	GetLatestSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error)
	GetRunningSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error)
	ListGroupHealthViews(ctx context.Context) ([]model.GroupHealthGroupView, error)
	GetGroupHealthViewByID(ctx context.Context, groupID int) (*model.GroupHealthGroupView, error)
}

type Service struct {
	repo   Repository
	prober *Prober
}

var runLocks sync.Map

func NewService(repo Repository, prober *Prober) *Service {
	if repo == nil {
		repo = op.NewGroupHealthRepository()
	}
	if prober == nil {
		prober = NewProber()
	}
	return &Service{
		repo:   repo,
		prober: prober,
	}
}

func lockGroup(groupID int) func() {
	value, _ := runLocks.LoadOrStore(groupID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return func() {
		lock.Unlock()
	}
}

// normalizeProbeMode returns the effective probe mode from a prioritized list.
// An empty list defaults to model.GroupHealthProbeModeStandard, and only the
// first element is considered. model.GroupHealthProbeModeFull is honored only
// when it appears first; all other cases fall back to Standard semantics.
func normalizeProbeMode(probeModes []model.GroupHealthProbeMode) model.GroupHealthProbeMode {
	if len(probeModes) == 0 {
		return model.GroupHealthProbeModeStandard
	}
	if probeModes[0] == model.GroupHealthProbeModeFull {
		return model.GroupHealthProbeModeFull
	}
	return model.GroupHealthProbeModeStandard
}

func resolveChannelName(ctx context.Context, channelID int) string {
	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return fmt.Sprintf("channel-%d", channelID)
	}
	return channel.Name
}

func newAttempt(item model.GroupItem, channelName string, metadata op.SiteChannelHealthMetadata, profile model.GroupHealthProbeProfile) model.GroupHealthAttempt {
	return model.GroupHealthAttempt{
		GroupItemID:          item.ID,
		ChannelID:            item.ChannelID,
		ChannelName:          channelName,
		ModelName:            item.ModelName,
		Priority:             item.Priority,
		Weight:               item.Weight,
		SiteID:               metadata.SiteID,
		SiteName:             metadata.SiteName,
		SiteTags:             append([]string(nil), metadata.SiteTags...),
		SiteGroupName:        metadata.SiteGroupName,
		SiteGroupRatio:       metadata.SiteGroupRatio,
		SiteGroupRatioSeenAt: metadata.RatioLastSeenAt,
		ProbeProfile:         profile,
	}
}

func (s *Service) RunGroupHealth(ctx context.Context, groupID int, probeModes ...model.GroupHealthProbeMode) error {
	unlock := lockGroup(groupID)
	defer unlock()

	if _, err := s.repo.GetRunningSnapshotByGroupID(ctx, groupID); err == nil {
		return ErrGroupHealthAlreadyRunning
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	group, err := op.GroupGet(groupID, ctx)
	if err != nil {
		return err
	}

	probeMode := normalizeProbeMode(probeModes)

	items := append([]model.GroupItem(nil), group.Items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if items[i].Weight != items[j].Weight {
			return items[i].Weight > items[j].Weight
		}
		if items[i].ChannelID != items[j].ChannelID {
			return items[i].ChannelID < items[j].ChannelID
		}
		return items[i].ID < items[j].ID
	})
	channelIDs := make([]int, 0, len(items))
	for _, item := range items {
		channelIDs = append(channelIDs, item.ChannelID)
	}
	metadataByChannelID, err := op.SiteChannelHealthMetadataMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return fmt.Errorf("load group health site metadata: %w", err)
	}

	snapshot, err := s.repo.CreateRunningSnapshot(ctx, *group, probeMode)
	if err != nil {
		return err
	}

	var successfulChannelID *int
	message := "all candidates failed"
	stopAfterSuccess := group.Mode == model.GroupModeFailover && probeMode != model.GroupHealthProbeModeFull
	successFound := false
	firstSuccessIndex := -1
	attemptedCount := 0
	successCount := 0

	for index, item := range items {
		metadata := metadataByChannelID[item.ChannelID]
		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			attemptedCount++
			attempt := newAttempt(item, fmt.Sprintf("channel-%d", item.ChannelID), metadata, model.GroupHealthProbeProfileStandard)
			attempt.Status = model.GroupHealthAttemptStatusFailed
			attempt.ErrorMessage = fmt.Sprintf("failed to load channel: %v", err)
			appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, attempt)
			if appendErr != nil {
				return appendErr
			}
			continue
		}

		usedKey := channel.GetChannelKey()
		profile := selectProbeProfile(metadata.SiteTags, channel.Type)
		if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" {
			attemptedCount++
			attempt := newAttempt(item, channel.Name, metadata, profile)
			attempt.Status = model.GroupHealthAttemptStatusFailed
			attempt.ErrorMessage = "no available key"
			appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, attempt)
			if appendErr != nil {
				return appendErr
			}
			continue
		}

		result := s.prober.RunCandidateWithProfile(ctx, *channel, usedKey, item.ModelName, profile)
		attemptedCount++
		attempt := newAttempt(item, channel.Name, metadata, profile)
		attempt.ChannelKeyID = usedKey.ID
		attempt.KeyRemark = usedKey.Remark
		attempt.HTTPStatus = result.HTTPStatus
		attempt.DurationMS = result.DurationMS
		attempt.ErrorMessage = result.ErrorMessage
		if result.Success {
			attempt.Status = model.GroupHealthAttemptStatusSuccess
		} else {
			attempt.Status = model.GroupHealthAttemptStatusFailed
		}
		if err := s.repo.AppendAttempt(ctx, snapshot.ID, attempt); err != nil {
			return err
		}

		if result.Success {
			successFound = true
			successCount++
			if firstSuccessIndex == -1 {
				firstSuccessIndex = index
				successfulChannelID = &item.ChannelID
			}
			if stopAfterSuccess {
				for _, skipped := range items[index+1:] {
					skippedMetadata := metadataByChannelID[skipped.ChannelID]
					channelName := fmt.Sprintf("channel-%d", skipped.ChannelID)
					skippedProfile := model.GroupHealthProbeProfileStandard
					if skippedChannel, getErr := op.ChannelGet(skipped.ChannelID, ctx); getErr == nil {
						channelName = skippedChannel.Name
						skippedProfile = selectProbeProfile(skippedMetadata.SiteTags, skippedChannel.Type)
					}
					skippedAttempt := newAttempt(skipped, channelName, skippedMetadata, skippedProfile)
					skippedAttempt.Status = model.GroupHealthAttemptStatusSkipped
					if err := s.repo.AppendAttempt(ctx, snapshot.ID, skippedAttempt); err != nil {
						return err
					}
				}
				break
			}
		}
	}

	finalStatus := model.GroupHealthStatusFailed
	if !successFound && len(items) == 0 {
		message = "group has no items"
	} else if successFound {
		successChannelName := resolveChannelName(ctx, items[firstSuccessIndex].ChannelID)
		switch {
		case stopAfterSuccess && firstSuccessIndex == 0:
			finalStatus = model.GroupHealthStatusSuccess
			message = fmt.Sprintf("candidate %s succeeded", successChannelName)
		case stopAfterSuccess:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("candidate %s succeeded after failover", successChannelName)
		case successCount == attemptedCount:
			finalStatus = model.GroupHealthStatusSuccess
			message = fmt.Sprintf("all %d candidates succeeded", successCount)
		default:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("%d/%d candidates succeeded", successCount, attemptedCount)
		}
	}

	finishedAt := time.Now()
	durationMS := finishedAt.Sub(snapshot.StartedAt).Milliseconds()
	return s.repo.FinishSnapshot(ctx, snapshot.ID, finalStatus, successfulChannelID, durationMS, message, finishedAt)
}

func (s *Service) RunAllGroupHealth(ctx context.Context, maxConcurrency int, probeModes ...model.GroupHealthProbeMode) {
	if maxConcurrency <= 0 {
		maxConcurrency = 2
	}
	probeMode := normalizeProbeMode(probeModes)
	groups, err := op.GroupList(ctx)
	if err != nil {
		return
	}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for _, group := range groups {
		groupID := group.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = s.RunGroupHealth(ctx, groupID, probeMode)
		}()
	}
	wg.Wait()
}

func (s *Service) ListGroupHealthViews(ctx context.Context) ([]model.GroupHealthGroupView, error) {
	return s.repo.ListGroupHealthViews(ctx)
}

func (s *Service) GetGroupHealthViewByID(ctx context.Context, groupID int) (*model.GroupHealthGroupView, error) {
	return s.repo.GetGroupHealthViewByID(ctx, groupID)
}

func (s *Service) GetRunningSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error) {
	return s.repo.GetRunningSnapshotByGroupID(ctx, groupID)
}
