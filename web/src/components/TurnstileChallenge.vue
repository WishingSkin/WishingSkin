<script lang="ts">
interface TurnstileAPI {
  render(element: HTMLElement, options: Record<string, unknown>): string
  remove(id: string): void
}
declare global { interface Window { turnstile?: TurnstileAPI } }
let loading: Promise<TurnstileAPI> | undefined
function loadTurnstile() {
  if (window.turnstile) return Promise.resolve(window.turnstile)
  if (!loading) loading = new Promise<TurnstileAPI>((resolve, reject) => {
    const script = document.createElement('script')
    const fail = () => { clearTimeout(timer); script.remove(); loading = undefined; reject(new Error('challenge')) }
    const timer = window.setTimeout(fail, 12000)
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
    script.async = true
    script.onload = () => { if (!window.turnstile) { fail(); return }; clearTimeout(timer); resolve(window.turnstile) }
    script.onerror = fail
    document.head.appendChild(script)
  })
  return loading
}
</script>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { locale, t } from '../i18n'
const props = defineProps<{ siteKey: string; action: 'login' | 'register'; dark: boolean }>()
const emit = defineEmits<{ token: [value: string] }>()
const host = ref<HTMLElement | null>(null)
const failed = ref(false)
const pending = ref(true)
let widget: string | undefined
let generation = 0
function remove() { if (widget !== undefined) window.turnstile?.remove(widget); widget = undefined }
async function render() {
  const version = ++generation
  remove()
  emit('token', '')
  failed.value = false
  pending.value = true
  if (!props.siteKey) { pending.value = false; failed.value = true; return }
  try {
    const api = await loadTurnstile()
    if (version !== generation || !host.value) return
    widget = api.render(host.value, {
      sitekey: props.siteKey, action: props.action, size: 'compact', theme: props.dark ? 'dark' : 'light', language: locale.value === 'en' ? 'en' : 'zh-cn',
      callback: (token: string) => { if (version === generation) { pending.value = false; failed.value = false; emit('token', token) } },
      'expired-callback': () => { if (version === generation) { failed.value = true; emit('token', '') } },
      'error-callback': () => { if (version === generation) { pending.value = false; failed.value = true; emit('token', '') }; return true },
      'timeout-callback': () => { if (version === generation) { pending.value = false; failed.value = true; emit('token', '') } },
    })
  } catch { if (version === generation) { failed.value = true; pending.value = false } }
}
onMounted(render)
watch([locale, () => props.dark, () => props.siteKey], render)
onBeforeUnmount(() => { generation++; remove() })
defineExpose({ reset: render })
</script>

<template>
  <div class="challenge-field" :aria-label="t('人机验证')">
    <div ref="host"></div>
    <p v-if="pending && !failed" role="status">{{ t('请完成人机验证。') }}</p>
    <div v-if="failed" role="alert"><p>{{ siteKey ? t('人机验证未完成或已失效，请重试。') : t('人机验证尚未配置，请联系管理员') }}</p><button v-if="siteKey" class="text-button" type="button" @click="render">{{ t('重新验证') }}</button></div>
  </div>
</template>
