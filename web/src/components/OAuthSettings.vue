<script setup lang="ts">
import { feedbackRef, confirmAction, toast } from '../feedback'
import AppModal from './AppModal.vue'
import CopyButton from './CopyButton.vue'
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'
import { defaultNewOAuthClientScopes, defaultOAuthClientScopes, oauthScopeCatalog } from '../oauthScopes'
import { settingsSnapshot } from '../settingsDraft'
import { oneTimeSecretModalConfirm, useUnsavedGuard } from '../unsavedGuard'

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()

interface Client {
  id: string
  name: string
  redirectUris: string[]
  public: boolean
  enabled: boolean
  allowedScopes?: string[]
}

const clients = ref<Client[]>([])
const availableScopeIds = ref<string[]>(oauthScopeCatalog.map(item => item.id))
const busy = ref(false)
const loaded = ref(false)
const error = feedbackRef('error')
const issued = ref('')

function emptyDraft(): Client {
  return {
    id: '',
    name: '',
    redirectUris: [],
    public: false,
    enabled: true,
    allowedScopes: [...defaultNewOAuthClientScopes],
  }
}

const draft = ref<Client>(emptyDraft())
const redirects = ref('')
const savedSnapshot = ref('')
const savedClientScopes = ref<string[]>([])

const identityScopes = computed(() => scopeList('identity'))
const apiScopes = computed(() => scopeList('api'))

function scopeList(group: 'identity' | 'api') {
  const allowed = new Set(availableScopeIds.value)
  return oauthScopeCatalog.filter(item => item.group === group && (!allowed.size || allowed.has(item.id)))
}

function normalizedClient(client?: Client): Client {
  if (!client) return emptyDraft()
  const scopes = client.allowedScopes?.length ? [...client.allowedScopes] : [...defaultOAuthClientScopes]
  return { ...client, allowedScopes: scopes }
}

function draftComparable() {
  return {
    id: draft.value.id,
    name: draft.value.name,
    public: draft.value.public,
    enabled: draft.value.enabled,
    allowedScopes: [...(draft.value.allowedScopes || [])].sort(),
    redirects: redirects.value,
  }
}

const dirty = computed(() => loaded.value && savedSnapshot.value !== '' && settingsSnapshot(draftComparable()) !== savedSnapshot.value)
useUnsavedGuard(dirty, '/admin/plugins/wishingskin.oauth2')

function edit(client?: Client) {
  issued.value = ''
  const next = normalizedClient(client)
  draft.value = next
  redirects.value = next.redirectUris.join('\n')
  savedClientScopes.value = [...(next.allowedScopes || [])]
  savedSnapshot.value = settingsSnapshot(draftComparable())
}

function toggleScope(id: string) {
  const current = new Set(draft.value.allowedScopes || [])
  if (current.has(id)) current.delete(id)
  else current.add(id)
  const ordered = oauthScopeCatalog.map(item => item.id).filter(item => current.has(item))
  for (const extra of current) {
    if (!ordered.includes(extra)) ordered.push(extra)
  }
  draft.value.allowedScopes = ordered
}

async function load() {
  try {
    const r = await fetch('/api/admin/oauth/clients', { credentials: 'same-origin' })
    if (r.status === 401) { emit('expired'); return }
    const data = await r.json()
    if (!r.ok) throw new Error(data.error)
    clients.value = data.clients || []
    if (Array.isArray(data.availableScopes) && data.availableScopes.length) {
      availableScopeIds.value = data.availableScopes
    }
    if (!loaded.value) {
      loaded.value = true
      edit(clients.value[0])
    } else if (draft.value.id) {
      const matched = clients.value.find(c => c.id === draft.value.id)
      if (matched && !dirty.value) edit(matched)
    }
  } catch (e) {
    error.value = (e as Error).message || '暂时无法读取应用'
  }
}

async function save() {
  if (busy.value) return
  const scopes = draft.value.allowedScopes || []
  if (!scopes.length) {
    error.value = '请至少选择一项权限范围'
    return
  }
  if (draft.value.id) {
    const narrowed = savedClientScopes.value.some(scope => !scopes.includes(scope))
    const message = narrowed
      ? t('保存应用设置并撤销现有授权？已授权的用户需要重新同意。')
      : t('保存应用设置并撤销现有授权？')
    if (!await confirmAction({ title: '修改应用', message })) return
  }
  busy.value = true
  error.value = ''
  issued.value = ''
  try {
    const r = await fetch('/api/admin/oauth/clients' + (draft.value.id ? '/' + draft.value.id : ''), {
      method: draft.value.id ? 'PATCH' : 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({
        ...draft.value,
        redirectUris: redirects.value.split('\n').map(s => s.trim()).filter(Boolean),
        allowedScopes: scopes,
      }),
    })
    if (r.status === 401) { emit('expired'); return }
    const data = await r.json()
    if (!r.ok) throw new Error(data.error)
    toast('应用设置已保存。')
    issued.value = data.clientSecret || ''
    const savedClient = normalizedClient(data.client)
    draft.value = savedClient
    redirects.value = savedClient.redirectUris.join('\n')
    savedClientScopes.value = [...(savedClient.allowedScopes || [])]
    savedSnapshot.value = settingsSnapshot(draftComparable())
    await load()
  } catch (e) {
    error.value = (e as Error).message || '暂时无法保存应用'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="settings-section oauth-settings">
    <div class="admin-record-heading">
      <h3>{{ t('OAuth2 授权服务') }}</h3>
      <button class="secondary-button" type="button" :disabled="busy" @click="edit()">{{ t('添加应用') }}</button>
    </div>

    <div v-if="clients.length" class="admin-list oauth-client-list">
      <div
        v-for="client in clients"
        :key="client.id"
        class="oauth-client-row"
        :class="{ active: draft.id === client.id }"
      >
        <div class="oauth-client-meta">
          <strong>{{ client.name }}</strong>
          <span class="oauth-client-sub">
            {{ t(client.enabled ? '已启用' : '已停用') }} / {{ t(client.public ? '公开' : '机密') }} / {{ t('{n} 项权限', { n: (client.allowedScopes?.length ? client.allowedScopes : defaultOAuthClientScopes).length }) }}
          </span>
        </div>
        <button class="secondary-button" type="button" :disabled="busy" @click="edit(client)">{{ t('编辑') }}</button>
      </div>
    </div>

    <form class="settings-form" @submit.prevent="save">
      <fieldset :disabled="busy">
        <fieldset class="oauth-form-group">
          <legend>{{ t('基本信息') }}</legend>
          <label class="binding-field"><span>{{ t('应用名称') }}</span><input v-model="draft.name" maxlength="128" required /></label>
          <label class="binding-field"><span>{{ t('回调地址，每行一个') }}</span><textarea v-model="redirects" class="oauth-redirects" rows="3" required /></label>
          <div class="toggle-setting plugin-settings-toggle">
            <div class="toggle-setting__text">
              <label class="toggle-setting__title" for="oauth-app-enabled-switch">{{ t('启用应用') }}</label>
            </div>
            <input id="oauth-app-enabled-switch" v-model="draft.enabled" class="switch" type="checkbox" role="switch" />
          </div>
        </fieldset>

        <fieldset class="oauth-form-group">
          <legend>{{ t('客户端类型') }}</legend>
          <div class="segmented" role="radiogroup" :aria-label="t('客户端类型')">
            <label><input v-model="draft.public" type="radio" name="oauth-client-type" :value="false" :disabled="!!draft.id" />{{ t('机密') }}</label>
            <label><input v-model="draft.public" type="radio" name="oauth-client-type" :value="true" :disabled="!!draft.id" />{{ t('公开') }}</label>
          </div>
          <p class="password-rule">{{ t('公开客户端无需密钥，强制使用 PKCE S256；客户端类型创建后不可修改。') }}</p>
        </fieldset>

        <fieldset class="oauth-form-group">
          <legend>{{ t('权限范围') }}</legend>
          <div class="oauth-scope-group">
            <h4>{{ t('身份') }}</h4>
            <label v-for="scope in identityScopes" :key="scope.id" class="settings-check oauth-scope-option">
              <input type="checkbox" :checked="draft.allowedScopes?.includes(scope.id)" @change="toggleScope(scope.id)" />
              <span><strong>{{ t(scope.label) }}</strong><small>{{ t(scope.detail) }}</small></span>
            </label>
          </div>
          <div class="oauth-scope-group">
            <h4>{{ t('只读 API') }}</h4>
            <label v-for="scope in apiScopes" :key="scope.id" class="settings-check oauth-scope-option">
              <input type="checkbox" :checked="draft.allowedScopes?.includes(scope.id)" @change="toggleScope(scope.id)" />
              <span><strong>{{ t(scope.label) }}</strong><small>{{ t(scope.detail) }}</small></span>
            </label>
          </div>
        </fieldset>

        <div v-if="draft.id" class="minecraft-redirect">
          <dl class="oidc-redirect"><dt>Client ID</dt><dd><code>{{ draft.id }}</code></dd></dl>
          <CopyButton :text="draft.id" :label="t('复制 Client ID')" />
        </div>

        <button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存应用') }}</button>
      </fieldset>
    </form>

    <AppModal v-if="issued" :title="t('应用密钥')" :confirm-close="oneTimeSecretModalConfirm" @close="issued = ''">
      <div>
        <p>{{ t('Client Secret 仅显示一次，请妥善保存。') }}</p>
        <div class="secret-copy-row">
          <code class="totp-secret">{{ issued }}</code>
          <CopyButton :text="issued" :label="t('复制应用密钥')" />
        </div>
        <button type="button" class="text-button" @click="issued = ''">{{ t('我已保存') }}</button>
      </div>
    </AppModal>
  </section>
</template>
