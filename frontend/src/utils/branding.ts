import { sanitizeUrl } from '@/utils/url'

export const DEFAULT_SITE_NAME = 'XTrust-AI'

export function resolveSiteName(siteName?: string | null): string {
  const normalizedSiteName = siteName?.trim() || ''
  if (!normalizedSiteName || normalizedSiteName.toLowerCase() === 'sub2api') {
    return DEFAULT_SITE_NAME
  }
  return normalizedSiteName
}

function faviconMimeType(logoUrl: string): string {
  const normalizedUrl = logoUrl.toLowerCase().split(/[?#]/, 1)[0]
  if (normalizedUrl.startsWith('data:image/svg+xml') || normalizedUrl.endsWith('.svg')) {
    return 'image/svg+xml'
  }
  if (normalizedUrl.startsWith('data:image/png') || normalizedUrl.endsWith('.png')) {
    return 'image/png'
  }
  if (normalizedUrl.startsWith('data:image/webp') || normalizedUrl.endsWith('.webp')) {
    return 'image/webp'
  }
  return 'image/x-icon'
}

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (!sanitizedLogoUrl) {
    return
  }

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  link.type = faviconMimeType(sanitizedLogoUrl)
  link.href = sanitizedLogoUrl
}
