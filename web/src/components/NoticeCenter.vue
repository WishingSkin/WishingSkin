<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { locale, t } from '../i18n'

const props = defineProps<{ notice: string; format: string }>()
interface Notice { id: string; html: string; createdAt: number }
const dialog = ref<HTMLDialogElement | null>(null)
const notices = ref<Notice[]>([])
const currentId = ref('')
const seenId = ref('')
const busy = ref(false)
const error = ref(false)
const unread = computed(() => !!currentId.value && currentId.value !== seenId.value)
let controller: AbortController | undefined
try { seenId.value = localStorage.getItem('wishingskin-notice-read') || '' } catch { /* Keep the read state for this visit. */ }

async function load(autoOpen = true) {
  controller?.abort()
  const request = controller = new AbortController()
  busy.value = true
  error.value = false
  try {
    const response = await fetch('/api/notices', { signal: request.signal, credentials: 'same-origin' })
    if (!response.ok) throw new Error('notices')
    const data: { notices: Notice[]; currentId: string } = await response.json()
    if (request.signal.aborted) return
    notices.value = data.notices
    currentId.value = data.currentId
    if (autoOpen && unread.value && !dialog.value?.open) dialog.value?.showModal()
  } catch {
    if (!request.signal.aborted) error.value = true
  } finally {
    if (!request.signal.aborted) busy.value = false
  }
}
function open() {
  dialog.value?.showModal()
  void load(false)
}
function closed() {
  if (!error.value && currentId.value) {
    seenId.value = currentId.value
    try { localStorage.setItem('wishingskin-notice-read', currentId.value) } catch { /* Keep the read state for this visit. */ }
  }
}
function date(value: number) {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
watch([() => props.notice, () => props.format], () => { void load() })
onMounted(() => { void load() })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <button class="icon-button notice-toggle" type="button" :aria-label="unread ? t('公告（有新公告）') : t('公告')" :title="t('公告')" aria-haspopup="dialog" @click="open">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/></svg>
    <span v-if="unread" class="notice-unread" aria-hidden="true"></span>
  </button>
  <Teleport to="body">
    <dialog ref="dialog" class="notice-dialog" aria-labelledby="notice-dialog-title" @close="closed" @click="$event.target === dialog && dialog?.close()">
      <header class="notice-dialog-header"><h2 id="notice-dialog-title">{{ t('站点公告') }}</h2><button class="icon-button" type="button" :aria-label="t('关闭公告')" autofocus @click="dialog?.close()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18"/></svg></button></header>
      <div class="notice-dialog-content" :aria-busy="busy">
        <p v-if="busy && !notices.length" role="status">{{ t('正在加载…') }}</p>
        <p v-else-if="error" class="form-error" role="alert">{{ t('暂时无法读取公告') }} <button class="text-button" type="button" @click="load(false)">{{ t('重试') }}</button></p>
        <template v-else>
          <p v-if="!notices.length">{{ t('暂无公告') }}</p>
          <article v-for="notice in notices" :key="notice.id" class="notice-entry">
            <div v-if="notice.createdAt || notices.length > 1" class="notice-entry-meta"><span v-if="notice.id === currentId">{{ t('最新公告') }}</span><time v-if="notice.createdAt" :datetime="new Date(notice.createdAt).toISOString()">{{ date(notice.createdAt) }}</time><span v-else-if="notice.id !== currentId">{{ t('历史公告') }}</span></div>
            <div class="notice-content" v-html="notice.html"></div>
          </article>
        </template>
      </div>
    </dialog>
  </Teleport>
</template>
