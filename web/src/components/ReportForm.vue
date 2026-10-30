<script setup lang="ts">
import { feedbackRef } from '../feedback'
import AppModal from './AppModal.vue'
import { computed, ref, watch } from 'vue'
import { t } from '../i18n'

const props = defineProps<{ recordId: string; name: string; csrfToken: string }>()
const emit = defineEmits<{ expired: []; login: [] }>()
const open = ref(false)
const reason = ref('')
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
const length = computed(() => Array.from(reason.value.trim()).length)
watch(() => props.recordId, () => { open.value = false; reason.value = ''; error.value = ''; message.value = '' })
async function submit() {
  if (busy.value) return
  if (!length.value || length.value > 1000) { error.value = '举报原因须为 1–1000 个字符'; return }
  const record = props.recordId
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/reports', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ recordId: record, reason: reason.value.trim() }) })
    if (response.status === 401) { emit('expired'); return }
    if (record !== props.recordId) return
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '举报未提交，请重试'; return }
    reason.value = ''; open.value = false; message.value = '举报已提交，可在账号页查看处理进度。'
  } catch { if (record === props.recordId) error.value = '无法确认提交结果，请到账号页检查后重试。' }
  finally { busy.value = false }
}
</script>

<template>
  <div class="report-form">
    <button v-if="!open" class="text-button" type="button" :disabled="busy" @click="csrfToken ? open = true : emit('login')">{{ csrfToken ? t('举报这份材质') : t('登录后举报') }}</button>
    <AppModal v-if="open" :title="t('举报材质')" :busy="busy" @close="open = false"><form class="report-fields" @submit.prevent="submit">
      <p>{{ t('举报材质：{name}', { name }) }}</p>
      <label class="binding-field"><span>{{ t('举报原因') }} <small>{{ length }}/1000</small></span><textarea v-model="reason" rows="4" :disabled="busy" required /></label>

      <div class="admin-actions"><button class="primary-button" type="submit" :disabled="busy">{{ busy ? t('正在提交…') : t('提交举报') }}</button><button class="secondary-button" type="button" :disabled="busy" @click="open = false">{{ t('取消') }}</button></div>
    </form></AppModal>

  </div>
</template>
