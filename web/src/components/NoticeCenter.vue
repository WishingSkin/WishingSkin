<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppIcon from './AppIcon.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { toast } from '../feedback'
import { locale, t } from '../i18n'

const props = defineProps<{ notice: string; format: string }>()
interface Notice { id: string; html: string; createdAt: number }
const opened = ref(false)
const trigger = ref<HTMLButtonElement>()
const popover = ref<HTMLElement>()
const position = ref({ top: '0px', left: '0px' })
const notices = ref<Notice[]>([])
const currentId = ref('')
const seenId = ref('')
const busy = ref(false)
const error = ref(false)
const unread = computed(() => !!currentId.value && currentId.value !== seenId.value)
let controller: AbortController | undefined
try { seenId.value = localStorage.getItem('wishingskin-notice-read') || '' } catch { /* Keep the read state for this visit. */ }

async function load() {
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
  } catch {
    if (!request.signal.aborted) { error.value = true; toast('暂时无法读取公告', 'error') }
  } finally {
    if (!request.signal.aborted) busy.value = false
  }
}
function place() {
  if (!trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const width = Math.min(420, window.innerWidth - 24)
  position.value = { top: Math.min(rect.bottom + 12, window.innerHeight - 120) + 'px', left: Math.max(12, Math.min(rect.right - width, window.innerWidth - width - 12)) + 'px' }
}
function toggled(event: Event) {
  opened.value = (event as ToggleEvent).newState === 'open'
  if (opened.value) { place(); void load() }
  else if (!error.value && currentId.value) {
    seenId.value = currentId.value
    try { localStorage.setItem('wishingskin-notice-read', currentId.value) } catch { /* Read state remains available for this visit. */ }
  }
}
function open() { place(); popover.value?.togglePopover() }
function date(value: number) {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
watch([() => props.notice, () => props.format], () => { void load() })
onMounted(() => { void load(); window.addEventListener('resize', place); window.addEventListener('scroll', place, true) })
onBeforeUnmount(() => { controller?.abort(); window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true) })
</script>

<template>
  <button ref="trigger" class="icon-button notice-toggle" type="button" :aria-label="unread ? t('公告（有新公告）') : t('公告')" :title="t('公告')" aria-haspopup="dialog" :aria-expanded="opened" @click="open">
    <AppIcon name="bell" />
    <span v-if="unread" class="notice-unread" aria-hidden="true"></span>
  </button>
  <Teleport to="body"><section ref="popover" popover="auto" role="dialog" class="notice-popover" :aria-label="t('站点公告')" :style="position" @toggle="toggled"><header class="notice-popover-heading"><h2>{{ t('站点公告') }}</h2><button type="button" class="text-button" :aria-label="t('关闭')" @click="popover?.hidePopover()"><AppIcon name="close" /></button></header>
      <div class="notice-dialog-content" :aria-busy="busy">
        <LoadingSkeleton v-if="busy && !notices.length" label="正在加载…" :count="2" />
        <p v-else-if="error" class="form-error" role="alert"><button class="text-button" type="button" @click="load()">{{ t('重试') }}</button></p>
        <template v-else>
          <p v-if="!notices.length">{{ t('暂无公告') }}</p>
          <article v-for="notice in notices" :key="notice.id" class="notice-entry">
            <div v-if="notice.createdAt || notices.length > 1" class="notice-entry-meta"><span v-if="notice.id === currentId">{{ t('最新公告') }}</span><time v-if="notice.createdAt" :datetime="new Date(notice.createdAt).toISOString()">{{ date(notice.createdAt) }}</time><span v-else-if="notice.id !== currentId">{{ t('历史公告') }}</span></div>
            <div class="notice-content" v-html="notice.html"></div>
          </article>
        </template>
      </div>
    </section></Teleport>
</template>
