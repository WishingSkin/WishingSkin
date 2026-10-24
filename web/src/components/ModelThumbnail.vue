<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { t } from '../i18n'
import { modelThumbnail } from './previewModel'
const props = defineProps<{ name: string; model: string; skinHash: string; capeHash: string }>()
const root = ref<HTMLElement>()
const image = ref('')
const failed = ref(false)
let visible = false
let observer: IntersectionObserver | undefined
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  image.value = ''; failed.value = false
  if (!visible) return
  const request = new AbortController()
  controller = request
  try {
    const value = await modelThumbnail(props.skinHash, props.capeHash, props.model, request.signal)
    if (!request.signal.aborted) image.value = value
  } catch { if (!request.signal.aborted) failed.value = true }
}
watch(() => [props.skinHash, props.capeHash, props.model], load)
onMounted(() => {
  observer = new IntersectionObserver(entries => {
    if (entries[0]?.isIntersecting && !visible) { visible = true; observer?.disconnect(); void load() }
  }, { rootMargin: '80px' })
  if (root.value) observer.observe(root.value)
})
onBeforeUnmount(() => { controller?.abort(); observer?.disconnect() })
</script>
<template>
  <span ref="root" class="model-thumbnail" :class="{ 'model-thumbnail-failed': failed }" :aria-busy="!image && !failed">
    <img v-if="image" class="model-snapshot" :src="image" :alt="t('“{name}”的模型预览', { name })" width="180" height="220" />
    <span v-else-if="failed" class="thumbnail-error">{{ t('模型预览不可用，请打开详情重试或查看 PNG。') }}</span>
    <span v-else class="model-loading" aria-hidden="true">◇</span>
  </span>
</template>
