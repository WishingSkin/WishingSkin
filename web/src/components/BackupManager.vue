<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import AppModal from './AppModal.vue'
import { toast } from '../feedback'
import { locale, t } from '../i18n'
const props = defineProps<{ csrfToken: string; revision?: string }>()
const emit = defineEmits<{ expired: [] }>()
type Entry = { id: string; createdAt: string; schema: number; files: number; state: string }
const entries = ref<Entry[]>([]), busy = ref(false), results = ref<Record<string, string>>({}), restore = ref<{ directory: string }>()
async function request(path: string, method = 'GET') {
 const r = await fetch('/api/admin/backups' + path, { method, headers: { 'X-CSRF-Token': props.csrfToken } })
 if (r.status === 401) emit('expired')
 const data = await r.json(); if (!r.ok) throw new Error(data.error); return data
}
async function load() { busy.value = true; try { entries.value = (await request('')).backups } catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false } }
async function action(entry: Entry, kind: 'verify' | 'restore-plan') {
 busy.value = true
 try {
  const data = await request('/' + encodeURIComponent(entry.id) + '/' + kind, 'POST')
  const sourcesVerified = !data.unverifiedFiles
  results.value[entry.id] = t(sourcesVerified ? '校验通过' : '部分来源待核验')
  if (kind === 'restore-plan') restore.value = data
  if (!sourcesVerified) toast('该备份包含无法核验来源的旧材质，可正常恢复，但不计入自动保留份数。', 'info')
  else if (kind === 'verify') toast('备份校验通过。')
 }
 catch (e) { results.value[entry.id] = t('校验未通过'); toast((e as Error).message, 'error') }
 finally { busy.value = false }
}
onMounted(load)
watch(() => props.revision, () => { void load() })
</script>
<template>
 <section class="settings-section backup-manager">
  <div class="settings-actions"><h3>{{ t('备份列表') }}</h3><button type="button" class="text-button" :disabled="busy" @click="load">{{ t('刷新') }}</button></div>
  <p v-if="!entries.length">{{ t('暂无备份') }}</p>
  <article v-for="entry in entries" :key="entry.id" class="backup-row">
   <div><strong>{{ entry.createdAt ? new Date(entry.createdAt).toLocaleString(locale) : entry.id }}</strong><span v-if="entry.state !== 'incomplete'">{{ t('版本 {schema}，{count} 个材质文件', { schema: entry.schema, count: entry.files }) }}</span><span role="status">{{ results[entry.id] || t(entry.state === 'incomplete' ? '备份未完成' : '待校验') }}</span></div>
   <div class="settings-actions"><button type="button" class="secondary-button" :disabled="busy || entry.state === 'incomplete'" @click="action(entry, 'verify')">{{ t('校验备份') }}</button><button type="button" class="secondary-button" :disabled="busy || entry.state === 'incomplete'" @click="action(entry, 'restore-plan')">{{ t('恢复备份') }}</button></div>
  </article>
  <AppModal v-if="restore" :title="t('恢复备份')" @close="restore = undefined">
   <ol><li>{{ t('停止所有站点实例。') }}</li><li>{{ t('配置新的空数据库、新材质目录和插件目录。') }}</li><li><code>wishingskin migrate</code></li><li><code>wishingskin restore --offline --directory &lt;directory&gt;</code></li><li>{{ t('恢复设置密钥，检查结果后启动服务。') }}</li></ol>
   <label class="binding-field"><span>{{ t('所选备份目录') }}</span><input :value="restore.directory" readonly /></label>
   <div class="settings-actions"><button type="button" class="primary-button" @click="restore = undefined">{{ t('知道了') }}</button></div>
  </AppModal>
 </section>
</template>
<style scoped>
.backup-manager { display: grid; gap: 1rem; }
.backup-row { display: flex; flex-wrap: wrap; align-items: center; gap: 1rem; padding-block: 1rem; border-bottom: 1px solid var(--border); }
.backup-row > div:first-child { display: grid; gap: .375rem; flex: 1; min-width: 0; overflow-wrap: anywhere; }
</style>
