<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import { interfaceAppearance } from '../interfaceAppearance'
defineProps<{ name: string }>()
</script>
<template><span v-if="interfaceAppearance.icons[name]" class="interface-icon" aria-hidden="true">{{ interfaceAppearance.icons[name] }}</span><AppIcon v-else :name="name" /></template>
