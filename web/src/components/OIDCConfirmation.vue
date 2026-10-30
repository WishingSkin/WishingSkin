<script setup lang="ts">
import { feedbackRef } from '../feedback'
import AppModal from './AppModal.vue'
import { onMounted, ref } from 'vue'
import { t } from '../i18n'

interface Confirmation {
  purpose: 'link' | 'login'
  provider: string
  accountName: string
  accountEmail: string
  name: string
  username: string
  email: string
  emailVerified: boolean
}

const props = defineProps<{ csrfToken: string; loginLayout?: boolean }>()
const emit = defineEmits<{ completed: [next: string] }>()
const data = ref<Confirmation | null>(null)
const busy = ref(false)
const error = feedbackRef('error')

async function load() {
  error.value = ''
  try {
    const response = await fetch('/api/auth/oidc/confirmation', { credentials: 'same-origin' })
    const result = await response.json()
    if (!response.ok) { error.value = result.error || 'OIDC 确认已失效，请重新开始'; return }
    data.value = result as Confirmation
  } catch { error.value = 'OIDC 确认已失效，请重新开始' }
}

async function decide(action: 'confirm' | 'cancel') {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/api/auth/oidc/confirmation', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({ action }),
    })
    const result = await response.json()
    if (!response.ok) { error.value = result.error || 'OIDC 确认已失效，请重新开始'; return }
    emit('completed', result.next || (data.value?.purpose === 'link' ? '/user/account/profile' : '/login'))
  } catch { error.value = '暂时无法完成 OIDC 确认' }
  finally { busy.value = false }
}

function back() { window.location.assign(props.loginLayout ? '/login' : '/user/account/profile') }

onMounted(() => {
  void load()
})
</script>

<template>
  <AppModal :title="t('确认 OIDC 账号')" :busy="busy" @close="data ? decide('cancel') : back()">
    <p v-if="!data && !error" class="password-rule" role="status">{{ t('正在加载…') }}</p>
    <dl v-if="data" class="oidc-identity-details">
      <div><dt>{{ t('身份提供商') }}</dt><dd>{{ data.provider || t('未提供') }}</dd></div>
      <div><dt>{{ data.purpose === 'link' ? t('将绑定到本站账号') : t('将登录本站账号') }}</dt><dd>{{ data.accountName || t('未设置昵称') }}<span v-if="data.accountEmail"> ({{ data.accountEmail }})</span></dd></div>
      <div><dt>{{ t('显示名称') }}</dt><dd>{{ data.name || t('未提供') }}</dd></div>
      <div><dt>{{ t('用户名') }}</dt><dd>{{ data.username || t('未提供') }}</dd></div>
      <div><dt>{{ t('OIDC 邮箱') }}</dt><dd>{{ data.email || t('未提供') }}</dd></div>
      <div><dt>{{ t('邮箱验证状态') }}</dt><dd>{{ data.emailVerified ? t('已验证') : t('未验证或未提供') }}</dd></div>
    </dl>

    <div class="oidc-confirmation-actions">
      <button class="primary-button" type="button" :disabled="busy || !data" @click="decide('confirm')">{{ busy ? t('正在处理…') : data?.purpose === 'link' ? t('确认并绑定') : t('确认并登录') }}</button>
      <button v-if="data" class="secondary-button" type="button" :disabled="busy" @click="decide('cancel')">{{ t('取消') }}</button>
      <button v-else class="secondary-button" type="button" :disabled="busy" @click="back">{{ t('返回') }}</button>
    </div>
  </AppModal>
</template>
