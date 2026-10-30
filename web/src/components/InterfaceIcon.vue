<script setup lang="ts">
import { interfaceAppearance } from '../interfaceAppearance'
defineProps<{ name: string }>()
</script>
<template><span v-if="interfaceAppearance.icons[name]" class="interface-icon" aria-hidden="true">{{ interfaceAppearance.icons[name] }}</span><slot v-else /></template>
