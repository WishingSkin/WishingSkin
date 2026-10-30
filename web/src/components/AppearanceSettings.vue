<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed, ref, watch } from 'vue'
import type { Appearance } from '../accountTypes'
import { t } from '../i18n'
import { imageData } from './imageInput'
import { cloneAdvanced } from '../interfaceAppearance'
import { settingsSnapshot } from '../settingsDraft'
const props = defineProps<{ appearance: Appearance; csrfToken: string; managed?: boolean }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
function cloneDraft() { return { ...props.appearance, surfaceEffect: props.appearance.surfaceEffect || 'solid', advanced: cloneAdvanced(props.appearance.advanced) } }
const draft = ref(cloneDraft())
const saved = ref(settingsSnapshot(draft.value))
const dirty = computed(() => settingsSnapshot(draft.value) !== saved.value)
watch(() => props.appearance, () => { if (!dirty.value) { draft.value = cloneDraft(); saved.value = settingsSnapshot(draft.value) } })
const busy = ref(false)
const imageBusy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
async function pick(event: Event) {
  imageBusy.value = true; error.value = ''; message.value = ''
  try { const value = await imageData(event); if (value) draft.value.logoImage = value }
  catch (e) { error.value = (e as Error).message }
  finally { imageBusy.value = false }
}
async function save() {
  if (busy.value || imageBusy.value) return false
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/appearance', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(draft.value) })
    if (response.status === 401) { emit('expired'); return false }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存站点设置'; return false }
    draft.value = { ...data, advanced: cloneAdvanced(data.advanced) }; saved.value = settingsSnapshot(draft.value)
    if (!props.managed) { message.value = '个性化设置已保存。'; emit('updated') }; return true
  } catch { error.value = '无法确认操作结果，请重新检查设置。'; return false }
  finally { busy.value = false }
}
defineExpose({ draft, dirty, busy: computed(() => busy.value || imageBusy.value), save })
</script>
<template>
  <section class="settings-section"><h3>{{ t('个性化') }}</h3>
    <form class="settings-form" @submit.prevent="!managed && save()"><fieldset :disabled="busy || imageBusy">
      <div class="image-setting"><img v-if="draft.logoImage" :src="draft.logoImage" :alt="t('Logo 预览')" width="96" height="96" /><span v-else class="image-placeholder large">W</span><div><label class="binding-field"><span>{{ t('站点 Logo') }}</span><input type="file" accept="image/png,image/jpeg" @change="pick" /></label><button class="text-button" type="button" :disabled="!draft.logoImage" @click="draft.logoImage = ''">{{ t('恢复默认标识') }}</button></div></div>
      <p class="password-rule">{{ t('PNG 或 JPEG，最大 512 KiB、2048×2048；保存为最长 512 px 的正方形 PNG。') }}</p>
      <label class="binding-field"><span>{{ t('主题配色') }}</span><select v-model="draft.accent"><option value="blue">{{ t('蓝色') }}</option><option value="green">{{ t('绿色') }}</option><option value="violet">{{ t('紫色') }}</option><option value="rose">{{ t('玫瑰') }}</option><option value="orange">{{ t('橙色') }}</option><option value="teal">{{ t('青色') }}</option><option value="gray">{{ t('石墨') }}</option></select></label>
      <label class="binding-field"><span>{{ t('站点默认主题') }}</span><select v-model="draft.defaultTheme"><option value="system">{{ t('跟随系统') }}</option><option value="light">{{ t('浅色') }}</option><option value="dark">{{ t('深色') }}</option></select></label>
      <label class="binding-field"><span>{{ t('面板效果') }}</span><select v-model="draft.surfaceEffect"><option value="solid">{{ t('实色') }}</option><option value="translucent">{{ t('半透明') }}</option><option value="glass">{{ t('毛玻璃') }}</option></select></label>
      <button v-if="!managed" class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存个性化设置') }}</button>
    </fieldset></form>
  </section>
</template>
