package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type openAIUsageMultiplierSubscriptionRepoStub struct {
	userSubRepoNoop
	active []UserSubscription
	err    error
}

func (s *openAIUsageMultiplierSubscriptionRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), s.active...), s.err
}

type openAIUsageMultiplierSourceStub struct {
	candidates []OpenAIQuotaEstimateCandidate
	err        error
}

func (s *openAIUsageMultiplierSourceStub) ListOpenAIQuotaEstimateCandidates(context.Context) ([]OpenAIQuotaEstimateCandidate, error) {
	return append([]OpenAIQuotaEstimateCandidate(nil), s.candidates...), s.err
}

func activeOpenAISubscription() UserSubscription {
	return UserSubscription{
		Status:    SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Group:     &Group{Platform: PlatformOpenAI},
	}
}

func quotaEstimateCandidate(planType string, min, max, coverageFrom float64) OpenAIQuotaEstimateCandidate {
	return OpenAIQuotaEstimateCandidate{
		PlanType:    planType,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_7d_quota_estimate_min":           min,
			"codex_7d_quota_estimate_max":           max,
			"codex_7d_quota_estimate_coverage_from": coverageFrom,
			"codex_7d_quota_estimate_coverage_to":   coverageFrom + 10,
		},
	}
}

func quotaEstimateCandidateWithPrevious(
	planType string,
	currentMin, currentMax, currentCoverageFrom float64,
	previousMin, previousMax, previousCoverageFrom float64,
) OpenAIQuotaEstimateCandidate {
	candidate := quotaEstimateCandidate(planType, currentMin, currentMax, currentCoverageFrom)
	candidate.Extra["codex_7d_quota_estimate_prev_min"] = previousMin
	candidate.Extra["codex_7d_quota_estimate_prev_max"] = previousMax
	candidate.Extra["codex_7d_quota_estimate_prev_coverage_from"] = previousCoverageFrom
	candidate.Extra["codex_7d_quota_estimate_prev_coverage_to"] = previousCoverageFrom + 10
	return candidate
}

func requireMultiplierTier(t *testing.T, result *OpenAIUsageMultiplierEstimate, tier string) OpenAIUsageMultiplierTierEstimate {
	t.Helper()
	for i := range result.Tiers {
		if result.Tiers[i].Tier == tier {
			return result.Tiers[i]
		}
	}
	t.Fatalf("tier %q not found in %#v", tier, result.Tiers)
	return OpenAIUsageMultiplierTierEstimate{}
}

func newOpenAIUsageMultiplierService(source *openAIUsageMultiplierSourceStub) *SubscriptionService {
	subRepo := &openAIUsageMultiplierSubscriptionRepoStub{active: []UserSubscription{activeOpenAISubscription()}}
	svc := NewSubscriptionService(nil, subRepo, nil, nil, nil)
	svc.openAIQuotaEstimateSource = source
	return svc
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierCalculatesBothTelemetryTiers(t *testing.T) {
	source := &openAIUsageMultiplierSourceStub{candidates: []OpenAIQuotaEstimateCandidate{
		quotaEstimateCandidateWithPrevious("plus", 118, 130, 30, 113.64, 120, 80),
		quotaEstimateCandidate("PLUS", 120, 130, 30),
		quotaEstimateCandidateWithPrevious("pro", 2488.25, 2504.75, 10, 2334.83, 2400, 50),
		quotaEstimateCandidate("chatgptpro", 2400, 2500, 20),
	}}

	result, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

	require.NoError(t, err)
	require.Len(t, result.Tiers, 2)
	oneX := requireMultiplierTier(t, result, "1x")
	require.Equal(t, 125.0, oneX.BaselineQuotaUSD)
	require.NotNil(t, oneX.TelemetryQuotaUSD)
	require.NotNil(t, oneX.DynamicMultiplier)
	require.InDelta(t, 113.64, *oneX.TelemetryQuotaUSD, 1e-9)
	require.InDelta(t, 1.1, *oneX.DynamicMultiplier, 1e-9)

	twentyX := requireMultiplierTier(t, result, "20x")
	require.Equal(t, 2500.0, twentyX.BaselineQuotaUSD)
	require.NotNil(t, twentyX.TelemetryQuotaUSD)
	require.NotNil(t, twentyX.DynamicMultiplier)
	require.InDelta(t, 2334.83, *twentyX.TelemetryQuotaUSD, 1e-9)
	require.InDelta(t, 1.08, *twentyX.DynamicMultiplier, 1e-9)
	require.NotNil(t, result.DynamicMultiplier)
	require.InDelta(t, 1.1, *result.DynamicMultiplier, 1e-9)
}

func TestRoundUpOpenAIUsageMultiplierKeepsTwoDecimalPlaces(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  float64
	}{
		{name: "already exact", value: 1.1, want: 1.1},
		{name: "third decimal below five", value: 1.0707, want: 1.08},
		{name: "small remainder", value: 1.1001, want: 1.11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, roundUpOpenAIUsageMultiplier(tt.value), 1e-9)
		})
	}
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierUsesLowerTrustedCurrentOrPrevious(t *testing.T) {
	source := &openAIUsageMultiplierSourceStub{candidates: []OpenAIQuotaEstimateCandidate{
		quotaEstimateCandidateWithPrevious("plus", 120, 130, 20, 100, 110, 90),
	}}

	result, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

	require.NoError(t, err)
	oneX := requireMultiplierTier(t, result, "1x")
	require.NotNil(t, oneX.TelemetryQuotaUSD)
	require.InDelta(t, 100, *oneX.TelemetryQuotaUSD, 1e-9)
	require.NotNil(t, oneX.DynamicMultiplier)
	require.InDelta(t, 1.25, *oneX.DynamicMultiplier, 1e-9)
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierFallsBackToOnlyTrustedRound(t *testing.T) {
	source := &openAIUsageMultiplierSourceStub{candidates: []OpenAIQuotaEstimateCandidate{
		quotaEstimateCandidateWithPrevious("plus", 90, 100, 10, 110, 120, 20),
	}}

	result, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

	require.NoError(t, err)
	oneX := requireMultiplierTier(t, result, "1x")
	require.NotNil(t, oneX.TelemetryQuotaUSD)
	require.InDelta(t, 110, *oneX.TelemetryQuotaUSD, 1e-9)
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierFiltersUnavailableAndUnsupportedAccounts(t *testing.T) {
	expiredAt := time.Now().Add(-time.Hour)
	disabled := quotaEstimateCandidate("plus", 25, 30, 90)
	disabled.Status = StatusDisabled
	unschedulable := quotaEstimateCandidate("plus", 50, 60, 90)
	unschedulable.Schedulable = false
	expired := quotaEstimateCandidate("plus", 75, 80, 90)
	expired.ExpiresAt = &expiredAt

	source := &openAIUsageMultiplierSourceStub{candidates: []OpenAIQuotaEstimateCandidate{
		quotaEstimateCandidate("plus", 100, 120, 10),
		quotaEstimateCandidate("free", 10, 20, 90),
		quotaEstimateCandidate("team", 20, 30, 90),
		quotaEstimateCandidate("unknown", 30, 40, 90),
		disabled,
		unschedulable,
		expired,
		quotaEstimateCandidate("pro", 2500, 2600, 20),
	}}

	result, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

	require.NoError(t, err)
	oneX := requireMultiplierTier(t, result, "1x")
	require.Nil(t, oneX.TelemetryQuotaUSD)
	require.Nil(t, oneX.DynamicMultiplier)
	twentyX := requireMultiplierTier(t, result, "20x")
	require.NotNil(t, twentyX.TelemetryQuotaUSD)
	require.InDelta(t, 2500, *twentyX.TelemetryQuotaUSD, 1e-9)
	require.NotNil(t, result.DynamicMultiplier)
	require.InDelta(t, 1, *result.DynamicMultiplier, 1e-9)
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierIsUncapped(t *testing.T) {
	tests := []struct {
		name       string
		quota      float64
		multiplier float64
	}{
		{name: "below one", quota: 250, multiplier: 0.5},
		{name: "above one", quota: 50, multiplier: 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &openAIUsageMultiplierSourceStub{candidates: []OpenAIQuotaEstimateCandidate{
				quotaEstimateCandidate("plus", tt.quota, tt.quota, 20),
			}}

			result, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

			require.NoError(t, err)
			require.NotNil(t, result.DynamicMultiplier)
			require.InDelta(t, tt.multiplier, *result.DynamicMultiplier, 1e-9)
		})
	}
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierReturnsEmptyTiersWithoutEligibleEstimate(t *testing.T) {
	source := &openAIUsageMultiplierSourceStub{candidates: []OpenAIQuotaEstimateCandidate{
		quotaEstimateCandidate("plus", 100, 120, 10),
		quotaEstimateCandidateWithPrevious("pro", 2000, 2200, 10, 2300, 2400, 10),
	}}

	result, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

	require.NoError(t, err)
	require.Len(t, result.Tiers, 2)
	for i := range result.Tiers {
		require.Nil(t, result.Tiers[i].TelemetryQuotaUSD)
		require.Nil(t, result.Tiers[i].DynamicMultiplier)
	}
	require.Nil(t, result.DynamicMultiplier)
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierRequiresActiveOpenAISubscription(t *testing.T) {
	tests := []struct {
		name string
		subs []UserSubscription
	}{
		{name: "no subscriptions"},
		{name: "other platform", subs: []UserSubscription{{
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(time.Hour),
			Group:     &Group{Platform: PlatformAnthropic},
		}}},
		{name: "expired openai", subs: []UserSubscription{{
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(-time.Hour),
			Group:     &Group{Platform: PlatformOpenAI},
		}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subRepo := &openAIUsageMultiplierSubscriptionRepoStub{active: tt.subs}
			svc := NewSubscriptionService(nil, subRepo, nil, nil, nil)
			svc.openAIQuotaEstimateSource = &openAIUsageMultiplierSourceStub{}

			_, err := svc.GetOpenAIUsageMultiplier(context.Background(), 7)

			require.ErrorIs(t, err, ErrActiveOpenAISubscriptionRequired)
		})
	}
}

func TestSubscriptionServiceGetOpenAIUsageMultiplierPropagatesSourceError(t *testing.T) {
	sourceErr := errors.New("estimate source failed")
	source := &openAIUsageMultiplierSourceStub{err: sourceErr}

	_, err := newOpenAIUsageMultiplierService(source).GetOpenAIUsageMultiplier(context.Background(), 7)

	require.ErrorIs(t, err, sourceErr)
}
