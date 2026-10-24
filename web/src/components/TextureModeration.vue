<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { t } from '../i18n'
import ModelThumbnail from './ModelThumbnail.vue'
import ModelPreviewPanel from './ModelPreviewPanel.vue'

interface Item { id: string; model: string; kind?: 'skin' | 'cape'; fileHash: string; name: string; ownerId: string; ownerEmail: string; public: boolean; recordDeleted: boolean; disabled: boolean; recordCount: number; characterCount: number; collectionCount: number }
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const preview = ref<Item | null>(null)
const search = ref('')
const query = ref('')
const page = ref(1)
const total = ref(0)
const items = ref<Item[]>([])
const busy = ref(false)
const saving = ref(false)
const error = ref('')
const actionError = ref('')
const message = ref('')
const pending = ref<Item | null>(null)
const confirmButton = ref<HTMLButtonElement | null>(null)
async function load() {
  if (busy.value || saving.value) return
  busy.value = true; error.value = ''; preview.value = null
  try {
    const response = await fetch(`/api/admin/textures?${new URLSearchParams({ q: query.value, page: String(page.value) })}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法读取管理材质'; return }
    const data = await response.json() as { textures: Item[]; total: number }
    items.value = data.textures; total.value = data.total
  } catch { error.value = '暂时无法读取管理材质' }
  finally { busy.value = false }
}
function find() { if (busy.value || saving.value) return; query.value = search.value; page.value = 1; pending.value = null; load() }
function turn(offset: number) { page.value += offset; pending.value = null; load() }
async function confirm(item: Item) { pending.value = item; actionError.value = ''; message.value = ''; await nextTick(); confirmButton.value?.focus() }
async function disable() {
  const item = pending.value
  if (!item || saving.value) return
  saving.value = true; actionError.value = ''; message.value = ''
  try {
    const response = await fetch(`/api/admin/textures/${item.fileHash}/disable`, { method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': props.csrfToken } })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); actionError.value = data.error || '管理操作未完成，请重试'; return }
    pending.value = null; message.value = '图片已下架，全部关联材质已停用。'
  } catch { actionError.value = '无法确认操作结果，请刷新材质列表后检查。' }
  finally { saving.value = false }
  await load()
}
onMounted(load)
</script>

<template>
  <section :aria-label="t('材质管理')">
    <p class="admin-moderation-note">{{ t('按整份图片下架，同一图片的所有记录、收藏和穿戴均会停用。') }}</p>
    <form class="admin-search panel" @submit.prevent="find"><label class="binding-field"><span>{{ t('搜索材质') }}</span><input v-model="search" maxlength="255" :placeholder="t('名称、图片哈希、上传者邮箱或编号')" :disabled="saving" /></label><button class="primary-button" type="submit" :disabled="busy || saving">{{ t('搜索') }}</button><button class="secondary-button" type="button" :disabled="busy || saving" @click="load">{{ t('刷新列表') }}</button></form>
    <div v-if="pending" class="admin-confirm panel" role="alertdialog" aria-modal="false" aria-labelledby="disable-title" aria-describedby="disable-description"><h2 id="disable-title">{{ t('确认下架整份图片') }}</h2><p class="admin-identity">{{ pending.name }}</p><p class="admin-file-hash">{{ pending.fileHash }}</p><p id="disable-description">{{ t('同一图片关联 {records} 份记录、{collections} 项收藏和 {characters} 个角色。数量可能随其他操作变化；所有关联内容均会停用。', { records: pending.recordCount, collections: pending.collectionCount, characters: pending.characterCount }) }} {{ t('原始 URL 将停止提供，已下载或缓存到客户端的副本无法撤回。文件与记录会保留，首版不提供恢复入口。') }}</p><div class="admin-actions"><button ref="confirmButton" class="danger-button" type="button" :disabled="saving" @click="disable">{{ saving ? t('正在保存…') : t('确认下架整份图片') }}</button><button class="secondary-button" type="button" :disabled="saving" @click="pending = null; actionError = ''">{{ t('取消') }}</button></div></div>
    <p v-if="actionError" class="form-error action-feedback" role="alert">{{ t(actionError) }}</p><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
    <ModelPreviewPanel v-if="preview" :key="preview.id" :name="preview.name" :model="preview.kind === 'skin' ? preview.model : 'default'" :skin-hash="preview.kind === 'skin' ? preview.fileHash : ''" :cape-hash="preview.kind === 'cape' ? preview.fileHash : ''" @close="preview = null" />
    <div v-if="busy" class="empty-state panel" role="status"><p>{{ t('正在加载材质…') }}</p></div><div v-else-if="error" class="empty-state panel" role="alert"><p>{{ t(error) }}</p><button class="secondary-button" type="button" @click="load">{{ t('重新加载') }}</button></div>
    <div v-else class="admin-list panel"><p v-if="!items.length" class="empty-state">{{ t('没有找到匹配的材质。') }}</p><article v-for="item in items" :key="item.id" class="admin-user admin-texture"><div class="admin-model-cell"><ModelThumbnail v-if="!item.disabled && !item.recordDeleted" :name="item.name" :model="item.kind === 'skin' ? item.model : 'default'" :skin-hash="item.kind === 'skin' ? item.fileHash : ''" :cape-hash="item.kind === 'cape' ? item.fileHash : ''" /><span v-else class="thumbnail-error">{{ t('材质已停用') }}</span><button class="text-button" type="button" :disabled="item.disabled || item.recordDeleted" @click="preview = item">{{ t('查看模型') }}</button></div><div class="admin-user-copy"><h2>{{ item.name }}</h2><p>{{ item.ownerId ? `${item.ownerEmail} · #${item.ownerId}` : t('上传者归属未导入') }}</p><p class="admin-file-hash">{{ item.fileHash }}</p><div class="admin-status"><span :class="{ 'form-error': item.disabled }">{{ item.disabled ? t('图片已下架') : t('图片可用') }}</span><span>{{ item.recordDeleted ? t('记录已删除') : item.public ? t('公开') : t('未公开') }}</span><span>{{ t('同图 {count} 份记录', { count: item.recordCount }) }}</span></div><a v-if="!item.disabled && !item.recordDeleted" class="text-button" :href="`/textures/${item.fileHash}`" target="_blank" rel="noopener">{{ t('查看 PNG 图片') }}</a></div><button class="danger-button" type="button" :disabled="saving || item.disabled" @click="confirm(item)">{{ item.disabled ? t('图片已下架') : t('下架整份图片') }}</button></article><div class="library-pagination"><button class="secondary-button" type="button" :disabled="page <= 1 || saving" @click="turn(-1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 份材质', { page, total }) }}</span><button class="secondary-button" type="button" :disabled="page * 20 >= total || saving" @click="turn(1)">{{ t('下一页') }}</button></div></div>
  </section>
</template>
