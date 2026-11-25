<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { ref, onMounted, onBeforeUnmount } from 'vue'
import AppModal from './AppModal.vue'
import ModelThumbnail from './ModelThumbnail.vue'
import { toast } from '../feedback'
import { t } from '../i18n'
import type { TextureRecord } from '../textureTypes'
const props = defineProps<{ kind: 'skin' | 'cape'; busy: boolean; confirmLabel?: string }>()
const emit = defineEmits<{ close: []; select: [item: TextureRecord, model?: string]; expired: [] }>()
const source = ref('wardrobe'), query = ref(''), page = ref(1), total = ref(0), loading = ref(false)
const records = ref<(TextureRecord & { displayName?: string })[]>([])
const selected = ref<TextureRecord & { displayName?: string }>()
const model = ref('default')
function choose(item: TextureRecord) { selected.value = item; model.value = item.model === 'slim' ? 'slim' : 'default' }
let request: AbortController | undefined
async function load() {
  request?.abort()
  const controller = new AbortController(); request = controller; loading.value = true
  try {
    const params = new URLSearchParams({ kind: props.kind, q: query.value.trim(), page: String(page.value), pageSize: '20' })
    const r = await fetch(`/api/${source.value}?${params}`, { signal: controller.signal, credentials: 'same-origin' })
    if (r.status === 401) { emit('expired'); return }
    const data = await r.json()
    if (controller.signal.aborted) return
    if (!r.ok) { toast(data.error || '材质列表暂时无法加载。', 'error'); return }
    records.value = data.textures; total.value = data.total
  } catch { if (!controller.signal.aborted) toast('材质列表暂时无法加载。', 'error') }
  finally { if (request === controller) loading.value = false }
}
function search() { selected.value = undefined; page.value = 1; void load() }
onMounted(load); onBeforeUnmount(() => request?.abort())
</script>
<template>
  <AppModal :title="t(kind === 'skin' ? '选择皮肤' : '选择披风')" wide :busy="busy" @close="emit('close')">
    <form class="library-filters" @submit.prevent="search"><label class="binding-field"><span>{{ t('来源') }}</span><select v-model="source" :disabled="busy" @change="search"><option value="wardrobe">{{ t('我的衣柜') }}</option><option value="textures">{{ t('我的材质') }}</option><option value="library">{{ t('公开库') }}</option></select></label><label class="binding-field library-search"><span>{{ t('搜索材质') }}</span><input v-model="query" maxlength="255" :disabled="busy" /></label><button class="primary-button" :disabled="loading || busy">{{ t('搜索') }}</button></form>
    <LoadingSkeleton v-if="loading" label="正在加载材质…" :count="2" />
    <div v-else class="texture-picker-grid"><button v-for="item in records" :key="item.id" type="button" :disabled="busy || (item.reviewState && item.reviewState !== 'approved')" @click="choose(item)" :aria-pressed="selected?.id === item.id"><ModelThumbnail :name="item.displayName || item.name" :model="item.kind === 'skin' ? item.model : 'default'" :skin-hash="item.kind === 'skin' ? item.fileHash : ''" :cape-hash="item.kind === 'cape' ? item.fileHash : ''" /><strong>{{ item.displayName || item.name }}</strong><span v-if="item.reviewState && item.reviewState !== 'approved'">{{ t('待审核') }}</span></button></div>
    <p v-if="!loading && !records.length" class="modal-empty">{{ t('没有找到匹配的材质') }}</p>
    <form v-if="selected" class="picker-selection" @submit.prevent="emit('select', selected, kind === 'skin' ? model : undefined)"><strong>{{ selected.displayName || selected.name }}</strong><label v-if="kind === 'skin'" class="binding-field"><span>{{ t('手臂模型') }}</span><select v-model="model" :disabled="busy"><option value="default">{{ t('标准手臂') }}</option><option value="slim">{{ t('纤细手臂') }}</option></select></label><button class="primary-button" :disabled="busy">{{ confirmLabel || t('应用到角色') }}</button></form>
    <nav class="library-pagination" :aria-label="t('材质分页')"><button class="secondary-button" type="button" :disabled="loading || busy || page <= 1" @click="page--; load()">{{ t('上一页') }}</button><span>{{ t('第 {page} / {total} 页', { page, total: Math.max(1, Math.ceil(total / 20)) }) }}</span><button class="secondary-button" type="button" :disabled="loading || busy || page * 20 >= total" @click="page++; load()">{{ t('下一页') }}</button></nav>
  </AppModal>
</template>
