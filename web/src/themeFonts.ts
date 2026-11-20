import type { FontPreset, FontScope } from './themePacks'

const SYSTEM_UI_STACK = '"Microsoft YaHei UI","Microsoft YaHei","PingFang SC",system-ui,sans-serif'

const BUILTIN_FACES = `
@font-face {
  font-family: "WishingMonocraft";
  src: url("/fonts/monocraft.otf") format("opentype");
  font-weight: 400 700;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: "WishingFusionPixel";
  src: url("/fonts/fusion-pixel-12px-zh_hans.woff2") format("woff2");
  font-weight: 400 700;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: "WishingArkPixel";
  src: url("/fonts/ark-pixel-12px-zh_hans.woff2") format("woff2");
  font-weight: 400 700;
  font-style: normal;
  font-display: swap;
}
`

let builtinInjected = false

function ensureBuiltinFaces() {
  if (builtinInjected || typeof document === 'undefined') return
  const style = document.createElement('style')
  style.dataset.wsPixelFonts = '1'
  style.textContent = BUILTIN_FACES
  document.head.appendChild(style)
  builtinInjected = true
}

function clearCustomFontNodes() {
  document.head.querySelectorAll('[data-ws-custom-font]').forEach(node => node.remove())
}

function isStylesheetFontUrl(url: string): boolean {
  const lower = url.toLowerCase()
  return lower.endsWith('.css') || lower.includes('fonts.googleapis.com') || lower.includes('fonts.bunny.net') || lower.includes('/css?') || lower.includes('/css2?')
}

function fontFormatFromUrl(url: string): string {
  const clean = url.split('?')[0].toLowerCase()
  if (clean.endsWith('.woff2') || clean.startsWith('/api/theme-font/')) return 'woff2'
  if (clean.endsWith('.woff')) return 'woff'
  if (clean.endsWith('.otf')) return 'opentype'
  if (clean.endsWith('.ttf')) return 'truetype'
  return 'woff2'
}

export function applyThemeFont(options: {
  preset?: FontPreset | string
  scope?: FontScope | string
  customFontUrl?: string
  uploadedFontUrl?: string
}) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const preset = (options.preset || 'system') as FontPreset
  const scope = (options.scope === 'all' ? 'all' : 'headings') as FontScope

  clearCustomFontNodes()
  if (preset === 'system') {
    delete root.dataset.fontPreset
    delete root.dataset.fontScope
    root.style.setProperty('--font-ui', SYSTEM_UI_STACK)
    root.style.setProperty('--font-display', SYSTEM_UI_STACK)
    return
  }

  ensureBuiltinFaces()
  let stack = SYSTEM_UI_STACK
  if (preset === 'monocraft') {
    stack = `"WishingMonocraft",${SYSTEM_UI_STACK}`
  } else if (preset === 'fusion_pixel') {
    stack = `"WishingMonocraft","WishingFusionPixel",${SYSTEM_UI_STACK}`
  } else if (preset === 'ark_pixel') {
    stack = `"WishingMonocraft","WishingArkPixel",${SYSTEM_UI_STACK}`
  } else if (preset === 'custom') {
    const source = (options.customFontUrl || '').trim() || (options.uploadedFontUrl || '').trim()
    if (source) {
      if (isStylesheetFontUrl(source)) {
        const link = document.createElement('link')
        link.rel = 'stylesheet'
        link.href = source
        link.dataset.wsCustomFont = '1'
        document.head.appendChild(link)
        stack = `"WishingCustomFont",${SYSTEM_UI_STACK}`
      } else {
        const style = document.createElement('style')
        style.dataset.wsCustomFont = '1'
        const safeUrl = source.replace(/["'\\]/g, '')
        style.textContent = `@font-face{font-family:"WishingCustomFont";src:url("${safeUrl}") format("${fontFormatFromUrl(safeUrl)}");font-weight:100 900;font-style:normal;font-display:swap}`
        document.head.appendChild(style)
        stack = `"WishingCustomFont",${SYSTEM_UI_STACK}`
      }
    }
  }

  root.dataset.fontPreset = preset
  root.dataset.fontScope = scope
  root.style.setProperty('--font-display', stack)
  root.style.setProperty('--font-ui', scope === 'all' ? stack : SYSTEM_UI_STACK)
}
