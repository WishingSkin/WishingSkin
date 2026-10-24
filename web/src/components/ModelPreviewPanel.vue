<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
import CharacterViewer from './CharacterViewer.vue'
defineProps<{ name: string; model: string; skinHash: string; capeHash: string }>()
defineEmits<{ close: [] }>()
const fallback = ref(false)
const heading = ref<HTMLHeadingElement>()
onMounted(() => { heading.value?.focus({ preventScroll: true }); heading.value?.scrollIntoView({ block: 'nearest' }) })
</script>
<template>
  <aside class="admin-model-preview panel" :aria-label="t('“{name}”的模型预览', { name })"><header><h2 ref="heading" tabindex="-1">{{ name }}</h2><button class="secondary-button" type="button" @click="$emit('close')">{{ t('关闭预览') }}</button></header>
    <CharacterViewer :name="name" :model="model" :skin-hash="skinHash" :cape-hash="capeHash" @fallback="fallback = true" />
    <details v-if="skinHash || capeHash" class="texture-sources" :open="fallback"><summary>{{ t('PNG 材质与下载') }}</summary><div class="admin-preview-sources"><a v-for="source in [{hash:skinHash,kind:'皮肤'},{hash:capeHash,kind:'披风'}].filter(item => item.hash)" :key="source.kind" :href="'/textures/' + source.hash" :download="name + '-' + source.kind + '.png'"><img :src="'/textures/' + source.hash" :alt="t(source.kind)" loading="lazy" />{{ t('下载 PNG') }} · {{ t(source.kind) }}</a></div></details>
  </aside>
</template>
