//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newOpenAIResetRepoSQLite(t *testing.T) (*accountRepository, *dbent.Client) {
	t.Helper()

	dsn := fmt.Sprintf("file:openai_reset_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return &accountRepository{client: client, sql: db}, client
}

func TestClassifyOpenAI7dResetObservation(t *testing.T) {
	oldReset := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	newReset := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	observedAt := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	previousObservedAt := observedAt.Add(-time.Hour)
	handledAt := observedAt.Add(-2 * time.Hour)

	t.Run("first observation only establishes baseline", func(t *testing.T) {
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			nil,
			nil,
			nil,
			false,
			observedAt,
			newReset,
			time.Minute,
		)
		require.False(t, changed)
		require.False(t, detected)
		require.False(t, clearPending)
	})

	t.Run("early change after the latest handled reset is detected", func(t *testing.T) {
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			&handledAt,
			false,
			observedAt,
			newReset,
			time.Minute,
		)
		require.True(t, changed)
		require.True(t, detected)
		require.False(t, clearPending)
	})

	t.Run("sub-two-hour reset-time drift is not detected", func(t *testing.T) {
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			nil,
			false,
			observedAt,
			oldReset.Add(2*time.Hour-time.Second),
			time.Minute,
		)
		require.True(t, changed)
		require.False(t, detected)
		require.False(t, clearPending)
	})

	t.Run("two-hour reset-time change is detected", func(t *testing.T) {
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			nil,
			false,
			observedAt,
			oldReset.Add(2*time.Hour),
			time.Minute,
		)
		require.True(t, changed)
		require.True(t, detected)
		require.False(t, clearPending)
	})

	t.Run("pending event suppresses repeated significant changes", func(t *testing.T) {
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			nil,
			true,
			observedAt,
			newReset,
			time.Minute,
		)
		require.True(t, changed)
		require.False(t, detected)
		require.False(t, clearPending)
	})

	t.Run("pending event survives short reset-time drift", func(t *testing.T) {
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			nil,
			true,
			observedAt,
			oldReset.Add(time.Minute),
			time.Minute,
		)
		require.True(t, changed)
		require.False(t, detected)
		require.False(t, clearPending)
	})

	t.Run("handled event suppresses all changes during cooldown", func(t *testing.T) {
		recentHandledAt := observedAt.Add(-time.Hour)
		freshObservation := recentHandledAt.Add(time.Minute)
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&freshObservation,
			&recentHandledAt,
			false,
			observedAt,
			newReset,
			time.Minute,
		)
		require.True(t, changed)
		require.False(t, detected)
		require.False(t, clearPending)
	})

	t.Run("late observation covered by latest global reset", func(t *testing.T) {
		latePreviousObservation := handledAt.Add(-time.Minute)
		_, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&latePreviousObservation,
			&handledAt,
			false,
			observedAt,
			newReset,
			time.Minute,
		)
		require.False(t, detected)
		require.False(t, clearPending)
	})

	t.Run("natural rollover clears stale pending event", func(t *testing.T) {
		naturalObservation := oldReset.Add(time.Minute)
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			nil,
			true,
			naturalObservation,
			newReset,
			time.Minute,
		)
		require.True(t, changed)
		require.False(t, detected)
		require.True(t, clearPending)
	})
}

func TestOpenAI7dResetObservationIsStale(t *testing.T) {
	previous := time.Date(2026, 8, 3, 10, 0, 1, 0, time.UTC)

	require.True(t, openAI7dResetObservationIsStale(&previous, previous.Add(-time.Second)))
	require.True(t, openAI7dResetObservationIsStale(&previous, previous))
	require.False(t, openAI7dResetObservationIsStale(&previous, previous.Add(time.Second)))
	require.False(t, openAI7dResetObservationIsStale(nil, previous))
}

func TestClassifyOpenAI7dResetObservation_DoesNotCombineShortDriftAcrossAccounts(t *testing.T) {
	observedAt := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	previousObservedAt := observedAt.Add(-time.Minute)
	accountResetTimes := []time.Time{
		time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
	}

	for i := range accountResetTimes {
		previousResetAt := accountResetTimes[i]
		changed, detected, clearPending := classifyOpenAI7dResetObservation(
			&previousResetAt,
			&previousObservedAt,
			nil,
			false,
			observedAt,
			previousResetAt.Add(90*time.Minute),
			time.Minute,
		)
		require.True(t, changed)
		require.False(t, detected)
		require.False(t, clearPending)
	}
}

func TestOpenAI7dResetBaselineFromExtra(t *testing.T) {
	baseline := openAI7dResetBaselineFromExtra(map[string]any{
		"codex_7d_quota_estimate_period_key": "2026-08-08T10:00:00Z",
	})

	require.NotNil(t, baseline)
	require.Equal(t, time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC), *baseline)
}

func TestClearOpenAIOfficial7dResetPending_RequiresExactDetectedAt(t *testing.T) {
	repo, client := newOpenAIResetRepoSQLite(t)
	ctx := context.Background()
	detectedAt := time.Date(2026, 8, 9, 7, 29, 36, 0, time.UTC)
	account, err := client.Account.Create().
		SetName("primary").
		SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeOAuth).
		SetStatus(service.StatusActive).
		SetCodexOfficialEarlyResetPending(true).
		SetCodexOfficialEarlyResetDetectedAt(detectedAt).
		Save(ctx)
	require.NoError(t, err)

	cleared, err := repo.ClearOpenAIOfficial7dResetPending(ctx, account.ID, detectedAt.Add(time.Second))
	require.NoError(t, err)
	require.False(t, cleared)

	unchanged, err := client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, unchanged.CodexOfficialEarlyResetPending)
	require.NotNil(t, unchanged.CodexOfficialEarlyResetDetectedAt)

	cleared, err = repo.ClearOpenAIOfficial7dResetPending(ctx, account.ID, detectedAt)
	require.NoError(t, err)
	require.True(t, cleared)

	updated, err := client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, updated.CodexOfficialEarlyResetPending)
	require.Nil(t, updated.CodexOfficialEarlyResetDetectedAt)
}
