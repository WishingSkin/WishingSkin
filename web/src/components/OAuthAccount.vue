<script setup lang="ts">
import { feedbackRef, confirmAction, toast } from '../feedback'
import { onMounted, ref } from 'vue'
import { locale, t } from '../i18n'
import { oauthScopeLabel } from '../oauthScopes'

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()

interface Grant {
  id: string
  name: string
  scope: string
  expiresAt?: number
}

const grants = ref<Grant[]>([])
const error = feedbackRef('error')
const busy = ref('')

function scopeTokens(raw: string) {
  return raw.trim().split(/\s+/).filter(Boolean)
}

function formatExpiry(seconds?: number) {
  if (!seconds || seconds <= 0) return ''
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(seconds * 1000))
}

async function load() {
  try {
    const r = await fetch('/api/account/oauth', { credentials: 'same-origin' })
    if (r.status === 401) { emit('expired'); return }
    const d = await r.json()
    if (!r.ok) throw new Error(d.error)
    grants.value = d.grants || []
  } catch (e) {
    error.value = (e as Error).message || '暂时无法读取授权'
  }
}

async function revoke(id: string) {
  if (busy.value || !await confirmAction({ title: '撤销授权', message: t('撤销此应用的账号访问授权？'), danger: true })) return
  error.value = ''
  busy.value = id
  try {
    const r = await fetch('/api/account/oauth/' + encodeURIComponent(id), {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (r.status === 401) { emit('expired'); return }
    if (!r.ok) throw new Error()
    toast('应用授权已撤销。')
    await load()
  } catch {
    error.value = '暂时无法撤销授权'
  } finally {
    busy.value = ''
  }
}

onMounted(load)
</script>

<template>
  <section class="account-password">
    <h3>{{ t('已授权的应用') }}</h3>
    <p v-if="!grants.length">{{ t('暂无应用授权') }}</p>
    <article v-for="grant in grants" :key="grant.id" class="oauth-grant-row">
      <div class="oauth-grant-info">
        <h4>{{ grant.name }}</h4>
        <div class="scope-tags">
          <span v-for="scope in scopeTokens(grant.scope)" :key="scope" class="scope-tag">{{ t(oauthScopeLabel(scope)) }}</span>
        </div>
        <p v-if="grant.expiresAt" class="password-rule">{{ t('授权有效至 {date}', { date: formatExpiry(grant.expiresAt) }) }}</p>
      </div>
      <button class="secondary-button" type="button" :disabled="!!busy" @click="revoke(grant.id)">{{ t('撤销授权') }}</button>
    </article>
  </section>
</template>
