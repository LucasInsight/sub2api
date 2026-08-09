//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type resetAllQuotaUserSubRepoStub struct {
	userSubRepoNoop
	active           []UserSubscription
	fiveHourIDs      []int64
	usageWindowIDs   []int64
	usageWindowFlags [][3]bool
	fiveHourStarts   []time.Time
	dailyStarts      []time.Time
	periodicStarts   []time.Time
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

func (r *resetAllQuotaUserSubRepoStub) ResetUsageWindows(_ context.Context, id int64, daily, weekly, monthly bool, dailyStart, periodicStart time.Time) error {
	r.usageWindowIDs = append(r.usageWindowIDs, id)
	r.usageWindowFlags = append(r.usageWindowFlags, [3]bool{daily, weekly, monthly})
	r.dailyStarts = append(r.dailyStarts, dailyStart)
	r.periodicStarts = append(r.periodicStarts, periodicStart)
	return nil
}

type official7dResetRepoStub struct {
	pending           []OpenAIOfficial7dResetState
	candidates        []OpenAIOfficial7dResetCandidate
	handledAt         time.Time
	markHandled       bool
	clearResult       bool
	clearErr          error
	clearedAccountID  int64
	clearedDetectedAt time.Time
}

func (r *official7dResetRepoStub) ObserveOpenAI7dReset(context.Context, int64, time.Time, time.Time, time.Duration) (OpenAIOfficial7dResetObservation, error) {
	panic("unexpected ObserveOpenAI7dReset call")
}

func (r *official7dResetRepoStub) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	return append([]OpenAIOfficial7dResetState(nil), r.pending...), nil
}

func (r *official7dResetRepoStub) ListEligibleOpenAIOfficial7dResetCandidates(context.Context, time.Time) ([]OpenAIOfficial7dResetCandidate, error) {
	return append([]OpenAIOfficial7dResetCandidate(nil), r.candidates...), nil
}

func (r *official7dResetRepoStub) MarkAllOpenAIOfficial7dResetsHandled(_ context.Context, handledAt time.Time) error {
	r.markHandled = true
	r.handledAt = handledAt
	return nil
}

func (r *official7dResetRepoStub) ClearOpenAIOfficial7dResetPending(_ context.Context, accountID int64, detectedAt time.Time) (bool, error) {
	r.clearedAccountID = accountID
	r.clearedDetectedAt = detectedAt
	return r.clearResult, r.clearErr
}

func newResetAllQuotaService(subRepo *resetAllQuotaUserSubRepoStub, tracker *official7dResetRepoStub) *SubscriptionQuotaResetService {
	subscriptionService := NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil)
	return NewSubscriptionQuotaResetService(subscriptionService, tracker, nil)
}

func TestAdminResetAllQuota_ReusesAllWindowResetMethods(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{
		{ID: 11, UserID: 101, GroupID: 201},
		{ID: 12, UserID: 102, GroupID: 202},
	}}
	tracker := &official7dResetRepoStub{pending: []OpenAIOfficial7dResetState{{AccountID: 7, DetectedAt: time.Now()}}}
	svc := newResetAllQuotaService(subRepo, tracker)

	result, err := svc.AdminResetAllQuota(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, 2, result.ResetCount)
	require.Equal(t, 1, result.ConsumedEventCount)
	require.Equal(t, []int64{11, 12}, subRepo.fiveHourIDs)
	require.Equal(t, []int64{11, 12}, subRepo.usageWindowIDs)
	require.Equal(t, [][3]bool{{true, true, true}, {true, true, true}}, subRepo.usageWindowFlags)
	require.Equal(t, subRepo.fiveHourStarts[0], subRepo.fiveHourStarts[1])
	require.Equal(t, subRepo.dailyStarts[0], subRepo.dailyStarts[1])
	require.Equal(t, subRepo.periodicStarts[0], subRepo.periodicStarts[1])
	require.Equal(t, subRepo.fiveHourStarts[0], subRepo.periodicStarts[0])
	require.Equal(t, timezone.StartOfDay(subRepo.fiveHourStarts[0]), subRepo.dailyStarts[0])
	require.True(t, tracker.markHandled)
}

func TestAdminResetAllQuota_AllowsResetWithoutPendingOfficialReset(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11}}}
	tracker := &official7dResetRepoStub{}
	svc := newResetAllQuotaService(subRepo, tracker)

	result, err := svc.AdminResetAllQuota(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, 1, result.ResetCount)
	require.Zero(t, result.ConsumedEventCount)
	require.Equal(t, []int64{11}, subRepo.fiveHourIDs)
	require.Equal(t, []int64{11}, subRepo.usageWindowIDs)
	require.True(t, tracker.markHandled)
}

func TestAdminResetAllQuota_DoesNotConsumeEventWhenResetFails(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{
		active:   []UserSubscription{{ID: 11}, {ID: 12}},
		failOnID: 12,
	}
	tracker := &official7dResetRepoStub{pending: []OpenAIOfficial7dResetState{{AccountID: 7, DetectedAt: time.Now()}}}
	svc := newResetAllQuotaService(subRepo, tracker)

	_, err := svc.AdminResetAllQuota(context.Background(), true)

	require.EqualError(t, err, "reset failed")
	require.False(t, tracker.markHandled)
}

func TestAdminResetAllQuota_RequiresServerAcknowledgement(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11}}}
	tracker := &official7dResetRepoStub{}
	svc := newResetAllQuotaService(subRepo, tracker)

	_, err := svc.AdminResetAllQuota(context.Background(), false)

	require.ErrorIs(t, err, ErrResetAllQuotaAckRequired)
	require.Empty(t, subRepo.fiveHourIDs)
	require.False(t, tracker.markHandled)
}

func TestAdminResetAllQuotaStatus_DependsOnlyOnActiveSubscriptions(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11}}}
	detectedAt := time.Now()
	tracker := &official7dResetRepoStub{
		pending: []OpenAIOfficial7dResetState{{AccountID: 1, AccountName: "primary", DetectedAt: detectedAt}},
		candidates: []OpenAIOfficial7dResetCandidate{
			{AccountID: 1, Pending: true, DetectedAt: &detectedAt},
			{AccountID: 2},
		},
	}
	svc := newResetAllQuotaService(subRepo, tracker)

	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.Empty(t, status.DisabledReason)
	require.Equal(t, 1, status.PendingEventCount)
	require.Equal(t, []AdminQuotaResetPendingEvent{{
		AccountID:   1,
		AccountName: "primary",
		DetectedAt:  detectedAt,
	}}, status.PendingEvents)
	require.Equal(t, 2, status.EligibleAccountCount)
	require.Equal(t, 1, status.ConfirmationCount)
	require.Equal(t, 2, status.RequiredConfirmationCount)
	require.False(t, status.AutomaticResetReady)

	subRepo.active = nil
	status, err = svc.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Equal(t, "no_active_subscriptions", status.DisabledReason)
}

func TestClearFalsePositiveOpenAIResetPending_UsesExactEventIdentity(t *testing.T) {
	detectedAt := time.Date(2026, 8, 9, 7, 29, 36, 0, time.UTC)
	tracker := &official7dResetRepoStub{clearResult: true}
	svc := newResetAllQuotaService(&resetAllQuotaUserSubRepoStub{}, tracker)

	err := svc.ClearFalsePositiveOpenAIResetPending(context.Background(), 17, detectedAt)

	require.NoError(t, err)
	require.Equal(t, int64(17), tracker.clearedAccountID)
	require.Equal(t, detectedAt, tracker.clearedDetectedAt)
}

func TestClearFalsePositiveOpenAIResetPending_RejectsChangedEvent(t *testing.T) {
	detectedAt := time.Date(2026, 8, 9, 7, 29, 36, 0, time.UTC)
	tracker := &official7dResetRepoStub{clearResult: false}
	svc := newResetAllQuotaService(&resetAllQuotaUserSubRepoStub{}, tracker)

	err := svc.ClearFalsePositiveOpenAIResetPending(context.Background(), 17, detectedAt)

	require.ErrorIs(t, err, ErrOpenAIOfficialResetPendingChanged)
}

func TestAutomaticResetAllQuota_IsDisabledByMasterSwitch(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11}}}
	tracker := &official7dResetRepoStub{}
	settings := NewSettingService(&panelRateLimitSettingRepo{values: map[string]string{}}, nil)
	svc := NewSubscriptionQuotaResetService(
		NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil),
		tracker,
		settings,
	)

	_, err := svc.AutomaticResetAllQuota(context.Background(), []int64{1})

	require.ErrorIs(t, err, ErrQuotaResetAutomationDisabled)
	require.Empty(t, subRepo.fiveHourIDs)
}

func TestAutomaticResetAllQuota_ExecutesWhenEnabledAndConfirmed(t *testing.T) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11}}}
	detectedAt := time.Now()
	tracker := &official7dResetRepoStub{
		pending:    []OpenAIOfficial7dResetState{{AccountID: 1, DetectedAt: detectedAt}},
		candidates: []OpenAIOfficial7dResetCandidate{{AccountID: 1, Pending: true, DetectedAt: &detectedAt}},
	}
	settingsRepo := &panelRateLimitSettingRepo{values: map[string]string{
		SettingKeyOpenAIOfficialQuotaAutoResetEnabled: "true",
	}}
	svc := NewSubscriptionQuotaResetService(
		NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil),
		tracker,
		NewSettingService(settingsRepo, nil),
	)

	result, err := svc.AutomaticResetAllQuota(context.Background(), []int64{1})

	require.NoError(t, err)
	require.Equal(t, 1, result.ResetCount)
	require.Equal(t, 1, result.ConfirmationCount)
	require.Equal(t, []int64{11}, subRepo.fiveHourIDs)
	require.True(t, tracker.markHandled)
}
