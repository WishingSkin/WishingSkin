<script setup lang="ts">
import { t } from '../i18n'
import AppIcon from './AppIcon.vue'

defineProps<{ label: string; placeholder: string; busy: boolean; activeFilters?: number }>()
const query = defineModel<string>('query', { required: true })
const kind = defineModel<string>('kind', { required: true })
const emit = defineEmits<{ search: [] }>()
</script>

<template>
  <form class="library-filters resource-filters panel" role="search" @submit.prevent="emit('search')">
    <div class="resource-filter-main">
      <label class="binding-field library-search"><span>{{ t(label) }}</span><input v-model="query" maxlength="255" :placeholder="t(placeholder)" /></label>
      <label class="binding-field"><span>{{ t('类型') }}</span><select v-model="kind" @change="emit('search')"><option value="">{{ t('全部材质') }}</option><option value="skin">{{ t('皮肤') }}</option><option value="cape">{{ t('披风') }}</option></select></label>
      <button class="primary-button" type="submit" :disabled="busy">{{ t('搜索') }}</button>
    </div>
    <details v-if="$slots.extra" class="resource-filter-details"><summary><AppIcon name="filters" />{{ t('更多筛选') }}<span v-if="activeFilters" class="filter-count">{{ activeFilters }}</span><AppIcon name="chevron-down" /></summary><div class="resource-filter-options"><slot name="extra" /></div></details>
    <slot name="summary" />
  </form>
</template>
