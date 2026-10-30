<script setup lang="ts">
import { ref } from 'vue'
import { t } from '../i18n'
import AppModal from './AppModal.vue'
import CharacterViewer from './CharacterViewer.vue'
defineProps<{ name: string; model: string; skinHash: string; capeHash: string }>()
defineEmits<{ close: [] }>()
const fallback = ref(false)
</script>
<template>
  <AppModal :title="name" wide @close="$emit('close')"><div class="admin-model-preview">
    <CharacterViewer :name="name" :model="model" :skin-hash="skinHash" :cape-hash="capeHash" @fallback="fallback = true" />
    <details v-if="skinHash || capeHash" class="texture-sources" :open="fallback"><summary>{{ t('PNG 材质与下载') }}</summary><div class="admin-preview-sources"><a v-for="source in [{hash:skinHash,kind:'皮肤'},{hash:capeHash,kind:'披风'}].filter(item => item.hash)" :key="source.kind" :href="'/textures/' + source.hash" :download="name + '-' + source.kind + '.png'"><img :src="'/textures/' + source.hash" :alt="t(source.kind)" loading="lazy" />{{ t('下载 PNG') }} / {{ t(source.kind) }}</a></div></details>
  </div></AppModal>
</template>
