<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { locale, t } from '../i18n'
import TextureModeration from './TextureModeration.vue'
import ResourceAdministration from './ResourceAdministration.vue'
import ReportsView from './ReportsView.vue'
import PluginWorkspace from './PluginWorkspace.vue'

interface AdminUser { id: string; email: string; nickname: string; isAdmin: boolean; banned: boolean; emailVerified: boolean; verificationRequired: boolean }
interface AdminEvent { id: string; actorId: string; action: string; targetType: string; targetId: string; createdAt: number }
const props = defineProps<{ csrfToken: string; userId: string }>()
const emit = defineEmits<{ expired: []; updated: [] }>()
const tab = ref<'users' | 'characters' | 'wardrobe' | 'textures' | 'reports' | 'events' | 'settings' | 'plugins'>('users')
const search = ref('')
const query = ref('')
const users = ref<AdminUser[]>([])
const events = ref<AdminEvent[]>([])
const userPage = ref(1)
const eventPage = ref(1)
const userTotal = ref(0)
const eventTotal = ref(0)
const usersBusy = ref(false)
const eventsBusy = ref(false)
const saving = ref(false)
const userError = ref('')
const eventError = ref('')
const actionError = ref('')
const message = ref('')
const pending = ref<{ user: AdminUser; field: 'banned' | 'isAdmin'; value: boolean } | null>(null)
const confirmButton = ref<HTMLButtonElement | null>(null)
const pendingLabel = computed(() => pending.value ? actionLabel(pending.value.field, pending.value.value) : '')
const actionNames: Record<string, string> = {
  'administrator.initialize': '创建首个管理员', 'user.ban': '封禁用户', 'user.unban': '解封用户',
  'user.promote': '设为管理员', 'user.demote': '设为普通用户', 'settings.character_limit': '修改角色上限',
  'registration.closed': '关闭注册', 'registration.open': '开放注册', 'registration.invitation': '启用邀请码注册', 'invitation.create': '生成邀请码',
  'texture.disable': '下架整份图片',
  'settings.branding': '修改品牌与公告',
  'settings.points': '修改积分规则',
  'settings.appearance': '修改个性化设置', 'settings.services': '修改邮件与验证设置', 'plugin.invitations': '修改邀请码插件',
  'character.rename': '修改角色名', 'character.clear_skin': '解除皮肤绑定', 'character.clear_cape': '解除披风绑定',
  'character.delete': '删除角色',
  'wardrobe.rename': '修改收藏名称', 'wardrobe.remove': '移出用户衣柜',
  'report.dismiss': '驳回举报', 'report.disable': '举报处置：下架图片', 'report.ban': '举报处置：封禁上传者',
}
const targetNames: Record<string, string> = { user: '用户', site: '站点', invitation: '邀请码', texture: '图片', character: '角色', wardrobe: '衣柜', report: '举报' }
const tabNames = { users: '用户管理', characters: '角色管理', wardrobe: '衣柜管理', textures: '材质管理', reports: '举报管理', events: '管理记录', settings: '站点设置', plugins: '官方插件' }
const settingNames: Record<string, string> = { registration: '注册模式', character_limit: '角色数量上限', branding: '品牌与公告', points: '积分与签到', appearance: '个性化', services: '邮件与验证', invitations: '邀请码插件' }
function actionLabel(field: 'banned' | 'isAdmin', value: boolean) { return field === 'banned' ? (value ? '封禁用户' : '解封用户') : (value ? '设为管理员' : '设为普通用户') }
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
    const response = await fetch(`/api/admin/events?page=${eventPage.value}`, { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); eventError.value = data.error || '暂时无法读取管理记录'; return }
    const data = await response.json() as { events: AdminEvent[]; total: number }
    events.value = data.events; eventTotal.value = data.total
  } catch { eventError.value = '暂时无法读取管理记录' }
  finally { eventsBusy.value = false }
}
function findUsers() { if (usersBusy.value || saving.value) return; query.value = search.value; userPage.value = 1; pending.value = null; loadUsers() }
function turnPage(kind: 'users' | 'events', offset: number) { if (kind === 'users') { userPage.value += offset; pending.value = null; loadUsers() } else { eventPage.value += offset; loadEvents() } }
function selectTab(value: typeof tab.value) { tab.value = value; if (value === 'events') loadEvents() }
async function confirm(user: AdminUser, field: 'banned' | 'isAdmin') { pending.value = { user, field, value: !user[field] }; actionError.value = ''; message.value = ''; await nextTick(); confirmButton.value?.focus() }
async function apply() {
  const change = pending.value
  if (!change || saving.value) return
  saving.value = true; actionError.value = ''; message.value = ''
  try {
    const response = await fetch(`/api/admin/users/${change.user.id}`, { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken }, body: JSON.stringify({ [change.field]: change.value }) })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { const data = await response.json().catch(() => ({})); actionError.value = data.error || '管理操作未完成，请重试'; return }
    pending.value = null
    if (change.user.id === props.userId) { emit('expired'); return }
    message.value = '用户状态已更新。'
  } catch { actionError.value = '无法确认操作结果，请刷新用户列表后检查。' }
  finally { saving.value = false }
  await loadUsers()
}
onMounted(loadUsers)
</script>

<template>
  <div class="administration">
    <div class="page-heading"><div><h1>{{ t('管理后台') }}</h1><p>{{ t('管理用户、查看操作记录和配置站点。') }}</p></div></div>
    <nav class="admin-tabs admin-navigation" :aria-label="t('管理栏目')"><div class="admin-core-navigation"><span class="admin-group-label">{{ t('核心管理') }}</span><div><button v-for="item in (['users', 'characters', 'textures', 'wardrobe', 'reports', 'events', 'settings'] as const)" :key="item" type="button" class="secondary-button" :aria-current="tab === item ? 'page' : undefined" :class="{ active: tab === item }" :disabled="saving" @click="selectTab(item)">{{ t(tabNames[item]) }}</button></div></div><div class="admin-plugin-navigation"><span class="admin-group-label">{{ t('插件') }}</span><button type="button" class="secondary-button" :aria-current="tab === 'plugins' ? 'page' : undefined" :class="{ active: tab === 'plugins' }" :disabled="saving" @click="selectTab('plugins')">{{ t('官方插件') }}</button></div></nav>
    <TextureModeration v-if="tab === 'textures'" :csrf-token="csrfToken" @expired="emit('expired')" />
    <ResourceAdministration v-if="tab === 'characters' || tab === 'wardrobe'" :key="tab" :mode="tab" :csrf-token="csrfToken" @expired="emit('expired')" />
    <section v-show="tab === 'users'" :aria-label="t('用户管理')">
      <form class="admin-search panel" @submit.prevent="findUsers"><label class="binding-field"><span>{{ t('搜索用户') }}</span><input v-model="search" maxlength="255" :placeholder="t('邮箱、昵称或用户编号')" :disabled="saving" /></label><button type="submit" class="primary-button" :disabled="usersBusy || saving">{{ t('搜索') }}</button><button type="button" class="secondary-button" :disabled="usersBusy || saving" @click="loadUsers">{{ t('刷新列表') }}</button></form>
      <div v-if="pending" class="admin-confirm panel" role="alertdialog" aria-modal="false" aria-labelledby="admin-confirm-title" aria-describedby="admin-confirm-description">
        <h2 id="admin-confirm-title">{{ t('确认{action}', { action: t(pendingLabel) }) }}</h2><p class="admin-identity">{{ pending.user.nickname }} · {{ pending.user.email }} · #{{ pending.user.id }}</p><p id="admin-confirm-description">{{ t('变更会退出该用户的全部网页会话，并撤销游戏令牌和邮件链接；角色与材质会保留。') }} {{ pending.user.id === userId ? t('你正在修改自己的账号，完成后需要重新登录。') : '' }}</p>
        <div class="admin-actions"><button ref="confirmButton" class="danger-button" type="button" :disabled="saving" @click="apply">{{ saving ? t('正在保存…') : t('确认{action}', { action: t(pendingLabel) }) }}</button><button class="secondary-button" type="button" :disabled="saving" @click="pending = null; actionError = ''">{{ t('取消') }}</button></div>
      </div>
      <p v-if="actionError" class="form-error action-feedback" role="alert">{{ t(actionError) }}</p><p v-if="message" class="action-success" role="status">{{ t(message) }}</p>
      <div v-if="usersBusy" class="empty-state panel" role="status"><p>{{ t('正在加载用户…') }}</p></div>
      <div v-else-if="userError" class="empty-state panel" role="alert"><p>{{ t(userError) }}</p><button class="secondary-button" type="button" @click="loadUsers">{{ t('重新加载') }}</button></div>
      <div v-else class="panel admin-list"><p v-if="!users.length" class="empty-state">{{ t('没有找到匹配的用户。') }}</p><article v-for="item in users" :key="item.id" class="admin-user"><div class="admin-user-copy"><h2>{{ item.nickname || t('用户') }} <small>#{{ item.id }}{{ item.id === userId ? ` · ${t('你自己')}` : '' }}</small></h2><p>{{ item.email }}</p><div class="admin-status"><span>{{ item.isAdmin ? t('管理员') : t('普通用户') }}</span><span :class="{ 'form-error': item.banned }">{{ item.banned ? t('已封禁') : t('正常') }}</span><span>{{ item.emailVerified ? t('邮箱已验证') : item.verificationRequired ? t('待验证邮箱后登录') : t('邮箱未验证') }}</span></div></div><div class="admin-actions"><button type="button" class="secondary-button" :disabled="saving" @click="confirm(item, 'isAdmin')">{{ t(actionLabel('isAdmin', !item.isAdmin)) }}</button><button type="button" :class="item.banned ? 'secondary-button' : 'danger-button'" :disabled="saving" @click="confirm(item, 'banned')">{{ t(actionLabel('banned', !item.banned)) }}</button></div></article>
        <div class="library-pagination"><button class="secondary-button" type="button" :disabled="userPage <= 1 || saving" @click="turnPage('users', -1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 位用户', { page: userPage, total: userTotal }) }}</span><button class="secondary-button" type="button" :disabled="userPage * 20 >= userTotal || saving" @click="turnPage('users', 1)">{{ t('下一页') }}</button></div>
      </div>
    </section>
    <ReportsView v-if="tab === 'reports'" admin :csrf-token="csrfToken" @expired="emit('expired')" />
    <section v-show="tab === 'events'" :aria-label="t('管理记录')">
      <div class="admin-record-heading"><p>{{ t('这里只展示已成功提交的管理变更。') }}</p><button type="button" class="secondary-button" :disabled="eventsBusy" @click="loadEvents">{{ t('刷新列表') }}</button></div>
      <div v-if="eventsBusy" class="empty-state panel" role="status"><p>{{ t('正在加载管理记录…') }}</p></div><div v-else-if="eventError" class="empty-state panel" role="alert"><p>{{ t(eventError) }}</p><button class="secondary-button" type="button" @click="loadEvents">{{ t('重新加载') }}</button></div>
      <div v-else class="panel admin-list"><p v-if="!events.length" class="empty-state">{{ t('暂无管理记录。') }}</p><article v-for="item in events" :key="item.id" class="admin-event"><h2>{{ t(actionNames[item.action] || '其他管理操作') }}</h2><time :datetime="new Date(item.createdAt * 1000).toISOString()">{{ timeLabel(item.createdAt) }}</time><p>{{ t('操作者') }}：{{ item.actorId === '0' ? t('本机初始化') : `#${item.actorId}` }}</p><p>{{ t(targetNames[item.targetType] || '管理对象') }}：{{ item.targetType === 'site' ? t(settingNames[item.targetId] || '站点设置') : item.targetType === 'invitation' ? t('已生成（不显示邀请码）') : `#${item.targetId}` }}</p></article><div class="library-pagination"><button class="secondary-button" type="button" :disabled="eventPage <= 1" @click="turnPage('events', -1)">{{ t('上一页') }}</button><span>{{ t('第 {page} 页，共 {total} 条记录', { page: eventPage, total: eventTotal }) }}</span><button class="secondary-button" type="button" :disabled="eventPage * 20 >= eventTotal" @click="turnPage('events', 1)">{{ t('下一页') }}</button></div></div>
    </section>
    <PluginWorkspace v-if="tab === 'plugins'" :csrf-token="csrfToken" @expired="emit('expired')" @updated="emit('updated')" />
    <section v-if="tab === 'settings'" class="admin-settings panel" :aria-label="t('站点设置')"><slot name="settings" /></section>
  </div>
</template>
