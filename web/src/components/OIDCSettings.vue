<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { feedbackRef } from '../feedback'
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
function blank() { return { enabled: false, name: 'OIDC', issuer: '', clientId: '', clientSecret: '', secretConfigured: false, clearSecret: false, redirectUri: '', id: '' } }
const providers = ref<ReturnType<typeof blank>[]>([])
const draft = ref(blank())
const selected = ref('')
function select(id: string) { selected.value=id; draft.value=providers.value.find(p=>p.id===id)! }
function add() { const id='provider-'+crypto.randomUUID().slice(0,8); providers.value.push({ ...blank(), id }); select(id) }
function remove() { providers.value=providers.value.filter(p=>p.id!==selected.value); select('') }
const loaded = ref(false), busy = ref(false), error = feedbackRef('error'), message = feedbackRef('success')
async function load() {
  error.value = ''
  try {
    const response = await fetch('/api/admin/oidc/providers', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法读取 OIDC 设置'; return }
    providers.value = data.providers.map((p: ReturnType<typeof blank>) => ({ ...blank(), ...p, clientSecret:'', clearSecret:false })); select(''); loaded.value=true
  } catch { error.value = '暂时无法读取 OIDC 设置' }
}
async function save() {
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/oidc/providers', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ providers: providers.value }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存 OIDC 设置'; return }
    providers.value=data.providers.map((p: ReturnType<typeof blank>)=>({ ...blank(), ...p, clientSecret:'',clearSecret:false })); select(selected.value); message.value = 'OIDC 设置已保存。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section class="settings-section oidc-settings"><h3>{{ t('OIDC 登录') }}</h3><p class="password-rule">{{ t('连接身份提供商。新账号仍遵守本站注册和邮箱验证规则。') }}</p>
    <LoadingSkeleton v-if="!loaded && !error" label="正在加载…" :count="2" /><p v-if="error" class="form-error" role="alert"><button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <div v-if="loaded" class="settings-actions"><label class="binding-field"><span>{{ t('身份提供商') }}</span><select :value="selected" @change="select(($event.target as HTMLSelectElement).value)"><option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }}{{ p.id ? '' : ' (' + t('默认') + ')' }}</option></select></label><button class="secondary-button" type="button" :disabled="busy || providers.length >= 12" @click="add">{{ t('添加提供商') }}</button><button v-if="selected" class="text-button" type="button" :disabled="busy" @click="remove">{{ t('移除提供商') }}</button></div><form v-if="loaded" class="settings-form" @submit.prevent="save"><fieldset :disabled="busy">
      <label class="checkbox-label"><input v-model="draft.enabled" type="checkbox" /> {{ t('启用 OIDC 登录') }}</label>
      <label class="binding-field"><span>{{ t('登录按钮名称') }}</span><input v-model="draft.name" maxlength="64" required /></label>
      <label class="binding-field"><span>Issuer</span><input v-model="draft.issuer" type="url" :required="draft.enabled" placeholder="https://identity.example.com/oidc" /></label>
      <label class="binding-field"><span>Client ID</span><input v-model="draft.clientId" maxlength="256" :required="draft.enabled" autocomplete="off" /></label>
      <label class="binding-field"><span>Client Secret</span><input v-model="draft.clientSecret" type="password" maxlength="4096" autocomplete="new-password" :placeholder="draft.secretConfigured ? t('已配置，留空保留') : t('尚未配置')" /></label>
      <label v-if="draft.secretConfigured" class="checkbox-label"><input v-model="draft.clearSecret" type="checkbox" /> {{ t('清除已保存的密钥') }}</label>
      <dl class="oidc-redirect"><dt>{{ t('回调地址') }}</dt><dd>{{ draft.redirectUri }}</dd></dl>
      <button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存 OIDC 设置') }}</button>
    </fieldset></form>
  </section>
</template>
