<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { feedbackRef, toast } from '../feedback'
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import AppModal from './AppModal.vue'
import { t } from '../i18n'
import type { TextureRecord } from '../textureTypes'
import { settingsSnapshot } from '../settingsDraft'
import { dirtyModalConfirm, useUnsavedGuard } from '../unsavedGuard'

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: []; updated: [] }>()

const enabled = ref(false)
const categories = ref('')
const maxSkinWidth = ref(128)
const maxCapeWidth = ref(1024)
const maxFileMiB = ref(8)
const savedPolicy = ref('')

const state = ref('pending')
const items = ref<TextureRecord[]>([])
const page = ref(1)
const total = ref(0)
const busy = ref(false)
const loading = ref(false)
const error = feedbackRef('error')
const pending = ref<{ item: TextureRecord; state: 'approved' | 'rejected' }>()
const reason = ref('')

const policyDirty = computed(() => savedPolicy.value !== '' && settingsSnapshot({
  enabled: enabled.value,
  categories: categories.value,
  maxSkinWidth: maxSkinWidth.value,
  maxCapeWidth: maxCapeWidth.value,
  maxFileMiB: maxFileMiB.value,
}) !== savedPolicy.value)
useUnsavedGuard(policyDirty, '/admin/resources')

let request: AbortController | undefined
const lifetime = new AbortController()

async function load() {
  request?.abort()
  const controller = request = new AbortController()
  loading.value = true
  error.value = ''
  try {
    const r = await fetch('/api/admin/review?' + new URLSearchParams({ state: state.value, page: String(page.value) }), { signal: controller.signal })
    if (r.status === 401) { emit('expired'); return }
    const d = await r.json()
    if (!r.ok) throw new Error(d.error)
    if (!controller.signal.aborted) { items.value = d.textures; total.value = d.total }
  } catch (e) {
    if (!controller.signal.aborted) error.value = (e as Error).message
  } finally {
    if (request === controller) loading.value = false
  }
}

async function savePolicy() {
  await write('/api/admin/content-policy', {
    reviewUploads: enabled.value,
    categories: categories.value.split('\n').map(s => s.trim()).filter(Boolean),
    maxSkinWidth: maxSkinWidth.value,
    maxCapeWidth: maxCapeWidth.value,
    maxFileBytes: maxFileMiB.value * 1048576,
  }, true)
}

async function write(path: string, body: unknown, isPolicy = false) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const r = await fetch(path, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify(body),
      signal: lifetime.signal,
    })
    if (r.status === 401) { emit('expired'); return }
    if (!r.ok) { const d = await r.json(); throw new Error(d.error) }
    if (isPolicy) {
      const d = await r.json().catch(() => ({}))
      enabled.value = !!d.reviewUploads
      categories.value = Array.isArray(d.categories) ? d.categories.join('\n') : categories.value
      maxSkinWidth.value = Number(d.maxSkinWidth) || maxSkinWidth.value
      maxCapeWidth.value = Number(d.maxCapeWidth) || maxCapeWidth.value
      maxFileMiB.value = Math.max(1, Math.min(8, Math.round((Number(d.maxFileBytes) || maxFileMiB.value * 1048576) / 1048576)))
      savedPolicy.value = settingsSnapshot({
        enabled: enabled.value,
        categories: categories.value,
        maxSkinWidth: maxSkinWidth.value,
        maxCapeWidth: maxCapeWidth.value,
        maxFileMiB: maxFileMiB.value,
      })
      toast('内容设置已更新。')
      emit('updated')
    } else {
      toast('审核结果已保存。')
      pending.value = undefined
    }
    await load()
  } catch (e) {
    if (!lifetime.signal.aborted) error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}

function review(item: TextureRecord, next: 'approved' | 'rejected') {
  pending.value = { item, state: next }
  reason.value = ''
}

onMounted(async () => {
  try {
    const r = await fetch('/api/content-policy', { signal: lifetime.signal })
    if (!r.ok) throw new Error()
    const d = await r.json()
    enabled.value = !!d.reviewUploads
    categories.value = Array.isArray(d.categories) ? d.categories.join('\n') : ''
    maxSkinWidth.value = Number(d.maxSkinWidth) || 128
    maxCapeWidth.value = Number(d.maxCapeWidth) || 1024
    maxFileMiB.value = Math.max(1, Math.min(8, Math.round((Number(d.maxFileBytes) || 8 * 1048576) / 1048576)))
    savedPolicy.value = settingsSnapshot({
      enabled: enabled.value,
      categories: categories.value,
      maxSkinWidth: maxSkinWidth.value,
      maxCapeWidth: maxCapeWidth.value,
      maxFileMiB: maxFileMiB.value,
    })
  } catch {
    if (!lifetime.signal.aborted) error.value = '暂时无法读取内容设置'
  }
  if (!lifetime.signal.aborted) await load()
})

onBeforeUnmount(() => { request?.abort(); lifetime.abort() })
</script>

<template>
  <section class="content-review panel">
    <details class="review-policy">
      <summary>{{ t('上传与审核设置') }}</summary>
      <form class="settings-form" @submit.prevent="savePolicy">
        <fieldset :disabled="busy">
          <label class="checkbox-label"><input v-model="enabled" type="checkbox" />{{ t('新上传和资料修改需要审核') }}</label>
          <label class="binding-field"><span>{{ t('分类，每行一个') }}</span><textarea v-model="categories" rows="4" /></label>
          <fieldset class="policy-dimensions">
            <legend>{{ t('尺寸与文件大小') }}</legend>
            <div class="settings-grid">
              <label class="binding-field">
                <span>{{ t('皮肤最大宽度') }}</span>
                <select v-model.number="maxSkinWidth">
                  <option v-for="w in [64, 128, 256, 512, 1024]" :key="w" :value="w">{{ w === 128 ? t('128 像素（默认）') : t('{w} 像素', { w }) }}</option>
                </select>
              </label>
              <label class="binding-field">
                <span>{{ t('披风最大宽度') }}</span>
                <select v-model.number="maxCapeWidth">
                  <option v-for="w in [64, 128, 256, 512, 1024]" :key="w" :value="w">{{ w === 1024 ? t('1024 像素（默认）') : t('{w} 像素', { w }) }}</option>
                </select>
              </label>
              <label class="binding-field">
                <span>{{ t('PNG 最大文件大小') }}</span>
                <select v-model.number="maxFileMiB">
                  <option v-for="m in [1, 2, 4, 6, 8]" :key="m" :value="m">{{ m === 8 ? t('8 MiB（默认）') : `${m} MiB` }}</option>
                </select>
              </label>
            </div>
            <p v-if="maxSkinWidth > 128" class="password-rule">{{ t('部分启动器和服务器插件只支持 64 或 128 像素皮肤。高清皮肤会增加加载和 3D 预览开销。') }}</p>
          </fieldset>
          <button class="primary-button">{{ t('保存内容设置') }}</button>
        </fieldset>
      </form>
    </details>
    <header class="review-toolbar">
      <h2>{{ t('材质审核') }}</h2>
      <label class="binding-field"><select :aria-label="t('审核状态')" v-model="state" :disabled="busy" @change="page = 1; load()"><option value="pending">{{ t('待审核') }}</option><option value="approved">{{ t('已通过') }}</option><option value="rejected">{{ t('已拒绝') }}</option></select></label>
      <button class="secondary-button" type="button" :disabled="busy || loading" @click="load">{{ t('刷新列表') }}</button>
    </header>
    <LoadingSkeleton v-if="loading" label="正在加载材质…" />
    <div v-else-if="error" class="review-empty"><button class="secondary-button" @click="load">{{ t('重试') }}</button></div>
    <div v-else-if="!items.length" class="review-empty">{{ t('没有对应的材质') }}</div>
    <div v-else class="review-list">
      <article v-for="item in items" :key="item.id" class="review-card">
        <img class="checkerboard" :src="'/api/admin/review/' + item.id + '/image'" :alt="item.name" loading="lazy" />
        <div class="review-card-copy">
          <h3>{{ item.name }}</h3>
          <p v-if="item.description">{{ item.description }}</p>
          <p v-if="item.category || item.tags?.length" class="review-meta">{{ [item.category, ...(item.tags || [])].filter(Boolean).join(' / ') }}</p>
          <p v-if="item.reviewReason">{{ item.reviewReason }}</p>
          <div class="settings-actions">
            <button class="secondary-button" type="button" :disabled="busy || state === 'approved'" @click="review(item, 'approved')">{{ t('通过') }}</button>
            <button class="danger-button" type="button" :disabled="busy || state === 'rejected'" @click="review(item, 'rejected')">{{ t('拒绝') }}</button>
          </div>
        </div>
      </article>
    </div>
    <nav v-if="total > 20" class="library-pagination" :aria-label="t('材质分页')">
      <button class="secondary-button" type="button" :disabled="loading || busy || page <= 1" @click="page--; load()">{{ t('上一页') }}</button>
      <span>{{ t('第 {page} / {total} 页', { page, total: Math.max(1, Math.ceil(total / 20)) }) }}</span>
      <button class="secondary-button" type="button" :disabled="loading || busy || page * 20 >= total" @click="page++; load()">{{ t('下一页') }}</button>
    </nav>
  </section>
  <AppModal v-if="pending" :title="t(pending.state === 'approved' ? '通过材质' : '拒绝材质')" :busy="busy" :confirm-close="reason.trim() ? dirtyModalConfirm : false" @close="pending = undefined">
    <form @submit.prevent="write('/api/admin/review/' + pending.item.id, { state: pending.state, reason })">
      <p>{{ pending.item.name }}</p>
      <label class="binding-field"><span>{{ t('审核备注（可选）') }}</span><textarea v-model="reason" maxlength="200" rows="3" :disabled="busy" /></label>
      <div class="modal-actions">
        <button type="button" class="secondary-button" :disabled="busy" @click="pending = undefined">{{ t('取消') }}</button>
        <button :class="pending.state === 'approved' ? 'primary-button' : 'danger-button'" :disabled="busy">{{ t('确认') }}</button>
      </div>
    </form>
  </AppModal>
</template>
