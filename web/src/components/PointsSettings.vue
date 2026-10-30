<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { ref, watch } from 'vue'
import type { PointRules } from '../pointTypes'
import { t } from '../i18n'

const props = defineProps<{ rules: PointRules; csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const enabled = ref(false)
const dailyReward = ref(0)
const uploadCost = ref(0)
const characterCost = ref(0)
const busy = ref(false)
const error = feedbackRef('error')
const message = feedbackRef('success')
watch([() => props.rules.enabled, () => props.rules.dailyReward, () => props.rules.uploadCost, () => props.rules.characterCost], ([active, reward, upload, character]) => { enabled.value = active; dailyReward.value = reward; uploadCost.value = upload; characterCost.value = character }, { immediate: true })
async function save() {
  if (busy.value) return
  error.value = ''; message.value = ''
  if (![dailyReward.value, uploadCost.value, characterCost.value].every(value => Number.isInteger(value) && value >= 0 && value <= 1000000000)) { error.value = '积分规则须为 0–1000000000 的整数'; return }
  busy.value = true
  try {
    const response = await fetch('/api/admin/points', { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ enabled: enabled.value, dailyReward: dailyReward.value, uploadCost: uploadCost.value, characterCost: characterCost.value }) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '管理操作未完成，请重试'; return }
    message.value = '积分规则已保存。'; emit('updated')
  } catch { error.value = '无法确认操作结果，请刷新列表后检查。' }
  finally { busy.value = false }
}
</script>

<template>
  <section class="points-settings"><h2>{{ t('积分与签到') }}</h2><p>{{ t('设置签到奖励、上传材质与新建角色费用。') }}</p><p>{{ t('签到按 Asia/Shanghai 时区每日 00:00 重置。') }}</p>
    <form class="report-fields" @submit.prevent="save"><label class="visibility-choice"><input v-model="enabled" type="checkbox" :disabled="busy" /><span>{{ t('启用积分与签到') }}</span></label><label class="binding-field"><span>{{ t('每日签到奖励') }}</span><input v-model.number="dailyReward" type="number" min="0" max="1000000000" step="1" required :disabled="busy" /></label><label class="binding-field"><span>{{ t('上传材质费用') }}</span><input v-model.number="uploadCost" type="number" min="0" max="1000000000" step="1" required :disabled="busy" /></label><label class="binding-field"><span>{{ t('新建角色费用') }}</span><input v-model.number="characterCost" type="number" min="0" max="1000000000" step="1" required :disabled="busy" /></label><p>{{ t('各项可设为 0；关闭后保留余额和历史流水。') }}</p><button class="primary-button" type="submit" :disabled="busy">{{ busy ? t('正在保存…') : t('保存积分规则') }}</button></form>
  </section>
</template>
