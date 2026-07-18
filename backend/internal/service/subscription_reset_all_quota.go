package service

import (
	"context"
	"log/slog"
	"time"
)

type subscriptionQuotaResetWindows struct {
	fiveHour bool
	daily    bool
	weekly   bool
	monthly  bool
}

func (w subscriptionQuotaResetWindows) any() bool {
	return w.fiveHour || w.daily || w.weekly || w.monthly
}

func (s *SubscriptionService) resetSubscriptionQuotaWindows(
	ctx context.Context,
	sub *UserSubscription,
	windows subscriptionQuotaResetWindows,
	now time.Time,
) error {
	if sub == nil || !windows.any() {
		return ErrInvalidInput
	}
	if windows.fiveHour {
		if err := s.userSubRepo.ResetFiveHourUsage(ctx, sub.ID, now); err != nil {
			return err
		}
	}
	if windows.daily || windows.weekly || windows.monthly {
		if err := s.userSubRepo.ResetUsageWindows(
			ctx,
			sub.ID,
			windows.daily,
			windows.weekly,
			windows.monthly,
			startOfDay(now),
		); err != nil {
			return err
		}
	}
	return nil
}

type subscriptionQuotaResetCacheTarget struct {
	userID  int64
	groupID int64
}

func (s *SubscriptionService) invalidateQuotaResetCaches(targets []subscriptionQuotaResetCacheTarget) {
	if len(targets) == 0 {
		return
	}
	if s.subCacheL1 != nil {
		for _, target := range targets {
			s.subCacheL1.Del(subCacheKey(target.userID, target.groupID))
		}
		s.subCacheL1.Wait()
	}
	if s.billingCacheService == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, target := range targets {
		if err := s.billingCacheService.InvalidateSubscription(ctx, target.userID, target.groupID); err != nil {
			slog.Warn("invalidate subscription billing cache after quota reset", "user_id", target.userID, "group_id", target.groupID, "error", err)
			continue
		}
		if err := s.billingCacheService.PublishSubscriptionCacheInvalidation(ctx, subCacheKey(target.userID, target.groupID)); err != nil {
			slog.Warn("publish subscription cache invalidation after quota reset", "user_id", target.userID, "group_id", target.groupID, "error", err)
		}
	}
}

type AdminResetAllQuotaStatus struct {
	Enabled                 bool       `json:"enabled"`
	PendingEventCount       int        `json:"pending_event_count"`
	ActiveSubscriptionCount int        `json:"active_subscription_count"`
	LatestDetectedAt        *time.Time `json:"latest_detected_at,omitempty"`
	DisabledReason          string     `json:"disabled_reason,omitempty"`
}

type AdminResetAllQuotaResult struct {
	ResetCount         int `json:"reset_count"`
	ConsumedEventCount int `json:"consumed_event_count"`
}

func (s *SubscriptionService) AdminResetAllQuotaStatus(ctx context.Context) (*AdminResetAllQuotaStatus, error) {
	lister, tracker, err := s.resetAllQuotaDependencies()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	pending, err := tracker.ListPendingOpenAIOfficial7dResets(ctx)
	if err != nil {
		return nil, err
	}
	active, err := lister.ListAllActiveForQuotaReset(ctx, now)
	if err != nil {
		return nil, err
	}

	status := &AdminResetAllQuotaStatus{
		PendingEventCount:       len(pending),
		ActiveSubscriptionCount: len(active),
	}
	for i := range pending {
		if status.LatestDetectedAt == nil || pending[i].DetectedAt.After(*status.LatestDetectedAt) {
			detectedAt := pending[i].DetectedAt
			status.LatestDetectedAt = &detectedAt
		}
	}
	switch {
	case len(pending) == 0:
		status.DisabledReason = "no_early_7d_reset"
	case len(active) == 0:
		status.DisabledReason = "no_active_subscriptions"
	default:
		status.Enabled = true
	}
	return status, nil
}

func (s *SubscriptionService) AdminResetAllQuota(ctx context.Context) (*AdminResetAllQuotaResult, error) {
	lister, tracker, err := s.resetAllQuotaDependencies()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var active []UserSubscription
	var pending []OpenAIOfficial7dResetState

	err = s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		var txErr error
		pending, txErr = tracker.ListPendingOpenAIOfficial7dResets(txCtx)
		if txErr != nil {
			return txErr
		}
		if len(pending) == 0 {
			return ErrOfficialEarlyResetRequired
		}
		active, txErr = lister.ListAllActiveForQuotaReset(txCtx, now)
		if txErr != nil {
			return txErr
		}
		if len(active) == 0 {
			return ErrNoActiveSubscriptions
		}

		windows := subscriptionQuotaResetWindows{fiveHour: true, daily: true, weekly: true, monthly: true}
		for i := range active {
			if txErr := s.resetSubscriptionQuotaWindows(txCtx, &active[i], windows, now); txErr != nil {
				return txErr
			}
		}

		accountIDs := make([]int64, 0, len(pending))
		for i := range pending {
			accountIDs = append(accountIDs, pending[i].AccountID)
		}
		return tracker.MarkOpenAIOfficial7dResetsHandled(txCtx, accountIDs, now)
	})
	if err != nil {
		return nil, err
	}

	targets := make([]subscriptionQuotaResetCacheTarget, 0, len(active))
	for i := range active {
		targets = append(targets, subscriptionQuotaResetCacheTarget{userID: active[i].UserID, groupID: active[i].GroupID})
	}
	s.invalidateQuotaResetCaches(targets)
	return &AdminResetAllQuotaResult{ResetCount: len(active), ConsumedEventCount: len(pending)}, nil
}

func (s *SubscriptionService) resetAllQuotaDependencies() (ActiveUserSubscriptionQuotaResetRepository, OpenAIOfficial7dResetRepository, error) {
	if s == nil || s.userSubRepo == nil || s.official7dResetRepo == nil {
		return nil, nil, ErrResetAllQuotaUnavailable
	}
	lister, ok := s.userSubRepo.(ActiveUserSubscriptionQuotaResetRepository)
	if !ok {
		return nil, nil, ErrResetAllQuotaUnavailable
	}
	return lister, s.official7dResetRepo, nil
}
