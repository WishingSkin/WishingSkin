<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { locale, t } from '../i18n'
import { toast } from '../feedback'

const props = defineProps<{
  pluginId: string
  enabled: boolean
  version: string
  csrfToken: string
  scope?: 'admin' | 'user'
  frontendId?: string
  initialContext?: Record<string, unknown>
  tall?: boolean
}>()
const emit = defineEmits<{ expired: []; mutated: [kind: 'textures' | 'characters'] }>()

const frame = ref<HTMLIFrameElement>()
const html = ref('')
const themeMode = ref<'dark' | 'light'>(
  typeof document !== 'undefined' && document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light',
)

let routes: { method: string; path: string }[] = []
let digest = ''
let generation = 0
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

function clear() {
  generation++
  html.value = ''
  routes = []
  digest = ''
  for (const request of requests.values()) request.abort()
  requests.clear()
  for (const request of hostRequests.values()) request.abort()
  hostRequests.clear()
}

function postContext() {
  if (!frame.value?.contentWindow || !html.value) return
  frame.value.contentWindow.postMessage({
    type: 'wishingskin:context',
    context: {
      pluginId: props.pluginId,
      frontendId: props.frontendId || '',
      locale: locale.value,
      theme: themeMode.value,
      ...(props.initialContext || {}),
    },
  }, '*')
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
  const reqPath = typeof message.path === 'string' ? message.path : ''
  const route = routes.find(r => r.method === method && r.path === reqPath)
  if (!route || !digest || requests.size >= 4) { respond(403, { error: 'Request unavailable' }); return }
  if (message.query !== undefined && (typeof message.query !== 'string' || message.query.length > 1024 || message.query.includes('#'))) {
    respond(400, { error: 'Invalid query' }); return
  }
  const query = typeof message.query === 'string' ? message.query.replace(/^\?/, '') : ''
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

async function handleHostBridge(message: Record<string, unknown>, target: Window) {
  if (props.scope !== 'user') return
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
  if (hostRequests.size >= 4) { reply(false, 429, undefined, 'Too many host requests'); return }
  const controller = new AbortController()
  hostRequests.set(id, controller)
  const version = generation
  try {
    if (action === 'listTextures') {
      const params = new URLSearchParams({ page: '1' })
      const size = typeof payload.pageSize === 'number' && Number.isFinite(payload.pageSize)
        ? Math.min(100, Math.max(1, Math.trunc(payload.pageSize)))
        : 50
      params.set('pageSize', String(size))
      if (payload.kind === 'skin' || payload.kind === 'cape') params.set('kind', payload.kind)
      const r = await fetch(`/api/textures?${params.toString()}`, { credentials: 'same-origin', signal: controller.signal })
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
  if (message.type === 'wishingskin:request') void handlePluginRequest(message, target)
  else if (message.type === 'wishingskin:host') void handleHostBridge(message, target)
}

let themeObserver: MutationObserver | undefined
if (typeof MutationObserver !== 'undefined' && typeof document !== 'undefined') {
  themeObserver = new MutationObserver(() => {
    themeMode.value = document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
}

window.addEventListener('message', receive)
watch(() => [props.pluginId, props.enabled, props.version, props.scope, props.frontendId], load, { immediate: true })
watch([locale, themeMode, () => props.initialContext], postContext)
onBeforeUnmount(() => { clear(); themeObserver?.disconnect(); window.removeEventListener('message', receive) })
</script>
<template><iframe v-if="html" ref="frame" class="plugin-frame" :class="{ tall: props.tall }" sandbox="allow-scripts" :srcdoc="html" :title="t('插件界面')" @load="postContext" /></template>
<style scoped>
.plugin-frame { width: 100%; min-height: 420px; margin-top: 24px; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--panel); }
.plugin-frame.tall { min-height: 620px; margin-top: 0; }
</style>
