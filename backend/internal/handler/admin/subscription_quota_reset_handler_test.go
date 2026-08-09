package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaResetPendingTrackerStub struct {
	clearResult      bool
	clearedAccountID int64
	clearedAt        time.Time
}

func (r *quotaResetPendingTrackerStub) ObserveOpenAI7dReset(context.Context, int64, time.Time, time.Time, time.Duration) (service.OpenAIOfficial7dResetObservation, error) {
	return service.OpenAIOfficial7dResetObservation{}, nil
}

func (r *quotaResetPendingTrackerStub) ListPendingOpenAIOfficial7dResets(context.Context) ([]service.OpenAIOfficial7dResetState, error) {
	return nil, nil
}

func (r *quotaResetPendingTrackerStub) ListEligibleOpenAIOfficial7dResetCandidates(context.Context, time.Time) ([]service.OpenAIOfficial7dResetCandidate, error) {
	return nil, nil
}

func (r *quotaResetPendingTrackerStub) MarkAllOpenAIOfficial7dResetsHandled(context.Context, time.Time) error {
	return nil
}

func (r *quotaResetPendingTrackerStub) ClearOpenAIOfficial7dResetPending(_ context.Context, accountID int64, detectedAt time.Time) (bool, error) {
	r.clearedAccountID = accountID
	r.clearedAt = detectedAt
	return r.clearResult, nil
}

func newQuotaResetPendingTestRouter(tracker *quotaResetPendingTrackerStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewSubscriptionHandler(nil)
	handler.SetQuotaResetService(service.NewSubscriptionQuotaResetService(nil, tracker, nil))
	router := gin.New()
	router.POST("/pending-events/clear", handler.ClearFalsePositiveQuotaResetPending)
	return router
}

func TestClearFalsePositiveQuotaResetPending_ClearsExactEvent(t *testing.T) {
	tracker := &quotaResetPendingTrackerStub{clearResult: true}
	router := newQuotaResetPendingTestRouter(tracker)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/pending-events/clear",
		bytes.NewBufferString(`{"account_id":17,"detected_at":"2026-08-09T07:29:36Z"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(17), tracker.clearedAccountID)
	require.Equal(t, time.Date(2026, 8, 9, 7, 29, 36, 0, time.UTC), tracker.clearedAt)
}

func TestClearFalsePositiveQuotaResetPending_RejectsStaleEvent(t *testing.T) {
	tracker := &quotaResetPendingTrackerStub{clearResult: false}
	router := newQuotaResetPendingTestRouter(tracker)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/pending-events/clear",
		bytes.NewBufferString(`{"account_id":17,"detected_at":"2026-08-09T07:29:36Z"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
}
