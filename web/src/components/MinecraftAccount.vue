<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppModal from './AppModal.vue'
import { feedbackRef, toast } from '../feedback'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { locale, t } from '../i18n'

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()

interface MinecraftAccountState {
  enabled: boolean
  linked: boolean
  name: string
  uuid: string
  verifiedAt: number
}

interface MinecraftConfirmation {
  name: string
  uuid: string
  accountName: string
  accountEmail: string
}

const errorMessages: Record<string, string> = {
  not_owned: '此 Microsoft 账号未拥有 Minecraft Java 版',
  already_linked: '该正版账号已绑定其他本站账号',
  no_xbox: '此 Microsoft 账号尚未创建 Xbox 档案',
  child: '未成年账号需先加入家庭组后再试',
  denied: '已取消 Microsoft 授权',
  expired: '正版验证已过期，请重新开始',
  session: '账号会话已变化，请重新开始绑定。',
  failed: '正版验证未完成，请重试。',
}

const params = new URLSearchParams(window.location.search)
const failureCode = params.get('minecraftError')
const callbackError = failureCode ? (errorMessages[failureCode] || errorMessages.failed) : ''
const linkedNow = params.get('minecraftLinked') === '1'
const confirming = ref(params.get('minecraftConfirm') === '1')
if (failureCode || linkedNow || confirming.value) {
  history.replaceState(null, '', window.location.pathname)
}

const state = ref<MinecraftAccountState>({ enabled: false, linked: false, name: '', uuid: '', verifiedAt: 0 })
const confirmData = ref<MinecraftConfirmation | null>(null)
const confirmError = ref('')
const loaded = ref(false)
const unlinking = ref(false)
const password = ref('')
const busy = ref(false)
const error = feedbackRef('error')

function date(seconds: number) {
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(seconds * 1000))
}

async function load() {
  error.value = ''
  try {
    const response = await fetch('/api/account/minecraft', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as Partial<MinecraftAccountState> & { error?: string }
    if (!response.ok) { error.value = data.error || '暂时无法读取正版绑定状态'; return }
    state.value = {
      enabled: data.enabled === true,
      linked: data.linked === true,
      name: data.name || '',
      uuid: data.uuid || '',
      verifiedAt: typeof data.verifiedAt === 'number' ? data.verifiedAt : 0,
    }
    loaded.value = true
  } catch {
    error.value = '暂时无法读取正版绑定状态'
  }
}

async function loadConfirmation() {
  confirmError.value = ''
  try {
    const response = await fetch('/api/account/minecraft/confirmation', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as Partial<MinecraftConfirmation> & { error?: string }
    if (!response.ok || !data.uuid) {
      confirmError.value = data.error || errorMessages.expired
      return
    }
    confirmData.value = {
      name: data.name || '',
      uuid: data.uuid,
      accountName: data.accountName || '',
      accountEmail: data.accountEmail || '',
    }
  } catch {
    confirmError.value = '暂时无法读取正版验证详情'
  }
}

async function start() {
  if (busy.value) return
  const currentPassword = password.value
  password.value = ''
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/api/account/minecraft/start', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({ password: currentPassword }),
    })
    const data = await response.json().catch(() => ({})) as { url?: string; error?: string }
    if (response.status === 401) {
      if (data.error === '请先登录') { emit('expired'); return }
      error.value = data.error || '当前密码不正确'
      return
    }
    if (!response.ok || !data.url) {
      error.value = data.error || '无法发起 Microsoft 验证'
      return
    }
    window.location.assign(data.url)
  } catch {
    error.value = '无法发起 Microsoft 验证'
  } finally {
    busy.value = false
  }
}

async function decide(action: 'confirm' | 'cancel') {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/api/account/minecraft/confirmation', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({ action }),
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as { error?: string }
    if (!response.ok) {
      error.value = data.error || '暂时无法完成正版绑定确认'
      return
    }
    confirming.value = false
    confirmData.value = null
    if (action === 'confirm') {
      toast('正版 Minecraft 账号已绑定。')
    }
    await load()
  } catch {
    error.value = '暂时无法完成正版绑定确认'
  } finally {
    busy.value = false
  }
}

function back() {
  confirming.value = false
  confirmData.value = null
  confirmError.value = ''
}

function closeUnlink() {
  if (busy.value) return
  unlinking.value = false
  password.value = ''
}

async function unlink() {
  if (busy.value) return
  const currentPassword = password.value
  password.value = ''
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/api/account/minecraft', {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({ password: currentPassword }),
    })
    const data = await response.json().catch(() => ({})) as { error?: string }
    if (response.status === 401) {
      if (data.error === '请先登录') { emit('expired'); return }
      error.value = data.error || '当前密码不正确'
      return
    }
    if (!response.ok) {
      error.value = data.error || '暂时无法解除正版绑定'
      return
    }
    unlinking.value = false
    toast('已解除正版 Minecraft 绑定。')
    await load()
  } catch {
    error.value = '暂时无法解除正版绑定'
  } finally {
    busy.value = false
  }
}

function handlePageShow() {
  busy.value = false
  password.value = ''
}

onMounted(() => {
  window.addEventListener('pageshow', handlePageShow)
  if (callbackError) toast(callbackError, 'error')
  if (linkedNow) toast('正版 Minecraft 账号已绑定。')
  void load()
  if (confirming.value) void loadConfirmation()
})

onBeforeUnmount(() => {
  window.removeEventListener('pageshow', handlePageShow)
  password.value = ''
})
</script>

<template>
  <section v-if="(loaded && (state.enabled || state.linked)) || error" class="account-password minecraft-account">
    <h3>{{ t('正版 Minecraft') }}</h3>
    <p v-if="!loaded && error" class="form-error" role="alert"><button class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>

    <template v-else-if="loaded && !state.linked">
      <p>{{ t('验证 Minecraft Java 版持有状态，不会修改现有站内角色 UUID。') }}</p>
      <form class="password-form" @submit.prevent="start">
        <fieldset :disabled="busy">
          <label class="binding-field"><span>{{ t('当前本站密码') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
          <button class="secondary-button" type="submit">{{ busy ? t('正在跳转…') : t('使用 Microsoft 验证') }}</button>
        </fieldset>
      </form>
    </template>

    <template v-else-if="loaded">
      <dl class="oidc-identity-details">
        <div><dt>{{ t('正版角色名') }}</dt><dd>{{ state.name }}</dd></div>
        <div><dt>UUID</dt><dd class="minecraft-uuid">{{ state.uuid }}</dd></div>
        <div v-if="state.verifiedAt"><dt>{{ t('验证时间') }}</dt><dd><time :datetime="new Date(state.verifiedAt * 1000).toISOString()">{{ date(state.verifiedAt) }}</time></dd></div>
      </dl>
      <div class="settings-actions"><button class="secondary-button" type="button" :disabled="busy" @click="password = ''; unlinking = true">{{ t('解除绑定') }}</button></div>
    </template>
  </section>

  <AppModal v-if="unlinking" :title="t('解除正版绑定')" :busy="busy" @close="closeUnlink">
    <p>{{ t('解除后可重新验证。站内角色和材质不受影响。') }}</p>
    <form class="settings-form" @submit.prevent="unlink">
      <fieldset :disabled="busy">
        <label class="binding-field"><span>{{ t('当前本站密码') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
        <div class="modal-actions">
          <button class="secondary-button" type="button" @click="closeUnlink">{{ t('取消') }}</button>
          <button class="danger-button" type="submit">{{ busy ? t('正在处理…') : t('解除绑定') }}</button>
        </div>
      </fieldset>
    </form>
  </AppModal>

  <AppModal v-if="confirming" :title="t('确认正版账号')" :busy="busy" @close="confirmData ? decide('cancel') : back()">
    <LoadingSkeleton v-if="!confirmData && !confirmError" label="正在加载…" :count="2" />
    <p v-else-if="confirmError" class="form-error" role="alert">{{ t(confirmError) }}</p>
    <dl v-if="confirmData" class="oidc-identity-details">
      <div><dt>{{ t('正版角色名') }}</dt><dd>{{ confirmData.name }}</dd></div>
      <div><dt>UUID</dt><dd class="minecraft-uuid">{{ confirmData.uuid }}</dd></div>
      <div><dt>{{ t('将绑定到本站账号') }}</dt><dd>{{ confirmData.accountName || t('未设置昵称') }}<span v-if="confirmData.accountEmail"> ({{ confirmData.accountEmail }})</span></dd></div>
    </dl>
    <div class="oidc-confirmation-actions">
      <button v-if="confirmData" class="primary-button" type="button" :disabled="busy" @click="decide('confirm')">{{ busy ? t('正在处理…') : t('确认绑定') }}</button>
      <button class="secondary-button" type="button" :disabled="busy" @click="confirmData ? decide('cancel') : back()">{{ confirmData ? t('取消') : t('返回') }}</button>
    </div>
  </AppModal>
</template>
