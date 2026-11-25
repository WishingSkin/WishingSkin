<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppModal from './AppModal.vue'
import CopyButton from './CopyButton.vue'
import { t } from '../i18n'
import { confirmAction, toast } from '../feedback'
import { settingsSnapshot } from '../settingsDraft'
import { dirtyModalConfirm, oneTimeSecretModalConfirm } from '../unsavedGuard'
type Subscription = { id: string; pluginId: string; url: string; events: string[]; enabled: boolean }
type Delivery = { id: string; subscriptionId: string; attempts: number; state: string; status: number; createdAt: number }
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const subscriptions = ref<Subscription[]>([]), deliveries = ref<Delivery[]>([]), events = ref<string[]>([])
const editing = ref<Subscription>(), editingInitial = ref(''), secret = ref(''), busy = ref(false)
const editingDirty = computed(() => !!editing.value && settingsSnapshot(editing.value) !== editingInitial.value)
const labels: Record<string, string> = { 'character.create': '创建角色', 'texture.upload': '上传材质', pending: '等待投递', failed: '投递失败', delivered: '已送达' }
async function request(path: string, method = 'GET', body?: unknown) {
 const response = await fetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: body === undefined ? undefined : JSON.stringify(body) })
 const data = await response.json()
 if (response.status === 401) emit('expired')
 if (!response.ok) throw new Error(data.error || '操作失败')
 return data
}
async function load() { try { const data = await request('/api/admin/webhooks'); subscriptions.value = data.subscriptions; deliveries.value = data.deliveries; events.value = data.events } catch (error) { toast((error as Error).message, 'error') } }
function edit(item?: Subscription) { editing.value = item ? { ...item, events: [...item.events] } : { id: '', pluginId: '', url: '', events: [], enabled: true }; editingInitial.value = settingsSnapshot(editing.value) }
async function save(rotateSecret = false) {
 if (!editing.value) return
 if (rotateSecret && !await confirmAction({ title: '轮换签名密钥', message: '轮换后请更新接收端的签名密钥。' })) return
 busy.value = true
 try {
  const item = editing.value
  const data = await request(`/api/admin/webhooks${item.id ? '/' + encodeURIComponent(item.id) : ''}`, item.id ? 'PUT' : 'POST', { ...item, rotateSecret })
  editing.value = undefined; secret.value = data.secret || ''; toast('Webhook 已保存。'); await load()
 } catch (error) { toast((error as Error).message, 'error') } finally { busy.value = false }
}
async function remove(item: Subscription) {
 if (!await confirmAction({ title: '删除 Webhook', message: '删除此订阅及其投递记录？', danger: true })) return
 busy.value = true
 try { await request(`/api/admin/webhooks/${encodeURIComponent(item.id)}`, 'DELETE'); toast('Webhook 已删除。'); await load() } catch (error) { toast((error as Error).message, 'error') } finally { busy.value = false }
}
async function retry(item: Delivery) {
 busy.value = true
 try { await request(`/api/admin/webhook-deliveries/${encodeURIComponent(item.id)}/retry`, 'POST'); toast('已加入投递队列。'); await load() } catch (error) { toast((error as Error).message, 'error') } finally { busy.value = false }
}
onMounted(load)
</script>
<template>
 <details class="webhook-settings"><summary>Webhook</summary>
  <div class="settings-actions"><button class="secondary-button" :disabled="busy" @click="edit()">{{ t('添加 Webhook') }}</button><button class="text-button" :disabled="busy" @click="load">{{ t('刷新') }}</button></div>
  <article v-for="item in subscriptions" :key="item.id" class="webhook-row"><div><strong>{{ item.url }}</strong><span>{{ item.events.map(event => t(labels[event] || event)).join(', ') }}</span><span>{{ t(item.enabled ? '已启用' : '已停用') }}</span></div><div class="settings-actions"><button class="secondary-button" :disabled="busy" @click="edit(item)">{{ t('编辑') }}</button><button class="danger-button" :disabled="busy" @click="remove(item)">{{ t('删除') }}</button></div></article>
  <template v-if="deliveries.length"><h4>{{ t('最近投递') }}</h4><article v-for="item in deliveries" :key="item.id" class="webhook-row"><div><span>{{ t(labels[item.state] || item.state) }} {{ item.status || '' }}</span><time>{{ new Date(item.createdAt * 1000).toLocaleString() }}</time><span>{{ t('尝试次数') }}: {{ item.attempts }}</span></div><button v-if="item.state === 'failed'" class="secondary-button" :disabled="busy" @click="retry(item)">{{ t('重试') }}</button></article></template>
 </details>
 <AppModal v-if="editing" :title="t('Webhook 设置')" :busy="busy" :confirm-close="editingDirty ? dirtyModalConfirm : false" @close="editing = undefined"><form class="settings-form" @submit.prevent="save()"><label class="binding-field"><span>{{ t('接收地址') }}</span><input v-model="editing.url" type="url" required placeholder="https://example.com/webhook" /></label><label class="binding-field"><span>{{ t('关联插件 ID') }}</span><input v-model="editing.pluginId" :placeholder="t('可选')" /></label><fieldset><legend>{{ t('订阅事件') }}</legend><label v-for="event in events" :key="event" class="checkbox-label"><input v-model="editing.events" type="checkbox" :value="event" />{{ t(labels[event] || event) }}</label></fieldset><div class="toggle-setting plugin-settings-toggle"><div class="toggle-setting__text"><label class="toggle-setting__title" for="webhook-enabled-switch">{{ t('启用') }}</label></div><input id="webhook-enabled-switch" v-model="editing.enabled" class="switch" type="checkbox" role="switch" /></div><div class="settings-actions"><button class="primary-button" :disabled="busy || !editing.events.length">{{ t('保存') }}</button><button v-if="editing.id" type="button" class="secondary-button" :disabled="busy" @click="save(true)">{{ t('轮换签名密钥') }}</button></div></form></AppModal>
 <AppModal v-if="secret" :title="t('签名密钥')" :confirm-close="oneTimeSecretModalConfirm" @close="secret = ''"><p>{{ t('请保存此密钥，用于接收端验证签名。') }}</p><div class="secret-copy-row"><code class="webhook-secret">{{ secret }}</code><CopyButton :text="secret" :label="t('复制签名密钥')" /></div><button class="primary-button" @click="secret = ''">{{ t('完成') }}</button></AppModal>
</template>
<style scoped>.webhook-settings { margin-top: 24px; }.webhook-settings > .settings-actions { margin-top: 16px; }.webhook-row { display: flex; flex-wrap: wrap; align-items: center; gap: 16px; padding-block: 16px; border-bottom: 1px solid var(--border); }.webhook-row > div:first-child { display: grid; gap: 4px; flex: 1; min-width: 0; overflow-wrap: anywhere; }.webhook-secret { display: block; overflow-wrap: anywhere; margin-block: 20px; }fieldset { border: 0; padding: 0; }fieldset label { margin-block: 8px; }</style>
