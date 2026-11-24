<script setup lang="ts">
import { feedbackRef, confirmAction, toast } from '../feedback'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import CharacterViewer from './CharacterViewer.vue'
import ModelThumbnail from './ModelThumbnail.vue'
import AppModal from './AppModal.vue'
import ApplyTextureDialog from './ApplyTextureDialog.vue'
import ReportForm from './ReportForm.vue'
import ResourceFilters from './ResourceFilters.vue'
import AppIcon from './AppIcon.vue'
import LoadingSkeleton from './LoadingSkeleton.vue'
import type { LibraryRecord } from '../textureTypes'
import { locale, t } from '../i18n'

const props = defineProps<{ mode: 'library' | 'wardrobe'; csrfToken: string; detailId?: string; wardrobe: LibraryRecord[]; wardrobeBusy: boolean; wardrobeError: string }>()
const emit = defineEmits<{ changed: []; refresh: []; wear: [texture: LibraryRecord]; openDetail: [id: string]; login: []; upload: []; expired: [] }>()
const records = ref<LibraryRecord[]>([])
const loading = ref(false)
const loadError = feedbackRef('error')
const actionError = feedbackRef('error')
const message = feedbackRef('success')
const busy = ref(false)
const query = ref('')
const kind = ref('')
const category=ref(''), tag=ref('')
const sort = ref('name')
const owner = ref('')
const ownerName = ref('')
const page = ref(1)
const total = ref(0)
const selectedId = ref('')
const detailRecord = ref<LibraryRecord | null>(null)
const detailBusy = ref(false)
const detailNotFound = ref(false)
const alias = ref('')
const fallback = ref('')
const failedImages = ref<string[]>([])
const items = computed(() => records.value)
const selected = computed(() => items.value.find(item => item.id === selectedId.value) || (detailRecord.value?.id === selectedId.value ? detailRecord.value : undefined))
const pending = computed(() => loading.value)
const error = computed(() => loadError.value)
const collected = computed(() => new Set(records.value.filter(item => item.collected).map(item => item.id)))
const applying = ref<LibraryRecord>()
let request: AbortController | undefined
let detailRequest: AbortController | undefined
const lifetime = new AbortController()
let active = true

watch(items, values => {
  if (props.mode === 'library' && props.detailId && selectedId.value === props.detailId) return
  if (!values.some(item => item.id === selectedId.value)) selectedId.value = ''
}, { immediate: true })
watch(selected, item => { alias.value = item?.displayName || item?.name || '' }, { immediate: true })

async function loadDetail(id: string) {
  detailRequest?.abort()
  if (!id || props.mode !== 'library') {
    detailRecord.value = null
    detailNotFound.value = false
    detailBusy.value = false
    return
  }
  selectedId.value = id
  detailNotFound.value = false
  if (items.value.some(item => item.id === id)) {
    detailRecord.value = null
    detailBusy.value = false
    return
  }
  const controller = new AbortController()
  detailRequest = controller
  detailBusy.value = true
  try {
    const response = await fetch(`/api/library/${encodeURIComponent(id)}`, { credentials: 'same-origin', signal: controller.signal })
    if (!active || controller.signal.aborted) return
    if (response.status === 404) {
      detailRecord.value = null
      detailNotFound.value = true
      return
    }
    if (!response.ok) throw new Error('detail')
    detailRecord.value = await response.json() as LibraryRecord
  } catch {
    if (active && !controller.signal.aborted) {
      detailRecord.value = null
      detailNotFound.value = true
    }
  } finally {
    if (detailRequest === controller) detailBusy.value = false
  }
}

watch(() => [props.mode, props.detailId] as const, ([, id]) => {
  if (props.mode === 'library') void loadDetail(id || '')
}, { immediate: true })

function selectItem(id: string) {
  if (props.mode === 'library') emit('openDetail', id)
  else selectedId.value = id
}

function closeDetail() {
  detailRecord.value = null
  detailNotFound.value = false
  if (props.mode === 'library' && props.detailId) emit('openDetail', '')
  else selectedId.value = ''
}

async function copyShareLink(id: string) {
  const url = `${window.location.origin}/library/${encodeURIComponent(id)}`
  try {
    await navigator.clipboard.writeText(url)
    toast('已复制分享链接。')
  } catch {
    toast('无法自动复制，请复制浏览器地址栏链接。', 'error')
  }
}

function formatDate(unix?: number) {
  if (!unix || unix <= 0) return ''
  return new Date(unix * 1000).toLocaleDateString(locale.value === 'en' ? 'en-US' : 'zh-CN')
}

async function load() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  loadError.value = ''
  try {
    const params = new URLSearchParams({ q: query.value.trim(), kind: kind.value, sort: sort.value, owner: owner.value, page: String(page.value), pageSize: '24',category:category.value,tag:tag.value })
    const response = await fetch(`/api/${props.mode}?${params}`, { signal: controller.signal })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) throw new Error('library')
    const data: { textures: LibraryRecord[]; total: number } = await response.json()
    if (controller.signal.aborted) return
    records.value = data.textures
    total.value = data.total
    if (page.value > 1 && !data.textures.length) { page.value = Math.max(1, Math.ceil(data.total / 24)); void load() }
  } catch {
    if (!controller.signal.aborted) loadError.value = props.mode === 'library' ? '公开库加载失败，请重试。' : '衣柜加载失败，请重试。'
  } finally {
    if (request === controller) loading.value = false
  }
}

function search() { selectedId.value = ''; page.value = 1; void load() }
function filterOwner(item: LibraryRecord) { owner.value = item.ownerId; ownerName.value = item.ownerName; search() }
function changePage(value: number) { selectedId.value = ''; page.value = value; void load() }
function label(item: LibraryRecord) { return props.mode === 'wardrobe' ? item.displayName : item.name }

async function change(method: 'PUT' | 'PATCH' | 'DELETE') {
  if (!props.csrfToken) { emit('login'); return }
  const item = selected.value
  if (!item || busy.value) return
  if (method === 'DELETE' && !await confirmAction({ title: '移出衣柜', message: t('将“{name}”移出衣柜？', { name: label(item) }) + (item.public ? '' : ' ' + t('此材质未公开，移出后无法再次收藏。')), danger: true, confirmLabel: '移出衣柜' })) return
  if (method === 'PATCH' && (!alias.value.trim() || Array.from(alias.value.trim()).length > 255)) { actionError.value = '衣柜名称不能为空且不能超过 255 个字符。'; return }
  busy.value = true
  actionError.value = ''
  message.value = ''
  try {
    const response = await fetch(`/api/wardrobe/${item.id}`, { method, credentials: 'same-origin', signal: lifetime.signal, headers: { 'X-CSRF-Token': props.csrfToken, 'Content-Type': 'application/json' }, body: method === 'PATCH' ? JSON.stringify({ name: alias.value.trim() }) : undefined })
    if (!active) return
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data: { error?: string } = await response.json().catch(() => ({}))
      if (active) actionError.value = data.error || '操作未完成，请重试。'
      return
    }
    message.value = method === 'PUT' ? '已加入我的衣柜。' : method === 'PATCH' ? '衣柜名称已保存。' : '已移出衣柜。'
    emit('changed')
    await load()
    if (props.mode === 'library' && props.detailId && detailRecord.value?.id === props.detailId) {
      await loadDetail(props.detailId)
    }
  } catch {
    if (active) actionError.value = '无法连接到服务，请重试。'
  } finally {
    if (active) busy.value = false
  }
}

const likeBusy = ref(false)
const sortPresets = [
  { value: 'trending', label: '近期热门' },
  { value: 'newest', label: '最新上传' },
  { value: 'likes', label: '点赞最多' },
  { value: 'collections', label: '收藏最多' },
  { value: 'name', label: '按名称' },
] as const
function selectSort(value: string) {
  if (sort.value === value) return
  sort.value = value
  search()
}
async function toggleLike() {
  if (!props.csrfToken) { emit('login'); return }
  const item = selected.value
  if (!item || likeBusy.value || busy.value) return
  likeBusy.value = true
  actionError.value = ''
  try {
    const method = item.liked ? 'DELETE' : 'PUT'
    const response = await fetch(`/api/library/${encodeURIComponent(item.id)}/like`, {
      method,
      credentials: 'same-origin',
      signal: lifetime.signal,
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (!active) return
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data: { error?: string } = await response.json().catch(() => ({}))
      if (active) actionError.value = data.error || '操作未完成，请重试。'
      return
    }
    const result = await response.json() as { liked: boolean; likeCount: number }
    if (!active) return
    item.liked = result.liked
    item.likeCount = result.likeCount
  } catch {
    if (active) actionError.value = '无法连接到服务，请重试。'
  } finally {
    if (active) likeBusy.value = false
  }
}

watch(() => props.mode, () => { selectedId.value = ''; detailRecord.value = null; detailNotFound.value = false; message.value = ''; actionError.value = ''; void load() }, { immediate: true })
onBeforeUnmount(() => { active = false; request?.abort(); detailRequest?.abort(); lifetime.abort() })
</script>

<template>
  <section class="texture-browser">
    <div class="page-heading"><div><h1>{{ mode === 'library' ? t('公开库') : t('我的衣柜') }}</h1></div><div class="page-heading-actions"><button v-if="mode === 'library'" type="button" class="primary-button" @click="emit('upload')">{{ t('上传材质') }}</button><button type="button" class="secondary-button" :disabled="pending || busy" @click="load">{{ pending ? t('正在刷新…') : t('刷新列表') }}</button></div></div>
    <ResourceFilters v-model:query="query" v-model:kind="kind" :label="mode === 'library' ? '搜索公开库' : '搜索我的衣柜'" :placeholder="mode === 'library' ? '搜索材质名称' : '收藏名或材质名称'" :active-filters="Number(!!category) + Number(!!tag) + Number(sort !== 'name')" :busy="pending" @search="search">
      <template v-if="mode === 'library'" #extra>
        <label class="binding-field"><span>{{ t('分类') }}</span><input v-model="category" maxlength="32" :placeholder="t('输入分类')" /></label>
        <label class="binding-field"><span>{{ t('标签') }}</span><input v-model="tag" maxlength="24" :placeholder="t('输入标签')" /></label>
        <label class="binding-field"><span>{{ t('排序') }}</span><select v-model="sort" @change="search"><option value="name">{{ t('名称升序') }}</option><option value="name-desc">{{ t('名称降序') }}</option><option value="newest">{{ t('最新上传') }}</option><option value="trending">{{ t('近期热门') }}</option><option value="likes">{{ t('点赞最多') }}</option><option value="collections">{{ t('收藏最多') }}</option></select></label>
      </template>
      <template #summary>
        <div v-if="mode === 'library'" class="library-sort-strip" role="group" :aria-label="t('快捷榜单排序')">
          <button
            v-for="preset in sortPresets"
            :key="preset.value"
            type="button"
            class="secondary-button library-sort-pill"
            :class="{ active: sort === preset.value }"
            :aria-pressed="sort === preset.value"
            :disabled="pending"
            @click="selectSort(preset.value)"
          >
            <AppIcon v-if="preset.value === 'trending'" name="fire" />
            <AppIcon v-else-if="preset.value === 'likes'" name="heart" />
            {{ t(preset.label) }}
          </button>
        </div>
        <p v-if="owner" class="owner-filter">{{ t('上传者：') }}{{ ownerName || t('这位上传者') }} <button type="button" class="text-button" @click="owner = ''; ownerName = ''; search()">{{ t('查看所有上传者') }}</button></p>
      </template>
    </ResourceFilters>

    <div class="materials-workspace browse-list-layout">
      <section class="material-library panel" :aria-label="mode === 'library' ? t('公开材质列表') : t('收藏列表')" :aria-busy="pending">
        <div class="panel-title"><h2>{{ mode === 'library' ? t('公开材质') : t('收藏的材质') }}</h2><span>{{ t('{count} 份材质', { count: total }) }}</span></div>
        <LoadingSkeleton v-if="pending" layout="cards" label="正在加载材质…" :count="6" />
        <div v-else-if="error" class="empty-state" role="alert"><h2>{{ t("加载失败") }}</h2><button class="secondary-button" type="button" @click="load">{{ t("重新加载") }}</button></div>
        <div v-else-if="!items.length" class="empty-state"><span class="empty-symbol"><AppIcon name="library" /></span><h2>{{ mode === 'library' ? t('没有找到公开材质') : t('衣柜还是空的') }}</h2><p>{{ mode === 'library' ? t('换个关键词或筛选条件试试。') : t('去公开库逛逛，或把你上传的皮肤和披风加入衣柜。') }}</p></div>
        <div v-else class="material-cards"><button v-for="item in items" :key="item.id" type="button" class="material-card" :class="{ selected: item.id === selectedId }" :aria-pressed="item.id === selectedId" @click="selectItem(item.id)"><ModelThumbnail class="material-thumbnail" :name="label(item)" :model="item.kind === 'skin' ? item.model : 'default'" :skin-hash="item.kind === 'skin' ? item.fileHash : ''" :cape-hash="item.kind === 'cape' ? item.fileHash : ''" /><span class="material-card-copy"><strong>{{ label(item) }}</strong><span>{{ item.kind === 'cape' ? t('披风') : item.model === 'slim' ? t('纤细手臂') : t('标准手臂') }}<small class="material-card-stats"><span class="material-like-stat" :class="{ liked: item.liked }"><AppIcon name="heart" />{{ item.likeCount ?? 0 }}</span><span>{{ item.public ? t('{count} 收藏', { count: item.collectionCount }) : t('未公开') }}</span></small></span></span></button></div>
        <nav v-if="total > 24" class="library-pagination" :aria-label="t('公开库分页')"><button class="secondary-button" type="button" :disabled="pending || page === 1" @click="changePage(page - 1)">{{ t("上一页") }}</button><span>{{ t('第 {page} / {total} 页', { page, total: Math.ceil(total / 24) }) }}</span><button class="secondary-button" type="button" :disabled="pending || page * 24 >= total" @click="changePage(page + 1)">{{ t("下一页") }}</button></nav>
      </section>
      <AppModal v-if="selected" :title="label(selected)" wide :busy="busy" @close="closeDetail"><div class="material-preview">
        <div class="material-preview-heading"><h2 id="browser-preview-title">{{ t("材质预览") }}</h2><span>{{ selected.public ? t('公开') : t('未公开') }}</span></div>
        <CharacterViewer :key="selected.id" :name="label(selected)" :model="selected.kind === 'skin' ? selected.model : 'default'" :skin-hash="selected.kind === 'skin' ? selected.fileHash : ''" :cape-hash="selected.kind === 'cape' ? selected.fileHash : ''" @fallback="fallback = selected.id" />
        <div class="material-description"><h3>{{ label(selected) }}</h3><p v-if="mode === 'wardrobe' && selected.displayName !== selected.name">{{ t("原名：") }}{{ selected.name }}</p><p>{{ selected.description || t('暂无描述。') }}</p><p>{{ t("上传者：") }}{{ selected.ownerName || t('归属未知') }} / {{ t('{count} 人点赞', { count: selected.likeCount ?? 0 }) }} / {{ t('{count} 人收藏', { count: selected.collectionCount }) }}<template v-if="formatDate(selected.createdAt)"> / {{ t('上传于 {date}', { date: formatDate(selected.createdAt) }) }}</template></p><button v-if="mode === 'library' && selected.ownerId" class="text-button uploader-link" type="button" @click="filterOwner(selected)">{{ t("查看此上传者的材质") }}</button></div>
        <div class="material-editing">
          <div class="material-social-actions">
            <button
              v-if="selected.public || selected.owned"
              type="button"
              class="secondary-button like-action-button"
              :class="{ liked: selected.liked }"
              :aria-pressed="!!selected.liked"
              :disabled="busy || likeBusy"
              @click="toggleLike"
            >
              <AppIcon name="heart" />
              {{ !csrfToken ? t('登录后点赞') : selected.liked ? t('已点赞') : t('点赞') }}
              <span>{{ selected.likeCount ?? 0 }}</span>
            </button>
            <button v-if="mode === 'library' && selected.public" type="button" class="secondary-button" :disabled="busy" @click="copyShareLink(selected.id)">{{ t('复制分享链接') }}</button>
            <button v-if="mode === 'library'" type="button" class="primary-button" :disabled="busy || (collected.has(selected.id) && !!csrfToken)" @click="change('PUT')">{{ !csrfToken ? t('登录后收藏') : collected.has(selected.id) ? t('已在衣柜') : t('收藏到衣柜') }}</button>
          </div>
          <template v-if="mode === 'wardrobe'"><form class="inline-form" @submit.prevent="change('PATCH')"><label for="wardrobe-alias">{{ t("衣柜名称") }}</label><div class="inline-controls"><input id="wardrobe-alias" v-model="alias" maxlength="255" required :disabled="busy" /><button type="submit" class="secondary-button" :disabled="busy || alias.trim() === selected.displayName">{{ t("保存名称") }}</button></div></form><button class="danger-button wardrobe-remove" type="button" :disabled="busy" @click="change('DELETE')">{{ t("移出衣柜") }}</button></template>
        </div>
        <details :key="selected.id" class="texture-sources" :open="fallback === selected.id"><summary>{{ t("PNG 材质与下载") }}</summary><div class="texture-preview checkerboard"><img v-if="!failedImages.includes(selected.fileHash)" :src="`/textures/${selected.fileHash}`" :alt="t('“{name}”的 PNG 材质平面图', { name: label(selected) })" @error="failedImages.push(selected.fileHash)" /><p v-else class="texture-placeholder">{{ t("材质预览加载失败，可尝试下载或刷新列表。") }}</p></div><a class="download-link material-download" :href="`/textures/${selected.fileHash}`" :download="`${label(selected)}.png`">{{ t("下载 PNG") }}</a></details>
        <button class="primary-button material-wear" type="button" :disabled="busy" @click="csrfToken ? applying = selected : emit('login')">{{ t("应用到角色") }}</button>
        <ReportForm :record-id="selected.id" :name="label(selected)" :csrf-token="csrfToken" @expired="emit('expired')" @login="emit('login')" />
      </div></AppModal>
      <AppModal v-else-if="mode === 'library' && detailId && (detailBusy || detailNotFound)" :title="detailBusy ? t('正在加载材质…') : t('未找到公开材质')" @close="closeDetail">
        <LoadingSkeleton v-if="detailBusy" layout="rows" label="正在加载材质…" :count="2" />
        <div v-else class="empty-state">
          <p>{{ t('该材质可能未公开、已删除或链接无效。') }}</p>
          <button type="button" class="primary-button" @click="closeDetail">{{ t('返回公开库') }}</button>
        </div>
      </AppModal>
    </div>
    <ApplyTextureDialog v-if="applying" :texture="applying" :csrf-token="csrfToken" @close="applying = undefined" @applied="emit('wear', applying!)" @expired="emit('expired')" />
  </section>
</template>
