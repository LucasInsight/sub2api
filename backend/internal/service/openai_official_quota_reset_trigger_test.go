//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type officialQuotaTriggerTrackerStub struct {
	mu                  sync.Mutex
	candidates          []OpenAIOfficial7dResetCandidate
	detectAccounts      map[int64]bool
	observeErr          error
	pendingErr          error
	pendingErrCall      int
	candidateErrCall    int
	pendingCalls        int
	candidateCalls      int
	markHandledCalls    int
	clearPendingChanged bool
}

func (s *officialQuotaTriggerTrackerStub) ObserveOpenAI7dReset(
	_ context.Context,
	accountID int64,
	observedAt, resetAt time.Time,
	_ time.Duration,
	_ time.Duration,
) (OpenAIOfficial7dResetObservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observeErr != nil {
		return OpenAIOfficial7dResetObservation{}, s.observeErr
	}
	observation := OpenAIOfficial7dResetObservation{ObservedAt: observedAt, ResetAt: resetAt}
	for i := range s.candidates {
		if s.candidates[i].AccountID != accountID {
			continue
		}
		observedAtCopy := observedAt
		s.candidates[i].QuotaObservedAt = &observedAtCopy
		if s.detectAccounts[accountID] && !s.candidates[i].Pending {
			s.candidates[i].Pending = true
			s.candidates[i].DetectedAt = &observedAtCopy
			observation.Detected = true
		}
		break
	}
	return observation, nil
}

func (s *officialQuotaTriggerTrackerStub) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCalls++
	if s.pendingErr != nil && (s.pendingErrCall == 0 || s.pendingCalls == s.pendingErrCall) {
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

func (s *officialQuotaTriggerTrackerStub) ListEligibleOpenAIOfficial7dResetCandidates(context.Context, time.Time) ([]OpenAIOfficial7dResetCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidateCalls++
	if s.candidateErrCall > 0 && s.candidateCalls == s.candidateErrCall {
		return nil, errors.New("candidate lookup failed")
	}
	return append([]OpenAIOfficial7dResetCandidate(nil), s.candidates...), nil
}

func (s *officialQuotaTriggerTrackerStub) MarkAllOpenAIOfficial7dResetsHandled(_ context.Context, handledAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markHandledCalls++
	for i := range s.candidates {
		s.candidates[i].Pending = false
		handledAtCopy := handledAt
		s.candidates[i].HandledAt = &handledAtCopy
	}
	return nil
}

func (s *officialQuotaTriggerTrackerStub) MarkOpenAIOfficial7dResetRoundHandled(_ context.Context, handledAt time.Time, events []OpenAIOfficial7dResetState) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markHandledCalls++
	confirmed := make(map[int64]struct{}, len(events))
	for _, event := range events {
		confirmed[event.AccountID] = struct{}{}
	}
	for i := range s.candidates {
		if _, ok := confirmed[s.candidates[i].AccountID]; ok {
			s.candidates[i].Pending = false
		}
		handledAtCopy := handledAt
		s.candidates[i].HandledAt = &handledAtCopy
	}
	return len(events), nil
}

func (s *officialQuotaTriggerTrackerStub) ClearOpenAIOfficial7dResetPending(context.Context, int64, time.Time) (bool, error) {
	return s.clearPendingChanged, nil
}

func (s *officialQuotaTriggerTrackerStub) setCandidates(candidates []OpenAIOfficial7dResetCandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = append([]OpenAIOfficial7dResetCandidate(nil), candidates...)
}

func (s *officialQuotaTriggerTrackerStub) candidateCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidateCalls
}

type officialQuotaTriggerQuerierStub struct {
	mu       sync.Mutex
	tracker  *officialQuotaTriggerTrackerStub
	observer *OpenAIOfficial7dResetObserver
	now      time.Time
	queried  []int64
	fail     map[int64]error
}

func (s *officialQuotaTriggerQuerierStub) QueryUsageSnapshot(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	s.mu.Lock()
	s.queried = append(s.queried, accountID)
	err := s.fail[accountID]
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	_, err = observeOpenAIOfficial7dReset(
		ctx,
		s.observer,
		accountID,
		s.now,
		s.now.Add(7*24*time.Hour),
		7*24*time.Hour,
		OpenAIOfficial7dResetSourcePeriodicProbe,
	)
	return &OpenAIQuotaUsage{}, err
}

func (s *officialQuotaTriggerQuerierStub) queriedAccountIDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.queried...)
}

type officialQuotaTriggerSettingsStub struct {
	enabled bool
}

func (s *officialQuotaTriggerSettingsStub) GetOpenAIOfficialQuotaAutoResetEnabled(context.Context) (bool, error) {
	return s.enabled, nil
}

func (s *officialQuotaTriggerSettingsStub) SetOpenAIOfficialQuotaAutoResetEnabled(_ context.Context, enabled bool) error {
	s.enabled = enabled
	return nil
}

type officialQuotaTriggerUserSubRepoStub struct {
	userSubRepoNoop
	active   []UserSubscription
	resetIDs []int64
}

func (s *officialQuotaTriggerUserSubRepoStub) ListAllActiveForQuotaReset(context.Context, time.Time) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), s.active...), nil
}

func (s *officialQuotaTriggerUserSubRepoStub) ResetFiveHourUsage(_ context.Context, id int64, _ time.Time) error {
	s.resetIDs = append(s.resetIDs, id)
	return nil
}

func (s *officialQuotaTriggerUserSubRepoStub) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time, time.Time) error {
	return nil
}

type officialQuotaTriggerNotifierStub struct {
	sources []OpenAIOfficial7dResetObservationSource
}

func (s *officialQuotaTriggerNotifierStub) notifyOpenAIOfficial7dResetDetected(source OpenAIOfficial7dResetObservationSource) {
	s.sources = append(s.sources, source)
}

type officialQuotaTriggerHarness struct {
	runner   *OpenAIOfficialQuotaResetRunner
	tracker  *officialQuotaTriggerTrackerStub
	querier  *officialQuotaTriggerQuerierStub
	observer *OpenAIOfficial7dResetObserver
}

func newOfficialQuotaTriggerHarness(
	accountIDs []int64,
	detectAccounts map[int64]bool,
	autoResetEnabled bool,
	interval time.Duration,
) *officialQuotaTriggerHarness {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	candidates := make([]OpenAIOfficial7dResetCandidate, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		candidates = append(candidates, OpenAIOfficial7dResetCandidate{AccountID: accountID})
	}
	tracker := &officialQuotaTriggerTrackerStub{
		candidates:     candidates,
		detectAccounts: detectAccounts,
	}
	observer := NewOpenAIOfficial7dResetObserver(tracker, nil)
	querier := &officialQuotaTriggerQuerierStub{
		tracker:  tracker,
		observer: observer,
		now:      now,
		fail:     map[int64]error{},
	}
	settings := &officialQuotaTriggerSettingsStub{enabled: autoResetEnabled}
	subRepo := &officialQuotaTriggerUserSubRepoStub{
		active: []UserSubscription{{ID: 11, UserID: 101, GroupID: 201}},
	}
	subscriptionService := NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil)
	quotaResetService := NewSubscriptionQuotaResetService(subscriptionService, tracker, settings)
	runner := NewOpenAIOfficialQuotaResetRunner(tracker, querier, quotaResetService, interval)
	runner.now = func() time.Time { return now }
	observer.setDetectionNotifier(runner)
	return &officialQuotaTriggerHarness{
		runner:   runner,
		tracker:  tracker,
		querier:  querier,
		observer: observer,
	}
}

func TestOpenAIOfficialQuotaResetTrigger_ObserverNotifiesEveryDetectionSourceOnce(t *testing.T) {
	sources := []OpenAIOfficial7dResetObservationSource{
		OpenAIOfficial7dResetSourceQuotaAPI,
		OpenAIOfficial7dResetSourcePeriodicProbe,
		OpenAIOfficial7dResetSourceGatewayHeader,
		OpenAIOfficial7dResetSourceAccountProbe,
	}
	for _, source := range sources {
		t.Run(string(source), func(t *testing.T) {
			now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
			tracker := &officialQuotaTriggerTrackerStub{
				candidates:     []OpenAIOfficial7dResetCandidate{{AccountID: 42}},
				detectAccounts: map[int64]bool{42: true},
			}
			notifier := &officialQuotaTriggerNotifierStub{}
			observer := NewOpenAIOfficial7dResetObserver(tracker, nil)
			observer.setDetectionNotifier(notifier)

			detected, err := observeOpenAIOfficial7dReset(context.Background(), observer, 42, now, now.Add(7*24*time.Hour), 7*24*time.Hour, source)
			require.NoError(t, err)
			require.True(t, detected)
			detected, err = observeOpenAIOfficial7dReset(context.Background(), observer, 42, now.Add(time.Minute), now.Add(7*24*time.Hour), 7*24*time.Hour, source)
			require.NoError(t, err)
			require.False(t, detected)
			require.Equal(t, []OpenAIOfficial7dResetObservationSource{source}, notifier.sources)
		})
	}
}

func TestOpenAIOfficialQuotaResetTrigger_ObservationErrorDoesNotNotify(t *testing.T) {
	tracker := &officialQuotaTriggerTrackerStub{observeErr: errors.New("observe failed")}
	notifier := &officialQuotaTriggerNotifierStub{}
	observer := NewOpenAIOfficial7dResetObserver(tracker, nil)
	observer.setDetectionNotifier(notifier)
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)

	detected, err := observeOpenAIOfficial7dReset(context.Background(), observer, 42, now, now.Add(7*24*time.Hour), 7*24*time.Hour, OpenAIOfficial7dResetSourceGatewayHeader)

	require.EqualError(t, err, "observe failed")
	require.False(t, detected)
	require.Empty(t, notifier.sources)
}

func TestOpenAIOfficialQuotaResetTrigger_InvalidObservationAndNilHooksAreNoops(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	notifier := &officialQuotaTriggerNotifierStub{}
	var nilObserver *OpenAIOfficial7dResetObserver
	nilObserver.setDetectionNotifier(notifier)

	detected, err := observeOpenAIOfficial7dReset(
		context.Background(), nil, 42, now, now.Add(7*24*time.Hour), 7*24*time.Hour, OpenAIOfficial7dResetSourceGatewayHeader,
	)
	require.NoError(t, err)
	require.False(t, detected)

	var nilRunner *OpenAIOfficialQuotaResetRunner
	require.Nil(t, nilRunner.immediateProbeWakeChannel())
	require.Nil(t, (&OpenAIOfficialQuotaResetRunner{}).immediateProbeWakeChannel())
}

func TestOpenAIOfficialQuotaResetTrigger_CoalescesPassiveSourcesAndSeparatesPeriodicDetection(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness(nil, nil, false, time.Hour)

	harness.runner.notifyOpenAIOfficial7dResetDetected(OpenAIOfficial7dResetSourceGatewayHeader)
	harness.runner.notifyOpenAIOfficial7dResetDetected(OpenAIOfficial7dResetSourceQuotaAPI)
	require.Len(t, harness.runner.trigger.wake, 1)
	require.False(t, harness.runner.consumePeriodicProbeDetection())

	harness.runner.notifyOpenAIOfficial7dResetDetected(OpenAIOfficial7dResetSourcePeriodicProbe)
	require.Len(t, harness.runner.trigger.wake, 1)
	require.True(t, harness.runner.consumePeriodicProbeDetection())
	require.False(t, harness.runner.consumePeriodicProbeDetection())

	var nilRunner *OpenAIOfficialQuotaResetRunner
	nilRunner.notifyOpenAIOfficial7dResetDetected(OpenAIOfficial7dResetSourceGatewayHeader)
	(&OpenAIOfficialQuotaResetRunner{}).notifyOpenAIOfficial7dResetDetected(OpenAIOfficial7dResetSourceGatewayHeader)
}

func TestOpenAIOfficialQuotaResetTrigger_PassiveDetectionImmediatelyProbesThreeOtherAccounts(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3, 4, 5}, map[int64]bool{1: true}, false, time.Hour)
	now := harness.querier.now

	detected, err := observeOpenAIOfficial7dReset(
		context.Background(), harness.observer, 1, now, now.Add(7*24*time.Hour), 7*24*time.Hour, OpenAIOfficial7dResetSourceGatewayHeader,
	)
	require.NoError(t, err)
	require.True(t, detected)
	<-harness.runner.immediateProbeWakeChannel()

	harness.runner.runTriggeredCycle()

	require.Equal(t, []int64{2, 3, 4}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_StaleOrFailedPendingCheckDoesNotProbe(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		harness := newOfficialQuotaTriggerHarness([]int64{1, 2}, nil, false, time.Hour)

		harness.runner.runTriggeredCycle()

		require.Empty(t, harness.querier.queriedAccountIDs())
	})

	t.Run("lookup failure", func(t *testing.T) {
		harness := newOfficialQuotaTriggerHarness([]int64{1, 2}, nil, false, time.Hour)
		harness.tracker.pendingErr = errors.New("pending lookup failed")

		harness.runner.runTriggeredCycle()

		require.Empty(t, harness.querier.queriedAccountIDs())
	})

	var nilRunner *OpenAIOfficialQuotaResetRunner
	nilRunner.runTriggeredCycle()
}

func TestOpenAIOfficialQuotaResetTrigger_PeriodicBatchDoesNotRepeatAccountsWhenAllWereProbed(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3}, map[int64]bool{1: true}, false, time.Hour)

	require.NoError(t, harness.runner.RunOnce(context.Background()))

	require.Equal(t, []int64{1, 2, 3}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_PeriodicDetectionAddsOneBoundedUnprobedBatch(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3, 4, 5, 6, 7}, map[int64]bool{1: true}, false, time.Hour)

	require.NoError(t, harness.runner.RunOnce(context.Background()))

	require.Equal(t, []int64{1, 2, 3, 4, 5, 6}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_PeriodicReadyBatchDoesNotAddProbeBatch(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3, 4, 5, 6}, map[int64]bool{1: true, 2: true}, false, time.Hour)

	require.NoError(t, harness.runner.RunOnce(context.Background()))

	require.Equal(t, []int64{1, 2, 3}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_NoPeriodicDetectionDoesNotAddProbeBatch(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3, 4, 5, 6}, nil, false, time.Hour)

	require.NoError(t, harness.runner.RunOnce(context.Background()))

	require.Equal(t, []int64{1, 2, 3}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_AdditionalBatchCandidateFailureStopsCycle(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3, 4, 5, 6}, map[int64]bool{1: true}, false, time.Hour)
	harness.tracker.candidateErrCall = 4

	err := harness.runner.RunOnce(context.Background())

	require.EqualError(t, err, "candidate lookup failed")
	require.Equal(t, []int64{1, 2, 3}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_AdditionalBatchStatusFailureStopsCycle(t *testing.T) {
	harness := newOfficialQuotaTriggerHarness([]int64{1, 2, 3, 4, 5, 6}, map[int64]bool{1: true}, false, time.Hour)
	harness.tracker.pendingErr = errors.New("refreshed status failed")
	harness.tracker.pendingErrCall = 4

	err := harness.runner.RunOnce(context.Background())

	require.EqualError(t, err, "refreshed status failed")
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6}, harness.querier.queriedAccountIDs())
}

func TestOpenAIOfficialQuotaResetTrigger_RunLoopHandlesTickerWakeAndStop(t *testing.T) {
	t.Run("ticker", func(t *testing.T) {
		harness := newOfficialQuotaTriggerHarness(nil, nil, false, 5*time.Millisecond)
		harness.runner.Start()
		t.Cleanup(harness.runner.Stop)

		require.Eventually(t, func() bool {
			return harness.tracker.candidateCallCount() >= 2
		}, time.Second, 5*time.Millisecond)
	})

	t.Run("wake", func(t *testing.T) {
		harness := newOfficialQuotaTriggerHarness(nil, nil, false, time.Hour)
		harness.runner.Start()
		t.Cleanup(harness.runner.Stop)
		require.Eventually(t, func() bool {
			return harness.tracker.candidateCallCount() >= 1
		}, time.Second, 5*time.Millisecond)
		harness.runner.runMutex.Lock()
		harness.runner.runMutex.Unlock()

		detectedAt := harness.querier.now
		harness.tracker.setCandidates([]OpenAIOfficial7dResetCandidate{
			{AccountID: 1, Pending: true, DetectedAt: &detectedAt},
			{AccountID: 2},
		})
		harness.runner.notifyOpenAIOfficial7dResetDetected(OpenAIOfficial7dResetSourceGatewayHeader)

		require.Eventually(t, func() bool {
			return len(harness.querier.queriedAccountIDs()) == 1
		}, time.Second, 5*time.Millisecond)
		require.Equal(t, []int64{2}, harness.querier.queriedAccountIDs())
	})
}

func TestProvideOpenAIOfficialQuotaResetRunner_WiresDetectionNotifier(t *testing.T) {
	observer := NewOpenAIOfficial7dResetObserver(nil, nil)
	runner := ProvideOpenAIOfficialQuotaResetRunner(nil, nil, nil, nil, nil, nil, observer)
	t.Cleanup(runner.Stop)

	observer.notifyDetection(OpenAIOfficial7dResetSourceGatewayHeader)

	require.Len(t, runner.trigger.wake, 1)
}
