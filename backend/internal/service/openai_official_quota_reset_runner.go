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
)

const (
	openAIOfficialQuotaResetInterval      = 5 * time.Minute
	openAIOfficialQuotaResetLockKey       = "subscription:openai-official-quota-reset:leader"
	openAIOfficialQuotaResetLockTTL       = 3 * time.Minute
	openAIOfficialQuotaResetCycleTimeout  = 90 * time.Second
	openAIOfficialQuotaResetProbeTimeout  = 20 * time.Second
	openAIOfficialQuotaResetMaxProbeCount = 3
	openAIOfficialAutoResetAuditAction    = "system.openai.quota.auto_reset_executed"
	openAIOfficialExpiredAuditAction      = "system.openai.quota.observation_expired"
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
	interval          time.Duration
	now               func() time.Time

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	wg                   sync.WaitGroup
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
	interval time.Duration,
) *OpenAIOfficialQuotaResetRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &OpenAIOfficialQuotaResetRunner{
		tracker:           tracker,
		quotaQuerier:      quotaQuerier,
		quotaResetService: quotaResetService,
		interval:          interval,
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
		openAIOfficialQuotaResetInterval,
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
	if r == nil || r.tracker == nil || r.quotaQuerier == nil || r.quotaResetService == nil || r.interval <= 0 {
		return
	}
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.wg.Add(1)
	r.mu.Unlock()
	go r.runLoop()
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
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *OpenAIOfficialQuotaResetRunner) runLoop() {
	defer r.wg.Done()
	r.runCycle()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.runCycle()
		case <-r.immediateProbeWakeChannel():
			r.runTriggeredCycle()
		}
	}
}

func (r *OpenAIOfficialQuotaResetRunner) runCycle() {
	ctx, cancel := context.WithTimeout(r.ctx, openAIOfficialQuotaResetCycleTimeout)
	defer cancel()
	if err := r.RunOnce(ctx); err != nil {
		slog.Warn("openai official quota reset cycle failed", "error", err)
	}
}

func (r *OpenAIOfficialQuotaResetRunner) RunOnce(ctx context.Context) error {
	if r == nil || r.tracker == nil || r.quotaQuerier == nil || r.quotaResetService == nil {
		return nil
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
		return nil
	}
	defer release()

	now := r.now()
	if err := r.reconcileExpiredRounds(ctx, now); err != nil {
		return err
	}
	status, err := r.quotaResetService.statusAt(ctx, now)
	if err != nil {
		return err
	}
	if status.ActiveSubscriptionCount == 0 {
		return nil
	}
	if status.AutomaticResetReady {
		if status.AutoResetEnabled && status.ActiveRound != nil {
			return r.executeAutomaticReset(ctx, now, status.ActiveRound)
		}
		return nil
	}
	if status.ActiveRound == nil && status.EligibleAccountCount == 0 {
		return nil
	}

	candidates, err := r.tracker.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)
	if err != nil {
		return err
	}
	r.resetPeriodicProbeDetection()
	defer r.resetPeriodicProbeDetection()
	attempted := r.probeCandidateBatch(ctx, r.nextProbeCandidates(filterOpenAIQuotaResetRoundCandidates(candidates, status.ActiveRound)))

	refreshed, err := r.quotaResetService.statusAt(ctx, now)
	if err != nil {
		return err
	}
	periodicDetection := r.consumePeriodicProbeDetection()
	// A newly detected reset gets one bounded confirmation batch, excluding
	// every account already attempted by this cycle.
	if periodicDetection &&
		refreshed.ConfirmationCount > status.ConfirmationCount &&
		!refreshed.AutomaticResetReady {
		refreshedCandidates, candidateErr := r.tracker.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)
		if candidateErr != nil {
			return candidateErr
		}
		additional := r.nextUnprobedCandidates(filterOpenAIQuotaResetRoundCandidates(refreshedCandidates, refreshed.ActiveRound), attempted)
		if len(additional) > 0 {
			r.probeCandidateBatch(ctx, additional)
			refreshed, err = r.quotaResetService.statusAt(ctx, now)
			if err != nil {
				return err
			}
		}
	}
	if refreshed.AutomaticResetReady && refreshed.AutoResetEnabled && refreshed.ActiveRound != nil {
		return r.executeAutomaticReset(ctx, now, refreshed.ActiveRound)
	}
	return nil
}

func (r *OpenAIOfficialQuotaResetRunner) executeAutomaticReset(ctx context.Context, now time.Time, round *OpenAIOfficial7dResetRound) error {
	if round == nil || !round.Ready {
		return ErrAutomaticQuotaResetPending
	}
	confirmedAccountIDs := make([]int64, 0, len(round.ConfirmedEvents))
	for _, event := range round.ConfirmedEvents {
		confirmedAccountIDs = append(confirmedAccountIDs, event.AccountID)
	}
	result, err := r.quotaResetService.AutomaticResetAllQuota(ctx, round.Anchor)
	if err != nil {
		return err
	}
	r.recordAutomaticResetAudit(now, confirmedAccountIDs, result)
	slog.Info("openai official quota reset executed",
		"confirmation_count", result.ConfirmationCount,
		"consumed_event_count", result.ConsumedEventCount,
		"reset_count", result.ResetCount,
	)
	return nil
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
