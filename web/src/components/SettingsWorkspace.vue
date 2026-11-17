<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AdvancedAppearanceSettings from './AdvancedAppearanceSettings.vue'
import type { AdvancedAppearance } from '../accountTypes'
import BrandingSettings from './BrandingSettings.vue'
import AppearanceSettings from './AppearanceSettings.vue'
import LoginImageSettings from './LoginImageSettings.vue'
import OperationsSettings from './OperationsSettings.vue'
import ServiceSettings from './ServiceSettings.vue'
import type { SiteAccess } from '../accountTypes'
import { t } from '../i18n'
import type { SettingsTab } from '../workspaceRoutes'
import { saveSettingsSections } from '../settingsDraft'
import type { SettingsSection } from '../settingsDraft'
const props = defineProps<{ site: SiteAccess; csrfToken: string; activeTab: SettingsTab }>()
const emit = defineEmits<{ updated: []; expired: []; navigate: [path: string] }>()
const tab = computed(() => props.activeTab)
const appearanceVisited = ref(false)
watch(tab, value => { if (value === 'appearance') appearanceVisited.value = true }, { immediate: true })
const appearance = ref<SettingsSection & { draft: { advanced: AdvancedAppearance } }>()
const branding = ref<SettingsSection>()
const homepage = ref<SettingsSection>()
const loginImage = ref<SettingsSection>()
const sections = computed(() => [appearance.value, branding.value, homepage.value, loginImage.value].filter((section): section is SettingsSection => !!section))
const dirty = computed(() => sections.value.some(section => section.dirty))
const pending = computed(() => sections.value.some(section => section.busy))
const saving = ref(false)
const saveError = feedbackRef('error')
const savedMessage = feedbackRef('success')
watch(dirty, () => { savedMessage.value = ''; saveError.value = '' })
async function saveAll() {
  if (!dirty.value || saving.value || pending.value) return
  saving.value = true; saveError.value = ''; savedMessage.value = ''
  try {
    if (await saveSettingsSections(sections.value)) savedMessage.value = '全部个性化设置已保存。'
    else saveError.value = '部分设置未保存，请检查对应区块后重试。'
    emit('updated')
  } finally { saving.value = false }
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) event.preventDefault() }
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
const labels = { site: '站点', appearance: '个性化', services: '邮件与验证', operations:'存储与备份' }
</script>
<template>
  <nav class="settings-tabs" :aria-label="t('设置分类')"><button v-for="item in (['site', 'appearance', 'services', 'operations'] as const)" :key="item" class="secondary-button" type="button" :class="{ active: tab === item }" :aria-current="tab === item ? 'page' : undefined" @click="emit('navigate', `/admin/settings/${item}`)">{{ t(labels[item]) }}</button></nav>
  <div data-panel-region="settings">
  <div v-show="tab === 'site'"><slot /></div>
  <div v-if="appearanceVisited" v-show="tab === 'appearance'" class="personalization-workspace">
    <fieldset class="personalization-fields" :disabled="saving" @submit.prevent="saveAll">
      <AppearanceSettings ref="appearance" managed :appearance="site.appearance" :csrf-token="csrfToken" @expired="emit('expired')" />
      <BrandingSettings ref="branding" managed section="site" :site="site" :csrf-token="csrfToken" @expired="emit('expired')" />
      <BrandingSettings ref="homepage" managed section="homepage" :site="site" :csrf-token="csrfToken" @expired="emit('expired')" />
      <LoginImageSettings ref="loginImage" managed :csrf-token="csrfToken" @expired="emit('expired')" />
      <section v-if="appearance" class="settings-section"><AdvancedAppearanceSettings v-model="appearance.draft.advanced" /></section>
    </fieldset>

  </div>
  <Teleport to="body"><div v-if="dirty || saving" class="personalization-save-bar" role="region" :aria-label="t('保存个性化设置')">
    <p role="status">{{ t('有未保存的个性化设置') }}</p>
    <button class="primary-button" type="button" :disabled="saving || pending" @click="saveAll">{{ saving ? t('正在保存…') : t('保存全部') }}</button>
  </div></Teleport>
  <OperationsSettings v-if="tab === 'operations'" :csrf-token="csrfToken" @expired="emit('expired')" />
  <ServiceSettings v-if="tab === 'services'" :csrf-token="csrfToken" @updated="emit('updated')" @expired="emit('expired')" />
  </div>
</template>
