<script setup lang="ts">
import AppModal from './AppModal.vue'
import { feedbackRef } from '../feedback'
import { computed, nextTick, onMounted, ref } from 'vue'
import { locale, t } from '../i18n'
import LegacyReports from './LegacyReports.vue'

interface Item { id: string; recordId: string; name: string; fileHash: string; reason: string; status: string; resolution: string; reply: string; createdAt: number; resolvedAt: number; disabled: boolean; reporterId?: string; reporterEmail?: string; ownerId?: string; ownerEmail?: string }
const props = defineProps<{ admin?: boolean; csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const items = ref<Item[]>([])
const page = ref(1)
const total = ref(0)
const status = ref(props.admin ? 'pending' : '')
const query = ref('')
const busy = ref(false)
const saving = ref(false)
const error = feedbackRef('error')
const actionError = feedbackRef('error')
const message = feedbackRef('success')
const selected = ref<Item | null>(null)
const action = ref('dismiss')
const reply = ref('')
const replyInput = ref<HTMLTextAreaElement | null>(null)
const replyLength = computed(() => Array.from(reply.value.trim()).length)
const statuses: Record<string, string> = { pending: '待处理', resolved: '已处置', dismissed: '已驳回' }
const resolutions: Record<string, string> = { dismiss: '驳回举报', disable: '下架整份图片', ban: '封禁上传者' }
function time(value: number) { return new Date(value * 1000).toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN') }
async function load() {
  if (busy.value || saving.value) return
  busy.value = true; error.value = ''
  try {
    const response = await fetch(`${props.admin ? '/api/admin/reports' : '/api/reports'}?${new URLSearchParams({ page: String(page.value), status: status.value, q: query.value.trim() })}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法读取举报'; return }
    const data = await response.json() as { reports: Item[]; total: number }
    items.value = data.reports; total.value = data.total
  } catch { error.value = '暂时无法读取举报' }
  finally { busy.value = false }
}
function filter() { page.value = 1; selected.value = null; load() }
function turn(offset: number) { page.value += offset; selected.value = null; load() }
async function choose(item: Item) { selected.value = item; action.value = 'dismiss'; reply.value = ''; actionError.value = ''; message.value = ''; await nextTick(); replyInput.value?.focus() }
async function resolve() {
  const item = selected.value
  if (!item || saving.value) return
  if (replyLength.value > 1000) { actionError.value = '处理说明不能超过 1000 个字符'; return }
  saving.value = true; actionError.value = ''; message.value = ''
  try {
    const response = await fetch(`/api/admin/reports/${item.id}`, { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ action: action.value, reply: reply.value.trim() }) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); actionError.value = data.error || '管理操作未完成，请重试'; return }
    selected.value = null; message.value = '举报处理已保存。'
  } catch { actionError.value = '无法确认操作结果，请刷新列表后检查。' }
  finally { saving.value = false }
  await load()
}
onMounted(load)
</script>

<template>
  <section class="reports-section" :aria-label="admin ? t('举报管理') : t('我的举报')">
    <h2 v-if="!admin">{{ t('我的举报') }}</h2>
    <p>{{ admin ? t('查看举报原因并选择处置；处理说明会向举报人展示。') : t('在公开库或衣柜的材质详情中提交举报，在这里查看进度。') }}</p>
    <form class="admin-search panel" @submit.prevent="filter"><label class="binding-field"><span>{{ t('搜索举报') }}</span><input v-model="query" maxlength="255" :placeholder="t(admin ? '材质名称、哈希、原因或账号' : '材质名称或原因')" :disabled="busy || saving" /></label><label class="binding-field"><span>{{ t('举报状态') }}</span><select v-model="status" :disabled="busy || saving"><option value="">{{ t('全部状态') }}</option><option v-for="(label, key) in statuses" :key="key" :value="key">{{ t(label) }}</option></select></label><button class="primary-button" type="submit" :disabled="busy || saving">{{ t('搜索') }}</button><button class="secondary-button" type="button" :disabled="busy || saving" @click="load">{{ t('刷新列表') }}</button></form>
    <AppModal v-if="selected" :title="t('处理举报')" :busy="saving" @close="selected = null"><form  class="admin-confirm panel report-fields" @submit.prevent="resolve">
      <p class="admin-identity">{{ selected.name }}</p>
      <label class="binding-field"><span>{{ t('处理方式') }}</span><select v-model="action" :disabled="saving"><option value="dismiss">{{ t('驳回举报') }}</option><option value="disable">{{ t('下架整份图片') }}</option><option value="ban" :disabled="!selected.ownerId">{{ t('封禁上传者') }}</option></select></label>
      <p v-if="action === 'disable'">{{ t('同一图片的所有记录、收藏和穿戴均会停用，原始 URL 返回不可用；首版不提供恢复入口。') }}</p>
      <p v-if="action === 'ban'">{{ t('仅封禁这份记录的上传者，并撤销其全部登录凭据；材质是否下架需单独处理。') }} {{ selected.ownerEmail }} / #{{ selected.ownerId }}</p>
      <label class="binding-field"><span>{{ t('处理说明（举报人可见，可选）') }} <small>{{ replyLength }}/1000</small></span><textarea ref="replyInput" v-model="reply" rows="4" :disabled="saving" /></label>
      <div class="admin-actions"><button class="danger-button" type="submit" :disabled="saving">{{ saving ? t('正在保存…') : t('确认处理举报') }}</button><button class="secondary-button" type="button" :disabled="saving" @click="selected = null">{{ t('取消') }}</button></div>
    </form></AppModal>

    <div v-if="busy" class="empty-state panel" role="status"><p>{{ t('正在加载举报…') }}</p></div><div v-else-if="error" class="empty-state panel" role="alert"><button class="secondary-button" type="button" @click="load">{{ t('重新加载') }}</button></div>
    <div v-else class="panel admin-list"><p v-if="!items.length" class="empty-state">{{ t('暂无匹配的举报。') }}</p><article v-for="item in items" :key="item.id" class="admin-user report-item"><div class="admin-user-copy"><h3>{{ item.name }}</h3><p>{{ t(statuses[item.status] || '未知状态') }} / <time :datetime="new Date(item.createdAt * 1000).toISOString()">{{ time(item.createdAt) }}</time></p><template v-if="admin"><p>{{ t('举报人') }}：{{ item.reporterEmail }} / #{{ item.reporterId }}</p><p>{{ t('上传者：') }}{{ item.ownerId ? `${item.ownerEmail} / #${item.ownerId}` : t('归属未知') }}</p><p class="admin-file-hash">{{ item.fileHash }}</p><p v-if="item.disabled">{{ t('图片已下架') }}</p></template><p class="report-text"><strong>{{ t('举报原因') }}：</strong>{{ item.reason }}</p><template v-if="item.status !== 'pending'"><p>{{ t(resolutions[item.resolution] || '其他管理操作') }} / {{ time(item.resolvedAt) }}</p><p class="report-text"><strong>{{ t('处理说明') }}：</strong>{{ item.reply || t('管理员未填写说明。') }}</p></template></div><button v-if="admin && item.status === 'pending'" class="secondary-button" type="button" :disabled="saving" @click="choose(item)">{{ t('处理举报') }}</button></article><div class="library-pagination"><button class="secondary-button" type="button" :disabled="page <= 1 || saving" @click="turn(-1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 条记录', { page, total }) }}</span><button class="secondary-button" type="button" :disabled="page * 20 >= total || saving" @click="turn(1)">{{ t('下一页') }}</button></div></div>
    <LegacyReports v-if="admin" @expired="emit('expired')" />
  </section>
</template>
