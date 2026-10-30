<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import AppModal from './AppModal.vue'
import CharacterViewer from './CharacterViewer.vue'
import type { TextureRecord } from '../textureTypes'
import { toast } from '../feedback'
import { t } from '../i18n'
const props = defineProps<{ texture: TextureRecord; csrfToken: string }>()
const emit = defineEmits<{ close: []; applied: []; expired: [] }>()
type Character = { id: number; name: string; skinHash: string; capeHash: string; model: string }
const characters = ref<Character[]>([]), selected = ref<Character>(), busy = ref(false), loading = ref(true)
const failed = ref(false)
const controller = new AbortController()
async function load() {
  loading.value = true; failed.value = false
  try {
    const r = await fetch('/api/characters', { signal: controller.signal, credentials: 'same-origin' })
    if (r.status === 401) { emit('expired'); return }
    const data = await r.json()
    if (controller.signal.aborted) return
    if (!r.ok) { failed.value = true; toast(data.error || '角色暂时无法加载，请重试。', 'error'); return }
    characters.value = data.characters; selected.value = data.characters[0]
  } catch { if (!controller.signal.aborted) { failed.value = true; toast('角色暂时无法加载，请重试。', 'error') } }
  finally { loading.value = false }
}
onMounted(load)
onBeforeUnmount(() => controller.abort())
async function apply() {
  if (!selected.value || busy.value) return
  busy.value = true
  try {
    const r = await fetch(`/api/characters/${selected.value.id}/textures/${props.texture.kind}`, { method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ recordId: props.texture.id }), signal: controller.signal })
    if (r.status === 401) { emit('expired'); return }
    if (!r.ok) { const data = await r.json(); toast(data.error || '操作未完成，请重试。', 'error'); return }
    toast(props.texture.kind === 'skin' ? '皮肤已应用。' : '披风已应用。'); emit('applied'); emit('close')
  } catch { if (!controller.signal.aborted) toast('无法连接到服务，请重试。', 'error') }
  finally { busy.value = false }
}
</script>
<template>
  <AppModal :title="t('应用到角色')" :busy="busy" @close="emit('close')">
    <p>{{ texture.name }}</p><p v-if="loading" class="modal-empty" role="status">{{ t('正在加载角色…') }}</p>
    <div v-else-if="failed" class="modal-empty"><button class="secondary-button" type="button" @click="load">{{ t('重新加载') }}</button></div>
    <form v-else-if="characters.length" class="settings-form" @submit.prevent="apply"><label class="binding-field"><span>{{ t('选择角色') }}</span><select v-model="selected" :disabled="busy"><option v-for="character in characters" :key="character.id" :value="character">{{ character.name }}</option></select></label>
      <CharacterViewer v-if="selected" :name="selected.name" :model="texture.kind === 'skin' ? texture.model : selected.model" :skin-hash="texture.kind === 'skin' ? texture.fileHash : selected.skinHash" :cape-hash="texture.kind === 'cape' ? texture.fileHash : selected.capeHash" />
      <div class="modal-actions"><button class="secondary-button" type="button" :disabled="busy" @click="emit('close')">{{ t('取消') }}</button><button class="primary-button" :disabled="busy || !selected">{{ busy ? t('正在保存…') : t('确认应用') }}</button></div>
    </form><p v-else class="modal-empty">{{ t('请先在角色管理中创建角色。') }}</p>
  </AppModal>
</template>
