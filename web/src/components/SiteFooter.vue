<script setup lang="ts">
import { t } from '../i18n'
defineProps<{ name: string; tagline: string; footer?: string; showMcDisclaimer?: boolean }>()
</script>
<template>
  <footer class="workspace-footer site-footer">
    <div class="footer-primary">
      <div class="footer-main-row">
        <span class="footer-site-name">{{ name }}</span>
        <span v-if="footer" class="footer-custom">{{ footer }}</span>
        <span class="footer-powered">Powered by <a href="https://github.com/WishingSkin/WishingSkin" target="_blank" rel="noopener noreferrer"><strong>WishingSkin</strong></a></span>
      </div>
      <span v-if="showMcDisclaimer" class="footer-mc-disclaimer">{{ t('非 Minecraft 官方产品，未获 Mojang 或 Microsoft 批准或关联。') }}</span>
    </div>
    <span v-if="tagline" class="footer-tagline">{{ tagline }}</span>
  </footer>
</template>
