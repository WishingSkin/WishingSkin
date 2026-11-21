<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
import { toast, confirmAction } from '../feedback'
import AppModal from './AppModal.vue'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
interface Manifest { id: string; name?: string; version: string; description?: string; permissions: string[]; dependencies: string[] }
interface Inspection { package: { manifest: Manifest }; digest: string }
interface Entry { id: string; name: string; version: string }
const packages = ref<Inspection[]>([]), catalog = ref<Entry[]>([]), source = ref(''), unavailable = ref(false), busy = ref(false)
const inspection = ref<Inspection>(), approved = ref(false), upload = ref<File>(), marketId = ref('')
const permissionNames: Record<string, string> = { 'routes.public': '公开接口', 'routes.user': '用户接口', 'routes.admin': '管理接口', events: '订阅事件', pages: '管理页面', tasks: '后台任务', frontend: '自定义界面', database: '插件数据与数据库迁移', webhooks: 'Webhook' }
async function request(path: string, method = 'GET', body?: BodyInit, extra: Record<string, string> = {}) {
 const r = await fetch(path, { method, credentials: 'same-origin', headers: { 'X-CSRF-Token': props.csrfToken, ...extra }, body })
 if (r.status === 401) { emit('expired'); throw new Error(t('请重新登录')) }
 const data = await r.json().catch(() => ({}))
 if (!r.ok) {
  if (data.retrySamePackage === true) throw new Error('插件升级未完成，请重新上传同一个 ZIP。')
  throw new Error(data.error || t('操作失败'))
 }
 return data
}
async function load() {
 try { const installed = await request('/api/admin/plugin-packages'); packages.value = installed.packages; const market = await request('/api/admin/plugin-market'); source.value = market.source; catalog.value = market.packages; unavailable.value = !!market.unavailable } catch (e) { toast((e as Error).message, 'error') }
}
async function saveSource() {
 busy.value = true; try { await request('/api/admin/plugin-market', 'PUT', JSON.stringify({ source: source.value }), { 'Content-Type': 'application/json' }); toast('市场源已保存。'); await load() } catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false }
}
async function inspect(file?: File, entry?: Entry) {
 if (!file && !entry) return
 busy.value = true; approved.value = false; inspection.value = undefined; upload.value = file; marketId.value = entry?.id || ''
 try { inspection.value = await request(entry ? '/api/admin/plugin-market/inspect' : '/api/admin/plugin-packages/inspect', 'POST', entry ? JSON.stringify({ id: entry.id }) : file, { 'Content-Type': entry ? 'application/json' : 'application/zip' }) } catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false }
}
function choose(event: Event) { const input = event.target as HTMLInputElement; const file = input.files?.[0]; if (file) void inspect(file); input.value = '' }
async function install() {
 if (!inspection.value || !approved.value) return
 busy.value = true; try { await request(marketId.value ? '/api/admin/plugin-market/install' : '/api/admin/plugin-packages/install', 'POST', marketId.value ? JSON.stringify({ id: marketId.value }) : upload.value, { 'Content-Type': marketId.value ? 'application/json' : 'application/zip', 'X-Plugin-Digest': inspection.value.digest, 'X-Plugin-Permissions': JSON.stringify(inspection.value.package.manifest.permissions) }); inspection.value = undefined; upload.value = undefined; toast('插件已安装。'); await load(); emit('updated') } catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false }
}
async function manage(item: Inspection, remove = false) {
 const id = item.package.manifest.id
 if (!await confirmAction({ title: remove ? '卸载插件' : '重新加载插件', message: t(remove ? '卸载“{name}”？插件数据将保留。' : '重新加载“{name}”？正在运行的任务将结束。', { name: item.package.manifest.name || id }), danger: remove })) return
 busy.value = true; try { await request(`/api/admin/plugin-packages/${encodeURIComponent(id)}${remove ? '' : '/reload'}`, remove ? 'DELETE' : 'POST'); toast(remove ? '插件已卸载。' : '插件已重新加载。'); await load(); emit('updated') } catch (e) { toast((e as Error).message, 'error') } finally { busy.value = false }
}
onMounted(load)
</script>
<template>
 <section class="settings-section package-manager">
  <div class="settings-actions"><label class="secondary-button package-upload">{{ t('安装插件包') }}<input type="file" accept=".zip,application/zip" :disabled="busy" @change="choose" /></label><button type="button" class="text-button" :disabled="busy" @click="load">{{ t('刷新') }}</button></div>
  <details><summary>{{ t('插件市场') }}</summary><form class="settings-form" @submit.prevent="saveSource"><label class="binding-field"><span>{{ t('市场源地址') }}</span><input v-model="source" type="url" placeholder="https://plugins.example.com/catalog.json" /></label><button class="secondary-button" :disabled="busy">{{ t('保存市场源') }}</button></form>
   <p v-if="unavailable" role="status">{{ t('市场暂时不可用') }}</p><p v-else-if="!catalog.length">{{ t(source ? '暂无插件' : '配置市场源后浏览插件') }}</p>
   <div class="extension-list"><article v-for="entry in catalog" :key="entry.id" class="extension-row"><div><strong>{{ entry.name || entry.id }}</strong><span>v{{ entry.version }}</span></div><button class="secondary-button" :disabled="busy" @click="inspect(undefined, entry)">{{ t('检查安装') }}</button></article></div>
  </details>
  <details v-if="packages.length"><summary>{{ t('已安装的插件包') }}</summary><div class="extension-list"><article v-for="item in packages" :key="item.package.manifest.id" class="extension-row"><div><strong>{{ item.package.manifest.name || item.package.manifest.id }}</strong><span>v{{ item.package.manifest.version }}</span></div><div class="settings-actions"><button class="secondary-button" :disabled="busy" @click="manage(item)">{{ t('重新加载') }}</button><button class="danger-button" :disabled="busy" @click="manage(item, true)">{{ t('卸载') }}</button></div></article></div></details>
  <AppModal v-if="inspection" :title="t('安装插件')" :busy="busy" @close="inspection = undefined; upload = undefined"><h3>{{ inspection.package.manifest.name || inspection.package.manifest.id }}</h3><p>{{ inspection.package.manifest.id }} / v{{ inspection.package.manifest.version }}</p><p v-if="inspection.package.manifest.description">{{ inspection.package.manifest.description }}</p><p>{{ t('插件以本机程序运行，请确认来源后安装。') }}</p><h4>{{ t('所需权限') }}</h4><ul><li v-for="permission in inspection.package.manifest.permissions" :key="permission">{{ t(permissionNames[permission] || permission) }}</li></ul><p v-if="inspection.package.manifest.dependencies?.length">{{ t('依赖') }}: {{ inspection.package.manifest.dependencies.join(', ') }}</p><label class="checkbox-label"><input v-model="approved" type="checkbox" />{{ t('允许上述权限') }}</label><div class="settings-actions"><button class="primary-button" :disabled="busy || !approved" @click="install">{{ t('确认安装') }}</button><button class="secondary-button" :disabled="busy" @click="inspection = undefined; upload = undefined">{{ t('取消') }}</button></div></AppModal>
 </section>
</template>
<style scoped>
.package-manager { display: grid; gap: 1rem; }
.package-upload { position: relative; overflow: hidden; cursor: pointer; }
.package-upload input { position: absolute; inset: 0; opacity: 0; cursor: pointer; width: 100%; }
.extension-list { display: grid; gap: .75rem; margin-top: 1rem; }
.extension-row { display: flex; flex-wrap: wrap; gap: 1rem; align-items: center; padding-block: .75rem; border-bottom: 1px solid var(--border); }
.extension-row > div:first-of-type { display: grid; gap: .25rem; flex: 1; min-width: 0; overflow-wrap: anywhere; }
</style>
