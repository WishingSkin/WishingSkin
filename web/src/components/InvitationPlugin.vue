<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import CopyButton from './CopyButton.vue'
import { feedbackRef } from '../feedback'
import { computed, nextTick, onMounted, ref } from 'vue'
import { locale, t } from '../i18n'
import { useUnsavedGuard } from '../unsavedGuard'

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()

interface InvitationOptions {
  enabled: boolean
  required: boolean
  migration?: { total: number; unused: number; used: number }
}

interface GeneratedResult {
  codes: string[]
  maxUses: number
  expiresAt: number
}

const options = ref<InvitationOptions | null>(null)
const enabled = ref(false)
const required = ref(false)
const count = ref(10)
const mode = ref<'single' | 'custom'>('single')
const maxUses = ref(5)
const hours = ref(72)
const result = ref<GeneratedResult | null>(null)
const resultRef = ref<HTMLElement | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
const dirty = computed(() => !!options.value && (enabled.value !== options.value.enabled || required.value !== options.value.required))
useUnsavedGuard(dirty, '/admin/plugins', 'plugin:wishingskin.invitations')

const ruleSummary = computed(() => {
  if (mode.value === 'single') {
    return t('每个邀请码只能使用一次，永久有效。服务器保存加密值，可在此页重复导出。')
  }
  const usesText = Number.isInteger(maxUses.value) && maxUses.value >= 1 ? maxUses.value : 1
  const hoursValue = Number.isInteger(hours.value) && hours.value >= 0 ? hours.value : 0
  if (hoursValue === 0) {
    return t('每码可用 {n} 次，永久有效。', { n: usesText })
  }
  return t('每码可用 {n} 次，生成后 {h} 小时内有效。', { n: usesText, h: hoursValue })
})

function date(seconds: number) {
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(seconds * 1000))
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await fetch('/api/admin/plugins/invitations', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法读取插件设置'; return }
    options.value = data
    enabled.value = data.enabled
    required.value = data.required
  } catch {
    error.value = '暂时无法读取插件设置'
  } finally {
    loading.value = false
  }
}

async function submit(generate: boolean) {
  if (busy.value) return
  error.value = ''
  message.value = ''
  let bodyPayload: Record<string, unknown>
  if (!generate) {
    bodyPayload = { enabled: enabled.value, required: required.value }
  } else {
    if (!Number.isInteger(count.value) || count.value < 1 || count.value > 100) {
      error.value = '批量数量须为 1–100'
      return
    }
    if (mode.value === 'single') {
      bodyPayload = { count: count.value }
    } else {
      if (!Number.isInteger(maxUses.value) || maxUses.value < 1 || maxUses.value > 1000) {
        error.value = '每码可用次数须为 1–1000 的整数'
        return
      }
      if (!Number.isInteger(hours.value) || hours.value < 0 || hours.value > 8760) {
        error.value = '有效时长须为 0–8760 小时的整数'
        return
      }
      bodyPayload = { count: count.value, maxUses: maxUses.value, hours: hours.value }
    }
  }
  busy.value = true
  try {
    const response = await fetch('/api/admin/plugins/invitations' + (generate ? '/codes' : ''), {
      method: generate ? 'POST' : 'PATCH',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify(bodyPayload),
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as {
      error?: string
      codes?: string[]
      maxUses?: number
      remainingUses?: number
      expiresAt?: number
      enabled?: boolean
      required?: boolean
    }
    if (!response.ok) { error.value = data.error || '操作未完成，请重试。'; return }
    if (generate) {
      result.value = {
        codes: Array.isArray(data.codes) ? data.codes : [],
        maxUses: data.maxUses ?? data.remainingUses ?? (mode.value === 'single' ? 1 : maxUses.value),
        expiresAt: typeof data.expiresAt === 'number' ? data.expiresAt : 0,
      }
      message.value = '邀请码已生成，可随时从服务器重复导出。'
      await load()
      await nextTick()
      resultRef.value?.focus()
    } else {
      options.value = { enabled: !!data.enabled, required: !!data.required, migration: options.value?.migration }
      message.value = '插件设置已保存。'
      emit('updated')
    }
  } catch {
    error.value = '无法确认操作结果，请重新检查设置。'
  } finally {
    busy.value = false
  }
}

async function download(status: 'available' | 'all', format: 'txt' | 'csv') {
  error.value = ''
  message.value = ''
  try {
    const response = await fetch(`/api/admin/plugins/invitations/export?status=${status}&format=${format}`, {
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      error.value = data.error || '暂时无法导出邀请码'
      return
    }
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `wishingskin-invitations-${status}.${format}`
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    message.value = '邀请码导出已开始。'
  } catch {
    error.value = '暂时无法导出邀请码'
  }
}

onMounted(load)
</script>

<template>
  <section class="settings-section invitation-plugin">
    <h3>{{ t('邀请码设置') }}</h3>
    <p>{{ t('设置注册邀请码的使用规则。') }}</p>
    <LoadingSkeleton v-if="loading && !options" label="正在加载…" :count="2" />
    <p v-if="error" class="form-error" role="alert"><button v-if="!options && !loading" class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <template v-if="options">
      <form class="settings-form" @submit.prevent="submit(false)">
        <fieldset :disabled="busy">
          <label class="settings-check"><input v-model="enabled" type="checkbox" />{{ t('启用邀请码插件') }}</label>
          <label class="settings-check"><input v-model="required" type="checkbox" :disabled="!enabled" />{{ t('注册时必须填写邀请码') }}</label>
          <button class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存插件设置') }}</button>
        </fieldset>
      </form>

      <form v-if="options.enabled" class="settings-form plugin-generation" @submit.prevent="submit(true)">
        <fieldset :disabled="busy">
          <h3>{{ t('批量生成') }}</h3>
          <div class="settings-grid">
            <label class="binding-field"><span>{{ t('生成数量') }}</span><input v-model.number="count" type="number" min="1" max="100" step="1" required /></label>
          </div>
          <fieldset class="invitation-mode">
            <legend>{{ t('生成规则') }}</legend>
            <div class="segmented">
              <label><input v-model="mode" type="radio" name="invitation-mode" value="single" />{{ t('单次永久') }}</label>
              <label><input v-model="mode" type="radio" name="invitation-mode" value="custom" />{{ t('自定义') }}</label>
            </div>
          </fieldset>
          <div v-if="mode === 'custom'" class="invitation-custom">
            <label class="binding-field"><span>{{ t('每码可用次数') }}</span><input v-model.number="maxUses" type="number" inputmode="numeric" min="1" max="1000" step="1" required /></label>
            <div>
              <label class="binding-field"><span>{{ t('有效时长（小时）') }}</span><input v-model.number="hours" type="number" inputmode="numeric" min="0" max="8760" step="1" required aria-describedby="invitation-hours-hint" /></label>
              <div class="invitation-presets" role="group" :aria-label="t('常用时长')">
                <button v-for="h in [24, 72, 168]" :key="h" class="secondary-button" type="button" :aria-pressed="hours === h" @click="hours = h">{{ h === 168 ? t('7 天') : t('{h} 小时', { h }) }}</button>
              </div>
              <p id="invitation-hours-hint" class="sr-only">{{ t('填 0 表示永久有效') }}</p>
            </div>
          </div>
          <p class="password-rule">{{ ruleSummary }}</p>
          <button class="secondary-button" type="submit">{{ t('生成邀请码') }}</button>
        </fieldset>
      </form>

      <div v-if="result && result.codes.length" ref="resultRef" class="invitation-result" tabindex="-1">
        <h3>{{ t('本次生成') }}</h3>
        <dl class="invitation-rule-meta">
          <div><dt>{{ t('数量') }}</dt><dd>{{ t('{n} 个', { n: result.codes.length }) }}</dd></div>
          <div><dt>{{ t('每码可用') }}</dt><dd>{{ t('{n} 次', { n: result.maxUses }) }}</dd></div>
          <div><dt>{{ t('有效期') }}</dt><dd><time v-if="result.expiresAt" :datetime="new Date(result.expiresAt * 1000).toISOString()">{{ t('至 {date}', { date: date(result.expiresAt) }) }}</time><template v-else>{{ t('永久有效') }}</template></dd></div>
        </dl>
        <label class="binding-field"><span class="sr-only">{{ t('生成的邀请码') }}</span><textarea :value="result.codes.join('\n')" readonly rows="6"></textarea></label>
        <div class="settings-actions"><CopyButton :text="result.codes.join('\n')" :label="t('复制全部')" variant="text" /></div>
      </div>

      <div v-if="options.enabled" class="invitation-export">
        <h3>{{ t('重复导出') }}</h3>
        <p class="password-rule">{{ t('可用指未用完且未过期。') }}</p>
        <div v-for="s in (['available', 'all'] as const)" :key="s" class="invitation-export-row">
          <span>{{ t(s === 'available' ? '可用' : '全部') }}</span>
          <button v-for="f in (['txt', 'csv'] as const)" :key="f" class="secondary-button" type="button" :aria-label="t(s === 'available' ? '导出可用邀请码 {f}' : '导出全部邀请码 {f}', { f: f.toUpperCase() })" @click="download(s, f)">{{ f.toUpperCase() }}</button>
        </div>
      </div>

      <div v-if="options.migration" class="invitation-migration">
        <h3>{{ t('迁移统计') }}</h3>
        <p>{{ t('共 {total} 个，未使用 {unused} 个，已使用 {used} 个。', options.migration) }}</p>
      </div>
    </template>
  </section>
</template>
