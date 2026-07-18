//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type resetAllQuotaUserSubRepoStub struct {
	userSubRepoNoop
	active           []UserSubscription
	fiveHourIDs      []int64
	usageWindowIDs   []int64
	usageWindowFlags [][3]bool
	fiveHourStarts   []time.Time
	calendarStarts   []time.Time
	failOnID         int64
}

func (r *resetAllQuotaUserSubRepoStub) ListAllActiveForQuotaReset(context.Context, time.Time) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), r.active...), nil
}

func (r *resetAllQuotaUserSubRepoStub) ResetFiveHourUsage(_ context.Context, id int64, start time.Time) error {
	r.fiveHourIDs = append(r.fiveHourIDs, id)
	r.fiveHourStarts = append(r.fiveHourStarts, start)
	if id == r.failOnID {
		return errors.New("reset failed")
	}
	return nil
}

func (r *resetAllQuotaUserSubRepoStub) ResetUsageWindows(_ context.Context, id int64, daily, weekly, monthly bool, start time.Time) error {
	r.usageWindowIDs = append(r.usageWindowIDs, id)
	r.usageWindowFlags = append(r.usageWindowFlags, [3]bool{daily, weekly, monthly})
	r.calendarStarts = append(r.calendarStarts, start)
	return nil
}

type official7dResetRepoStub struct {
	pending     []OpenAIOfficial7dResetState
	handledIDs  []int64
	handledAt   time.Time
	markHandled bool
}

func (r *official7dResetRepoStub) ObserveOpenAI7dReset(context.Context, int64, time.Time, time.Time, time.Duration) (bool, error) {
	panic("unexpected ObserveOpenAI7dReset call")
}

func (r *official7dResetRepoStub) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	return append([]OpenAIOfficial7dResetState(nil), r.pending...), nil
}

func (r *official7dResetRepoStub) MarkOpenAIOfficial7dResetsHandled(_ context.Context, ids []int64, handledAt time.Time) error {
	r.markHandled = true
	r.handledIDs = append([]int64(nil), ids...)
	r.handledAt = handledAt
	return nil
}

func newResetAllQuotaService(subRepo *resetAllQuotaUserSubRepoStub, tracker *official7dResetRepoStub) *SubscriptionService {
	svc := NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil)
	svc.official7dResetRepo = tracker
	return svc
}

func TestAdminResetAllQuota_ReusesAllWindowResetMethods(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{
		{ID: 11, UserID: 101, GroupID: 201},
		{ID: 12, UserID: 102, GroupID: 202},
	}}
	tracker := &official7dResetRepoStub{pending: []OpenAIOfficial7dResetState{{AccountID: 7, DetectedAt: time.Now()}}}
	svc := newResetAllQuotaService(subRepo, tracker)

	result, err := svc.AdminResetAllQuota(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.ResetCount)
	require.Equal(t, 1, result.ConsumedEventCount)
	require.Equal(t, []int64{11, 12}, subRepo.fiveHourIDs)
	require.Equal(t, []int64{11, 12}, subRepo.usageWindowIDs)
	require.Equal(t, [][3]bool{{true, true, true}, {true, true, true}}, subRepo.usageWindowFlags)
	require.Equal(t, subRepo.fiveHourStarts[0], subRepo.fiveHourStarts[1])
	require.Equal(t, subRepo.calendarStarts[0], subRepo.calendarStarts[1])
	require.Equal(t, startOfDay(subRepo.fiveHourStarts[0]), subRepo.calendarStarts[0])
	require.True(t, tracker.markHandled)
	require.Equal(t, []int64{7}, tracker.handledIDs)
}

func TestAdminResetAllQuota_RequiresPendingOfficialReset(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11}}}
	tracker := &official7dResetRepoStub{}
	svc := newResetAllQuotaService(subRepo, tracker)

	_, err := svc.AdminResetAllQuota(context.Background())

	require.ErrorIs(t, err, ErrOfficialEarlyResetRequired)
	require.Empty(t, subRepo.fiveHourIDs)
	require.False(t, tracker.markHandled)
}

func TestAdminResetAllQuota_DoesNotConsumeEventWhenResetFails(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{
		active:   []UserSubscription{{ID: 11}, {ID: 12}},
		failOnID: 12,
	}
	tracker := &official7dResetRepoStub{pending: []OpenAIOfficial7dResetState{{AccountID: 7, DetectedAt: time.Now()}}}
	svc := newResetAllQuotaService(subRepo, tracker)

	_, err := svc.AdminResetAllQuota(context.Background())

	require.EqualError(t, err, "reset failed")
	require.False(t, tracker.markHandled)
}

func TestAdminResetAllQuotaStatus_DisablesWithoutEventOrSubscription(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{}
	tracker := &official7dResetRepoStub{}
	svc := newResetAllQuotaService(subRepo, tracker)

	status, err := svc.AdminResetAllQuotaStatus(context.Background())
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Equal(t, "no_early_7d_reset", status.DisabledReason)

	tracker.pending = []OpenAIOfficial7dResetState{{AccountID: 7, DetectedAt: time.Now()}}
	status, err = svc.AdminResetAllQuotaStatus(context.Background())
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Equal(t, "no_active_subscriptions", status.DisabledReason)
}
