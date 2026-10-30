<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { locale, t } from '../i18n'
const props = defineProps<{ pluginId: string; enabled: boolean; csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
interface Task { id: string; schedule: string; status: { state: string; startedAt: number; finishedAt: number; message?: string } }
interface Document { title: string; summary: string; sections: { title: string; body: string; items: { label: string; value: string }[] | null }[] | null }
const pages = ref<{ id: string; label: string }[]>([])
const tasks = ref<Task[]>([])
const selected = ref(''), document = ref<Document>(), loading = ref(false), busy = ref(''), error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
const base = `/api/admin/plugin-runtime/${encodeURIComponent(props.pluginId)}`
function stateLabel(state: string) { return ({ idle: '未运行', running: '正在运行', succeeded: '已完成', failed: '未完成' } as Record<string, string>)[state] || state }
function timeLabel(timestamp: number) { return new Date(timestamp * 1000).toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN') }
async function load() {
  clearTimeout(timer)
  if (!props.enabled) return
  const version = generation
  try {
    const response = await fetch(base, { credentials: 'same-origin' })
    if (version !== generation) return
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) throw new Error()
    const data = await response.json()
    if (version !== generation) return
    pages.value = data.pages; tasks.value = data.tasks
    if (tasks.value.some(task => task.status.state === 'running')) timer = setTimeout(load, 2000)
  } catch { if (version === generation) error.value = '暂时无法读取插件任务' }
}
async function open(id: string) {
  const version = ++generation
  selected.value = id; document.value = undefined; loading.value = true; error.value = ''
  try {
    const response = await fetch(`${base}/pages/${encodeURIComponent(id)}`, { credentials: 'same-origin' })
    if (version !== generation) return
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) throw new Error()
    const data = await response.json()
    if (version === generation) document.value = data
  } catch { if (version === generation) error.value = '暂时无法读取插件页面' }
  finally { if (version === generation) { loading.value = false; await load() } }
}
async function run(task: Task) {
  if (busy.value) return
  busy.value = task.id; error.value = ''
  const version = generation
  try {
    const response = await fetch(`${base}/tasks/${encodeURIComponent(task.id)}`, { method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': props.csrfToken } })
    if (version !== generation) return
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); error.value = data.error || '暂时无法启动插件任务'; return }
    await load()
  } catch { if (version === generation) error.value = '暂时无法启动插件任务' }
  finally { busy.value = '' }
}
watch(() => props.enabled, () => { generation++; clearTimeout(timer); pages.value = []; tasks.value = []; document.value = undefined; selected.value = ''; error.value = ''; loading.value = false; void load() })
onMounted(load)
onBeforeUnmount(() => { generation++; clearTimeout(timer) })
</script>

<template>
  <section v-if="enabled" class="community-runtime" :aria-label="t('插件页面与任务')">
    <div v-if="pages.length" class="runtime-pages">
      <h4>{{ t('插件页面') }}</h4>
      <div class="settings-actions"><button v-for="page in pages" :key="page.id" class="secondary-button" type="button" :aria-expanded="selected === page.id" @click="open(page.id)">{{ t(page.label) }}</button></div>
      <p v-if="loading" role="status">{{ t('正在加载…') }}</p>
      <article v-if="document" class="runtime-document">
        <h4>{{ t(document.title) }}</h4><p v-if="document.summary">{{ t(document.summary) }}</p>
        <section v-for="(section, index) in document.sections || []" :key="index">
          <h5 v-if="section.title">{{ t(section.title) }}</h5><p v-if="section.body">{{ t(section.body) }}</p>
          <dl v-if="section.items?.length"><div v-for="(item, itemIndex) in section.items" :key="itemIndex"><dt>{{ t(item.label) }}</dt><dd>{{ t(item.value) }}</dd></div></dl>
        </section>
      </article>
    </div>
    <div v-if="tasks.length" class="runtime-tasks">
      <h4>{{ t('插件任务') }}</h4>
      <article v-for="task in tasks" :key="task.id" class="runtime-task">
        <div><strong>{{ task.id === 'check-settings' ? t('检查配置') : task.id }}</strong><p>{{ t(task.schedule ? '固定间隔' : '仅手动运行') }}<span v-if="task.schedule"> {{ task.schedule.replace('@every ', '') }}</span></p><p>{{ t(stateLabel(task.status.state)) }}<time v-if="task.status.finishedAt"> {{ timeLabel(task.status.finishedAt) }}</time></p><p v-if="task.status.message" class="form-error">{{ t(task.status.message) }}</p></div>
        <button class="secondary-button" type="button" :disabled="!!busy || task.status.state === 'running'" @click="run(task)">{{ t(task.status.state === 'running' ? '正在运行' : '运行任务') }}</button>
      </article>
    </div>
    <p v-if="error" class="form-error" role="alert">{{ t(error) }} <button class="text-button" type="button" @click="selected ? open(selected) : load()">{{ t('重试') }}</button></p>
  </section>
  <p v-else class="password-rule">{{ t('启用插件后可访问页面和运行任务。') }}</p>
</template>

<style scoped>
.community-runtime{margin-top:24px;font-size:13px;line-height:1.7;min-width:0}
.community-runtime h4,.community-runtime h5{font-size:14px;font-weight:600}
.runtime-tasks{margin-top:24px}
.runtime-document{margin-top:18px;padding:18px;background:var(--subtle);border-radius:6px}
.runtime-document section{margin-top:18px}
.runtime-document p{white-space:pre-wrap;overflow-wrap:anywhere}
.runtime-document dl{margin:10px 0 0}
.runtime-document dl>div{display:grid;grid-template-columns:minmax(90px,1fr) minmax(0,2fr);gap:12px;padding:8px 0;border-bottom:1px solid var(--border)}
.runtime-document dt,.runtime-task p{color:var(--muted)}
.runtime-document dd{margin:0;overflow-wrap:anywhere;white-space:pre-wrap}
.runtime-task{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:16px 0;border-bottom:1px solid var(--border)}
.runtime-task>div{min-width:0;overflow-wrap:anywhere}
.runtime-task button{flex-shrink:0}
.runtime-task time{display:block;font-size:12px}
@media(max-width:560px){.runtime-task{flex-direction:column;gap:10px}.runtime-document{padding:14px}.runtime-document dl>div{grid-template-columns:minmax(0,1fr);gap:3px}}
</style>
