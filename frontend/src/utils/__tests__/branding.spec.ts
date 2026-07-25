import { beforeEach, describe, expect, it } from 'vitest'
import { DEFAULT_SITE_NAME, resolveSiteName, updateFavicon } from '@/utils/branding'

describe('resolveSiteName', () => {
  it('maps empty and legacy site names to the XTrust-AI brand', () => {
    expect(resolveSiteName()).toBe(DEFAULT_SITE_NAME)
    expect(resolveSiteName('')).toBe(DEFAULT_SITE_NAME)
    expect(resolveSiteName('   ')).toBe(DEFAULT_SITE_NAME)
    expect(resolveSiteName('Sub2API')).toBe(DEFAULT_SITE_NAME)
  })

  it('preserves a custom site name', () => {
    expect(resolveSiteName('Custom Gateway')).toBe('Custom Gateway')
  })
})

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.png">'
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.png')
  })

  it('sets the MIME type for supported image formats', () => {
    updateFavicon('/favicon.ico')
    expect(document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.type).toBe('image/x-icon')

    updateFavicon('/custom-logo.png')
    expect(document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.type).toBe('image/png')

    updateFavicon('/custom-logo.webp')
    expect(document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.type).toBe('image/webp')
  })
})
