import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'

import SubscriptionsView from '../SubscriptionsView.vue'

const { listSubscriptions, getResetAllQuotaStatus, resetAllQuota, getAllGroups, showError, showSuccess } = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  getResetAllQuotaStatus: vi.fn(),
  resetAllQuota: vi.fn(),
  getAllGroups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: {
      list: listSubscriptions,
      getResetAllQuotaStatus,
      resetAllQuota
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

const ConfirmDialogStub = {
  props: ['show', 'confirmDisabled'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-testid="reset-all-quota-dialog">
      <slot />
      <button
        data-testid="reset-all-quota-submit"
        :disabled="confirmDisabled"
        @click="$emit('confirm')"
      >confirm</button>
    </div>
  `
}

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
      ConfirmDialog: ConfirmDialogStub,
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
    resetAllQuota.mockResolvedValue({ reset_count: 3, consumed_event_count: 0 })
  })

  it('enables the action with active subscriptions and no pending reset event', async () => {
    getResetAllQuotaStatus.mockResolvedValue({
      enabled: true,
      pending_event_count: 0,
      active_subscription_count: 3
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="reset-all-quota-button"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('requires acknowledgement before submitting and resets it when reopened', async () => {
    getResetAllQuotaStatus.mockResolvedValue({
      enabled: true,
      pending_event_count: 0,
      active_subscription_count: 3
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="reset-all-quota-button"]').trigger('click')
    const submit = wrapper.get('[data-testid="reset-all-quota-submit"]')
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="reset-all-quota-acknowledgement"]').setValue(true)
    expect(submit.attributes('disabled')).toBeUndefined()
    await submit.trigger('click')
    await flushPromises()

    expect(resetAllQuota).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-testid="reset-all-quota-button"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('[data-testid="reset-all-quota-acknowledgement"]').element.checked).toBe(false)
    expect(wrapper.get('[data-testid="reset-all-quota-submit"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
