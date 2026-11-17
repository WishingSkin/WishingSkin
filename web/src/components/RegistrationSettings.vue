<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { ref, watch } from 'vue'
import type { SiteAccess } from '../accountTypes'
import { t } from '../i18n'
const props = defineProps<{ site: SiteAccess; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const mode = ref(props.site.registration === 'closed' ? 'closed' : 'open')
const manualEmailVerification = ref(props.site.manualEmailVerification === true)
watch(() => props.site.manualEmailVerification, value => { manualEmailVerification.value = value === true })
const randomUserIds = ref(props.site.randomUserIds === true)
const emailDomains = ref(props.site.emailDomains.join('\n'))
const blockedEmailDomains = ref((props.site.blockedEmailDomains || []).join('\n'))
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
watch(() => props.site.registration, value => { mode.value = value === 'closed' ? 'closed' : 'open' })
watch(() => props.site.emailDomains, value => { emailDomains.value = value.join('\n') })
watch(() => props.site.blockedEmailDomains, value => { blockedEmailDomains.value = (value || []).join('\n') })
watch(() => props.site.randomUserIds, value => { randomUserIds.value = value === true })
async function submit() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const domains = emailDomains.value.split(/[\n,]/).map(value => value.trim()).filter(Boolean)
    const blocked = blockedEmailDomains.value.split(/[\n,]/).map(value => value.trim()).filter(Boolean)
    const response = await fetch('/api/admin/registration', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ mode: mode.value, manualEmailVerification: manualEmailVerification.value, emailDomains: domains, blockedEmailDomains: blocked, randomUserIds: randomUserIds.value }) })
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
    <p>{{ t('选择注册开放状态与邮箱验证方式。') }}</p>
    <form class="password-form registration-form" @submit.prevent="submit()">
      <label class="binding-field">
        <span>{{ t('注册模式') }}</span>
        <select v-model="mode" :aria-label="t('注册模式')" :disabled="busy">
          <option value="closed">{{ t('关闭注册') }}</option>
          <option value="open">{{ t('开放注册') }}</option>
        </select>
      </label>
      <label class="binding-field"><span>{{ t('邮箱验证方式') }}</span><select v-model="manualEmailVerification" :disabled="busy"><option :value="false">{{ t('邮件验证') }}</option><option :value="true">{{ t('管理员验证') }}</option></select></label>
      <label class="binding-field">
        <span>{{ t('允许的邮箱域名') }}</span>
        <textarea v-model="emailDomains" rows="4" maxlength="5000" :placeholder="t('每行或逗号分隔，例如 example.com')" aria-describedby="registration-domains-hint" :disabled="busy" />
        <small id="registration-domains-hint">{{ t('留空表示不限制；只影响新注册。') }}</small>
      </label>
      <label class="binding-field">
        <span>{{ t('禁止的邮箱域名') }}</span>
        <textarea v-model="blockedEmailDomains" rows="3" maxlength="5000" :placeholder="t('每行或逗号分隔，例如 example.com')" aria-describedby="registration-blocked-hint" :disabled="busy" />
        <small id="registration-blocked-hint">{{ t('按完整域名匹配；黑名单优先于允许列表。') }}</small>
      </label>
      <label class="registration-id-option">
        <input v-model="randomUserIds" type="checkbox" :disabled="busy" aria-describedby="registration-id-hint" />
        <span><strong>{{ t('使用 10 位随机用户 ID') }}</strong><small id="registration-id-hint">{{ t('默认使用递增序号，仅影响新账号。') }}</small></span>
      </label>
      <button class="primary-button" type="submit" :disabled="busy">{{ t('保存注册设置') }}</button>
    </form>

  </section>
</template>

<style scoped>
.registration-form{display:grid;gap:20px}
.registration-form .binding-field,.registration-form .primary-button{margin-top:0}
.registration-form small{display:block;font-size:var(--font-small);line-height:1.7;color:var(--muted);margin-top:7px}
.registration-id-option{display:flex;align-items:flex-start;gap:10px;font-size:var(--font-body);line-height:1.7;cursor:pointer}
.registration-id-option input{flex-shrink:0;width:16px;height:16px;margin:3px 0 0;accent-color:var(--accent)}
.registration-id-option span{min-width:0}
.registration-id-option strong{font-weight:600}
.registration-id-option small{margin-top:3px}
.registration-form .primary-button{justify-self:start}
</style>
