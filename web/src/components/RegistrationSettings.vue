<script setup lang="ts">
import { ref, watch } from 'vue'
import type { SiteAccess } from '../accountTypes'
import { t } from '../i18n'
const props = defineProps<{ site: SiteAccess; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const mode = ref(props.site.registration === 'closed' ? 'closed' : 'open')
const busy = ref(false)
const error = ref('')
const message = ref('')
watch(() => props.site.registration, value => { mode.value = value === 'closed' ? 'closed' : 'open' })
async function submit() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/registration', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ mode: mode.value }) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '操作未完成，请重试。'; return }
    message.value = '注册模式已保存。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
</script>

<template>
  <section class="account-password registration-settings" aria-labelledby="registration-settings-title">
    <h3 id="registration-settings-title">{{ t('注册') }}</h3>
    <p>{{ t('默认使用邮箱验证注册。邀请码要求在官方插件页单独设置。') }}</p>
    <form class="password-form" @submit.prevent="submit()"><label class="binding-field"><span>{{ t('注册模式') }}</span><select v-model="mode" :aria-label="t('注册模式')" :disabled="busy"><option value="closed">{{ t('关闭注册') }}</option><option value="open">{{ t('邮箱验证注册') }}</option></select></label><button class="primary-button" type="submit" :disabled="busy">{{ t('保存注册模式') }}</button></form>
    <p v-if="error" class="form-error" role="alert">{{ t(error) }}</p><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
  </section>
</template>
