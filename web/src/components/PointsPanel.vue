<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { feedbackRef, toast } from '../feedback'
import { onMounted, ref } from 'vue'
import type { PointRules } from '../pointTypes'
import { locale, t } from '../i18n'

interface Entry {
  id: string
  kind: string
  referenceId: string
  note?: string
  delta: string
  balanceAfter: string
  createdAt: number
}
interface View {
  rules: PointRules
  balance: string
  day: string
  timeZone: string
  checkedIn: boolean
  entries: Entry[]
  total: number
}

const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()

const view = ref<View | null>(null)
const page = ref(1)
const busy = ref(false)
const saving = ref(false)
const redeeming = ref(false)
const redeemCode = ref('')
const error = feedbackRef('error')
const actionError = feedbackRef('error')
const message = feedbackRef('success')
const names: Record<string, string> = {
  checkin: '每日签到',
  upload: '上传材质',
  character: '新建角色',
  admin: '管理员调整',
  redeem: '兑换积分码',
}

function time(value: number) {
  return new Date(value * 1000).toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN')
}

async function load() {
  if (busy.value || saving.value || redeeming.value) return
  busy.value = true
  error.value = ''
  try {
    const response = await fetch(`/api/points?page=${page.value}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      error.value = data.error || '暂时无法读取积分'
      return
    }
    view.value = await response.json()
  } catch {
    error.value = '暂时无法读取积分'
  } finally {
    busy.value = false
  }
}

async function checkin() {
  if (saving.value || busy.value || redeeming.value) return
  saving.value = true
  actionError.value = ''
  message.value = ''
  try {
    const response = await fetch('/api/points/checkin', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      actionError.value = data.error || '积分操作暂不可用，请重试'
    } else {
      message.value = '签到成功。'
      page.value = 1
    }
  } catch {
    actionError.value = '无法确认操作结果，请刷新列表后检查。'
  } finally {
    saving.value = false
  }
  await load()
}

async function redeem() {
  const code = redeemCode.value.trim()
  if (!code || redeeming.value || busy.value || saving.value) return
  redeeming.value = true
  actionError.value = ''
  message.value = ''
  try {
    const response = await fetch('/api/points/redeem', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify({ code }),
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as { error?: string; points?: string }
    if (!response.ok) {
      actionError.value = data.error || '兑换失败，请检查积分码'
      return
    }
    toast(t('已兑换 {n} 积分。', { n: data.points || '' }))
    redeemCode.value = ''
    page.value = 1
  } catch {
    actionError.value = '无法确认操作结果，请刷新列表后检查。'
  } finally {
    redeeming.value = false
  }
  await load()
}

function turn(offset: number) {
  page.value += offset
  void load()
}

onMounted(load)
</script>

<template>
  <section class="points-panel reports-section" :aria-label="t('积分与签到')">
    <div class="admin-record-heading">
      <h2>{{ t('积分与签到') }}</h2>
      <button class="secondary-button" type="button" :disabled="busy || saving || redeeming" @click="load">{{ t('刷新列表') }}</button>
    </div>
    <LoadingSkeleton v-if="busy" class="panel" label="正在加载积分…" />
    <div v-else-if="error" class="empty-state panel" role="alert"><button class="secondary-button" type="button" @click="load">{{ t('重新加载') }}</button></div>
    <template v-else-if="view">
      <div class="point-summary panel">
        <p>{{ t('积分余额') }} <strong class="point-balance">{{ view.balance }}</strong></p>
        <template v-if="view.rules.enabled">
          <p>{{ t('每日签到 +{reward}，每次上传 -{upload}，新建角色 -{character}。', { reward: view.rules.dailyReward, upload: view.rules.uploadCost, character: view.rules.characterCost }) }}</p>
          <p>{{ t('北京时间日期：{day}，每日 00:00 重置。', { day: view.day }) }}</p>
          <button class="primary-button" type="button" :disabled="view.checkedIn || saving || redeeming" @click="checkin">{{ saving ? t('正在签到…') : view.checkedIn ? t('今天已经签到') : t('签到领取 {reward} 积分', { reward: view.rules.dailyReward }) }}</button>
          <form class="point-redeem" @submit.prevent="redeem">
            <label class="binding-field">
              <span>{{ t('兑换活动积分码') }}</span>
              <div class="inline-controls">
                <input v-model="redeemCode" maxlength="64" autocomplete="off" spellcheck="false" :placeholder="t('输入活动积分码')" :disabled="redeeming || saving" />
                <button class="secondary-button" type="submit" :disabled="redeeming || saving || !redeemCode.trim()">{{ redeeming ? t('正在兑换…') : t('兑换') }}</button>
              </div>
            </label>
          </form>
        </template>
        <p v-else>{{ t('积分功能已关闭，上传和角色操作不扣费；余额和历史流水保留。') }}</p>
      </div>
      <p class="points-history-note">{{ t('流水记录签到、兑换与扣费，不补造导入余额的历史。') }}</p>
      <div class="admin-list panel">
        <p v-if="!view.entries.length" class="empty-state">{{ t('暂无积分流水。') }}</p>
        <article v-for="entry in view.entries" :key="entry.id" class="admin-event">
          <h3>{{ t(names[entry.kind] || '积分变更') }} <span>{{ entry.delta.startsWith('-') ? entry.delta : `+${entry.delta}` }}</span></h3>
          <time :datetime="new Date(entry.createdAt * 1000).toISOString()">{{ time(entry.createdAt) }}</time>
          <p>{{ t('变更后余额：{balance}', { balance: entry.balanceAfter }) }}</p>
          <p v-if="entry.kind === 'redeem' && entry.note">{{ t('备注') }}：{{ entry.note }}</p>
          <p v-else-if="entry.kind !== 'redeem'">{{ t('关联记录') }}：{{ entry.referenceId }}</p>
        </article>
        <div class="library-pagination">
          <button class="secondary-button" type="button" :disabled="page <= 1 || saving || redeeming" @click="turn(-1)">{{ t('上一页') }}</button>
          <span>{{ t('第 {page} 页，共 {total} 条记录', { page, total: view.total }) }}</span>
          <button class="secondary-button" type="button" :disabled="page * 20 >= view.total || saving || redeeming" @click="turn(1)">{{ t('下一页') }}</button>
        </div>
      </div>
    </template>
  </section>
</template>
