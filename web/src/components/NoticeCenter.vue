<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppIcon from './AppIcon.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { toast } from '../feedback'
import { locale, t } from '../i18n'

const props = defineProps<{
  notice: string
  format: string
  signedIn?: boolean
  userId?: number
  csrfToken?: string
}>()
const emit = defineEmits<{ navigate: [path: string]; expired: [] }>()

interface Notice { id: string; html: string; createdAt: number }
interface UserNotification {
  id: string
  kind: string
  title: string
  body: string
  link: string
  readAt: number
  createdAt: number
}

const opened = ref(false)
const trigger = ref<HTMLButtonElement>()
const popover = ref<HTMLElement>()
const tabPersonal = ref<HTMLButtonElement>()
const tabSite = ref<HTMLButtonElement>()
const position = ref({ top: '0px', left: '0px' })

const notices = ref<Notice[]>([])
const currentId = ref('')
const seenId = ref('')
const busy = ref(false)
const error = ref(false)

const items = ref<UserNotification[]>([])
const nextCursor = ref('')
const personalUnread = ref(0)
const inboxBusy = ref(false)
const inboxError = ref(false)
const readingAll = ref(false)

const tab = ref<'personal' | 'site'>('personal')
const lastTab = ref<'personal' | 'site'>('personal')
const sawSiteTab = ref(false)
const siteLoaded = ref(false)

const siteUnread = computed(() => !!currentId.value && currentId.value !== seenId.value)
const toggleLabel = computed(() => {
  if (!props.signedIn) return siteUnread.value ? t('公告（有新公告）') : t('公告')
  if (personalUnread.value > 0) return t('通知（{n} 条未读）', { n: personalUnread.value })
  if (siteUnread.value) return t('通知（有新公告）')
  return t('通知')
})

let noticeController: AbortController | undefined
let inboxController: AbortController | undefined
try { seenId.value = localStorage.getItem('wishingskin-notice-read') || '' } catch { /* Keep the read state for this visit. */ }

function safePath(value: string) {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/api/')) return ''
  return value
}

async function loadNotices() {
  noticeController?.abort()
  const request = noticeController = new AbortController()
  busy.value = true
  error.value = false
  try {
    const response = await fetch('/api/notices', { signal: request.signal, credentials: 'same-origin' })
    if (!response.ok) throw new Error('notices')
    const data: { notices: Notice[]; currentId: string } = await response.json()
    if (request.signal.aborted) return
    notices.value = data.notices
    currentId.value = data.currentId
    siteLoaded.value = true
  } catch {
    if (!request.signal.aborted) {
      error.value = true
      toast('暂时无法读取公告', 'error')
    }
  } finally {
    if (!request.signal.aborted) busy.value = false
  }
}

async function loadInbox(reset = false) {
  if (!props.signedIn) return
  inboxController?.abort()
  const request = inboxController = new AbortController()
  const cursor = reset ? '' : nextCursor.value
  inboxBusy.value = true
  inboxError.value = false
  try {
    const query = new URLSearchParams({ limit: '20' })
    if (cursor) query.set('cursor', cursor)
    const response = await fetch(`/api/notifications?${query}`, { signal: request.signal, credentials: 'same-origin' })
    if (response.status === 401) {
      if (!request.signal.aborted) emit('expired')
      return
    }
    if (!response.ok) throw new Error('notifications')
    const data: { notifications: UserNotification[]; unreadCount: number; nextCursor?: string } = await response.json()
    if (request.signal.aborted) return
    const list = Array.isArray(data.notifications) ? data.notifications : []
    items.value = reset ? list : [...items.value, ...list.filter(next => !items.value.some(prev => prev.id === next.id))]
    personalUnread.value = typeof data.unreadCount === 'number' ? data.unreadCount : 0
    nextCursor.value = data.nextCursor || ''
  } catch {
    if (!request.signal.aborted) inboxError.value = true
  } finally {
    if (!request.signal.aborted) inboxBusy.value = false
  }
}

function selectTab(next: 'personal' | 'site', focus = false) {
  tab.value = next
  lastTab.value = next
  if (next === 'site') {
    sawSiteTab.value = true
    if (!siteLoaded.value) void loadNotices()
  }
  if (focus) {
    void nextTick(() => {
      (next === 'personal' ? tabPersonal.value : tabSite.value)?.focus()
    })
  }
}

function moveTab(event: KeyboardEvent) {
  if (!props.signedIn) return
  if (event.key === 'ArrowRight' || event.key === 'ArrowLeft' || event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    if (event.key === 'Home') selectTab('personal', true)
    else if (event.key === 'End') selectTab('site', true)
    else selectTab(tab.value === 'personal' ? 'site' : 'personal', true)
  }
}

function place() {
  if (!trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const width = Math.min(420, window.innerWidth - 24)
  position.value = {
    top: Math.min(rect.bottom + 12, window.innerHeight - 120) + 'px',
    left: Math.max(12, Math.min(rect.right - width, window.innerWidth - width - 12)) + 'px',
  }
}

function toggled(event: Event) {
  opened.value = (event as ToggleEvent).newState === 'open'
  if (opened.value) {
    place()
    if (props.signedIn) {
      const initial = personalUnread.value > 0 ? 'personal' : siteUnread.value ? 'site' : lastTab.value
      selectTab(initial)
      void loadInbox(true)
    } else {
      sawSiteTab.value = true
    }
    if (!props.signedIn || tab.value === 'site') {
      void loadNotices()
    }
  } else {
    if ((!props.signedIn || sawSiteTab.value) && !error.value && currentId.value) {
      seenId.value = currentId.value
      try { localStorage.setItem('wishingskin-notice-read', currentId.value) } catch { /* Read state remains available for this visit. */ }
    }
    sawSiteTab.value = false
    if (props.signedIn) void loadInbox(true)
  }
}

function open() {
  place()
  popover.value?.togglePopover()
}

function openItem(item: UserNotification, event: MouseEvent) {
  if (!item.readAt) {
    item.readAt = Math.floor(Date.now() / 1000)
    personalUnread.value = Math.max(0, personalUnread.value - 1)
    if (props.csrfToken) {
      void fetch(`/api/notifications/${encodeURIComponent(item.id)}/read`, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': props.csrfToken },
        keepalive: true,
      }).catch(() => {})
    }
  }
  const dest = safePath(item.link)
  if (!event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && event.button === 0) {
    event.preventDefault()
    if (dest) {
      popover.value?.hidePopover()
      emit('navigate', dest)
    }
  }
}

async function readAll() {
  if (readingAll.value || !props.signedIn || !props.csrfToken) return
  readingAll.value = true
  const now = Math.floor(Date.now() / 1000)
  for (const item of items.value) {
    if (!item.readAt) item.readAt = now
  }
  personalUnread.value = 0
  await nextTick()
  tabPersonal.value?.focus()
  try {
    const response = await fetch('/api/notifications/read-all', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      toast('暂时无法标记全部已读', 'error')
      await loadInbox(true)
    }
  } catch {
    toast('暂时无法标记全部已读', 'error')
    await loadInbox(true)
  } finally {
    readingAll.value = false
  }
}

function formatSiteDate(ms: number) {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(ms))
}

function formatInboxDate(seconds: number) {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(seconds * 1000))
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible' && props.signedIn) {
    void loadInbox(true)
  }
}

watch([() => props.notice, () => props.format], () => { void loadNotices() })
watch([() => props.signedIn, () => props.userId], ([signedIn]) => {
  inboxController?.abort()
  items.value = []
  nextCursor.value = ''
  personalUnread.value = 0
  inboxError.value = false
  if (signedIn) {
    tab.value = 'personal'
    lastTab.value = 'personal'
    void loadInbox(true)
  } else {
    tab.value = 'site'
  }
}, { immediate: true })

onMounted(() => {
  void loadNotices()
  window.addEventListener('resize', place)
  window.addEventListener('scroll', place, true)
  document.addEventListener('visibilitychange', onVisibilityChange)
})
onBeforeUnmount(() => {
  noticeController?.abort()
  inboxController?.abort()
  window.removeEventListener('resize', place)
  window.removeEventListener('scroll', place, true)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <button ref="trigger" class="icon-button notice-toggle" type="button" :aria-label="toggleLabel" :title="signedIn ? t('通知') : t('公告')" aria-haspopup="dialog" :aria-expanded="opened" @click="open">
    <AppIcon name="bell" />
    <span v-if="signedIn && personalUnread" class="notice-count" aria-hidden="true">{{ personalUnread > 99 ? '99+' : personalUnread }}</span>
    <span v-else-if="siteUnread" class="notice-unread" aria-hidden="true"></span>
  </button>
  <Teleport to="body">
    <section ref="popover" popover="auto" role="dialog" class="notice-popover" aria-labelledby="notice-title" :style="position" @toggle="toggled">
      <div class="notice-popover-top">
        <header class="notice-popover-heading">
          <h2 id="notice-title">{{ signedIn ? t('通知') : t('站点公告') }}</h2>
          <div class="notice-popover-actions">
            <button v-if="signedIn && tab === 'personal' && personalUnread" class="text-button notice-read-all" type="button" :disabled="readingAll" @click="readAll">{{ t('全部标为已读') }}</button>
            <button type="button" class="text-button" :aria-label="t('关闭')" @click="popover?.hidePopover()"><AppIcon name="close" /></button>
          </div>
        </header>
        <div v-if="signedIn" class="segmented notice-tabs" role="tablist" :aria-label="t('通知类型')" @keydown="moveTab">
          <button id="notice-tab-personal" ref="tabPersonal" role="tab" type="button" :aria-selected="tab === 'personal'" :tabindex="tab === 'personal' ? 0 : -1" aria-controls="notice-panel-personal" @click="selectTab('personal')">
            {{ t('个人通知') }}<span v-if="personalUnread" class="notice-tab-count">{{ personalUnread > 99 ? '99+' : personalUnread }}</span>
          </button>
          <button id="notice-tab-site" ref="tabSite" role="tab" type="button" :aria-selected="tab === 'site'" :tabindex="tab === 'site' ? 0 : -1" aria-controls="notice-panel-site" @click="selectTab('site')">
            {{ t('站点公告') }}<span v-if="siteUnread" class="notice-tab-dot" aria-hidden="true"></span>
          </button>
        </div>
      </div>

      <div v-if="signedIn" v-show="tab === 'personal'" id="notice-panel-personal" role="tabpanel" aria-labelledby="notice-tab-personal" class="notice-inbox-panel" :aria-busy="inboxBusy">
        <LoadingSkeleton v-if="inboxBusy && !items.length" label="正在加载…" :count="3" />
        <p v-else-if="inboxError && !items.length" class="form-error" role="alert">{{ t('暂时无法读取通知') }} <button class="text-button" type="button" @click="loadInbox(true)">{{ t('重试') }}</button></p>
        <p v-else-if="!items.length" class="modal-empty">{{ t('暂无通知') }}</p>
        <ul v-else class="notice-inbox">
          <li v-for="item in items" :key="item.id">
            <a v-if="safePath(item.link)" class="notice-item" :class="{ 'is-unread': !item.readAt }" :href="safePath(item.link)" @click="openItem(item, $event)">
              <span class="notice-item-dot" aria-hidden="true"></span>
              <span class="notice-item-body">
                <span v-if="!item.readAt" class="sr-only">{{ t('未读') }}</span>
                <span class="notice-item-title">{{ item.title }}</span>
                <span v-if="item.body" class="notice-item-text">{{ item.body }}</span>
                <time :datetime="new Date(item.createdAt * 1000).toISOString()">{{ formatInboxDate(item.createdAt) }}</time>
              </span>
            </a>
            <button v-else type="button" class="notice-item notice-item-button" :class="{ 'is-unread': !item.readAt }" @click="openItem(item, $event)">
              <span class="notice-item-dot" aria-hidden="true"></span>
              <span class="notice-item-body">
                <span v-if="!item.readAt" class="sr-only">{{ t('未读') }}</span>
                <span class="notice-item-title">{{ item.title }}</span>
                <span v-if="item.body" class="notice-item-text">{{ item.body }}</span>
                <time :datetime="new Date(item.createdAt * 1000).toISOString()">{{ formatInboxDate(item.createdAt) }}</time>
              </span>
            </button>
          </li>
        </ul>
        <p v-if="inboxError && items.length" class="form-error" role="alert">{{ t('加载失败') }} <button class="text-button" type="button" @click="loadInbox()">{{ t('重试') }}</button></p>
        <button v-else-if="nextCursor" class="secondary-button notice-more" type="button" :disabled="inboxBusy" @click="loadInbox()">{{ inboxBusy ? t('正在加载…') : t('加载更多') }}</button>
      </div>

      <div v-show="!signedIn || tab === 'site'" id="notice-panel-site" :role="signedIn ? 'tabpanel' : undefined" :aria-labelledby="signedIn ? 'notice-tab-site' : undefined" class="notice-dialog-content" :aria-busy="busy">
        <LoadingSkeleton v-if="busy && !notices.length" label="正在加载…" :count="2" />
        <p v-else-if="error" class="form-error" role="alert"><button class="text-button" type="button" @click="loadNotices()">{{ t('重试') }}</button></p>
        <template v-else>
          <p v-if="!notices.length">{{ t('暂无公告') }}</p>
          <article v-for="item in notices" :key="item.id" class="notice-entry">
            <div v-if="item.createdAt || notices.length > 1" class="notice-entry-meta"><span v-if="item.id === currentId">{{ t('最新公告') }}</span><time v-if="item.createdAt" :datetime="new Date(item.createdAt).toISOString()">{{ formatSiteDate(item.createdAt) }}</time><span v-else-if="item.id !== currentId">{{ t('历史公告') }}</span></div>
            <div class="notice-content" v-html="item.html"></div>
          </article>
        </template>
      </div>
    </section>
  </Teleport>
</template>
