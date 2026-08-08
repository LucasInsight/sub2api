//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyOpenAI7dResetObservation(t *testing.T) {
	oldReset := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	newReset := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	observedAt := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	previousObservedAt := observedAt.Add(-time.Hour)
	handledAt := observedAt.Add(-2 * time.Hour)

	t.Run("first observation only establishes baseline", func(t *testing.T) {
		changed, detected := classifyOpenAI7dResetObservation(
			nil,
			nil,
			nil,
			observedAt,
			newReset,
			time.Minute,
		)
		require.False(t, changed)
		require.False(t, detected)
	})

	t.Run("early change after the latest handled reset is detected", func(t *testing.T) {
		changed, detected := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			&handledAt,
			observedAt,
			newReset,
			time.Minute,
		)
		require.True(t, changed)
		require.True(t, detected)
	})

	t.Run("late observation covered by latest global reset", func(t *testing.T) {
		latePreviousObservation := handledAt.Add(-time.Minute)
		_, got := classifyOpenAI7dResetObservation(
			&oldReset,
			&latePreviousObservation,
			&handledAt,
			observedAt,
			newReset,
			time.Minute,
		)
		require.False(t, got)
	})

	t.Run("natural rollover is not early", func(t *testing.T) {
		naturalObservation := oldReset.Add(time.Minute)
		_, got := classifyOpenAI7dResetObservation(
			&oldReset,
			&previousObservedAt,
			nil,
			naturalObservation,
			newReset,
			time.Minute,
		)
		require.False(t, got)
	})
}

func TestOpenAI7dResetObservationIsStale(t *testing.T) {
	previous := time.Date(2026, 8, 3, 10, 0, 1, 0, time.UTC)

	require.True(t, openAI7dResetObservationIsStale(&previous, previous.Add(-time.Second)))
	require.True(t, openAI7dResetObservationIsStale(&previous, previous))
	require.False(t, openAI7dResetObservationIsStale(&previous, previous.Add(time.Second)))
	require.False(t, openAI7dResetObservationIsStale(nil, previous))
}

func TestOpenAI7dResetBaselineFromExtra(t *testing.T) {
	baseline := openAI7dResetBaselineFromExtra(map[string]any{
		"codex_7d_quota_estimate_period_key": "2026-08-08T10:00:00Z",
	})

	require.NotNil(t, baseline)
	require.Equal(t, time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC), *baseline)
}
