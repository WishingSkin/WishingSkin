<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed } from 'vue'
import { t } from '../i18n'
import { imageData } from './imageInput'
const model = defineModel<string>()
defineProps<{ label: string }>()
const emit = defineEmits<{ processing: [value: boolean] }>()
const error = feedbackRef('error')
const address = computed({ get: () => model.value?.startsWith('data:') ? '' : model.value || '', set: value => { model.value = value; error.value = '' } })
async function pick(event: Event) {
  emit('processing', true); error.value = ''
  try { const value = await imageData(event); if (value) model.value = value }
  catch (e) { error.value = (e as Error).message }
  finally { emit('processing', false) }
}
</script>
<template>
  <div class="icon-image-field">
    <label class="binding-field"><span>{{ t(label) }}</span><input v-model="address" :placeholder="t('HTTPS URL 或 /assets/icon.png')" maxlength="2048" /></label>
    <div class="icon-image-actions"><img v-if="model" :src="model" alt="" referrerpolicy="no-referrer" /><label class="binding-field"><span>{{ t('上传图标') }}</span><input type="file" accept="image/png,image/jpeg" @change="pick" /></label><button class="text-button" type="button" :disabled="!model" @click="model = ''; error = ''">{{ t('默认') }}</button></div>

  </div>
</template>
