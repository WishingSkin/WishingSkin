<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import type { PluginManifest, PluginSettingField } from '../pluginModel'
import { t } from '../i18n'
import { settingsSnapshot } from '../settingsDraft'
import { useUnsavedGuard } from '../unsavedGuard'
import PluginFrontend from './PluginFrontend.vue'
import CommunityPluginRuntime from './CommunityPluginRuntime.vue'

const props = defineProps<{ plugin: PluginManifest; csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()

const draft = ref<Record<string, string | number | boolean>>({})
const configured = ref<Record<string, boolean>>({})
const busy = ref(false)
const loaded = ref(false)
const savedSnapshot = ref('')
const settingsRevision = ref(0)
const error = feedbackRef('error')
const message = feedbackRef('success')

const dirty = computed(() => loaded.value && savedSnapshot.value !== '' && settingsSnapshot(draft.value) !== savedSnapshot.value)
useUnsavedGuard(dirty, `/admin/plugins/${encodeURIComponent(props.plugin.id)}`, `plugin:${props.plugin.id}`)

function defaultValue(field: PluginSettingField): string | number | boolean {
  if (field.type !== 'secret' && field.default !== undefined) {
    return field.default
  }
  return field.type === 'boolean' ? false : field.type === 'number' ? 0 : ''
}

const hasNonSecretDefaults = computed(() =>
  (props.plugin.settings || []).some(field => field.type !== 'secret' && field.default !== undefined),
)

const canResetDefaults = computed(() =>
  (props.plugin.settings || []).some(field =>
    field.type !== 'secret' && field.default !== undefined && draft.value[field.key] !== field.default,
  ),
)

function resetDefaults() {
  for (const field of props.plugin.settings || []) {
    if (field.type === 'secret' || field.default === undefined) continue
    draft.value[field.key] = field.default
  }
}

function fieldInputId(key: string) {
  return `plugin-setting-${props.plugin.id}-${key}`
}

async function load() {
  try {
    const r = await fetch(`/api/admin/plugin-settings/${encodeURIComponent(props.plugin.id)}`, { credentials: 'same-origin' })
    if (r.status === 401) { emit('expired'); return }
    if (!r.ok) throw new Error()
    const data = await r.json()
    configured.value = data.configured || {}
    const nextDraft: Record<string, string | number | boolean> = {}
    for (const field of props.plugin.settings || []) {
      const val = data.values?.[field.key]
      nextDraft[field.key] = val !== undefined && val !== null ? val : defaultValue(field)
    }
    draft.value = nextDraft
    savedSnapshot.value = settingsSnapshot(draft.value)
    loaded.value = true
  } catch {
    error.value = '暂时无法读取插件配置'
  }
}

async function save() {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const r = await fetch(`/api/admin/plugin-settings/${encodeURIComponent(props.plugin.id)}`, {
      method: 'PATCH',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify(draft.value),
    })
    if (r.status === 401) { emit('expired'); return }
    const data = await r.json().catch(() => ({}))
    if (!r.ok) {
      error.value = data.error || '暂时无法保存插件配置'
      return
    }
    await load()
    settingsRevision.value++
    message.value = '插件配置已保存。'
  } catch {
    error.value = '无法确认操作结果，请重新检查设置。'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <form v-if="plugin.settings?.length" class="settings-form" @submit.prevent="save">
    <fieldset :disabled="!loaded || busy">
      <template v-for="field in plugin.settings" :key="field.key">
        <div v-if="field.type === 'boolean'" class="toggle-setting plugin-settings-toggle">
          <div class="toggle-setting__text">
            <label class="toggle-setting__title" :for="fieldInputId(field.key)">{{ t(field.label) }}</label>
            <p v-if="field.description" class="toggle-setting__desc">{{ t(field.description) }}</p>
          </div>
          <input :id="fieldInputId(field.key)" v-model="draft[field.key]" class="switch" type="checkbox" role="switch" />
        </div>
        <label v-else class="binding-field">
          <span>{{ t(field.label) }}</span>
          <select v-if="field.type === 'text' && field.options?.length" :id="fieldInputId(field.key)" v-model="draft[field.key]">
            <option value="">{{ t('自动 / 未指定') }}</option>
            <option v-for="opt in field.options" :key="opt.value" :value="opt.value">{{ t(opt.label) }}</option>
          </select>
          <input v-else-if="field.type === 'number'" :id="fieldInputId(field.key)" v-model.number="draft[field.key]" type="number" />
          <input
            v-else
            :id="fieldInputId(field.key)"
            v-model="draft[field.key]"
            :type="field.type === 'secret' ? 'password' : 'text'"
            maxlength="4096"
            :autocomplete="field.type === 'secret' ? 'new-password' : 'off'"
            :placeholder="configured[field.key] ? t('留空保留已保存凭据') : ''"
          />
          <small v-if="field.description" class="setting-field-desc">{{ t(field.description) }}</small>
        </label>
      </template>
      <div class="settings-actions">
        <button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存配置') }}</button>
        <button v-if="hasNonSecretDefaults" class="secondary-button" type="button" :disabled="busy || !canResetDefaults" @click="resetDefaults">{{ t('恢复默认值') }}</button>
      </div>
    </fieldset>
  </form>
  <p v-else class="password-rule">{{ t('此插件无需配置。') }}</p>
  <p v-if="error" class="form-error" role="alert"><button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
  <CommunityPluginRuntime :plugin-id="plugin.id" :enabled="plugin.enabled" :version="plugin.version" :csrf-token="csrfToken" @expired="emit('expired')" />
  <PluginFrontend :plugin-id="plugin.id" :enabled="plugin.enabled" :version="plugin.version" :csrf-token="csrfToken" :settings-revision="settingsRevision" @expired="emit('expired')" />
</template>
