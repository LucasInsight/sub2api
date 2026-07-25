import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const homeViewSource = readFileSync(resolve(dir, '../../../views/HomeView.vue'), 'utf8')
const keyUsageViewSource = readFileSync(resolve(dir, '../../../views/KeyUsageView.vue'), 'utf8')
const indexSource = readFileSync(resolve(dir, '../../../../index.html'), 'utf8')

describe('public XTrust-AI branding', () => {
  it('removes the Sub2API repository links from public pages', () => {
    for (const source of [homeViewSource, keyUsageViewSource]) {
      expect(source).not.toContain('https://github.com/Wei-Shaw/sub2api')
      expect(source).not.toContain('githubUrl')
    }
  })

  it('uses the XTrust-AI static title and ICO favicon', () => {
    expect(indexSource).toContain('<title>XTrust-AI - AI API Gateway</title>')
    expect(indexSource).toContain('type="image/x-icon" href="/favicon.ico"')
  })
})
