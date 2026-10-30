<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const options = ref<{ enabled: boolean; required: boolean; migration?: { total: number; unused: number; used: number } } | null>(null)
const enabled = ref(false)
const required = ref(false)
const count = ref(10)
const codes = ref<string[]>([])
const loading = ref(false)
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
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
    const response = await fetch('/api/admin/plugins/invitations' + (generate ? '/codes' : ''), { method: generate ? 'POST' : 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(generate ? { count: count.value } : { enabled: enabled.value, required: required.value }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '操作未完成，请重试。'; return }
    if (generate) { codes.value = data.codes; message.value = '邀请码已生成，可随时从服务器重复导出。'; await load() }
    else { options.value = data; message.value = '插件设置已保存。'; emit('updated') }
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
async function download(status: 'unused' | 'all', format: 'txt' | 'csv') {
  error.value = ''; message.value = ''
  try {
    const response = await fetch(`/api/admin/plugins/invitations/export?status=${status}&format=${format}`, { credentials: 'same-origin', headers: { 'X-CSRF-Token': props.csrfToken } })
    if (!response.ok) {
      const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法导出邀请码'; return
    }
    const blob = await response.blob(); const url = URL.createObjectURL(blob); const link = document.createElement('a')
    link.href = url; link.download = `wishingskin-invitations-${status}.${format}`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
    message.value = '邀请码导出已开始。'
  } catch { error.value = '暂时无法导出邀请码' }
}
onMounted(load)
</script>
<template>
  <section class="settings-section invitation-plugin"><h3>{{ t('邀请码设置') }}</h3><p>{{ t('设置注册邀请码的使用规则。') }}</p>
    <p v-if="loading" role="status">{{ t('正在加载…') }}</p><p v-if="error" class="form-error" role="alert"><button v-if="!options && !loading" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <template v-if="options"><form class="settings-form" @submit.prevent="submit(false)"><fieldset :disabled="busy"><label class="settings-check"><input v-model="enabled" type="checkbox" />{{ t('启用邀请码插件') }}</label><label class="settings-check"><input v-model="required" type="checkbox" :disabled="!enabled" />{{ t('注册时必须填写邀请码') }}</label><button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存插件设置') }}</button></fieldset></form>
      <form v-if="options.enabled" class="settings-form plugin-generation" @submit.prevent="submit(true)"><fieldset :disabled="busy"><h3>{{ t('批量生成') }}</h3><div class="settings-grid"><label class="binding-field"><span>{{ t('生成数量') }}</span><input v-model.number="count" type="number" min="1" max="100" required /></label></div><p class="password-rule">{{ t('每个邀请码只能使用一次，永久有效。服务器保存加密值，可在此页重复导出。') }}</p><button class="secondary-button" type="submit">{{ t('生成邀请码') }}</button></fieldset></form>
      <div v-if="options.migration" class="invitation-migration"><h3>{{ t('迁移统计') }}</h3><p>{{ t('共 {total} 个，未使用 {unused} 个，已使用 {used} 个。', options.migration) }}</p></div>
      <div v-if="options.enabled" class="invitation-export"><h3>{{ t('重复导出') }}</h3><div class="settings-actions"><button v-for="format in ['txt', 'csv']" :key="`unused-${format}`" class="secondary-button" type="button" @click="download('unused', format as 'txt' | 'csv')">{{ t(`导出未使用 ${format.toUpperCase()}`) }}</button><button v-for="format in ['txt', 'csv']" :key="`all-${format}`" class="secondary-button" type="button" @click="download('all', format as 'txt' | 'csv')">{{ t(`导出全部 ${format.toUpperCase()}`) }}</button></div></div>
      <div v-if="codes.length" class="invitation-result"><label class="binding-field"><span>{{ t('本次生成的邀请码') }}</span><textarea :value="codes.join('\n')" readonly rows="6" :aria-label="t('生成的邀请码')"></textarea></label></div>
    </template>
  </section>
</template>
