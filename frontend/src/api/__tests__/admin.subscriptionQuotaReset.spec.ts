import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, post },
}))

import {
  clearFalsePositiveQuotaResetPending,
  getResetAllQuotaStatus,
  resetAllQuota,
  updateQuotaResetAutomation,
} from '@/api/admin/subscriptionQuotaReset'

describe('admin subscription quota reset API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
  })

  it('loads quota reset status', async () => {
    get.mockResolvedValue({ data: { enabled: false, auto_reset_enabled: false } })

    await getResetAllQuotaStatus()

    expect(get).toHaveBeenCalledWith('/admin/subscriptions/reset-all-quota/status')
  })

  it('updates the automation master switch', async () => {
    put.mockResolvedValue({ data: { enabled: true, auto_reset_enabled: true } })

    await updateQuotaResetAutomation(true)

    expect(put).toHaveBeenCalledWith(
      '/admin/subscriptions/reset-all-quota/automation',
      { enabled: true },
    )
  })

  it('sends the stable idempotency key with the manual reset', async () => {
    post.mockResolvedValue({ data: { reset_count: 3, consumed_event_count: 1 } })

    await resetAllQuota('subscription-reset-all-test-key', true)

    expect(post).toHaveBeenCalledWith(
      '/admin/subscriptions/reset-all-quota',
      { acknowledged: true },
      { headers: { 'Idempotency-Key': 'subscription-reset-all-test-key' } },
    )
  })

  it('clears only the selected false-positive pending event', async () => {
    post.mockResolvedValue({ data: { cleared: true } })

    await clearFalsePositiveQuotaResetPending(17, '2026-08-09T07:29:36Z')

    expect(post).toHaveBeenCalledWith(
      '/admin/subscriptions/reset-all-quota/pending-events/clear',
      { account_id: 17, detected_at: '2026-08-09T07:29:36Z' },
    )
  })
})
