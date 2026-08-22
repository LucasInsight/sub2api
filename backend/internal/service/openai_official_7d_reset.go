package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const openAIOfficialEarlyResetDetectedAuditAction = "system.openai.quota.early_reset_detected"

type OpenAIOfficial7dResetObservationSource string

const (
	OpenAIOfficial7dResetSourceQuotaAPI      OpenAIOfficial7dResetObservationSource = "quota_api"
	OpenAIOfficial7dResetSourcePeriodicProbe OpenAIOfficial7dResetObservationSource = "periodic_probe"
	OpenAIOfficial7dResetSourceGatewayHeader OpenAIOfficial7dResetObservationSource = "gateway_header"
	OpenAIOfficial7dResetSourceAccountProbe  OpenAIOfficial7dResetObservationSource = "account_probe"
)

type OpenAIOfficial7dResetObserver struct {
	repository        OpenAIOfficial7dResetRepository
	auditService      *AuditLogService
	detectionNotifier openAIOfficial7dResetDetectionNotifier
}

func ProvideOpenAIOfficial7dResetObserver(
	accountRepo AccountRepository,
	auditService *AuditLogService,
) *OpenAIOfficial7dResetObserver {
	repository, _ := accountRepo.(OpenAIOfficial7dResetRepository)
	return NewOpenAIOfficial7dResetObserver(repository, auditService)
}

func NewOpenAIOfficial7dResetObserver(
	repository OpenAIOfficial7dResetRepository,
	auditService *AuditLogService,
) *OpenAIOfficial7dResetObserver {
	return &OpenAIOfficial7dResetObserver{repository: repository, auditService: auditService}
}

func observeOpenAIOfficial7dReset(
	ctx context.Context,
	observer *OpenAIOfficial7dResetObserver,
	accountID int64,
	observedAt, resetAt time.Time,
	windowDuration time.Duration,
	source OpenAIOfficial7dResetObservationSource,
) (bool, error) {
	if observer == nil || observer.repository == nil || accountID <= 0 || observedAt.IsZero() || resetAt.IsZero() || windowDuration <= 0 {
		return false, nil
	}
	observation, err := observer.repository.ObserveOpenAI7dReset(
		ctx,
		accountID,
		observedAt.UTC(),
		resetAt.UTC(),
		windowDuration,
		openaiOfficialResetGrace,
	)
	if err != nil {
		return false, err
	}
	if observation.Detected {
		observer.recordDetectionAudit(accountID, observation, source)
		observer.notifyDetection(source)
	}
	return observation.Detected, nil
}

func (o *OpenAIOfficial7dResetObserver) recordDetectionAudit(
	accountID int64,
	observation OpenAIOfficial7dResetObservation,
	source OpenAIOfficial7dResetObservationSource,
) {
	if o == nil || o.auditService == nil {
		return
	}
	extra := map[string]any{
		"account_id":         accountID,
		"new_reset_at":       observation.ResetAt.UTC().Format(time.RFC3339),
		"observed_at":        observation.ObservedAt.UTC().Format(time.RFC3339),
		"observation_source": string(source),
		"result":             "detected",
	}
	if observation.PreviousResetAt != nil {
		extra["previous_reset_at"] = observation.PreviousResetAt.UTC().Format(time.RFC3339)
	}
	o.auditService.Record(&AuditLog{
		CreatedAt:  observation.ObservedAt.UTC(),
		ActorEmail: "system",
		ActorRole:  "system",
		AuthMethod: "system",
		Action:     openAIOfficialEarlyResetDetectedAuditAction,
		Method:     "SYSTEM",
		Path:       "/internal/openai-official-quota-reset/observe",
		StatusCode: http.StatusOK,
		Extra:      extra,
	})
}

func openAIOfficial7dResetTimesFromExtraUpdates(
	updates map[string]any,
	fallbackObservedAt time.Time,
) (observedAt, resetAt time.Time, windowDuration time.Duration, ok bool) {
	if len(updates) == 0 {
		return time.Time{}, time.Time{}, 0, false
	}
	resetText, ok := updates["codex_7d_reset_at"].(string)
	if !ok || resetText == "" {
		return time.Time{}, time.Time{}, 0, false
	}
	resetAt, err := time.Parse(time.RFC3339, resetText)
	if err != nil || resetAt.IsZero() {
		return time.Time{}, time.Time{}, 0, false
	}
	windowMinutes, ok := openAIPositiveInteger(updates["codex_7d_window_minutes"])
	if !ok {
		return time.Time{}, time.Time{}, 0, false
	}
	windowDuration = time.Duration(windowMinutes) * time.Minute
	observedAt = fallbackObservedAt
	if observedText, ok := updates["codex_usage_updated_at"].(string); ok && observedText != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, observedText); parseErr == nil {
			observedAt = parsed
		}
	}
	if observedAt.IsZero() {
		return time.Time{}, time.Time{}, 0, false
	}
	return observedAt.UTC(), resetAt.UTC(), windowDuration, true
}

func openAIPositiveInteger(value any) (int64, bool) {
	var parsed int64
	switch typed := value.(type) {
	case int:
		parsed = int64(typed)
	case int64:
		parsed = typed
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		parsed = int64(typed)
	case json.Number:
		parsed, _ = typed.Int64()
	case string:
		parsed, _ = strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, false
	}
	return parsed, parsed > 0
}
