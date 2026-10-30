<script setup lang="ts">
import { ref, watch } from 'vue'
const props = defineProps<{ image?: string; text?: string; kind: 'intro' | 'characters' | 'library' | 'game' }>()
const failed = ref(false)
watch(() => props.image, () => { failed.value = false })
</script>
<template>
  <img v-if="image && !failed" :src="image" alt="" referrerpolicy="no-referrer" @error="failed = true" />
  <span v-else-if="text">{{ text }}</span>
  <svg v-else viewBox="0 0 32 32" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path v-if="kind === 'intro'" d="m16 4 11 6v12l-11 6-11-6V10Zm0 12L5 10m11 6 11-6M16 16v12M11 7l11 6" />
    <path v-else-if="kind === 'characters'" d="M12 5h8v8h-8zM9 17h14v10H9zM5 17v8m22-8v8M13 27v2m6-2v2" />
    <path v-else-if="kind === 'library'" d="M5 5h9v9H5zm13 0h9v9h-9zM5 18h9v9H5zm13 0h9v9h-9z" />
    <path v-else d="M11 12V7a3 3 0 0 1 3-3h4a3 3 0 0 1 3 3v5M7 12h18v15H7zM12 20h8m-4-4v8" />
  </svg>
</template>
