import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'

import SubscriptionsView from '../SubscriptionsView.vue'

const { listSubscriptions, getResetAllQuotaStatus, getAllGroups, showError, showSuccess } = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  getResetAllQuotaStatus: vi.fn(),
  getAllGroups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: {
      list: listSubscriptions,
      getResetAllQuotaStatus,
      resetAllQuota: vi.fn()
    },
    groups: { getAll: getAllGroups },
    usage: { searchUsers: vi.fn() }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const mountView = () => shallowMount(SubscriptionsView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: {
        template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
      },
      DataTable: true,
      Pagination: true,
      BaseDialog: true,
      ConfirmDialog: true,
      EmptyState: true,
      Select: true,
      GroupBadge: true,
      GroupOptionItem: true,
      Icon: true,
      SubscriptionUpgradeDialog: true
    }
  }
})

describe('admin SubscriptionsView reset-all quota gate', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listSubscriptions.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getAllGroups.mockResolvedValue([])
  })

  it('disables the action without a pending 7-day early reset', async () => {
    getResetAllQuotaStatus.mockResolvedValue({
      enabled: false,
      pending_event_count: 0,
      active_subscription_count: 3,
      disabled_reason: 'no_early_7d_reset'
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="reset-all-quota-button"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('enables the action when an early-reset event and active subscriptions exist', async () => {
    getResetAllQuotaStatus.mockResolvedValue({
      enabled: true,
      pending_event_count: 1,
      active_subscription_count: 3,
      latest_detected_at: '2026-07-18T10:00:00Z'
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="reset-all-quota-button"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
