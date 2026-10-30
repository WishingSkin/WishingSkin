<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { PluginManifest } from '../pluginModel'
import { t } from '../i18n'
import CommunityPluginRuntime from './CommunityPluginRuntime.vue'
const props = defineProps<{ plugin: PluginManifest; csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const draft = ref<Record<string, string | number | boolean>>({})
const configured = ref<Record<string, boolean>>({})
const busy = ref(false), loaded = ref(false), error = ref(''), message = ref('')
async function load() {
  try { const r = await fetch(`/api/admin/plugin-settings/${encodeURIComponent(props.plugin.id)}`, { credentials: 'same-origin' }); if (r.status === 401) { emit('expired'); return }; if (!r.ok) throw new Error(); const data = await r.json(); configured.value = data.configured; for (const field of props.plugin.settings || []) draft.value[field.key] = data.values[field.key] ?? (field.type === 'boolean' ? false : field.type === 'number' ? 0 : ''); loaded.value = true }
  catch { error.value = '暂时无法读取插件配置' }
}
async function save() {
  busy.value = true; error.value = ''; message.value = ''
  try { const r = await fetch(`/api/admin/plugin-settings/${encodeURIComponent(props.plugin.id)}`, { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify(draft.value) }); if (r.status === 401) { emit('expired'); return }; const data = await r.json(); if (!r.ok) { error.value = data.error || '暂时无法保存插件配置'; return }; await load(); message.value = '插件配置已保存。' }
  catch { error.value = '无法确认操作结果，请重新检查设置。' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template><form v-if="plugin.settings?.length" class="settings-form" @submit.prevent="save"><fieldset :disabled="!loaded || busy"><label v-for="field in plugin.settings" :key="field.key" class="binding-field"><span>{{ t(field.label) }}</span><input v-if="field.type === 'boolean'" v-model="draft[field.key]" type="checkbox" /><input v-else-if="field.type === 'number'" v-model.number="draft[field.key]" type="number" /><input v-else v-model="draft[field.key]" :type="field.type === 'secret' ? 'password' : 'text'" maxlength="4096" :autocomplete="field.type === 'secret' ? 'new-password' : 'off'" :placeholder="configured[field.key] ? t('留空保留已保存凭据') : ''" /></label><button class="primary-button" type="submit">{{ t('保存配置') }}</button></fieldset></form><p v-else class="password-rule">{{ t('此插件无需配置。') }}</p><p v-if="error" class="form-error" role="alert">{{ t(error) }} <button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p><p v-if="message" class="action-success" role="status">{{ t(message) }}</p><CommunityPluginRuntime :plugin-id="plugin.id" :enabled="plugin.enabled" :csrf-token="csrfToken" @expired="emit('expired')" /></template>
