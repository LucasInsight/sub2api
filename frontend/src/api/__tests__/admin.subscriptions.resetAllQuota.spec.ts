import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post },
}))

import { getResetAllQuotaStatus, resetAllQuota } from '@/api/admin/subscriptions'

describe('admin subscription reset-all quota API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('loads global reset eligibility', async () => {
    get.mockResolvedValue({ data: { enabled: false, pending_event_count: 0, active_subscription_count: 3 } })

    await getResetAllQuotaStatus()

    expect(get).toHaveBeenCalledWith('/admin/subscriptions/reset-all-quota/status')
  })

  it('sends the stable idempotency key with the reset request', async () => {
    post.mockResolvedValue({ data: { reset_count: 3, consumed_event_count: 1 } })

    await resetAllQuota('subscription-reset-all-test-key')

    expect(post).toHaveBeenCalledWith(
      '/admin/subscriptions/reset-all-quota',
      {},
      { headers: { 'Idempotency-Key': 'subscription-reset-all-test-key' } },
    )
  })
})
