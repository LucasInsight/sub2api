//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyOpenAI7dResetObservation(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	previous := now.Add(24 * time.Hour)
	changedReset := now.Add(7 * 24 * time.Hour)

	t.Run("first observation only establishes baseline", func(t *testing.T) {
		changed, detected := classifyOpenAI7dResetObservation(nil, now, changedReset, time.Minute)
		require.False(t, changed)
		require.False(t, detected)
	})

	t.Run("any early change is detected for administrator review", func(t *testing.T) {
		changed, detected := classifyOpenAI7dResetObservation(&previous, now, changedReset, time.Minute)
		require.True(t, changed)
		require.True(t, detected)
	})

	t.Run("natural rollover is not early", func(t *testing.T) {
		observedAfterBoundary := previous.Add(time.Second)
		changed, detected := classifyOpenAI7dResetObservation(&previous, observedAfterBoundary, changedReset, time.Minute)
		require.True(t, changed)
		require.False(t, detected)
	})
}
