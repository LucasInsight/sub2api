import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import HomeView from '../HomeView.vue'
import type { CurrentIPGeo } from '@/api/ipGeo'
import { resetCurrentIPGeoStatusForTest } from '@/composables/useCurrentIPGeoStatus'

const { getCurrentIPGeo, authState, appState } = vi.hoisted(() => ({
  getCurrentIPGeo: vi.fn(),
  authState: {
    isAuthenticated: false,
    isAdmin: false,
    user: null as { email?: string } | null,
    checkAuth: vi.fn()
  },
  appState: {
    cachedPublicSettings: null as Record<string, unknown> | null,
    siteName: 'Sub2API',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn()
  }
}))

const messages: Record<string, string> = {
  'home.viewDocs': 'Docs',
  'home.switchToLight': 'Light',
  'home.switchToDark': 'Dark',
  'home.dashboard': 'Dashboard',
  'home.login': 'Login',
  'home.getStarted': 'Get Started',
  'home.goToDashboard': 'Go to Dashboard',
  'home.tags.subscriptionToApi': 'Subscription to API',
  'home.tags.stickySession': 'Session Persistence',
  'home.tags.realtimeBilling': 'Pay As You Go',
  'home.features.unifiedGateway': 'One-Click Access',
  'home.features.unifiedGatewayDesc': 'Gateway description',
  'home.features.multiAccount': 'Always Reliable',
  'home.features.multiAccountDesc': 'Account description',
  'home.features.balanceQuota': 'Pay What You Use',
  'home.features.balanceQuotaDesc': 'Quota description',
  'home.providers.title': 'Supported AI Models',
  'home.providers.description': 'One API, Multiple Choices',
  'home.providers.supported': 'Supported',
  'home.providers.soon': 'Soon',
  'home.providers.claude': 'Claude',
  'home.providers.gemini': 'Gemini',
  'home.providers.antigravity': 'Antigravity',
  'home.providers.more': 'More',
  'home.footer.allRightsReserved': 'All rights reserved.',
  'home.docs': 'Docs',
  'home.ipNotice.supportedTitle': 'Access region supported',
  'home.ipNotice.unsupportedTitle': 'This service is not supported in the current country/region',
  'home.ipNotice.unknownTitle': 'Unable to determine access region',
  'home.ipNotice.meta': 'IP: {ip} · Country/region: {country}',
  'home.ipNotice.blockedAction': 'This service is not available in this country/region.',
  'home.ipNotice.supportedDescription': 'This country or region is currently supported.',
  'home.ipNotice.unsupportedDescription': '',
  'home.ipNotice.unknownDescription': 'The current IP country or region cannot be determined.',
  'home.ipNotice.unknownCountry': 'Unknown'
}

vi.mock('@/api/ipGeo', () => ({
  getCurrentIPGeo
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authState,
  useAppStore: () => appState
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => {
        let value = messages[key] ?? key
        for (const [paramKey, paramValue] of Object.entries(params ?? {})) {
          value = value.replace(`{${paramKey}}`, paramValue)
        }
        return value
      }
    })
  }
})

function makeIPGeo(overrides: Partial<CurrentIPGeo>): CurrentIPGeo {
  return {
    ip: '8.8.8.8',
    country_code: 'US',
    country_known: true,
    is_china: false,
    supported: true,
    support_status: 'supported',
    ...overrides
  }
}

async function mountHome() {
  const wrapper = mount(HomeView, {
    global: {
      stubs: {
        RouterLink: { props: ['to'], template: '<a :data-to="to"><slot /></a>' },
        LocaleSwitcher: true,
        Icon: { props: ['name'], template: '<span :data-icon="name" />' }
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('HomeView IP geo notice', () => {
  beforeEach(() => {
    getCurrentIPGeo.mockReset()
    resetCurrentIPGeoStatusForTest()
    authState.checkAuth.mockReset()
    appState.fetchPublicSettings.mockReset()
    appState.cachedPublicSettings = null
    appState.publicSettingsLoaded = true
    localStorage.clear()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false })
    })
  })

  it('does not render a notice and keeps login entry when the country is supported', async () => {
    getCurrentIPGeo.mockResolvedValue(makeIPGeo({ supported: true, support_status: 'supported' }))

    const wrapper = await mountHome()

    expect(wrapper.text()).not.toContain('Access region supported')
    expect(wrapper.text()).not.toContain('IP:')
    expect(wrapper.findAll('[data-to="/login"]').length).toBeGreaterThan(0)
  })

  it('renders a warning notice and hides login entries when the country is unsupported', async () => {
    getCurrentIPGeo.mockResolvedValue(makeIPGeo({ supported: false, support_status: 'unsupported' }))

    const wrapper = await mountHome()

    expect(wrapper.text()).toContain('This service is not supported in the current country/region')
    expect(wrapper.text()).toContain('IP: 8.8.8.8 · Country/region: US')
    expect(wrapper.html()).toContain('border-amber-300')
    expect(wrapper.text()).toContain('This service is not available in this country/region.')
    expect(wrapper.findAll('[data-to="/login"]')).toHaveLength(0)
  })

  it('renders an unknown-region notice when the country cannot be determined', async () => {
    getCurrentIPGeo.mockResolvedValue(makeIPGeo({
      ip: '127.0.0.1',
      country_code: '',
      country_known: false,
      supported: false,
      support_status: 'unknown'
    }))

    const wrapper = await mountHome()

    expect(wrapper.text()).toContain('Unable to determine access region')
    expect(wrapper.text()).toContain('IP: 127.0.0.1 · Country/region: Unknown')
    expect(wrapper.html()).toContain('border-gray-200')
    expect(wrapper.findAll('[data-to="/login"]').length).toBeGreaterThan(0)
  })
})
