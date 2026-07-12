//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionUpgradeCopiesStateMigratesKeysAndRevokesSource(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: fmt.Sprintf("upgrade-%d@example.com", suffix)})
	sourceGroup := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:             fmt.Sprintf("upgrade-source-%d", suffix),
		Platform:         service.PlatformAnthropic,
		Status:           service.StatusActive,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	targetGroup := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:             fmt.Sprintf("upgrade-target-%d", suffix),
		Platform:         service.PlatformAnthropic,
		Status:           service.StatusActive,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})

	subRepo := NewUserSubscriptionRepository(integrationEntClient)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	groupRepo := NewGroupRepository(integrationEntClient, integrationDB)
	startsAt := time.Now().Add(-6 * time.Hour).Truncate(time.Microsecond)
	expiresAt := time.Now().Add(42 * time.Hour).Truncate(time.Microsecond)
	fiveHourStart := time.Now().Add(-2 * time.Hour).Truncate(time.Microsecond)
	dailyStart := time.Now().Add(-3 * time.Hour).Truncate(time.Microsecond)
	weeklyStart := time.Now().Add(-48 * time.Hour).Truncate(time.Microsecond)
	monthlyStart := time.Now().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	source := &service.UserSubscription{
		UserID:              user.ID,
		GroupID:             sourceGroup.ID,
		StartsAt:            startsAt,
		ExpiresAt:           expiresAt,
		Status:              service.SubscriptionStatusActive,
		FiveHourWindowStart: &fiveHourStart,
		DailyWindowStart:    &dailyStart,
		WeeklyWindowStart:   &weeklyStart,
		MonthlyWindowStart:  &monthlyStart,
		FiveHourUsageUSD:    1.25,
		DailyUsageUSD:       2.5,
		WeeklyUsageUSD:      3.75,
		MonthlyUsageUSD:     4.5,
		AssignedAt:          startsAt,
		Notes:               "original",
	}
	require.NoError(t, subRepo.Create(ctx, source))
	key := &service.APIKey{
		UserID:  user.ID,
		Key:     fmt.Sprintf("sk-upgrade-%d", suffix),
		Name:    "upgrade key",
		GroupID: &sourceGroup.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, apiKeyRepo.Create(ctx, key))

	svc := service.ProvideSubscriptionService(groupRepo, subRepo, apiKeyRepo, nil, nil, integrationEntClient, nil)
	result, err := svc.UpgradeSubscription(ctx, service.UpgradeSubscriptionInput{
		SubscriptionID: source.ID,
		TargetGroupID:  targetGroup.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.MigratedAPIKeyCount)

	target, err := subRepo.GetByID(ctx, result.Subscription.ID)
	require.NoError(t, err)
	require.Equal(t, targetGroup.ID, target.GroupID)
	require.WithinDuration(t, startsAt, target.StartsAt, time.Microsecond)
	require.WithinDuration(t, expiresAt, target.ExpiresAt, time.Microsecond)
	require.WithinDuration(t, fiveHourStart, *target.FiveHourWindowStart, time.Microsecond)
	require.WithinDuration(t, dailyStart, *target.DailyWindowStart, time.Microsecond)
	require.WithinDuration(t, weeklyStart, *target.WeeklyWindowStart, time.Microsecond)
	require.WithinDuration(t, monthlyStart, *target.MonthlyWindowStart, time.Microsecond)
	require.InDelta(t, 1.25, target.FiveHourUsageUSD, 1e-9)
	require.InDelta(t, 2.5, target.DailyUsageUSD, 1e-9)
	require.InDelta(t, 3.75, target.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 4.5, target.MonthlyUsageUSD, 1e-9)

	revoked, err := subRepo.GetByIDIncludeDeleted(ctx, source.ID)
	require.NoError(t, err)
	require.NotNil(t, revoked.DeletedAt)
	require.Contains(t, revoked.Notes, fmt.Sprintf("Upgraded to subscription %d", target.ID))

	migratedKey, err := apiKeyRepo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, migratedKey.GroupID)
	require.Equal(t, targetGroup.ID, *migratedKey.GroupID)
}
