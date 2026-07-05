package service

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type accountUsageCodexProbeRepo struct {
	stubOpenAIAccountRepo
	updateExtraCh    chan map[string]any
	updateExtraCalls []map[string]any
	rateLimitCh      chan time.Time
}

func (r *accountUsageCodexProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	copied := make(map[string]any, len(updates))
	for k, v := range updates {
		copied[k] = v
	}
	r.updateExtraCalls = append(r.updateExtraCalls, copied)
	if r.updateExtraCh != nil {
		r.updateExtraCh <- copied
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	if r.rateLimitCh != nil {
		r.rateLimitCh <- resetAt
	}
	return nil
}

func TestShouldRefreshOpenAICodexSnapshot(t *testing.T) {
	t.Parallel()

	rateLimitedUntil := time.Now().Add(5 * time.Minute)
	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{RateLimitResetAt: &rateLimitedUntil}, usage, now) {
		t.Fatal("expected rate-limited account to force codex snapshot refresh")
	}

	if shouldRefreshOpenAICodexSnapshot(&Account{}, usage, now) {
		t.Fatal("expected complete non-rate-limited usage to skip codex snapshot refresh")
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{}, &UsageInfo{FiveHour: nil, SevenDay: &UsageProgress{}}, now) {
		t.Fatal("expected missing 5h snapshot to require refresh")
	}

	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	if !shouldRefreshOpenAICodexSnapshot(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"codex_usage_updated_at":                       staleAt,
		},
	}, usage, now) {
		t.Fatal("expected stale ws snapshot to trigger refresh")
	}
}

// TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2 外审第9轮 P1:spark 影子用量走
// QueryUsage(/wham/usage,与 WSv2 无关),staleness 不得被 WSv2 门控,否则首刷后窗口永久冻结。
func TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2(t *testing.T) {
	t.Parallel()

	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}
	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	freshAt := now.Add(-time.Minute).Format(time.RFC3339)
	parentID := int64(7001)

	// 影子无 WSv2,但首刷后窗口已存在;过期 codex_usage_updated_at 必须触发再刷新。
	shadowStale := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": staleAt},
	}
	if !shouldRefreshOpenAICodexSnapshot(shadowStale, usage, now) {
		t.Fatal("expected stale spark shadow (no WSv2) to trigger refresh")
	}

	// 影子时间戳仍新鲜→不刷(TTL 生效)。
	shadowFresh := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": freshAt},
	}
	if shouldRefreshOpenAICodexSnapshot(shadowFresh, usage, now) {
		t.Fatal("expected fresh spark shadow to skip refresh (TTL not elapsed)")
	}

	// 反向对照:普通账号无 WSv2 + 过期时间戳→仍不刷(WSv2 门控普通账号的 probe 刷新)。
	normalNoWS := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_usage_updated_at": staleAt},
	}
	if shouldRefreshOpenAICodexSnapshot(normalNoWS, usage, now) {
		t.Fatal("expected non-WSv2 normal account to skip codex probe refresh")
	}
}

func TestExtractOpenAICodexProbeUpdatesAccepts429WithCodexHeaders(t *testing.T) {
	t.Parallel()

	headers := make(http.Header)
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	updates, err := extractOpenAICodexProbeUpdates(&http.Response{StatusCode: http.StatusTooManyRequests, Header: headers})
	if err != nil {
		t.Fatalf("extractOpenAICodexProbeUpdates() error = %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected codex probe updates from 429 headers")
	}
	if got := updates["codex_5h_used_percent"]; got != 100.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 100", got)
	}
	if got := updates["codex_7d_used_percent"]; got != 100.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 100", got)
	}
}

func TestAccountUsageService_PersistOpenAICodexProbeSnapshotOnlyUpdatesExtra(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		updateExtraCh: make(chan map[string]any, 1),
		rateLimitCh:   make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	svc.persistOpenAICodexProbeSnapshot(321, map[string]any{
		"codex_7d_used_percent": 100.0,
		"codex_7d_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
	})

	select {
	case updates := <-repo.updateExtraCh:
		if got := updates["codex_7d_used_percent"]; got != 100.0 {
			t.Fatalf("codex_7d_used_percent = %v, want 100", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待 codex 探测快照写入 extra 超时")
	}

	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将探测快照写入运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAccountUsageService_GetOpenAIUsage_DoesNotPromoteCodexExtraToRateLimit(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		rateLimitCh: make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 1.0,
			"codex_5h_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     resetAt.Format(time.RFC3339),
		},
	}

	usage, err := svc.getOpenAIUsage(context.Background(), account, false)
	if err != nil {
		t.Fatalf("getOpenAIUsage() error = %v", err)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 100.0 {
		t.Fatalf("预期 7 天用量仍然可见，实际为 %#v", usage.SevenDay)
	}
	if account.RateLimitResetAt != nil {
		t.Fatalf("不应让已耗尽的 codex extra 改写运行时限流状态: %v", account.RateLimitResetAt)
	}
	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将已耗尽的 codex extra 持久化为运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBuildCodexUsageProgressFromExtra_ZerosExpiredWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)

	t.Run("expired 5h window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     "2026-03-16T10:00:00Z", // 2h ago
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired window, got %v", progress.Utilization)
		}
		if progress.RemainingSeconds != 0 {
			t.Fatalf("expected RemainingSeconds=0, got %v", progress.RemainingSeconds)
		}
	})

	t.Run("active 5h window keeps utilization", func(t *testing.T) {
		resetAt := now.Add(2 * time.Hour).Format(time.RFC3339)
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     resetAt,
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 42.0 {
			t.Fatalf("expected Utilization=42, got %v", progress.Utilization)
		}
	})

	t.Run("expired 7d window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_7d_used_percent": 88.0,
			"codex_7d_reset_at":     "2026-03-15T00:00:00Z", // yesterday
		}
		progress := buildCodexUsageProgressFromExtra(extra, "7d", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired 7d window, got %v", progress.Utilization)
		}
	})
}

func TestBuildCodexQuotaEstimateUpdates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	activeReset := now.Add(2 * time.Hour)

	t.Run("first valid sample initializes min max", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 25,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 2.5},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(nil, progress, "5h", now)
		if estimate == nil {
			t.Fatal("expected estimate")
		}
		if estimate.Min != 10 || estimate.Max != 10 {
			t.Fatalf("estimate = %#v, want min=max=10", estimate)
		}
		if estimate.CoverageFrom != 20 || estimate.CoverageTo != 30 {
			t.Fatalf("estimate coverage = %#v, want 20-30", estimate)
		}
		if updates["codex_5h_quota_estimate_min"] != 10.0 || updates["codex_5h_quota_estimate_max"] != 10.0 {
			t.Fatalf("unexpected updates: %#v", updates)
		}
		if updates["codex_5h_quota_estimate_coverage_from"] != 20.0 || updates["codex_5h_quota_estimate_coverage_to"] != 30.0 {
			t.Fatalf("unexpected coverage updates: %#v", updates)
		}
	})

	t.Run("same coverage lower sample updates min only", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 50,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 4},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(map[string]any{
			"codex_7d_quota_estimate_min":           10.0,
			"codex_7d_quota_estimate_max":           20.0,
			"codex_7d_quota_estimate_updated_at":    "2026-03-16T10:00:00Z",
			"codex_7d_quota_estimate_coverage_from": 50.0,
			"codex_7d_quota_estimate_coverage_to":   60.0,
		}, progress, "7d", now)

		if estimate.Min != 8 || estimate.Max != 20 {
			t.Fatalf("estimate = %#v, want min=8 max=20", estimate)
		}
		if estimate.CoverageFrom != 50 || estimate.CoverageTo != 60 {
			t.Fatalf("estimate coverage = %#v, want 50-60", estimate)
		}
		if updates["codex_7d_quota_estimate_min"] != 8.0 {
			t.Fatalf("expected min update, got %#v", updates)
		}
		if _, ok := updates["codex_7d_quota_estimate_max"]; ok {
			t.Fatalf("did not expect max update: %#v", updates)
		}
	})

	t.Run("inside range does not update", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 50,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 7.5},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(map[string]any{
			"codex_5h_quota_estimate_min":           10.0,
			"codex_5h_quota_estimate_max":           20.0,
			"codex_5h_quota_estimate_coverage_from": 50.0,
			"codex_5h_quota_estimate_coverage_to":   60.0,
		}, progress, "5h", now)

		if estimate.Min != 10 || estimate.Max != 20 {
			t.Fatalf("estimate = %#v, want existing range", estimate)
		}
		if len(updates) != 0 {
			t.Fatalf("expected no updates, got %#v", updates)
		}
	})

	t.Run("invalid samples are ignored", func(t *testing.T) {
		cases := []*UsageProgress{
			{Utilization: 0, ResetsAt: &activeReset, WindowStats: &WindowStats{Cost: 10}},
			{Utilization: 1, ResetsAt: &activeReset, WindowStats: &WindowStats{Cost: 10}},
			{Utilization: 101, ResetsAt: &activeReset, WindowStats: &WindowStats{Cost: 10}},
			{Utilization: 50, ResetsAt: &activeReset, WindowStats: &WindowStats{Cost: 0}},
			{Utilization: 5, ResetsAt: &activeReset, WindowStats: &WindowStats{Cost: 0.1, Requests: 2}},
			{Utilization: 50, ResetsAt: ptrQuotaEstimateTime(now.Add(-time.Minute)), WindowStats: &WindowStats{Cost: 10}},
		}

		for _, progress := range cases {
			estimate, updates := buildCodexQuotaEstimateUpdates(map[string]any{
				"codex_5h_quota_estimate_min":           10.0,
				"codex_5h_quota_estimate_max":           20.0,
				"codex_5h_quota_estimate_coverage_from": 50.0,
				"codex_5h_quota_estimate_coverage_to":   60.0,
			}, progress, "5h", now)
			if estimate == nil || estimate.Min != 10 || estimate.Max != 20 {
				t.Fatalf("expected existing estimate, got %#v", estimate)
			}
			if len(updates) != 0 {
				t.Fatalf("expected no updates, got %#v", updates)
			}
		}
	})

	t.Run("minimum coverage boundary initializes warmup range", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 5,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 0.25},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(nil, progress, "5h", now)
		if estimate == nil || estimate.Min != 5 || estimate.Max != 5 {
			t.Fatalf("estimate = %#v, want min=max=5", estimate)
		}
		if estimate.CoverageFrom != 5 || estimate.CoverageTo != 10 {
			t.Fatalf("estimate coverage = %#v, want 5-10", estimate)
		}
		if updates["codex_5h_quota_estimate_coverage_from"] != 5.0 || updates["codex_5h_quota_estimate_coverage_to"] != 10.0 {
			t.Fatalf("unexpected updates: %#v", updates)
		}
	})

	t.Run("higher coverage resets old range", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 25,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 3},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(map[string]any{
			"codex_5h_quota_estimate_min":           4.0,
			"codex_5h_quota_estimate_max":           40.0,
			"codex_5h_quota_estimate_coverage_from": 5.0,
			"codex_5h_quota_estimate_coverage_to":   10.0,
		}, progress, "5h", now)

		if estimate.Min != 12 || estimate.Max != 12 {
			t.Fatalf("estimate = %#v, want reset min=max=12", estimate)
		}
		if estimate.CoverageFrom != 20 || estimate.CoverageTo != 30 {
			t.Fatalf("estimate coverage = %#v, want 20-30", estimate)
		}
		if updates["codex_5h_quota_estimate_min"] != 12.0 || updates["codex_5h_quota_estimate_max"] != 12.0 {
			t.Fatalf("unexpected reset updates: %#v", updates)
		}
	})

	t.Run("lower coverage does not replace existing range", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 25,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 3},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(map[string]any{
			"codex_7d_quota_estimate_min":           15.0,
			"codex_7d_quota_estimate_max":           18.0,
			"codex_7d_quota_estimate_coverage_from": 50.0,
			"codex_7d_quota_estimate_coverage_to":   60.0,
		}, progress, "7d", now)

		if estimate.Min != 15 || estimate.Max != 18 {
			t.Fatalf("estimate = %#v, want existing range", estimate)
		}
		if len(updates) != 0 {
			t.Fatalf("expected no updates, got %#v", updates)
		}
	})

	t.Run("legacy estimate without coverage is rebuilt by next valid sample", func(t *testing.T) {
		progress := &UsageProgress{
			Utilization: 25,
			ResetsAt:    &activeReset,
			WindowStats: &WindowStats{Cost: 3},
		}

		estimate, updates := buildCodexQuotaEstimateUpdates(map[string]any{
			"codex_5h_quota_estimate_min": 10.0,
			"codex_5h_quota_estimate_max": 20.0,
		}, progress, "5h", now)

		if estimate.Min != 12 || estimate.Max != 12 {
			t.Fatalf("estimate = %#v, want rebuilt min=max=12", estimate)
		}
		if estimate.CoverageFrom != 20 || estimate.CoverageTo != 30 {
			t.Fatalf("estimate coverage = %#v, want 20-30", estimate)
		}
		if updates["codex_5h_quota_estimate_coverage_from"] != 20.0 || updates["codex_5h_quota_estimate_coverage_to"] != 30.0 {
			t.Fatalf("unexpected coverage updates: %#v", updates)
		}
	})
}

func TestAccountUsageServiceApplyCodexQuotaEstimateUpdatesExtra(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	repo := &accountUsageCodexProbeRepo{}
	svc := &AccountUsageService{accountRepo: repo}
	account := &Account{ID: 44, Extra: map[string]any{}}
	progress := &UsageProgress{
		Utilization: 20,
		ResetsAt:    &resetAt,
		WindowStats: &WindowStats{Cost: 1},
	}

	svc.applyCodexQuotaEstimate(context.Background(), account, progress, "5h", now)

	if progress.QuotaEstimate == nil || progress.QuotaEstimate.Min != 5 || progress.QuotaEstimate.Max != 5 {
		t.Fatalf("progress quota estimate = %#v, want min=max=5", progress.QuotaEstimate)
	}
	if account.Extra["codex_5h_quota_estimate_min"] != 5.0 {
		t.Fatalf("account extra not updated: %#v", account.Extra)
	}
	if account.Extra["codex_5h_quota_estimate_coverage_from"] != 20.0 || account.Extra["codex_5h_quota_estimate_coverage_to"] != 30.0 {
		t.Fatalf("account extra coverage not updated: %#v", account.Extra)
	}
	if len(repo.updateExtraCalls) != 1 {
		t.Fatalf("expected one UpdateExtra call, got %d", len(repo.updateExtraCalls))
	}
}

func ptrQuotaEstimateTime(t time.Time) *time.Time {
	return &t
}
