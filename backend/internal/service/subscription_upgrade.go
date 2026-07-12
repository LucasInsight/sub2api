package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSubscriptionUpgradeUnavailable      = infraerrors.ServiceUnavailable("SUBSCRIPTION_UPGRADE_UNAVAILABLE", "subscription upgrade is unavailable")
	ErrSubscriptionUpgradeSourceInvalid    = infraerrors.Conflict("SUBSCRIPTION_UPGRADE_SOURCE_INVALID", "source subscription must be active and unexpired")
	ErrSubscriptionUpgradeSameGroup        = infraerrors.BadRequest("SUBSCRIPTION_UPGRADE_SAME_GROUP", "target group must differ from source group")
	ErrSubscriptionUpgradeTargetInactive   = infraerrors.BadRequest("SUBSCRIPTION_UPGRADE_TARGET_INACTIVE", "target group must be active")
	ErrSubscriptionUpgradePlatformMismatch = infraerrors.BadRequest("SUBSCRIPTION_UPGRADE_PLATFORM_MISMATCH", "target group must use the same platform as the source group")
	ErrSubscriptionUpgradeTargetExists     = infraerrors.Conflict("SUBSCRIPTION_UPGRADE_TARGET_EXISTS", "user already has a subscription for the target group")
)

type UpgradeSubscriptionInput struct {
	SubscriptionID int64
	TargetGroupID  int64
	AssignedBy     int64
}

type UpgradeSubscriptionResult struct {
	SourceSubscriptionID int64
	Subscription         *UserSubscription
	MigratedAPIKeyCount  int64
}

// ProvideSubscriptionService wires the optional dependencies used by the admin upgrade flow.
func ProvideSubscriptionService(
	groupRepo GroupRepository,
	userSubRepo UserSubscriptionRepository,
	apiKeyRepo APIKeyRepository,
	billingCacheService *BillingCacheService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	entClient *dbent.Client,
	cfg *config.Config,
) *SubscriptionService {
	svc := NewSubscriptionService(groupRepo, userSubRepo, billingCacheService, entClient, cfg)
	svc.apiKeyRepo = apiKeyRepo
	svc.authCacheInvalidator = authCacheInvalidator
	return svc
}

func (s *SubscriptionService) UpgradeSubscription(ctx context.Context, input UpgradeSubscriptionInput) (*UpgradeSubscriptionResult, error) {
	if input.SubscriptionID <= 0 || input.TargetGroupID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_SUBSCRIPTION_UPGRADE", "source subscription and target group are required")
	}
	if s.entClient == nil || s.apiKeyRepo == nil {
		return nil, ErrSubscriptionUpgradeUnavailable
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin subscription upgrade transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	if _, err := tx.Client().UserSubscription.Query().
		Where(usersubscription.IDEQ(input.SubscriptionID)).
		ForUpdate().
		OnlyID(txCtx); err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, fmt.Errorf("lock source subscription: %w", err)
	}

	source, err := s.userSubRepo.GetByID(txCtx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if source.Status != SubscriptionStatusActive || !source.ExpiresAt.After(now) {
		return nil, ErrSubscriptionUpgradeSourceInvalid
	}
	if source.GroupID == input.TargetGroupID {
		return nil, ErrSubscriptionUpgradeSameGroup
	}

	targetGroup, err := s.groupRepo.GetByID(txCtx, input.TargetGroupID)
	if err != nil {
		return nil, err
	}
	if !targetGroup.IsSubscriptionType() {
		return nil, ErrGroupNotSubscriptionType
	}
	if !targetGroup.IsActive() {
		return nil, ErrSubscriptionUpgradeTargetInactive
	}
	if source.Group == nil {
		source.Group, err = s.groupRepo.GetByID(txCtx, source.GroupID)
		if err != nil {
			return nil, err
		}
	}
	if source.Group.Platform != targetGroup.Platform {
		return nil, ErrSubscriptionUpgradePlatformMismatch
	}

	exists, err := s.userSubRepo.ExistsByUserIDAndGroupID(txCtx, source.UserID, input.TargetGroupID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrSubscriptionUpgradeTargetExists
	}

	target := upgradedSubscriptionFromSource(source, targetGroup, input.AssignedBy, now)
	if err := s.userSubRepo.Create(txCtx, target); err != nil {
		return nil, fmt.Errorf("create upgraded subscription: %w", err)
	}
	if err := s.userSubRepo.UpdateNotes(txCtx, source.ID, appendSubscriptionNotes(
		source.Notes,
		fmt.Sprintf("Upgraded to subscription %d (group %d)", target.ID, target.GroupID),
	)); err != nil {
		return nil, fmt.Errorf("record source subscription upgrade: %w", err)
	}

	migratedKeys, err := s.apiKeyRepo.UpdateGroupIDByUserAndGroup(txCtx, source.UserID, source.GroupID, target.GroupID)
	if err != nil {
		return nil, fmt.Errorf("migrate subscription api keys: %w", err)
	}
	if err := s.userSubRepo.Delete(txCtx, source.ID); err != nil {
		return nil, fmt.Errorf("revoke source subscription: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit subscription upgrade: %w", err)
	}

	s.invalidateUpgradeCaches(ctx, source.UserID, source.GroupID, target.GroupID)
	if refreshed, refreshErr := s.userSubRepo.GetByID(ctx, target.ID); refreshErr == nil {
		target = refreshed
	} else {
		target.User = source.User
		target.Group = targetGroup
		slog.Warn("subscription upgrade committed but target reload failed", "subscription_id", target.ID, "error", refreshErr)
	}

	return &UpgradeSubscriptionResult{
		SourceSubscriptionID: source.ID,
		Subscription:         target,
		MigratedAPIKeyCount:  migratedKeys,
	}, nil
}

func upgradedSubscriptionFromSource(source *UserSubscription, targetGroup *Group, assignedBy int64, assignedAt time.Time) *UserSubscription {
	target := &UserSubscription{
		UserID:              source.UserID,
		GroupID:             targetGroup.ID,
		StartsAt:            source.StartsAt,
		ExpiresAt:           source.ExpiresAt,
		Status:              SubscriptionStatusActive,
		FiveHourWindowStart: source.FiveHourWindowStart,
		DailyWindowStart:    source.DailyWindowStart,
		WeeklyWindowStart:   source.WeeklyWindowStart,
		MonthlyWindowStart:  source.MonthlyWindowStart,
		FiveHourUsageUSD:    source.FiveHourUsageUSD,
		DailyUsageUSD:       source.DailyUsageUSD,
		WeeklyUsageUSD:      source.WeeklyUsageUSD,
		MonthlyUsageUSD:     source.MonthlyUsageUSD,
		AssignedAt:          assignedAt,
		Notes:               appendSubscriptionNotes(source.Notes, fmt.Sprintf("Upgraded from subscription %d (group %d)", source.ID, source.GroupID)),
	}
	if assignedBy > 0 {
		target.AssignedBy = &assignedBy
	}
	return target
}

func (s *SubscriptionService) invalidateUpgradeCaches(ctx context.Context, userID, sourceGroupID, targetGroupID int64) {
	s.InvalidateSubCacheSync(userID, sourceGroupID)
	s.InvalidateSubCacheSync(userID, targetGroupID)
	if s.billingCacheService != nil {
		if err := s.billingCacheService.InvalidateSubscription(ctx, userID, sourceGroupID); err != nil {
			slog.Warn("invalidate source subscription cache after upgrade", "user_id", userID, "group_id", sourceGroupID, "error", err)
		}
		if err := s.billingCacheService.InvalidateSubscription(ctx, userID, targetGroupID); err != nil {
			slog.Warn("invalidate target subscription cache after upgrade", "user_id", userID, "group_id", targetGroupID, "error", err)
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
}
