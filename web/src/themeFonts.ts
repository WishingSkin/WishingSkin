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

function sanitizeFontFamilyName(raw: string): string {
  return raw
    .replace(/["'\\;{}<>\r\n\t]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

export function extractStylesheetFontFamilies(source: string): string[] {
  const families: string[] = []
  const seen = new Set<string>()
  const push = (value: string) => {
    const clean = sanitizeFontFamilyName(value)
    if (!clean || seen.has(clean)) return
    seen.add(clean)
    families.push(clean)
  }
  try {
    const parsed = new URL(source, 'https://wishingskin.local')
    for (const raw of parsed.searchParams.getAll('family')) {
      for (const segment of raw.split('|')) {
        const name = segment.split(':')[0] || ''
        push(name)
      }
    }
  } catch {
    // Ignore malformed URLs and rely on explicit customFontFamily or fallback.
  }
  return families
}

export function resolveCustomFontStack(options: {
  source: string
  customFontFamily?: string
}): { isStylesheet: boolean; stack: string } {
  const source = options.source.trim()
  const isStylesheet = isStylesheetFontUrl(source)
  if (!isStylesheet) {
    return { isStylesheet: false, stack: `"WishingCustomFont",${SYSTEM_UI_STACK}` }
  }
  const explicit = (options.customFontFamily || '')
    .split(',')
    .map(sanitizeFontFamilyName)
    .filter(Boolean)
  const families = explicit.length > 0 ? explicit : extractStylesheetFontFamilies(source)
  const quoted = families.map(name => JSON.stringify(name))
  quoted.push('"WishingCustomFont"', SYSTEM_UI_STACK)
  return { isStylesheet: true, stack: quoted.join(',') }
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
  customFontFamily?: string
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
      const resolved = resolveCustomFontStack({ source, customFontFamily: options.customFontFamily })
      if (resolved.isStylesheet) {
        const link = document.createElement('link')
        link.rel = 'stylesheet'
        link.href = source
        link.dataset.wsCustomFont = '1'
        document.head.appendChild(link)
        stack = resolved.stack
      } else {
        const style = document.createElement('style')
        style.dataset.wsCustomFont = '1'
        const safeUrl = source.replace(/["'\\]/g, '')
        style.textContent = `@font-face{font-family:"WishingCustomFont";src:url("${safeUrl}") format("${fontFormatFromUrl(safeUrl)}");font-weight:100 900;font-style:normal;font-display:swap}`
        document.head.appendChild(style)
        stack = resolved.stack
      }
    }
  }

  root.dataset.fontPreset = preset
  root.dataset.fontScope = scope
  root.style.setProperty('--font-display', stack)
  root.style.setProperty('--font-ui', scope === 'all' ? stack : SYSTEM_UI_STACK)
}
