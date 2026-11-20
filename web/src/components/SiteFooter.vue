<script setup lang="ts">
import { t } from '../i18n'
defineProps<{ name: string; tagline: string; footer?: string; showMcDisclaimer?: boolean }>()
</script>
<template>
  <footer class="workspace-footer site-footer">
    <span>
      {{ name }}
      <span v-if="footer" class="footer-custom">{{ footer }}</span>
      <span class="footer-powered">Powered by <a href="https://github.com/KyanOWO1107/WishingSkin" target="_blank" rel="noopener noreferrer"><strong>WishingSkin</strong></a></span>
      <span v-if="showMcDisclaimer" class="footer-mc-disclaimer">{{ t('非 Minecraft 官方产品，未获 Mojang 或 Microsoft 批准或关联。') }}</span>
    </span>
    <span>{{ tagline }}</span>
  </footer>
</template>
