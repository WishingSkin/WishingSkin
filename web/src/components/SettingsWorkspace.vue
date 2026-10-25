<script setup lang="ts">
import { ref } from 'vue'
import AppearanceSettings from './AppearanceSettings.vue'
import ServiceSettings from './ServiceSettings.vue'
import OIDCSettings from './OIDCSettings.vue'
import type { SiteAccess } from '../accountTypes'
import { t } from '../i18n'
defineProps<{ site: SiteAccess; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const tab = ref<'site' | 'appearance' | 'services' | 'oidc'>('site')
const labels = { site: '站点', appearance: '个性化', services: '邮件与验证', oidc: 'OIDC 登录' }
</script>
<template>
  <nav class="settings-tabs" :aria-label="t('设置分类')"><button v-for="item in (['site', 'appearance', 'services', 'oidc'] as const)" :key="item" class="secondary-button" type="button" :class="{ active: tab === item }" :aria-current="tab === item ? 'page' : undefined" @click="tab = item">{{ t(labels[item]) }}</button></nav>
  <div v-show="tab === 'site'"><slot /></div>
  <AppearanceSettings v-if="tab === 'appearance'" :appearance="site.appearance" :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
  <ServiceSettings v-if="tab === 'services'" :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
  <OIDCSettings v-if="tab === 'oidc'" :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
</template>
