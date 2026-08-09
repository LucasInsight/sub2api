import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import SubscriptionQuotaResetControls from '../SubscriptionQuotaResetControls.vue'

const { getStatus, updateAutomation, resetAll, clearFalsePositive, showError, showSuccess } = vi.hoisted(() => ({
  getStatus: vi.fn(),
  updateAutomation: vi.fn(),
  resetAll: vi.fn(),
  clearFalsePositive: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin/subscriptionQuotaReset', () => ({
  getResetAllQuotaStatus: getStatus,
  updateQuotaResetAutomation: updateAutomation,
  resetAllQuota: resetAll,
  clearFalsePositiveQuotaResetPending: clearFalsePositive,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const status = (overrides: Record<string, unknown> = {}) => ({
  enabled: true,
  auto_reset_enabled: false,
  pending_event_count: 0,
  active_subscription_count: 3,
  eligible_account_count: 2,
  confirmation_count: 0,
  required_confirmation_count: 2,
  automatic_reset_ready: false,
  pending_events: [],
  ...overrides,
})

const ConfirmDialogStub = {
  props: ['show', 'confirmDisabled', 'title'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-testid="reset-all-quota-dialog">
      <slot />
      <button
        :data-testid="title === 'admin.subscriptions.clearFalsePositiveTitle' ? 'clear-false-positive-submit' : 'reset-all-quota-submit'"
        :disabled="confirmDisabled"
        @click="$emit('confirm')"
      >confirm</button>
    </div>
  `,
}

const ToggleStub = {
  props: ['modelValue', 'disabled'],
  emits: ['update:modelValue'],
  template: `
    <button
      type="button"
      :disabled="disabled"
      :aria-checked="modelValue"
      @click="$emit('update:modelValue', !modelValue)"
    >toggle</button>
  `,
}

const SelectStub = {
  props: ['modelValue', 'options', 'disabled'],
  emits: ['update:modelValue'],
  template: `
    <select
      :value="modelValue"
      :disabled="disabled"
      @change="$emit('update:modelValue', Number($event.target.value))"
    >
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `,
}

const mountControls = () => mount(SubscriptionQuotaResetControls, {
  global: {
    stubs: {
      ConfirmDialog: ConfirmDialogStub,
      Select: SelectStub,
      Toggle: ToggleStub,
      Icon: true,
    },
  },
})

describe('SubscriptionQuotaResetControls', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getStatus.mockResolvedValue(status())
    updateAutomation.mockResolvedValue(status({ auto_reset_enabled: true }))
    resetAll.mockResolvedValue({ reset_count: 3, consumed_event_count: 2, confirmation_count: 0 })
    clearFalsePositive.mockResolvedValue({ cleared: true })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses a solid amber button when the official reset is confirmed', async () => {
    getStatus.mockResolvedValue(status({
      confirmation_count: 2,
      automatic_reset_ready: true,
    }))
    const wrapper = mountControls()
    await flushPromises()

    expect(wrapper.get('[data-testid="reset-all-quota-button"]').classes()).toContain('bg-amber-500')
    wrapper.unmount()
  })

  it('updates the automation master switch', async () => {
    const wrapper = mountControls()
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-automation-toggle"]').trigger('click')
    await flushPromises()

    expect(updateAutomation).toHaveBeenCalledWith(true)
    expect(wrapper.get('[data-testid="quota-reset-automation-toggle"]').attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })

  it('requires acknowledgement and restores the normal style after a manual reset', async () => {
    getStatus
      .mockResolvedValueOnce(status({ confirmation_count: 2, automatic_reset_ready: true }))
      .mockResolvedValueOnce(status())
    const wrapper = mountControls()
    await flushPromises()

    await wrapper.get('[data-testid="reset-all-quota-button"]').trigger('click')
    expect(wrapper.get('[data-testid="reset-all-quota-submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="reset-all-quota-acknowledgement"]').setValue(true)
    await wrapper.get('[data-testid="reset-all-quota-submit"]').trigger('click')
    await flushPromises()

    expect(resetAll).toHaveBeenCalledWith(expect.any(String), true)
    expect(wrapper.get('[data-testid="reset-all-quota-button"]').classes()).not.toContain('bg-amber-500')
    expect(wrapper.emitted('reset-completed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('polls status every minute while the page is visible and stops after unmount', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    const wrapper = mountControls()
    await flushPromises()
    expect(getStatus).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(60_000)
    await flushPromises()
    expect(getStatus).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(getStatus).toHaveBeenCalledTimes(2)
  })

  it('clears a selected false-positive event without resetting subscription quotas', async () => {
    const pendingEvent = {
      account_id: 17,
      account_name: 'primary',
      detected_at: '2026-08-09T07:29:36Z',
    }
    getStatus
      .mockResolvedValueOnce(status({ pending_event_count: 1, pending_events: [pendingEvent] }))
      .mockResolvedValueOnce(status())
    const wrapper = mountControls()
    await flushPromises()

    await wrapper.get('[data-testid="clear-false-positive-button"]').trigger('click')
    await wrapper.get('[data-testid="clear-false-positive-submit"]').trigger('click')
    await flushPromises()

    expect(clearFalsePositive).toHaveBeenCalledWith(17, '2026-08-09T07:29:36Z')
    expect(resetAll).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="clear-false-positive-button"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('clears only the event selected from multiple pending accounts', async () => {
    const pendingEvents = [
      { account_id: 17, account_name: 'primary', detected_at: '2026-08-09T07:29:36Z' },
      { account_id: 23, account_name: 'secondary', detected_at: '2026-08-09T07:31:04Z' },
    ]
    getStatus
      .mockResolvedValueOnce(status({ pending_event_count: 2, pending_events: pendingEvents }))
      .mockResolvedValueOnce(status({ pending_event_count: 1, pending_events: [pendingEvents[0]] }))
    const wrapper = mountControls()
    await flushPromises()

    await wrapper.get('[data-testid="clear-false-positive-button"]').trigger('click')
    await wrapper.get('[data-testid="clear-false-positive-select"]').setValue('23')
    await wrapper.get('[data-testid="clear-false-positive-submit"]').trigger('click')
    await flushPromises()

    expect(clearFalsePositive).toHaveBeenCalledWith(23, '2026-08-09T07:31:04Z')
    expect(clearFalsePositive).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
