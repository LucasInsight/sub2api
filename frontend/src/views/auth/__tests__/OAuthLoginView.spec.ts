import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OAuthLoginView from '../OAuthLoginView.vue'

const {
  getPublicSettingsMock,
  showWarningMock,
  useCurrentIPGeoStatusMock,
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  showWarningMock: vi.fn(),
  useCurrentIPGeoStatusMock: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: (...args: any[]) => getPublicSettingsMock(...args),
  isWeChatWebOAuthEnabled: (settings: any) => settings?.wechat_oauth_enabled === true,
  resolveWeChatOAuthStart: () => ({ mode: null, unavailableReason: 'not_configured' }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
    showWarning: (...args: any[]) => showWarningMock(...args),
  }),
}))

vi.mock('@/composables/useCurrentIPGeoStatus', () => ({
  useCurrentIPGeoStatus: (...args: any[]) => useCurrentIPGeoStatusMock(...args),
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  }),
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: 'en' },
  }),
}))

function publicSettings(overrides: Record<string, unknown> = {}) {
  return {
    linuxdo_oauth_enabled: false,
    dingtalk_oauth_enabled: false,
    wechat_oauth_enabled: false,
    oidc_oauth_enabled: false,
    oidc_oauth_provider_name: 'OIDC',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    backend_mode_enabled: false,
    login_agreement_enabled: false,
    login_agreement_documents: [],
    ...overrides,
  }
}

function mountView() {
  return mount(OAuthLoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div class="card-glass"><slot /><slot name="footer" /></div>' },
        Icon: { props: ['name'], template: '<span :data-icon="name" />' },
        EmailOAuthButtons: {
          props: ['disabled', 'githubEnabled', 'googleEnabled'],
          template: '<div v-if="githubEnabled || googleEnabled" data-testid="email-oauth-buttons" :data-disabled="String(disabled)"></div>',
        },
        LinuxDoOAuthSection: {
          props: ['disabled'],
          template: '<div data-testid="linuxdo-oauth" :data-disabled="String(disabled)"></div>',
        },
        DingTalkOAuthSection: {
          props: ['disabled'],
          template: '<div data-testid="dingtalk-oauth" :data-disabled="String(disabled)"></div>',
        },
        WechatOAuthSection: {
          props: ['disabled'],
          template: '<div data-testid="wechat-oauth" :data-disabled="String(disabled)"></div>',
        },
        OidcOAuthSection: {
          props: ['disabled', 'providerName'],
          template: '<div data-testid="oidc-oauth" :data-disabled="String(disabled)" :data-provider="providerName"></div>',
        },
        LoginAgreementPrompt: {
          props: ['accepted'],
          emits: ['accept', 'reject', 'open'],
          template: '<button type="button" data-testid="login-agreement" :data-accepted="String(accepted)" @click="$emit(\'accept\')"></button>',
        },
      },
    },
  })
}

describe('OAuthLoginView', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    showWarningMock.mockReset()
    useCurrentIPGeoStatusMock.mockReset()
    sessionStorage.clear()
    localStorage.clear()
  })

  it('shows enabled OAuth providers without email/password login controls', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      linuxdo_oauth_enabled: true,
      dingtalk_oauth_enabled: true,
      wechat_oauth_enabled: true,
      oidc_oauth_enabled: true,
      oidc_oauth_provider_name: 'Company SSO',
    }))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.oauthLoginPageTitle')
    expect(wrapper.find('[data-testid="email-oauth-buttons"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="linuxdo-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dingtalk-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wechat-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="oidc-oauth"]').attributes('data-provider')).toBe('Company SSO')
    expect(wrapper.find('input#email').exists()).toBe(false)
    expect(wrapper.find('input#password').exists()).toBe(false)
    expect(wrapper.find('button[type="submit"]').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('does not run the IP geo check', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({ github_oauth_enabled: true }))

    mountView()
    await flushPromises()

    expect(useCurrentIPGeoStatusMock).not.toHaveBeenCalled()
  })

  it('shows an empty state when no OAuth provider is available', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings())

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.oauthLoginNoProvider')
    expect(wrapper.find('[data-testid="email-oauth-buttons"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="linuxdo-oauth"]').exists()).toBe(false)
  })

  it('keeps OAuth actions disabled until the login agreement is accepted', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      login_agreement_enabled: true,
      login_agreement_mode: 'checkbox',
      login_agreement_revision: 'terms-v1',
      login_agreement_documents: [{ id: 'terms', title: 'Terms' }],
    }))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="email-oauth-buttons"]').attributes('data-disabled')).toBe('true')
    await wrapper.find('[data-testid="login-agreement"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="email-oauth-buttons"]').attributes('data-disabled')).toBe('false')
  })
})
