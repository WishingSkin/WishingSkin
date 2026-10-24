<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { HomepageBlock, SiteAccess } from '../accountTypes'
import { t } from '../i18n'
const props = defineProps<{ site: SiteAccess; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const name = ref(props.site.name || 'WishingSkin')
const tagline = ref(props.site.tagline || '')
const footer = ref(props.site.footer || '')
const notice = ref(props.site.notice || '')
const format = ref(props.site.noticeFormat || 'plain')
const preview = ref('')
const busy = ref(false)
const error = ref('')
const message = ref('')
const nameLength = computed(() => Array.from(name.value.trim()).length)
const taglineLength = computed(() => Array.from(tagline.value).length)
const noticeLength = computed(() => Array.from(notice.value).length)
const homepageBlocks = ref<HomepageBlock[]>(cloneBlocks(props.site.homepage?.blocks || []))
const homepageBusy = ref(false)
const homepageError = ref('')
const homepageMessage = ref('')
const homepageKinds: Array<{ value: HomepageBlock['kind']; label: string }> = [
  { value: 'intro', label: '介绍' },
  { value: 'features', label: '功能卡' },
  { value: 'notice', label: '公告' },
  { value: 'cta', label: '行动区' },
]

function cloneBlocks(value: HomepageBlock[]) {
  return value.map(block => ({ ...block, features: block.features?.map(feature => ({ ...feature })) }))
}

watch([() => props.site.name, () => props.site.tagline, () => props.site.footer, () => props.site.notice, () => props.site.noticeFormat], () => { name.value = props.site.name || 'WishingSkin'; tagline.value = props.site.tagline || ''; footer.value = props.site.footer || ''; notice.value = props.site.notice || ''; format.value = props.site.noticeFormat || 'plain'; preview.value = '' })
watch(() => props.site.homepage?.blocks, blocks => { homepageBlocks.value = cloneBlocks(blocks || []); homepageMessage.value = ''; homepageError.value = '' }, { deep: true })
async function save() {
  if (busy.value) return
  error.value = ''; message.value = ''
  if (!nameLength.value || nameLength.value > 64) { error.value = '站点名称须为 1–64 个字符且不能换行'; return }
  if (taglineLength.value > 200) { error.value = '站点简介最多 200 个字符且不能换行'; return }
  if (Array.from(footer.value).length > 200 || /[\r\n]/.test(footer.value)) { error.value = '页脚信息最多 200 个字符且不能换行'; return }
  if (noticeLength.value > 1000) { error.value = '站点公告最多 1000 个字符'; return }
  busy.value = true
  try {
    const response = await fetch('/api/admin/branding', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ name: name.value, tagline: tagline.value, footer: footer.value, notice: notice.value, noticeFormat: format.value }) })
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
function addHomepageBlock() {
  if (homepageBlocks.value.length >= 8) { homepageError.value = '首页区块最多 8 个'; return }
  homepageBlocks.value.push({ id: `block-${Date.now().toString(36)}`, kind: 'cta', enabled: true, title: '新的首页区块', body: '', buttonLabel: '', buttonHref: '', features: [] })
  homepageMessage.value = ''
  homepageError.value = ''
}
function removeHomepageBlock(index: number) { homepageBlocks.value.splice(index, 1); homepageMessage.value = ''; homepageError.value = '' }
function moveHomepageBlock(index: number, offset: number) {
  const next = index + offset
  if (next < 0 || next >= homepageBlocks.value.length) return
  const [block] = homepageBlocks.value.splice(index, 1)
  homepageBlocks.value.splice(next, 0, block!)
  homepageMessage.value = ''
}
function addFeature(block: HomepageBlock) {
  if (!block.features) block.features = []
  if (block.features.length >= 6) { homepageError.value = '首页功能卡最多 6 个'; return }
  block.features.push({ title: '新的功能卡', body: '', href: '' })
  homepageMessage.value = ''
  homepageError.value = ''
}
function removeFeature(block: HomepageBlock, index: number) { block.features?.splice(index, 1); homepageMessage.value = '' }
async function saveHomepage() {
  if (homepageBusy.value) return
  homepageBusy.value = true; homepageError.value = ''; homepageMessage.value = ''
  try {
    const response = await fetch('/api/admin/homepage', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ blocks: homepageBlocks.value }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({}))
    if (!response.ok) { homepageError.value = data.error || '暂时无法保存首页设置'; return }
    homepageMessage.value = '首页区块已保存。'; emit('updated')
  } catch { homepageError.value = '无法确认操作结果，请重新检查首页设置。' }
  finally { homepageBusy.value = false }
}
watch([notice, format], () => { preview.value = '' })
</script>

<template>
  <section class="account-password branding-settings" aria-labelledby="branding-title"><h3 id="branding-title">{{ t('品牌与公告') }}</h3><form class="password-form" @submit.prevent="save"><fieldset :disabled="busy"><label class="binding-field"><span>{{ t('站点名称') }} <small>{{ nameLength }}/64</small></span><input v-model="name" required /></label><label class="binding-field"><span>{{ t('站点简介') }} <small>{{ taglineLength }}/200</small></span><input v-model="tagline" /></label><label class="binding-field"><span>{{ t('自定义页脚信息') }} <small>{{ Array.from(footer).length }}/200</small></span><input v-model="footer" :placeholder="t('显示在项目页脚旁，不会替换 Powered by WishingSkin')" /></label><label class="binding-field"><span>{{ t('公告格式') }}</span><select v-model="format"><option value="plain">{{ t('纯文本') }}</option><option value="markdown">Markdown + HTML</option></select></label><label class="binding-field"><span>{{ t('站点公告') }} <small>{{ noticeLength }}/1000</small></span><textarea v-model="notice" rows="5"></textarea></label><p v-if="format === 'markdown'" class="password-rule">{{ t('支持粗体、链接、列表等排版，脚本、图片和自定义样式会被过滤。') }}</p><button class="secondary-button" type="button" @click="previewNotice">{{ t('预览公告') }}</button><div v-if="preview" class="notice-content notice-preview" v-html="preview"></div><p class="password-rule">{{ t('保存会发布新公告；清空后保留历史记录。') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }}</p><button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存品牌与公告') }}</button></fieldset></form><p v-if="message" class="action-success" role="status">{{ t(message) }}</p></section>
<section class="account-password homepage-settings" aria-labelledby="homepage-title">
  <div class="settings-section-heading"><div><h3 id="homepage-title">{{ t('首页区块') }}</h3><p class="password-rule">{{ t('用安全区块编辑主页内容，可启停并调整顺序。按钮只允许站内路径或 HTTPS。') }}</p></div><button class="secondary-button" type="button" :disabled="homepageBusy || homepageBlocks.length >= 8" @click="addHomepageBlock">{{ t('添加区块') }}</button></div>
  <div v-if="homepageBlocks.length" class="homepage-block-list">
    <article v-for="(block, index) in homepageBlocks" :key="block.id" class="homepage-block-editor">
      <div class="homepage-block-toolbar"><strong>{{ index + 1 }}. {{ block.id }}</strong><label class="checkbox-label"><input v-model="block.enabled" type="checkbox" :disabled="homepageBusy" /> {{ t('启用') }}</label><select v-model="block.kind" :disabled="homepageBusy" :aria-label="t('区块类型')"><option v-for="kind in homepageKinds" :key="kind.value" :value="kind.value">{{ t(kind.label) }}</option></select><button class="text-button" type="button" :disabled="homepageBusy || index === 0" @click="moveHomepageBlock(index, -1)">{{ t('上移') }}</button><button class="text-button" type="button" :disabled="homepageBusy || index === homepageBlocks.length - 1" @click="moveHomepageBlock(index, 1)">{{ t('下移') }}</button><button class="text-button danger-text" type="button" :disabled="homepageBusy" @click="removeHomepageBlock(index)">{{ t('删除') }}</button></div>
      <div class="homepage-block-fields"><label class="binding-field"><span>{{ t('区块标题') }}</span><input v-model="block.title" :disabled="homepageBusy" maxlength="80" /></label><label class="binding-field"><span>{{ t('区块正文') }}</span><textarea v-model="block.body" :disabled="homepageBusy" rows="3" maxlength="1000"></textarea></label><label class="binding-field"><span>{{ t('按钮文字') }}</span><input v-model="block.buttonLabel" :disabled="homepageBusy" maxlength="40" /></label><label class="binding-field"><span>{{ t('按钮链接') }}</span><input v-model="block.buttonHref" :disabled="homepageBusy" placeholder="/login 或 https://…" /></label></div>
      <div v-if="block.kind === 'features'" class="homepage-feature-editor"><div class="settings-section-heading"><strong>{{ t('功能卡') }}</strong><button class="text-button" type="button" :disabled="homepageBusy || (block.features?.length || 0) >= 6" @click="addFeature(block)">{{ t('添加功能卡') }}</button></div><div v-for="(feature, featureIndex) in block.features || []" :key="featureIndex" class="homepage-feature-row"><label class="binding-field"><span>{{ t('卡片标题') }}</span><input v-model="feature.title" :disabled="homepageBusy" maxlength="80" /></label><label class="binding-field"><span>{{ t('卡片正文') }}</span><input v-model="feature.body" :disabled="homepageBusy" maxlength="240" /></label><label class="binding-field"><span>{{ t('卡片链接') }}</span><input v-model="feature.href" :disabled="homepageBusy" placeholder="/library" /></label><button class="text-button danger-text" type="button" :disabled="homepageBusy" @click="removeFeature(block, featureIndex)">{{ t('删除') }}</button></div></div>
    </article>
  </div>
  <p v-else class="password-rule">{{ t('尚未配置首页区块，主页将显示站点简介。') }}</p>
  <p v-if="homepageError" class="form-error" role="alert">{{ t(homepageError) }}</p><p v-if="homepageMessage" class="action-success" role="status">{{ t(homepageMessage) }}</p><button class="primary-button" type="button" :disabled="homepageBusy" @click="saveHomepage">{{ homepageBusy ? t('正在保存…') : t('保存首页区块') }}</button>
</section>
</template>
