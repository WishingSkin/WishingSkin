<script setup lang="ts">
defineProps<{ name: string; tagline: string }>()
</script>
<template>
  <footer class="workspace-footer site-footer"><span>{{ name }}<span class="footer-powered">Powered by <strong>WishingSkin</strong></span></span><span>{{ tagline }}</span></footer>
</template>
