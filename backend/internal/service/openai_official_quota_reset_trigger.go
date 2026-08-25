package service

import (
	"context"
	"log/slog"
	"sync/atomic"
)

type openAIOfficial7dResetDetectionNotifier interface {
	notifyOpenAIOfficial7dResetDetected(source OpenAIOfficial7dResetObservationSource)
}

type openAIOfficialQuotaResetTrigger struct {
	periodicDetection atomic.Bool
}

func newOpenAIOfficialQuotaResetTrigger() *openAIOfficialQuotaResetTrigger {
	return &openAIOfficialQuotaResetTrigger{}
}

func (o *OpenAIOfficial7dResetObserver) setDetectionNotifier(notifier openAIOfficial7dResetDetectionNotifier) {
	if o == nil {
		return
	}
	o.detectionNotifier = notifier
}

func (o *OpenAIOfficial7dResetObserver) notifyDetection(source OpenAIOfficial7dResetObservationSource) {
	if o == nil || o.detectionNotifier == nil {
		return
	}
	o.detectionNotifier.notifyOpenAIOfficial7dResetDetected(source)
}

func (r *OpenAIOfficialQuotaResetRunner) notifyOpenAIOfficial7dResetDetected(source OpenAIOfficial7dResetObservationSource) {
	if r == nil || r.trigger == nil {
		return
	}
	if source != OpenAIOfficial7dResetSourcePeriodicProbe {
		return
	}
	// The active cron cycle consumes this after its current batch. Passive
	// observations wait for the next cron boundary instead of waking the runner.
	r.trigger.periodicDetection.Store(true)
}

func (r *OpenAIOfficialQuotaResetRunner) resetPeriodicProbeDetection() {
	if r != nil && r.trigger != nil {
		r.trigger.periodicDetection.Store(false)
	}
}

func (r *OpenAIOfficialQuotaResetRunner) consumePeriodicProbeDetection() bool {
	return r != nil && r.trigger != nil && r.trigger.periodicDetection.Swap(false)
}

type openAIOfficialQuotaProbeBatchResult struct {
	attempted map[int64]struct{}
	succeeded int
	failed    int
}

func (r *OpenAIOfficialQuotaResetRunner) probeCandidateBatch(
	ctx context.Context,
	candidates []OpenAIOfficial7dResetCandidate,
) openAIOfficialQuotaProbeBatchResult {
	result := openAIOfficialQuotaProbeBatchResult{attempted: make(map[int64]struct{}, len(candidates))}
	for _, candidate := range candidates {
		accountID := candidate.AccountID
		result.attempted[accountID] = struct{}{}
		r.probeCursorAccountID = accountID
		probeCtx, cancel := context.WithTimeout(ctx, openAIOfficialQuotaResetProbeTimeout)
		_, probeErr := r.quotaQuerier.QueryUsageSnapshot(probeCtx, accountID)
		cancel()
		if probeErr != nil {
			result.failed++
			slog.Warn("openai official quota reset probe failed", "account_id", accountID, "error", probeErr)
			continue
		}
		result.succeeded++
	}
	return result
}

func (r *OpenAIOfficialQuotaResetRunner) nextUnprobedCandidates(
	candidates []OpenAIOfficial7dResetCandidate,
	attempted map[int64]struct{},
) []OpenAIOfficial7dResetCandidate {
	remaining := make([]OpenAIOfficial7dResetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, alreadyAttempted := attempted[candidate.AccountID]; !alreadyAttempted {
			remaining = append(remaining, candidate)
		}
	}
	return r.nextProbeCandidates(remaining)
}
