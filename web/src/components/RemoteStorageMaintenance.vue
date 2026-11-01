<script setup lang="ts">
import { ref } from 'vue'
import { confirmAction, toast } from '../feedback'
import { t } from '../i18n'
const props = defineProps<{ csrfToken: string; running: boolean }>()
const emit = defineEmits<{ expired: []; started: [job: { id: string; kind: string; state: string; total: number; completed: number; failed: number; bytes: number; destination: string; startedAt: number; finishedAt: number }] }>()
const files = ref<{ hash: string; bytes: number }[]>([]), next = ref(''), revision = ref(''), busy = ref(false), scanned = ref(false)
async function preview(cursor = '') {
 busy.value = true
 try { const r = await fetch('/api/admin/storage/remote-orphans?cursor=' + encodeURIComponent(cursor)); if (r.status === 401) emit('expired'); const data = await r.json(); if (!r.ok) throw new Error(data.error); files.value = data.files; next.value = data.next; revision.value = data.revision; scanned.value = true }
 catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false }
}
async function collect() {
 if (!await confirmAction({ title: '远端存储回收', message: t('删除当前页的 {count} 个远端孤立文件？', { count: files.value.length }), danger: true })) return
 busy.value = true
 try { const r = await fetch('/api/admin/operations/remote-gc', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ hashes: files.value.map(f => f.hash), confirmed: true, revision: revision.value }) }); if (r.status === 401) emit('expired'); const data = await r.json(); if (!r.ok) throw new Error(data.error); emit('started', data); files.value = []; scanned.value = false; toast('任务已开始。') }
 catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false }
}
</script>
<template>
 <details class="remote-maintenance"><summary>{{ t('远端存储回收') }}</summary>
  <div class="settings-actions"><button type="button" class="secondary-button" :disabled="busy || running" @click="preview()">{{ t('预览远端孤立文件') }}</button><button v-if="next" type="button" class="secondary-button" :disabled="busy || running" @click="preview(next)">{{ t('下一页') }}</button></div>
  <p v-if="scanned && !files.length">{{ t('当前页没有可回收的文件。') }}</p>
  <template v-if="files.length"><p>{{ t('当前页可回收 {count} 个文件', { count: files.length }) }}</p><ul class="remote-files"><li v-for="file in files" :key="file.hash"><code>{{ file.hash }}</code><span>{{ (file.bytes / 1048576).toFixed(2) }} MiB</span></li></ul><button class="danger-button" type="button" :disabled="busy || running" @click="collect">{{ t('回收当前页') }}</button></template>
 </details>
</template>
<style scoped>
.remote-maintenance { display: grid; gap: 1rem; margin-top: 1rem; }
.remote-maintenance .settings-actions { margin-block: 1rem; }
.remote-files { max-height: 16rem; overflow: auto; padding: 0; list-style: none; }
.remote-files li { display: flex; flex-wrap: wrap; gap: .75rem; margin-block: .5rem; overflow-wrap: anywhere; }
</style>
