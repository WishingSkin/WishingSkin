<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const options = ref<{ enabled: boolean; required: boolean } | null>(null)
const enabled = ref(false)
const required = ref(false)
const count = ref(10)
const uses = ref(1)
const hours = ref(24)
const codes = ref<string[]>([])
const expiresAt = ref(0)
const codeUses = ref(0)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const message = ref('')
async function load() {
  loading.value = true; error.value = ''
  try {
    const response = await fetch('/api/admin/plugins/invitations', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法读取插件设置'; return }
    options.value = data; enabled.value = data.enabled; required.value = data.required
  } catch { error.value = '暂时无法读取插件设置' }
  finally { loading.value = false }
}
async function submit(generate: boolean) {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/plugins/invitations' + (generate ? '/codes' : ''), { method: generate ? 'POST' : 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(generate ? { count: count.value, uses: uses.value, hours: hours.value } : { enabled: enabled.value, required: required.value }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '操作未完成，请重试。'; return }
    if (generate) { codes.value = data.codes; expiresAt.value = data.expiresAt; codeUses.value = data.remainingUses; message.value = '邀请码已生成，请立即保存或导出。' }
    else { options.value = data; message.value = '插件设置已保存。'; emit('updated') }
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
function download(format: 'txt' | 'csv') {
  const content = format === 'txt' ? codes.value.join('\r\n') + '\r\n' : '\uFEFFcode,remaining_uses,expires_at\r\n' + codes.value.map(code => [code, codeUses.value, new Date(expiresAt.value * 1000).toISOString()].join(',')).join('\r\n') + '\r\n'
  const url = URL.createObjectURL(new Blob([content], { type: format === 'txt' ? 'text/plain;charset=utf-8' : 'text/csv;charset=utf-8' }))
  const link = document.createElement('a'); link.href = url; link.download = 'wishingskin-invitations.' + format; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
}
onMounted(load)
</script>
<template>
  <section class="settings-section invitation-plugin"><h3>{{ t('邀请码设置') }}</h3><p>{{ t('开启后可要求新账号提供邀请码，邮箱验证与人机验证仍然必需。关闭插件不会删除已生成的码。') }}</p>
    <p v-if="loading" role="status">{{ t('正在加载…') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }} <button v-if="!options && !loading" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <template v-if="options"><form class="settings-form" @submit.prevent="submit(false)"><fieldset :disabled="busy"><label class="settings-check"><input v-model="enabled" type="checkbox" />{{ t('启用邀请码插件') }}</label><label class="settings-check"><input v-model="required" type="checkbox" :disabled="!enabled" />{{ t('注册时必须填写邀请码') }}</label><button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存插件设置') }}</button></fieldset></form>
      <form v-if="options.enabled" class="settings-form plugin-generation" @submit.prevent="submit(true)"><fieldset :disabled="busy"><h3>{{ t('批量生成') }}</h3><div class="settings-grid"><label class="binding-field"><span>{{ t('生成数量') }}</span><input v-model.number="count" type="number" min="1" max="100" required /></label><label class="binding-field"><span>{{ t('每码可使用次数') }}</span><input v-model.number="uses" type="number" min="1" max="1000" required /></label><label class="binding-field"><span>{{ t('有效小时数') }}</span><input v-model.number="hours" type="number" min="1" max="8760" required /></label></div><button class="secondary-button" type="submit">{{ t('生成邀请码') }}</button></fieldset></form>
      <div v-if="codes.length" class="invitation-result"><label class="binding-field"><span>{{ t('邀请码（仅本次显示，请立即保存）') }}</span><textarea :value="codes.join('\n')" readonly rows="6" :aria-label="t('生成的邀请码')"></textarea></label><div class="settings-actions"><button class="secondary-button" type="button" @click="download('txt')">{{ t('导出 TXT') }}</button><button class="secondary-button" type="button" @click="download('csv')">{{ t('导出 CSV') }}</button></div><p class="password-rule">{{ t('服务器只保存邀请码哈希，离开页面后不能重新导出原码。再次生成不会撤销旧码。') }}</p></div>
    </template><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
  </section>
</template>
