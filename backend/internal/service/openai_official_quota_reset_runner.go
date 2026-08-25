package service

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const (
	openAIOfficialQuotaResetCronSchedule  = "*/5 * * * *"
	openAIOfficialQuotaResetLogComponent  = "service.openai_official_quota_reset"
	openAIOfficialQuotaResetLockKey       = "subscription:openai-official-quota-reset:leader"
	openAIOfficialQuotaResetLockTTL       = 3 * time.Minute
	openAIOfficialQuotaResetCycleTimeout  = 90 * time.Second
	openAIOfficialQuotaResetProbeTimeout  = 20 * time.Second
	openAIOfficialQuotaResetStopTimeout   = 3 * time.Second
	openAIOfficialQuotaResetMaxProbeCount = 3
	openAIOfficialAutoResetAuditAction    = "system.openai.quota.auto_reset_executed"
	openAIOfficialExpiredAuditAction      = "system.openai.quota.observation_expired"
)

var openAIOfficialQuotaResetCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

const (
	openAIOfficialQuotaResetCycleOutcomeFailed                  = "failed"
	openAIOfficialQuotaResetCycleOutcomeLeaderLockNotAcquired   = "leader_lock_not_acquired"
	openAIOfficialQuotaResetCycleOutcomeNoActiveSubscriptions   = "no_active_subscriptions"
	openAIOfficialQuotaResetCycleOutcomeNoEligibleAccounts      = "no_eligible_accounts"
	openAIOfficialQuotaResetCycleOutcomeNoUnconfirmedCandidates = "no_unconfirmed_candidates"
	openAIOfficialQuotaResetCycleOutcomeProbed                  = "probed"
	openAIOfficialQuotaResetCycleOutcomeReadyAutoResetDisabled  = "ready_auto_reset_disabled"
	openAIOfficialQuotaResetCycleOutcomeAutomaticResetExecuted  = "automatic_reset_executed"
	openAIOfficialQuotaResetCycleOutcomeDependenciesUnavailable = "dependencies_unavailable"
)

type OpenAIQuotaUsageQuerier interface {
	QueryUsageSnapshot(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error)
}

// OpenAIOfficialQuotaResetRunner confirms official early 7-day resets and
// executes one global subscription-quota reset after enough accounts agree.
type OpenAIOfficialQuotaResetRunner struct {
	tracker           OpenAIOfficial7dResetRepository
	quotaQuerier      OpenAIQuotaUsageQuerier
	quotaResetService *SubscriptionQuotaResetService
	auditService      *AuditLogService
	schedule          string
	now               func() time.Time

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	cron                 *cron.Cron
	started              bool
	stopped              bool
	runMutex             sync.Mutex
	probeCursorAccountID int64
	trigger              *openAIOfficialQuotaResetTrigger
}

func NewOpenAIOfficialQuotaResetRunner(
	tracker OpenAIOfficial7dResetRepository,
	quotaQuerier OpenAIQuotaUsageQuerier,
	quotaResetService *SubscriptionQuotaResetService,
	schedule string,
) *OpenAIOfficialQuotaResetRunner {
	if schedule == "" {
		schedule = openAIOfficialQuotaResetCronSchedule
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &OpenAIOfficialQuotaResetRunner{
		tracker:           tracker,
		quotaQuerier:      quotaQuerier,
		quotaResetService: quotaResetService,
		schedule:          schedule,
		now:               time.Now,
		instanceID:        uuid.NewString(),
		ctx:               ctx,
		cancel:            cancel,
		trigger:           newOpenAIOfficialQuotaResetTrigger(),
	}
}

func ProvideOpenAIOfficialQuotaResetRunner(
	accountRepo AccountRepository,
	quotaService *OpenAIQuotaService,
	quotaResetService *SubscriptionQuotaResetService,
	lockCache LeaderLockCache,
	db *sql.DB,
	auditService *AuditLogService,
	official7dResetObserver *OpenAIOfficial7dResetObserver,
) *OpenAIOfficialQuotaResetRunner {
	tracker, _ := accountRepo.(OpenAIOfficial7dResetRepository)
	runner := NewOpenAIOfficialQuotaResetRunner(
		tracker,
		quotaService,
		quotaResetService,
		openAIOfficialQuotaResetCronSchedule,
	)
	runner.SetLeaderLock(lockCache, db)
	runner.auditService = auditService
	official7dResetObserver.setDetectionNotifier(runner)
	runner.Start()
	return runner
}

func (r *OpenAIOfficialQuotaResetRunner) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if r == nil {
		return
	}
	r.lockCache = lockCache
	r.db = db
}

func (r *OpenAIOfficialQuotaResetRunner) Start() {
	if r == nil || r.tracker == nil || r.quotaQuerier == nil || r.quotaResetService == nil || r.schedule == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return
	}
	scheduler := cron.New(cron.WithParser(openAIOfficialQuotaResetCronParser), cron.WithLocation(time.Local))
	if _, err := scheduler.AddFunc(r.schedule, r.runScheduledCycle); err != nil {
		slog.Error("openai official quota reset cron start failed",
			"component", openAIOfficialQuotaResetLogComponent,
			"schedule", r.schedule,
			"error", err,
		)
		return
	}
	r.cron = scheduler
	r.started = true
	scheduler.Start()
	slog.Info("openai official quota reset cron scheduled",
		"component", openAIOfficialQuotaResetLogComponent,
		"schedule", r.schedule,
	)
}

func (r *OpenAIOfficialQuotaResetRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	r.cancel()
	scheduler := r.cron
	r.cron = nil
	r.mu.Unlock()
	if scheduler == nil {
		return
	}
	stopped := scheduler.Stop()
	select {
	case <-stopped.Done():
	case <-time.After(openAIOfficialQuotaResetStopTimeout):
		slog.Warn("openai official quota reset cron stop timed out",
			"component", openAIOfficialQuotaResetLogComponent,
			"schedule", r.schedule,
		)
	}
}

type openAIOfficialQuotaResetCycleResult struct {
	Outcome                   string
	LeaderAcquired            bool
	ActiveSubscriptionCount   int
	EligibleAccountCount      int
	ProbeAttemptedCount       int
	ProbeSucceededCount       int
	ProbeFailedCount          int
	ConfirmationCount         int
	RequiredConfirmationCount int
	AutomaticResetReady       bool
	AutoResetEnabled          bool
	ResetCount                int
	ConsumedEventCount        int
}

func (r *OpenAIOfficialQuotaResetRunner) runScheduledCycle() {
	triggeredAt := time.Now().UTC()
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(r.ctx, openAIOfficialQuotaResetCycleTimeout)
	defer cancel()
	result, err := r.runOnce(ctx)
	r.recordCycleSummary(triggeredAt, time.Since(startedAt), result, err)
}

func (r *OpenAIOfficialQuotaResetRunner) recordCycleSummary(
	triggeredAt time.Time,
	duration time.Duration,
	result openAIOfficialQuotaResetCycleResult,
	err error,
) {
	outcome := result.Outcome
	if err != nil {
		outcome = openAIOfficialQuotaResetCycleOutcomeFailed
	}
	fields := []any{
		"component", openAIOfficialQuotaResetLogComponent,
		"trigger", "cron",
		"schedule", r.schedule,
		"triggered_at", triggeredAt.UTC().Format(time.RFC3339Nano),
		"outcome", outcome,
		"leader_acquired", result.LeaderAcquired,
		"active_subscription_count", result.ActiveSubscriptionCount,
		"eligible_account_count", result.EligibleAccountCount,
		"probe_attempted_count", result.ProbeAttemptedCount,
		"probe_succeeded_count", result.ProbeSucceededCount,
		"probe_failed_count", result.ProbeFailedCount,
		"confirmation_count", result.ConfirmationCount,
		"required_confirmation_count", result.RequiredConfirmationCount,
		"automatic_reset_ready", result.AutomaticResetReady,
		"auto_reset_enabled", result.AutoResetEnabled,
		"reset_count", result.ResetCount,
		"consumed_event_count", result.ConsumedEventCount,
		"duration_ms", duration.Milliseconds(),
	}
	if err != nil {
		fields = append(fields, "error", err)
		slog.Warn("openai official quota reset cron cycle completed", fields...)
		return
	}
	slog.Info("openai official quota reset cron cycle completed", fields...)
}

func (r *OpenAIOfficialQuotaResetRunner) RunOnce(ctx context.Context) error {
	_, err := r.runOnce(ctx)
	return err
}

func (r *OpenAIOfficialQuotaResetRunner) runOnce(ctx context.Context) (openAIOfficialQuotaResetCycleResult, error) {
	result := openAIOfficialQuotaResetCycleResult{Outcome: openAIOfficialQuotaResetCycleOutcomeDependenciesUnavailable}
	if r == nil || r.tracker == nil || r.quotaQuerier == nil || r.quotaResetService == nil {
		return result, nil
	}
	r.runMutex.Lock()
	defer r.runMutex.Unlock()

	release, acquired := tryAcquireSingletonLeaderLock(
		ctx,
		r.lockCache,
		r.db,
		openAIOfficialQuotaResetLockKey,
		r.instanceID,
		openAIOfficialQuotaResetLockTTL,
	)
	if !acquired {
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeLeaderLockNotAcquired
		return result, nil
	}
	result.LeaderAcquired = true
	defer release()

	now := r.now()
	if err := r.reconcileExpiredRounds(ctx, now); err != nil {
		return result, err
	}
	status, err := r.quotaResetService.statusAt(ctx, now)
	if err != nil {
		return result, err
	}
	result.applyStatus(status)
	if status.ActiveSubscriptionCount == 0 {
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeNoActiveSubscriptions
		return result, nil
	}
	if status.AutomaticResetReady {
		if status.AutoResetEnabled && status.ActiveRound != nil {
			resetResult, resetErr := r.executeAutomaticReset(ctx, now, status.ActiveRound)
			result.applyResetResult(resetResult)
			if resetErr == nil {
				result.Outcome = openAIOfficialQuotaResetCycleOutcomeAutomaticResetExecuted
			}
			return result, resetErr
		}
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeReadyAutoResetDisabled
		return result, nil
	}
	if status.ActiveRound == nil && status.EligibleAccountCount == 0 {
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeNoEligibleAccounts
		return result, nil
	}

	candidates, err := r.tracker.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)
	if err != nil {
		return result, err
	}
	r.resetPeriodicProbeDetection()
	defer r.resetPeriodicProbeDetection()
	firstBatch := r.probeCandidateBatch(ctx, r.nextProbeCandidates(filterOpenAIQuotaResetRoundCandidates(candidates, status.ActiveRound)))
	result.addProbeBatch(firstBatch)

	refreshed, err := r.quotaResetService.statusAt(ctx, now)
	if err != nil {
		return result, err
	}
	result.applyStatus(refreshed)
	periodicDetection := r.consumePeriodicProbeDetection()
	// A newly detected reset gets one bounded confirmation batch, excluding
	// every account already attempted by this cycle.
	if periodicDetection &&
		refreshed.ConfirmationCount > status.ConfirmationCount &&
		!refreshed.AutomaticResetReady {
		refreshedCandidates, candidateErr := r.tracker.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)
		if candidateErr != nil {
			return result, candidateErr
		}
		additional := r.nextUnprobedCandidates(filterOpenAIQuotaResetRoundCandidates(refreshedCandidates, refreshed.ActiveRound), firstBatch.attempted)
		if len(additional) > 0 {
			result.addProbeBatch(r.probeCandidateBatch(ctx, additional))
			refreshed, err = r.quotaResetService.statusAt(ctx, now)
			if err != nil {
				return result, err
			}
			result.applyStatus(refreshed)
		}
	}
	if refreshed.AutomaticResetReady && refreshed.AutoResetEnabled && refreshed.ActiveRound != nil {
		resetResult, resetErr := r.executeAutomaticReset(ctx, now, refreshed.ActiveRound)
		result.applyResetResult(resetResult)
		if resetErr == nil {
			result.Outcome = openAIOfficialQuotaResetCycleOutcomeAutomaticResetExecuted
		}
		return result, resetErr
	}
	if refreshed.AutomaticResetReady {
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeReadyAutoResetDisabled
	} else if result.ProbeAttemptedCount == 0 {
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeNoUnconfirmedCandidates
	} else {
		result.Outcome = openAIOfficialQuotaResetCycleOutcomeProbed
	}
	return result, nil
}

func (r *openAIOfficialQuotaResetCycleResult) applyStatus(status *AdminResetAllQuotaStatus) {
	if r == nil || status == nil {
		return
	}
	r.ActiveSubscriptionCount = status.ActiveSubscriptionCount
	r.EligibleAccountCount = status.EligibleAccountCount
	r.ConfirmationCount = status.ConfirmationCount
	r.RequiredConfirmationCount = status.RequiredConfirmationCount
	r.AutomaticResetReady = status.AutomaticResetReady
	r.AutoResetEnabled = status.AutoResetEnabled
}

func (r *openAIOfficialQuotaResetCycleResult) addProbeBatch(batch openAIOfficialQuotaProbeBatchResult) {
	if r == nil {
		return
	}
	r.ProbeAttemptedCount += len(batch.attempted)
	r.ProbeSucceededCount += batch.succeeded
	r.ProbeFailedCount += batch.failed
}

func (r *openAIOfficialQuotaResetCycleResult) applyResetResult(result *AdminResetAllQuotaResult) {
	if r == nil || result == nil {
		return
	}
	r.ResetCount = result.ResetCount
	r.ConsumedEventCount = result.ConsumedEventCount
}

func (r *OpenAIOfficialQuotaResetRunner) executeAutomaticReset(ctx context.Context, now time.Time, round *OpenAIOfficial7dResetRound) (*AdminResetAllQuotaResult, error) {
	if round == nil || !round.Ready {
		return nil, ErrAutomaticQuotaResetPending
	}
	confirmedAccountIDs := make([]int64, 0, len(round.ConfirmedEvents))
	for _, event := range round.ConfirmedEvents {
		confirmedAccountIDs = append(confirmedAccountIDs, event.AccountID)
	}
	result, err := r.quotaResetService.AutomaticResetAllQuota(ctx, round.Anchor)
	if err != nil {
		return nil, err
	}
	r.recordAutomaticResetAudit(now, confirmedAccountIDs, result)
	slog.Info("openai official quota reset executed",
		"confirmation_count", result.ConfirmationCount,
		"consumed_event_count", result.ConsumedEventCount,
		"reset_count", result.ResetCount,
	)
	return result, nil
}

func (r *OpenAIOfficialQuotaResetRunner) reconcileExpiredRounds(ctx context.Context, now time.Time) error {
	for {
		pending, err := r.tracker.ListPendingOpenAIOfficial7dResets(ctx)
		if err != nil {
			return err
		}
		_, expired := evaluateOpenAIOfficial7dResetRounds(pending, now)
		if len(expired) == 0 {
			return nil
		}
		oldest := expired[0]
		cleared, err := r.tracker.ClearOpenAIOfficial7dResetPending(ctx, oldest.AccountID, oldest.DetectedAt)
		if err != nil {
			return err
		}
		if cleared {
			r.recordExpiredObservationAudit(now, oldest)
		} else {
			return nil
		}
		// Re-read after every CAS clear. A concurrent manual action or a newly
		// arrived event may have changed which pending event is the oldest anchor.
	}
}

func (r *OpenAIOfficialQuotaResetRunner) recordExpiredObservationAudit(now time.Time, event OpenAIOfficial7dResetState) {
	if r == nil || r.auditService == nil {
		return
	}
	r.auditService.Record(&AuditLog{
		CreatedAt:  now.UTC(),
		ActorEmail: "system",
		ActorRole:  "system",
		AuthMethod: "system",
		Action:     openAIOfficialExpiredAuditAction,
		Method:     "SYSTEM",
		Path:       "/internal/openai-official-quota-reset/reconcile",
		StatusCode: http.StatusOK,
		Extra: map[string]any{
			"account_id":  event.AccountID,
			"detected_at": event.DetectedAt.UTC().Format(time.RFC3339),
			"result":      "expired",
		},
	})
}

func filterOpenAIQuotaResetRoundCandidates(
	candidates []OpenAIOfficial7dResetCandidate,
	round *OpenAIOfficial7dResetRound,
) []OpenAIOfficial7dResetCandidate {
	if round == nil {
		return candidates
	}
	eligible := make(map[int64]struct{}, len(round.EligibleAccountIDs))
	for _, accountID := range round.EligibleAccountIDs {
		eligible[accountID] = struct{}{}
	}
	confirmed := make(map[int64]struct{}, len(round.ConfirmedEvents))
	for _, event := range round.ConfirmedEvents {
		confirmed[event.AccountID] = struct{}{}
	}
	filtered := make([]OpenAIOfficial7dResetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := eligible[candidate.AccountID]; !ok {
			continue
		}
		if _, ok := confirmed[candidate.AccountID]; ok {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func (r *OpenAIOfficialQuotaResetRunner) recordAutomaticResetAudit(
	executedAt time.Time,
	confirmedAccountIDs []int64,
	result *AdminResetAllQuotaResult,
) {
	if r == nil || r.auditService == nil || result == nil {
		return
	}
	r.auditService.Record(&AuditLog{
		CreatedAt:  executedAt.UTC(),
		ActorEmail: "system",
		ActorRole:  "system",
		AuthMethod: "system",
		Action:     openAIOfficialAutoResetAuditAction,
		Method:     "SYSTEM",
		Path:       "/internal/openai-official-quota-reset/execute",
		StatusCode: http.StatusOK,
		Extra: map[string]any{
			"confirmed_account_ids": confirmedAccountIDs,
			"confirmation_count":    result.ConfirmationCount,
			"consumed_event_count":  result.ConsumedEventCount,
			"reset_count":           result.ResetCount,
			"result":                "executed",
		},
	})
}

func (r *OpenAIOfficialQuotaResetRunner) nextProbeCandidates(
	candidates []OpenAIOfficial7dResetCandidate,
) []OpenAIOfficial7dResetCandidate {
	unconfirmed := make([]OpenAIOfficial7dResetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Pending {
			unconfirmed = append(unconfirmed, candidate)
		}
	}
	if len(unconfirmed) == 0 {
		return nil
	}
	sort.Slice(unconfirmed, func(i, j int) bool {
		return unconfirmed[i].AccountID < unconfirmed[j].AccountID
	})

	start := sort.Search(len(unconfirmed), func(i int) bool {
		return unconfirmed[i].AccountID > r.probeCursorAccountID
	})
	if start == len(unconfirmed) {
		start = 0
	}
	count := min(len(unconfirmed), openAIOfficialQuotaResetMaxProbeCount)
	selected := make([]OpenAIOfficial7dResetCandidate, 0, count)
	for offset := 0; offset < count; offset++ {
		selected = append(selected, unconfirmed[(start+offset)%len(unconfirmed)])
	}
	return selected
}
