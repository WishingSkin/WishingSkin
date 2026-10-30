<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'

const props = defineProps<{ csrfToken: string; managed?: boolean }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const image = ref('')
const saved = ref('')
const dirty = computed(() => loaded.value && image.value !== saved.value)
const loaded = ref(false)
const busy = ref(false)
const imageBusy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')

async function load() {
  busy.value = true; error.value = ''
  try {
    const response = await fetch('/api/admin/login-image', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) throw new Error()
    image.value = (await response.json()).image || ''
    saved.value = image.value
    loaded.value = true
  } catch { error.value = '暂时无法读取登录页图片' }
  finally { busy.value = false }
}

async function pick(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]; input.value = ''
  if (!file) return
  error.value = ''; message.value = ''
  if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 2 * 1024 * 1024) {
    error.value = '登录页图片须为不超过 2 MiB、4096×4096 的 PNG 或 JPEG'; return
  }
  imageBusy.value = true
  try {
    image.value = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result))
      reader.onerror = reject
      reader.readAsDataURL(file)
    })
  } catch { error.value = '图片读取失败，请重新选择' }
  finally { imageBusy.value = false }
}

async function save() {
  if (!loaded.value || busy.value || imageBusy.value) return false
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/login-image', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ image: image.value }) })
    if (response.status === 401) { emit('expired'); return false }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存登录页图片'; return false }
    image.value = data.image; saved.value = image.value; if (!props.managed) { message.value = '登录页图片已保存。'; emit('updated') }; return true
  } catch { error.value = '无法确认操作结果，请重新检查设置。'; return false }
  finally { busy.value = false }
}

function remove() { image.value = ''; error.value = ''; message.value = '' }
onMounted(load)
defineExpose({ dirty, busy: computed(() => busy.value || imageBusy.value), save })
</script>

<template>
  <section class="settings-section login-image-settings" aria-labelledby="login-image-title">
    <h3 id="login-image-title">{{ t('登录页图片') }}</h3>
    <form class="settings-form" @submit.prevent="!managed && save()"><fieldset :disabled="!loaded || busy || imageBusy">
      <img v-if="image" class="login-image-preview" :src="image" :alt="t('登录页图片预览')" />
      <label class="binding-field"><span>{{ t('选择图片') }}</span><input type="file" accept="image/png,image/jpeg" @change="pick" /></label>
      <p class="password-rule">{{ t('可选，显示在登录页左侧。PNG 或 JPEG，最大 2 MiB、4096×4096。') }}</p>
      <div class="login-image-actions">
        <button class="text-button" type="button" :disabled="!image" @click="remove">{{ t('移除图片') }}</button>
        <button v-if="!managed" class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存登录页图片') }}</button>
      </div>
    </fieldset></form>
    <p v-if="error" class="form-error" role="alert"><button v-if="!loaded" class="text-button" type="button" :disabled="busy" @click="load">{{ t('重试') }}</button></p>

  </section>
</template>
