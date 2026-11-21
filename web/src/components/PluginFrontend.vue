<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { t } from '../i18n'
import { toast } from '../feedback'
const props = defineProps<{ pluginId: string; enabled: boolean; version: string; csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const frame = ref<HTMLIFrameElement>()
const html = ref('')
let routes: { method: string; path: string }[] = []
let digest = ''
let generation = 0
const requests = new Map<string, AbortController>()
function clear() { generation++; html.value = ''; routes = []; digest = ''; for (const request of requests.values()) request.abort(); requests.clear() }
async function load() {
 clear(); if (!props.enabled) return
 const version = generation
 try {
  const response = await fetch(`/api/admin/plugin-packages/${encodeURIComponent(props.pluginId)}/frontend`)
  if (version !== generation) return
  if (response.status === 401) { emit('expired'); return }
  if (response.status === 404) return
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || '插件界面加载失败')
  if (version !== generation || typeof data.digest !== 'string' || !/^[a-f0-9]{64}$/.test(data.digest)) return
  html.value = data.html; routes = data.routes || []; digest = data.digest
 } catch (error) { if (version === generation) toast((error as Error).message, 'error') }
}
async function receive(event: MessageEvent) {
 if (!props.enabled || !frame.value || event.source !== frame.value.contentWindow || event.origin !== 'null') return
 const message = event.data
 if (!message || message.type !== 'wishingskin:request' || typeof message.id !== 'string' || message.id.length > 80 || !message.id || requests.has(message.id)) return
 const target = event.source as Window
 const respond = (status: number, body: unknown) => target.postMessage({ type: 'wishingskin:response', id: message.id, status, body }, '*')
 const method = typeof message.method === 'string' ? message.method.toUpperCase() : 'GET'
 const route = routes.find(route => route.method === method && route.path === message.path)
 if (!route || !digest || requests.size >= 4) { respond(403, { error: 'Request unavailable' }); return }
 const expectedPath = `/api/plugins/${encodeURIComponent(props.pluginId)}${route.path}`
 const endpoint = new URL(expectedPath, window.location.origin)
 if (endpoint.origin !== window.location.origin || endpoint.pathname !== expectedPath || endpoint.search || endpoint.hash) { respond(403, { error: 'Request unavailable' }); return }
 const body = message.body === undefined ? undefined : JSON.stringify(message.body)
 if (body && new TextEncoder().encode(body).length > 65536) { respond(413, { error: 'Request too large' }); return }
 const controller = new AbortController(); requests.set(message.id, controller)
 const version = generation
 try {
  const response = await fetch(endpoint, { method, mode: 'same-origin', redirect: 'error', signal: controller.signal, headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken, 'X-Plugin-Digest': digest }, body: method === 'GET' ? undefined : body })
  if (version !== generation) return
  if (response.status === 401) emit('expired')
  const payload = await response.json()
  if (version === generation) respond(response.status, payload)
 } catch { if (version === generation) respond(503, { error: 'Request failed' }) }
 finally { if (version === generation) requests.delete(message.id) }
}
window.addEventListener('message', receive)
watch(() => [props.pluginId, props.enabled, props.version], load, { immediate: true })
onBeforeUnmount(() => { clear(); window.removeEventListener('message', receive) })
</script>
<template><iframe v-if="html" ref="frame" class="plugin-frame" sandbox="allow-scripts" :srcdoc="html" :title="t('插件界面')" /></template>
<style scoped>.plugin-frame { width: 100%; min-height: 420px; margin-top: 24px; border: 1px solid var(--border); border-radius:var(--radius-card); background: var(--panel); }</style>
