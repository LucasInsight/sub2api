package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpgradedSubscriptionFromSourceCopiesTermWindowsAndUsage(t *testing.T) {
	startsAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	expiresAt := startsAt.Add(48 * time.Hour)
	fiveHourStart := startsAt.Add(2 * time.Hour)
	dailyStart := startsAt
	weeklyStart := startsAt.Add(-24 * time.Hour)
	monthlyStart := startsAt.Add(-7 * 24 * time.Hour)
	assignedAt := startsAt.Add(12 * time.Hour)
	source := &UserSubscription{
		ID:                  11,
		UserID:              22,
		GroupID:             33,
		StartsAt:            startsAt,
		ExpiresAt:           expiresAt,
		FiveHourWindowStart: &fiveHourStart,
		DailyWindowStart:    &dailyStart,
		WeeklyWindowStart:   &weeklyStart,
		MonthlyWindowStart:  &monthlyStart,
		FiveHourUsageUSD:    1.25,
		DailyUsageUSD:       2.5,
		WeeklyUsageUSD:      3.75,
		MonthlyUsageUSD:     4.5,
		Notes:               "source note",
	}

	target := upgradedSubscriptionFromSource(source, &Group{ID: 44}, 55, assignedAt)

	require.Equal(t, source.UserID, target.UserID)
	require.Equal(t, int64(44), target.GroupID)
	require.Equal(t, source.StartsAt, target.StartsAt)
	require.Equal(t, source.ExpiresAt, target.ExpiresAt)
	require.Equal(t, source.FiveHourWindowStart, target.FiveHourWindowStart)
	require.Equal(t, source.DailyWindowStart, target.DailyWindowStart)
	require.Equal(t, source.WeeklyWindowStart, target.WeeklyWindowStart)
	require.Equal(t, source.MonthlyWindowStart, target.MonthlyWindowStart)
	require.Equal(t, source.FiveHourUsageUSD, target.FiveHourUsageUSD)
	require.Equal(t, source.DailyUsageUSD, target.DailyUsageUSD)
	require.Equal(t, source.WeeklyUsageUSD, target.WeeklyUsageUSD)
	require.Equal(t, source.MonthlyUsageUSD, target.MonthlyUsageUSD)
	require.Equal(t, assignedAt, target.AssignedAt)
	require.NotNil(t, target.AssignedBy)
	require.Equal(t, int64(55), *target.AssignedBy)
	require.Contains(t, target.Notes, "Upgraded from subscription 11 (group 33)")
}
