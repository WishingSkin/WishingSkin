<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { locale, t } from '../i18n'
import { toast } from '../feedback'
import { useUnsavedGuard } from '../unsavedGuard'
import CharacterViewer from './CharacterViewer.vue'
import TexturePicker from './TexturePicker.vue'
import type { TextureRecord } from '../textureTypes'

const props = defineProps<{
  pluginId: string
  enabled: boolean
  version: string
  csrfToken: string
  scope?: 'admin' | 'user'
  frontendId?: string
  initialContext?: Record<string, unknown>
  availableTools?: { pluginId: string; frontendId: string; label: string }[]
  tall?: boolean
  settingsRevision?: number
}>()
const emit = defineEmits<{
  expired: []
  mutated: [kind: 'textures' | 'characters']
  dirtyChange: [dirty: boolean]
  openTool: [payload: { pluginId: string; frontendId: string; context?: Record<string, unknown> }]
}>()

const frame = ref<HTMLIFrameElement>()
const html = ref('')
const frameHeight = ref(props.tall ? 660 : 460)
const iframeDirty = ref(false)
useUnsavedGuard(iframeDirty)

const pickerOpen = ref(false)
const pickerKind = ref<'skin' | 'cape'>('skin')
const pickerBusy = ref(false)
let pickerResolver: ((value: Record<string, unknown>) => void) | undefined
let pickerToken = 0
let pickerAbort: AbortController | undefined

const livePreview = ref<{ title: string; model: string; dataUrl: string } | null>(null)

const fontCache = new Map<string, string>()

let routes: { method: string; path: string }[] = []
let digest = ''
let generation = 0
let contextFrame: number | undefined
const requests = new Map<string, AbortController>()
const hostRequests = new Map<string, AbortController>()

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

function decodePNGBase64(raw: string): Uint8Array | null {
  const clean = raw.replace(/^data:image\/png;base64,/i, '').trim()
  if (!clean) return null
  try {
    const bin = atob(clean)
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out
  } catch {
    return null
  }
}

async function copyTextFallback(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Fall back to execCommand copy below.
  }
  if (typeof document === 'undefined') return false
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.cssText = 'position:fixed;left:-9999px;top:0;opacity:0'
  document.body.appendChild(area)
  area.select()
  let copied = false
  try {
    copied = document.execCommand('copy')
  } catch {
    copied = false
  } finally {
    area.remove()
  }
  return copied
}

async function loadFontFaceCss(fontPreset: string, fontScope: string): Promise<string> {
  const key = `${fontPreset}:${fontScope}`
  const cached = fontCache.get(key)
  if (cached !== undefined) return cached

  let url = ''
  let family = ''
  let format = 'woff2'
  if (fontPreset === 'monocraft') {
    url = '/fonts/monocraft.otf'
    family = 'WishingMonocraft'
    format = 'opentype'
  } else if (fontPreset === 'fusion_pixel') {
    url = '/fonts/fusion-pixel-12px-zh_hans.woff2'
    family = 'WishingFusionPixel'
  } else if (fontPreset === 'ark_pixel') {
    url = '/fonts/ark-pixel-12px-zh_hans.woff2'
    family = 'WishingArkPixel'
  }
  if (!url) {
    fontCache.set(key, '')
    return ''
  }
  try {
    const resp = await fetch(url, { credentials: 'same-origin' })
    if (!resp.ok) throw new Error('font fetch failed')
    const buf = await resp.arrayBuffer()
    const mime = format === 'opentype' ? 'font/otf' : 'font/woff2'
    const dataUri = `data:${mime};base64,${arrayBufferToBase64(buf)}`
    const css = `@font-face{font-family:"${family}";src:url("${dataUri}") format("${format}");font-weight:400 700;font-style:normal;font-display:swap;}`
    fontCache.set(key, css)
    return css
  } catch {
    return ''
  }
}

function readHostThemeTokens() {
  if (typeof document === 'undefined') {
    return { mode: 'light' as const, themePack: 'default', accent: 'blue', surface: 'solid', fontPreset: 'system', fontScope: 'headings', tokens: {} }
  }
  const root = document.documentElement
  const cs = getComputedStyle(root)
  const prop = (name: string) => cs.getPropertyValue(name).trim()
  const mode = root.dataset.theme === 'dark' ? 'dark' : 'light'
  return {
    mode,
    themePack: root.dataset.themePack || 'default',
    accent: root.dataset.accent || 'blue',
    surface: root.dataset.surface || 'solid',
    fontPreset: root.dataset.fontPreset || 'system',
    fontScope: root.dataset.fontScope || 'headings',
    tokens: {
      bg: prop('--bg') || prop('--canvas'),
      panel: prop('--panel'),
      subtle: prop('--subtle'),
      ink: prop('--ink'),
      muted: prop('--muted'),
      border: prop('--border'),
      accent: prop('--accent'),
      accentHover: prop('--accent-hover'),
      accentSoft: prop('--accent-soft'),
      buttonText: prop('--button-text') || '#ffffff',
      error: prop('--error'),
      errorBg: prop('--error-bg'),
      radiusCard: prop('--radius-card') || '10px',
      radiusControl: prop('--radius-control') || '8px',
      shadowCard: prop('--shadow-card'),
      fontSans: prop('--font-sans') || cs.fontFamily,
      fontDisplay: prop('--font-display') || prop('--font-sans') || cs.fontFamily,
    },
  }
}

function clear() {
  generation++
  html.value = ''
  routes = []
  digest = ''
  frameHeight.value = props.tall ? 660 : 460
  iframeDirty.value = false
  emit('dirtyChange', false)
  livePreview.value = null
  pickerToken++
  pickerAbort?.abort()
  pickerAbort = undefined
  if (pickerResolver) {
    pickerResolver({ cancelled: true })
    pickerResolver = undefined
  }
  pickerOpen.value = false
  pickerBusy.value = false
  for (const request of requests.values()) request.abort()
  requests.clear()
  for (const request of hostRequests.values()) request.abort()
  hostRequests.clear()
}

async function postContext() {
  const version = generation
  const themeSnap = readHostThemeTokens()
  const fontFaceCss = await loadFontFaceCss(themeSnap.fontPreset, themeSnap.fontScope)
  if (version !== generation || !frame.value?.contentWindow || !html.value) return
  frame.value.contentWindow.postMessage({
    type: 'wishingskin:context',
    context: {
      pluginId: props.pluginId,
      frontendId: props.frontendId || '',
      siteOrigin: typeof window !== 'undefined' ? window.location.origin : '',
      locale: locale.value,
      theme: themeSnap.mode,
      themePack: themeSnap.themePack,
      accent: themeSnap.accent,
      surface: themeSnap.surface,
      fontPreset: themeSnap.fontPreset,
      fontScope: themeSnap.fontScope,
      tokens: themeSnap.tokens,
      fontFaceCss,
      ...(props.availableTools ? { availableTools: props.availableTools } : {}),
      ...(props.initialContext || {}),
    },
  }, '*')
}

function schedulePostContext() {
  if (contextFrame !== undefined) cancelAnimationFrame(contextFrame)
  contextFrame = requestAnimationFrame(() => {
    contextFrame = undefined
    void postContext()
  })
}

async function load() {
  clear()
  if (!props.enabled) return
  if (props.scope === 'user' && !props.frontendId) return
  const version = generation
  const endpoint = props.scope === 'user' && props.frontendId
    ? `/api/user/plugin-packages/${encodeURIComponent(props.pluginId)}/frontend/${encodeURIComponent(props.frontendId)}`
    : `/api/admin/plugin-packages/${encodeURIComponent(props.pluginId)}/frontend`
  try {
    const response = await fetch(endpoint, { credentials: 'same-origin' })
    if (version !== generation) return
    if (response.status === 401) { emit('expired'); return }
    if (response.status === 404) return
    const data = await response.json()
    if (!response.ok) throw new Error(data.error || '插件界面加载失败')
    if (version !== generation || typeof data.digest !== 'string' || !/^[a-f0-9]{64}$/.test(data.digest)) return
    html.value = data.html
    routes = data.routes || []
    digest = data.digest
  } catch (error) {
    if (version === generation) toast((error as Error).message, 'error')
  }
}

async function handlePluginRequest(message: Record<string, unknown>, target: Window) {
  const id = message.id
  if (typeof id !== 'string' || !id || id.length > 80 || requests.has(id)) return
  const respond = (status: number, body: unknown) => target.postMessage({ type: 'wishingskin:response', id, status, body }, '*')
  const method = typeof message.method === 'string' ? message.method.toUpperCase() : 'GET'
  const rawPath = typeof message.path === 'string' ? message.path : ''
  let reqPath = rawPath
  let inlineQuery = ''
  const qIndex = rawPath.indexOf('?')
  if (qIndex >= 0) {
    reqPath = rawPath.slice(0, qIndex)
    inlineQuery = rawPath.slice(qIndex + 1)
  }
  const route = routes.find(r => r.method === method && r.path === reqPath)
  if (!route || !digest || requests.size >= 8) { respond(403, { error: 'Request unavailable' }); return }
  if (message.query !== undefined && (typeof message.query !== 'string' || message.query.length > 1024 || message.query.includes('#'))) {
    respond(400, { error: 'Invalid query' }); return
  }
  const rawQuery = typeof message.query === 'string' ? message.query : inlineQuery
  if (rawQuery.length > 1024 || rawQuery.includes('#')) {
    respond(400, { error: 'Invalid query' }); return
  }
  const query = rawQuery.replace(/^\?/, '')
  const expectedPath = `/api/plugins/${encodeURIComponent(props.pluginId)}${route.path}`
  const endpoint = new URL(expectedPath + (query ? `?${query}` : ''), window.location.origin)
  if (endpoint.origin !== window.location.origin || endpoint.pathname !== expectedPath || endpoint.hash) {
    respond(403, { error: 'Request unavailable' }); return
  }
  const maxBodyBytes = props.scope === 'user' ? 12 * 1024 * 1024 : 65536
  const body = message.body === undefined ? undefined : JSON.stringify(message.body)
  if (body && new TextEncoder().encode(body).length > maxBodyBytes) {
    respond(413, { error: 'Request too large' }); return
  }
  const controller = new AbortController()
  requests.set(id, controller)
  const version = generation
  try {
    const response = await fetch(endpoint, {
      method,
      mode: 'same-origin',
      redirect: 'error',
      signal: controller.signal,
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': props.csrfToken,
        'X-Plugin-Digest': digest,
      },
      body: method === 'GET' ? undefined : body,
    })
    if (version !== generation) return
    if (response.status === 401) emit('expired')
    const contentType = (response.headers.get('Content-Type') || '').split(';')[0].trim().toLowerCase()
    if (contentType.startsWith('image/') || contentType === 'application/octet-stream') {
      const buf = await response.arrayBuffer()
      if (version === generation) respond(response.status, { contentType, bodyBase64: arrayBufferToBase64(buf) })
      return
    }
    const payload = await response.json().catch(() => ({}))
    if (version === generation) respond(response.status, payload)
  } catch {
    if (version === generation) respond(503, { error: 'Request failed' })
  } finally {
    if (version === generation) requests.delete(id)
  }
}

async function handlePickSelection(item: TextureRecord, model?: string) {
  if (!pickerResolver) {
    pickerOpen.value = false
    return
  }
  const token = pickerToken
  pickerAbort?.abort()
  const controller = new AbortController()
  pickerAbort = controller
  pickerBusy.value = true
  try {
    const r = await fetch(`/textures/${encodeURIComponent(item.fileHash)}`, {
      credentials: 'same-origin',
      signal: controller.signal,
    })
    if (token !== pickerToken || !pickerResolver) return
    if (!r.ok) {
      toast('材质文件暂时无法加载。', 'error')
      return
    }
    const buf = await r.arrayBuffer()
    if (token !== pickerToken || !pickerResolver) return
    const resolve = pickerResolver
    pickerResolver = undefined
    pickerAbort = undefined
    pickerOpen.value = false
    resolve({
      cancelled: false,
      id: item.id,
      fileHash: item.fileHash,
      name: ('displayName' in item && typeof item.displayName === 'string' && item.displayName) || item.name,
      kind: item.kind,
      model: model === 'slim' || item.model === 'slim' ? 'slim' : 'default',
      dataUrl: `data:image/png;base64,${arrayBufferToBase64(buf)}`,
    })
  } catch (err) {
    if ((err as Error)?.name !== 'AbortError' && token === pickerToken) {
      toast('材质文件暂时无法加载。', 'error')
    }
  } finally {
    if (pickerAbort === controller) pickerAbort = undefined
    if (token === pickerToken) pickerBusy.value = false
  }
}

function handlePickCancel() {
  pickerToken++
  pickerAbort?.abort()
  pickerAbort = undefined
  const resolve = pickerResolver
  pickerResolver = undefined
  pickerOpen.value = false
  pickerBusy.value = false
  resolve?.({ cancelled: true })
}

async function handleHostBridge(message: Record<string, unknown>, target: Window) {
  const id = message.id
  const action = message.action
  if (typeof id !== 'string' || !id || id.length > 80 || typeof action !== 'string' || hostRequests.has(id)) return
  const reply = (ok: boolean, status: number, data?: unknown, error?: string) => {
    target.postMessage({ type: 'wishingskin:host-response', id, ok, status, data, error }, '*')
  }
  const payload = message.payload && typeof message.payload === 'object' ? message.payload as Record<string, unknown> : {}

  if (action === 'notify') {
    const text = typeof payload.message === 'string' ? payload.message.trim().slice(0, 240) : ''
    const kind = payload.kind === 'error' ? 'error' : 'success'
    if (text) toast(text, kind)
    reply(true, 200, { shown: !!text })
    return
  }
  if (action === 'copyText') {
    const text = typeof payload.text === 'string' ? payload.text : ''
    if (!text || text.length > 8192) {
      reply(false, 400, undefined, 'Invalid copy text')
      return
    }
    const copied = await copyTextFallback(text)
    if (!copied) {
      toast('无法自动复制，请选择并复制文本。', 'error')
      reply(false, 500, { copied: false }, 'Clipboard unavailable')
      return
    }
    if (payload.silent !== true) toast('已复制到剪贴板。')
    reply(true, 200, { copied: true })
    return
  }
  if (action === 'setDirty') {
    const nextDirty = payload.dirty === true
    if (iframeDirty.value !== nextDirty) {
      iframeDirty.value = nextDirty
      emit('dirtyChange', nextDirty)
    }
    reply(true, 200, { dirty: nextDirty })
    return
  }
  if (action === 'downloadFile') {
    const raw = typeof payload.dataUrl === 'string' && payload.dataUrl.trim()
      ? payload.dataUrl.trim()
      : (typeof payload.pngBase64 === 'string' && payload.pngBase64.trim()
          ? payload.pngBase64.trim()
          : (typeof payload.dataBase64 === 'string' ? payload.dataBase64.trim() : ''))
    if (!raw || raw.length > 12 * 1024 * 1024) {
      reply(false, 400, undefined, 'Invalid download payload')
      return
    }
    let blob: Blob | null = null
    if (/^data:image\/svg\+xml;base64,/i.test(raw)) {
      try {
        const bin = atob(raw.replace(/^data:image\/svg\+xml;base64,/i, '').trim())
        const out = new Uint8Array(bin.length)
        for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
        blob = new Blob([out], { type: 'image/svg+xml' })
      } catch {
        blob = null
      }
    } else if (/^data:image\/svg\+xml;charset=utf-8,/i.test(raw)) {
      try {
        const text = decodeURIComponent(raw.replace(/^data:image\/svg\+xml;charset=utf-8,/i, ''))
        blob = new Blob([text], { type: 'image/svg+xml' })
      } catch {
        blob = null
      }
    } else {
      const decoded = decodePNGBase64(raw)
      if (decoded && decoded.length > 0 && decoded.length <= 8 * 1024 * 1024) {
        const copy = new Uint8Array(decoded.length)
        copy.set(decoded)
        blob = new Blob([copy], { type: 'image/png' })
      }
    }
    if (!blob || typeof document === 'undefined') {
      reply(false, 400, undefined, 'Unsupported file payload')
      return
    }
    const rawName = (typeof payload.filename === 'string' ? payload.filename.trim() : '') || 'skin.png'
    const safeName = rawName.replace(/[^a-zA-Z0-9._\-\u4e00-\u9fff]/g, '_').slice(0, 120) || 'skin.png'
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = safeName
    a.style.display = 'none'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1500)
    reply(true, 200, { downloaded: true, filename: safeName })
    return
  }
  if (props.scope !== 'user') {
    reply(false, 403, undefined, 'Forbidden in admin scope')
    return
  }
  if (action === 'notifyMutated') {
    const kinds = new Set<'textures' | 'characters'>()
    const rawList = Array.isArray(payload.kinds) ? payload.kinds : [payload.kind]
    for (const k of rawList) {
      if (k === 'textures' || k === 'characters') kinds.add(k)
    }
    if (payload.textures === true) kinds.add('textures')
    if (payload.characters === true) kinds.add('characters')
    for (const k of kinds) emit('mutated', k)
    reply(true, 200, { notified: kinds.size })
    return
  }
  if (action === 'openPluginTool') {
    const targetPluginId = typeof payload.pluginId === 'string' ? payload.pluginId.trim() : ''
    const targetFrontendId = typeof payload.frontendId === 'string' ? payload.frontendId.trim() : ''
    const toolsList = props.availableTools || []
    const allowed = Boolean(targetPluginId && targetFrontendId) && toolsList.some(
      t => t.pluginId === targetPluginId && t.frontendId === targetFrontendId,
    )
    if (!allowed) {
      reply(false, 404, undefined, 'Target plugin tool unavailable')
      return
    }
    let handoffCtx: Record<string, unknown> | undefined
    if (payload.context !== undefined) {
      if (!payload.context || typeof payload.context !== 'object' || Array.isArray(payload.context)) {
        reply(false, 400, undefined, 'Invalid tool handoff context')
        return
      }
      try {
        if (JSON.stringify(payload.context).length > 2 * 1024 * 1024) {
          reply(false, 413, undefined, 'Tool handoff context too large')
          return
        }
      } catch {
        reply(false, 400, undefined, 'Invalid tool handoff context')
        return
      }
      handoffCtx = payload.context as Record<string, unknown>
    }
    if (iframeDirty.value) {
      iframeDirty.value = false
      emit('dirtyChange', false)
    }
    emit('openTool', { pluginId: targetPluginId, frontendId: targetFrontendId, context: handoffCtx })
    reply(true, 200, { opened: true })
    return
  }
  if (action === 'setPreview') {
    const rawB64 = typeof payload.pngBase64 === 'string' ? payload.pngBase64.trim() : ''
    if (!rawB64) {
      livePreview.value = null
      reply(true, 200, { cleared: true })
      return
    }
    if (rawB64.length > 2 * 1024 * 1024) {
      reply(false, 413, undefined, 'Preview PNG too large')
      return
    }
    const dataUrl = rawB64.startsWith('data:image/png;base64,') ? rawB64 : `data:image/png;base64,${rawB64}`
    livePreview.value = {
      title: typeof payload.title === 'string' && payload.title.trim() ? payload.title.trim().slice(0, 64) : '3D 实时预览',
      model: payload.model === 'slim' ? 'slim' : 'default',
      dataUrl,
    }
    reply(true, 200, { active: true })
    return
  }
  if (action === 'pickTexture') {
    if (pickerResolver) {
      reply(false, 409, undefined, 'Texture picker already open')
      return
    }
    pickerKind.value = payload.kind === 'cape' ? 'cape' : 'skin'
    pickerOpen.value = true
    const result = await new Promise<Record<string, unknown>>(resolve => {
      pickerResolver = resolve
    })
    reply(!result.cancelled, result.cancelled ? 499 : 200, result)
    return
  }
  if (hostRequests.size >= 8) { reply(false, 429, undefined, 'Too many host requests'); return }
  const controller = new AbortController()
  hostRequests.set(id, controller)
  const version = generation
  try {
    if (action === 'listTextures') {
      const source = payload.source === 'wardrobe' ? 'wardrobe' : payload.source === 'library' ? 'library' : 'textures'
      const params = new URLSearchParams({ page: '1' })
      const size = typeof payload.pageSize === 'number' && Number.isFinite(payload.pageSize)
        ? Math.min(100, Math.max(1, Math.trunc(payload.pageSize)))
        : 50
      params.set('pageSize', String(size))
      if (payload.kind === 'skin' || payload.kind === 'cape') params.set('kind', payload.kind)
      if (typeof payload.q === 'string' && payload.q.trim()) params.set('q', payload.q.trim().slice(0, 255))
      const r = await fetch(`/api/${source}?${params.toString()}`, { credentials: 'same-origin', signal: controller.signal })
      if (version !== generation) return
      if (r.status === 401) emit('expired')
      const data = await r.json().catch(() => ({}))
      reply(r.ok, r.status, r.ok ? data : undefined, r.ok ? undefined : data.error)
      return
    }
    if (action === 'getTextureDataUrl') {
      const hash = typeof payload.hash === 'string' ? payload.hash : ''
      if (!/^[a-f0-9]{64}$/.test(hash)) { reply(false, 400, undefined, 'Invalid texture hash'); return }
      const r = await fetch(`/textures/${hash}`, { credentials: 'same-origin', signal: controller.signal })
      if (version !== generation) return
      if (!r.ok) { reply(false, r.status, undefined, 'Texture unavailable'); return }
      const buf = await r.arrayBuffer()
      if (version === generation) reply(true, 200, { dataUrl: `data:image/png;base64,${arrayBufferToBase64(buf)}` })
      return
    }
    if (action === 'saveTexture') {
      const kind = payload.kind === 'skin' || payload.kind === 'cape' ? payload.kind : ''
      const rawB64 = typeof payload.pngBase64 === 'string' ? payload.pngBase64 : ''
      const decoded = decodePNGBase64(rawB64)
      if (!kind || !decoded || decoded.length === 0 || decoded.length > 8 * 1024 * 1024) {
        reply(false, 400, undefined, 'Invalid PNG upload payload'); return
      }
      const copy = new Uint8Array(decoded.length)
      copy.set(decoded)
      const name = (typeof payload.name === 'string' ? payload.name.trim() : '').slice(0, 255) || 'Custom Texture'
      const form = new FormData()
      form.append('file', new Blob([copy], { type: 'image/png' }), `${name}.png`)
      form.append('kind', kind)
      form.append('model', kind === 'skin' && payload.model === 'slim' ? 'slim' : 'default')
      form.append('name', name)
      form.append('description', typeof payload.description === 'string' ? payload.description.trim().slice(0, 200) : '')
      form.append('category', typeof payload.category === 'string' ? payload.category.trim() : '')
      form.append('tags', Array.isArray(payload.tags) ? payload.tags.map(String).join(',') : (typeof payload.tags === 'string' ? payload.tags : ''))
      form.append('isPublic', payload.isPublic === true ? 'true' : 'false')
      if (typeof payload.expectedCost === 'number' && Number.isFinite(payload.expectedCost) && payload.expectedCost >= 0) {
        form.append('expectedCost', String(Math.trunc(payload.expectedCost)))
      }
      const r = await fetch('/api/textures', {
        method: 'POST',
        credentials: 'same-origin',
        signal: controller.signal,
        headers: { 'X-CSRF-Token': props.csrfToken },
        body: form,
      })
      if (version !== generation) return
      if (r.status === 401) emit('expired')
      const data = await r.json().catch(() => ({}))
      if (r.ok) emit('mutated', 'textures')
      reply(r.ok, r.status, r.ok ? data : undefined, r.ok ? undefined : data.error)
      return
    }
    if (action === 'listCharacters') {
      const r = await fetch('/api/characters', { credentials: 'same-origin', signal: controller.signal })
      if (version !== generation) return
      if (r.status === 401) emit('expired')
      const data = await r.json().catch(() => ({}))
      reply(r.ok, r.status, r.ok ? data : undefined, r.ok ? undefined : data.error)
      return
    }
    if (action === 'applyTexture') {
      const charId = typeof payload.characterId === 'number' && Number.isInteger(payload.characterId) && payload.characterId > 0 ? payload.characterId : 0
      const kind = payload.kind === 'skin' || payload.kind === 'cape' ? payload.kind : ''
      const recordId = typeof payload.recordId === 'string' ? payload.recordId : ''
      const model = payload.model === 'default' || payload.model === 'slim' ? payload.model : undefined
      if (!charId || !kind || !/^[a-f0-9]{32}$/.test(recordId)) {
        reply(false, 400, undefined, 'Invalid character binding request'); return
      }
      const r = await fetch(`/api/characters/${charId}/textures/${kind}`, {
        method: 'PUT',
        credentials: 'same-origin',
        signal: controller.signal,
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
        body: JSON.stringify({ recordId, ...(model ? { model } : {}) }),
      })
      if (version !== generation) return
      if (r.status === 401) emit('expired')
      const data = r.status === 204 ? { applied: true } : await r.json().catch(() => ({}))
      if (r.ok) emit('mutated', 'characters')
      reply(r.ok, r.status, r.ok ? data : undefined, r.ok ? undefined : data.error)
      return
    }
    reply(false, 400, undefined, 'Unsupported action')
  } catch {
    if (version === generation) reply(false, 503, undefined, 'Request failed')
  } finally {
    if (version === generation) hostRequests.delete(id)
  }
}

function receive(event: MessageEvent) {
  if (!props.enabled || !frame.value || event.source !== frame.value.contentWindow || event.origin !== 'null') return
  const message = event.data as Record<string, unknown> | null
  if (!message || typeof message !== 'object') return
  const target = event.source as Window
  if (message.type === 'wishingskin:resize' && typeof message.height === 'number' && Number.isFinite(message.height)) {
    const next = Math.min(4800, Math.max(260, Math.ceil(message.height)))
    frameHeight.value = next
    return
  }
  if (message.type === 'wishingskin:request') void handlePluginRequest(message, target)
  else if (message.type === 'wishingskin:host') void handleHostBridge(message, target)
}

let themeObserver: MutationObserver | undefined
if (typeof MutationObserver !== 'undefined' && typeof document !== 'undefined') {
  themeObserver = new MutationObserver(schedulePostContext)
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme', 'data-theme-pack', 'data-accent', 'data-surface', 'data-font-preset', 'data-font-scope', 'style'],
  })
}

window.addEventListener('message', receive)
watch(() => [props.pluginId, props.enabled, props.version, props.scope, props.frontendId], load, { immediate: true })
watch([locale, () => props.initialContext, () => props.availableTools], schedulePostContext)
watch(() => props.settingsRevision, (rev) => {
  if (!rev || !frame.value?.contentWindow || !html.value) return
  frame.value.contentWindow.postMessage({ type: 'wishingskin:settings-updated', revision: rev }, '*')
})
onBeforeUnmount(() => {
  if (contextFrame !== undefined) cancelAnimationFrame(contextFrame)
  clear()
  themeObserver?.disconnect()
  window.removeEventListener('message', receive)
})
</script>
<template>
  <TexturePicker v-if="pickerOpen" :kind="pickerKind" :busy="pickerBusy" :confirm-label="t('载入此皮肤')" @close="handlePickCancel" @select="handlePickSelection" @expired="emit('expired')" />
  <div class="plugin-frontend-shell" :class="{ 'has-preview': Boolean(livePreview) }">
    <iframe v-if="html" ref="frame" class="plugin-frame" :class="{ tall: props.tall }" :style="{ height: `${frameHeight}px` }" sandbox="allow-scripts" :srcdoc="html" :title="t('插件界面')" @load="postContext" />
    <aside v-if="livePreview" class="plugin-preview-dock" :aria-label="t('3D 实时预览')">
      <div class="plugin-preview-dock-head">
        <h3>{{ t(livePreview.title) }}</h3>
        <button type="button" class="text-button" @click="livePreview = null">{{ t('收起') }}</button>
      </div>
      <CharacterViewer compact :name="livePreview.title" :model="livePreview.model" skin-hash="" cape-hash="" :skin-data-url="livePreview.dataUrl" />
    </aside>
  </div>
</template>
<style scoped>
.plugin-frame { width: 100%; min-height: 260px; margin-top: 16px; border: 0; border-radius: var(--radius-card); background: transparent; display: block; }
.plugin-frame.tall { min-height: 520px; margin-top: 0; }
</style>
