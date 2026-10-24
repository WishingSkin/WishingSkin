<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
import TurnstileChallenge from './TurnstileChallenge.vue'
interface Services { smtpEnabled: boolean; address: string; from: string; username: string; tlsMode: string; passwordConfigured: boolean; turnstileEnabled: boolean; siteKey: string; secretConfigured: boolean; source: string; testRecipient: string; editable?: boolean }
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const draft = ref<Services | null>(null)
const password = ref('')
const secret = ref('')
const clearPassword = ref(false)
const clearSecret = ref(false)
const loading = ref(false)
const busy = ref(false)
const testBusy = ref(false)
const turnstileTestBusy = ref(false)
const turnstileTestToken = ref('')
const turnstileChallenge = ref<InstanceType<typeof TurnstileChallenge> | null>(null)
const error = ref('')
const message = ref('')
async function load() {
  loading.value = true; error.value = ''
  try {
    const response = await fetch('/api/admin/services', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json() as Services & { error?: string }
    if (!response.ok) { error.value = data.error || '暂时无法读取服务设置'; return }
    draft.value = { ...data, testRecipient: data.testRecipient || '' }
  } catch { error.value = '暂时无法读取服务设置' }
  finally { loading.value = false }
}
async function save() {
  if (!draft.value || busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/services', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ ...draft.value, password: password.value, secret: secret.value, clearPassword: clearPassword.value, clearSecret: clearSecret.value }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存服务设置'; return }
    draft.value = { ...data, editable: true }; password.value = ''; secret.value = ''; clearPassword.value = false; clearSecret.value = false
    message.value = '服务设置已保存并生效。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
async function sendTest() {
  if (testBusy.value || busy.value) return
  testBusy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/services/test', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ recipient: draft.value?.testRecipient || '' }) })
    const data = await response.json().catch(() => ({})) as { error?: string; message?: string; recipient?: string }
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { error.value = data.error || '测试邮件发送失败，请检查 SMTP 配置'; return }
    message.value = `${data.message || '测试邮件已提交'}：${data.recipient || draft.value?.testRecipient || ''}`
  } catch { error.value = '无法确认测试邮件结果，请检查管理员邮箱。' }
  finally { testBusy.value = false }
}
async function testTurnstile() {
  if (turnstileTestBusy.value || busy.value || !turnstileTestToken.value) return
  turnstileTestBusy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/services/turnstile-test', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ turnstileToken: turnstileTestToken.value }) })
    const data = await response.json().catch(() => ({})) as { error?: string; message?: string }
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { error.value = data.error || 'Turnstile 测试失败，请重试'; return }
    message.value = data.message || 'Turnstile 测试通过'
  } catch { error.value = '无法确认 Turnstile 测试结果，请重试。' }
  finally { turnstileTestBusy.value = false; turnstileTestToken.value = ''; void turnstileChallenge.value?.reset() }
}
onMounted(load)
</script>
<template>
  <section class="settings-section"><h3>{{ t('邮件与验证') }}</h3><p>{{ t('注册始终需要验证邮箱和完成人机验证。请先配置邮件服务与 Turnstile。') }}</p>
    <p v-if="loading" role="status">{{ t('正在加载…') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }} <button v-if="!draft && !loading" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <form v-if="draft" class="settings-form" @submit.prevent="save"><p class="password-rule">{{ draft.source === 'database' ? t('当前使用后台保存的配置。') : t('当前使用启动时的配置，保存后将以后台配置为准。') }}</p><p v-if="!draft.editable" class="notice">{{ t('本地设置密钥尚不可用') }}</p><fieldset :disabled="busy || !draft.editable">
      <h4>SMTP</h4><label class="settings-check"><input v-model="draft.smtpEnabled" type="checkbox" />{{ t('启用邮件服务') }}</label>
      <label class="binding-field"><span>{{ t('SMTP 服务器与端口') }}</span><input v-model="draft.address" placeholder="smtp.example.com:587" :required="draft.smtpEnabled" /></label>
      <label class="binding-field"><span>{{ t('发件人邮箱') }}</span><input v-model="draft.from" type="email" :required="draft.smtpEnabled" /></label>
      <label class="binding-field"><span>{{ t('加密方式') }}</span><select v-model="draft.tlsMode"><option value="starttls">STARTTLS</option><option value="tls">TLS</option><option value="local">{{ t('本机明文（仅回环地址）') }}</option></select></label>
      <label class="binding-field"><span>{{ t('SMTP 用户名') }}</span><input v-model="draft.username" autocomplete="off" /></label>
      <label class="binding-field"><span>{{ t('SMTP 密码') }} <small>{{ draft.passwordConfigured ? t('已配置') : t('未配置') }}</small></span><input v-model="password" type="password" autocomplete="new-password" :disabled="clearPassword" /></label>
      <label class="settings-check"><input v-model="clearPassword" type="checkbox" />{{ t('清除已保存的 SMTP 密码') }}</label>
      <h4>Cloudflare Turnstile</h4><label class="settings-check"><input v-model="draft.turnstileEnabled" type="checkbox" />{{ t('启用人机验证') }}</label>
      <label class="binding-field"><span>Site key</span><input v-model="draft.siteKey" :required="draft.turnstileEnabled" maxlength="256" autocomplete="off" /></label>
      <label class="binding-field"><span>Secret key <small>{{ draft.secretConfigured ? t('已配置') : t('未配置') }}</small></span><input v-model="secret" type="password" maxlength="256" autocomplete="new-password" :disabled="clearSecret" /></label>
      <label class="settings-check"><input v-model="clearSecret" type="checkbox" />{{ t('清除已保存的 Turnstile Secret') }}</label>
      <p class="password-rule">{{ t('密码与 Secret 留空会保留现有值；勾选清除后才会移除。启用中的服务必须配置完整。') }}</p>
      <div class="mail-test-settings"><label class="binding-field"><span>{{ t('SMTP 测试收件地址') }}</span><input v-model="draft.testRecipient" type="email" :placeholder="t('默认使用当前管理员邮箱')" /></label><p class="password-rule">{{ t('测试邮件只发送到这里，不会修改站点发件人配置。') }}</p></div>
      <div v-if="draft.turnstileEnabled" class="turnstile-test-settings"><h5>{{ t('测试 Turnstile') }}</h5><p class="password-rule">{{ t('使用当前已保存配置验证 hostname 和 action，不会计入登录失败次数。') }}</p><TurnstileChallenge ref="turnstileChallenge" :site-key="draft.siteKey" action="admin-test" :dark="false" @token="turnstileTestToken = $event" /><button class="secondary-button" type="button" :disabled="turnstileTestBusy || busy || !turnstileTestToken" @click="testTurnstile">{{ turnstileTestBusy ? t('正在测试 Turnstile…') : t('测试 Turnstile') }}</button></div>
      <div class="settings-actions"><button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存邮件与验证设置') }}</button><button class="secondary-button" type="button" :disabled="testBusy || busy || !draft.smtpEnabled" @click="sendTest">{{ testBusy ? t('正在发送测试邮件…') : t('发送测试邮件') }}</button></div>
    </fieldset></form><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
  </section>
</template>
