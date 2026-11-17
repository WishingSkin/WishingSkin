<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { feedbackRef, confirmAction, toast } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import { oidcMessages } from '../oidcMessages'
import { t } from '../i18n'
import OIDCConfirmation from './OIDCConfirmation.vue'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const providers = ref<{ id: string; enabled: boolean; name: string; linked: boolean }[]>([])
const selected=ref('')
const state=computed(()=>providers.value.find(p=>p.id===selected.value) || { id:'',enabled:false,name:'OIDC',linked:false })
const loaded = ref(false), password = ref(''), busy = ref(false), error = feedbackRef('error')
const failureCode = new URLSearchParams(window.location.search).get('oidcError')
const callbackError = failureCode ? oidcMessages[failureCode] || oidcMessages.failed : ''
const linkedNow = new URLSearchParams(window.location.search).get('oidcLinked') === '1'
const confirming = new URLSearchParams(window.location.search).get('oidcConfirm') === '1'
if (failureCode || linkedNow) history.replaceState(null, '', window.location.pathname)
async function load() {
  error.value = ''
  try {
    const response = await fetch('/api/account/oidc', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法读取绑定状态'; return }
    providers.value = data.providers || [{ ...data,id:'' }]; loaded.value = true
  } catch { error.value = '暂时无法读取绑定状态' }
}
async function submit() {
  if (busy.value || (state.value.linked && !await confirmAction({ title: '解除绑定', message: t('解除与 {name} 的绑定？完成后需要重新登录。', { name: state.value.name }), danger: true }))) return
  busy.value = true; error.value = ''
  try {
    const response = await fetch(state.value.linked ? '/api/account/oidc' : '/api/auth/oidc/start', { method: state.value.linked ? 'DELETE' : 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ password: password.value, link: true, provider: selected.value }) })
    password.value = ''
    if (response.status === 401) { emit('expired'); return }
    if (response.status === 204) { window.location.assign('/login'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '无法开始 OIDC 登录'; return }
    window.location.assign(data.url)
  } catch { error.value = '无法开始 OIDC 登录' }
  finally { busy.value = false }
}
onMounted(() => { if (callbackError) toast(callbackError, 'error'); if (linkedNow) toast('OIDC 绑定成功。'); void load() })
function finishConfirmation(path: string) { window.location.assign(path) }
</script>
<template>
  <section class="account-password oidc-account">
    <h3>{{ t('第三方登录') }}</h3>
    <LoadingSkeleton v-if="!loaded && !error" label="正在加载…" :count="2" />

    <p v-if="error" class="form-error" role="alert"><button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <template v-if="loaded">
      <label class="binding-field"><span>{{ t('身份提供商') }}</span><select v-model="selected"><option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }}</option></select></label>
      <p>{{ state.linked ? t('已绑定 {name}', { name: state.name }) : state.enabled ? t('绑定后可使用 {name} 登录当前账号。', { name: state.name }) : t('站点尚未启用第三方登录。') }}</p>

      <form v-if="state.enabled || state.linked" class="password-form" @submit.prevent="submit">
        <fieldset :disabled="busy">
          <label class="binding-field"><span>{{ t('当前密码') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
          <button class="secondary-button" type="submit">{{ busy ? t('正在处理…') : state.linked ? t('解除绑定') : t('绑定 {name}', { name: state.name }) }}</button>
        </fieldset>
      </form>
    </template>
  </section>
  <OIDCConfirmation v-if="confirming" :csrf-token="csrfToken" @completed="finishConfirmation" />
</template>
