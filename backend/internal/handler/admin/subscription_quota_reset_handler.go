package admin

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type clearFalsePositiveQuotaResetPendingRequest struct {
	AccountID  int64     `json:"account_id"`
	DetectedAt time.Time `json:"detected_at"`
}

// ResetAllQuotaStatus reports the manual reset availability, automation switch,
// and official-reset confirmation state.
func (h *SubscriptionHandler) ResetAllQuotaStatus(c *gin.Context) {
	if h.quotaResetService == nil {
		response.ErrorFrom(c, service.ErrResetAllQuotaUnavailable)
		return
	}
	status, err := h.quotaResetService.Status(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// UpdateQuotaResetAutomation persists the master switch used by periodic
// detection and any future automatic reset execution path.
func (h *SubscriptionHandler) UpdateQuotaResetAutomation(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if h.quotaResetService == nil {
		response.ErrorFrom(c, service.ErrResetAllQuotaUnavailable)
		return
	}

	middleware2.SetAuditAction(c, "admin.subscriptions.quota_reset_automation.update")
	status, err := h.quotaResetService.Status(c.Request.Context())
	if err != nil {
		middleware2.SetAuditExtra(c, map[string]any{
			"enabled":    *req.Enabled,
			"result":     "failed",
			"error_code": infraerrors.Reason(err),
		})
		response.ErrorFrom(c, err)
		return
	}
	if err := h.quotaResetService.SetAutomationEnabled(c.Request.Context(), *req.Enabled); err != nil {
		middleware2.SetAuditExtra(c, map[string]any{
			"enabled":    *req.Enabled,
			"result":     "failed",
			"error_code": infraerrors.Reason(err),
		})
		response.ErrorFrom(c, err)
		return
	}
	status.AutoResetEnabled = *req.Enabled
	middleware2.SetAuditExtra(c, map[string]any{"enabled": *req.Enabled, "result": "success"})
	response.Success(c, status)
}

// ClearFalsePositiveQuotaResetPending dismisses one exact pending event
// without resetting subscription quotas or deleting its audit history.
func (h *SubscriptionHandler) ClearFalsePositiveQuotaResetPending(c *gin.Context) {
	var req clearFalsePositiveQuotaResetPendingRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.AccountID <= 0 || req.DetectedAt.IsZero() {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if h.quotaResetService == nil {
		response.ErrorFrom(c, service.ErrResetAllQuotaUnavailable)
		return
	}

	middleware2.SetAuditAction(c, "admin.subscriptions.quota.false_positive.clear")
	auditFields := map[string]any{
		"account_id":  req.AccountID,
		"detected_at": req.DetectedAt.UTC().Format(time.RFC3339),
		"result":      "success",
	}
	if err := h.quotaResetService.ClearFalsePositiveOpenAIResetPending(
		c.Request.Context(),
		req.AccountID,
		req.DetectedAt,
	); err != nil {
		auditFields["result"] = "failed"
		auditFields["error_code"] = infraerrors.Reason(err)
		middleware2.SetAuditExtra(c, auditFields)
		response.ErrorFrom(c, err)
		return
	}
	middleware2.SetAuditExtra(c, auditFields)
	response.Success(c, gin.H{"cleared": true})
}

// ResetAllQuota resets every active, unexpired user subscription and consumes
// pending OpenAI official-reset events atomically.
func (h *SubscriptionHandler) ResetAllQuota(c *gin.Context) {
	var req struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if h.quotaResetService == nil {
		response.ErrorFrom(c, service.ErrResetAllQuotaUnavailable)
		return
	}

	middleware2.SetAuditAction(c, "admin.subscriptions.quota.reset_all")
	executeAdminIdempotentJSON(
		c,
		"admin.subscriptions.reset_all_quota",
		req,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			result, err := h.quotaResetService.AdminResetAllQuota(ctx, req.Acknowledged)
			auditFields := map[string]any{
				"trigger_type": "manual_override",
				"result":       "success",
			}
			if result != nil {
				auditFields["reset_count"] = result.ResetCount
				auditFields["pending_event_count"] = result.ConsumedEventCount
				auditFields["confirmation_count"] = result.ConfirmationCount
			}
			if err != nil {
				auditFields["result"] = "failed"
				auditFields["error_code"] = infraerrors.Reason(err)
			}
			middleware2.SetAuditExtra(c, auditFields)
			return result, err
		},
	)
}
