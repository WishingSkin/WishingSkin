<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { SiteAccess } from '../accountTypes'
import { t } from '../i18n'
const props = defineProps<{ site: SiteAccess; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const name = ref(props.site.name || 'WishingSkin')
const tagline = ref(props.site.tagline || '')
const notice = ref(props.site.notice || '')
const format = ref(props.site.noticeFormat || 'plain')
const preview = ref('')
const busy = ref(false)
const error = ref('')
const message = ref('')
const nameLength = computed(() => Array.from(name.value.trim()).length)
const taglineLength = computed(() => Array.from(tagline.value).length)
const noticeLength = computed(() => Array.from(notice.value).length)
watch([() => props.site.name, () => props.site.tagline, () => props.site.notice, () => props.site.noticeFormat], () => { name.value = props.site.name || 'WishingSkin'; tagline.value = props.site.tagline || ''; notice.value = props.site.notice || ''; format.value = props.site.noticeFormat || 'plain'; preview.value = '' })
async function save() {
  if (busy.value) return
  error.value = ''; message.value = ''
  if (!nameLength.value || nameLength.value > 64) { error.value = '站点名称须为 1–64 个字符且不能换行'; return }
  if (taglineLength.value > 200) { error.value = '站点简介最多 200 个字符且不能换行'; return }
  if (noticeLength.value > 1000) { error.value = '站点公告最多 1000 个字符'; return }
  busy.value = true
  try {
    const response = await fetch('/api/admin/branding', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ name: name.value, tagline: tagline.value, notice: notice.value, noticeFormat: format.value }) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法保存站点设置'; return }
    message.value = '站点文案已保存。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
async function previewNotice() {
  if (busy.value) return
  busy.value = true; error.value = ''; preview.value = ''
  try {
    const response = await fetch('/api/admin/notice-preview', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ notice: notice.value, format: format.value }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法预览公告'; return }
    preview.value = data.html
  } catch { error.value = '暂时无法预览公告' }
  finally { busy.value = false }
}
watch([notice, format], () => { preview.value = '' })
</script>

<template>
  <section class="account-password branding-settings" aria-labelledby="branding-title"><h3 id="branding-title">{{ t('品牌与公告') }}</h3><p>{{ t('名称与简介按纯文本显示。公告可使用 Markdown 和安全 HTML 排版。') }}</p><form class="password-form" @submit.prevent="save"><fieldset :disabled="busy"><label class="binding-field"><span>{{ t('站点名称') }} <small>{{ nameLength }}/64</small></span><input v-model="name" required /></label><label class="binding-field"><span>{{ t('站点简介') }} <small>{{ taglineLength }}/200</small></span><input v-model="tagline" /></label><label class="binding-field"><span>{{ t('公告格式') }}</span><select v-model="format"><option value="plain">{{ t('纯文本') }}</option><option value="markdown">Markdown + HTML</option></select></label><label class="binding-field"><span>{{ t('站点公告') }} <small>{{ noticeLength }}/1000</small></span><textarea v-model="notice" rows="5"></textarea></label><p class="password-rule">{{ t('支持粗体、链接、列表等排版，脚本、图片和自定义样式会被过滤。') }}</p><button class="secondary-button" type="button" @click="previewNotice">{{ t('预览公告') }}</button><div v-if="preview" class="notice-content notice-preview" v-html="preview"></div><p class="password-rule">{{ t('简介留空使用默认文案；公告留空后不展示。') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }}</p><button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存品牌与公告') }}</button></fieldset></form><p v-if="message" class="action-success" role="status">{{ t(message) }}</p></section>
</template>
