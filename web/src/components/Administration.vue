<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppModal from './AppModal.vue'
import AppIcon from './AppIcon.vue'
import { feedbackRef } from '../feedback'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { formatBytes, quotaPercent } from '../formatBytes'
import { locale, t } from '../i18n'
import ContentReview from './ContentReview.vue'
import TextureModeration from './TextureModeration.vue'
import ResourceAdministration from './ResourceAdministration.vue'
import ReportsView from './ReportsView.vue'
import PluginWorkspace from './PluginWorkspace.vue'
import eventLabels from '../../../internal/httpapi/admin_event_labels.json'
import { adminPath, type AdminTab } from '../workspaceRoutes'
import { fontPresetNames, fontScopeNames, themePackNames } from '../themePacks'

interface AdminUser { id: string; email: string; nickname: string; isAdmin: boolean; banned: boolean; emailVerified: boolean; verificationRequired: boolean }
interface AdminEvent { id: string; actorId: string; action: string; targetType: string; targetId: string; createdAt: number; changes?: { field: string; before: string; after: string }[] }
interface AdminOverview {
  users: number
  characters: number
  textures: number
  pendingReviews: number
  pendingReports: number
  storage: { siteUsedBytes: number; siteLimitBytes: number; accountLimitBytes: number }
  plugins: { enabled: number; total: number }
  backup: { configured: boolean; count: number; latestCreatedAt?: string; latestState?: string; latestUnverifiedFiles?: number }
}
const props = defineProps<{ csrfToken: string; userId: string; activeTab: AdminTab; pluginId: string }>()
const emit = defineEmits<{ expired: []; updated: []; navigate: [path: string] }>()
const tab = computed(() => props.activeTab)
const overview = ref<AdminOverview | null>(null)
const overviewBusy = ref(false)
const search = ref('')
const query = ref('')
const users = ref<AdminUser[]>([])
const events = ref<AdminEvent[]>([])
const userPage = ref(1)
const eventPage = ref(1)
const eventSearch = ref('')
const eventQuery = ref('')
const userTotal = ref(0)
const eventTotal = ref(0)
const usersBusy = ref(false)
const eventsBusy = ref(false)
const saving = ref(false)
const userError = feedbackRef('error')
const eventError = feedbackRef('error')
const actionError = feedbackRef('error')
const message = feedbackRef('success')
const pending = ref<{ user: AdminUser; field: 'banned' | 'isAdmin' | 'emailVerified'; value: boolean } | null>(null)
const confirmButton = ref<HTMLButtonElement | null>(null)
const pendingLabel = computed(() => pending.value ? actionLabel(pending.value.field, pending.value.value) : '')
const actionNames = Object.fromEntries(Object.entries(eventLabels.actions).map(([key, labels]) => [key, labels['zh-CN']]))
const targetNames = Object.fromEntries(Object.entries(eventLabels.targets).map(([key, labels]) => [key, labels['zh-CN']]))
const settingNames = Object.fromEntries(Object.entries(eventLabels.settings).map(([key, labels]) => [key, labels['zh-CN']]))
const tabNames = { users: '用户管理', characters: '角色管理', wardrobe: '衣柜管理', resources: '材质管理', reports: '举报管理', events: '管理记录', settings: '站点设置', plugins: '插件管理' }
const fieldNames: Record<string, string> = { homepage_blocks: '首页区块', homepage_background: '首页背景', site_appearance: '界面外观', backup_schedule: '自动备份', cdn_config: 'CDN', storage_quota: '存储配额', accountLimitBytes: '单账号配额（字节）', siteLimitBytes: '全站配额（字节）', content_policy: '内容设置', services: '邮件与验证', smtpEnabled: 'SMTP', turnstileEnabled: 'Turnstile', address: '服务地址', from: '发件人', tlsMode: '连接加密', smtpPassword: 'SMTP 凭据', turnstileSecret: 'Turnstile 凭据', objectStorage: '对象存储', object_storage_config: '对象存储', external_connectors: '账号连接器', oidc_config: 'OIDC 配置', oidc_providers: 'OIDC 提供商', oidc: 'OIDC', connectors: '账号连接器', plugin: '插件', oauth: '授权应用', credentials: '访问凭据', endpoint: '服务地址', bucket: '存储桶', prefix: '存储前缀', region: '区域', issuer: 'Issuer', clientId: '应用 ID', clientSecret: '应用密钥', kind: '类型', dailyReward: '每日签到积分', uploadCost: '上传消耗积分', characterCost: '创建角色消耗积分', points: '积分规则', public: '公开客户端', redirects: '回调地址',  site_name: '站点名称', site_tagline: '站点简介', site_footer: '页脚信息', site_notice: '站点公告', site_notice_format: '公告格式', character_limit: '角色数量上限', registration_mode: '注册模式', name: '名称', model: '手臂模型', banned: '封禁状态', permission: '用户权限', emailVerified: '邮箱验证', review_state: '审核状态', review_reason: '审核备注', title: '标题', body: '正文', image: '图片', imageAlt: '图片说明', enabled: '启用', overlay: '背景遮罩', time: '每日备份时间', timezone: '备份时区', directory: '备份目录', accent: '主题配色', themePack: '主题包', defaultTheme: '默认明暗模式', surfaceEffect: '面板效果', surfaceOpacity: '面板不透明度', surfaceBlur: '毛玻璃模糊度', allowUserTheme: '允许用户自选主题', disableParticles: '关闭氛围粒子', fontPreset: '字体预设', fontScope: '字体应用范围', customFontUrl: '自定义字体地址', customFontFamily: 'CSS 字体族名称', source: '展示内容来源', reviewUploads: '上传审核', categories: '分类', buttonHref: '按钮链接', buttonLabel: '按钮文字', skin: '皮肤', cape: '披风', reason: '备注', registration_email_domains: '允许注册域名', registration_blocked_domains: '禁止注册域名', manual_email_verification: '管理员邮箱验证', random_user_ids: '随机用户 ID', maxAge: '缓存上限', baseUrl: 'CDN 地址' }
function changeField(value: string) { return value.split('.').map(part => { const match = part.match(/^(.*?)(\[\d+\])?$/); return t(fieldNames[match?.[1] || part] || match?.[1] || part) + (match?.[2] || '') }).join(' / ') }
const changeValues: Record<string, Record<string, string>> = {
  themePack: themePackNames,
  fontPreset: fontPresetNames,
  fontScope: fontScopeNames,
  defaultTheme: { system: '跟随系统', light: '浅色', dark: '深色' },
  surfaceEffect: { solid: '实色', translucent: '半透明', glass: '毛玻璃' },
  review_state: { pending: '待审核', approved: '已通过', rejected: '已拒绝' },
  permission: { '0': '普通用户', '2': '管理员' },
  registration_mode: { closed: '关闭注册', open: '开放注册', invitation: '启用邀请码注册' },
  model: { default: '标准手臂', slim: '纤细手臂' },
}
function changeValue(value: string, field: string) { return value === '' ? t('未设置') : value === 'true' ? t('是') : value === 'false' ? t('否') : t(changeValues[field.split('.').at(-1) || field]?.[value] || value) }
function actionLabel(field: 'banned' | 'isAdmin' | 'emailVerified', value: boolean) { return field === 'emailVerified' ? '完成邮箱验证' : field === 'banned' ? (value ? '封禁用户' : '解封用户') : (value ? '设为管理员' : '设为普通用户') }
function timeLabel(value: number) { return new Date(value * 1000).toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN') }
async function loadUsers() {
  if (usersBusy.value || saving.value) return
  usersBusy.value = true; userError.value = ''
  try {
    const response = await fetch(`/api/admin/users?${new URLSearchParams({ q: query.value, page: String(userPage.value) })}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); userError.value = data.error || '暂时无法读取用户列表'; return }
    const data = await response.json() as { users: AdminUser[]; total: number }
    users.value = data.users; userTotal.value = data.total
  } catch { userError.value = '暂时无法读取用户列表' }
  finally { usersBusy.value = false }
}
async function loadEvents() {
  if (eventsBusy.value) return
  eventsBusy.value = true; eventError.value = ''
  try {
    const response = await fetch(`/api/admin/events?${new URLSearchParams({ page: String(eventPage.value), q: eventQuery.value })}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); eventError.value = data.error || '暂时无法读取管理记录'; return }
    const data = await response.json() as { events: AdminEvent[]; total: number }
    events.value = data.events; eventTotal.value = data.total
  } catch { eventError.value = '暂时无法读取管理记录' }
  finally { eventsBusy.value = false }
}
function findEvents() { eventPage.value = 1; eventQuery.value = eventSearch.value.trim(); loadEvents() }
async function exportEvents() {
  if (eventsBusy.value) return
  const response = await fetch(`/api/admin/events/export?${new URLSearchParams({ q: eventQuery.value })}`, { credentials: 'same-origin' })
  if (response.status === 401) { emit('expired'); return }
  if (!response.ok) { eventError.value = '暂时无法导出管理记录'; return }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = 'wishingskin-admin-events.csv'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
}
function findUsers() { if (usersBusy.value || saving.value) return; query.value = search.value; userPage.value = 1; pending.value = null; loadUsers() }
function turnPage(kind: 'users' | 'events', offset: number) { if (kind === 'users') { userPage.value += offset; pending.value = null; loadUsers() } else { eventPage.value += offset; loadEvents() } }
function selectTab(value: AdminTab) { emit('navigate', adminPath(value)) }
async function confirm(user: AdminUser, field: 'banned' | 'isAdmin' | 'emailVerified') { pending.value = { user, field, value: !user[field] }; actionError.value = ''; message.value = ''; await nextTick(); confirmButton.value?.focus() }
async function apply() {
  const change = pending.value
  if (!change || saving.value) return
  saving.value = true; actionError.value = ''; message.value = ''
  try {
    const response = await fetch(`/api/admin/users/${change.user.id}`, { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ [change.field]: change.value }) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); actionError.value = data.error || '管理操作未完成，请重试'; return }
    pending.value = null
    if (change.user.id === props.userId && change.field !== 'emailVerified') { emit('expired'); return }
    message.value = '用户状态已更新。'
  } catch { actionError.value = '无法确认操作结果，请刷新用户列表后检查。' }
  finally { saving.value = false }
  await Promise.all([loadUsers(), loadOverview()])
}
async function loadOverview() {
  if (overviewBusy.value) return
  overviewBusy.value = true
  try {
    const response = await fetch('/api/admin/overview', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (response.ok) overview.value = await response.json() as AdminOverview
  } catch {
    // Keep admin tabs usable even if overview summary temporarily fails.
  } finally {
    overviewBusy.value = false
  }
}
function formatBackupDate(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN')
}
onMounted(() => { void loadOverview() })
watch(tab, value => { if (value === 'users') void loadUsers(); if (value === 'events') void loadEvents() }, { immediate: true })
</script>

<template>
  <div class="administration">
    <div class="page-heading">
      <div><h1>{{ t('管理后台') }}</h1><p>{{ t('管理用户、查看操作记录和配置站点。') }}</p></div>
      <div class="page-heading-actions"><button type="button" class="secondary-button" :disabled="overviewBusy" @click="loadOverview">{{ overviewBusy ? t('正在刷新…') : t('刷新概览') }}</button></div>
    </div>
    <section v-if="overview" class="admin-overview-grid" :aria-label="t('站点运行概览')">
      <button type="button" class="panel admin-overview-card" @click="selectTab('users')">
        <span class="admin-overview-label">{{ t('用户与角色') }}</span>
        <strong>{{ overview.users }}</strong>
        <span class="admin-overview-meta">{{ t('{count} 个角色', { count: overview.characters }) }}</span>
      </button>
      <button type="button" class="panel admin-overview-card" @click="selectTab('resources')">
        <span class="admin-overview-label">{{ t('材质与审核') }}</span>
        <strong>{{ overview.textures }}</strong>
        <span class="admin-overview-meta" :class="{ attention: overview.pendingReviews > 0 }">{{ overview.pendingReviews > 0 ? t('{count} 份待审核', { count: overview.pendingReviews }) : t('当前无待审核') }}</span>
      </button>
      <div class="panel admin-overview-card admin-overview-static">
        <span class="admin-overview-label">{{ t('全站材质存储') }}</span>
        <strong>{{ formatBytes(overview.storage.siteUsedBytes) }}</strong>
        <span class="admin-overview-meta">{{ overview.storage.siteLimitBytes > 0 ? t('上限 {limit}', { limit: formatBytes(overview.storage.siteLimitBytes) }) : t('未设容量上限') }}</span>
        <div v-if="overview.storage.siteLimitBytes > 0" class="quota-bar" role="progressbar" :aria-valuenow="quotaPercent(overview.storage.siteUsedBytes, overview.storage.siteLimitBytes)" aria-valuemin="0" aria-valuemax="100" :aria-label="t('全站材质存储使用比例')">
          <span class="quota-bar-fill" :class="{ warning: quotaPercent(overview.storage.siteUsedBytes, overview.storage.siteLimitBytes) >= 85 }" :style="{ width: `${quotaPercent(overview.storage.siteUsedBytes, overview.storage.siteLimitBytes)}%` }"></span>
        </div>
      </div>
      <button type="button" class="panel admin-overview-card" @click="emit('navigate', '/admin/settings/operations')">
        <span class="admin-overview-label">{{ t('备份状态') }}</span>
        <strong>{{ overview.backup.configured ? t('{count} 份', { count: overview.backup.count }) : t('未配置') }}</strong>
        <span class="admin-overview-meta">{{ overview.backup.latestCreatedAt ? formatBackupDate(overview.backup.latestCreatedAt) : overview.backup.configured ? t('暂无已生成备份') : t('前往存储与备份配置') }}</span>
      </button>
      <button type="button" class="panel admin-overview-card" @click="selectTab(overview.pendingReports > 0 ? 'reports' : 'plugins')">
        <span class="admin-overview-label">{{ t('插件与举报') }}</span>
        <strong>{{ overview.plugins.enabled }} / {{ overview.plugins.total }}</strong>
        <span class="admin-overview-meta" :class="{ attention: overview.pendingReports > 0 }">{{ overview.pendingReports > 0 ? t('{count} 条待处理举报', { count: overview.pendingReports }) : t('暂无待处理举报') }}</span>
      </button>
    </section>
    <nav class="admin-tabs admin-navigation" :aria-label="t('管理栏目')"><div class="admin-core-navigation"><span class="admin-group-label">{{ t('核心管理') }}</span><div><button v-for="item in (['users', 'characters', 'resources', 'wardrobe', 'reports', 'events', 'settings'] as const)" :key="item" type="button" class="secondary-button" :aria-current="tab === item ? 'page' : undefined" :class="{ active: tab === item }" :disabled="saving" @click="selectTab(item)">{{ t(tabNames[item]) }}</button></div></div><div class="admin-plugin-navigation"><span class="admin-group-label">{{ t('插件') }}</span><button type="button" class="secondary-button" :aria-current="tab === 'plugins' ? 'page' : undefined" :class="{ active: tab === 'plugins' }" :disabled="saving" @click="selectTab('plugins')">{{ t('插件管理') }}</button></div></nav>
    <div data-panel-region="admin">
    <ContentReview v-if="tab === 'resources'" :csrf-token="csrfToken" @expired="emit('expired')" /><TextureModeration v-if="tab === 'resources'" :csrf-token="csrfToken" @expired="emit('expired')" />
    <ResourceAdministration v-if="tab === 'characters' || tab === 'wardrobe'" :key="tab" :mode="tab" :csrf-token="csrfToken" @expired="emit('expired')" />
    <section v-show="tab === 'users'" :aria-label="t('用户管理')">
      <form class="admin-search panel" @submit.prevent="findUsers"><label class="binding-field"><span>{{ t('搜索用户') }}</span><input v-model="search" maxlength="255" :placeholder="t('邮箱、昵称或用户编号')" :disabled="saving" /></label><button type="submit" class="primary-button" :disabled="usersBusy || saving">{{ t('搜索') }}</button><button type="button" class="secondary-button" :disabled="usersBusy || saving" @click="loadUsers">{{ t('刷新列表') }}</button></form>
      <AppModal v-if="pending" :title="t('确认{action}', { action: t(pendingLabel) })" :busy="saving" @close="pending = null"><div  class="admin-confirm panel" aria-describedby="admin-confirm-description">
        <p class="admin-identity">{{ pending.user.nickname }} / {{ pending.user.email }} / #{{ pending.user.id }}</p><p id="admin-confirm-description">{{ pending.field === 'emailVerified' ? t('将此用户的邮箱标记为已验证。') : t('确认修改此用户的账号状态？') }}</p>
        <div class="admin-actions"><button ref="confirmButton" :class="pending.field === 'emailVerified' ? 'primary-button' : 'danger-button'" type="button" :disabled="saving" @click="apply">{{ saving ? t('正在保存…') : t('确认{action}', { action: t(pendingLabel) }) }}</button><button class="secondary-button" type="button" :disabled="saving" @click="pending = null; actionError = ''">{{ t('取消') }}</button></div>
      </div></AppModal>

      <LoadingSkeleton v-if="usersBusy" class="panel" label="正在加载用户…" />
      <div v-else-if="userError" class="empty-state panel" role="alert"><button class="secondary-button" type="button" @click="loadUsers">{{ t('重新加载') }}</button></div>
      <div v-else class="panel admin-list"><p v-if="!users.length" class="empty-state">{{ t('没有找到匹配的用户。') }}</p><article v-for="item in users" :key="item.id" class="admin-user"><div class="admin-user-copy"><h2>{{ item.nickname || t('用户') }} <small>#{{ item.id }}{{ item.id === userId ? ` / ${t('你自己')}` : '' }}</small></h2><p>{{ item.email }}</p><div class="admin-status"><span>{{ item.isAdmin ? t('管理员') : t('普通用户') }}</span><span :class="{ 'form-error': item.banned }">{{ item.banned ? t('已封禁') : t('正常') }}</span><span>{{ item.emailVerified ? t('邮箱已验证') : item.verificationRequired ? t('待验证邮箱后登录') : t('邮箱未验证') }}</span></div></div><div class="admin-actions"><button v-if="!item.emailVerified" type="button" class="secondary-button" :disabled="saving" @click="confirm(item, 'emailVerified')">{{ t('完成邮箱验证') }}</button><button type="button" class="secondary-button" :disabled="saving" @click="confirm(item, 'isAdmin')">{{ t(actionLabel('isAdmin', !item.isAdmin)) }}</button><button type="button" :class="item.banned ? 'secondary-button' : 'danger-button'" :disabled="saving" @click="confirm(item, 'banned')">{{ t(actionLabel('banned', !item.banned)) }}</button></div></article>
        <div class="library-pagination"><button class="secondary-button" type="button" :disabled="userPage <= 1 || saving" @click="turnPage('users', -1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 位用户', { page: userPage, total: userTotal }) }}</span><button class="secondary-button" type="button" :disabled="userPage * 20 >= userTotal || saving" @click="turnPage('users', 1)">{{ t('下一页') }}</button></div>
      </div>
    </section>
    <ReportsView v-if="tab === 'reports'" admin :csrf-token="csrfToken" @expired="emit('expired')" />
    <section v-show="tab === 'events'" :aria-label="t('管理记录')">
      <form class="admin-search panel" @submit.prevent="findEvents"><label class="binding-field"><span>{{ t('搜索管理记录') }}</span><input v-model="eventSearch" maxlength="255" :placeholder="t('动作、对象类型、对象编号或操作者')" :disabled="eventsBusy" /></label><button type="submit" class="primary-button" :disabled="eventsBusy">{{ t('搜索') }}</button><button type="button" class="secondary-button" :disabled="eventsBusy" @click="loadEvents">{{ t('刷新列表') }}</button><button type="button" class="secondary-button" :disabled="eventsBusy" @click="exportEvents">{{ t('导出 CSV') }}</button></form>
      <div class="admin-record-heading"><p>{{ t('查看用户与站点的管理记录。') }}</p></div>
      <LoadingSkeleton v-if="eventsBusy" class="panel" label="正在加载管理记录…" /><div v-else-if="eventError" class="empty-state panel" role="alert"><button class="secondary-button" type="button" @click="loadEvents">{{ t('重新加载') }}</button></div>
      <div v-else class="panel admin-list"><p v-if="!events.length" class="empty-state">{{ t('暂无管理记录。') }}</p><article v-for="item in events" :key="item.id" class="admin-event"><h2>{{ t(actionNames[item.action] || '其他管理操作') }}</h2><time :datetime="new Date(item.createdAt * 1000).toISOString()">{{ timeLabel(item.createdAt) }}</time><p>{{ t('操作者') }}：{{ item.actorId === '0' ? t('本机初始化') : `#${item.actorId}` }}</p><p>{{ t(targetNames[item.targetType] || '管理对象') }}：{{ item.targetType === 'site' ? t(settingNames[item.targetId] || '站点设置') : item.targetType === 'invitation' ? t('已生成（不显示邀请码）') : `#${item.targetId}` }}</p><div v-if="item.changes?.length" class="admin-change-list"><div v-for="(change, index) in item.changes" :key="index" class="admin-change-row"><strong>{{ changeField(change.field) }}</strong><del>{{ changeValue(change.before, change.field) }}</del><AppIcon name="arrow-right" /><span>{{ changeValue(change.after, change.field) }}</span></div></div></article><div class="library-pagination"><button class="secondary-button" type="button" :disabled="eventPage <= 1" @click="turnPage('events', -1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 条记录', { page: eventPage, total: eventTotal }) }}</span><button class="secondary-button" type="button" :disabled="eventPage * 20 >= eventTotal" @click="turnPage('events', 1)">{{ t('下一页') }}</button></div></div>
    </section>
    <PluginWorkspace v-if="tab === 'plugins'" :selected-id="pluginId" @navigate="emit('navigate', $event)" :csrf-token="csrfToken" @expired="emit('expired')" @updated="emit('updated')" />
    <section v-if="tab === 'settings'" class="admin-settings panel" :aria-label="t('站点设置')"><slot name="settings" /></section>
    </div>
  </div>
</template>
