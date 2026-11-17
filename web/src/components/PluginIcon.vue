<script setup lang="ts">
import AppIcon from './AppIcon.vue'
defineProps<{ id: string }>()
</script>
<template><AppIcon :name="id.startsWith('wishingskin.') ? id.slice('wishingskin.'.length) : 'plugin'" /></template>
