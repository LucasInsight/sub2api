import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '../RegisterView.vue'
import type { CurrentIPGeo } from '@/api/ipGeo'
import { resetCurrentIPGeoStatusForTest } from '@/composables/useCurrentIPGeoStatus'

const { getCurrentIPGeo, getPublicSettings, authStore, appStore, routerPush } = vi.hoisted(() => ({
  getCurrentIPGeo: vi.fn(),
  getPublicSettings: vi.fn(),
  authStore: {
    register: vi.fn()
  },
  appStore: {
    showError: vi.fn(),
    showWarning: vi.fn()
  },
  routerPush: vi.fn()
}))

const messages: Record<string, string> = {
  'auth.createAccount': 'Create account',
  'auth.signUpToStart': 'Sign up to start {siteName}',
  'auth.emailLabel': 'Email',
  'auth.emailPlaceholder': 'you@example.com',
  'auth.passwordLabel': 'Password',
  'auth.createPasswordPlaceholder': 'Create password',
  'auth.passwordHint': 'Use at least 6 characters',
  'auth.continue': 'Continue',
  'auth.alreadyHaveAccount': 'Already have an account?',
  'auth.signIn': 'Sign in',
  'common.optional': 'optional',
  'home.ipNotice.unsupportedTitle': 'Unsupported country/region',
  'home.ipNotice.supportedTitle': 'Access region supported',
  'home.ipNotice.unknownTitle': 'Unable to determine access region',
  'home.ipNotice.pendingTitle': 'Checking access country/region',
  'home.ipNotice.meta': 'IP: {ip} · Country/region: {country}',
  'home.ipNotice.blockedAction': 'This service is not available in this country/region.',
  'home.ipNotice.unsupportedDescription': '',
  'home.ipNotice.supportedDescription': 'This country or region is currently supported.',
  'home.ipNotice.unknownDescription': 'The current IP country or region cannot be determined.',
  'home.ipNotice.pendingDescription': 'Please wait while we verify the country/region for your current IP.',
  'home.ipNotice.unknownCountry': 'Unknown'
}

vi.mock('@/api/ipGeo', () => ({
  getCurrentIPGeo
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings,
  isWeChatWebOAuthEnabled: () => false,
  validatePromoCode: vi.fn(),
  validateInvitationCode: vi.fn()
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authStore,
  useAppStore: () => appStore
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRouter: () => ({ push: routerPush }),
    useRoute: () => ({ query: {} })
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en' },
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

function pendingIPGeoRequest(): Promise<CurrentIPGeo> {
  return new Promise(() => undefined)
}

function basePublicSettings() {
  return {
    registration_enabled: true,
    registration_oauth_only_enabled: false,
    email_verify_enabled: false,
    promo_code_enabled: false,
    invitation_code_enabled: false,
    turnstile_enabled: false,
    turnstile_site_key: '',
    site_name: 'Sub2API',
    linuxdo_oauth_enabled: false,
    wechat_oauth_enabled: false,
    oidc_oauth_enabled: false,
    oidc_oauth_provider_name: 'OIDC',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    registration_email_suffix_whitelist: [],
    login_agreement_enabled: false,
    login_agreement_documents: []
  }
}

async function mountRegister() {
  const wrapper = mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: {
          props: ['hideCard'],
          template: '<div><div v-if="$slots.notice"><slot name="notice" /></div><div v-if="!hideCard" class="card-glass"><slot /></div><footer><slot name="footer" /></footer></div>'
        },
        RouterLink: { props: ['to'], template: '<a :data-to="to"><slot /></a>' },
        Icon: { props: ['name'], template: '<span :data-icon="name" />' },
        TurnstileWidget: true,
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        LoginAgreementPrompt: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('RegisterView IP geo blocking', () => {
  beforeEach(() => {
    resetCurrentIPGeoStatusForTest()
    getCurrentIPGeo.mockReset()
    getPublicSettings.mockReset()
    routerPush.mockReset()
    appStore.showError.mockReset()
    appStore.showWarning.mockReset()
    getPublicSettings.mockResolvedValue(basePublicSettings())
  })

  it('shows a pending notice and hides registration form before the region check completes', async () => {
    getCurrentIPGeo.mockReturnValue(pendingIPGeoRequest())

    const wrapper = await mountRegister()

    expect(wrapper.text()).toContain('Checking access country/region')
    expect(wrapper.text()).toContain('Please wait while we verify the country/region')
    expect(wrapper.find('.card-glass').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[data-to="/login"]').exists()).toBe(false)
  })

  it('hides registration form and login link when the region is unsupported', async () => {
    getCurrentIPGeo.mockResolvedValue(makeIPGeo({ supported: false, support_status: 'unsupported' }))

    const wrapper = await mountRegister()

    expect(wrapper.text()).toContain('Unsupported country/region')
    expect(wrapper.text()).toContain('This service is not available in this country/region.')
    expect(wrapper.find('.card-glass').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[data-to="/login"]').exists()).toBe(false)
  })

  it('keeps registration form when the region is supported', async () => {
    getCurrentIPGeo.mockResolvedValue(makeIPGeo({ supported: true, support_status: 'supported' }))

    const wrapper = await mountRegister()

    expect(wrapper.text()).not.toContain('Access region not supported')
    expect(wrapper.find('.card-glass').exists()).toBe(true)
    expect(wrapper.find('form').exists()).toBe(true)
    expect(wrapper.find('[data-to="/login"]').exists()).toBe(true)
  })

  it('keeps registration form when the region check fails', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    getCurrentIPGeo.mockRejectedValue(new Error('lookup failed'))

    const wrapper = await mountRegister()

    expect(wrapper.find('.card-glass').exists()).toBe(true)
    expect(wrapper.find('form').exists()).toBe(true)
    expect(wrapper.find('[data-to="/login"]').exists()).toBe(true)
    warnSpy.mockRestore()
  })
})
