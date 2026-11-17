<script setup lang="ts">
import { ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'
const props = defineProps<{ image?: string; text?: string; kind: 'intro' | 'characters' | 'library' | 'game' }>()
const failed = ref(false)
watch(() => props.image, () => { failed.value = false })
</script>
<template>
  <img v-if="image && !failed" :src="image" alt="" referrerpolicy="no-referrer" @error="failed = true" />
  <span v-else-if="text">{{ text }}</span>
  <AppIcon v-else :name="kind === 'intro' ? 'object-storage' : kind" />
</template>
