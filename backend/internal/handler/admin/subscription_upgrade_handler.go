package admin

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UpgradeSubscriptionRequest struct {
	TargetGroupID int64 `json:"target_group_id" binding:"required,gt=0"`
}

type UpgradeSubscriptionResponse struct {
	SourceSubscriptionID int64                      `json:"source_subscription_id"`
	Subscription         *dto.AdminUserSubscription `json:"subscription"`
	MigratedAPIKeyCount  int64                      `json:"migrated_api_key_count"`
}

// Upgrade replaces an active subscription with a same-platform subscription group.
// POST /api/v1/admin/subscriptions/:id/upgrade
func (h *SubscriptionHandler) Upgrade(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || subscriptionID <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}

	var req UpgradeSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	payload := struct {
		SubscriptionID int64                      `json:"subscription_id"`
		Body           UpgradeSubscriptionRequest `json:"body"`
	}{SubscriptionID: subscriptionID, Body: req}

	executeAdminIdempotentJSON(c, "admin.subscriptions.upgrade", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		result, execErr := h.subscriptionService.UpgradeSubscription(ctx, service.UpgradeSubscriptionInput{
			SubscriptionID: subscriptionID,
			TargetGroupID:  req.TargetGroupID,
			AssignedBy:     getAdminIDFromContext(c),
		})
		if execErr != nil {
			return nil, execErr
		}
		return &UpgradeSubscriptionResponse{
			SourceSubscriptionID: result.SourceSubscriptionID,
			Subscription:         dto.UserSubscriptionFromServiceAdmin(result.Subscription),
			MigratedAPIKeyCount:  result.MigratedAPIKeyCount,
		}, nil
	})
}
