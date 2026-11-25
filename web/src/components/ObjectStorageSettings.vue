<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'
import { settingsSnapshot } from '../settingsDraft'
import { useUnsavedGuard } from '../unsavedGuard'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: []; updated: [] }>()
const draft = ref({ endpoint: '', bucket: '', region: 'us-east-1', prefix: 'textures', accessKey: '', secretKey: '' })
const configured = ref(false), loaded = ref(false), busy = ref(false)
const error = feedbackRef('error'), message = feedbackRef('success')
const savedSnapshot = ref('')
const dirty = computed(() => loaded.value && savedSnapshot.value !== '' && settingsSnapshot(draft.value) !== savedSnapshot.value)
useUnsavedGuard(dirty, '/admin/plugins/wishingskin.object-storage')
async function load() {
  try { const r = await fetch('/api/admin/object-storage', { credentials: 'same-origin' }); if (r.status === 401) { emit('expired'); return }; if (!r.ok) throw new Error(); const data = await r.json(); draft.value = { ...draft.value, ...data.config }; configured.value = data.credentialsConfigured; savedSnapshot.value = settingsSnapshot(draft.value); loaded.value = true }
  catch { error.value = '暂时无法读取对象存储配置' }
}
async function save() {
  busy.value = true; error.value = ''; message.value = ''
  try { const r = await fetch('/api/admin/object-storage', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(draft.value) }); if (r.status === 401) { emit('expired'); return }; const data = await r.json(); if (!r.ok) { error.value = data.error || '暂时无法保存对象存储配置'; return }; draft.value.accessKey = ''; draft.value.secretKey = ''; configured.value = true; savedSnapshot.value = settingsSnapshot(draft.value); message.value = '对象存储配置已保存。'; emit('updated') }
  catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template><section class="settings-section"><h3>{{ t('S3 兼容对象存储') }}</h3><p>{{ t('启用后，新上传材质同步至对象存储。保留本地副本用于回退和备份，已有材质继续读取本地文件。') }}</p>
  <form class="settings-form" @submit.prevent="save"><fieldset :disabled="!loaded || busy"><label v-for="[key,label] in ([['endpoint','服务地址'],['bucket','存储桶'],['region','区域'],['prefix','对象前缀']] as const)" :key="key" class="binding-field"><span>{{ t(label) }}</span><input v-model="draft[key]" :required="key === 'endpoint' || key === 'bucket'" :placeholder="key === 'endpoint' ? 'https://s3.example.com' : ''" /></label>
    <label class="binding-field"><span>Access Key</span><input v-model="draft.accessKey" type="password" autocomplete="new-password" :required="!configured" :placeholder="configured ? t('留空保留已保存凭据') : ''" /></label><label class="binding-field"><span>Secret Key</span><input v-model="draft.secretKey" type="password" autocomplete="new-password" :required="!configured" :placeholder="configured ? t('留空保留已保存凭据') : ''" /></label>
    <button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存配置') }}</button></fieldset></form><p v-if="error" class="form-error" role="alert"><button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
</section></template>
