package service

import (
	"context"
	"database/sql"
	"log/slog"
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
)

type OpenAIQuotaUsageQuerier interface {
	QueryUsageSnapshot(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error)
}

// OpenAIOfficialQuotaResetRunner actively confirms official early 7-day
// resets. The current notify-only mode leaves durable pending markers for an
// administrator to consume through the manual reset action.
type OpenAIOfficialQuotaResetRunner struct {
	tracker           OpenAIOfficial7dResetRepository
	quotaQuerier      OpenAIQuotaUsageQuerier
	quotaResetService *SubscriptionQuotaResetService
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
	}
}

func ProvideOpenAIOfficialQuotaResetRunner(
	accountRepo AccountRepository,
	quotaService *OpenAIQuotaService,
	quotaResetService *SubscriptionQuotaResetService,
	lockCache LeaderLockCache,
	db *sql.DB,
) *OpenAIOfficialQuotaResetRunner {
	tracker, _ := accountRepo.(OpenAIOfficial7dResetRepository)
	runner := NewOpenAIOfficialQuotaResetRunner(
		tracker,
		quotaService,
		quotaResetService,
		openAIOfficialQuotaResetInterval,
	)
	runner.SetLeaderLock(lockCache, db)
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
	automationEnabled, err := r.quotaResetService.AutomationEnabled(ctx)
	if err != nil {
		return err
	}
	if !automationEnabled {
		return nil
	}
	status, err := r.quotaResetService.Status(ctx)
	if err != nil {
		return err
	}
	if status.ActiveSubscriptionCount == 0 || status.EligibleAccountCount == 0 || status.AutomaticResetReady {
		return nil
	}

	candidates, err := r.tracker.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)
	if err != nil {
		return err
	}
	for _, candidate := range r.nextProbeCandidates(candidates) {
		accountID := candidate.AccountID
		r.probeCursorAccountID = accountID
		probeCtx, cancel := context.WithTimeout(ctx, openAIOfficialQuotaResetProbeTimeout)
		_, probeErr := r.quotaQuerier.QueryUsageSnapshot(probeCtx, accountID)
		cancel()
		if probeErr != nil {
			slog.Warn("openai official quota reset probe failed", "account_id", accountID, "error", probeErr)
			continue
		}
	}

	refreshed, err := r.quotaResetService.Status(ctx)
	if err != nil {
		return err
	}
	if !status.AutomaticResetReady && refreshed.AutomaticResetReady {
		slog.Info("openai official quota reset requires manual confirmation",
			"confirmation_count", refreshed.ConfirmationCount,
			"required_confirmation_count", refreshed.RequiredConfirmationCount,
		)
	}
	return nil
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
