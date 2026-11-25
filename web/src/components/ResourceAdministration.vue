<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppModal from './AppModal.vue'
import { feedbackRef } from '../feedback'
import { computed, nextTick, onMounted, ref } from 'vue'
import { t } from '../i18n'
import ModelThumbnail from './ModelThumbnail.vue'
import ModelPreviewPanel from './ModelPreviewPanel.vue'
import { dirtyModalConfirm } from '../unsavedGuard'
interface Item { id: string; model: string; kind?: 'skin' | 'cape'; ownerId: string; ownerEmail: string; name: string; uuid?: string; skinHash?: string; capeHash?: string; recordId?: string; fileHash?: string; recordPublic?: boolean; disabled?: boolean }
type Action = 'rename' | 'clearSkin' | 'clearCape' | 'remove' | 'delete'
const props = defineProps<{ mode: 'characters' | 'wardrobe'; csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const preview = ref<Item | null>(null)
const search = ref('')
const query = ref('')
const page = ref(1)
const total = ref(0)
const items = ref<Item[]>([])
const busy = ref(false)
const saving = ref(false)
const error = feedbackRef('error')
const actionError = feedbackRef('error')
const message = feedbackRef('success')
const pending = ref<{ item: Item; action: Action } | null>(null)
const nextName = ref('')
const confirmUUID = ref('')
const pendingDirty = computed(() => !!pending.value && ((pending.value.action === 'rename' && nextName.value !== pending.value.item.name) || (pending.value.action === 'delete' && confirmUUID.value !== '')))
const confirmButton = ref<HTMLButtonElement | null>(null)
const labels: Record<Action, string> = { rename: '修改名称', clearSkin: '解除皮肤绑定', clearCape: '解除披风绑定', remove: '移出用户衣柜', delete: '删除角色' }
const heading = computed(() => props.mode === 'characters' ? '角色管理' : '衣柜管理')
async function load() {
  if (busy.value || saving.value) return
  busy.value = true; error.value = ''; preview.value = null
  try {
    const response = await fetch(`/api/admin/${props.mode}?${new URLSearchParams({ q: query.value, page: String(page.value) })}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '管理资源暂时无法加载'; return }
    const data = await response.json() as { items: Item[]; total: number }
    items.value = data.items; total.value = data.total
  } catch { error.value = '管理资源暂时无法加载' }
  finally { busy.value = false }
}
function find() { if (busy.value || saving.value) return; query.value = search.value; page.value = 1; pending.value = null; load() }
function turn(offset: number) { page.value += offset; pending.value = null; load() }
async function confirm(item: Item, action: Action) { pending.value = { item, action }; nextName.value = item.name; confirmUUID.value = ''; actionError.value = ''; message.value = ''; await nextTick(); confirmButton.value?.focus() }
async function apply() {
  const change = pending.value
  if (!change || saving.value) return
  actionError.value = ''; message.value = ''
  if (change.action === 'delete' && confirmUUID.value !== change.item.uuid) { actionError.value = '角色 UUID 不匹配，请刷新后确认'; return }
  if (change.action === 'rename') {
    const name = nextName.value.trim()
    if (props.mode === 'characters' && !/^[A-Za-z0-9_]{3,16}$/.test(name)) { actionError.value = '角色名须为 3–16 位英文字母、数字或下划线'; return }
    if (props.mode === 'wardrobe' && (!name || Array.from(name).length > 255)) { actionError.value = '衣柜名称不能为空且不能超过 255 个字符'; return }
  }
  saving.value = true
  try {
    const path = props.mode === 'characters' ? `/api/admin/characters/${change.item.id}` : `/api/admin/wardrobe/${change.item.ownerId}/${change.item.recordId}`
    const body = change.action === 'rename' ? { name: nextName.value.trim() } : change.action === 'delete' ? { confirmUUID: confirmUUID.value } : { [change.action]: true }
    const response = await fetch(path, { method: change.action === 'remove' || change.action === 'delete' ? 'DELETE' : 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: change.action === 'remove' ? undefined : JSON.stringify(body) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); actionError.value = data.error || '管理操作未完成，请重试'; return }
    pending.value = null; message.value = '管理变更已保存。'
  } catch { actionError.value = '无法确认操作结果，请刷新列表后检查。' }
  finally { saving.value = false }
  await load()
}
onMounted(load)
</script>

<template>
  <section :aria-label="t(heading)">
    <p class="admin-moderation-note">{{ mode === 'characters' ? t('改名保留角色 UUID；解除绑定只改变该角色外观。') : t('管理用户的收藏名称与收藏关系，原始材质保持原样。') }}</p>
    <form class="admin-search panel" @submit.prevent="find"><label class="binding-field"><span>{{ t(mode === 'characters' ? '搜索角色' : '搜索衣柜') }}</span><input v-model="search" maxlength="255" :placeholder="t(mode === 'characters' ? '角色名、UUID、用户邮箱或编号' : '收藏名、材质哈希、用户邮箱或编号')" :disabled="saving" /></label><button class="primary-button" type="submit" :disabled="busy || saving">{{ t('搜索') }}</button><button class="secondary-button" type="button" :disabled="busy || saving" @click="load">{{ t('刷新列表') }}</button></form>
    <AppModal v-if="pending" :title="t('确认{action}', { action: t(labels[pending.action]) })" :busy="saving" :confirm-close="pendingDirty ? dirtyModalConfirm : false" @close="pending = null"><form  class="admin-confirm panel" @submit.prevent="apply"><p class="admin-identity">{{ pending.item.name }} / {{ pending.item.ownerEmail }} / #{{ pending.item.ownerId }}</p><p v-if="pending.item.uuid" class="admin-file-hash">{{ t('角色 UUID') }}：{{ pending.item.uuid }}</p><label v-if="pending.action === 'rename'" class="binding-field"><span>{{ t(mode === 'characters' ? '新角色名' : '新的收藏名称') }}</span><input v-model="nextName" required :disabled="saving" /></label><label v-if="pending.action === 'delete'" class="binding-field"><span>{{ t('输入完整 UUID 以确认删除') }}</span><input v-model="confirmUUID" required :disabled="saving" autocomplete="off" /></label><p>{{ pending.action === 'delete' ? t('删除角色及其外观绑定。此操作无法撤销。') : pending.action === 'remove' ? t('将此材质移出用户衣柜。') : mode === 'characters' ? t('角色 UUID 与其他用户的角色均会保留。') : t('只修改这一位用户的收藏名称。') }}</p><div class="admin-actions"><button ref="confirmButton" :class="pending.action === 'delete' ? 'danger-button' : 'primary-button'" type="submit" :disabled="saving || (pending.action === 'delete' && confirmUUID !== pending.item.uuid)">{{ saving ? t('正在保存…') : t('确认{action}', { action: t(labels[pending.action]) }) }}</button><button class="secondary-button" type="button" :disabled="saving" @click="pending = null; actionError = ''">{{ t('取消') }}</button></div></form></AppModal>

    <ModelPreviewPanel v-if="preview" :key="preview.id" :name="preview.name" :model="preview.model || 'default'" :skin-hash="mode === 'characters' ? preview.skinHash || '' : preview.kind === 'skin' ? preview.fileHash || '' : ''" :cape-hash="mode === 'characters' ? preview.capeHash || '' : preview.kind === 'cape' ? preview.fileHash || '' : ''" @close="preview = null" />
    <LoadingSkeleton v-if="busy" class="panel" label="正在加载…" /><div v-else-if="error" class="empty-state panel" role="alert"><button class="secondary-button" type="button" @click="load">{{ t('重新加载') }}</button></div>
    <div v-else class="admin-list panel"><p v-if="!items.length" class="empty-state">{{ t('没有找到匹配的记录。') }}</p><article v-for="item in items" :key="item.id" class="admin-user admin-resource"><div class="admin-model-cell"><ModelThumbnail v-if="!item.disabled" :name="item.name" :model="item.model || 'default'" :skin-hash="mode === 'characters' ? item.skinHash || '' : item.kind === 'skin' ? item.fileHash || '' : ''" :cape-hash="mode === 'characters' ? item.capeHash || '' : item.kind === 'cape' ? item.fileHash || '' : ''" /><span v-else class="thumbnail-error">{{ t('材质已停用') }}</span><button class="text-button" type="button" :disabled="item.disabled" @click="preview = item">{{ t('查看模型') }}</button></div><div class="admin-user-copy"><h2>{{ item.name }}</h2><p>{{ item.ownerEmail }} / #{{ item.ownerId }}</p><p v-if="item.uuid" class="admin-file-hash">{{ t('角色 UUID') }}：{{ item.uuid }}</p><p v-if="item.fileHash" class="admin-file-hash">{{ item.fileHash }}</p><p v-if="mode === 'wardrobe'" class="admin-status">{{ item.disabled ? t('材质已停用') : item.recordPublic ? t('公开') : t('未公开') }}</p></div><div class="admin-actions"><button class="secondary-button" type="button" :disabled="saving" @click="confirm(item, 'rename')">{{ t('修改名称') }}</button><template v-if="mode === 'characters'"><button class="secondary-button" type="button" :disabled="saving || !item.skinHash" @click="confirm(item, 'clearSkin')">{{ t('解除皮肤绑定') }}</button><button class="secondary-button" type="button" :disabled="saving || !item.capeHash" @click="confirm(item, 'clearCape')">{{ t('解除披风绑定') }}</button><button class="danger-button" type="button" :disabled="saving" @click="confirm(item, 'delete')">{{ t('删除角色') }}</button></template><button v-else class="danger-button" type="button" :disabled="saving" @click="confirm(item, 'remove')">{{ t('移出用户衣柜') }}</button></div></article><div class="library-pagination"><button class="secondary-button" type="button" :disabled="page <= 1 || saving" @click="turn(-1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 条记录', { page, total }) }}</span><button class="secondary-button" type="button" :disabled="page * 20 >= total || saving" @click="turn(1)">{{ t('下一页') }}</button></div></div>
  </section>
</template>
