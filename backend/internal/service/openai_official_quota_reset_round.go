package service

import (
	"sort"
	"strings"
	"time"
)

const (
	openAIOfficialQuotaResetObservationWindow = 3 * time.Hour
	openAIOfficial7dWindow                    = 7 * 24 * time.Hour
	openAIOfficial7dResetEvidenceVersion      = 1
)

type OpenAIOfficial7dResetRound struct {
	Anchor                    OpenAIOfficial7dResetState
	StartedAt                 time.Time
	ExpiresAt                 time.Time
	EligibleAccountIDs        []int64
	ConfirmedEvents           []OpenAIOfficial7dResetState
	ConfirmationCount         int
	RequiredConfirmationCount int
	Ready                     bool
}

// evaluateOpenAIOfficial7dResetRounds applies a rolling, left-closed and
// right-open three-hour confirmation window. A ready round is retained for
// retry even after its deadline; an unconfirmed expired round drops only its
// oldest anchor so the next pending event can start a new round.
func evaluateOpenAIOfficial7dResetRounds(
	pending []OpenAIOfficial7dResetState,
	now time.Time,
) (*OpenAIOfficial7dResetRound, []OpenAIOfficial7dResetState) {
	trusted := make([]OpenAIOfficial7dResetState, 0, len(pending))
	for _, event := range pending {
		if isTrustedOpenAIOfficial7dResetEvent(event) {
			trusted = append(trusted, event)
		}
	}
	sort.SliceStable(trusted, func(i, j int) bool {
		left := trusted[i].DetectedAt.UTC().Truncate(time.Second)
		right := trusted[j].DetectedAt.UTC().Truncate(time.Second)
		if left.Equal(right) {
			return trusted[i].AccountID < trusted[j].AccountID
		}
		return left.Before(right)
	})

	expired := make([]OpenAIOfficial7dResetState, 0)
	for len(trusted) > 0 {
		anchor := trusted[0]
		startedAt := anchor.DetectedAt.UTC().Truncate(time.Second)
		expiresAt := startedAt.Add(openAIOfficialQuotaResetObservationWindow)
		eligible := normalizedOpenAIAccountIDs(anchor.Evidence.EligibleAccountIDs)
		eligibleSet := make(map[int64]struct{}, len(eligible))
		for _, accountID := range eligible {
			eligibleSet[accountID] = struct{}{}
		}

		confirmed := make([]OpenAIOfficial7dResetState, 0, len(eligible))
		confirmedAccounts := make(map[int64]struct{}, len(eligible))
		for _, event := range trusted {
			detectedAt := event.DetectedAt.UTC().Truncate(time.Second)
			if detectedAt.Before(startedAt) || !detectedAt.Before(expiresAt) {
				continue
			}
			if _, ok := eligibleSet[event.AccountID]; !ok {
				continue
			}
			if _, duplicate := confirmedAccounts[event.AccountID]; duplicate {
				continue
			}
			confirmedAccounts[event.AccountID] = struct{}{}
			confirmed = append(confirmed, event)
		}
		required := automaticQuotaResetConfirmationRequirement(len(eligible))
		round := &OpenAIOfficial7dResetRound{
			Anchor:                    anchor,
			StartedAt:                 startedAt,
			ExpiresAt:                 expiresAt,
			EligibleAccountIDs:        eligible,
			ConfirmedEvents:           confirmed,
			ConfirmationCount:         len(confirmed),
			RequiredConfirmationCount: required,
			Ready:                     required > 0 && len(confirmed) >= required,
		}
		if round.Ready || now.UTC().Truncate(time.Second).Before(expiresAt) {
			return round, expired
		}

		expired = append(expired, anchor)
		trusted = trusted[1:]
	}
	return nil, expired
}

func isTrustedOpenAIOfficial7dResetEvent(event OpenAIOfficial7dResetState) bool {
	evidence := event.Evidence
	if evidence == nil {
		return false
	}
	planType := strings.ToLower(strings.TrimSpace(evidence.PlanType))
	if event.AccountID <= 0 || event.DetectedAt.IsZero() ||
		evidence.Version != openAIOfficial7dResetEvidenceVersion ||
		evidence.ObservedAt.IsZero() || evidence.ResetAt.IsZero() ||
		evidence.WindowSeconds != int64(openAIOfficial7dWindow/time.Second) ||
		planType == "" || planType == "free" || planType == "abnormal" ||
		!evidence.ObservedAt.UTC().Truncate(time.Second).Equal(event.DetectedAt.UTC().Truncate(time.Second)) {
		return false
	}
	if event.HandledAt != nil && !event.DetectedAt.UTC().Truncate(time.Second).After(event.HandledAt.UTC().Truncate(time.Second)) {
		return false
	}
	if evidence.SubscriptionExpiresAt != nil &&
		!evidence.ObservedAt.UTC().Truncate(time.Second).Before(evidence.SubscriptionExpiresAt.UTC().Truncate(time.Second)) {
		return false
	}
	for _, accountID := range evidence.EligibleAccountIDs {
		if accountID == event.AccountID {
			return true
		}
	}
	return false
}

func normalizedOpenAIAccountIDs(accountIDs []int64) []int64 {
	unique := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID > 0 {
			unique[accountID] = struct{}{}
		}
	}
	result := make([]int64, 0, len(unique))
	for accountID := range unique {
		result = append(result, accountID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
