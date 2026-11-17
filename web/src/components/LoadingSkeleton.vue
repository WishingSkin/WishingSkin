<script setup lang="ts">
import { t } from '../i18n'
withDefaults(defineProps<{ layout?: 'cards' | 'rows' | 'characters'; label?: string; count?: number }>(), { layout: 'rows', label: '正在加载…', count: 3 })
</script>
<template>
  <div class="loading-skeleton" :class="['skeleton-' + layout, { 'material-cards': layout === 'cards' }]" role="status" :aria-label="t(label)">
    <span class="sr-only">{{ t(label) }}</span>
    <div v-for="item in count" :key="item" class="skeleton-item" aria-hidden="true"><span class="skeleton-block skeleton-picture"></span><span class="skeleton-copy"><span class="skeleton-block skeleton-line"></span><span class="skeleton-block skeleton-line skeleton-line-short"></span></span></div>
  </div>
</template>
