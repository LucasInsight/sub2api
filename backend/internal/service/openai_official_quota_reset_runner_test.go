//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type automaticResetTrackerStub struct {
	candidates []OpenAIOfficial7dResetCandidate
	handled    bool
}

func (r *automaticResetTrackerStub) ObserveOpenAI7dReset(context.Context, int64, time.Time, time.Time, time.Duration) (OpenAIOfficial7dResetObservation, error) {
	return OpenAIOfficial7dResetObservation{}, nil
}

func (r *automaticResetTrackerStub) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	pending := make([]OpenAIOfficial7dResetState, 0, len(r.candidates))
	for i := range r.candidates {
		candidate := r.candidates[i]
		if !candidate.Pending || candidate.DetectedAt == nil {
			continue
		}
		pending = append(pending, OpenAIOfficial7dResetState{AccountID: candidate.AccountID, DetectedAt: *candidate.DetectedAt})
	}
	return pending, nil
}

func (r *automaticResetTrackerStub) ListEligibleOpenAIOfficial7dResetCandidates(context.Context, time.Time) ([]OpenAIOfficial7dResetCandidate, error) {
	return append([]OpenAIOfficial7dResetCandidate(nil), r.candidates...), nil
}

func (r *automaticResetTrackerStub) MarkAllOpenAIOfficial7dResetsHandled(_ context.Context, handledAt time.Time) error {
	r.handled = true
	for i := range r.candidates {
		r.candidates[i].Pending = false
		handled := handledAt
		r.candidates[i].HandledAt = &handled
	}
	return nil
}

func (r *automaticResetTrackerStub) ClearOpenAIOfficial7dResetPending(context.Context, int64, time.Time) (bool, error) {
	return false, nil
}

type automaticQuotaQuerierStub struct {
	tracker *automaticResetTrackerStub
	pending map[int64]bool
	fail    map[int64]error
	queried []int64
	now     time.Time
}

func (q *automaticQuotaQuerierStub) QueryUsageSnapshot(_ context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	q.queried = append(q.queried, accountID)
	if err := q.fail[accountID]; err != nil {
		return nil, err
	}
	for i := range q.tracker.candidates {
		if q.tracker.candidates[i].AccountID != accountID {
			continue
		}
		observedAt := q.now
		q.tracker.candidates[i].QuotaObservedAt = &observedAt
		coolingDown := q.tracker.candidates[i].HandledAt != nil &&
			q.now.Before(q.tracker.candidates[i].HandledAt.Add(2*time.Hour))
		if q.pending[accountID] && !coolingDown {
			q.tracker.candidates[i].Pending = true
			q.tracker.candidates[i].DetectedAt = &observedAt
		}
	}
	return &OpenAIQuotaUsage{}, nil
}

type quotaResetAutomationSettingsStub struct {
	enabled bool
}

func (s *quotaResetAutomationSettingsStub) GetOpenAIOfficialQuotaAutoResetEnabled(context.Context) (bool, error) {
	return s.enabled, nil
}

func (s *quotaResetAutomationSettingsStub) SetOpenAIOfficialQuotaAutoResetEnabled(_ context.Context, enabled bool) error {
	s.enabled = enabled
	return nil
}

func newAutomaticResetRunnerTestService(
	tracker *automaticResetTrackerStub,
	querier *automaticQuotaQuerierStub,
	automationEnabled bool,
) (*OpenAIOfficialQuotaResetRunner, *resetAllQuotaUserSubRepoStub, *SubscriptionQuotaResetService) {
	subRepo := &resetAllQuotaUserSubRepoStub{active: []UserSubscription{{ID: 11, UserID: 101, GroupID: 201}}}
	subscriptionService := NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil)
	quotaResetService := NewSubscriptionQuotaResetService(
		subscriptionService,
		tracker,
		&quotaResetAutomationSettingsStub{enabled: automationEnabled},
	)
	runner := NewOpenAIOfficialQuotaResetRunner(tracker, querier, quotaResetService, 5*time.Minute)
	runner.now = func() time.Time { return querier.now }
	return runner, subRepo, quotaResetService
}

func TestOpenAIOfficialQuotaResetRunner_DisabledProbesWithoutReset(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{{AccountID: 1}}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{1: true}, fail: map[int64]error{}, now: now}
	runner, subRepo, _ := newAutomaticResetRunnerTestService(tracker, querier, false)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{1}, querier.queried)
	require.Empty(t, subRepo.fiveHourIDs)
}

func TestOpenAIOfficialQuotaResetRunner_SingleAccountBecomesReadyAndResets(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{{AccountID: 1}}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{1: true}, fail: map[int64]error{}, now: now}
	runner, subRepo, quotaResetService := newAutomaticResetRunnerTestService(tracker, querier, true)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{1}, querier.queried)
	require.Equal(t, []int64{11}, subRepo.fiveHourIDs)
	require.True(t, tracker.handled)
	status, err := quotaResetService.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.AutomaticResetReady)
}

func TestOpenAIOfficialQuotaResetRunner_MultipleAccountsNeedTwoConfirmations(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{{AccountID: 1}, {AccountID: 2}}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{1: true}, fail: map[int64]error{}, now: now}
	runner, subRepo, quotaResetService := newAutomaticResetRunnerTestService(tracker, querier, true)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.ElementsMatch(t, []int64{1, 2}, querier.queried)
	require.Empty(t, subRepo.fiveHourIDs)
	status, err := quotaResetService.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.AutomaticResetReady)
}

func TestOpenAIOfficialQuotaResetRunner_TwoAccountsBecomeReadyAndResetOnce(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{{AccountID: 1}, {AccountID: 2}, {AccountID: 3}}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{1: true, 2: true}, fail: map[int64]error{}, now: now}
	runner, subRepo, quotaResetService := newAutomaticResetRunnerTestService(tracker, querier, true)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{11}, subRepo.fiveHourIDs)
	require.True(t, tracker.handled)
	status, err := quotaResetService.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.AutomaticResetReady)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{11}, subRepo.fiveHourIDs)
}

func TestOpenAIOfficialQuotaResetRunner_AlreadyReadyResetsWithoutProbing(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	detectedAt := now.Add(-time.Minute)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{
		{AccountID: 1, Pending: true, DetectedAt: &detectedAt},
		{AccountID: 2, Pending: true, DetectedAt: &detectedAt},
	}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{}, fail: map[int64]error{}, now: now}
	runner, subRepo, quotaResetService := newAutomaticResetRunnerTestService(tracker, querier, true)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Empty(t, querier.queried)
	require.Equal(t, []int64{11}, subRepo.fiveHourIDs)
	require.True(t, tracker.handled)
	status, err := quotaResetService.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.AutomaticResetReady)
}

func TestOpenAIOfficialQuotaResetRunner_HandlesEveryAccountAndAuditsOnce(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	detectedAt := now.Add(-time.Minute)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{
		{AccountID: 1, Pending: true, DetectedAt: &detectedAt},
		{AccountID: 2, Pending: true, DetectedAt: &detectedAt},
		{AccountID: 3},
	}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{}, fail: map[int64]error{}, now: now}
	runner, _, _ := newAutomaticResetRunnerTestService(tracker, querier, true)
	auditService := NewAuditLogService(nil, nil)
	runner.auditService = auditService

	require.NoError(t, runner.RunOnce(context.Background()))
	for i := range tracker.candidates {
		require.False(t, tracker.candidates[i].Pending)
		require.NotNil(t, tracker.candidates[i].HandledAt)
	}
	require.Len(t, auditService.queue, 1)
	entry := <-auditService.queue
	require.Equal(t, openAIOfficialAutoResetAuditAction, entry.Action)
	require.Equal(t, "system", entry.ActorEmail)
	require.Equal(t, []int64{1, 2}, entry.Extra["confirmed_account_ids"])
	require.Equal(t, 1, entry.Extra["reset_count"])
	require.Equal(t, 2, entry.Extra["confirmation_count"])
	require.Equal(t, 2, entry.Extra["consumed_event_count"])
}

func TestOpenAIOfficialQuotaResetRunner_ResetFailureRetainsPendingForRetry(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	detectedAt := now.Add(-time.Minute)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{
		{AccountID: 1, Pending: true, DetectedAt: &detectedAt},
	}}
	querier := &automaticQuotaQuerierStub{tracker: tracker, pending: map[int64]bool{}, fail: map[int64]error{}, now: now}
	runner, subRepo, _ := newAutomaticResetRunnerTestService(tracker, querier, true)
	subRepo.failOnID = 11

	require.EqualError(t, runner.RunOnce(context.Background()), "reset failed")
	require.True(t, tracker.candidates[0].Pending)
	require.False(t, tracker.handled)

	subRepo.failOnID = 0
	require.NoError(t, runner.RunOnce(context.Background()))
	require.False(t, tracker.candidates[0].Pending)
	require.True(t, tracker.handled)
}

func TestOpenAIOfficialQuotaResetRunner_ProbeFailureDoesNotLowerThreshold(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{{AccountID: 1}, {AccountID: 2}}}
	querier := &automaticQuotaQuerierStub{
		tracker: tracker,
		pending: map[int64]bool{1: true},
		fail:    map[int64]error{2: errors.New("probe failed")},
		now:     now,
	}
	runner, subRepo, quotaResetService := newAutomaticResetRunnerTestService(tracker, querier, true)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Empty(t, subRepo.fiveHourIDs)
	status, err := quotaResetService.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.AutomaticResetReady)
}

func TestOpenAIOfficialQuotaResetRunner_RotatesPastFailedAccounts(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	tracker := &automaticResetTrackerStub{candidates: []OpenAIOfficial7dResetCandidate{
		{AccountID: 1}, {AccountID: 2}, {AccountID: 3}, {AccountID: 4},
	}}
	querier := &automaticQuotaQuerierStub{
		tracker: tracker,
		pending: map[int64]bool{4: true},
		fail: map[int64]error{
			1: errors.New("probe failed"),
			2: errors.New("probe failed"),
			3: errors.New("probe failed"),
		},
		now: now,
	}
	runner, _, quotaResetService := newAutomaticResetRunnerTestService(tracker, querier, true)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{1, 2, 3}, querier.queried)
	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{1, 2, 3, 4, 1, 2}, querier.queried)

	status, err := quotaResetService.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, status.ConfirmationCount)
	require.False(t, status.AutomaticResetReady)
}
