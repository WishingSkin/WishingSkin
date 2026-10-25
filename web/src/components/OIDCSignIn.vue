<script setup lang="ts">
import { ref } from 'vue'
import { t } from '../i18n'
defineProps<{ name: string }>()
const busy = ref(false), error = ref('')
async function start() {
  busy.value = true; error.value = ''
  try {
    const next = new URLSearchParams(window.location.search).get('next') || '/user'
    const response = await fetch('/api/auth/oidc/start', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ next }) })
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '无法开始 OIDC 登录'; return }
    window.location.assign(data.url)
  } catch { error.value = '无法开始 OIDC 登录' }
  finally { busy.value = false }
}
</script>
<template><div class="oidc-sign-in"><button class="secondary-button" type="button" :disabled="busy" @click="start">{{ busy ? t('正在连接…') : t('使用 {name} 登录', { name }) }}</button><p v-if="error" class="form-error" role="alert">{{ t(error) }}</p></div></template>
