<script setup lang="ts">
import { ref } from 'vue'
import AppearanceSettings from './AppearanceSettings.vue'
import LoginImageSettings from './LoginImageSettings.vue'
import ServiceSettings from './ServiceSettings.vue'
import type { SiteAccess } from '../accountTypes'
import { t } from '../i18n'
defineProps<{ site: SiteAccess; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const tab = ref<'site' | 'appearance' | 'services'>('site')
const labels = { site: '站点', appearance: '个性化', services: '邮件与验证' }
</script>
<template>
  <nav class="settings-tabs" :aria-label="t('设置分类')"><button v-for="item in (['site', 'appearance', 'services'] as const)" :key="item" class="secondary-button" type="button" :class="{ active: tab === item }" :aria-current="tab === item ? 'page' : undefined" @click="tab = item">{{ t(labels[item]) }}</button></nav>
  <div v-show="tab === 'site'"><slot /></div>
  <template v-if="tab === 'appearance'">
    <AppearanceSettings :appearance="site.appearance" :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
    <LoginImageSettings :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
  </template>
  <ServiceSettings v-if="tab === 'services'" :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
</template>
