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
	wake              chan struct{}
	periodicDetection atomic.Bool
}

func newOpenAIOfficialQuotaResetTrigger() *openAIOfficialQuotaResetTrigger {
	return &openAIOfficialQuotaResetTrigger{wake: make(chan struct{}, 1)}
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
	if source == OpenAIOfficial7dResetSourcePeriodicProbe {
		// The active RunOnce consumes this after its current batch; waking the
		// loop here would schedule a duplicate cycle for the same observation.
		r.trigger.periodicDetection.Store(true)
		return
	}
	select {
	case r.trigger.wake <- struct{}{}:
	default:
	}
}

func (r *OpenAIOfficialQuotaResetRunner) immediateProbeWakeChannel() <-chan struct{} {
	if r == nil || r.trigger == nil {
		return nil
	}
	return r.trigger.wake
}

func (r *OpenAIOfficialQuotaResetRunner) resetPeriodicProbeDetection() {
	if r != nil && r.trigger != nil {
		r.trigger.periodicDetection.Store(false)
	}
}

func (r *OpenAIOfficialQuotaResetRunner) consumePeriodicProbeDetection() bool {
	return r != nil && r.trigger != nil && r.trigger.periodicDetection.Swap(false)
}

func (r *OpenAIOfficialQuotaResetRunner) runTriggeredCycle() {
	if r == nil || r.tracker == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, openAIOfficialQuotaResetCycleTimeout)
	defer cancel()
	pending, err := r.tracker.ListPendingOpenAIOfficial7dResets(ctx)
	if err != nil {
		slog.Warn("openai official quota reset trigger pending lookup failed", "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}
	if err := r.RunOnce(ctx); err != nil {
		slog.Warn("openai official quota reset triggered cycle failed", "error", err)
	}
}

func (r *OpenAIOfficialQuotaResetRunner) probeCandidateBatch(
	ctx context.Context,
	candidates []OpenAIOfficial7dResetCandidate,
) map[int64]struct{} {
	attempted := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		accountID := candidate.AccountID
		attempted[accountID] = struct{}{}
		r.probeCursorAccountID = accountID
		probeCtx, cancel := context.WithTimeout(ctx, openAIOfficialQuotaResetProbeTimeout)
		_, probeErr := r.quotaQuerier.QueryUsageSnapshot(probeCtx, accountID)
		cancel()
		if probeErr != nil {
			slog.Warn("openai official quota reset probe failed", "account_id", accountID, "error", probeErr)
		}
	}
	return attempted
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
