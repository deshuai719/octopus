package balancer

import (
	"reflect"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func ratio(value float64) *float64 {
	return &value
}

func candidateIDs(items []model.GroupItem) []int {
	ids := make([]int, len(items))
	for i, item := range items {
		ids[i] = item.ChannelID
	}
	return ids
}

func TestApplyPaidSiteLowRatioFirstDisabledKeepsOrder(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 1, PaidSite: true, SiteGroupRatio: ratio(2)},
		{ChannelID: 2, PaidSite: true, SiteGroupRatio: ratio(1)},
	}

	applyPaidSiteLowRatioFirst(items, false)

	if got, want := candidateIDs(items), []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate order = %v, want %v", got, want)
	}
}

func TestApplyPaidSiteLowRatioFirstSortsOnlyPaidSlots(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 1, PaidSite: false},
		{ChannelID: 2, PaidSite: true, SiteGroupRatio: ratio(1.5)},
		{ChannelID: 3, PaidSite: false},
		{ChannelID: 4, PaidSite: true, SiteGroupRatio: ratio(0.75)},
		{ChannelID: 5, PaidSite: true, SiteGroupRatio: ratio(1.0)},
	}

	applyPaidSiteLowRatioFirst(items, true)

	if got, want := candidateIDs(items), []int{1, 4, 3, 5, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate order = %v, want %v", got, want)
	}
}

func TestApplyPaidSiteLowRatioFirstKeepsNilRatioLastWithinPaidSlots(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 1, PaidSite: true},
		{ChannelID: 2, PaidSite: true, SiteGroupRatio: ratio(0.8)},
		{ChannelID: 3, PaidSite: false},
		{ChannelID: 4, PaidSite: true, SiteGroupRatio: ratio(0.6)},
	}

	applyPaidSiteLowRatioFirst(items, true)

	if got, want := candidateIDs(items), []int{4, 2, 3, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate order = %v, want %v", got, want)
	}
}

func TestNewIteratorTTLStickyExpires(t *testing.T) {
	Reset()
	globalSession.Store(sessionKey(7, "gpt-test"), &SessionEntry{
		ChannelID:    2,
		ChannelKeyID: 20,
		Timestamp:    time.Now().Add(-time.Hour),
	})

	group := model.Group{
		Mode:            model.GroupModeFailover,
		SessionKeepTime: 1,
		SessionKeepMode: model.GroupSessionKeepModeTTL,
		Items: []model.GroupItem{
			{ChannelID: 1, Priority: 1},
			{ChannelID: 2, Priority: 2},
		},
	}

	iter := NewIterator(group, 7, "gpt-test")
	if !iter.Next() {
		t.Fatal("expected first candidate")
	}
	if got := iter.Item().ChannelID; got != 1 {
		t.Fatalf("first candidate = %d, want 1", got)
	}
	if iter.IsSticky() {
		t.Fatal("expired ttl sticky should not be marked sticky")
	}
}

func TestNewIteratorUntilFailureStickyDoesNotExpireByTime(t *testing.T) {
	Reset()
	globalSession.Store(sessionKey(7, "gpt-test"), &SessionEntry{
		ChannelID:    2,
		ChannelKeyID: 20,
		Timestamp:    time.Now().Add(-time.Hour),
	})

	group := model.Group{
		Mode:            model.GroupModeFailover,
		SessionKeepTime: 1,
		SessionKeepMode: model.GroupSessionKeepModeUntilFailure,
		Items: []model.GroupItem{
			{ChannelID: 1, Priority: 1},
			{ChannelID: 2, Priority: 2},
		},
	}

	iter := NewIterator(group, 7, "gpt-test")
	if !iter.Next() {
		t.Fatal("expected first candidate")
	}
	if got := iter.Item().ChannelID; got != 2 {
		t.Fatalf("first candidate = %d, want sticky channel 2", got)
	}
	if !iter.IsSticky() || iter.StickyKeyID() != 20 {
		t.Fatalf("expected until_failure sticky key 20, sticky=%t key=%d", iter.IsSticky(), iter.StickyKeyID())
	}
}

func TestNewIteratorUntilFailureNonPaidStickyStaysAheadOfLowerPaidRatio(t *testing.T) {
	Reset()
	SetSticky(7, "gpt-test", 2, 20)

	group := model.Group{
		Mode:                  model.GroupModeFailover,
		SessionKeepMode:       model.GroupSessionKeepModeUntilFailure,
		PaidSiteLowRatioFirst: true,
		Items: []model.GroupItem{
			{ChannelID: 1, Priority: 1, PaidSite: true, SiteGroupRatio: ratio(0.5)},
			{ChannelID: 2, Priority: 2, PaidSite: false},
		},
	}

	iter := NewIterator(group, 7, "gpt-test")
	if !iter.Next() {
		t.Fatal("expected first candidate")
	}
	if got := iter.Item().ChannelID; got != 2 {
		t.Fatalf("first candidate = %d, want non-paid sticky channel 2", got)
	}
	if !iter.IsSticky() {
		t.Fatal("expected non-paid sticky to remain sticky")
	}
}

func TestNewIteratorUntilFailurePaidStickyYieldsToLowerPaidRatio(t *testing.T) {
	Reset()
	SetSticky(7, "gpt-test", 2, 20)

	group := model.Group{
		Mode:                  model.GroupModeFailover,
		SessionKeepMode:       model.GroupSessionKeepModeUntilFailure,
		PaidSiteLowRatioFirst: true,
		Items: []model.GroupItem{
			{ChannelID: 1, Priority: 1, PaidSite: true, SiteGroupRatio: ratio(0.5)},
			{ChannelID: 2, Priority: 2, PaidSite: true, SiteGroupRatio: ratio(1.0)},
		},
	}

	iter := NewIterator(group, 7, "gpt-test")
	if !iter.Next() {
		t.Fatal("expected first candidate")
	}
	if got := iter.Item().ChannelID; got != 1 {
		t.Fatalf("first candidate = %d, want lower-ratio paid channel 1", got)
	}
	if iter.IsSticky() {
		t.Fatal("lower-ratio paid candidate should not be old sticky")
	}
	if !iter.Next() {
		t.Fatal("expected old sticky candidate")
	}
	if got := iter.Item().ChannelID; got != 2 {
		t.Fatalf("second candidate = %d, want old sticky channel 2", got)
	}
	if !iter.IsSticky() {
		t.Fatal("old sticky should still be marked when reached after lower-ratio candidates")
	}
}

func TestNewIteratorPreferredAlwaysMovesFirst(t *testing.T) {
	Reset()

	group := model.Group{
		Mode:                  model.GroupModeFailover,
		SessionKeepMode:       model.GroupSessionKeepModeUntilFailure,
		PaidSiteLowRatioFirst: true,
		Items: []model.GroupItem{
			{ChannelID: 1, Priority: 1, PaidSite: true, SiteGroupRatio: ratio(0.5)},
			{ChannelID: 2, Priority: 2, PaidSite: true, SiteGroupRatio: ratio(1.0)},
		},
	}

	iter := NewIteratorWithPreference(group, 7, "gpt-test", &SessionEntry{ChannelID: 2, ChannelKeyID: 20})
	if !iter.Next() {
		t.Fatal("expected first candidate")
	}
	if got := iter.Item().ChannelID; got != 2 {
		t.Fatalf("first candidate = %d, want preferred channel 2", got)
	}
	if !iter.IsSticky() || iter.StickyKeyID() != 20 {
		t.Fatalf("expected preferred sticky key 20, sticky=%t key=%d", iter.IsSticky(), iter.StickyKeyID())
	}
}

func TestClearStickyOnFailureDeletesUntilFailureSticky(t *testing.T) {
	Reset()
	SetSticky(7, "gpt-test", 2, 20)

	group := model.Group{
		Mode:            model.GroupModeFailover,
		SessionKeepMode: model.GroupSessionKeepModeUntilFailure,
		Items: []model.GroupItem{
			{ChannelID: 1, Priority: 1},
			{ChannelID: 2, Priority: 2},
		},
	}

	iter := NewIterator(group, 7, "gpt-test")
	if !iter.Next() {
		t.Fatal("expected first candidate")
	}
	if !iter.IsSticky() {
		t.Fatal("expected sticky candidate before clear")
	}
	iter.ClearStickyOnFailure()
	if entry := GetStickyUntilFailure(7, "gpt-test"); entry != nil {
		t.Fatalf("expected sticky to be deleted, got %#v", entry)
	}
	if iter.IsSticky() {
		t.Fatal("iterator should no longer mark current candidate as sticky after clear")
	}
}
