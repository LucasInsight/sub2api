import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import ConfirmDialog from '../ConfirmDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('ConfirmDialog', () => {
  it('disables confirmation when requested', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: {
        show: true,
        title: 'Confirm',
        message: 'Continue?',
        confirmDisabled: true
      },
      global: { stubs: { teleport: true } }
    })

    const confirm = wrapper.get('[data-testid="confirm-dialog-confirm"]')
    expect(confirm.attributes('disabled')).toBeDefined()

    await wrapper.setProps({ confirmDisabled: false })
    const enabledConfirm = wrapper.get('[data-testid="confirm-dialog-confirm"]')
    expect(enabledConfirm.attributes('disabled')).toBeUndefined()
    await enabledConfirm.trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })
})
