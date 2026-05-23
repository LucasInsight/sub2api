import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'

const {
  getPublicSettingsMock,
  registerMock,
  showErrorMock,
  showSuccessMock,
  showWarningMock,
  pushMock,
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  registerMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showWarningMock: vi.fn(),
  pushMock: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: (...args: any[]) => pushMock(...args),
  }),
  useRoute: () => ({
    query: {},
  }),
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key,
    },
  }),
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: 'en' },
  }),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    register: (...args: any[]) => registerMock(...args),
  }),
  useAppStore: () => ({
    cachedPublicSettings: null,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
    showError: (...args: any[]) => showErrorMock(...args),
    showSuccess: (...args: any[]) => showSuccessMock(...args),
    showWarning: (...args: any[]) => showWarningMock(...args),
  }),
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: any[]) => getPublicSettingsMock(...args),
    isWeChatWebOAuthEnabled: (settings: any) => settings?.wechat_oauth_enabled === true,
    validatePromoCode: vi.fn(),
    validateInvitationCode: vi.fn(),
  }
})

function publicSettings(overrides: Record<string, unknown> = {}) {
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
    login_agreement_documents: [],
    ...overrides,
  }
}

function mountView() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        EmailOAuthButtons: {
          props: ['githubEnabled', 'googleEnabled'],
          template: '<div v-if="githubEnabled || googleEnabled" data-testid="email-oauth-buttons"></div>',
        },
        LinuxDoOAuthSection: { template: '<div data-testid="linuxdo-oauth"></div>' },
        WechatOAuthSection: { template: '<div data-testid="wechat-oauth"></div>' },
        OidcOAuthSection: { template: '<div data-testid="oidc-oauth"></div>' },
        LoginAgreementPrompt: true,
        TurnstileWidget: true,
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

describe('RegisterView', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    registerMock.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    showWarningMock.mockReset()
    pushMock.mockReset()
    sessionStorage.clear()
    localStorage.clear()
  })

  it('hides email registration and keeps OAuth options in OAuth-only mode', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      registration_oauth_only_enabled: true,
      github_oauth_enabled: true,
    }))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('input#email').exists()).toBe(false)
    expect(wrapper.text()).toContain('auth.oauthOnlyRegistration')
    expect(wrapper.find('[data-testid="email-oauth-buttons"]').exists()).toBe(true)
  })

  it('shows a no-provider message when OAuth-only registration has no enabled providers', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      registration_oauth_only_enabled: true,
    }))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('input#email').exists()).toBe(false)
    expect(wrapper.text()).toContain('auth.oauthOnlyRegistrationNoProvider')
    expect(wrapper.find('[data-testid="email-oauth-buttons"]').exists()).toBe(false)
  })

  it('shows the agreement prompt in OAuth-only mode when agreement is required', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      registration_oauth_only_enabled: true,
      github_oauth_enabled: true,
      login_agreement_enabled: true,
      login_agreement_mode: 'checkbox',
      login_agreement_documents: [{ id: 'terms', title: 'Terms' }],
    }))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('input#email').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'LoginAgreementPrompt' }).exists()).toBe(true)
  })
})
