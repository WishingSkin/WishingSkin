<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppIcon from './AppIcon.vue'
import { feedbackRef } from '../feedback'
import { computed, onMounted, ref, watch } from 'vue'
import { t } from '../i18n'
type Purpose = 'verify' | 'reset' | 'test'
interface MailCopy { subject: string; title: string; body: string; button: string }
interface MailTemplate { brandName: string; logoUrl: string; accentColor: string; backgroundColor: string; footer: string; verify: MailCopy; reset: MailCopy; test: MailCopy }
const props = defineProps<{ csrfToken: string; recipient: string; smtpEnabled: boolean }>()
const emit = defineEmits<{ expired: [] }>()
const draft = ref<MailTemplate | null>(null)
const saved = ref('')
const purpose = ref<Purpose>('verify')
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
const preview = ref<{ subject: string; text: string; html: string } | null>(null)
const dirty = computed(() => JSON.stringify(draft.value) !== saved.value)
const titles: Record<Purpose, string> = { verify: '验证邮箱', reset: '找回密码', test: 'SMTP 测试' }
async function request(path: string, method = 'GET', body?: unknown) {
  const response = await fetch(path, { method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: body === undefined ? undefined : JSON.stringify(body) })
  if (response.status === 401) { emit('expired'); throw new Error('登录已过期，请重新登录。') }
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || '操作未完成，请重试。')
  return data
}
async function updatePreview() {
  if (!draft.value) return
  preview.value = await request('/api/admin/email-template/preview', 'POST', { template: draft.value, purpose: purpose.value })
}
async function load() {
  busy.value = true; error.value = ''
  try {
    draft.value = await request('/api/admin/email-template')
    saved.value = JSON.stringify(draft.value)
    await updatePreview()
  } catch (e) { error.value = e instanceof Error ? e.message : '暂时无法读取邮件样式' }
  finally { busy.value = false }
}
async function perform(action: 'save' | 'preview' | 'sample') {
  if (!draft.value || busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    if (action === 'save') {
      draft.value = await request('/api/admin/email-template', 'PATCH', draft.value)
      saved.value = JSON.stringify(draft.value)
      message.value = '邮件样式已保存'
    }
    if (action === 'sample') {
      const data = await request('/api/admin/services/test', 'POST', { recipient: props.recipient, purpose: purpose.value })
      message.value = t('测试邮件已提交到 {email}', { email: data.recipient })
    } else await updatePreview()
  } catch (e) { error.value = e instanceof Error ? e.message : '操作未完成，请重试。' }
  finally { busy.value = false }
}
watch(purpose, () => { if (!busy.value) void perform('preview') })
onMounted(load)
</script>

<template>
  <section class="settings-section email-template-settings" aria-labelledby="email-template-heading">
    <h4 id="email-template-heading">{{ t('邮件样式') }}</h4>
    <LoadingSkeleton v-if="!draft && busy" label="正在加载…" :count="2" />
    <p v-if="error" class="form-error" role="alert"><button v-if="!draft" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <form v-if="draft" class="email-template-editor" @submit.prevent="perform('save')">
      <fieldset :disabled="busy" class="settings-form">
        <div class="email-brand-fields">
          <label class="binding-field"><span>{{ t('邮件品牌名') }}</span><input v-model="draft.brandName" maxlength="64" required /></label>
          <label class="binding-field"><span>{{ t('邮件 Logo 地址') }}</span><input v-model="draft.logoUrl" type="url" maxlength="2048" placeholder="https://example.com/logo.png" /></label>
          <label class="binding-field"><span>{{ t('邮件主题色') }}</span><input v-model="draft.accentColor" type="color" /></label>
          <label class="binding-field"><span>{{ t('邮件背景色') }}</span><input v-model="draft.backgroundColor" type="color" /></label>
        </div>
        <label class="binding-field"><span>{{ t('邮件页脚') }}</span><textarea v-model="draft.footer" rows="2" maxlength="500"></textarea></label>
        <div class="settings-tabs" role="group" :aria-label="t('邮件用途')">
          <button v-for="kind in (['verify', 'reset', 'test'] as const)" :key="kind" class="secondary-button" :class="{ active: purpose === kind }" type="button" :aria-pressed="purpose === kind" @click="purpose = kind">{{ t(titles[kind]) }}</button>
        </div>
        <label class="binding-field"><span>{{ t('邮件主题') }}</span><input v-model="draft[purpose].subject" maxlength="160" required /></label>
        <label class="binding-field"><span>{{ t('邮件标题') }}</span><input v-model="draft[purpose].title" maxlength="160" required /></label>
        <label class="binding-field"><span>{{ t('邮件正文') }}</span><textarea v-model="draft[purpose].body" rows="5" maxlength="2000" required></textarea></label>
        <label v-if="purpose !== 'test'" class="binding-field"><span>{{ t('按钮文字') }}</span><input v-model="draft[purpose].button" maxlength="80" required /></label>
        <p class="password-rule">{{ t('预览验证与找回密码邮件的样式。') }}</p>
        <p class="password-rule email-template-entry">{{ t('实际验证和找回邮件请从账号邮件入口发起。') }} <a href="/login?mode=mail">{{ t('打开实际账号邮件入口') }}<AppIcon name="arrow" /></a></p>
        <div class="settings-actions">
          <button class="primary-button" type="submit">{{ t('保存邮件样式') }}</button>
          <button class="secondary-button" type="button" @click="perform('preview')">{{ t('更新预览') }}</button>
          <button class="secondary-button" type="button" :disabled="dirty || !smtpEnabled" @click="perform('sample')">{{ t('发送已保存的样例') }}</button>
        </div>
      </fieldset>
      <div v-if="preview" class="email-preview">
        <h5>{{ t('邮件预览') }}</h5><p>{{ preview.subject }}</p>
        <iframe :srcdoc="preview.html" sandbox="" referrerpolicy="no-referrer" :title="t('邮件预览')"></iframe>
        <details><summary>{{ t('纯文本版本') }}</summary><pre>{{ preview.text }}</pre></details>
      </div>
    </form>

  </section>
</template>
