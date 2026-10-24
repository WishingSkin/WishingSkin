<script setup lang="ts">
import { ref } from 'vue'
import type { Appearance } from '../accountTypes'
import { t } from '../i18n'
import { imageData } from './imageInput'
const props = defineProps<{ appearance: Appearance; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const draft = ref({ ...props.appearance })
const busy = ref(false)
const imageBusy = ref(false)
const error = ref('')
const message = ref('')
async function pick(event: Event) {
  imageBusy.value = true; error.value = ''; message.value = ''
  try { const value = await imageData(event); if (value) draft.value.logoImage = value }
  catch (e) { error.value = (e as Error).message }
  finally { imageBusy.value = false }
}
async function save() {
  if (busy.value || imageBusy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/appearance', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(draft.value) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存站点设置'; return }
    draft.value = data; message.value = '个性化设置已保存。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
</script>
<template>
  <section class="settings-section"><h3>{{ t('个性化') }}</h3><p>{{ t('配置站点标识、配色和默认主题。用户自己的浅深选择优先。') }}</p>
    <form class="settings-form" @submit.prevent="save"><fieldset :disabled="busy || imageBusy">
      <div class="image-setting"><img v-if="draft.logoImage" :src="draft.logoImage" :alt="t('Logo 预览')" width="96" height="96" /><span v-else class="image-placeholder large">W</span><div><label class="binding-field"><span>{{ t('站点 Logo') }}</span><input type="file" accept="image/png,image/jpeg" @change="pick" /></label><button class="text-button" type="button" :disabled="!draft.logoImage" @click="draft.logoImage = ''">{{ t('恢复默认标识') }}</button></div></div>
      <p class="password-rule">{{ t('PNG 或 JPEG，最大 512 KiB、2048×2048；保存为最长 512 px 的正方形 PNG。') }}</p>
      <label class="binding-field"><span>{{ t('主题配色') }}</span><select v-model="draft.accent"><option value="blue">{{ t('蓝色') }}</option><option value="green">{{ t('绿色') }}</option><option value="violet">{{ t('紫色') }}</option></select></label>
      <label class="binding-field"><span>{{ t('站点默认主题') }}</span><select v-model="draft.defaultTheme"><option value="system">{{ t('跟随系统') }}</option><option value="light">{{ t('浅色') }}</option><option value="dark">{{ t('深色') }}</option></select></label>
      <p v-if="error" class="form-error" role="alert">{{ t(error) }}</p><button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存个性化设置') }}</button>
    </fieldset></form><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
  </section>
</template>
