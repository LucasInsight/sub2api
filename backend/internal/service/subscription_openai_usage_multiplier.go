package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	openAIUsageMultiplierMinimumCoveragePercent  = 20.0
	openAIUsageMultiplierTierOneX                = "1x"
	openAIUsageMultiplierTierTwentyX             = "20x"
	openAIUsageMultiplierOneXBaselineQuotaUSD    = 125.0
	openAIUsageMultiplierTwentyXBaselineQuotaUSD = 2500.0
	openAIUsageMultiplierDecimalFactor           = 100.0
	openAIUsageMultiplierRoundingEpsilon         = 1e-9
)

var (
	ErrActiveOpenAISubscriptionRequired = infraerrors.Forbidden(
		"ACTIVE_OPENAI_SUBSCRIPTION_REQUIRED",
		"an active OpenAI subscription is required",
	)
	ErrOpenAIUsageMultiplierUnavailable = infraerrors.ServiceUnavailable(
		"OPENAI_USAGE_MULTIPLIER_UNAVAILABLE",
		"OpenAI usage multiplier estimate is unavailable",
	)
)

// OpenAIQuotaEstimateCandidate is the minimal account projection required to
// calculate the user-facing global OpenAI quota multiplier.
type OpenAIQuotaEstimateCandidate struct {
	PlanType    string
	Status      string
	Schedulable bool
	ExpiresAt   *time.Time
	Extra       map[string]any
}

// OpenAIQuotaEstimateSource intentionally excludes credentials and account
// identity fields from the user-facing multiplier data path.
type OpenAIQuotaEstimateSource interface {
	ListOpenAIQuotaEstimateCandidates(ctx context.Context) ([]OpenAIQuotaEstimateCandidate, error)
}

type OpenAIUsageMultiplierTierEstimate struct {
	Tier              string   `json:"tier"`
	BaselineQuotaUSD  float64  `json:"baseline_quota_usd"`
	TelemetryQuotaUSD *float64 `json:"telemetry_quota_usd"`
	DynamicMultiplier *float64 `json:"dynamic_multiplier"`
}

type OpenAIUsageMultiplierEstimate struct {
	Tiers             []OpenAIUsageMultiplierTierEstimate `json:"tiers"`
	DynamicMultiplier *float64                            `json:"dynamic_multiplier"`
}

func (s *SubscriptionService) GetOpenAIUsageMultiplier(ctx context.Context, userID int64) (*OpenAIUsageMultiplierEstimate, error) {
	if s == nil || s.userSubRepo == nil {
		return nil, ErrOpenAIUsageMultiplierUnavailable
	}

	now := time.Now()
	subscriptions, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list active subscriptions for OpenAI usage multiplier: %w", err)
	}
	if !hasActiveOpenAISubscription(subscriptions, now) {
		return nil, ErrActiveOpenAISubscriptionRequired
	}
	if s.openAIQuotaEstimateSource == nil {
		return nil, ErrOpenAIUsageMultiplierUnavailable
	}

	candidates, err := s.openAIQuotaEstimateSource.ListOpenAIQuotaEstimateCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list OpenAI quota estimate candidates: %w", err)
	}

	result := &OpenAIUsageMultiplierEstimate{
		Tiers: []OpenAIUsageMultiplierTierEstimate{
			{Tier: openAIUsageMultiplierTierOneX, BaselineQuotaUSD: openAIUsageMultiplierOneXBaselineQuotaUSD},
			{Tier: openAIUsageMultiplierTierTwentyX, BaselineQuotaUSD: openAIUsageMultiplierTwentyXBaselineQuotaUSD},
		},
	}

	telemetryByTier := conservativeOpenAIQuotaEstimateByTier(candidates, now)
	for i := range result.Tiers {
		tier := &result.Tiers[i]
		telemetryQuota, ok := telemetryByTier[tier.Tier]
		if !ok {
			continue
		}

		multiplier := roundUpOpenAIUsageMultiplier(tier.BaselineQuotaUSD / telemetryQuota)
		tier.TelemetryQuotaUSD = float64Pointer(telemetryQuota)
		tier.DynamicMultiplier = float64Pointer(multiplier)
		if result.DynamicMultiplier == nil || multiplier > *result.DynamicMultiplier {
			result.DynamicMultiplier = float64Pointer(multiplier)
		}
	}
	return result, nil
}

func roundUpOpenAIUsageMultiplier(value float64) float64 {
	scaled := value * openAIUsageMultiplierDecimalFactor
	nearestInteger := math.Round(scaled)
	if math.Abs(scaled-nearestInteger) <= openAIUsageMultiplierRoundingEpsilon {
		return nearestInteger / openAIUsageMultiplierDecimalFactor
	}
	return math.Ceil(scaled) / openAIUsageMultiplierDecimalFactor
}

func hasActiveOpenAISubscription(subscriptions []UserSubscription, now time.Time) bool {
	for i := range subscriptions {
		subscription := &subscriptions[i]
		if subscription.Status != SubscriptionStatusActive || !subscription.ExpiresAt.After(now) {
			continue
		}
		if subscription.Group != nil && subscription.Group.Platform == PlatformOpenAI {
			return true
		}
	}
	return false
}

func conservativeOpenAIQuotaEstimateByTier(candidates []OpenAIQuotaEstimateCandidate, now time.Time) map[string]float64 {
	result := make(map[string]float64, 2)
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Status != StatusActive || !candidate.Schedulable {
			continue
		}
		if candidate.ExpiresAt != nil && !candidate.ExpiresAt.After(now) {
			continue
		}
		tier, ok := openAIUsageMultiplierTier(candidate.PlanType)
		if !ok {
			continue
		}
		estimate := quotaEstimateFromExtra(candidate.Extra, "7d")
		quota, ok := trustedOpenAIQuotaEstimateLowerBound(estimate)
		if !ok {
			continue
		}
		if current, found := result[tier]; !found || quota < current {
			result[tier] = quota
		}
	}
	return result
}

func openAIUsageMultiplierTier(planType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(planType)) {
	case "plus":
		return openAIUsageMultiplierTierOneX, true
	case "pro", "chatgptpro":
		return openAIUsageMultiplierTierTwentyX, true
	default:
		return "", false
	}
}

func trustedOpenAIQuotaEstimateLowerBound(estimate *QuotaEstimate) (float64, bool) {
	if estimate == nil {
		return 0, false
	}

	var result float64
	found := false
	if validTrustedOpenAIQuotaLowerBound(estimate.Min, estimate.CoverageFrom) {
		result = estimate.Min
		found = true
	}
	if estimate.Previous != nil && validTrustedOpenAIQuotaLowerBound(estimate.Previous.Min, estimate.Previous.CoverageFrom) {
		if !found || estimate.Previous.Min < result {
			result = estimate.Previous.Min
		}
		found = true
	}
	return result, found
}

func validTrustedOpenAIQuotaLowerBound(quota, coverageFrom float64) bool {
	return coverageFrom >= openAIUsageMultiplierMinimumCoveragePercent &&
		quota > 0 &&
		!math.IsNaN(quota) &&
		!math.IsInf(quota, 0)
}

func float64Pointer(value float64) *float64 {
	return &value
}
