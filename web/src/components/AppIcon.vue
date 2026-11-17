<script setup lang="ts">
import { iconPaths } from '../icons'
defineProps<{ name: string }>()
</script>
<template>
  <svg class="app-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path v-for="(path, index) in iconPaths[name] || iconPaths.plugin" :key="index" :d="path" /></svg>
</template>
