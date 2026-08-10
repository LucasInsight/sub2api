import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OAuthLoginView from '../OAuthLoginView.vue'

const {
  getPublicSettingsMock,
  startOAuthLoginMock,
  showErrorMock,
  showWarningMock,
  useCurrentIPGeoStatusMock,
  verifyActionMock,
  captchaResetMock,
  locationState,
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  startOAuthLoginMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  useCurrentIPGeoStatusMock: vi.fn(),
  verifyActionMock: vi.fn(),
  captchaResetMock: vi.fn(),
  locationState: { href: 'http://localhost/oauth2-sso' },
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: any[]) => getPublicSettingsMock(...args),
    startOAuthLogin: (...args: any[]) => startOAuthLoginMock(...args),
    isWeChatWebOAuthEnabled: (settings: any) => settings?.wechat_oauth_enabled === true,
    resolveWeChatOAuthStart: () => ({ mode: null, unavailableReason: 'not_configured' }),
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
    showError: (...args: any[]) => showErrorMock(...args),
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
    tencent_captcha_enabled: false,
    tencent_captcha_app_id: '',
    tencent_captcha_region: 'cn',
    aliyun_captcha_enabled: false,
    aliyun_captcha_scene_id: '',
    aliyun_captcha_prefix: '',
    aliyun_captcha_region: 'cn',
    login_agreement_enabled: false,
    login_agreement_documents: [],
    ...overrides,
  }
}

const CaptchaChallengeStub = defineComponent({
  setup(_, { expose }) {
    expose({
      verifyAction: verifyActionMock,
      reset: captchaResetMock,
    })
    return () => h('div', { 'data-testid': 'action-captcha' })
  },
})

const EmailOAuthButtonsStub = defineComponent({
  props: {
    disabled: Boolean,
    githubEnabled: Boolean,
    googleEnabled: Boolean,
  },
  emits: ['start'],
  setup(props, { emit }) {
    return () => h('div', {
      'data-testid': 'email-oauth-buttons',
      'data-disabled': String(props.disabled),
    }, [
      props.githubEnabled
        ? h('button', {
            type: 'button',
            disabled: props.disabled,
            'data-testid': 'github-oauth',
            onClick: () => emit('start', {
              provider: 'github',
              params: { redirect: '/dashboard', aff_code: 'AFF123' },
            }),
          }, 'GitHub')
        : null,
      props.googleEnabled
        ? h('button', {
            type: 'button',
            disabled: props.disabled,
            'data-testid': 'google-oauth',
            onClick: () => emit('start', {
              provider: 'google',
              params: { redirect: '/dashboard', aff_code: 'AFF123' },
            }),
          }, 'Google')
        : null,
    ])
  },
})

function oauthSectionStub(testId: string, provider: string) {
  return defineComponent({
    props: {
      disabled: Boolean,
      providerName: String,
    },
    emits: ['start'],
    setup(props, { emit }) {
      return () => h('button', {
        type: 'button',
        disabled: props.disabled,
        'data-testid': testId,
        'data-disabled': String(props.disabled),
        'data-provider': props.providerName,
        onClick: () => emit('start', {
          provider,
          params: { redirect: '/dashboard' },
        }),
      }, provider)
    },
  })
}

function mountView() {
  return mount(OAuthLoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div class="card-glass"><slot /><slot name="footer" /></div>' },
        Icon: { props: ['name'], template: '<span :data-icon="name" />' },
        TurnstileWidget: CaptchaChallengeStub,
        EmailOAuthButtons: EmailOAuthButtonsStub,
        LinuxDoOAuthSection: oauthSectionStub('linuxdo-oauth', 'linuxdo'),
        DingTalkOAuthSection: oauthSectionStub('dingtalk-oauth', 'dingtalk'),
        WechatOAuthSection: oauthSectionStub('wechat-oauth', 'wechat'),
        OidcOAuthSection: oauthSectionStub('oidc-oauth', 'oidc'),
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
    startOAuthLoginMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    useCurrentIPGeoStatusMock.mockReset()
    verifyActionMock.mockReset()
    captchaResetMock.mockReset()
    startOAuthLoginMock.mockResolvedValue({ authorize_url: 'https://provider.example/authorize' })
    verifyActionMock.mockResolvedValue({ token: 'captcha-token', randstr: '' })
    locationState.href = 'http://localhost/oauth2-sso'
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState,
    })
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

  it.each([
    ['github', 'github_oauth_enabled', 'github-oauth'],
    ['google', 'google_oauth_enabled', 'google-oauth'],
    ['linuxdo', 'linuxdo_oauth_enabled', 'linuxdo-oauth'],
    ['dingtalk', 'dingtalk_oauth_enabled', 'dingtalk-oauth'],
    ['wechat', 'wechat_oauth_enabled', 'wechat-oauth'],
    ['oidc', 'oidc_oauth_enabled', 'oidc-oauth'],
  ])('starts the %s OAuth provider without an action captcha', async (provider, setting, testId) => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({ [setting]: true }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get(`[data-testid="${testId}"]`).trigger('click')

    const expectedParams = new URLSearchParams({ redirect: '/dashboard' })
    if (provider === 'github' || provider === 'google') {
      expectedParams.set('aff_code', 'AFF123')
    }
    expect(locationState.href).toBe(
      `/api/v1/auth/oauth/${provider}/start?${expectedParams.toString()}`
    )
    expect(startOAuthLoginMock).not.toHaveBeenCalled()
  })

  it('starts OAuth with a Tencent action captcha proof', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: 'tencent-app-id',
    }))
    verifyActionMock.mockResolvedValue({ token: 'ticket-1', randstr: '@rand-1' })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="github-oauth"]').trigger('click')
    await flushPromises()

    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(startOAuthLoginMock).toHaveBeenCalledWith(
      {
        provider: 'github',
        params: { redirect: '/dashboard', aff_code: 'AFF123' },
      },
      {
        tencent_captcha_ticket: 'ticket-1',
        tencent_captcha_randstr: '@rand-1',
      }
    )
    expect(locationState.href).toBe('https://provider.example/authorize')
    expect(captchaResetMock).toHaveBeenCalledOnce()
  })

  it('starts OAuth with an Aliyun action captcha proof', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      google_oauth_enabled: true,
      aliyun_captcha_enabled: true,
      aliyun_captcha_scene_id: 'scene-id',
      aliyun_captcha_prefix: 'prefix',
    }))
    verifyActionMock.mockResolvedValue({ token: 'captcha-verify-param', randstr: '' })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="google-oauth"]').trigger('click')
    await flushPromises()

    expect(startOAuthLoginMock).toHaveBeenCalledWith(
      {
        provider: 'google',
        params: { redirect: '/dashboard', aff_code: 'AFF123' },
      },
      { turnstile_token: 'captcha-verify-param' }
    )
    expect(locationState.href).toBe('https://provider.example/authorize')
    expect(captchaResetMock).toHaveBeenCalledOnce()
  })

  it('uses Aliyun when Tencent is enabled without a configured app ID', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      tencent_captcha_enabled: true,
      aliyun_captcha_enabled: true,
      aliyun_captcha_scene_id: 'scene-id',
      aliyun_captcha_prefix: 'prefix',
    }))
    verifyActionMock.mockResolvedValue({ token: 'captcha-verify-param', randstr: '' })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="github-oauth"]').trigger('click')
    await flushPromises()

    expect(startOAuthLoginMock).toHaveBeenCalledWith(
      {
        provider: 'github',
        params: { redirect: '/dashboard', aff_code: 'AFF123' },
      },
      { turnstile_token: 'captcha-verify-param' }
    )
  })

  it('does not start OAuth when the action captcha is cancelled', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: 'tencent-app-id',
    }))
    verifyActionMock.mockResolvedValue(null)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="github-oauth"]').trigger('click')
    await flushPromises()

    expect(startOAuthLoginMock).not.toHaveBeenCalled()
    expect(locationState.href).toBe('http://localhost/oauth2-sso')
    expect(captchaResetMock).toHaveBeenCalledOnce()
  })

  it('ignores repeated OAuth starts while an action captcha is pending', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: 'tencent-app-id',
    }))
    let resolveProof: (proof: null) => void = () => undefined
    verifyActionMock.mockImplementation(() => new Promise((resolve) => {
      resolveProof = resolve
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="github-oauth"]').trigger('click')
    expect(wrapper.get('[data-testid="email-oauth-buttons"]').attributes('data-disabled')).toBe('true')
    await wrapper.get('[data-testid="github-oauth"]').trigger('click')

    expect(verifyActionMock).toHaveBeenCalledOnce()
    resolveProof(null)
    await flushPromises()
    expect(wrapper.get('[data-testid="email-oauth-buttons"]').attributes('data-disabled')).toBe('false')
  })

  it('shows an error when the protected OAuth start fails', async () => {
    getPublicSettingsMock.mockResolvedValue(publicSettings({
      github_oauth_enabled: true,
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: 'tencent-app-id',
    }))
    startOAuthLoginMock.mockRejectedValue(new Error('oauth start failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="github-oauth"]').trigger('click')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith('oauth start failed')
    expect(locationState.href).toBe('http://localhost/oauth2-sso')
    expect(captchaResetMock).toHaveBeenCalledOnce()
  })
})
