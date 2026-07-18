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
}

func (r *captureOfficial7dResetRepo) ObserveOpenAI7dReset(_ context.Context, accountID int64, observedAt, resetAt time.Time, grace time.Duration) (bool, error) {
	r.observedAccountID = accountID
	r.observedAt = observedAt
	r.resetAt = resetAt
	r.grace = grace
	return true, nil
}

func (r *captureOfficial7dResetRepo) ListPendingOpenAIOfficial7dResets(context.Context) ([]OpenAIOfficial7dResetState, error) {
	return nil, nil
}

func (r *captureOfficial7dResetRepo) MarkOpenAIOfficial7dResetsHandled(context.Context, []int64, time.Time) error {
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
	svc := &OpenAIQuotaService{official7dResetRepo: tracker}
	observedAt := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(7 * 24 * time.Hour)

	svc.observeOfficial7dReset(context.Background(), 42, &OpenAIQuotaUsage{
		FetchedAt: observedAt.Unix(),
		RateLimit: &OpenAIRateLimit{
			PrimaryWindow:   &OpenAIRateLimitWindow{LimitWindowSeconds: int64((5 * time.Hour) / time.Second), ResetAt: observedAt.Add(5 * time.Hour).Unix()},
			SecondaryWindow: &OpenAIRateLimitWindow{LimitWindowSeconds: int64((7 * 24 * time.Hour) / time.Second), ResetAt: resetAt.Unix()},
		},
	})

	require.Equal(t, int64(42), tracker.observedAccountID)
	require.Equal(t, observedAt, tracker.observedAt)
	require.Equal(t, resetAt, tracker.resetAt)
	require.Equal(t, time.Minute, tracker.grace)
}
