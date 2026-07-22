import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import type { UserSubscription } from '@/types'
import SubscriptionsView from '../SubscriptionsView.vue'

const { getMySubscriptions, getOpenAIUsageMultiplier, showError, push } = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(),
  getOpenAIUsageMultiplier: vi.fn(),
  showError: vi.fn(),
  push: vi.fn(),
}))

const messages: Record<string, string> = {
  'userSubscriptions.openaiUsageTip.label': 'Dynamic multiplier',
  'userSubscriptions.openaiUsageTip.title': 'OpenAI dynamic multiplier details',
  'userSubscriptions.openaiUsageTip.ariaLabel': 'View OpenAI dynamic multiplier details',
  'userSubscriptions.openaiUsageTip.tierTitle': '{tier} quota',
  'userSubscriptions.openaiUsageTip.baselineQuota': 'Baseline quota: {quota}',
  'userSubscriptions.openaiUsageTip.telemetryQuota': 'Telemetry quota: {quota}',
  'userSubscriptions.openaiUsageTip.tierFormula': 'Dynamic multiplier: {baseline} / {telemetry} = {value}x',
  'userSubscriptions.openaiUsageTip.noTelemetryQuota': 'No telemetry quota available',
  'userSubscriptions.openaiUsageTip.currentMultiplier': 'Current dynamic multiplier: {value}x',
  'userSubscriptions.openaiUsageTip.updatedAt': 'Telemetry time: {time}',
  'userSubscriptions.openaiUsageTip.noMultiplier': 'No dynamic multiplier estimate available',
  'userSubscriptions.expires': 'Expires',
  'userSubscriptions.noExpiration': 'No expiration',
  'userSubscriptions.fiveHour': '5-Hour',
  'userSubscriptions.daily': 'Daily',
  'userSubscriptions.weekly': 'Weekly',
  'userSubscriptions.monthly': 'Monthly',
  'userSubscriptions.unlimited': 'Unlimited',
  'userSubscriptions.unlimitedDesc': 'No usage limits',
  'userSubscriptions.status.active': 'Active',
  'userSubscriptions.status.expired': 'Expired',
  'payment.planCard.rate': 'Rate',
  'payment.renewNow': 'Renew',
}

vi.mock('@/api/subscriptions', () => ({
  default: {
    getMySubscriptions,
    getOpenAIUsageMultiplier,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError,
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        let text = messages[key] ?? key
        for (const [name, value] of Object.entries(params ?? {})) {
          text = text.replace(`{${name}}`, String(value))
        }
        return text
      },
    }),
  }
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

function subscriptionFixture(overrides: Partial<UserSubscription> = {}): UserSubscription {
  return {
    id: 1,
    user_id: 1,
    group_id: 10,
    status: 'active',
    starts_at: '2026-01-01T00:00:00Z',
    expires_at: '2099-01-01T00:00:00Z',
    five_hour_usage_usd: 0,
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 0,
    five_hour_window_start: null,
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    group: {
      id: 10,
      name: 'Codex Plan',
      description: null,
      platform: 'openai',
      rate_multiplier: 1.3,
      rpm_limit: 0,
      is_exclusive: false,
      status: 'active',
      subscription_type: 'subscription',
      five_hour_limit_usd: 10,
      daily_limit_usd: null,
      weekly_limit_usd: null,
      monthly_limit_usd: null,
      allow_image_generation: false,
      allow_batch_image_generation: false,
      image_rate_independent: false,
      image_rate_multiplier: 1,
      batch_image_discount_multiplier: 1,
      batch_image_hold_multiplier: 1,
      image_price_1k: null,
      image_price_2k: null,
      image_price_4k: null,
      video_rate_independent: false,
      video_rate_multiplier: 1,
      video_price_480p: null,
      video_price_720p: null,
      video_price_1080p: null,
      web_search_price_per_call: null,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      claude_code_only: false,
      fallback_group_id: null,
      fallback_group_id_on_invalid_request: null,
      require_oauth_only: false,
      require_privacy_set: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    ...overrides,
  }
}

async function mountView(): Promise<VueWrapper> {
  const wrapper = mount(SubscriptionsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
      },
    },
  })
  await flushPromises()
  return wrapper
}

function tooltipElement(): HTMLElement {
  const tooltip = document.body.querySelector<HTMLElement>('[role="tooltip"]')
  if (!tooltip) throw new Error('tooltip not found')
  return tooltip
}

describe('SubscriptionsView OpenAI dynamic multiplier tip', () => {
  beforeEach(() => {
    getMySubscriptions.mockReset()
    getOpenAIUsageMultiplier.mockReset().mockResolvedValue({
      tiers: [
        {
          tier: '1x',
          baseline_quota_usd: 125,
          telemetry_quota_usd: 113.64,
          telemetry_updated_at: '2026-07-14T08:00:00Z',
          dynamic_multiplier: 1.1,
        },
        {
          tier: '20x',
          baseline_quota_usd: 2500,
          telemetry_quota_usd: 2334.83,
          telemetry_updated_at: '2026-07-13T08:00:00Z',
          dynamic_multiplier: 1.08,
        },
      ],
      dynamic_multiplier: 1.1,
      updated_at: '2026-07-14T08:00:00Z',
    })
    showError.mockReset()
    push.mockReset()
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('shows an icon beside each active OpenAI badge and reveals the formulas on hover', async () => {
    getMySubscriptions.mockResolvedValue([
      subscriptionFixture(),
      subscriptionFixture({ id: 2, group_id: 11 }),
    ])

    const wrapper = await mountView()

    expect(getOpenAIUsageMultiplier).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="openai-usage-tip"]').exists()).toBe(false)
    const triggers = wrapper.findAll('[data-testid="openai-dynamic-multiplier-trigger"]')
    expect(triggers).toHaveLength(2)
    expect(triggers[0].text()).toContain('Dynamic multiplier')
    expect(triggers[0].element.parentElement?.textContent).toContain('OpenAI')
    expect(wrapper.text()).toContain('Rate: ×1.3')
    expect(wrapper.text()).not.toContain('Current dynamic multiplier')
    expect(tooltipElement().style.display).toBe('none')

    await triggers[0].trigger('mouseenter')
    await flushPromises()

    const tooltip = tooltipElement()
    const tooltipText = tooltip.textContent ?? ''
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltipText).toContain('OpenAI dynamic multiplier details')
    expect(tooltipText).toContain('1x quota')
    expect(tooltipText).toContain('Baseline quota: $125')
    expect(tooltipText).toContain('Telemetry quota: $113.64')
    expect(tooltipText).toContain('Dynamic multiplier: $125 / $113.64 = 1.10x')
    expect(tooltipText).toContain('20x quota')
    expect(tooltipText).toContain('Baseline quota: $2,500')
    expect(tooltipText).toContain('Telemetry quota: $2,334.83')
    expect(tooltipText).toContain('Dynamic multiplier: $2,500 / $2,334.83 = 1.08x')
    expect(tooltipText).toContain('Current dynamic multiplier: 1.10x')
    expect(tooltipText).toContain('Telemetry time:')
    expect(tooltipText).toContain('2026')
    expect(tooltipText.indexOf('Telemetry time:')).toBeLessThan(tooltipText.indexOf('1x quota'))
    expect(tooltipText.toLowerCase()).not.toContain('minimum')
    expect(tooltipText.toLowerCase()).not.toContain('maximum')
    expect(tooltipText).not.toContain('20%')
    expect(tooltipText).not.toContain('coverage')
  })

  it('does not request or show the icon without an active OpenAI subscription', async () => {
    getMySubscriptions.mockResolvedValue([
      subscriptionFixture({
        group: { ...subscriptionFixture().group!, platform: 'anthropic' },
      }),
      subscriptionFixture({ id: 2, expires_at: '2020-01-01T00:00:00Z' }),
    ])

    const wrapper = await mountView()

    expect(getOpenAIUsageMultiplier).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="openai-dynamic-multiplier-trigger"]').exists()).toBe(false)
  })

  it('shows per-tier missing telemetry without an external pending state', async () => {
    getMySubscriptions.mockResolvedValue([subscriptionFixture()])
    getOpenAIUsageMultiplier.mockResolvedValue({
      tiers: [
        {
          tier: '1x',
          baseline_quota_usd: 125,
          telemetry_quota_usd: null,
          dynamic_multiplier: null,
        },
        {
          tier: '20x',
          baseline_quota_usd: 2500,
          telemetry_quota_usd: 2334.83,
          dynamic_multiplier: 1.08,
        },
      ],
      dynamic_multiplier: 1.08,
    })

    const wrapper = await mountView()
    expect(wrapper.text()).not.toContain('pending')

    await wrapper.get('[data-testid="openai-dynamic-multiplier-trigger"]').trigger('mouseenter')
    await flushPromises()

    const tooltipText = tooltipElement().textContent ?? ''
    expect(tooltipText).toContain('1x quota')
    expect(tooltipText).toContain('No telemetry quota available')
    expect(tooltipText).toContain('Current dynamic multiplier: 1.08x')
    expect(tooltipText).not.toContain('$125 /')
  })

  it('keeps the baseline tooltip when the supplemental request fails', async () => {
    getMySubscriptions.mockResolvedValue([subscriptionFixture()])
    getOpenAIUsageMultiplier.mockRejectedValue(new Error('temporary failure'))

    const wrapper = await mountView()

    expect(wrapper.find('[data-testid="openai-dynamic-multiplier-trigger"]').exists()).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('pending')

    await wrapper.get('[data-testid="openai-dynamic-multiplier-trigger"]').trigger('mouseenter')
    await flushPromises()

    const tooltipText = tooltipElement().textContent ?? ''
    expect(tooltipText).toContain('Baseline quota: $125')
    expect(tooltipText).toContain('Baseline quota: $2,500')
    expect(tooltipText).toContain('No dynamic multiplier estimate available')
    expect(tooltipText).not.toContain('20%')
    expect(tooltipText).not.toContain('coverage')
  })
})
