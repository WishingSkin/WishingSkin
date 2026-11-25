<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import CopyButton from './CopyButton.vue'
import { feedbackRef } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'
import { settingsSnapshot } from '../settingsDraft'
import { useUnsavedGuard } from '../unsavedGuard'

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()

interface MinecraftVerifyDraft {
  enabled: boolean
  clientId: string
  clientSecret: string
  secretConfigured: boolean
  clearSecret: boolean
  redirectUri: string
}

function blank(): MinecraftVerifyDraft {
  return {
    enabled: false,
    clientId: '',
    clientSecret: '',
    secretConfigured: false,
    clearSecret: false,
    redirectUri: `${window.location.origin}/api/account/minecraft/callback`,
  }
}

const draft = ref<MinecraftVerifyDraft>(blank())
const savedSnapshot = ref('')
const loaded = ref(false)
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
const dirty = computed(() => loaded.value && savedSnapshot.value !== '' && settingsSnapshot(draft.value) !== savedSnapshot.value)
useUnsavedGuard(dirty, '/admin/plugins/wishingskin.minecraft')

async function load() {
  error.value = ''
  try {
    const response = await fetch('/api/admin/minecraft', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as Partial<MinecraftVerifyDraft> & { error?: string }
    if (!response.ok) { error.value = data.error || '暂时无法读取正版验证设置'; return }
    draft.value = {
      ...blank(),
      enabled: data.enabled === true,
      clientId: data.clientId || '',
      secretConfigured: data.secretConfigured === true,
      clearSecret: false,
      redirectUri: data.redirectUri || blank().redirectUri,
    }
    savedSnapshot.value = settingsSnapshot(draft.value)
    loaded.value = true
  } catch {
    error.value = '暂时无法读取正版验证设置'
  }
}

async function save() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const payload: Record<string, unknown> = {
      enabled: draft.value.enabled,
      clientId: draft.value.clientId.trim(),
      clearSecret: draft.value.clearSecret,
    }
    if (!draft.value.clearSecret && draft.value.clientSecret) {
      payload.clientSecret = draft.value.clientSecret
    }
    const response = await fetch('/api/admin/minecraft', {
      method: 'PATCH',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify(payload),
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as Partial<MinecraftVerifyDraft> & { error?: string }
    if (!response.ok) { error.value = data.error || '暂时无法保存正版验证设置'; return }
    draft.value = {
      ...blank(),
      enabled: data.enabled === true,
      clientId: data.clientId || '',
      secretConfigured: data.secretConfigured === true,
      clearSecret: false,
      redirectUri: data.redirectUri || draft.value.redirectUri,
    }
    savedSnapshot.value = settingsSnapshot(draft.value)
    message.value = '正版验证设置已保存。'
    emit('updated')
  } catch {
    error.value = '无法确认操作结果，请重新检查设置。'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="settings-section minecraft-verify-settings">
    <h3>{{ t('正版验证') }}</h3>
    <p class="password-rule">{{ t('使用 Microsoft 账号验证 Minecraft Java 版持有状态，不改变现有站内角色 UUID。') }}</p>
    <LoadingSkeleton v-if="!loaded && !error" label="正在加载…" :count="2" />
    <p v-if="error" class="form-error" role="alert"><button v-if="!loaded" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <form v-if="loaded" class="settings-form" @submit.prevent="save">
      <fieldset :disabled="busy">
        <div class="toggle-setting">
          <div class="toggle-setting__text">
            <label class="toggle-setting__title" for="mc-verify-enabled">{{ t('启用正版验证') }}</label>
            <p id="mc-verify-enabled-desc" class="toggle-setting__desc">{{ t('开启后，用户可在账号页绑定正版 Minecraft 账号。') }}</p>
          </div>
          <input id="mc-verify-enabled" v-model="draft.enabled" class="switch" type="checkbox" role="switch" aria-describedby="mc-verify-enabled-desc" />
        </div>
        <label class="binding-field"><span>Client ID</span><input v-model.trim="draft.clientId" maxlength="128" :required="draft.enabled" autocomplete="off" spellcheck="false" /></label>
        <label class="binding-field"><span>Client Secret</span><input v-model="draft.clientSecret" type="password" maxlength="1024" autocomplete="new-password" :disabled="draft.clearSecret" :required="draft.enabled && (!draft.secretConfigured || draft.clearSecret)" :placeholder="draft.secretConfigured && !draft.clearSecret ? t('已配置，留空保留') : t('尚未配置')" /></label>
        <label v-if="draft.secretConfigured" class="checkbox-label"><input v-model="draft.clearSecret" type="checkbox" /> {{ t('清除已保存的密钥') }}</label>
        <div class="minecraft-redirect">
          <dl class="oidc-redirect"><dt>{{ t('回调地址') }}</dt><dd>{{ draft.redirectUri }}</dd></dl>
          <CopyButton :text="draft.redirectUri" :label="t('复制回调地址')" />
        </div>
        <details class="minecraft-setup">
          <summary>{{ t('配置说明') }}</summary>
          <ol>
            <li>{{ t('在 Microsoft Entra 管理中心注册应用，账户类型需包含个人 Microsoft 账户。') }}</li>
            <li>{{ t('添加 Web 平台，重定向 URI 填写上方回调地址。') }}</li>
            <li>{{ t('在“证书和密码”中创建客户端密码，填入 Client Secret。') }}</li>
            <li>{{ t('授权范围使用 XboxLive.signin。') }}</li>
            <li>{{ t('新注册的应用需先通过 Minecraft 应用审核，才能读取正版信息。') }}</li>
          </ol>
        </details>
        <button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存正版验证设置') }}</button>
      </fieldset>
    </form>
  </section>
</template>
