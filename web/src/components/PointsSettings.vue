<script setup lang="ts">
import { confirmAction, feedbackRef, toast } from '../feedback'
import CopyButton from './CopyButton.vue'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import type { PointCodeItem, PointRules } from '../pointTypes'
import { locale, t } from '../i18n'
import { settingsSnapshot } from '../settingsDraft'
import { useUnsavedGuard } from '../unsavedGuard'

const props = defineProps<{ rules: PointRules; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()

const enabled = ref(false)
const dailyReward = ref(0)
const uploadCost = ref(0)
const characterCost = ref(0)
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')

const savedRules = ref('')
const rulesDirty = computed(() => savedRules.value !== '' && settingsSnapshot({
  enabled: enabled.value,
  dailyReward: dailyReward.value,
  uploadCost: uploadCost.value,
  characterCost: characterCost.value,
}) !== savedRules.value)
useUnsavedGuard(rulesDirty, '/admin/plugins', 'plugin:wishingskin.points')

watch(
  [() => props.rules.enabled, () => props.rules.dailyReward, () => props.rules.uploadCost, () => props.rules.characterCost],
  ([active, reward, upload, character]) => {
    enabled.value = active
    dailyReward.value = reward
    uploadCost.value = upload
    characterCost.value = character
    savedRules.value = settingsSnapshot({ enabled: active, dailyReward: reward, uploadCost: upload, characterCost: character })
  },
  { immediate: true },
)

async function save() {
  if (busy.value) return
  error.value = ''
  message.value = ''
  if (![dailyReward.value, uploadCost.value, characterCost.value].every(value => Number.isInteger(value) && value >= 0 && value <= 1000000000)) {
    error.value = '积分规则须为 0–1000000000 的整数'
    return
  }
  busy.value = true
  try {
    const response = await fetch('/api/admin/points', {
      method: 'PATCH',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({
        enabled: enabled.value,
        dailyReward: dailyReward.value,
        uploadCost: uploadCost.value,
        characterCost: characterCost.value,
      }),
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      error.value = data.error || '管理操作未完成，请重试'
      return
    }
    savedRules.value = settingsSnapshot({
      enabled: enabled.value,
      dailyReward: dailyReward.value,
      uploadCost: uploadCost.value,
      characterCost: characterCost.value,
    })
    message.value = '积分规则已保存。'
    emit('updated')
  } catch {
    error.value = '无法确认操作结果，请刷新列表后检查。'
  } finally {
    busy.value = false
  }
}

interface GeneratedPointCodes {
  codes: string[]
  points: number
  maxUses: number
  expiresAt: number
  note: string
}

const count = ref(10)
const codePoints = ref(50)
const codeNote = ref('')
const codeMode = ref<'single' | 'custom'>('single')
const maxUses = ref(5)
const hours = ref(72)
const codeBusy = ref(false)
const codesLoading = ref(false)
const recentCodes = ref<PointCodeItem[]>([])
const generatedResult = ref<GeneratedPointCodes | null>(null)
const resultRef = ref<HTMLElement | null>(null)

const ruleSummary = computed(() => {
  const pts = Number.isInteger(codePoints.value) && codePoints.value >= 1 ? codePoints.value : 1
  if (codeMode.value === 'single') {
    return t('每码 +{p} 积分，可用 1 次，永久有效。服务器保存加密值，可在此页重复导出。', { p: pts })
  }
  const usesValue = Number.isInteger(maxUses.value) && maxUses.value >= 1 ? maxUses.value : 1
  const hoursValue = Number.isInteger(hours.value) && hours.value >= 0 ? hours.value : 0
  if (hoursValue === 0) {
    return t('每码 +{p} 积分，可用 {n} 次，永久有效。', { p: pts, n: usesValue })
  }
  return t('每码 +{p} 积分，可用 {n} 次，生成后 {h} 小时内有效。', { p: pts, n: usesValue, h: hoursValue })
})

const statusLabels: Record<PointCodeItem['status'], string> = {
  available: '可用',
  exhausted: '已用完',
  expired: '已过期',
  disabled: '已停用',
}

function formatDate(seconds: number) {
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(seconds * 1000))
}

async function loadCodes() {
  codesLoading.value = true
  try {
    const response = await fetch('/api/admin/points/codes?limit=10', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({}))
    if (!response.ok) { error.value = data.error || '暂时无法读取活动积分码'; return }
    recentCodes.value = Array.isArray(data.codes) ? data.codes : []
  } catch {
    error.value = '暂时无法读取活动积分码'
  } finally {
    codesLoading.value = false
  }
}

async function generateCodes() {
  if (codeBusy.value || busy.value) return
  error.value = ''
  message.value = ''
  if (!Number.isInteger(count.value) || count.value < 1 || count.value > 100) {
    error.value = '批量数量须为 1–100'
    return
  }
  if (!Number.isInteger(codePoints.value) || codePoints.value < 1 || codePoints.value > 1000000000) {
    error.value = '每码积分须为 1–1000000000 的整数'
    return
  }
  const payload: Record<string, unknown> = {
    count: count.value,
    points: codePoints.value,
    note: codeNote.value.trim(),
  }
  if (codeMode.value === 'custom') {
    if (!Number.isInteger(maxUses.value) || maxUses.value < 1 || maxUses.value > 10000) {
      error.value = '每码可用次数须为 1–10000 的整数'
      return
    }
    if (!Number.isInteger(hours.value) || hours.value < 0 || hours.value > 8760) {
      error.value = '有效时长须为 0–8760 小时的整数'
      return
    }
    payload.maxUses = maxUses.value
    payload.hours = hours.value
  }
  codeBusy.value = true
  try {
    const response = await fetch('/api/admin/points/codes', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify(payload),
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as {
      error?: string
      codes?: string[]
      points?: number
      maxUses?: number
      expiresAt?: number
      note?: string
    }
    if (!response.ok) { error.value = data.error || '暂时无法生成活动积分码'; return }
    generatedResult.value = {
      codes: Array.isArray(data.codes) ? data.codes : [],
      points: typeof data.points === 'number' ? data.points : codePoints.value,
      maxUses: typeof data.maxUses === 'number' ? data.maxUses : (codeMode.value === 'single' ? 1 : maxUses.value),
      expiresAt: typeof data.expiresAt === 'number' ? data.expiresAt : 0,
      note: data.note || '',
    }
    message.value = '活动积分码已生成，可随时从服务器重复导出。'
    await loadCodes()
    await nextTick()
    resultRef.value?.focus()
  } catch {
    error.value = '无法确认操作结果，请刷新列表后检查。'
  } finally {
    codeBusy.value = false
  }
}

async function disableCode(item: PointCodeItem) {
  if (codeBusy.value || item.status === 'disabled') return
  if (!await confirmAction({
    title: '停用活动积分码',
    message: t('停用积分码 {code}？停用后无法继续兑换。', { code: item.code }),
    danger: true,
    confirmLabel: '停用',
  })) return
  codeBusy.value = true
  error.value = ''
  try {
    const response = await fetch(`/api/admin/points/codes/${encodeURIComponent(item.id)}/disable`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      error.value = data.error || '暂时无法停用积分码'
      return
    }
    toast('活动积分码已停用。')
    await loadCodes()
  } catch {
    error.value = '暂时无法停用积分码'
  } finally {
    codeBusy.value = false
  }
}

async function downloadCodes(status: 'available' | 'all', format: 'txt' | 'csv') {
  error.value = ''
  message.value = ''
  try {
    const response = await fetch(`/api/admin/points/codes/export?status=${status}&format=${format}`, {
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      error.value = data.error || '暂时无法导出活动积分码'
      return
    }
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `wishingskin-point-codes-${status}.${format}`
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    message.value = '活动积分码导出已开始。'
  } catch {
    error.value = '暂时无法导出活动积分码'
  }
}

onMounted(loadCodes)
</script>

<template>
  <section class="points-settings">
    <h2>{{ t('积分与签到') }}</h2>
    <p>{{ t('设置签到奖励、上传材质与新建角色费用。') }}</p>
    <p>{{ t('签到按 Asia/Shanghai 时区每日 00:00 重置。') }}</p>
    <form class="report-fields" @submit.prevent="save">
      <label class="visibility-choice"><input v-model="enabled" type="checkbox" :disabled="busy" /><span>{{ t('启用积分与签到') }}</span></label>
      <label class="binding-field"><span>{{ t('每日签到奖励') }}</span><input v-model.number="dailyReward" type="number" min="0" max="1000000000" step="1" required :disabled="busy" /></label>
      <label class="binding-field"><span>{{ t('上传材质费用') }}</span><input v-model.number="uploadCost" type="number" min="0" max="1000000000" step="1" required :disabled="busy" /></label>
      <label class="binding-field"><span>{{ t('新建角色费用') }}</span><input v-model.number="characterCost" type="number" min="0" max="1000000000" step="1" required :disabled="busy" /></label>
      <p>{{ t('各项可设为 0；关闭后保留余额和历史流水。') }}</p>
      <button class="primary-button" type="submit" :disabled="busy">{{ busy ? t('正在保存…') : t('保存积分规则') }}</button>
    </form>

    <section class="settings-section point-codes">
      <h3>{{ t('活动积分码') }}</h3>
      <p class="password-rule">{{ t('生成单次或多次可兑换的活动积分码，分发给用户充值积分。') }}</p>
      <p v-if="!rules.enabled" class="password-rule">{{ t('请先在上方保存并启用积分规则后再生成新积分码。') }}</p>

      <form v-if="rules.enabled" class="settings-form plugin-generation" @submit.prevent="generateCodes">
        <fieldset :disabled="codeBusy || busy">
          <h4>{{ t('批量生成') }}</h4>
          <div class="settings-grid">
            <label class="binding-field"><span>{{ t('生成数量') }}</span><input v-model.number="count" type="number" min="1" max="100" step="1" required /></label>
            <label class="binding-field"><span>{{ t('每码积分') }}</span><input v-model.number="codePoints" type="number" min="1" max="1000000000" step="1" required /></label>
          </div>
          <label class="binding-field"><span>{{ t('活动备注（可选）') }}</span><input v-model="codeNote" maxlength="48" :placeholder="t('例如：周年庆活动')" /></label>
          <fieldset class="invitation-mode">
            <legend>{{ t('生成规则') }}</legend>
            <div class="segmented">
              <label><input v-model="codeMode" type="radio" name="point-code-mode" value="single" />{{ t('单次永久') }}</label>
              <label><input v-model="codeMode" type="radio" name="point-code-mode" value="custom" />{{ t('自定义') }}</label>
            </div>
          </fieldset>
          <div v-if="codeMode === 'custom'" class="invitation-custom">
            <label class="binding-field"><span>{{ t('每码可用次数') }}</span><input v-model.number="maxUses" type="number" inputmode="numeric" min="1" max="10000" step="1" required /></label>
            <div>
              <label class="binding-field"><span>{{ t('有效时长（小时）') }}</span><input v-model.number="hours" type="number" inputmode="numeric" min="0" max="8760" step="1" required /></label>
              <div class="invitation-presets" role="group" :aria-label="t('常用时长')">
                <button v-for="h in [24, 72, 168, 720]" :key="h" class="secondary-button" type="button" :aria-pressed="hours === h" @click="hours = h">{{ h === 168 ? t('7 天') : h === 720 ? t('30 天') : t('{h} 小时', { h }) }}</button>
              </div>
            </div>
          </div>
          <p class="password-rule">{{ ruleSummary }}</p>
          <button class="secondary-button" type="submit">{{ codeBusy ? t('正在生成…') : t('生成积分码') }}</button>
        </fieldset>
      </form>

      <div v-if="generatedResult && generatedResult.codes.length" ref="resultRef" class="invitation-result" tabindex="-1">
        <h4>{{ t('本次生成') }}</h4>
        <dl class="invitation-rule-meta">
          <div><dt>{{ t('数量') }}</dt><dd>{{ t('{n} 个', { n: generatedResult.codes.length }) }}</dd></div>
          <div><dt>{{ t('每码积分') }}</dt><dd>+{{ generatedResult.points }}</dd></div>
          <div><dt>{{ t('每码可用') }}</dt><dd>{{ t('{n} 次', { n: generatedResult.maxUses }) }}</dd></div>
          <div><dt>{{ t('有效期') }}</dt><dd><time v-if="generatedResult.expiresAt" :datetime="new Date(generatedResult.expiresAt * 1000).toISOString()">{{ t('至 {date}', { date: formatDate(generatedResult.expiresAt) }) }}</time><template v-else>{{ t('永久有效') }}</template></dd></div>
          <div v-if="generatedResult.note"><dt>{{ t('备注') }}</dt><dd>{{ generatedResult.note }}</dd></div>
        </dl>
        <label class="binding-field"><span class="sr-only">{{ t('生成的积分码') }}</span><textarea :value="generatedResult.codes.join('\n')" class="oauth-redirects" readonly rows="5"></textarea></label>
        <div class="settings-actions"><CopyButton :text="generatedResult.codes.join('\n')" :label="t('复制全部')" variant="text" /></div>
      </div>

      <div class="invitation-export">
        <h4>{{ t('重复导出') }}</h4>
        <p class="password-rule">{{ t('可用指未停用、未用完且未过期。') }}</p>
        <div v-for="s in (['available', 'all'] as const)" :key="s" class="invitation-export-row">
          <span>{{ t(s === 'available' ? '可用' : '全部') }}</span>
          <button v-for="f in (['txt', 'csv'] as const)" :key="f" class="secondary-button" type="button" :aria-label="t(s === 'available' ? '导出可用积分码 {f}' : '导出全部积分码 {f}', { f: f.toUpperCase() })" @click="downloadCodes(s, f)">{{ f.toUpperCase() }}</button>
        </div>
      </div>

      <div class="point-codes-recent">
        <div class="admin-record-heading">
          <h4>{{ t('最近活动积分码') }}</h4>
          <button class="secondary-button" type="button" :disabled="codesLoading || codeBusy" @click="loadCodes">{{ t('刷新列表') }}</button>
        </div>
        <div v-if="recentCodes.length" class="admin-list">
          <article v-for="item in recentCodes" :key="item.id" class="admin-event point-code-row">
            <div class="point-code-main">
              <code>{{ item.code }}</code>
              <CopyButton :text="item.code" :label="t('复制积分码')" />
              <strong class="point-code-value">+{{ item.points }}</strong>
              <span class="scope-tag">{{ t(statusLabels[item.status] || item.status) }}</span>
            </div>
            <div class="point-code-meta">
              <span>{{ t('剩余 {rem} / {max}', { rem: item.remainingUses, max: item.maxUses }) }}</span>
              <span>{{ item.expiresAt ? t('至 {date}', { date: formatDate(item.expiresAt) }) : t('永久有效') }}</span>
              <span v-if="item.note" class="point-code-note">{{ item.note }}</span>
            </div>
            <button
              v-if="item.status !== 'disabled'"
              type="button"
              class="text-button danger-text"
              :disabled="codeBusy"
              @click="disableCode(item)"
            >{{ t('停用') }}</button>
          </article>
        </div>
        <p v-else class="password-rule">{{ t('暂无活动积分码。') }}</p>
      </div>
    </section>
  </section>
</template>
