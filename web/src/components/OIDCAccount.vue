<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { oidcMessages } from '../oidcMessages'
import { t } from '../i18n'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const state = ref({ enabled: false, name: 'OIDC', linked: false })
const loaded = ref(false), password = ref(''), busy = ref(false), error = ref('')
const failureCode = new URLSearchParams(window.location.search).get('oidcError')
const callbackError = failureCode ? oidcMessages[failureCode] || oidcMessages.failed : ''
const linkedNow = new URLSearchParams(window.location.search).get('oidcLinked') === '1'
if (failureCode || linkedNow) history.replaceState(null, '', window.location.pathname)
async function load() {
  error.value = ''
  try {
    const response = await fetch('/api/account/oidc', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法读取绑定状态'; return }
    state.value = data; loaded.value = true
  } catch { error.value = '暂时无法读取绑定状态' }
}
async function submit() {
  if (state.value.linked && !window.confirm(t('解除绑定会退出所有网页会话，之后可用本地密码重新登录。'))) return
  busy.value = true; error.value = ''
  try {
    const response = await fetch(state.value.linked ? '/api/account/oidc' : '/api/auth/oidc/start', { method: state.value.linked ? 'DELETE' : 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ password: password.value, link: true }) })
    password.value = ''
    if (response.status === 401) { emit('expired'); return }
    if (response.status === 204) { window.location.assign('/login'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '无法开始 OIDC 登录'; return }
    window.location.assign(data.url)
  } catch { error.value = '无法开始 OIDC 登录' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template><section class="account-password oidc-account"><h3>{{ t('第三方登录') }}</h3><p v-if="!loaded && !error" role="status">{{ t('正在加载…') }}</p><p v-if="callbackError" class="form-error" role="alert">{{ t(callbackError) }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }} <button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p><template v-if="loaded"><p>{{ state.linked ? t('已绑定 {name}', { name: state.name }) : state.enabled ? t('绑定后可使用 {name} 登录当前账号。', { name: state.name }) : t('站点尚未启用第三方登录。') }}</p><p v-if="linkedNow && state.linked" class="action-success" role="status">{{ t('OIDC 绑定成功。') }}</p><form v-if="state.enabled || state.linked" class="password-form" @submit.prevent="submit"><fieldset :disabled="busy"><label class="binding-field"><span>{{ t('当前密码') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label><button class="secondary-button" type="submit">{{ busy ? t('正在处理…') : state.linked ? t('解除绑定') : t('绑定 {name}', { name: state.name }) }}</button></fieldset></form></template></section></template>
