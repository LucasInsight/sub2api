package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type UserSubscriptionRepository interface {
	Create(ctx context.Context, sub *UserSubscription) error
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error)
	GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	Update(ctx context.Context, sub *UserSubscription) error
	Delete(ctx context.Context, id int64) error
	Restore(ctx context.Context, subscriptionID int64, restoredStatus string) (*UserSubscription, error)

	ListByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error)
	List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error)

	ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) error
	UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error

	ActivateWindows(ctx context.Context, id int64, fiveHourStart, calendarWindowStart time.Time) error
	ActivateFiveHourWindow(ctx context.Context, id int64, fiveHourStart time.Time) error
	ResetFiveHourUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, newWindowStart time.Time) error
	ResetDailyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	ResetWeeklyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	ResetMonthlyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	IncrementUsage(ctx context.Context, id int64, costUSD float64) error

	BatchUpdateExpiredStatus(ctx context.Context) (int64, error)
}

// ActiveUserSubscriptionQuotaResetRepository is the bounded extension used by
// the global admin reset flow. Keeping it separate avoids broadening every
// gateway-facing UserSubscriptionRepository test double.
type ActiveUserSubscriptionQuotaResetRepository interface {
	ListAllActiveForQuotaReset(ctx context.Context, now time.Time) ([]UserSubscription, error)
}

type OpenAIOfficial7dResetState struct {
	AccountID  int64
	DetectedAt time.Time
}

// OpenAIOfficial7dResetRepository persists authoritative 7d window
// observations and coordinates consumption of pending early-reset events.
type OpenAIOfficial7dResetRepository interface {
	ObserveOpenAI7dReset(ctx context.Context, accountID int64, observedAt, resetAt time.Time, boundaryGrace time.Duration) (bool, error)
	ListPendingOpenAIOfficial7dResets(ctx context.Context) ([]OpenAIOfficial7dResetState, error)
	MarkOpenAIOfficial7dResetsHandled(ctx context.Context, accountIDs []int64, handledAt time.Time) error
}
