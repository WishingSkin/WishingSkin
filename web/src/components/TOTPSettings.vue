<script setup lang="ts">
import { feedbackRef, confirmAction } from '../feedback'
import AppModal from './AppModal.vue'
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { t } from '../i18n'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const enabled = ref(false), loaded = ref(false), busy = ref(false)
const password = ref(''), code = ref(''), secret = ref(''), error = feedbackRef('error'), message = feedbackRef('success')
const qrCode = ref('')
const recovery = ref<string[]>([])
async function load() {
  try { const r = await fetch('/api/account/totp', { credentials: 'same-origin' }); if (r.status === 401) { emit('expired'); return }; if (!r.ok) throw new Error(); enabled.value = (await r.json()).enabled; loaded.value = true }
  catch { error.value = '暂时无法读取双重验证设置' }
}
async function submit(action: 'setup' | 'enable' | 'disable' | 'recovery') {
  if (busy.value) return
  if ((action === 'disable' || action === 'recovery') && !await confirmAction({ title: action === 'disable' ? '停用双重验证' : '重新生成恢复码', message: t(action === 'disable' ? '停用当前账号的双重验证？' : '重新生成恢复码？现有恢复码将失效。'), danger: true })) return
  busy.value = true; error.value = ''; message.value = ''; recovery.value = []
  try {
    const r = await fetch(`/api/account/totp/${action}`, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ currentPassword: password.value, code: code.value }) })
    if (r.status === 401) { emit('expired'); return }
    const data = await r.json(); if (!r.ok) { error.value = data.error || '暂时无法修改双重验证'; return }
    if (action === 'setup') { secret.value = data.secret; qrCode.value = data.qrCode || '' }
    else { enabled.value = data.enabled; secret.value = ''; qrCode.value = ''; recovery.value = data.recoveryCodes || []; message.value = action === 'disable' ? '双重验证已停用。' : '双重验证设置已保存。' }
    code.value = ''; password.value = ''
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
onMounted(load)
onBeforeUnmount(() => { secret.value = ''; qrCode.value = ''; recovery.value = []; password.value = ''; code.value = '' })
</script>
<template>
  <section class="account-password totp-settings"><h3>{{ t('双重验证 (TOTP)') }}</h3><p>{{ t('使用验证器保护网页登录。Minecraft 游戏登录仍使用本站密码。') }}</p>
    <p v-if="!loaded" role="status"><button class="text-button" type="button" @click="load">{{ t('重新加载') }}</button></p>
    <form v-if="loaded" class="settings-form" @submit.prevent="submit(secret ? 'enable' : enabled ? 'disable' : 'setup')"><fieldset :disabled="busy">
      <p class="password-rule">{{ enabled ? t('已启用') : t('未启用') }}</p>
      <template v-if="secret"><img v-if="qrCode" class="totp-qr" :src="qrCode" :alt="t('验证器设置二维码')" width="240" height="240" /><p class="password-rule">{{ t('使用验证器扫描二维码或输入密钥，再填写六位验证码。密钥 10 分钟后失效。') }}</p><code class="totp-secret">{{ secret }}</code></template>
      <label v-else class="binding-field"><span>{{ t('当前密码') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
      <label v-if="enabled || secret" class="binding-field"><span>{{ t('验证码或恢复码') }}</span><input v-model="code" autocomplete="one-time-code" maxlength="32" required /></label>
      <div class="settings-actions"><button class="primary-button" type="submit">{{ t(secret ? '确认启用' : enabled ? '停用双重验证' : '设置双重验证') }}</button><button v-if="enabled" class="secondary-button" type="button" :disabled="!password || !code" @click="submit('recovery')">{{ t('重新生成恢复码') }}</button></div>
    </fieldset></form>
    <AppModal v-if="recovery.length" :title="t('恢复码')" @close="recovery = []"><div><p class="password-rule">{{ t('恢复码仅显示一次，每个只能使用一次。请离线保存。重新生成后旧码失效。') }}</p><ul class="totp-codes"><li v-for="item in recovery" :key="item"><code>{{ item }}</code></li></ul><button class="secondary-button" type="button" @click="recovery = []">{{ t('我已保存') }}</button></div></AppModal>

  </section>
</template>
