<script setup lang="ts">
import { feedbackRef } from '../feedback'
import { ref } from 'vue'
import { t } from '../i18n'

interface Item { id: string; textureId: string; uploaderId: string; reporterId: string; reason: string; status: number; reportAt: string | null }
const emit = defineEmits<{ expired: [] }>()
const items = ref<Item[]>([])
const page = ref(1)
const total = ref(0)
const status = ref('')
const query = ref('')
const busy = ref(false)
const loaded = ref(false)
const error = feedbackRef('error')
const statuses = ['待处理', '已处置', '已驳回']
async function load() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const response = await fetch(`/api/admin/legacy-reports?${new URLSearchParams({ page: String(page.value), status: status.value, q: query.value.trim() })}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法读取历史举报'; return }
    const data = await response.json() as { reports: Item[]; total: number }
    items.value = data.reports; total.value = data.total; loaded.value = true
  } catch { error.value = '暂时无法读取历史举报' }
  finally { busy.value = false }
}
function toggle(event: Event) { if ((event.target as HTMLDetailsElement).open && !loaded.value) load() }
function filter() { page.value = 1; load() }
function turn(offset: number) { page.value += offset; load() }
</script>

<template>
  <details class="legacy-reports" @toggle="toggle">
    <summary>{{ t('旧 BS 举报历史（只读）') }}</summary>
    <section class="reports-section" :aria-label="t('旧 BS 举报历史（只读）')">
      <p>{{ t('这里只显示已导入的旧举报。编号和状态来自原站，不关联新站账号或材质；待处理项请在新后台另行核查处置。') }}</p>
      <p>{{ t('原站未记录处理人、处理动作和处理时间。下方为原始时间文本，未换算时区；空值表示缺失，零日期保留原样。') }}</p>
      <form class="admin-search panel" @submit.prevent="filter">
        <label class="binding-field"><span>{{ t('搜索历史举报') }}</span><input v-model="query" maxlength="255" :placeholder="t('原因、材质或用户编号')" :disabled="busy" /></label>
        <label class="binding-field"><span>{{ t('原站举报状态') }}</span><select v-model="status" :disabled="busy"><option value="">{{ t('全部状态') }}</option><option v-for="(label, index) in statuses" :key="index" :value="String(index)">{{ t(label) }}</option></select></label>
        <button class="primary-button" type="submit" :disabled="busy">{{ t('筛选') }}</button><button class="secondary-button" type="button" :disabled="busy" @click="load">{{ t('刷新列表') }}</button>
      </form>
      <div v-if="busy" class="empty-state panel" role="status"><p>{{ t('正在加载历史举报…') }}</p></div>
      <div v-else-if="error" class="empty-state panel" role="alert"><button class="secondary-button" type="button" @click="load">{{ t('重新加载') }}</button></div>
      <div v-else class="panel admin-list">
        <p v-if="!items.length" class="empty-state">{{ t('暂无匹配的历史举报。') }}</p>
        <article v-for="item in items" :key="item.id" class="admin-user report-item">
          <div class="admin-user-copy">
            <h3>{{ t('原站举报') }} #{{ item.id }}</h3><p>{{ t(statuses[item.status] || '未知状态') }}</p>
            <p>{{ t('原站材质编号') }}：{{ item.textureId }}</p><p>{{ t('原站上传者编号') }}：{{ item.uploaderId }}</p><p>{{ t('原站举报人编号') }}：{{ item.reporterId }}</p>
            <p>{{ t('原始举报时间') }}：{{ item.reportAt ?? t('未记录') }}</p>
            <details class="legacy-reason"><summary>{{ t('举报原因（原文）') }}</summary><p class="report-text" tabindex="0">{{ item.reason || t('原因为空。') }}</p></details>
          </div>
        </article>
        <div class="library-pagination"><button class="secondary-button" type="button" :disabled="page <= 1" @click="turn(-1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 条记录', { page, total }) }}</span><button class="secondary-button" type="button" :disabled="page * 20 >= total" @click="turn(1)">{{ t('下一页') }}</button></div>
      </div>
    </section>
  </details>
</template>
