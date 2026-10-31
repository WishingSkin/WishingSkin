<script setup lang="ts">
defineProps<{ name: string; tagline: string; footer?: string }>()
</script>
<template>
  <footer class="workspace-footer site-footer"><span>{{ name }}<span v-if="footer" class="footer-custom">{{ footer }}</span><span class="footer-powered">Powered by <a href="https://github.com/KyanOWO1107/WishingSkin" target="_blank" rel="noopener noreferrer"><strong>WishingSkin</strong></a></span></span><span>{{ tagline }}</span></footer>
</template>
