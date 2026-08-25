//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaProbeSwitchTrackerStub struct {
	candidates []OpenAIOfficial7dResetCandidate
	handled    bool
	pendingErr error
}

func (s *quotaProbeSwitchTrackerStub) ObserveOpenAI7dReset(context.Context, int64, time.Time, time.Time, time.Duration, time.Duration) (OpenAIOfficial7dResetObservation, error) {
	return OpenAIOfficial7dResetObservation{}, nil
}

func (s *quotaProbeSwitchTrackerStub) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	if s.pendingErr != nil {
		return nil, s.pendingErr
	}
	pending := make([]OpenAIOfficial7dResetState, 0, len(s.candidates))
	eligibleAccountIDs := make([]int64, 0, len(s.candidates))
	for _, candidate := range s.candidates {
		eligibleAccountIDs = append(eligibleAccountIDs, candidate.AccountID)
	}
	for _, candidate := range s.candidates {
		if candidate.Pending && candidate.DetectedAt != nil {
			event := trustedOfficialResetEvent(candidate.AccountID, *candidate.DetectedAt, eligibleAccountIDs...)
			event.HandledAt = candidate.HandledAt
			pending = append(pending, event)
		}
	}
	return pending, nil
}

func (s *quotaProbeSwitchTrackerStub) ListEligibleOpenAIOfficial7dResetCandidates(context.Context, time.Time) ([]OpenAIOfficial7dResetCandidate, error) {
	return append([]OpenAIOfficial7dResetCandidate(nil), s.candidates...), nil
}

func (s *quotaProbeSwitchTrackerStub) MarkAllOpenAIOfficial7dResetsHandled(_ context.Context, handledAt time.Time) error {
	s.handled = true
	for i := range s.candidates {
		s.candidates[i].Pending = false
		handledAtCopy := handledAt
		s.candidates[i].HandledAt = &handledAtCopy
	}
	return nil
}

func (s *quotaProbeSwitchTrackerStub) MarkOpenAIOfficial7dResetRoundHandled(_ context.Context, handledAt time.Time, events []OpenAIOfficial7dResetState) (int, error) {
	s.handled = true
	consumed := make(map[int64]struct{}, len(events))
	for _, event := range events {
		consumed[event.AccountID] = struct{}{}
	}
	for i := range s.candidates {
		if _, ok := consumed[s.candidates[i].AccountID]; ok {
			s.candidates[i].Pending = false
			s.candidates[i].DetectedAt = nil
		}
		handledAtCopy := handledAt
		s.candidates[i].HandledAt = &handledAtCopy
	}
	return len(events), nil
}

func (s *quotaProbeSwitchTrackerStub) ClearOpenAIOfficial7dResetPending(_ context.Context, accountID int64, detectedAt time.Time) (bool, error) {
	for i := range s.candidates {
		candidate := &s.candidates[i]
		if candidate.AccountID == accountID && candidate.Pending && candidate.DetectedAt != nil && candidate.DetectedAt.Equal(detectedAt) {
			candidate.Pending = false
			candidate.DetectedAt = nil
			return true, nil
		}
	}
	return false, nil
}

type quotaProbeSwitchQuerierStub struct {
	tracker          *quotaProbeSwitchTrackerStub
	detectedAccounts map[int64]bool
	settings         *quotaProbeSwitchSettingsStub
	setEnabled       *bool
	queried          []int64
	now              time.Time
}

func (s *quotaProbeSwitchQuerierStub) QueryUsageSnapshot(_ context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	s.queried = append(s.queried, accountID)
	for i := range s.tracker.candidates {
		if s.tracker.candidates[i].AccountID != accountID {
			continue
		}
		observedAt := s.now
		s.tracker.candidates[i].QuotaObservedAt = &observedAt
		if s.detectedAccounts[accountID] {
			s.tracker.candidates[i].Pending = true
			s.tracker.candidates[i].DetectedAt = &observedAt
		}
	}
	if s.settings != nil && s.setEnabled != nil {
		s.settings.enabled = *s.setEnabled
	}
	return &OpenAIQuotaUsage{}, nil
}

type quotaProbeSwitchSettingsStub struct {
	enabled bool
}

func (s *quotaProbeSwitchSettingsStub) GetOpenAIOfficialQuotaAutoResetEnabled(context.Context) (bool, error) {
	return s.enabled, nil
}

func (s *quotaProbeSwitchSettingsStub) SetOpenAIOfficialQuotaAutoResetEnabled(_ context.Context, enabled bool) error {
	s.enabled = enabled
	return nil
}

type quotaProbeSwitchUserSubRepoStub struct {
	userSubRepoNoop
	active   []UserSubscription
	resetIDs []int64
}

func (s *quotaProbeSwitchUserSubRepoStub) ListAllActiveForQuotaReset(context.Context, time.Time) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), s.active...), nil
}

func (s *quotaProbeSwitchUserSubRepoStub) ResetFiveHourUsage(_ context.Context, id int64, _ time.Time) error {
	s.resetIDs = append(s.resetIDs, id)
	return nil
}

func (s *quotaProbeSwitchUserSubRepoStub) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time, time.Time) error {
	return nil
}

func newQuotaProbeSwitchRunner(
	candidates []OpenAIOfficial7dResetCandidate,
	detectedAccounts map[int64]bool,
	automationEnabled bool,
) (*OpenAIOfficialQuotaResetRunner, *quotaProbeSwitchTrackerStub, *quotaProbeSwitchQuerierStub, *quotaProbeSwitchSettingsStub, *quotaProbeSwitchUserSubRepoStub) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	tracker := &quotaProbeSwitchTrackerStub{candidates: candidates}
	settings := &quotaProbeSwitchSettingsStub{enabled: automationEnabled}
	querier := &quotaProbeSwitchQuerierStub{
		tracker:          tracker,
		detectedAccounts: detectedAccounts,
		settings:         settings,
		now:              now,
	}
	subRepo := &quotaProbeSwitchUserSubRepoStub{
		active: []UserSubscription{{ID: 11, UserID: 101, GroupID: 201}},
	}
	subscriptionService := NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil)
	quotaResetService := NewSubscriptionQuotaResetService(subscriptionService, tracker, settings)
	runner := NewOpenAIOfficialQuotaResetRunner(tracker, querier, quotaResetService, openAIOfficialQuotaResetCronSchedule)
	runner.now = func() time.Time { return now }
	return runner, tracker, querier, settings, subRepo
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_DisabledStillProbesAndKeepsReadyPending(t *testing.T) {
	runner, tracker, querier, _, subRepo := newQuotaProbeSwitchRunner(
		[]OpenAIOfficial7dResetCandidate{{AccountID: 1}},
		map[int64]bool{1: true},
		false,
	)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Equal(t, []int64{1}, querier.queried)
	require.True(t, tracker.candidates[0].Pending)
	require.False(t, tracker.handled)
	require.Empty(t, subRepo.resetIDs)
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_DisabledKeepsMultiAccountReadyWithoutReprobe(t *testing.T) {
	detectedAt := time.Date(2026, 8, 11, 9, 55, 0, 0, time.UTC)
	runner, tracker, querier, _, subRepo := newQuotaProbeSwitchRunner(
		[]OpenAIOfficial7dResetCandidate{
			{AccountID: 1, Pending: true, DetectedAt: &detectedAt},
			{AccountID: 2, Pending: true, DetectedAt: &detectedAt},
		},
		nil,
		false,
	)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.Empty(t, querier.queried)
	require.True(t, tracker.candidates[0].Pending)
	require.True(t, tracker.candidates[1].Pending)
	require.False(t, tracker.handled)
	require.Empty(t, subRepo.resetIDs)
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_EnablingAfterReadyResetsOnNextCycle(t *testing.T) {
	runner, tracker, querier, settings, subRepo := newQuotaProbeSwitchRunner(
		[]OpenAIOfficial7dResetCandidate{{AccountID: 1}},
		map[int64]bool{1: true},
		false,
	)

	require.NoError(t, runner.RunOnce(context.Background()))
	require.True(t, tracker.candidates[0].Pending)
	settings.enabled = true
	require.NoError(t, runner.RunOnce(context.Background()))

	require.Equal(t, []int64{1}, querier.queried)
	require.Equal(t, []int64{11}, subRepo.resetIDs)
	require.True(t, tracker.handled)
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_UsesRefreshedSwitchBeforeReset(t *testing.T) {
	runner, tracker, querier, settings, subRepo := newQuotaProbeSwitchRunner(
		[]OpenAIOfficial7dResetCandidate{{AccountID: 1}},
		map[int64]bool{1: true},
		true,
	)
	disabled := false
	querier.setEnabled = &disabled

	require.NoError(t, runner.RunOnce(context.Background()))
	require.False(t, settings.enabled)
	require.True(t, tracker.candidates[0].Pending)
	require.False(t, tracker.handled)
	require.Empty(t, subRepo.resetIDs)
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_MissingDependenciesAreNoop(t *testing.T) {
	var nilRunner *OpenAIOfficialQuotaResetRunner
	require.NoError(t, nilRunner.RunOnce(context.Background()))
	require.NoError(t, (&OpenAIOfficialQuotaResetRunner{}).RunOnce(context.Background()))
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_NoEligibleWorkDoesNotProbe(t *testing.T) {
	t.Run("no active subscriptions", func(t *testing.T) {
		runner, _, querier, _, subRepo := newQuotaProbeSwitchRunner(
			[]OpenAIOfficial7dResetCandidate{{AccountID: 1}},
			map[int64]bool{1: true},
			false,
		)
		subRepo.active = nil

		require.NoError(t, runner.RunOnce(context.Background()))
		require.Empty(t, querier.queried)
	})

	t.Run("no eligible accounts", func(t *testing.T) {
		runner, _, querier, _, _ := newQuotaProbeSwitchRunner(nil, nil, false)

		require.NoError(t, runner.RunOnce(context.Background()))
		require.Empty(t, querier.queried)
	})
}

func TestOpenAIOfficialQuotaResetRunnerSwitch_StatusFailureStopsProbe(t *testing.T) {
	runner, tracker, querier, _, _ := newQuotaProbeSwitchRunner(
		[]OpenAIOfficial7dResetCandidate{{AccountID: 1}},
		map[int64]bool{1: true},
		false,
	)
	tracker.pendingErr = errors.New("status failed")

	require.EqualError(t, runner.RunOnce(context.Background()), "status failed")
	require.Empty(t, querier.queried)
}
