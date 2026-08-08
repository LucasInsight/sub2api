package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
)

const SettingKeyOpenAIOfficialQuotaAutoResetEnabled = "openai_official_quota_auto_reset_enabled"

type OpenAIQuotaResetAutomationSettings interface {
	GetOpenAIOfficialQuotaAutoResetEnabled(ctx context.Context) (bool, error)
	SetOpenAIOfficialQuotaAutoResetEnabled(ctx context.Context, enabled bool) error
}

func (s *SettingService) GetOpenAIOfficialQuotaAutoResetEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.settingRepo == nil {
		return false, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIOfficialQuotaAutoResetEnabled)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return false, nil
		}
		return false, err
	}
	enabled, parseErr := strconv.ParseBool(strings.TrimSpace(value))
	if parseErr != nil {
		slog.Warn("invalid OpenAI official quota auto-reset setting; using disabled default",
			"key", SettingKeyOpenAIOfficialQuotaAutoResetEnabled,
			"value", value,
		)
		return false, nil
	}
	return enabled, nil
}

func (s *SettingService) SetOpenAIOfficialQuotaAutoResetEnabled(ctx context.Context, enabled bool) error {
	if s == nil || s.settingRepo == nil {
		return ErrResetAllQuotaUnavailable
	}
	return s.settingRepo.Set(ctx, SettingKeyOpenAIOfficialQuotaAutoResetEnabled, strconv.FormatBool(enabled))
}
