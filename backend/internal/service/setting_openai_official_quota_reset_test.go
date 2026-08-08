//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIOfficialQuotaAutoResetSetting_DefaultsDisabled(t *testing.T) {
	repo := &panelRateLimitSettingRepo{values: map[string]string{}}
	service := NewSettingService(repo, nil)

	enabled, err := service.GetOpenAIOfficialQuotaAutoResetEnabled(context.Background())

	require.NoError(t, err)
	require.False(t, enabled)
}

func TestOpenAIOfficialQuotaAutoResetSetting_InvalidValueDefaultsDisabled(t *testing.T) {
	repo := &panelRateLimitSettingRepo{values: map[string]string{
		SettingKeyOpenAIOfficialQuotaAutoResetEnabled: "not-a-bool",
	}}
	service := NewSettingService(repo, nil)

	enabled, err := service.GetOpenAIOfficialQuotaAutoResetEnabled(context.Background())

	require.NoError(t, err)
	require.False(t, enabled)
}

func TestOpenAIOfficialQuotaAutoResetSetting_RoundTrip(t *testing.T) {
	repo := &panelRateLimitSettingRepo{values: map[string]string{}}
	service := NewSettingService(repo, nil)

	require.NoError(t, service.SetOpenAIOfficialQuotaAutoResetEnabled(context.Background(), true))
	enabled, err := service.GetOpenAIOfficialQuotaAutoResetEnabled(context.Background())

	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, "true", repo.values[SettingKeyOpenAIOfficialQuotaAutoResetEnabled])
}
