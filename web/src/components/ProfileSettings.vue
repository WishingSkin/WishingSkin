<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'
import { imageData } from './imageInput'
interface Profile { nickname: string; avatarMode: 'default' | 'upload' | 'character'; characterUUID: string; avatarImage: string }
const props = defineProps<{ csrfToken: string; characters: { uuid: string; name: string }[] }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const draft = ref<Profile>({ nickname: '', avatarMode: 'default', characterUUID: '', avatarImage: '' })
const loading = ref(true)
const loaded = ref(false)
const busy = ref(false)
const imageBusy = ref(false)
const error = ref('')
const message = ref('')
const length = computed(() => Array.from(draft.value.nickname.trim()).length)
async function load() {
  loading.value = true; error.value = ''
  try {
    const response = await fetch('/api/account/profile', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法读取个人资料'; return }
    draft.value = data; loaded.value = true
  } catch { error.value = '暂时无法读取个人资料' }
  finally { loading.value = false }
}
async function pick(event: Event) {
  imageBusy.value = true; error.value = ''; message.value = ''
  try { const value = await imageData(event); if (value) draft.value.avatarImage = value }
  catch (e) { error.value = (e as Error).message }
  finally { imageBusy.value = false }
}
async function save() {
  if (busy.value || imageBusy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/account/profile', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(draft.value) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存个人资料'; return }
    draft.value = data; message.value = '个人资料已保存。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请重新检查个人资料。' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section class="account-password profile-settings"><h3>{{ t('编辑个人资料') }}</h3><p>{{ t('昵称仅用于站内显示，不改变登录邮箱、角色名或 UUID。') }}</p>
    <p v-if="loading" role="status">{{ t('正在加载…') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }} <button v-if="!loaded && !loading" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <form v-if="loaded" class="settings-form" @submit.prevent="save"><fieldset :disabled="busy || imageBusy">
      <label class="binding-field"><span>{{ t('昵称') }} <small>{{ length }}/64</small></span><input v-model="draft.nickname" required autocomplete="nickname" /></label>
      <label class="binding-field"><span>{{ t('头像来源') }}</span><select v-model="draft.avatarMode"><option value="default">{{ t('默认头像') }}</option><option value="upload">{{ t('上传图片') }}</option><option value="character">{{ t('跟随角色皮肤') }}</option></select></label>
      <template v-if="draft.avatarMode === 'upload'"><label class="binding-field"><span>{{ t('头像图片') }}</span><input type="file" accept="image/png,image/jpeg" @change="pick" /></label><img v-if="draft.avatarImage" class="profile-preview" :src="draft.avatarImage" :alt="t('头像预览')" width="64" height="64" /><p class="password-rule">{{ t('PNG 或 JPEG，最大 512 KiB、2048×2048；保存为 64×64 的正方形 PNG。') }}</p></template>
      <template v-if="draft.avatarMode === 'character'"><label class="binding-field"><span>{{ t('用于头像的角色') }}</span><select v-model="draft.characterUUID" required><option value="" disabled>{{ t('请选择角色') }}</option><option v-if="draft.characterUUID && !characters.some(c => c.uuid === draft.characterUUID)" :value="draft.characterUUID" disabled>{{ t('原角色已不可用，请重新选择') }}</option><option v-for="c in characters" :key="c.uuid" :value="c.uuid">{{ c.name }}</option></select></label><p class="password-rule">{{ t('自动显示该角色当前皮肤的头部；无皮肤、角色删除或图片下架时使用默认头像。') }}</p></template>
      <button class="primary-button" type="submit" :disabled="length < 1 || length > 64 || (draft.avatarMode === 'character' && !characters.some(c => c.uuid === draft.characterUUID))">{{ busy ? t('正在保存…') : t('保存个人资料') }}</button>
    </fieldset></form><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
  </section>
</template>
