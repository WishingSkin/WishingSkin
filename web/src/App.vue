<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import CharacterViewer from './components/CharacterViewer.vue'
import ModelThumbnail from './components/ModelThumbnail.vue'
import SiteFooter from './components/SiteFooter.vue'
import NoticeCenter from './components/NoticeCenter.vue'
import TextureBrowser from './components/TextureBrowser.vue'
import AccountAccess from './components/AccountAccess.vue'
import RegistrationSettings from './components/RegistrationSettings.vue'
import Administration from './components/Administration.vue'
import BrandingSettings from './components/BrandingSettings.vue'
import SettingsWorkspace from './components/SettingsWorkspace.vue'
import ProfileSettings from './components/ProfileSettings.vue'
import ReportsView from './components/ReportsView.vue'
import PointsSettings from './components/PointsSettings.vue'
import PointsPanel from './components/PointsPanel.vue'
import IntegrationPanel from './components/IntegrationPanel.vue'
import TurnstileChallenge from './components/TurnstileChallenge.vue'
import type { AccessMode, SiteAccess } from './accountTypes'
import type { LibraryRecord, TextureRecord } from './textureTypes'
import { validNewPassword } from './passwordPolicy'
import { locale, setLocale, t } from './i18n'

interface User { id: number; email: string; nickname: string; isAdmin: boolean; emailVerified: boolean; legacyAccount: boolean }
interface Session { user: User; csrfToken: string }
interface Character { id: number; uuid: string; name: string; skinHash: string; capeHash: string; model: string }

const user = ref<User | null>(null)
const csrfToken = ref('')
const initializing = ref(true)
const sessionError = ref('')
const email = ref('')
const password = ref('')
const loginBusy = ref(false)
const loginError = ref('')
const loginMessage = ref('')
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordBusy = ref(false)
const passwordError = ref('')
const logoutBusy = ref(false)
const logoutError = ref('')
type Page = 'characters' | 'materials' | 'wardrobe' | 'library' | 'account' | 'administration'
const page = ref<Page>('characters')
type AccountTab = 'dashboard' | 'profile' | 'points' | 'integration' | 'reports'
const accountTab = ref<AccountTab>('dashboard')
const publicHome = ref(true)
const publicLibraryOpen = ref(false)
const materialTab = ref<'skin' | 'cape' | 'upload'>('skin')
const materialSearch = ref('')
const characters = ref<Character[]>([])
const selectedId = ref<number | null>(null)
const charactersBusy = ref(false)
const charactersError = ref('')
const characterLimit = ref(1)
const textures = ref<TextureRecord[]>([])
const texturesBusy = ref(false)
const textureError = ref('')
const wardrobe = ref<LibraryRecord[]>([])
const wardrobeBusy = ref(false)
const wardrobeError = ref('')
const bindingTextures = computed(() => [...textures.value, ...wardrobe.value.filter(item => !textures.value.some(own => own.id === item.id)).map(item => ({ ...item, name: item.displayName }))])
const selectedTextureId = ref('')
const visibleTextures = computed(() => textures.value.filter(texture => texture.kind === materialTab.value))
const selectedTexture = computed(() => visibleTextures.value.find(texture => texture.id === selectedTextureId.value))
const materialFallback = ref('')
const editingTextureId = ref('')
const editTextureName = ref('')
const editTextureDescription = ref('')
const editTexturePublic = ref(false)
const editDescriptionLength = computed(() => Array.from(editTextureDescription.value).length)
const actionMessage = ref('')
const actionError = ref('')
const actionBusy = ref(false)
const actionPage = ref<Page>('characters')
const newCharacterName = ref('')
const renameCharacterName = ref('')
const uploadKind = ref<'skin' | 'cape'>('skin')
const uploadModel = ref('default')
const uploadName = ref('')
const uploadDescription = ref('')
const uploadFile = ref<File | null>(null)
const uploadPublic = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)
const adminCharacterLimit = ref(1)
const bindingDraft = ref({ skin: '', cape: '' })
const descriptionLength = computed(() => Array.from(uploadDescription.value).length)
let sessionVersion = 0
const failedImages = ref<string[]>([])
const selected = computed(() => characters.value.find(character => character.id === selectedId.value))
const viewerKey = computed(() => selected.value ? `${selected.value.id}:${selected.value.model}:${selected.value.skinHash}:${selected.value.capeHash}` : '')
const fallbackViewer = ref('')
const site = ref<SiteAccess>({ appearance: { logoImage: '', accent: 'blue', defaultTheme: 'system' }, noticeFormat: '', noticeHtml: '', name: 'WishingSkin', tagline: '', footer: '', notice: '', registration: 'closed', emailDomains: [], mailEnabled: false, turnstileSiteKey: '', points: { enabled: false, dailyReward: 0, uploadCost: 0, characterCost: 0 } })
watch([() => site.value.name, locale], () => { document.title = `${site.value.name} · ${t('角色与材质')}` }, { immediate: true, flush: 'sync' })
const siteError = ref(false)
const accessMode = ref<AccessMode | null>(null)
const linkToken = ref('')
const captchaRequired = ref(false)
const loginChallengeToken = ref('')
const loginChallenge = ref<InstanceType<typeof TurnstileChallenge> | null>(null)
const emailVerificationRequired = ref(false)
// Remove mail secrets before any third-party widget can load. Reading a link never submits it.
function readMailLink() {
  const fragment = new URLSearchParams(window.location.hash.slice(1))
  const mode = fragment.has('verify') ? 'verify' : fragment.has('reset') ? 'reset' : null
  if (!mode) return
  linkToken.value = fragment.get(mode) || ''
  history.replaceState(null, '', window.location.pathname + window.location.search)
  accessMode.value = mode
}
readMailLink()
async function loadSite() {
  try {
    const response = await api('/api/site')
    if (!response.ok) throw new Error('site')
    site.value = await response.json() as SiteAccess
    siteTheme()
    siteError.value = false
  } catch { siteError.value = true }
}
async function accessCompleted(message: string) {
  const wasReset = accessMode.value === 'reset'
  accessMode.value = null
  linkToken.value = ''
  if (wasReset) clearSession()
  else if (user.value) await restoreSession()
  loginError.value = ''
  loginMessage.value = message
}
const dark = ref(false)
const avatarVersion = ref(0)
let themePreference = ''
function siteTheme() {
  document.documentElement.dataset.accent = site.value.appearance.accent
  const favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (favicon) {
    favicon.href = site.value.appearance.logoImage || '/favicon.svg'
    favicon.type = site.value.appearance.logoImage ? 'image/png' : 'image/svg+xml'
  }
  if (!themePreference) applyTheme(site.value.appearance.defaultTheme === 'system' ? window.matchMedia('(prefers-color-scheme: dark)').matches : site.value.appearance.defaultTheme === 'dark')
}
async function profileUpdated() {
  try {
    const response = await api('/api/auth/me')
    if (response.status === 401) { expireSession(); return }
    if (!response.ok) throw new Error('session')
    acceptSession(await response.json() as Session)
    avatarVersion.value++
  } catch { sessionError.value = '暂时无法连接到服务。请检查连接后重试。' }
}
watch(characters, () => { avatarVersion.value++ })

watch([selected, bindingTextures], () => {
  for (const kind of ['skin', 'cape'] as const) {
    const hash = kind === 'skin' ? selected.value?.skinHash : selected.value?.capeHash
    bindingDraft.value[kind] = hash ? bindingTextures.value.find(item => item.kind === kind && item.fileHash === hash && (kind === 'cape' || item.model === selected.value?.model))?.id || '__current' : ''
  }
})

watch(visibleTextures, items => {
  if (!items.some(texture => texture.id === selectedTextureId.value)) selectedTextureId.value = items[0]?.id || ''
})

watch(selectedTextureId, () => { editingTextureId.value = '' })

function applyTheme(value: boolean) {
  dark.value = value
  document.documentElement.dataset.theme = value ? 'dark' : 'light'
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', value ? '#111824' : '#f4f6fa')
}

function toggleTheme() {
  applyTheme(!dark.value)
  themePreference = dark.value ? 'dark' : 'light'
  try { localStorage.setItem('wishingskin-theme', themePreference) } catch { /* Theme still works without storage. */ }
}

async function api(path: string, options: RequestInit = {}) {
  return fetch(path, { ...options, credentials: 'same-origin' })
}

function acceptSession(session: Session) {
  sessionVersion++
  loginMessage.value = ''
  user.value = session.user
  csrfToken.value = session.csrfToken
  sessionError.value = ''
  publicHome.value = false
  publicLibraryOpen.value = false
}

function clearSession() {
  sessionVersion++
  loginMessage.value = ''
  user.value = null
  csrfToken.value = ''
  characters.value = []
  selectedId.value = null
  textures.value = []
  wardrobe.value = []
  wardrobeBusy.value = false
  wardrobeError.value = ''
  texturesBusy.value = false
  selectedTextureId.value = ''
  materialTab.value = 'skin'
  materialSearch.value = ''
  materialFallback.value = ''
  editingTextureId.value = ''
  editTextureName.value = ''
  editTextureDescription.value = ''
  editTexturePublic.value = false
  newCharacterName.value = ''
  renameCharacterName.value = ''
  uploadName.value = ''
  uploadDescription.value = ''
  uploadKind.value = 'skin'
  uploadModel.value = 'default'
  uploadPublic.value = false
  uploadFile.value = null
  if (uploadInput.value) uploadInput.value.value = ''
  actionMessage.value = ''
  actionError.value = ''
  textureError.value = ''
  charactersError.value = ''
  logoutError.value = ''
  currentPassword.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
  passwordError.value = ''
  characterLimit.value = 1
  adminCharacterLimit.value = 1
  failedImages.value = []
  fallbackViewer.value = ''
  page.value = 'characters'
  accountTab.value = 'dashboard'
  publicHome.value = true
}

async function loadCharacters() {
  const version = sessionVersion
  charactersBusy.value = true
  charactersError.value = ''
  try {
    const response = await api('/api/characters')
    if (version !== sessionVersion) return
    if (response.status === 401) {
      clearSession()
      loginError.value = '登录已过期，请重新登录。'
      return
    }
    if (!response.ok) throw new Error('characters')
    const data: { characters: Character[]; characterLimit: number } = await response.json()
    if (version !== sessionVersion) return
    characters.value = data.characters
    characterLimit.value = data.characterLimit
    adminCharacterLimit.value = data.characterLimit
    selectedId.value = data.characters.some(character => character.id === selectedId.value)
      ? selectedId.value : (data.characters[0]?.id ?? null)
    renameCharacterName.value = selected.value?.name ?? ''
    failedImages.value = []
  } catch {
    if (version === sessionVersion) charactersError.value = '角色暂时无法加载，请重试。'
  } finally {
    charactersBusy.value = false
  }
}

async function loadTextures() {
  const version = sessionVersion
  texturesBusy.value = true
  textureError.value = ''
  try {
    const params = new URLSearchParams({ q: materialSearch.value.trim() })
    const response = await api(`/api/textures?${params}`)
    if (version !== sessionVersion) return
    if (response.status === 401) { clearSession(); loginError.value = '登录已过期，请重新登录。'; return }
    if (!response.ok) throw new Error('textures')
    const data: { textures: TextureRecord[] } = await response.json()
    if (version !== sessionVersion) return
    textures.value = data.textures
    failedImages.value = []
  } catch {
    if (version === sessionVersion) textureError.value = '材质列表暂时无法加载。'
  } finally {
    if (version === sessionVersion) texturesBusy.value = false
  }
}

function searchMaterials() {
  selectedTextureId.value = ''
  void loadTextures()
}

async function loadWardrobe() {
  const version = sessionVersion
  wardrobeBusy.value = true
  wardrobeError.value = ''
  try {
    const response = await api('/api/wardrobe')
    if (version !== sessionVersion) return
    if (response.status === 401) { expireSession(); return }
    if (!response.ok) throw new Error('wardrobe')
    const data: { textures: LibraryRecord[] } = await response.json()
    if (version === sessionVersion) wardrobe.value = data.textures
  } catch {
    if (version === sessionVersion) wardrobeError.value = '衣柜暂时无法加载，请重试。'
  } finally {
    if (version === sessionVersion) wardrobeBusy.value = false
  }
}

function expireSession() { clearSession(); loginError.value = '登录已过期，请重新登录。' }

async function wearCollected(texture: LibraryRecord) {
  page.value = 'characters'
  await nextTick()
  bindingDraft.value[texture.kind] = texture.id
}

async function collectOwnTexture() {
  if (!selectedTexture.value) return
  const ok = await mutate(`/api/wardrobe/${selectedTexture.value.id}`, 'PUT', undefined, '')
  if (!ok) return
  actionMessage.value = '已加入我的衣柜。'
  await loadWardrobe()
}

async function mutate(path: string, method: string, body?: BodyInit, contentType = 'application/json') {
  if (actionBusy.value) return false
  const version = sessionVersion
  actionBusy.value = true
  actionPage.value = page.value
  actionError.value = ''
  actionMessage.value = ''
  try {
    const headers: Record<string, string> = { 'X-CSRF-Token': csrfToken.value }
    if (contentType) headers['Content-Type'] = contentType
    const response = await api(path, { method, headers, body })
    if (version !== sessionVersion) return false
    if (response.status === 401) { clearSession(); loginError.value = '登录已过期，请重新登录。'; return false }
    if (!response.ok) {
      const payload = await response.json().catch(() => ({})) as { error?: string; pointsRulesChanged?: boolean }
      if (version === sessionVersion) actionError.value = payload.error || '操作未完成，请重试。'
      if (payload.pointsRulesChanged) await loadSite()
      return false
    }
    return response
  } catch {
    if (version === sessionVersion) actionError.value = '无法连接到服务，请检查连接后重试。'
    return false
  } finally {
    actionBusy.value = false
  }
}

async function createCharacter() {
  if (!newCharacterName.value.trim()) return
  const ok = await mutate('/api/characters', 'POST', JSON.stringify({ name: newCharacterName.value.trim(), expectedCost: site.value.points.enabled ? site.value.points.characterCost : 0 }))
  if (!ok) return
  newCharacterName.value = ''
  actionMessage.value = '角色已创建。'
  await loadCharacters()
}

async function renameCharacter() {
  if (!selected.value || !renameCharacterName.value.trim()) return
  const ok = await mutate(`/api/characters/${selected.value.id}`, 'PATCH', JSON.stringify({ name: renameCharacterName.value.trim() }))
  if (!ok) return
  actionMessage.value = '角色名已更新，UUID 保持不变。'
  await loadCharacters()
}

async function deleteCharacter() {
  if (!selected.value || !window.confirm(t('删除角色“{name}”及其外观绑定？此操作无法撤销。', { name: selected.value.name }))) return
  const ok = await mutate(`/api/characters/${selected.value.id}`, 'DELETE', undefined, '')
  if (!ok) return
  selectedId.value = null
  actionMessage.value = '角色已删除。'
  await Promise.all([loadCharacters(), loadTextures()])
}

async function bindTexture(kind: 'skin' | 'cape', recordId: string) {
  if (!selected.value) return
  const ok = await mutate(`/api/characters/${selected.value.id}/textures/${kind}`, 'PUT', JSON.stringify({ recordId }))
  if (!ok) return
  fallbackViewer.value = ''
  actionMessage.value = recordId ? (kind === 'skin' ? '皮肤已应用。' : '披风已应用。') : (kind === 'skin' ? '皮肤已清除。' : '披风已清除。')
  await Promise.all([loadCharacters(), loadTextures()])
}

async function uploadTexture() {
  const version = sessionVersion
  actionPage.value = 'materials'
  actionMessage.value = ''
  actionError.value = ''
  if (!uploadFile.value) { actionError.value = '请选择 PNG 文件。'; return }
  if (descriptionLength.value > 200) { actionError.value = '材质描述不能超过 200 个字符。'; return }
  const form = new FormData()
  form.set('file', uploadFile.value)
  form.set('kind', uploadKind.value)
  form.set('model', uploadKind.value === 'skin' ? uploadModel.value : 'default')
  form.set('name', uploadName.value.trim() || uploadFile.value.name.replace(/\.png$/i, ''))
  form.set('description', uploadDescription.value)
  form.set('isPublic', String(uploadPublic.value))
  form.set('expectedCost', String(site.value.points.enabled ? site.value.points.uploadCost : 0))
  const ok = await mutate('/api/textures', 'POST', form, '')
  if (!ok) return
  const uploaded: TextureRecord = await ok.json()
  if (version !== sessionVersion) return
  materialTab.value = uploaded.kind
  actionMessage.value = '材质已上传到我的材质。'
  uploadName.value = ''
  uploadDescription.value = ''
  uploadFile.value = null
  uploadPublic.value = false
  if (uploadInput.value) uploadInput.value.value = ''
  await loadTextures()
  if (version === sessionVersion) selectedTextureId.value = uploaded.id
}

async function saveCharacterLimit() {
  const ok = await mutate('/api/admin/settings', 'PATCH', JSON.stringify({ characterLimit: adminCharacterLimit.value }))
  if (!ok) return
  characterLimit.value = adminCharacterLimit.value
  actionMessage.value = '站点角色数量上限已保存。'
}

function editTexture() {
  if (!selectedTexture.value?.owned) return
  editingTextureId.value = selectedTexture.value.id
  editTextureName.value = selectedTexture.value.name
  editTextureDescription.value = selectedTexture.value.description
  editTexturePublic.value = selectedTexture.value.public
}

async function saveTexture() {
  if (!editingTextureId.value || !editTextureName.value.trim()) return
  actionPage.value = 'materials'
  actionError.value = ''
  actionMessage.value = ''
  if (editDescriptionLength.value > 200) { actionError.value = '材质描述不能超过 200 个字符。'; return }
  const ok = await mutate(`/api/textures/${editingTextureId.value}`, 'PATCH', JSON.stringify({ name: editTextureName.value.trim(), description: editTextureDescription.value, isPublic: editTexturePublic.value }))
  if (!ok) return
  editingTextureId.value = ''
  actionMessage.value = '材质资料已保存。'
  await Promise.all([loadTextures(), loadWardrobe()])
}

async function deleteTexture() {
  if (!selectedTexture.value?.owned || !window.confirm(t('删除材质“{name}”？已穿戴的角色外观会保留，此记录将无法再次选择。', { name: selectedTexture.value.name }))) return
  const ok = await mutate(`/api/textures/${selectedTexture.value.id}`, 'DELETE', undefined, '')
  if (!ok) return
  editingTextureId.value = ''
  actionMessage.value = '材质记录已删除，已穿戴的角色外观保持不变。'
  await Promise.all([loadTextures(), loadWardrobe()])
}

async function restoreSession() {
  initializing.value = true
  sessionError.value = ''
  try {
    const response = await api('/api/auth/me')
    if (response.ok) {
      acceptSession(await response.json() as Session)
      await Promise.all([loadCharacters(), loadTextures(), loadWardrobe()])
    } else if (response.status !== 401) {
      throw new Error('session')
    }
  } catch {
    sessionError.value = '暂时无法连接到服务。请检查连接后重试。'
  } finally {
    initializing.value = false
  }
}

async function login() {
  if (loginBusy.value) return
  loginBusy.value = true
  loginError.value = ''
  loginMessage.value = ''
  try {
    const response = await api('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: email.value.trim(), password: password.value, turnstileToken: loginChallengeToken.value }),
    })
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      captchaRequired.value = captchaRequired.value || !!data.captchaRequired
      emailVerificationRequired.value = !!data.emailVerificationRequired
      loginError.value = data.error || (response.status === 429 ? '尝试次数过多，请稍后再试。' : '暂时无法登录，请稍后重试。')
      return
    }
    acceptSession(await response.json() as Session)
    captchaRequired.value = false
    emailVerificationRequired.value = false
    password.value = ''
    await Promise.all([loadCharacters(), loadTextures(), loadWardrobe()])
  } catch {
    loginError.value = '无法连接到服务，请检查连接后重试。'
  } finally {
    loginBusy.value = false
    loginChallengeToken.value = ''
    void loginChallenge.value?.reset()
  }
}

async function logout() {
  if (logoutBusy.value) return
  logoutBusy.value = true
  logoutError.value = ''
  try {
    const response = await api('/api/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrfToken.value } })
    if (!response.ok && response.status !== 401) throw new Error('logout')
    clearSession()
  } catch {
    logoutError.value = '退出未完成，请重试。'
  } finally {
    logoutBusy.value = false
  }
}

async function changePassword() {
  if (passwordBusy.value) return
  passwordError.value = ''
  if (!validNewPassword(newPassword.value)) { passwordError.value = '新密码须为 8–128 个字符，且同时包含英文字母、数字和符号'; return }
  if (newPassword.value !== confirmPassword.value) { passwordError.value = '两次输入的新密码不一致。'; return }
  const version = sessionVersion
  passwordBusy.value = true
  try {
    const response = await api('/api/auth/password', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken.value }, body: JSON.stringify({ currentPassword: currentPassword.value, newPassword: newPassword.value }) })
    if (version !== sessionVersion) return
    if (response.status === 401) { expireSession(); return }
    if (!response.ok) {
      const payload: { error?: string } = await response.json().catch(() => ({}))
      if (version === sessionVersion) passwordError.value = payload.error || '暂时无法修改密码'
      return
    }
    clearSession()
    loginError.value = ''
    loginMessage.value = '密码已修改，所有网页会话与游戏令牌已撤销。请用新密码重新登录。'
  } catch {
    if (version === sessionVersion) passwordError.value = '无法确认修改结果，请尝试用新密码重新登录。'
  } finally {
    passwordBusy.value = false
  }
}

function textureUrl(hash: string) { return `/textures/${encodeURIComponent(hash)}` }
function modelLabel(model: string) {
  if (model === 'slim') return t('纤细手臂')
  if (model === 'default') return t('标准手臂')
  return t('未指定')
}
function imageFailed(hash: string) { if (!failedImages.value.includes(hash)) failedImages.value.push(hash) }

onMounted(() => {
  window.addEventListener('hashchange', readMailLink)
  let preference: string | null = null
  try { preference = localStorage.getItem('wishingskin-theme') } catch { /* Use the system preference. */ }
  themePreference = preference === 'light' || preference === 'dark' ? preference : ''
  applyTheme(themePreference ? themePreference === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches)
  void restoreSession()
  void loadSite()
})
onBeforeUnmount(() => window.removeEventListener('hashchange', readMailLink))
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <a class="brand" href="/" :aria-label="t('{name} 首页', { name: site.name })" :title="site.name"><img v-if="site.appearance.logoImage" class="brand-logo" :src="site.appearance.logoImage" alt="" width="32" height="32" /><span v-else class="brand-mark" aria-hidden="true"><i></i><i></i><i></i></span><span class="brand-name"><template v-if="site.name === 'WishingSkin'">Wishing<span class="brand-light">Skin</span></template><template v-else>{{ site.name }}</template></span></a>
      <div class="topbar-actions">
        <NoticeCenter :notice="site.notice" :format="site.noticeFormat" />
        <select class="language-select" :value="locale" :aria-label="t('界面语言')" @change="setLocale(($event.target as HTMLSelectElement).value)"><option value="zh-CN" lang="zh-CN">简体中文</option><option value="en" lang="en">English</option></select>
        <button class="icon-button theme-toggle" type="button" :aria-label="dark ? t('切换浅色主题') : t('切换深色主题')" @click="toggleTheme">
          <svg v-if="dark" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/></svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M20.5 13.2A8.5 8.5 0 0 1 10.8 3.5a8.5 8.5 0 1 0 9.7 9.7Z"/></svg>
        </button>
        <button v-if="user" class="topbar-user" type="button" :title="user.nickname || t('我的账号')" :aria-label="t('打开账号：{name}', { name: user.nickname || t('我的账号') })" @click="page = 'account'; publicLibraryOpen = false; accessMode = null"><img class="user-avatar" :src="'/api/avatars/' + user.id + '.png?v=' + avatarVersion" alt="" width="34" height="34" /><span>{{ user.nickname || t('我的账号') }}</span></button>
      </div>
    </header>

    <main v-if="initializing" class="initial-state" aria-live="polite"><span class="loading-dot"></span><p>{{ t("正在打开你的工作台…") }}</p></main>

    <main v-else-if="accessMode" class="access-layout"><AccountAccess :mode="accessMode" :token="linkToken" :site="site" :email="user?.email || email" :dark="dark" @back="accessMode = null; linkToken = ''" @completed="accessCompleted" /></main>

    <main v-else-if="!user && publicLibraryOpen" class="public-library-layout"><button class="secondary-button public-library-back" type="button" @click="publicLibraryOpen = false">{{ t("返回主页") }}</button><TextureBrowser mode="library" csrf-token="" :wardrobe="[]" :wardrobe-busy="false" wardrobe-error="" @login="publicLibraryOpen = false; publicHome = false" /></main>
    <main v-else-if="!user && publicHome" class="public-home-layout">
      <section class="public-home-hero" aria-labelledby="public-home-title">
        <div class="public-home-copy"><span class="small-square" aria-hidden="true"></span><p class="eyebrow">{{ t('Minecraft 外观工作台') }}</p><h1 id="public-home-title">{{ site.name }}</h1><p class="public-home-tagline">{{ site.tagline || t('每一个像素，都是你的模样。') }}</p><p class="public-home-description">{{ t('管理角色、皮肤和披风，在公开库中发现新的外观，并通过 Yggdrasil 接入游戏。') }}</p><div class="public-home-actions"><button class="primary-button" type="button" @click="publicHome = false">{{ t('登录工作台') }}<span aria-hidden="true">↗</span></button><button class="secondary-button" type="button" @click="publicLibraryOpen = true">{{ t('浏览公开库') }}</button></div></div>
        <div class="public-home-preview checkerboard" :aria-label="t('默认角色预览')"><ModelThumbnail class="login-model-thumbnail" name="Steve" model="default" skin-hash="" cape-hash="" /><ModelThumbnail class="login-model-thumbnail" name="Alex" model="slim" skin-hash="" cape-hash="" /></div>
      </section>
      <section class="public-home-features" :aria-label="t('站点功能')"><article class="public-home-feature panel"><span>01</span><h2>{{ t('角色与外观') }}</h2><p>{{ t('用模型预览管理标准和纤细模型，皮肤与披风分别绑定。') }}</p></article><article class="public-home-feature panel"><span>02</span><h2>{{ t('公开库') }}</h2><p>{{ t('浏览公开材质并收藏到自己的衣柜。') }}</p></article><article class="public-home-feature panel"><span>03</span><h2>{{ t('游戏接入') }}</h2><p>{{ t('使用本站提供的 Yggdrasil 或兼容地址加载外观。') }}</p></article></section>
    </main>
    <main v-else-if="!user" class="login-layout">
      <section class="welcome-panel" aria-labelledby="welcome-title">
        <div class="welcome-copy"><span class="small-square" aria-hidden="true"></span><h1 id="welcome-title">{{ t("每个角色，") }}<br>{{ t("都有自己的模样。") }}</h1><p>{{ t("在这里查看你的 Minecraft 角色，") }}<br class="desktop-break">{{ t("找到属于它的皮肤和披风。") }}</p></div>
        <div class="login-preview checkerboard"><ModelThumbnail class="login-model-thumbnail" name="Steve" model="default" skin-hash="" cape-hash="" /><ModelThumbnail class="login-model-thumbnail" name="Alex" model="slim" skin-hash="" cape-hash="" /></div>
      </section>
      <section class="login-panel" aria-labelledby="login-title">
        <div class="login-heading"><span class="section-symbol" aria-hidden="true">↳</span><h2 id="login-title">{{ t("登录工作台") }}</h2><p>{{ t("使用你的已有账号继续。") }}</p><button class="text-button" type="button" @click="publicHome = true">{{ t('返回主页') }}</button></div>
        <div v-if="sessionError" class="notice error" role="alert"><p>{{ t(sessionError) }}</p><button class="text-button" type="button" @click="restoreSession">{{ t("重新连接") }}</button></div>
        <form @submit.prevent="login">
          <label for="email">{{ t("邮箱") }}</label><input id="email" v-model="email" name="email" type="email" autocomplete="username" placeholder="you@example.com" required :disabled="loginBusy" />
          <label for="password">{{ t("密码") }}</label><input id="password" v-model="password" name="password" type="password" autocomplete="current-password" :placeholder="t('输入你的密码')" required :disabled="loginBusy" />
          <TurnstileChallenge v-if="captchaRequired" ref="loginChallenge" :site-key="site.turnstileSiteKey" action="login" :dark="dark" @token="loginChallengeToken = $event" />
          <p v-if="loginError" class="form-error" role="alert">{{ t(loginError) }}</p>
          <p v-if="loginMessage" class="action-success" role="status">{{ t(loginMessage) }}</p>
          <button v-if="emailVerificationRequired" class="text-button" type="button" @click="accessMode = 'mail'">{{ t('重新发送验证邮件') }}</button>
          <button class="primary-button login-button" type="submit" :disabled="loginBusy || (captchaRequired && !loginChallengeToken)">{{ loginBusy ? t('正在登录…') : t('登录') }}<span aria-hidden="true">↗</span></button>
        </form>
        <button class="secondary-button browse-library" type="button" @click="publicLibraryOpen = true">{{ t("浏览公开库") }}</button>
        <div class="access-links"><button v-if="site.registration !== 'closed'" class="text-button" type="button" @click="accessMode = 'register'">{{ t('创建账号') }}</button><button class="text-button" type="button" @click="accessMode = 'mail'">{{ t('验证邮箱 / 找回密码') }}</button></div>
        <div v-if="siteError" class="notice" role="alert">{{ t('暂时无法读取站点设置') }} <button class="text-button" type="button" @click="loadSite">{{ t('重试') }}</button></div>
        <div v-else-if="site.registration === 'closed'" class="registration-note"><span class="status-dot" aria-hidden="true"></span><p>{{ t('目前暂未开放注册。') }}<br><span>{{ t("账号由站点管理员提供。") }}</span></p></div>
      </section>
    </main>

    <div v-else class="workspace-layout">
      <aside class="sidebar">
        <div class="sidebar-title">{{ t("我的工作台") }}</div>
        <nav :aria-label="t('工作台导航')" :class="{ 'has-admin': user.isAdmin }">
          <button type="button" class="nav-button" :class="{ active: page === 'characters' }" :aria-current="page === 'characters' ? 'page' : undefined" @click="page = 'characters'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="8" y="3" width="8" height="7" rx="1"/><path d="M7 13h10v8H7zM3 13h4v6H3zm14 0h4v6h-4zM12 13v8"/></svg>{{ t("角色管理") }}<span class="nav-count">{{ characters.length }}</span></button>
          <button type="button" class="nav-button" :class="{ active: page === 'materials' }" :aria-current="page === 'materials' ? 'page' : undefined" @click="page = 'materials'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M3 7h18v14H3zM2 3h20v4H2zM9 11h6"/></svg>{{ t("我的材质") }}<span class="nav-count">{{ textures.length }}</span></button>
          <button type="button" class="nav-button" :class="{ active: page === 'wardrobe' }" :aria-current="page === 'wardrobe' ? 'page' : undefined" @click="page = 'wardrobe'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M4 3h16v18H4zM12 3v18M9 11v3m6-3v3"/></svg>{{ t("我的衣柜") }}<span class="nav-count">{{ wardrobe.length }}</span></button>
          <button type="button" class="nav-button" :class="{ active: page === 'library' }" :aria-current="page === 'library' ? 'page' : undefined" @click="page = 'library'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M3 4h7v7H3zM14 4h7v7h-7zM3 15h7v6H3zM14 15h7v6h-7z"/></svg>{{ t("公开库") }}</button>
          <button type="button" class="nav-button" :class="{ active: page === 'account' }" :aria-current="page === 'account' ? 'page' : undefined" @click="page = 'account'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/></svg>{{ t("账号") }}</button>
          <button v-if="user.isAdmin" type="button" class="nav-button" :class="{ active: page === 'administration' }" :aria-current="page === 'administration' ? 'page' : undefined" @click="page = 'administration'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z"/><path d="m8 12 3 3 5-6"/></svg>{{ t('管理后台') }}</button>
        </nav>
      </aside>

      <main class="workspace-content">
        <Administration v-if="page === 'administration' && user.isAdmin" :csrf-token="csrfToken" :user-id="String(user.id)" @expired="expireSession" @updated="loadSite"><template #settings>
          <SettingsWorkspace :site="site" :csrf-token="csrfToken" @updated="loadSite" @expired="expireSession">
          <section class="character-setting" :aria-label="t('角色数量设置')"><form class="inline-form" @submit.prevent="saveCharacterLimit"><label for="character-limit">{{ t("站点每账号角色上限") }}</label><div class="inline-controls"><input id="character-limit" v-model.number="adminCharacterLimit" type="number" min="1" step="1" required :disabled="actionBusy" /><button class="secondary-button" type="submit" :disabled="actionBusy">{{ t("保存设置") }}</button></div><small>{{ t("已有角色会保留；上限降低后，用户暂时不能继续新增角色。") }}</small></form></section>
          <template v-if="actionPage === 'administration'"><p v-if="actionError" class="form-error" role="alert">{{ t(actionError) }}</p><p v-if="actionMessage" class="action-success" role="status">{{ t(actionMessage) }}</p></template>
          <RegistrationSettings :site="site" :csrf-token="csrfToken" @updated="loadSite" @expired="expireSession" />
          <BrandingSettings :site="site" :csrf-token="csrfToken" @updated="loadSite" @expired="expireSession" />
          <PointsSettings :rules="site.points" :csrf-token="csrfToken" @updated="loadSite" @expired="expireSession" />
          </SettingsWorkspace></template></Administration>
        <TextureBrowser v-if="page === 'wardrobe' || page === 'library'" :key="`${user.id}:${page}`" :mode="page" :csrf-token="csrfToken" :wardrobe="wardrobe" :wardrobe-busy="wardrobeBusy" :wardrobe-error="wardrobeError" @refresh="loadWardrobe" @changed="loadWardrobe" @wear="wearCollected" @expired="expireSession" />
        <template v-if="page === 'characters'">
          <div class="page-heading"><div><h1>{{ t("角色管理") }}<span class="heading-pixel" aria-hidden="true"></span></h1></div><button class="secondary-button" type="button" :disabled="charactersBusy || actionBusy" @click="loadCharacters">{{ charactersBusy ? t('正在刷新…') : t('刷新列表') }}</button></div>
          <template v-if="actionPage === 'characters'"><p v-if="actionError" class="form-error action-feedback" role="alert">{{ t(actionError) }}</p><p v-if="actionMessage" class="action-success" role="status">{{ t(actionMessage) }}</p></template>
          <div v-if="charactersBusy" class="empty-state panel" role="status"><span class="loading-dot"></span><h2>{{ t("正在加载角色…") }}</h2></div>
          <div v-else-if="charactersError" class="empty-state panel" role="alert"><span class="empty-symbol" aria-hidden="true">!</span><h2>{{ t("暂时没有加载出来") }}</h2><p>{{ t(charactersError) }}</p><button class="secondary-button" type="button" @click="loadCharacters">{{ t("重新加载") }}</button></div>
          <div v-else class="character-workspace">
            <section class="character-list panel" aria-labelledby="character-list-title"><div class="panel-title"><h2 id="character-list-title">{{ t("角色列表") }}</h2><span>{{ t('{count} / {limit} 个', { count: characters.length, limit: characterLimit }) }}</span></div><div v-if="characters.length" class="character-options"><button v-for="character in characters" :key="character.id" type="button" class="character-option" :class="{ selected: character.id === selectedId }" :aria-pressed="character.id === selectedId" @click="selectedId = character.id; renameCharacterName = character.name"><ModelThumbnail class="character-model-thumbnail" :name="character.name" :model="character.model" :skin-hash="character.skinHash" :cape-hash="character.capeHash" /><span class="character-name"><strong>{{ character.name }}</strong><small>{{ modelLabel(character.model) }}</small></span><span class="selection-indicator" aria-hidden="true"></span></button></div><div v-else class="list-empty"><span aria-hidden="true">♙</span><p>{{ t("还没有角色，先创建一个吧。") }}</p></div>
              <p v-if="site.points.enabled && characters.length < characterLimit" class="point-fee">{{ t('新建一个角色需 {cost} 积分；失败不扣费。', { cost: site.points.characterCost }) }}</p><form v-if="characters.length < characterLimit" class="inline-form create-character-form" @submit.prevent="createCharacter"><label for="new-character-name">{{ t("新建角色") }}</label><div class="inline-controls"><input id="new-character-name" v-model="newCharacterName" name="name" autocomplete="off" minlength="3" maxlength="16" pattern="[A-Za-z0-9_]{3,16}" required :placeholder="t('3–16 位英文、数字或下划线')" :disabled="actionBusy" /><button class="primary-button" type="submit" :disabled="actionBusy || !newCharacterName.trim()">{{ t("创建") }}</button></div></form>
              <p v-else class="limit-note">{{ characters.length > characterLimit ? t('已有角色超出当前上限，均会保留；暂时无法新增。') : t('已达到角色上限。') }}</p>
            </section>
            <div class="character-tools">
            <section v-if="selected" class="character-detail panel" aria-labelledby="character-title">
              <div class="detail-heading"><span class="detail-icon" aria-hidden="true">▦</span><div><h2 id="character-title">{{ selected.name }}</h2><span>{{ modelLabel(selected.model) }}</span></div></div>
              <dl class="character-meta"><div><dt>{{ t("角色 UUID") }}</dt><dd>{{ selected.uuid }}</dd></div></dl>
              <CharacterViewer :key="viewerKey" :name="selected.name" :model="selected.model" :skin-hash="selected.skinHash" :cape-hash="selected.capeHash" @fallback="fallbackViewer = viewerKey" />
              <section class="management-panel" :aria-label="t('角色和材质操作')">
                <div class="management-heading"><div><h3>{{ t("角色设置") }}</h3><p>{{ t("改名会保留当前 UUID。") }}</p></div><button class="danger-button" type="button" :disabled="actionBusy" @click="deleteCharacter">{{ t("删除角色") }}</button></div>
                <form class="inline-form rename-form" @submit.prevent="renameCharacter"><label :for="`rename-${selected.id}`">{{ t("角色名") }}</label><div class="inline-controls"><input :id="`rename-${selected.id}`" v-model="renameCharacterName" name="name" autocomplete="off" minlength="3" maxlength="16" pattern="[A-Za-z0-9_]{3,16}" required :disabled="actionBusy" /><button class="secondary-button" type="submit" :disabled="actionBusy || renameCharacterName.trim() === selected.name">{{ t("保存名称") }}</button></div></form>
                <div class="binding-grid">
                  <form class="binding-form" @submit.prevent="bindTexture('skin', bindingDraft.skin)"><label class="binding-field"><span>{{ t("皮肤") }}</span><select v-model="bindingDraft.skin" :aria-label="t('皮肤')" :disabled="actionBusy || charactersBusy || texturesBusy || wardrobeBusy"><option v-if="bindingDraft.skin === '__current'" value="__current" disabled>{{ t("当前皮肤（暂无可选记录）") }}</option><option value="">{{ t("不使用皮肤") }}</option><option v-for="texture in bindingTextures.filter(item => item.kind === 'skin')" :key="texture.id" :value="texture.id">{{ texture.name }}{{ texture.kind === 'skin' && texture.model === 'slim' ? t(' · 纤细') : '' }}{{ texture.public ? t(' · 公开') : t(' · 未公开') }}</option></select></label><button class="secondary-button" type="submit" :disabled="actionBusy || charactersBusy || texturesBusy || wardrobeBusy || bindingDraft.skin === '__current'">{{ t("应用皮肤") }}</button></form>
                  <form class="binding-form" @submit.prevent="bindTexture('cape', bindingDraft.cape)"><label class="binding-field"><span>{{ t("披风") }}</span><select v-model="bindingDraft.cape" :aria-label="t('披风')" :disabled="actionBusy || charactersBusy || texturesBusy || wardrobeBusy"><option v-if="bindingDraft.cape === '__current'" value="__current" disabled>{{ t("当前披风（暂无可选记录）") }}</option><option value="">{{ t("不使用披风") }}</option><option v-for="texture in bindingTextures.filter(item => item.kind === 'cape')" :key="texture.id" :value="texture.id">{{ texture.name }}{{ texture.kind === 'skin' && texture.model === 'slim' ? t(' · 纤细') : '' }}{{ texture.public ? t(' · 公开') : t(' · 未公开') }}</option></select></label><button class="secondary-button" type="submit" :disabled="actionBusy || charactersBusy || texturesBusy || wardrobeBusy || bindingDraft.cape === '__current'">{{ t("应用披风") }}</button></form>
                </div>
                <div class="material-link"><p v-if="textureError" class="form-error" role="alert">{{ t(textureError) }} <button class="text-button" type="button" :disabled="texturesBusy" @click="loadTextures">{{ t("重新加载") }}</button></p><p v-if="wardrobeError" class="form-error" role="alert">{{ t(wardrobeError) }} <button class="text-button" type="button" :disabled="wardrobeBusy" @click="loadWardrobe">{{ t("重新加载衣柜") }}</button></p><p>{{ t("需要新的皮肤或披风？") }}<button class="text-button" type="button" @click="page = 'materials'; materialTab = 'upload'">{{ t("前往上传材质") }}</button><button class="text-button" type="button" @click="page = 'wardrobe'">{{ t("打开我的衣柜") }}</button></p></div>
              </section>
              <details :key="`sources:${viewerKey}`" class="texture-sources" :open="fallbackViewer === viewerKey">
                <summary>{{ t("PNG 材质与下载") }}</summary>
                <div class="texture-grid">
                <section v-for="texture in [{ label: t('皮肤'), hash: selected.skinHash, kind: 'skin' }, { label: t('披风'), hash: selected.capeHash, kind: 'cape' }]" :key="`${selected.id}-${texture.kind}`" class="texture-section"><div class="texture-heading"><h3>{{ texture.label }}</h3><a v-if="texture.hash" class="download-link" :href="textureUrl(texture.hash)" :download="`${selected.name}-${texture.kind}.png`">{{ t("下载 PNG") }}<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M10 3v9m-3-3 3 3 3-3M4 13v4h12v-4"/></svg></a></div><div class="texture-preview checkerboard" :class="{ 'has-texture': texture.hash && !failedImages.includes(texture.hash) }"><img v-if="texture.hash && !failedImages.includes(texture.hash)" :src="textureUrl(texture.hash)" :alt="t('角色“{name}”的 {kind} PNG 材质平面图', { name: selected.name, kind: texture.label })" @error="imageFailed(texture.hash)" /><div v-else class="texture-placeholder"><span aria-hidden="true">{{ texture.hash ? '!' : '＋' }}</span><p>{{ texture.hash ? t('材质预览加载失败') : t('尚未设置{kind}', { kind: texture.label }) }}</p><small v-if="texture.hash">{{ t("可尝试下载或刷新列表") }}</small></div></div></section>
                </div>
              </details>
            </section>
            <section v-else class="empty-state panel"><span class="empty-character" aria-hidden="true">♙</span><h2>{{ t("先创建你的角色") }}</h2><p>{{ t("创建后，就可以为它选择皮肤和披风。也可以先上传材质。") }}</p><button class="secondary-button" type="button" @click="page = 'materials'; materialTab = 'upload'">{{ t("前往上传材质") }}</button></section>
            </div>
          </div>
        </template>
        <section v-show="page === 'materials'" class="materials-page" aria-labelledby="materials-title">
          <div class="page-heading"><div><h1 id="materials-title">{{ t("我的材质") }}<span class="heading-pixel" aria-hidden="true"></span></h1></div><button class="secondary-button" type="button" :disabled="texturesBusy || actionBusy" @click="loadTextures">{{ texturesBusy ? t('正在刷新…') : t('刷新列表') }}</button></div>
          <template v-if="actionPage === 'materials'"><p v-if="actionError" class="form-error action-feedback" role="alert">{{ t(actionError) }}</p><p v-if="actionMessage" class="action-success" role="status">{{ t(actionMessage) }}</p></template>
          <nav class="material-tabs" :aria-label="t('材质分类')">
            <button v-for="kind in (['skin', 'cape'] as const)" :key="kind" type="button" :class="{ active: materialTab === kind }" :aria-current="materialTab === kind ? 'page' : undefined" @click="materialTab = kind">{{ kind === 'skin' ? t('皮肤') : t('披风') }}<span>{{ textures.filter(texture => texture.kind === kind).length }}</span></button>
            <button type="button" :class="{ active: materialTab === 'upload' }" :aria-current="materialTab === 'upload' ? 'page' : undefined" @click="materialTab = 'upload'">{{ t("上传材质") }}<span aria-hidden="true">＋</span></button>
          </nav>
          <div v-if="materialTab !== 'upload'" class="materials-workspace">
            <section class="material-library panel" :aria-label="materialTab === 'skin' ? t('皮肤列表') : t('披风列表')" :aria-busy="texturesBusy">
              <form class="library-filters material-search" role="search" @submit.prevent="searchMaterials"><label class="binding-field"><span>{{ t('搜索我的材质') }}</span><input v-model="materialSearch" maxlength="255" :placeholder="t('名称、描述或图片哈希')" :disabled="texturesBusy" /></label><button class="primary-button" type="submit" :disabled="texturesBusy">{{ t('搜索') }}</button></form>
              <div class="panel-title"><h2>{{ materialTab === 'skin' ? t('皮肤') : t('披风') }}</h2><span>{{ t('{count} 份材质', { count: visibleTextures.length }) }}</span></div>
              <div v-if="texturesBusy" class="empty-state" role="status"><span class="loading-dot"></span><h2>{{ t("正在加载材质…") }}</h2></div>
              <div v-else-if="textureError" class="empty-state" role="alert"><span class="empty-symbol" aria-hidden="true">!</span><h2>{{ t("暂时没有加载出来") }}</h2><p>{{ t(textureError) }}</p><button class="secondary-button" type="button" @click="loadTextures">{{ t("重新加载") }}</button></div>
              <div v-else-if="!visibleTextures.length" class="empty-state"><span class="empty-symbol" aria-hidden="true">＋</span><h2>{{ t(materialTab === 'skin' ? '还没有皮肤' : '还没有披风') }}</h2><p>{{ t("上传一份 PNG 材质，在这里预览和查看。") }}</p><button class="secondary-button" type="button" @click="uploadKind = materialTab as 'skin' | 'cape'; materialTab = 'upload'">{{ t(materialTab === 'skin' ? '上传皮肤' : '上传披风') }}</button></div>
              <div v-else class="material-cards"><button v-for="texture in visibleTextures" :key="texture.id" class="material-card" :class="{ selected: texture.id === selectedTextureId }" type="button" :aria-pressed="texture.id === selectedTextureId" @click="selectedTextureId = texture.id"><ModelThumbnail class="material-thumbnail" :name="texture.name" :model="texture.kind === 'skin' ? texture.model : 'default'" :skin-hash="texture.kind === 'skin' ? texture.fileHash : ''" :cape-hash="texture.kind === 'cape' ? texture.fileHash : ''" /><span class="material-card-copy"><strong>{{ texture.name }}</strong><span>{{ texture.kind === 'skin' ? modelLabel(texture.model) : t('披风') }}<small>{{ texture.public ? t('公开') : t('未公开') }}</small></span></span></button></div>
            </section>
            <aside v-if="selectedTexture && !texturesBusy && !textureError" class="material-preview panel" aria-labelledby="material-preview-title">
              <div class="material-preview-heading"><h2 id="material-preview-title">{{ t("材质预览") }}</h2><span>{{ selectedTexture.public ? t('公开') : t('未公开') }}</span></div>
              <CharacterViewer v-if="page === 'materials'" :key="selectedTexture.id" :name="selectedTexture.name" :model="selectedTexture.kind === 'skin' ? selectedTexture.model : 'default'" :skin-hash="selectedTexture.kind === 'skin' ? selectedTexture.fileHash : ''" :cape-hash="selectedTexture.kind === 'cape' ? selectedTexture.fileHash : ''" @fallback="materialFallback = selectedTexture.id" />
              <div class="material-description"><h3>{{ selectedTexture.name }}</h3><p>{{ selectedTexture.description || t('暂无描述。') }}</p></div>
              <div v-if="selectedTexture.owned" class="material-editing">
                <form v-if="editingTextureId === selectedTexture.id" class="texture-edit-form" @submit.prevent="saveTexture"><fieldset :disabled="actionBusy"><label class="binding-field"><span>{{ t("材质名称") }}</span><input v-model="editTextureName" maxlength="255" required /></label><label class="binding-field description-field"><span>{{ t("材质描述") }} <small>{{ editDescriptionLength }}/200</small></span><textarea v-model="editTextureDescription" rows="3"></textarea></label><label class="visibility-choice"><input v-model="editTexturePublic" type="checkbox" /><span><strong>{{ t("公开这份材质") }}</strong><small>{{ t("未公开后保留原收藏和穿戴权限，禁止其他用户新增收藏；已知图片 URL 仍可下载。") }}</small></span></label><div class="texture-edit-actions"><button class="primary-button" type="submit">{{ t("保存资料") }}</button><button class="secondary-button" type="button" @click="editingTextureId = ''">{{ t("取消") }}</button></div></fieldset></form>
                <div v-else class="texture-edit-actions"><button class="secondary-button" type="button" :disabled="actionBusy" @click="editTexture">{{ t("编辑资料") }}</button><button class="danger-button" type="button" :disabled="actionBusy" @click="deleteTexture">{{ t("删除材质") }}</button></div>
              </div>
              <button v-if="selectedTexture.owned || selectedTexture.public" class="secondary-button material-wear" type="button" :disabled="actionBusy || wardrobeBusy || !!wardrobeError || wardrobe.some(item => item.id === selectedTexture?.id)" @click="collectOwnTexture">{{ wardrobe.some(item => item.id === selectedTexture?.id) ? t('已在衣柜') : t('收藏到衣柜') }}</button>
              <details :key="selectedTexture.id" class="texture-sources" :open="materialFallback === selectedTexture.id"><summary>{{ t("PNG 材质与下载") }}</summary><div class="texture-preview checkerboard"><img v-if="!failedImages.includes(selectedTexture.fileHash)" :src="textureUrl(selectedTexture.fileHash)" :alt="t('“{name}”的 PNG 材质平面图', { name: selectedTexture.name })" @error="imageFailed(selectedTexture.fileHash)" /><p v-else class="texture-placeholder">{{ t("材质预览加载失败，可尝试下载或刷新列表。") }}</p></div><a class="download-link material-download" :href="textureUrl(selectedTexture.fileHash)" :download="`${selectedTexture.name}.png`">{{ t("下载 PNG") }}</a></details>
              <button class="secondary-button material-wear" type="button" @click="page = 'characters'">{{ t("前往角色管理穿戴") }}</button>
            </aside>
          </div>
          <div v-show="materialTab === 'upload'" class="upload-workspace">
            <section class="material-panel panel" aria-labelledby="upload-title">
              <form class="upload-form" @submit.prevent="uploadTexture"><fieldset :disabled="actionBusy">
                <div class="management-heading"><div><h2 id="upload-title">{{ t("上传材质") }}</h2><p>{{ t("上传后可在皮肤或披风列表中查看。") }}</p></div></div>
                <div class="upload-controls"><label class="binding-field"><span>{{ t("类型") }}</span><select v-model="uploadKind" :aria-label="t('类型')" @change="uploadModel = 'default'"><option value="skin">{{ t("皮肤") }}</option><option value="cape">{{ t("披风") }}</option></select></label><label v-if="uploadKind === 'skin'" class="binding-field"><span>{{ t("手臂模型") }}</span><select v-model="uploadModel" :aria-label="t('手臂模型')"><option value="default">{{ t("标准手臂") }}</option><option value="slim">{{ t("纤细手臂") }}</option></select></label><label class="binding-field"><span>{{ t("名称") }}</span><input v-model="uploadName" maxlength="255" :placeholder="t('给材质起个名字')" /></label><label class="binding-field"><span>{{ t("PNG 文件") }}</span><input ref="uploadInput" type="file" accept="image/png,.png" @change="uploadFile = ($event.target as HTMLInputElement).files?.[0] || null" /></label></div>
                <label class="binding-field description-field"><span>{{ t("描述") }} <small>{{ descriptionLength }}/200</small></span><textarea v-model="uploadDescription" rows="3" :placeholder="t('简单介绍这份材质（可选）')"></textarea></label>
                <label class="visibility-choice"><input v-model="uploadPublic" type="checkbox" /><span><strong>{{ t("公开这份材质") }}</strong><small>{{ t("默认未公开；知道图片 URL 的游戏客户端仍可下载。") }}</small></span></label>
                <p v-if="site.points.enabled" class="point-fee">{{ t('每次上传需 {cost} 积分，包括相同图片的新记录；失败不扣费。', { cost: site.points.uploadCost }) }}</p><button class="primary-button" type="submit" :disabled="actionBusy">{{ actionBusy ? t('正在上传…') : t('上传到我的材质') }}</button>
              </fieldset></form>
            </section>
            <aside class="upload-guide panel" aria-labelledby="upload-guide-title"><h2 id="upload-guide-title">{{ t("上传要求") }}</h2><dl><div><dt>{{ t("文件") }}</dt><dd>{{ t("PNG 格式，最大 8 MiB。") }}</dd></div><div><dt>{{ t("尺寸") }}</dt><dd>{{ t("宽度为 64–1024 px，且为 64 的倍数。") }}</dd></div><div><dt>{{ t("皮肤") }}</dt><dd>{{ t("高度为宽度的 1 倍或 1/2；按皮肤选择标准或纤细手臂。") }}</dd></div><div><dt>{{ t("披风") }}</dt><dd>{{ t("高度为宽度的 1/2。") }}</dd></div></dl><p>{{ t("上传完成后，前往“角色管理”选择材质并应用。") }}</p></aside>
          </div>
        </section>
        <template v-if="page === 'account'">
          <div class="page-heading"><div><h1>{{ t("账号") }}<span class="heading-pixel" aria-hidden="true"></span></h1><p>{{ t("管理个人资料、安全设置和账号相关服务。") }}</p></div></div>
          <nav class="account-tabs admin-navigation" :aria-label="t('账号栏目')"><div class="admin-core-navigation"><span class="admin-group-label">{{ t('账号工作区') }}</span><div><button v-for="item in (['dashboard', 'profile', 'points', 'integration', 'reports'] as const)" :key="item" type="button" class="secondary-button" :class="{ active: accountTab === item }" :aria-current="accountTab === item ? 'page' : undefined" @click="accountTab = item">{{ t({ dashboard: '仪表盘', profile: '个人资料与安全', points: '积分', integration: '游戏接入', reports: '我的举报' }[item]) }}</button></div></div></nav>
          <section v-if="accountTab === 'dashboard'" class="account-dashboard" :aria-label="t('账号仪表盘')">
            <div class="account-dashboard-grid"><article class="panel account-summary-card account-summary-primary"><div class="account-profile"><img class="user-avatar large" :src="'/api/avatars/' + user.id + '.png?v=' + avatarVersion" alt="" width="64" height="64" /><div><p class="eyebrow">{{ t('当前账号') }}</p><h2>{{ user.nickname || t('我的账号') }}</h2><p>{{ user.email }}</p></div></div><span class="account-status" :class="{ verified: user.emailVerified }">{{ user.emailVerified ? t('邮箱已验证') : t('邮箱待验证') }}</span></article><article class="panel account-summary-card"><span class="account-card-index">01</span><h2>{{ t('角色') }}</h2><strong>{{ characters.length }} / {{ characterLimit }}</strong><p>{{ t('已创建角色') }}</p><button class="text-button" type="button" @click="page = 'characters'">{{ t('打开角色管理') }} ↗</button></article><article class="panel account-summary-card"><span class="account-card-index">02</span><h2>{{ t('材质与衣柜') }}</h2><strong>{{ textures.length + wardrobe.length }}</strong><p>{{ t('材质与收藏总数') }}</p><button class="text-button" type="button" @click="page = 'materials'">{{ t('打开我的材质') }} ↗</button></article></div>
            <div class="account-dashboard-columns"><section class="panel account-dashboard-section"><div class="panel-title"><h2>{{ t('快速操作') }}</h2><span>{{ t('常用入口') }}</span></div><div class="account-quick-actions"><button class="secondary-button" type="button" @click="accountTab = 'profile'">{{ t('修改个人资料') }}</button><button class="secondary-button" type="button" @click="page = 'materials'; materialTab = 'upload'">{{ t('上传材质') }}</button><button class="secondary-button" type="button" @click="page = 'wardrobe'">{{ t('打开我的衣柜') }}</button><button class="secondary-button" type="button" @click="accountTab = 'reports'">{{ t('查看我的举报') }}</button></div></section><section class="panel account-dashboard-section"><div class="panel-title"><h2>{{ t('账号状态') }}</h2><span>{{ site.name }}</span></div><dl class="account-fields"><div><dt>{{ t('昵称') }}</dt><dd>{{ user.nickname || t('未设置') }}</dd></div><div><dt>{{ t('登录邮箱') }}</dt><dd>{{ user.email }}</dd></div><div><dt>{{ t('账号类型') }}</dt><dd>{{ user.isAdmin ? t('管理员') : t('普通用户') }}</dd></div></dl></section></div>
          </section>
          <section v-else-if="accountTab === 'profile'" class="account-panel panel" aria-labelledby="account-title"><div class="account-block account-overview"><div class="account-section-heading"><span>01</span><div><h2>{{ t('个人资料') }}</h2><p>{{ t('头像、昵称和站内身份信息。') }}</p></div></div><div class="account-profile"><img class="user-avatar large" :src="'/api/avatars/' + user.id + '.png?v=' + avatarVersion" alt="" width="64" height="64" /><div><h2 id="account-title">{{ user.nickname || t('我的账号') }}</h2><p>{{ t('已登录 {name}', { name: site.name }) }}</p></div></div><dl class="account-fields"><div><dt>{{ t("昵称") }}</dt><dd>{{ user.nickname || t('未设置') }}</dd></div><div><dt>{{ t("邮箱") }}</dt><dd>{{ user.email }}</dd></div><div><dt>{{ t('邮箱状态') }}</dt><dd>{{ user.emailVerified ? t('已验证') : t('未验证') }}</dd></div></dl></div><div class="account-block account-security"><div class="account-section-heading"><span>02</span><div><h2>{{ t('登录与安全') }}</h2><p>{{ t('更新个人资料、邮箱验证和密码。') }}</p></div></div><ProfileSettings :csrf-token="csrfToken" :characters="characters" @updated="profileUpdated" @expired="expireSession" /><section v-if="!user.emailVerified" class="account-password"><h3>{{ t('验证邮箱') }}</h3><p v-if="user.legacyAccount">{{ t('迁移账号可继续使用，邮箱尚未验证。') }}</p><p v-else>{{ t('本机创建的管理员可先配置站点，邮箱尚未验证。') }}</p><button class="secondary-button" type="button" @click="accessMode = 'mail'">{{ t('发送验证邮件') }}</button></section><section class="account-password" aria-labelledby="password-title"><h3 id="password-title">{{ t("修改密码") }}</h3><p>{{ t("修改后会退出所有网页会话并撤销游戏令牌，需要重新登录。") }}</p><form class="password-form" @submit.prevent="changePassword"><fieldset :disabled="passwordBusy"><input type="hidden" name="username" :value="user.email" autocomplete="username" /><label class="binding-field"><span>{{ t("当前密码") }}</span><input v-model="currentPassword" type="password" autocomplete="current-password" required /></label><label class="binding-field"><span>{{ t("新密码") }}</span><input v-model="newPassword" type="password" autocomplete="new-password" required aria-describedby="password-rule" /></label><p id="password-rule" class="password-rule">{{ t("8–128 个字符，须同时包含英文字母、数字和符号；空白不计作符号。") }}</p><label class="binding-field"><span>{{ t("确认新密码") }}</span><input v-model="confirmPassword" type="password" autocomplete="new-password" required /></label><p v-if="passwordError" class="form-error" role="alert">{{ t(passwordError) }}</p><button class="primary-button" type="submit" :disabled="passwordBusy || actionBusy">{{ passwordBusy ? t('正在修改…') : t('修改密码并重新登录') }}</button></fieldset></form></section><div class="account-logout"><div><h3>{{ t("退出当前账号") }}</h3><p>{{ t("下次使用工作台时需要重新登录。") }}</p></div><button class="secondary-button" type="button" :disabled="logoutBusy || passwordBusy || actionBusy || charactersBusy || texturesBusy || wardrobeBusy" @click="logout">{{ logoutBusy ? t('正在退出…') : t('退出登录') }}</button></div><p v-if="logoutError" class="form-error" role="alert">{{ t(logoutError) }}</p></div></section>
          <PointsPanel v-else-if="accountTab === 'points'" :csrf-token="csrfToken" @expired="expireSession" />
          <IntegrationPanel v-else-if="accountTab === 'integration'" />
          <ReportsView v-else-if="accountTab === 'reports'" :csrf-token="csrfToken" @expired="expireSession" />
        </template>
        <SiteFooter :name="site.name" :tagline="site.tagline || t('每一个像素，都是你的模样。')" :footer="site.footer" />
      </main>
    </div>
    <SiteFooter v-if="!user" class="public-site-footer" :name="site.name" :tagline="site.tagline" :footer="site.footer" />
  </div>
</template>
