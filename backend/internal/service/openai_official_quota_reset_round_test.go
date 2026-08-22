//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func trustedOfficialResetEvent(accountID int64, detectedAt time.Time, eligibleAccountIDs ...int64) OpenAIOfficial7dResetState {
	expiresAt := detectedAt.Add(30 * 24 * time.Hour)
	return OpenAIOfficial7dResetState{
		AccountID:  accountID,
		DetectedAt: detectedAt,
		Evidence: &OpenAIOfficial7dResetEvidence{
			Version:               1,
			ObservedAt:            detectedAt,
			ResetAt:               detectedAt.Add(7 * 24 * time.Hour),
			WindowSeconds:         int64((7 * 24 * time.Hour) / time.Second),
			PlanType:              "pro",
			SubscriptionExpiresAt: &expiresAt,
			EligibleAccountIDs:    eligibleAccountIDs,
		},
	}
}

func TestEvaluateOpenAIOfficial7dResetRounds_ConfirmsInsideThreeHours(t *testing.T) {
	startedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	pending := []OpenAIOfficial7dResetState{
		trustedOfficialResetEvent(1, startedAt, 1, 2, 3),
		trustedOfficialResetEvent(2, startedAt.Add(3*time.Hour-time.Second), 1, 2, 3),
	}

	round, expired := evaluateOpenAIOfficial7dResetRounds(pending, startedAt.Add(4*time.Hour))

	require.Empty(t, expired)
	require.NotNil(t, round)
	require.Equal(t, int64(1), round.Anchor.AccountID)
	require.Equal(t, startedAt.Add(3*time.Hour), round.ExpiresAt)
	require.Equal(t, 2, round.ConfirmationCount)
	require.Equal(t, 2, round.RequiredConfirmationCount)
	require.True(t, round.Ready, "a round confirmed before its deadline remains retryable after the deadline")
}

func TestEvaluateOpenAIOfficial7dResetRounds_ExpiresOldestAndRollsAtBoundary(t *testing.T) {
	startedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	boundaryEvent := trustedOfficialResetEvent(2, startedAt.Add(3*time.Hour), 1, 2, 3)
	pending := []OpenAIOfficial7dResetState{
		trustedOfficialResetEvent(1, startedAt, 1, 2, 3),
		boundaryEvent,
	}

	round, expired := evaluateOpenAIOfficial7dResetRounds(pending, startedAt.Add(3*time.Hour))

	require.Equal(t, []OpenAIOfficial7dResetState{pending[0]}, expired)
	require.NotNil(t, round)
	require.Equal(t, boundaryEvent.AccountID, round.Anchor.AccountID)
	require.Equal(t, boundaryEvent.DetectedAt, round.StartedAt)
	require.Equal(t, 1, round.ConfirmationCount)
	require.False(t, round.Ready)
}

func TestEvaluateOpenAIOfficial7dResetRounds_UsesAnchorMembershipSnapshot(t *testing.T) {
	startedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	pending := []OpenAIOfficial7dResetState{
		trustedOfficialResetEvent(1, startedAt, 1, 2),
		trustedOfficialResetEvent(3, startedAt.Add(time.Hour), 1, 2, 3),
	}

	round, expired := evaluateOpenAIOfficial7dResetRounds(pending, startedAt.Add(2*time.Hour))

	require.Empty(t, expired)
	require.NotNil(t, round)
	require.Equal(t, []int64{1, 2}, round.EligibleAccountIDs)
	require.Equal(t, 1, round.ConfirmationCount, "an account added after the anchor must wait for a later round")
	require.False(t, round.Ready)
}

func TestEvaluateOpenAIOfficial7dResetRounds_IgnoresLegacyPendingWithoutEvidence(t *testing.T) {
	detectedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)

	round, expired := evaluateOpenAIOfficial7dResetRounds([]OpenAIOfficial7dResetState{{
		AccountID:  1,
		DetectedAt: detectedAt,
	}}, detectedAt.Add(24*time.Hour))

	require.Nil(t, round)
	require.Empty(t, expired, "legacy false positives remain available for manual cleanup")
}

func TestOpenAIChatGPTSubscriptionIdentity(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)
	past := now.Add(-time.Second).Format(time.RFC3339)

	tests := []struct {
		name        string
		credentials map[string]any
		wantActive  bool
	}{
		{name: "paid without expiry", credentials: map[string]any{"plan_type": " Pro "}, wantActive: true},
		{name: "paid with future expiry", credentials: map[string]any{"plan_type": "plus", "subscription_expires_at": future}, wantActive: true},
		{name: "expired paid subscription", credentials: map[string]any{"plan_type": "pro", "subscription_expires_at": past}},
		{name: "explicit free plan", credentials: map[string]any{"plan_type": "free"}},
		{name: "abnormal plan", credentials: map[string]any{"plan_type": "abnormal"}},
		{name: "missing plan", credentials: map[string]any{}},
		{name: "invalid explicit expiry", credentials: map[string]any{"plan_type": "pro", "subscription_expires_at": "not-a-time"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: tt.credentials}
			identity, active := account.OpenAIChatGPTSubscriptionIdentity(now)
			require.Equal(t, tt.wantActive, active)
			if active {
				require.NotEmpty(t, identity.PlanType)
			}
		})
	}
}
