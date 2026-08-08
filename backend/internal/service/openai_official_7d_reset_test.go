package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type captureOfficial7dResetRepo struct {
	observedAccountID int64
	observedAt        time.Time
	resetAt           time.Time
	grace             time.Duration
	responses         []OpenAIOfficial7dResetObservation
	calls             int
}

func (r *captureOfficial7dResetRepo) ObserveOpenAI7dReset(_ context.Context, accountID int64, observedAt, resetAt time.Time, grace time.Duration) (OpenAIOfficial7dResetObservation, error) {
	r.observedAccountID = accountID
	r.observedAt = observedAt
	r.resetAt = resetAt
	r.grace = grace
	if r.calls < len(r.responses) {
		response := r.responses[r.calls]
		r.calls++
		return response, nil
	}
	r.calls++
	return OpenAIOfficial7dResetObservation{Detected: true, ObservedAt: observedAt, ResetAt: resetAt}, nil
}

func (r *captureOfficial7dResetRepo) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	return nil, nil
}

func (r *captureOfficial7dResetRepo) ListEligibleOpenAIOfficial7dResetCandidates(context.Context, time.Time) ([]OpenAIOfficial7dResetCandidate, error) {
	return nil, nil
}

func (r *captureOfficial7dResetRepo) MarkAllOpenAIOfficial7dResetsHandled(context.Context, time.Time) error {
	return nil
}

func TestOpenAIQuota7dResetAt_OnlyUsesLongWindow(t *testing.T) {
	fiveHour := time.Date(2026, 7, 18, 15, 0, 0, 0, time.UTC)
	sevenDay := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	rateLimit := &OpenAIRateLimit{
		PrimaryWindow:   &OpenAIRateLimitWindow{LimitWindowSeconds: int64((5 * time.Hour) / time.Second), ResetAt: fiveHour.Unix()},
		SecondaryWindow: &OpenAIRateLimitWindow{LimitWindowSeconds: int64((7 * 24 * time.Hour) / time.Second), ResetAt: sevenDay.Unix()},
	}

	require.Equal(t, sevenDay, *openAIQuota7dResetAt(rateLimit))
	require.Nil(t, openAIQuota7dResetAt(&OpenAIRateLimit{PrimaryWindow: rateLimit.PrimaryWindow}))
	require.Nil(t, openAIQuota7dResetAt(&OpenAIRateLimit{
		PrimaryWindow:   rateLimit.PrimaryWindow,
		SecondaryWindow: &OpenAIRateLimitWindow{LimitWindowSeconds: int64((4 * time.Hour) / time.Second), ResetAt: fiveHour.Unix()},
	}))
}

func TestObserveOfficial7dReset_PersistsOnlyMainRateLimit7d(t *testing.T) {
	tracker := &captureOfficial7dResetRepo{}
	svc := &OpenAIQuotaService{official7dResetObserver: NewOpenAIOfficial7dResetObserver(tracker, nil)}
	observedAt := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(7 * 24 * time.Hour)

	svc.observeOfficial7dReset(context.Background(), 42, &OpenAIQuotaUsage{
		FetchedAt: observedAt.Unix(),
		RateLimit: &OpenAIRateLimit{
			PrimaryWindow:   &OpenAIRateLimitWindow{LimitWindowSeconds: int64((5 * time.Hour) / time.Second), ResetAt: observedAt.Add(5 * time.Hour).Unix()},
			SecondaryWindow: &OpenAIRateLimitWindow{LimitWindowSeconds: int64((7 * 24 * time.Hour) / time.Second), ResetAt: resetAt.Unix()},
		},
	}, OpenAIOfficial7dResetSourceQuotaAPI)

	require.Equal(t, int64(42), tracker.observedAccountID)
	require.Equal(t, observedAt, tracker.observedAt)
	require.Equal(t, resetAt, tracker.resetAt)
	require.Equal(t, time.Minute, tracker.grace)
}

func TestObserveOpenAIOfficial7dReset_AuditsEachAccountDetectionOnce(t *testing.T) {
	observedAt := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	previousResetAt := observedAt.Add(24 * time.Hour)
	resetAt := observedAt.Add(7 * 24 * time.Hour)
	tracker := &captureOfficial7dResetRepo{responses: []OpenAIOfficial7dResetObservation{
		{Detected: true, PreviousResetAt: &previousResetAt, ResetAt: resetAt, ObservedAt: observedAt},
		{Detected: false, PreviousResetAt: &resetAt, ResetAt: resetAt, ObservedAt: observedAt.Add(time.Minute)},
	}}
	auditService := NewAuditLogService(nil, nil)
	observer := NewOpenAIOfficial7dResetObserver(tracker, auditService)

	detected, err := observeOpenAIOfficial7dReset(
		context.Background(), observer, 42, observedAt, resetAt, OpenAIOfficial7dResetSourceGatewayHeader,
	)
	require.NoError(t, err)
	require.True(t, detected)
	detected, err = observeOpenAIOfficial7dReset(
		context.Background(), observer, 42, observedAt.Add(time.Minute), resetAt, OpenAIOfficial7dResetSourceGatewayHeader,
	)
	require.NoError(t, err)
	require.False(t, detected)

	require.Len(t, auditService.queue, 1)
	entry := <-auditService.queue
	require.Equal(t, openAIOfficialEarlyResetDetectedAuditAction, entry.Action)
	require.Equal(t, "system", entry.ActorEmail)
	require.Equal(t, int64(42), entry.Extra["account_id"])
	require.Equal(t, "gateway_header", entry.Extra["observation_source"])
	require.Equal(t, previousResetAt.Format(time.RFC3339), entry.Extra["previous_reset_at"])
	require.Equal(t, resetAt.Format(time.RFC3339), entry.Extra["new_reset_at"])
}

func TestOpenAIOfficial7dResetTimesFromExtraUpdates(t *testing.T) {
	fallback := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	observedAt, resetAt, ok := openAIOfficial7dResetTimesFromExtraUpdates(map[string]any{
		"codex_usage_updated_at": "2026-08-03T09:59:00Z",
		"codex_7d_reset_at":      "2026-08-10T09:59:00Z",
	}, fallback)

	require.True(t, ok)
	require.Equal(t, time.Date(2026, 8, 3, 9, 59, 0, 0, time.UTC), observedAt)
	require.Equal(t, time.Date(2026, 8, 10, 9, 59, 0, 0, time.UTC), resetAt)
}
