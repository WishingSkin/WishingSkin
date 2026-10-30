<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import HomepageContent from './HomepageContent.vue'
import IconImageField from './IconImageField.vue'
import { settingsSnapshot } from '../settingsDraft'
import type { HomepageBlock, SiteAccess } from '../accountTypes'
import { t } from '../i18n'
const props = defineProps<{ site: SiteAccess; csrfToken: string; section?: 'site' | 'homepage'; managed?: boolean }>()
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
const homepageLoaded = ref(false)
const homepagePreview = ref(false)
const background = ref({ image: '', overlay: 65 })
const previewSettings = computed(() => ({ blocks: homepageBlocks.value, background: background.value }))
const brandingValue = computed(() => ({ name: name.value, tagline: tagline.value, footer: footer.value, notice: notice.value, noticeFormat: format.value }))
const savedBranding = ref(settingsSnapshot(brandingValue.value))
const savedHomepage = ref('')
const brandingDirty = computed(() => settingsSnapshot(brandingValue.value) !== savedBranding.value)
const homepageDirty = computed(() => homepageLoaded.value && settingsSnapshot(previewSettings.value) !== savedHomepage.value)
const imageProcessing = ref(0)
function processImage(value: boolean) { imageProcessing.value += value ? 1 : -1 }
async function loadHomepage() {
  homepageError.value = ''; homepageBusy.value = true
  try {
    const response = await fetch('/api/admin/homepage', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) throw new Error()
    const data = await response.json()
    homepageBlocks.value = cloneBlocks(data.blocks || [])
    background.value = { image: data.background?.image || '', overlay: data.background?.overlay ?? 65 }
    savedHomepage.value = settingsSnapshot(previewSettings.value)
    homepageLoaded.value = true
  } catch { homepageError.value = '暂时无法读取首页设置' }
  finally { homepageBusy.value = false }
}
async function pickBackground(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]; input.value = ''
  if (!file) return
  if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 2 * 1024 * 1024) { homepageError.value = '首页背景须为不超过 2 MiB、4096×4096 的 PNG 或 JPEG'; return }
  processImage(true)
  try {
    background.value.image = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = reject; reader.readAsDataURL(file) })
    homepageError.value = ''; homepageMessage.value = ''
  } catch { homepageError.value = '图片读取失败，请重新选择' }
  finally { processImage(false) }
}
onMounted(() => { if (props.section !== 'site') void loadHomepage() })
const homepageKinds: Array<{ value: HomepageBlock['kind']; label: string }> = [
  { value: 'intro', label: '介绍' },
  { value: 'features', label: '功能卡' },
  { value: 'notice', label: '公告' },
  { value: 'cta', label: '行动区' },
]

function cloneBlocks(value: HomepageBlock[]) {
  return value.map(block => ({ ...block, features: block.features?.map(feature => ({ ...feature })) }))
}

watch([() => props.site.name, () => props.site.tagline, () => props.site.footer, () => props.site.notice, () => props.site.noticeFormat], () => { if (brandingDirty.value) return; name.value = props.site.name || 'WishingSkin'; tagline.value = props.site.tagline || ''; footer.value = props.site.footer || ''; notice.value = props.site.notice || ''; format.value = props.site.noticeFormat || 'plain'; preview.value = ''; savedBranding.value = settingsSnapshot(brandingValue.value) })
watch(() => props.site.homepage?.blocks, blocks => { if (homepageDirty.value) return; homepageBlocks.value = cloneBlocks(blocks || []); savedHomepage.value = settingsSnapshot(previewSettings.value) }, { deep: true })
async function save() {
  if (busy.value) return false
  error.value = ''; message.value = ''
  if (!nameLength.value || nameLength.value > 64) { error.value = '站点名称须为 1–64 个字符且不能换行'; return false }
  if (taglineLength.value > 200) { error.value = '站点简介最多 200 个字符且不能换行'; return false }
  if (Array.from(footer.value).length > 200 || /[\r\n]/.test(footer.value)) { error.value = '页脚信息最多 200 个字符且不能换行'; return false }
  if (noticeLength.value > 1000) { error.value = '站点公告最多 1000 个字符'; return false }
  busy.value = true
  try {
    const response = await fetch('/api/admin/branding', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ name: name.value, tagline: tagline.value, footer: footer.value, notice: notice.value, noticeFormat: format.value }) })
    if (response.status === 401) { emit('expired'); return false }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法保存站点设置'; return false }
    savedBranding.value = settingsSnapshot(brandingValue.value); message.value = '站点文案已保存。'; if (!props.managed) emit('updated'); return true
  } catch { error.value = '无法确认操作结果，请重新检查设置。'; return false }
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
  block.features.push({ title: '新的功能卡', body: '', href: '', icon: '' })
  homepageMessage.value = ''
  homepageError.value = ''
}
function removeFeature(block: HomepageBlock, index: number) { block.features?.splice(index, 1); homepageMessage.value = '' }
async function saveHomepage() {
  if (homepageBusy.value || imageProcessing.value || !homepageLoaded.value) return false
  homepageBusy.value = true; homepageError.value = ''; homepageMessage.value = ''
  try {
    const response = await fetch('/api/admin/homepage', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ blocks: homepageBlocks.value, background: background.value }) })
    if (response.status === 401) { emit('expired'); return false }
    const data = await response.json().catch(() => ({}))
    if (!response.ok) { homepageError.value = data.error || '暂时无法保存首页设置'; return false }
    homepageBlocks.value = cloneBlocks(data.blocks || [])
    background.value = { image: data.background?.image || '', overlay: data.background?.overlay ?? 65 }
    savedHomepage.value = settingsSnapshot(previewSettings.value)
    homepageMessage.value = '首页区块已保存。'; if (!props.managed) emit('updated'); return true
  } catch { homepageError.value = '无法确认操作结果，请重新检查首页设置。'; return false }
  finally { homepageBusy.value = false }
}
watch([notice, format], () => { preview.value = '' })
defineExpose({ dirty: computed(() => props.section === 'homepage' ? homepageDirty.value : brandingDirty.value), busy: computed(() => busy.value || homepageBusy.value || imageProcessing.value > 0), save: () => props.section === 'homepage' ? saveHomepage() : save() })
</script>

<template>
  <section v-if="section !== 'homepage'" class="account-password branding-settings" aria-labelledby="branding-title"><h3 id="branding-title">{{ t('品牌与公告') }}</h3><form class="password-form" @submit.prevent="!managed && save()"><fieldset :disabled="busy"><label class="binding-field"><span>{{ t('站点名称') }} <small>{{ nameLength }}/64</small></span><input v-model="name" required /></label><label class="binding-field"><span>{{ t('站点简介') }} <small>{{ taglineLength }}/200</small></span><input v-model="tagline" /></label><label class="binding-field"><span>{{ t('自定义页脚信息') }} <small>{{ Array.from(footer).length }}/200</small></span><input v-model="footer" :placeholder="t('显示在项目页脚旁，不会替换 Powered by WishingSkin')" /></label><label class="binding-field"><span>{{ t('公告格式') }}</span><select v-model="format"><option value="plain">{{ t('纯文本') }}</option><option value="markdown">Markdown + HTML</option></select></label><label class="binding-field"><span>{{ t('站点公告') }} <small>{{ noticeLength }}/1000</small></span><textarea v-model="notice" rows="5"></textarea></label><p v-if="format === 'markdown'" class="password-rule">{{ t('支持粗体、链接、列表等排版，脚本、图片和自定义样式会被过滤。') }}</p><button class="secondary-button" type="button" @click="previewNotice">{{ t('预览公告') }}</button><div v-if="preview" class="notice-content notice-preview" v-html="preview"></div><p class="password-rule">{{ t('保存会发布新公告；清空后保留历史记录。') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }}</p><button v-if="!managed" class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存品牌与公告') }}</button></fieldset></form><p v-if="message" class="action-success" role="status">{{ t(message) }}</p></section>
<section v-if="section !== 'site'" class="account-password homepage-settings" aria-labelledby="homepage-title">
  <p v-if="!homepageLoaded" role="status">{{ t('正在加载…') }} <button v-if="homepageError" class="text-button" type="button" @click="loadHomepage">{{ t('重试') }}</button></p>
  <fieldset :disabled="!homepageLoaded || homepageBusy" class="homepage-editor-fields">
  <div class="homepage-background-settings"><label class="binding-field"><span>{{ t('主页背景') }}</span><input type="file" accept="image/png,image/jpeg" @change="pickBackground" /></label><p class="password-rule">{{ t('PNG 或 JPEG，最大 2 MiB、4096×4096。仅用于公开主页。') }}</p><img v-if="background.image" class="homepage-background-thumbnail" :src="background.image" :alt="t('主页背景预览')" /><div class="settings-actions"><button class="secondary-button" type="button" :disabled="!background.image" @click="background.image = ''">{{ t('移除背景') }}</button><button class="secondary-button" type="button" :aria-expanded="homepagePreview" @click="homepagePreview = !homepagePreview">{{ homepagePreview ? t('收起主页预览') : t('预览主页') }}</button></div><label v-if="background.image" class="binding-field"><span>{{ t('背景遮罩') }} {{ background.overlay }}%</span><input v-model.number="background.overlay" type="range" min="0" max="100" step="1" /></label></div>
  <div v-if="homepagePreview" class="homepage-draft-preview"><p class="password-rule">{{ t('草稿预览，保存后才会公开。') }}</p><HomepageContent :homepage="previewSettings" :name="name" :tagline="tagline" :notice="site.notice" :notice-html="site.noticeHtml" preview /></div>
  <div class="settings-section-heading"><div><h3 id="homepage-title">{{ t('首页区块') }}</h3><p class="password-rule">{{ t('用安全区块编辑主页内容，可启停并调整顺序。按钮只允许站内路径或 HTTPS。') }}</p></div><button class="secondary-button" type="button" :disabled="homepageBusy || homepageBlocks.length >= 8" @click="addHomepageBlock">{{ t('添加区块') }}</button></div>
  <p class="password-rule">{{ t('图标可上传 PNG/JPEG（最大 512 KiB、2048×2048），或填写 HTTPS URL、站内资源路径（如 /assets/icon.png）。不支持本机文件路径。') }}</p>
  <div v-if="homepageBlocks.length" class="homepage-block-list">
    <article v-for="(block, index) in homepageBlocks" :key="block.id" class="homepage-block-editor">
      <div class="homepage-block-toolbar"><strong>{{ index + 1 }}. {{ block.id }}</strong><label class="checkbox-label"><input v-model="block.enabled" type="checkbox" :disabled="homepageBusy" /> {{ t('启用') }}</label><select v-model="block.kind" :disabled="homepageBusy" :aria-label="t('区块类型')"><option v-for="kind in homepageKinds" :key="kind.value" :value="kind.value">{{ t(kind.label) }}</option></select><button class="text-button" type="button" :disabled="homepageBusy || index === 0" @click="moveHomepageBlock(index, -1)">{{ t('上移') }}</button><button class="text-button" type="button" :disabled="homepageBusy || index === homepageBlocks.length - 1" @click="moveHomepageBlock(index, 1)">{{ t('下移') }}</button><button class="text-button danger-text" type="button" :disabled="homepageBusy" @click="removeHomepageBlock(index)">{{ t('删除') }}</button></div>
      <IconImageField v-if="block.kind === 'intro'" v-model="block.iconImage" label="首页主标志图片" @processing="processImage" />
      <div class="homepage-block-fields"><label class="binding-field"><span>{{ t('区块标题') }}</span><input v-model="block.title" :disabled="homepageBusy" maxlength="80" /></label><label class="binding-field"><span>{{ t('区块正文') }}</span><textarea v-model="block.body" :disabled="homepageBusy" rows="3" maxlength="1000"></textarea></label><label class="binding-field"><span>{{ t('按钮文字') }}</span><input v-model="block.buttonLabel" :disabled="homepageBusy" maxlength="40" /></label><label class="binding-field"><span>{{ t('按钮链接') }}</span><input v-model="block.buttonHref" :disabled="homepageBusy" placeholder="/login 或 https://…" /></label></div>
      <div v-if="block.kind === 'features'" class="homepage-feature-editor"><div class="settings-section-heading"><strong>{{ t('功能卡') }}</strong><button class="text-button" type="button" :disabled="homepageBusy || (block.features?.length || 0) >= 6" @click="addFeature(block)">{{ t('添加功能卡') }}</button></div><div v-for="(feature, featureIndex) in block.features || []" :key="featureIndex" class="homepage-feature-row"><div class="homepage-feature-icon-editor"><IconImageField v-model="feature.iconImage" label="功能卡图标图片" @processing="processImage" /><label class="binding-field"><span>{{ t('备用图标文本') }}</span><input v-model="feature.icon" maxlength="4" :placeholder="t('留空使用默认线条图标')" /></label></div><label class="binding-field"><span>{{ t('卡片标题') }}</span><input v-model="feature.title" :disabled="homepageBusy" maxlength="80" /></label><label class="binding-field"><span>{{ t('卡片正文') }}</span><input v-model="feature.body" :disabled="homepageBusy" maxlength="240" /></label><label class="binding-field"><span>{{ t('卡片链接') }}</span><input v-model="feature.href" :disabled="homepageBusy" placeholder="/library" /></label><button class="text-button danger-text" type="button" :disabled="homepageBusy" @click="removeFeature(block, featureIndex)">{{ t('删除') }}</button></div></div>
    </article>
  </div>
  <p v-else class="password-rule">{{ t('尚未配置首页区块，主页将显示站点简介。') }}</p>
  <p v-if="homepageError" class="form-error" role="alert">{{ t(homepageError) }}</p><p v-if="homepageMessage" class="action-success" role="status">{{ t(homepageMessage) }}</p><button v-if="!managed" class="primary-button" type="button" :disabled="homepageBusy" @click="saveHomepage">{{ homepageBusy ? t('正在保存…') : t('保存首页区块') }}</button>
  </fieldset>
</section>
</template>
