<script setup lang="ts">
defineProps<{ id: string }>()
</script>
<template>
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <template v-if="id === 'wishingskin.invitations'"><path d="M3 6h18v4a2 2 0 0 0 0 4v4H3v-4a2 2 0 0 0 0-4Z" /><path d="M15 6v2m0 3v2m0 3v2" /></template>
    <template v-else-if="id === 'wishingskin.points'"><circle cx="12" cy="12" r="9" /><path d="m12 6 1.7 3.5 3.8.5-2.8 2.7.7 3.8-3.4-1.8-3.4 1.8.7-3.8L6.5 10l3.8-.5Z" /></template>
    <template v-else-if="id === 'wishingskin.oidc'"><path d="M12 3 4 6v5c0 5 8 10 8 10s8-5 8-10V6Z" /><circle cx="12" cy="10" r="2" /><path d="M8 16c1-3 7-3 8 0" /></template>
    <template v-else-if="id === 'wishingskin.object-storage'"><path d="m3 7 9-4 9 4-9 4Z" /><path d="M3 7v10l9 4 9-4V7M12 11v10" /></template>
    <template v-else-if="id === 'wishingskin.oauth2'"><circle cx="8" cy="15" r="5" /><path d="m12 11 8-8m-3 3 3 3m-6 0 3 3" /></template>
    <template v-else-if="id === 'wishingskin.connectors'"><path d="m9 15 6-6M8 8l3-3a5 5 0 0 1 7 7l-3 3M16 16l-3 3a5 5 0 0 1-7-7l3-3" /></template>
    <template v-else><path d="M9 3h6v4h4v4h2v4h-2v4h-4v2H9v-2H5v-4H3v-4h2V7h4Z" /><circle cx="12" cy="12" r="2" /></template>
  </svg>
</template>
