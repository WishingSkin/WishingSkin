<script setup lang="ts">
import { feedbackRef, confirmAction, toast } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import { t } from '../i18n'
import PluginIcon from './PluginIcon.vue'
import PluginPackages from './PluginPackages.vue'
import WebhookSettings from './WebhookSettings.vue'
import InvitationPlugin from './InvitationPlugin.vue'
import PointsSettings from './PointsSettings.vue'
import ObjectStorageSettings from './ObjectStorageSettings.vue'
import CommunityPluginSettings from './CommunityPluginSettings.vue'
import ConnectorSettings from './ConnectorSettings.vue'
import OAuthSettings from './OAuthSettings.vue'
import OIDCSettings from './OIDCSettings.vue'
import type { PointRules } from '../pointTypes'
import { normalizePluginManifests, type PluginManifest } from '../pluginModel'

const props = defineProps<{ csrfToken: string; selectedId: string }>()
const emit = defineEmits<{ updated: []; expired: []; navigate: [path: string] }>()
const plugins = ref<PluginManifest[]>([])
const pointsRules = ref<PointRules>({ enabled: false, dailyReward: 0, uploadCost: 0, characterCost: 0 })
const selected = computed(() => props.selectedId)
const loading = ref(false)
const busy = ref('')
const error = feedbackRef('error')

async function load() {
  loading.value = true; error.value = ''
  try {
    const [pluginResponse, siteResponse] = await Promise.all([
      fetch('/api/admin/plugins', { credentials: 'same-origin' }),
      fetch('/api/site', { credentials: 'same-origin' }),
    ])
    if (pluginResponse.status === 401 || siteResponse.status === 401) { emit('expired'); return }
    const pluginData = await pluginResponse.json().catch(() => ({})) as { plugins?: unknown[]; error?: string }
    const siteData = await siteResponse.json().catch(() => ({})) as { points?: PointRules }
    if (!pluginResponse.ok) { error.value = pluginData.error || '暂时无法读取插件状态'; return }
    plugins.value = normalizePluginManifests(pluginData.plugins)
    if (siteData.points) pointsRules.value = siteData.points
  } catch { error.value = '暂时无法读取插件状态' }
  finally { loading.value = false }
}
async function toggle(plugin: PluginManifest) {
  if (busy.value) return
  if (plugin.enabled && !await confirmAction({ title: '停用插件', message: t('停用“{name}”？', { name: label(plugin.id) }) })) return
  busy.value = plugin.id; error.value = ''
  try {
    const response = await fetch(`/api/admin/plugins/${encodeURIComponent(plugin.id)}`, { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ enabled: !plugin.enabled }) })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({})) as { error?: string }
    if (!response.ok) { error.value = data.error || '插件状态保存失败'; return }
    toast('插件状态已更新。'); await load(); emit('updated')
  } catch { error.value = '无法确认插件状态，请刷新重试。' }
  finally { busy.value = '' }
}
function label(id: string) { const item = plugins.value.find(p => p.id === id); if (item?.name) return item.name; if (id === 'wishingskin.object-storage') return '对象存储'; return id === 'wishingskin.points' ? '积分与签到' : id === 'wishingskin.invitations' ? '邀请码' : id === 'wishingskin.oidc' ? 'OIDC 登录' : id }
async function settingsUpdated() { await load(); emit('updated') }
function open(plugin: PluginManifest) { emit('navigate', selected.value === plugin.id ? '/admin/plugins' : `/admin/plugins/${encodeURIComponent(plugin.id)}`) }
onMounted(load)
</script>

<template>
  <section class="plugin-workspace" :aria-label="t('插件区域')">
    <header class="plugin-workspace-heading"><div><h2>{{ t('插件') }}</h2><p>{{ t('管理官方模块和社区插件。') }}</p></div><span class="plugin-origin">WishingSkin</span></header>
    <p v-if="loading" role="status">{{ t('正在加载…') }}</p><p v-if="error" class="form-error" role="alert"><button class="text-button" type="button" @click="load">{{ t('重试') }}</button></p>
    <div v-if="!loading" class="plugin-catalog"><article v-for="plugin in plugins" :key="plugin.id" class="plugin-card panel" :class="{ selected: selected === plugin.id }"><div class="plugin-card-heading"><span class="plugin-symbol"><PluginIcon :id="plugin.id" /></span><div><h3>{{ t(label(plugin.id)) }}</h3><span class="plugin-origin">{{ plugin.origin === 'community' ? t('社区') + ' / ' : '' }}{{ plugin.id }} / v{{ plugin.version }}</span></div><span class="plugin-status">{{ plugin.enabled ? t('已启用') : t('已停用') }}</span></div><p>{{ t(plugin.description || (plugin.id === 'wishingskin.object-storage' ? '将材质同步到 S3 兼容存储，保留本地回退副本。' : plugin.origin === 'community' ? '可独立启停和配置。' : plugin.id === 'wishingskin.points' ? '管理每日签到、上传费用和新建角色费用。' : plugin.id === 'wishingskin.oidc' ? '通过身份提供商登录和绑定账号，保留本站注册规则。' : '为邮箱验证注册增加可选或必填的邀请码，支持批量生成和导出。')) }}</p><p v-if="plugin.dependencies.length" class="password-rule">{{ t('依赖') }}：{{ plugin.dependencies.join(', ') }}</p><p v-if="plugin.error" class="form-error">{{ t(plugin.error) }}</p><div class="settings-actions"><button class="secondary-button" type="button" :aria-expanded="selected === plugin.id" @click="open(plugin)">{{ selected === plugin.id ? t('收起设置') : t('配置插件') }}</button><button class="secondary-button" type="button" :disabled="busy === plugin.id" @click="toggle(plugin)">{{ busy === plugin.id ? t('正在保存…') : plugin.enabled ? t('停用') : t('启用') }}</button></div></article></div>
    <PluginPackages :csrf-token="props.csrfToken" @expired="emit('expired')" @updated="settingsUpdated" /><WebhookSettings :csrf-token="props.csrfToken" @expired="emit('expired')" /><div data-panel-region="plugins">
    <div v-for="plugin in plugins" :key="plugin.id + '-settings'" v-show="selected === plugin.id" class="plugin-settings panel"><ConnectorSettings v-if="plugin.id === 'wishingskin.connectors' && selected === plugin.id" :csrf-token="props.csrfToken" @expired="emit('expired')" /><OAuthSettings v-else-if="plugin.id === 'wishingskin.oauth2' && selected === plugin.id" :csrf-token="props.csrfToken" @expired="emit('expired')" /><InvitationPlugin v-else-if="plugin.id === 'wishingskin.invitations'" :csrf-token="props.csrfToken" @expired="emit('expired')" @updated="load" /><OIDCSettings v-else-if="plugin.id === 'wishingskin.oidc' && selected === plugin.id" :key="String(plugin.enabled)" :csrf-token="props.csrfToken" @expired="emit('expired')" @updated="settingsUpdated" /><ObjectStorageSettings v-else-if="plugin.id === 'wishingskin.object-storage' && selected === plugin.id" :csrf-token="props.csrfToken" @expired="emit('expired')" @updated="settingsUpdated" /><CommunityPluginSettings v-else-if="plugin.origin === 'community' && selected === plugin.id" :plugin="plugin" :csrf-token="props.csrfToken" @expired="emit('expired')" /><PointsSettings v-else-if="plugin.id === 'wishingskin.points'" :rules="pointsRules" :csrf-token="props.csrfToken" @expired="emit('expired')" @updated="load" /></div>
    </div>
  </section>
</template>
