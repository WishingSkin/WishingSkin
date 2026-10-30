<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed, onMounted, ref, watch } from 'vue'
import type { AccessMode, SiteAccess } from '../accountTypes'
import { t } from '../i18n'
import { validNewPassword } from '../passwordPolicy'
import TurnstileChallenge from './TurnstileChallenge.vue'
const props = defineProps<{ mode: AccessMode; token: string; site: SiteAccess; email: string; dark: boolean }>()
const emit = defineEmits<{ back: []; completed: [message: string] }>()
const address = ref(props.email)
const nickname = ref('')
const password = ref('')
const confirmation = ref('')
const invitation = ref('')
const purpose = ref<'verify' | 'reset'>('verify')
const challengeToken = ref('')
const challenge = ref<InstanceType<typeof TurnstileChallenge> | null>(null)
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
const oidcRegistration = new URLSearchParams(window.location.search).get('oidc') === '1'
const oidcReady = ref(!oidcRegistration)
onMounted(async () => {
  if (!oidcRegistration) return
  try {
    const response = await fetch('/api/auth/oidc/pending', { credentials: 'same-origin' })
    const data = await response.json()
    if (!response.ok) { error.value = data.error || 'OIDC 注册已失效，请重新登录'; return }
    address.value = data.email || ''; oidcReady.value = true
  } catch { error.value = 'OIDC 注册已失效，请重新登录' }
})
const title = computed(() => ({ register: '创建账号', mail: '账号邮件', verify: '确认邮箱', reset: '设置新密码' })[props.mode])
watch(() => props.mode, () => { error.value = ''; message.value = '' })
async function submit() {
  if (busy.value || !oidcReady.value) return
  error.value = ''; message.value = ''
  if (props.mode === 'register' || props.mode === 'reset') {
    if (!validNewPassword(password.value)) { error.value = '新密码须为 8–128 个字符，且同时包含英文字母、数字和符号'; return }
    if (password.value !== confirmation.value) { error.value = '两次输入的新密码不一致。'; return }
  }
  busy.value = true
  const body = props.mode === 'register' ? { email: address.value.trim(), nickname: nickname.value.trim(), password: password.value, invitation: invitation.value.trim(), turnstileToken: challengeToken.value, oidc: oidcRegistration }
    : props.mode === 'mail' ? { email: address.value.trim(), purpose: purpose.value }
    : props.mode === 'verify' ? { token: props.token } : { token: props.token, newPassword: password.value }
  const endpoint = props.mode === 'mail' ? 'email' : props.mode
  try {
    const response = await fetch(`/api/auth/${endpoint}`, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '操作未完成，请重试。'; return }
    if (props.mode === 'verify' || props.mode === 'reset') {
      password.value = ''; confirmation.value = ''
      emit('completed', props.mode === 'verify' ? '邮箱已验证，请登录。' : '密码已重设，所有网页会话与游戏令牌已撤销。请用新密码登录。')
    } else {
      message.value = props.mode === 'register' ? (props.site.manualEmailVerification ? '账号已创建，请等待管理员完成邮箱验证。' : '账号已创建，验证邮件已提交发送。请验证邮箱后登录。') : '如有符合条件的账号，邮件已提交发送，请检查邮箱。'
      if (props.mode === 'register') { password.value = ''; confirmation.value = '' }
    }
  } catch { error.value = '无法确认操作结果，请检查邮箱或稍后重试。' }
  finally { busy.value = false; if (props.mode === 'register') { challengeToken.value = ''; void challenge.value?.reset() } }
}
</script>

<template>
  <section class="login-panel access-panel" aria-labelledby="access-title">
    <div class="login-heading"><h2 id="access-title">{{ t(title) }}</h2><p>{{ mode === 'register' ? t(site.manualEmailVerification ? '创建账号后由管理员完成邮箱验证。' : '验证邮箱后即可使用工作台和游戏认证。') : mode === 'verify' ? t('确认后完成邮箱验证。') : mode === 'reset' ? t('重设后需要重新登录网页和启动器。') : t('选择邮件用途，输入账号邮箱。') }}</p></div>
    <p v-if="mode === 'register' && site.registration === 'closed'" class="notice" role="status">{{ t('注册当前已关闭') }}</p>
    <form v-else class="access-form" @submit.prevent="submit">
      <fieldset :disabled="busy">
        <template v-if="mode === 'register' || mode === 'mail'">
          <label for="access-email">{{ t('邮箱') }}</label><input id="access-email" v-model="address" type="email" autocomplete="email" maxlength="254" required />
        </template>
        <template v-if="mode === 'register'">
          <label for="access-nickname">{{ t('昵称') }}</label><input id="access-nickname" v-model="nickname" autocomplete="nickname" />
          <p v-if="site.emailDomains.length" class="password-rule">{{ t('仅允许这些邮箱域名注册：') }}{{ site.emailDomains.join('、') }}</p>
          <template v-if="site.registration === 'invitation'"><label for="access-invitation">{{ t('邀请码') }}</label><input id="access-invitation" v-model="invitation" autocomplete="off" required /></template>
        </template>
        <template v-if="mode === 'mail'">
          <label for="mail-purpose">{{ t('邮件用途') }}</label><select id="mail-purpose" v-model="purpose"><option value="verify">{{ t('验证邮箱') }}</option><option value="reset">{{ t('找回密码') }}</option></select>
          <p class="password-rule">{{ purpose === 'verify' ? t('验证链接 24 小时内有效。') : t('找回链接 30 分钟内有效。') }} {{ t('成功重发会撤销同用途旧链接，请使用最新邮件。') }}</p>
        </template>
        <template v-if="mode === 'register' || mode === 'reset'">
          <p v-if="mode === 'register' && oidcRegistration" class="password-rule">{{ t('设置本站密码，用于 Minecraft 登录和账号恢复。') }}</p>
          <label for="access-password">{{ t('新密码') }}</label><input id="access-password" v-model="password" type="password" autocomplete="new-password" required aria-describedby="access-password-rule" />
          <p id="access-password-rule" class="password-rule">{{ t('新密码须为 8–128 个字符，且同时包含英文字母、数字和符号') }}</p>
          <label for="access-confirm">{{ t('确认新密码') }}</label><input id="access-confirm" v-model="confirmation" type="password" autocomplete="new-password" required />
        </template>
      </fieldset>
      <TurnstileChallenge v-if="mode === 'register'" ref="challenge" :site-key="site.turnstileSiteKey" action="register" :dark="dark" @token="challengeToken = $event" />
      <p v-if="mode === 'mail' && !site.mailEnabled" class="notice">{{ t('邮件服务尚未配置，请联系管理员') }}</p>


      <button class="primary-button login-button" type="submit" :disabled="busy || !oidcReady || (mode === 'register' && (!challengeToken || (!site.mailEnabled && !site.manualEmailVerification))) || (mode === 'mail' && !site.mailEnabled)">{{ busy ? t('正在处理…') : mode === 'register' ? t(site.manualEmailVerification ? '创建账号' : '创建账号并发送验证邮件') : mode === 'mail' ? t('发送邮件') : mode === 'verify' ? t('确认验证邮箱') : t('保存新密码') }}</button>
    </form>
    <button class="secondary-button browse-library" type="button" :disabled="busy" @click="emit('back')">{{ t('返回') }}</button>
  </section>
</template>
