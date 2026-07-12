import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Group, UserSubscription } from '@/types'
import SubscriptionUpgradeDialog from '../SubscriptionUpgradeDialog.vue'

const { listUserSubscriptions, upgradeSubscription, showError, showSuccess } = vi.hoisted(() => ({
  listUserSubscriptions: vi.fn(),
  upgradeSubscription: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin/subscriptionUpgrade', () => ({
  default: { listUserSubscriptions, upgradeSubscription }
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

const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<button data-test="select" @click="$emit(\'update:modelValue\', options[0]?.value)">{{ modelValue }}</button>'
}

const sourceGroup = {
  id: 10,
  name: 'Starter',
  platform: 'anthropic',
  status: 'active',
  subscription_type: 'subscription'
} as Group

const source = {
  id: 100,
  user_id: 7,
  group_id: sourceGroup.id,
  status: 'active',
  starts_at: '2026-07-01T00:00:00Z',
  expires_at: '2026-08-01T00:00:00Z',
  five_hour_usage_usd: 1,
  daily_usage_usd: 2,
  weekly_usage_usd: 3,
  monthly_usage_usd: 4,
  five_hour_window_start: null,
  daily_window_start: null,
  weekly_window_start: null,
  monthly_window_start: null,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  group: sourceGroup
} as UserSubscription

const target = {
  id: 20,
  name: 'Pro',
  platform: 'anthropic',
  status: 'active',
  subscription_type: 'subscription'
} as Group

const crossPlatform = {
  id: 30,
  name: 'OpenAI Pro',
  platform: 'openai',
  status: 'active',
  subscription_type: 'subscription'
} as Group

const existingTarget = {
  ...source,
  id: 101,
  group_id: 40,
  group: { ...target, id: 40, name: 'Already owned' }
} as UserSubscription

describe('SubscriptionUpgradeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listUserSubscriptions.mockResolvedValue([source, existingTarget])
    upgradeSubscription.mockResolvedValue({
      source_subscription_id: source.id,
      subscription: { ...source, id: 102, group_id: target.id },
      migrated_api_key_count: 1
    })
  })

  it('filters targets, auto-selects the sole candidate, and submits an idempotent upgrade', async () => {
    const wrapper = mount(SubscriptionUpgradeDialog, {
      props: {
        show: true,
        subscription: source,
        groups: [sourceGroup, target, crossPlatform, existingTarget.group!]
      },
      global: {
        stubs: { BaseDialog: BaseDialogStub, Select: SelectStub }
      }
    })

    await flushPromises()

    const select = wrapper.findComponent(SelectStub)
    expect(select.props('options')).toEqual([{ value: target.id, label: target.name }])
    expect(select.props('modelValue')).toBe(target.id)

    const submit = wrapper.findAll('button').find(button => button.text() === 'admin.subscriptions.upgrade')
    expect(submit).toBeDefined()
    await submit!.trigger('click')
    await flushPromises()

    expect(upgradeSubscription).toHaveBeenCalledWith(
      source.id,
      { target_group_id: target.id },
      expect.any(String)
    )
    expect(showSuccess).toHaveBeenCalledWith('admin.subscriptions.subscriptionUpgraded')
    expect(wrapper.emitted('upgraded')).toHaveLength(1)
  })
})
