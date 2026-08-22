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

	t.Run("natural rollover leaves pending event to round reconciliation", func(t *testing.T) {
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
		require.False(t, clearPending)
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
		SetExtra(map[string]any{openAI7dResetPendingEvidenceExtraKey: map[string]any{"version": 1}}).
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
	require.NotContains(t, updated.Extra, openAI7dResetPendingEvidenceExtraKey)
}

func createOpenAIOfficialResetTestAccount(
	t *testing.T,
	client *dbent.Client,
	name, planType string,
	subscriptionExpiresAt *time.Time,
) *dbent.Account {
	t.Helper()
	credentials := map[string]any{"plan_type": planType}
	if subscriptionExpiresAt != nil {
		credentials["subscription_expires_at"] = subscriptionExpiresAt.UTC().Format(time.RFC3339)
	}
	account, err := client.Account.Create().
		SetName(name).
		SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeOAuth).
		SetStatus(service.StatusActive).
		SetCredentials(credentials).
		Save(context.Background())
	require.NoError(t, err)
	return account
}

func TestObserveOpenAI7dReset_RequiresStablePaidSevenDayEvidence(t *testing.T) {
	repo, client := newOpenAIResetRepoSQLite(t)
	ctx := context.Background()
	startedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	expiresAt := startedAt.Add(60 * 24 * time.Hour)
	first := createOpenAIOfficialResetTestAccount(t, client, "first", "pro", &expiresAt)
	second := createOpenAIOfficialResetTestAccount(t, client, "second", "plus", &expiresAt)

	observation, err := repo.ObserveOpenAI7dReset(
		ctx, first.ID, startedAt, startedAt.Add(30*24*time.Hour), 30*24*time.Hour, time.Minute,
	)
	require.NoError(t, err)
	require.False(t, observation.Detected, "a subscription-length window must only rebuild the baseline")

	sevenDayBaselineAt := startedAt.Add(time.Minute)
	observation, err = repo.ObserveOpenAI7dReset(
		ctx, first.ID, sevenDayBaselineAt, sevenDayBaselineAt.Add(7*24*time.Hour), 7*24*time.Hour, time.Minute,
	)
	require.NoError(t, err)
	require.False(t, observation.Detected, "transitioning from a 30-day window to a 7-day window is not an official reset")

	detectedAt := sevenDayBaselineAt.Add(3 * time.Hour)
	observation, err = repo.ObserveOpenAI7dReset(
		ctx, first.ID, detectedAt, detectedAt.Add(7*24*time.Hour), 7*24*time.Hour, time.Minute,
	)
	require.NoError(t, err)
	require.True(t, observation.Detected)

	pending, err := repo.ListPendingOpenAIOfficial7dResets(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.NotNil(t, pending[0].Evidence)
	require.Equal(t, []int64{first.ID, second.ID}, pending[0].Evidence.EligibleAccountIDs)
	require.Equal(t, int64((7*24*time.Hour)/time.Second), pending[0].Evidence.WindowSeconds)
}

func TestObserveOpenAI7dReset_SubscriptionLifecycleChangesRebaseline(t *testing.T) {
	t.Run("renewal changes expiry", func(t *testing.T) {
		repo, client := newOpenAIResetRepoSQLite(t)
		ctx := context.Background()
		startedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
		firstExpiry := startedAt.Add(30 * 24 * time.Hour)
		account := createOpenAIOfficialResetTestAccount(t, client, "renewed", "pro", &firstExpiry)

		_, err := repo.ObserveOpenAI7dReset(ctx, account.ID, startedAt, startedAt.Add(7*24*time.Hour), 7*24*time.Hour, time.Minute)
		require.NoError(t, err)
		secondExpiry := firstExpiry.Add(30 * 24 * time.Hour)
		_, err = client.Account.UpdateOneID(account.ID).SetCredentials(map[string]any{
			"plan_type":               "pro",
			"subscription_expires_at": secondExpiry.Format(time.RFC3339),
		}).Save(ctx)
		require.NoError(t, err)

		renewalObservedAt := startedAt.Add(3 * time.Hour)
		observation, err := repo.ObserveOpenAI7dReset(ctx, account.ID, renewalObservedAt, renewalObservedAt.Add(7*24*time.Hour), 7*24*time.Hour, time.Minute)
		require.NoError(t, err)
		require.False(t, observation.Detected)

		stableObservedAt := renewalObservedAt.Add(3 * time.Hour)
		observation, err = repo.ObserveOpenAI7dReset(ctx, account.ID, stableObservedAt, stableObservedAt.Add(7*24*time.Hour), 7*24*time.Hour, time.Minute)
		require.NoError(t, err)
		require.True(t, observation.Detected)
	})

	t.Run("new paid subscription", func(t *testing.T) {
		repo, client := newOpenAIResetRepoSQLite(t)
		ctx := context.Background()
		startedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
		account := createOpenAIOfficialResetTestAccount(t, client, "new-subscription", "free", nil)
		_, err := repo.ObserveOpenAI7dReset(ctx, account.ID, startedAt, startedAt.Add(30*24*time.Hour), 30*24*time.Hour, time.Minute)
		require.NoError(t, err)

		expiresAt := startedAt.Add(30 * 24 * time.Hour)
		_, err = client.Account.UpdateOneID(account.ID).SetCredentials(map[string]any{
			"plan_type":               "pro",
			"subscription_expires_at": expiresAt.Format(time.RFC3339),
		}).Save(ctx)
		require.NoError(t, err)
		paidObservedAt := startedAt.Add(time.Hour)
		observation, err := repo.ObserveOpenAI7dReset(ctx, account.ID, paidObservedAt, paidObservedAt.Add(7*24*time.Hour), 7*24*time.Hour, time.Minute)
		require.NoError(t, err)
		require.False(t, observation.Detected, "the first paid observation establishes a new subscription baseline")
	})
}

func TestListEligibleOpenAIOfficial7dResetCandidates_FiltersUpstreamSubscriptionState(t *testing.T) {
	repo, client := newOpenAIResetRepoSQLite(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)
	active := createOpenAIOfficialResetTestAccount(t, client, "active", "pro", &future)
	createOpenAIOfficialResetTestAccount(t, client, "expired", "pro", &past)
	createOpenAIOfficialResetTestAccount(t, client, "free", "free", nil)

	candidates, err := repo.ListEligibleOpenAIOfficial7dResetCandidates(ctx, now)

	require.NoError(t, err)
	require.Equal(t, []service.OpenAIOfficial7dResetCandidate{{AccountID: active.ID}}, candidates)
}

func TestMarkOpenAIOfficial7dResetRoundHandled_ConsumesOnlyRoundEvents(t *testing.T) {
	repo, client := newOpenAIResetRepoSQLite(t)
	ctx := context.Background()
	detectedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	accounts := make([]*dbent.Account, 0, 3)
	for i := 1; i <= 3; i++ {
		account, err := client.Account.Create().
			SetName(fmt.Sprintf("account-%d", i)).
			SetPlatform(service.PlatformOpenAI).
			SetType(service.AccountTypeOAuth).
			SetStatus(service.StatusActive).
			SetCodexOfficialEarlyResetPending(true).
			SetCodexOfficialEarlyResetDetectedAt(detectedAt.Add(time.Duration(i) * time.Minute)).
			SetExtra(map[string]any{openAI7dResetPendingEvidenceExtraKey: map[string]any{"version": 1}}).
			Save(ctx)
		require.NoError(t, err)
		accounts = append(accounts, account)
	}
	events := []service.OpenAIOfficial7dResetState{
		{AccountID: accounts[0].ID, DetectedAt: detectedAt.Add(time.Minute)},
		{AccountID: accounts[1].ID, DetectedAt: detectedAt.Add(2 * time.Minute)},
	}
	handledAt := detectedAt.Add(time.Hour)

	consumed, err := repo.MarkOpenAIOfficial7dResetRoundHandled(ctx, handledAt, events)

	require.NoError(t, err)
	require.Equal(t, 2, consumed)
	for i, account := range accounts {
		updated, getErr := client.Account.Get(ctx, account.ID)
		require.NoError(t, getErr)
		require.NotNil(t, updated.CodexOfficialEarlyResetHandledAt)
		require.Equal(t, handledAt, *updated.CodexOfficialEarlyResetHandledAt)
		if i < 2 {
			require.False(t, updated.CodexOfficialEarlyResetPending)
			require.Nil(t, updated.CodexOfficialEarlyResetDetectedAt)
			require.NotContains(t, updated.Extra, openAI7dResetPendingEvidenceExtraKey)
		} else {
			require.True(t, updated.CodexOfficialEarlyResetPending)
			require.Contains(t, updated.Extra, openAI7dResetPendingEvidenceExtraKey)
		}
	}
}
