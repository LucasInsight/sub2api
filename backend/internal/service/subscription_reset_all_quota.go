package service

import (
	"context"
	"log/slog"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrResetAllQuotaUnavailable       = infraerrors.ServiceUnavailable("RESET_ALL_QUOTA_UNAVAILABLE", "reset-all subscription quota dependencies are unavailable")
	ErrResetAllQuotaAckRequired       = infraerrors.BadRequest("RESET_ALL_QUOTA_ACKNOWLEDGEMENT_REQUIRED", "reset-all quota acknowledgement is required")
	ErrQuotaResetAutomationDisabled   = infraerrors.Conflict("QUOTA_RESET_AUTOMATION_DISABLED", "quota reset automation is disabled")
	ErrQuotaResetAutomationNotifyOnly = infraerrors.Conflict("QUOTA_RESET_AUTOMATION_NOTIFY_ONLY", "quota reset automation is currently notify-only")
	ErrAutomaticQuotaResetPending     = infraerrors.Conflict("AUTOMATIC_QUOTA_RESET_NOT_CONFIRMED", "automatic quota reset does not have enough account confirmations")
)

type quotaResetAutomationMode string

const (
	quotaResetAutomationModeNotifyOnly quotaResetAutomationMode = "notify_only"
	quotaResetAutomationModeExecute    quotaResetAutomationMode = "execute"

	currentQuotaResetAutomationMode = quotaResetAutomationModeNotifyOnly
)

type SubscriptionQuotaResetService struct {
	subscriptionService *SubscriptionService
	tracker             OpenAIOfficial7dResetRepository
	settings            OpenAIQuotaResetAutomationSettings
}

func ProvideSubscriptionQuotaResetService(
	subscriptionService *SubscriptionService,
	accountRepo AccountRepository,
	settingService *SettingService,
) *SubscriptionQuotaResetService {
	tracker, _ := accountRepo.(OpenAIOfficial7dResetRepository)
	return NewSubscriptionQuotaResetService(subscriptionService, tracker, settingService)
}

func NewSubscriptionQuotaResetService(
	subscriptionService *SubscriptionService,
	tracker OpenAIOfficial7dResetRepository,
	settings OpenAIQuotaResetAutomationSettings,
) *SubscriptionQuotaResetService {
	return &SubscriptionQuotaResetService{
		subscriptionService: subscriptionService,
		tracker:             tracker,
		settings:            settings,
	}
}

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
	if s == nil || sub == nil || !windows.any() {
		return ErrInvalidInput
	}
	repo := s.userSubRepo
	if windows.fiveHour {
		if err := repo.ResetFiveHourUsage(ctx, sub.ID, now); err != nil {
			return err
		}
	}
	if windows.daily || windows.weekly || windows.monthly {
		if err := repo.ResetUsageWindows(ctx, sub.ID, windows.daily, windows.weekly, windows.monthly, now); err != nil {
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
	if s == nil || len(targets) == 0 {
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
	Enabled                   bool       `json:"enabled"`
	AutoResetEnabled          bool       `json:"auto_reset_enabled"`
	PendingEventCount         int        `json:"pending_event_count"`
	ActiveSubscriptionCount   int        `json:"active_subscription_count"`
	EligibleAccountCount      int        `json:"eligible_account_count"`
	ConfirmationCount         int        `json:"confirmation_count"`
	RequiredConfirmationCount int        `json:"required_confirmation_count"`
	AutomaticResetReady       bool       `json:"automatic_reset_ready"`
	LatestDetectedAt          *time.Time `json:"latest_detected_at,omitempty"`
	LastHandledAt             *time.Time `json:"last_handled_at,omitempty"`
	DisabledReason            string     `json:"disabled_reason,omitempty"`
}

type AdminResetAllQuotaResult struct {
	ResetCount         int `json:"reset_count"`
	ConsumedEventCount int `json:"consumed_event_count"`
	ConfirmationCount  int `json:"confirmation_count"`
}

func (s *SubscriptionQuotaResetService) Status(ctx context.Context) (*AdminResetAllQuotaStatus, error) {
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
	candidates, err := tracker.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)
	if err != nil {
		return nil, err
	}
	autoResetEnabled, err := s.AutomationEnabled(ctx)
	if err != nil {
		return nil, err
	}

	status := &AdminResetAllQuotaStatus{
		AutoResetEnabled:          autoResetEnabled,
		PendingEventCount:         len(pending),
		ActiveSubscriptionCount:   len(active),
		EligibleAccountCount:      len(candidates),
		RequiredConfirmationCount: automaticQuotaResetConfirmationRequirement(len(candidates)),
	}
	for i := range pending {
		if status.LatestDetectedAt == nil || pending[i].DetectedAt.After(*status.LatestDetectedAt) {
			detectedAt := pending[i].DetectedAt
			status.LatestDetectedAt = &detectedAt
		}
	}
	for i := range candidates {
		candidate := candidates[i]
		if candidate.Pending {
			status.ConfirmationCount++
		}
		if candidate.HandledAt != nil && (status.LastHandledAt == nil || candidate.HandledAt.After(*status.LastHandledAt)) {
			handledAt := *candidate.HandledAt
			status.LastHandledAt = &handledAt
		}
	}
	status.AutomaticResetReady = status.RequiredConfirmationCount > 0 && status.ConfirmationCount >= status.RequiredConfirmationCount
	if len(active) == 0 {
		status.DisabledReason = "no_active_subscriptions"
	} else {
		status.Enabled = true
	}
	return status, nil
}

func automaticQuotaResetConfirmationRequirement(eligibleAccountCount int) int {
	switch {
	case eligibleAccountCount <= 0:
		return 0
	case eligibleAccountCount == 1:
		return 1
	default:
		return 2
	}
}

func (s *SubscriptionQuotaResetService) AutomationEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.settings == nil {
		return false, nil
	}
	return s.settings.GetOpenAIOfficialQuotaAutoResetEnabled(ctx)
}

func (s *SubscriptionQuotaResetService) SetAutomationEnabled(ctx context.Context, enabled bool) error {
	if s == nil || s.settings == nil {
		return ErrResetAllQuotaUnavailable
	}
	return s.settings.SetOpenAIOfficialQuotaAutoResetEnabled(ctx, enabled)
}

func (s *SubscriptionQuotaResetService) AdminResetAllQuota(ctx context.Context, acknowledged bool) (*AdminResetAllQuotaResult, error) {
	if !acknowledged {
		return nil, ErrResetAllQuotaAckRequired
	}
	return s.resetAllSubscriptionQuotas(ctx, nil, false)
}

func (s *SubscriptionQuotaResetService) AutomaticResetAllQuota(ctx context.Context, confirmedAccountIDs []int64) (*AdminResetAllQuotaResult, error) {
	enabled, err := s.AutomationEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrQuotaResetAutomationDisabled
	}
	if currentQuotaResetAutomationMode != quotaResetAutomationModeExecute {
		return nil, ErrQuotaResetAutomationNotifyOnly
	}
	return s.resetAllSubscriptionQuotas(ctx, confirmedAccountIDs, true)
}

func (s *SubscriptionQuotaResetService) resetAllSubscriptionQuotas(
	ctx context.Context,
	confirmedAccountIDs []int64,
	requireAutomaticConfirmation bool,
) (*AdminResetAllQuotaResult, error) {
	lister, tracker, err := s.resetAllQuotaDependencies()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var active []UserSubscription
	var pending []OpenAIOfficial7dResetState
	confirmationCount := 0

	err = s.subscriptionService.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		var txErr error
		pending, txErr = tracker.ListPendingOpenAIOfficial7dResets(txCtx)
		if txErr != nil {
			return txErr
		}
		if requireAutomaticConfirmation {
			candidates, candidateErr := tracker.ListEligibleOpenAIOfficial7dResetCandidates(txCtx, now)
			if candidateErr != nil {
				return candidateErr
			}
			confirmedSet := make(map[int64]struct{}, len(confirmedAccountIDs))
			for _, accountID := range confirmedAccountIDs {
				confirmedSet[accountID] = struct{}{}
			}
			for i := range candidates {
				candidate := candidates[i]
				if !candidate.Pending {
					continue
				}
				if _, ok := confirmedSet[candidate.AccountID]; ok {
					confirmationCount++
				}
			}
			if confirmationCount < automaticQuotaResetConfirmationRequirement(len(candidates)) {
				return ErrAutomaticQuotaResetPending
			}
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
			if txErr := s.subscriptionService.resetSubscriptionQuotaWindows(txCtx, &active[i], windows, now); txErr != nil {
				return txErr
			}
		}

		return tracker.MarkAllOpenAIOfficial7dResetsHandled(txCtx, now)
	})
	if err != nil {
		return nil, err
	}

	targets := make([]subscriptionQuotaResetCacheTarget, 0, len(active))
	for i := range active {
		targets = append(targets, subscriptionQuotaResetCacheTarget{userID: active[i].UserID, groupID: active[i].GroupID})
	}
	s.subscriptionService.invalidateQuotaResetCaches(targets)
	return &AdminResetAllQuotaResult{
		ResetCount:         len(active),
		ConsumedEventCount: len(pending),
		ConfirmationCount:  confirmationCount,
	}, nil
}

func (s *SubscriptionQuotaResetService) resetAllQuotaDependencies() (ActiveUserSubscriptionQuotaResetRepository, OpenAIOfficial7dResetRepository, error) {
	if s == nil || s.subscriptionService == nil || s.subscriptionService.userSubRepo == nil || s.tracker == nil {
		return nil, nil, ErrResetAllQuotaUnavailable
	}
	lister, ok := s.subscriptionService.userSubRepo.(ActiveUserSubscriptionQuotaResetRepository)
	if !ok {
		return nil, nil, ErrResetAllQuotaUnavailable
	}
	return lister, s.tracker, nil
}
