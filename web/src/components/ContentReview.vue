<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { feedbackRef, toast } from '../feedback'
import { onMounted, onBeforeUnmount, ref } from 'vue'
import AppModal from './AppModal.vue'
import { t } from '../i18n'
import type { TextureRecord } from '../textureTypes'
const props = defineProps<{ csrfToken: string }>(), emit = defineEmits<{ expired: [] }>()
const enabled = ref(false), categories = ref(''), state = ref('pending'), items = ref<TextureRecord[]>([])
const page = ref(1), total = ref(0), busy = ref(false), loading = ref(false), error = feedbackRef('error')
const pending = ref<{ item: TextureRecord; state: 'approved' | 'rejected' }>(), reason = ref('')
let request: AbortController | undefined
const lifetime = new AbortController()
async function load() {
  request?.abort(); const controller = request = new AbortController(); loading.value = true; error.value = ''
  try {
    const r = await fetch('/api/admin/review?' + new URLSearchParams({ state: state.value, page: String(page.value) }), { signal: controller.signal })
    if (r.status === 401) { emit('expired'); return }
    const d = await r.json(); if (!r.ok) throw new Error(d.error)
    if (!controller.signal.aborted) { items.value = d.textures; total.value = d.total }
  } catch (e) { if (!controller.signal.aborted) error.value = (e as Error).message }
  finally { if (request === controller) loading.value = false }
}
async function write(path: string, body: unknown) {
  if (busy.value) return; busy.value = true; error.value = ''
  try {
    const r = await fetch(path, { method: 'PATCH', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(body), signal: lifetime.signal })
    if (r.status === 401) { emit('expired'); return }
    if (!r.ok) { const d = await r.json(); throw new Error(d.error) }
    toast(pending.value ? '审核结果已保存。' : '内容设置已更新。'); pending.value = undefined; await load()
  } catch (e) { if (!lifetime.signal.aborted) error.value = (e as Error).message }
  finally { busy.value = false }
}
function review(item: TextureRecord, next: 'approved' | 'rejected') { pending.value = { item, state: next }; reason.value = '' }
onMounted(async () => {
  try { const r = await fetch('/api/content-policy', { signal: lifetime.signal }); if (!r.ok) throw new Error(); const d = await r.json(); enabled.value = d.reviewUploads; categories.value = d.categories.join('\n') }
  catch { if (!lifetime.signal.aborted) error.value = '暂时无法读取内容设置' }
  if (!lifetime.signal.aborted) await load()
})
onBeforeUnmount(() => { request?.abort(); lifetime.abort() })
</script>
<template>
  <section class="content-review panel">
    <details class="review-policy"><summary>{{ t('分类与上传审核设置') }}</summary>
      <form class="settings-form" @submit.prevent="write('/api/admin/content-policy', { reviewUploads: enabled, categories: categories.split('\n').map(s => s.trim()).filter(Boolean) })"><fieldset :disabled="busy">
        <label class="checkbox-label"><input v-model="enabled" type="checkbox" />{{ t('新上传和资料修改需要审核') }}</label>
        <label class="binding-field"><span>{{ t('分类，每行一个') }}</span><textarea v-model="categories" rows="4" /></label>
        <button class="primary-button">{{ t('保存内容设置') }}</button>
      </fieldset></form>
    </details>
    <header class="review-toolbar"><h2>{{ t('材质审核') }}</h2><label class="binding-field"><select :aria-label="t('审核状态')" v-model="state" :disabled="busy" @change="page = 1; load()"><option value="pending">{{ t('待审核') }}</option><option value="approved">{{ t('已通过') }}</option><option value="rejected">{{ t('已拒绝') }}</option></select></label><button class="secondary-button" type="button" :disabled="busy || loading" @click="load">{{ t('刷新列表') }}</button></header>
    <LoadingSkeleton v-if="loading" label="正在加载材质…" />
    <div v-else-if="error" class="review-empty"><button class="secondary-button" @click="load">{{ t('重试') }}</button></div>
    <div v-else-if="!items.length" class="review-empty">{{ t('没有对应的材质') }}</div>
    <div v-else class="review-list"><article v-for="item in items" :key="item.id" class="review-card"><img class="checkerboard" :src="'/api/admin/review/' + item.id + '/image'" :alt="item.name" loading="lazy" /><div class="review-card-copy"><h3>{{ item.name }}</h3><p v-if="item.description">{{ item.description }}</p><p v-if="item.category || item.tags?.length" class="review-meta">{{ [item.category, ...(item.tags || [])].filter(Boolean).join(' / ') }}</p><p v-if="item.reviewReason">{{ item.reviewReason }}</p><div class="settings-actions"><button class="secondary-button" type="button" :disabled="busy || state === 'approved'" @click="review(item, 'approved')">{{ t('通过') }}</button><button class="danger-button" type="button" :disabled="busy || state === 'rejected'" @click="review(item, 'rejected')">{{ t('拒绝') }}</button></div></div></article></div>
    <nav v-if="total > 20" class="library-pagination" :aria-label="t('材质分页')"><button class="secondary-button" type="button" :disabled="loading || busy || page <= 1" @click="page--; load()">{{ t('上一页') }}</button><span>{{ t('第 {page} / {total} 页', { page, total: Math.max(1, Math.ceil(total / 20)) }) }}</span><button class="secondary-button" type="button" :disabled="loading || busy || page * 20 >= total" @click="page++; load()">{{ t('下一页') }}</button></nav>
  </section>
  <AppModal v-if="pending" :title="t(pending.state === 'approved' ? '通过材质' : '拒绝材质')" :busy="busy" @close="pending = undefined"><form @submit.prevent="write('/api/admin/review/' + pending.item.id, { state: pending.state, reason })"><p>{{ pending.item.name }}</p><label class="binding-field"><span>{{ t('审核备注（可选）') }}</span><textarea v-model="reason" maxlength="200" rows="3" :disabled="busy" /></label><div class="modal-actions"><button type="button" class="secondary-button" :disabled="busy" @click="pending = undefined">{{ t('取消') }}</button><button :class="pending.state === 'approved' ? 'primary-button' : 'danger-button'" :disabled="busy">{{ t('确认') }}</button></div></form></AppModal>
</template>
