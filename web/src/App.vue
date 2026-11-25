<script setup lang="ts">
import { feedbackRef, confirmAction, clearFeedback, toast } from './feedback'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import FeedbackHost from './components/FeedbackHost.vue'
import BrandMark from './components/BrandMark.vue'
import AppIcon from './components/AppIcon.vue'
import AccountMenu from './components/AccountMenu.vue'
import LoadingSkeleton from './components/LoadingSkeleton.vue'
import NotFoundPage from './components/NotFoundPage.vue'
import InterfaceIcon from './components/InterfaceIcon.vue'
import { setInterfaceAppearance, applyInterfaceColors } from './interfaceAppearance'
import HomepageContent from './components/HomepageContent.vue'
import OIDCSignIn from './components/OIDCSignIn.vue'
import OIDCConfirmation from './components/OIDCConfirmation.vue'
import TOTPSettings from './components/TOTPSettings.vue'
import ConnectorAccount from './components/ConnectorAccount.vue'
import OAuthAccount from './components/OAuthAccount.vue'
import OIDCAccount from './components/OIDCAccount.vue'
import MinecraftAccount from './components/MinecraftAccount.vue'
import CharacterViewer from './components/CharacterViewer.vue'
import ModelThumbnail from './components/ModelThumbnail.vue'
import SiteFooter from './components/SiteFooter.vue'
import NoticeCenter from './components/NoticeCenter.vue'
import AppModal from './components/AppModal.vue'
import CopyButton from './components/CopyButton.vue'
import TexturePicker from './components/TexturePicker.vue'
import ApplyTextureDialog from './components/ApplyTextureDialog.vue'
import TextureBrowser from './components/TextureBrowser.vue'
import ResourceFilters from './components/ResourceFilters.vue'
import UserToolsWorkspace from './components/UserToolsWorkspace.vue'
import PluginFrontend from './components/PluginFrontend.vue'
import { normalizeUserPlugins, type UserPluginEntry, type UserPluginFrontend } from './pluginModel'
import { adminRoute, canonicalWorkspaceURL, isPagePath, isProtectedPath, libraryItemId, libraryItemPath, navigationRegion, normalizePath, resourcePath, userToolRoute } from './workspaceRoutes'
import AccountAccess from './components/AccountAccess.vue'
import RegistrationSettings from './components/RegistrationSettings.vue'
import Administration from './components/Administration.vue'
import SettingsWorkspace from './components/SettingsWorkspace.vue'
import ProfileSettings from './components/ProfileSettings.vue'
import ReportsView from './components/ReportsView.vue'
import PointsPanel from './components/PointsPanel.vue'
import IntegrationPanel from './components/IntegrationPanel.vue'
import TurnstileChallenge from './components/TurnstileChallenge.vue'
import type { AccessMode, SiteAccess } from './accountTypes'
import type { LibraryRecord, TextureRecord } from './textureTypes'
import { validNewPassword } from './passwordPolicy'
import { oidcMessages } from './oidcMessages'
import { locale, t } from './i18n'
import { fontPresetNames, isDarkOnlyTheme, isMCBiomeTheme, isMCFamilyTheme, resolveUserThemePack } from './themePacks'
import { applyThemeTextures } from './themeTextures'
import { applyThemeFont } from './themeFonts'
import { formatBytes, quotaPercent } from './formatBytes'
import { confirmLeave, dirtyModalConfirm, hasUnsavedChanges, shouldConfirmLeave, useUnsavedGuard } from './unsavedGuard'

interface User { id: number; email: string; nickname: string; isAdmin: boolean; emailVerified: boolean; legacyAccount: boolean }
interface Session { user: User; csrfToken: string }
interface Character { id: number; uuid: string; name: string; skinHash: string; capeHash: string; model: string }

const user = ref<User | null>(null)
const csrfToken = ref('')
const initializing = ref(true)
const sessionError = feedbackRef('error')
const email = ref('')
const password = ref('')
const totpCode = ref('')
const totpRequired = ref(false)
const oidcTOTP = ref(window.location.pathname === '/login' && new URLSearchParams(window.location.search).get('totp') === '1')
const oidcConfirmation = ref(false)
const loginBusy = ref(false)
const loginError = feedbackRef('error')
const loginMessage = feedbackRef('success')
const oidcError = new URLSearchParams(window.location.search).get('oidcError')
if (oidcError && window.location.pathname === '/login') { loginError.value = oidcMessages[oidcError] || oidcMessages.failed; history.replaceState(null, '', '/login') }

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordBusy = ref(false)
const passwordError = feedbackRef('error')
const logoutBusy = ref(false)
const logoutError = feedbackRef('error')
type Page = 'characters' | 'resources' | 'wardrobe' | 'library' | 'account' | 'administration' | 'tools'
const page = ref<Page>('characters')
const userPlugins = ref<UserPluginEntry[]>([])
const userPluginsLoading = ref(false)
const userPluginsError = ref('')
const activeTextureAction = ref<{ plugin: UserPluginEntry; frontend: UserPluginFrontend; texture: TextureRecord } | null>(null)
const activeTextureActionDirty = ref(false)
type AccountTab = 'dashboard' | 'profile' | 'points' | 'integration' | 'reports'
const accountTab = ref<AccountTab>('dashboard')
const publicHome = ref(true)
const publicLibraryOpen = ref(false)
const materialTab = ref<'browse' | 'upload'>('browse')
const materialKind = ref('')
const routePath = ref(window.location.pathname || '/')
const notFound = ref(false)
const pendingNext = ref<string | null>(null)
const materialSearch = ref('')
const characters = ref<Character[]>([])
const selectedId = ref<number | null>(null)
const charactersBusy = ref(false)
const charactersError = feedbackRef('error')
const characterLimit = ref(1)
const textures = ref<TextureRecord[]>([])
const texturesBusy = ref(false)
const textureError = feedbackRef('error')
const wardrobe = ref<LibraryRecord[]>([])
const wardrobeBusy = ref(false)
const wardrobeError = feedbackRef('error')
const texturePicker = ref<'skin' | 'cape'>()
const applyingTexture = ref<TextureRecord>()
const texturePage = ref(1), textureTotal = ref(0), wardrobeTotal = ref(0)
const selectedTextureId = ref('')
const visibleTextures = computed(() => textures.value.filter(texture => !materialKind.value || texture.kind === materialKind.value))
const selectedTexture = computed(() => visibleTextures.value.find(texture => texture.id === selectedTextureId.value))
const materialFallback = ref('')
const editingTextureId = ref('')
const editTextureName = ref('')
const editTextureDescription = ref('')
const editTexturePublic = ref(false)
const editDescriptionLength = computed(() => Array.from(editTextureDescription.value).length)
const actionMessage = feedbackRef('success')
const actionError = feedbackRef('error')
const actionBusy = ref(false)
const actionPage = ref<Page>('characters')
const newCharacterName = ref('')
const renameCharacterName = ref('')
const uploadKind = ref<'skin' | 'cape'>('skin')
const uploadModel = ref('default')
const uploadName = ref('')
const uploadCategory=ref(''), uploadTags=ref(''), editTextureCategory=ref(''),editTextureTags=ref('')
const editTextureInitial = ref('')
const editTextureDirty = computed(() => !!editingTextureId.value && !!selectedTexture.value && editingTextureId.value === selectedTexture.value.id && JSON.stringify({
  name: editTextureName.value,
  description: editTextureDescription.value,
  category: editTextureCategory.value,
  tags: editTextureTags.value,
  public: editTexturePublic.value,
}) !== editTextureInitial.value)
const uploadDescription = ref('')
const uploadFile = ref<File | null>(null)
const uploadPublic = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)
const uploadDragActive = ref(false)
let uploadDragDepth = 0
const uploadPrecheck = ref<{ ok: boolean; text: string } | null>(null)
let uploadPrecheckVersion = 0
interface AccountQuota { accountUsedBytes: number; accountLimitBytes: number; siteUsedBytes: number; siteLimitBytes: number; maxFileBytes: number }
const accountQuota = ref<AccountQuota | null>(null)
const adminCharacterLimit = ref(1)
const adminNonEmailLogin = ref(false)

const descriptionLength = computed(() => Array.from(uploadDescription.value).length)
let sessionVersion = 0
let textureRequest: AbortController | undefined
const failedImages = ref<string[]>([])
const selected = computed(() => characters.value.find(character => character.id === selectedId.value))
const viewerKey = computed(() => selected.value ? `${selected.value.id}:${selected.value.model}:${selected.value.skinHash}:${selected.value.capeHash}` : '')
const fallbackViewer = ref('')
const site = ref<SiteAccess>({ appearance: { logoImage: '', accent: 'blue', defaultTheme: 'system' }, homepage: { blocks: [] }, noticeFormat: '', noticeHtml: '', name: 'WishingSkin', tagline: '', footer: '', notice: '', registration: 'closed', emailDomains: [], mailEnabled: false, turnstileSiteKey: '', points: { enabled: false, dailyReward: 0, uploadCost: 0, characterCost: 0 } })
const uploadPolicy = computed(() => ({
  maxSkinWidth: site.value.contentPolicy?.maxSkinWidth || 128,
  maxCapeWidth: site.value.contentPolicy?.maxCapeWidth || 1024,
  maxFileBytes: site.value.contentPolicy?.maxFileBytes || 8 * 1048576,
}))
const uploadFileRuleText = computed(() => t('PNG 格式，最大 {size}。', { size: formatBytes(uploadPolicy.value.maxFileBytes) }))
const uploadSkinRuleText = computed(() => uploadPolicy.value.maxSkinWidth <= 64
  ? t('标准尺寸 64×64，兼容 64×32 像素。')
  : t('宽度 64–{max} 像素（64 的 2 的幂倍数），高度等于宽度或为宽度的一半。', { max: uploadPolicy.value.maxSkinWidth }))
const uploadCapeRuleText = computed(() => t('宽度 64–{max} 像素且为 64 的倍数，高度为宽度的 1/2。', { max: uploadPolicy.value.maxCapeWidth }))
const uploadNamePlaceholder = computed(() => uploadFile.value ? uploadFile.value.name.replace(/\.png$/i, '') : t('给材质起个名字'))
async function verifySelectedUploadFile(file: File | null) {
  const version = ++uploadPrecheckVersion
  if (!file) { uploadPrecheck.value = null; return }
  const policy = uploadPolicy.value
  if (file.size > policy.maxFileBytes) {
    uploadPrecheck.value = { ok: false, text: t('PNG 文件不能超过 {size}', { size: formatBytes(policy.maxFileBytes) }) }
    return
  }
  try {
    const header = new Uint8Array(await file.slice(0, 8).arrayBuffer())
    if (version !== uploadPrecheckVersion) return
    const sig = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]
    if (header.length < 8 || !sig.every((byte, idx) => header[idx] === byte)) {
      uploadPrecheck.value = { ok: false, text: t('PNG 图像数据无效') }
      return
    }
    if (typeof createImageBitmap !== 'function') {
      uploadPrecheck.value = { ok: true, text: `PNG，${formatBytes(file.size)}` }
      return
    }
    const bitmap = await createImageBitmap(file)
    const width = bitmap.width
    const height = bitmap.height
    bitmap.close()
    if (version !== uploadPrecheckVersion) return
    const valid = uploadKind.value === 'cape'
      ? (width >= 64 && width <= policy.maxCapeWidth && width % 64 === 0 && height === width / 2)
      : (width >= 64 && width <= policy.maxSkinWidth && (width & (width - 1)) === 0 && (height === width || height === width / 2))
    if (!valid) {
      uploadPrecheck.value = {
        ok: false,
        text: uploadKind.value === 'skin' && policy.maxSkinWidth === 128
          ? t('皮肤尺寸仅支持 64×64、64×32、128×128 或 128×64 像素')
          : (uploadKind.value === 'skin' ? uploadSkinRuleText.value : uploadCapeRuleText.value),
      }
      return
    }
    uploadPrecheck.value = { ok: true, text: `${width}×${height} PNG，${formatBytes(file.size)}` }
  } catch {
    if (version === uploadPrecheckVersion) uploadPrecheck.value = { ok: false, text: t('PNG 图像数据无效') }
  }
}
watch([uploadFile, uploadKind, uploadPolicy], ([file]) => { void verifySelectedUploadFile(file) })
function onUploadDragEnter() { uploadDragDepth++; uploadDragActive.value = true }
function onUploadDragLeave() { uploadDragDepth = Math.max(0, uploadDragDepth - 1); if (!uploadDragDepth) uploadDragActive.value = false }
function onUploadDrop(event: DragEvent) {
  uploadDragDepth = 0
  uploadDragActive.value = false
  if (actionBusy.value) return
  const file = event.dataTransfer?.files?.[0]
  if (!file) return
  uploadFile.value = file
  if (uploadInput.value && typeof DataTransfer !== 'undefined') {
    try {
      const dt = new DataTransfer()
      dt.items.add(file)
      uploadInput.value.files = dt.files
    } catch { /* Keep selected file in component state if DataTransfer assignment is unsupported. */ }
  }
}
const loginImageFailed = ref(false)
watch(() => site.value.loginImageUrl, () => { loginImageFailed.value = false })
watch([() => site.value.name, locale, notFound], () => { document.title = `${site.value.name} / ${t(notFound.value ? '页面不存在' : '角色与材质')}` }, { immediate: true, flush: 'sync' })
const siteError = ref(false)
const accessMode = ref<AccessMode | null>(null)
const linkToken = ref('')
const captchaRequired = ref(false)
const loginChallengeToken = ref('')
const loginChallenge = ref<InstanceType<typeof TurnstileChallenge> | null>(null)
const emailVerificationRequired = ref(false)
const verificationBusy = ref(false)
const verificationMessage = feedbackRef('success')
const verificationError = feedbackRef('error')

function safeNext(value: string | null) {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/api/')) return null
  return value
}
function accountPath(tab: AccountTab) { return tab === 'dashboard' ? '/user/account' : `/user/account/${tab}` }
function materialPath(tab: 'browse' | 'upload') { return resourcePath(tab, materialKind.value) }
function openMaterialTab(tab: 'browse' | 'upload') {
  if (tab === 'upload') uploadKind.value = materialKind.value === 'cape' ? 'cape' : 'skin'
  navigate(materialPath(tab))
}
function openUpload() { navigate(user.value ? '/user/resources/upload' : '/login?next=/user/resources/upload') }
const adminView = computed(() => adminRoute(routePath.value))
const userToolView = computed(() => userToolRoute(routePath.value))
const hasUserTools = computed(() => userPlugins.value.some(p => p.frontends.some(f => f.slot === 'user.tools')))
const selectedTextureActions = computed(() => {
  const tex = selectedTexture.value
  if (!tex || (tex.reviewState && tex.reviewState !== 'approved')) return []
  return allTextureActions.value.filter(entry => !entry.frontend.kinds?.length || entry.frontend.kinds.includes(tex.kind))
})
const allTextureActions = computed(() =>
  userPlugins.value.flatMap(plugin =>
    plugin.frontends
      .filter(frontend => frontend.slot === 'texture.action')
      .map(frontend => ({ plugin, frontend })),
  ),
)
const siteSettingsDirty = computed(() => !!user.value?.isAdmin && adminView.value?.tab === 'settings' && adminView.value.settingsTab === 'site' && (
  adminCharacterLimit.value !== (site.value.characterLimit ?? 1) ||
  adminNonEmailLogin.value !== !!site.value.yggNonEmailLogin
))
useUnsavedGuard(siteSettingsDirty, '/admin/settings/site')
watch(adminView, view => {
  if (view?.tab === 'settings' && view.settingsTab === 'site') {
    if (typeof site.value.characterLimit === 'number' && site.value.characterLimit >= 1) {
      adminCharacterLimit.value = site.value.characterLimit
    }
    adminNonEmailLogin.value = !!site.value.yggNonEmailLogin
  }
}, { immediate: true })
const libraryDetailId = computed(() => libraryItemId(routePath.value))
let currentURL = canonicalWorkspaceURL(window.location.pathname, window.location.search)
function setRouteView(path: string, search = '') {
  const canonical = new URL(canonicalWorkspaceURL(path, search), window.location.origin)
  const normalized = canonical.pathname
  if (normalized + canonical.search !== path + search) window.history.replaceState({}, '', normalized + canonical.search + window.location.hash)
  currentURL = normalized + canonical.search
  routePath.value = normalized
  notFound.value = !isPagePath(normalized)
  if (notFound.value) { publicHome.value = false; publicLibraryOpen.value = false; accessMode.value = null; return }
  if ((normalized === '/admin' || normalized.startsWith('/admin/')) && user.value && !user.value.isAdmin) {
    const fallback = '/user'
    if (window.location.pathname !== fallback || window.location.search) window.history.replaceState({}, '', fallback)
    setRouteView(fallback)
    return
  }
  const params = canonical.searchParams
  const isLibrary = normalized === '/library' || !!libraryItemId(normalized)
  publicHome.value = normalized === '/'
  publicLibraryOpen.value = isLibrary && !user.value
  if (isLibrary && user.value) page.value = 'library'
  else if (normalized === '/user/resources' || normalized === '/user/resources/upload') page.value = 'resources'
  else if (normalized === '/user/wardrobe') page.value = 'wardrobe'
  else if (normalized === '/user/account' || normalized.startsWith('/user/account/')) {
    page.value = 'account'
    const tab = normalized.split('/').pop() as AccountTab
    accountTab.value = ['profile', 'points', 'integration', 'reports'].includes(tab) ? tab : 'dashboard'
  } else if (normalized === '/user/tools' || !!userToolRoute(normalized)) page.value = 'tools'
  else if (normalized === '/admin' || normalized.startsWith('/admin/')) page.value = 'administration'
  else if (normalized === '/user') page.value = 'characters'
  if (normalized === '/user/resources' || normalized === '/user/resources/upload') {
    materialTab.value = normalized.endsWith('/upload') ? 'upload' : 'browse'
    if (materialTab.value === 'browse') {
      const kind = ['skin', 'cape'].includes(params.get('kind') || '') ? params.get('kind')! : ''
      if (kind !== materialKind.value) { materialKind.value = kind; if (user.value) void loadTextures() }
    }
  }
  if (normalized === '/login') {
    if (params.has('next')) pendingNext.value = safeNext(params.get('next'))
    oidcTOTP.value = params.get('totp') === '1'
    oidcConfirmation.value = params.get('oidcConfirm') === '1'
    if (!oidcTOTP.value) {
      totpCode.value = ''
      totpRequired.value = false
      captchaRequired.value = false
      loginChallengeToken.value = ''
    }
    const mode = params.get('mode')
    accessMode.value = mode === 'register' || mode === 'mail' ? mode : null
  } else if (!linkToken.value) accessMode.value = null
}
let navigationVersion = 0
let navigationAnimations: Animation[] = []
async function navigate(path: string, replace = false) {
  const target = new URL(path, window.location.origin)
  const destination = new URL(canonicalWorkspaceURL(target.pathname, target.search), window.location.origin)
  const destFull = destination.pathname + destination.search
  if (!replace && destFull !== currentURL && !await confirmLeave(destFull)) return
  const version = ++navigationVersion
  navigationAnimations.forEach(animation => animation.cancel()); navigationAnimations = []
  const selector = navigationRegion(routePath.value, destination.pathname)
  const region = selector ? document.querySelector<HTMLElement>(selector) : null
  const animate = !!region && !window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const oldAnimation = animate ? region.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 200, easing: 'ease-in' }) : undefined
  if (oldAnimation) navigationAnimations.push(oldAnimation)
  const change = async () => {
    oldAnimation?.cancel()
    if (version !== navigationVersion) return
    updateRoute(path, replace)
    readMailLink()
    await nextTick()
    if (animate) {
      const entering = document.querySelector<HTMLElement>(selector)?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 250, easing: 'ease-out' })
      if (entering) { navigationAnimations.push(entering); void entering.finished.then(() => entering.cancel(), () => {}) }
    }
  }
  if (oldAnimation) void oldAnimation.finished.then(change, change)
  else void change()
}
function updateRoute(path: string, replace = false) {
  const url = new URL(path, window.location.origin)
  const canonical = new URL(canonicalWorkspaceURL(url.pathname, url.search), window.location.origin)
  const normalized = canonical.pathname
  const next = normalized + canonical.search
  if (window.location.pathname + window.location.search !== next) {
    window.history[replace ? 'replaceState' : 'pushState']({}, '', next)
  }
  setRouteView(normalized, canonical.search)
}
function finishOIDCConfirmation(path: string) { window.location.assign(path) }
function homepageLink(event: MouseEvent, href: string) {
  if (href.startsWith('/') && !href.startsWith('//')) {
    event.preventDefault()
    navigate(href)
  }
}
// Remove mail secrets before any third-party widget can load. Reading a link never submits it.
function readMailLink() {
  const fragment = new URLSearchParams(window.location.hash.slice(1))
  const mode = fragment.has('verify') ? 'verify' : fragment.has('reset') ? 'reset' : null
  if (!mode) return
  linkToken.value = fragment.get(mode) || ''
  history.replaceState(null, '', window.location.pathname + window.location.search)
  accessMode.value = mode
}
const initialPath = normalizePath(window.location.pathname)
pendingNext.value = safeNext(new URLSearchParams(window.location.search).get('next'))
if (!pendingNext.value && isPagePath(initialPath) && isProtectedPath(initialPath)) pendingNext.value = initialPath + window.location.search
setRouteView(initialPath, window.location.search)
readMailLink()
async function loadSite() {
  try {
    const response = await api('/api/site')
    if (!response.ok) throw new Error('site')
    site.value = await response.json() as SiteAccess
    if (typeof site.value.characterLimit === 'number' && site.value.characterLimit >= 1) {
      adminCharacterLimit.value = site.value.characterLimit
    }
    adminNonEmailLogin.value = !!site.value.yggNonEmailLogin
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
let themeTransition: ViewTransition | undefined
const userThemePack = ref('')
const userFontPreset = ref('')
const userParticlesPref = ref<'' | 'on' | 'off'>('')
const effectiveThemePack = computed(() => (site.value.appearance.allowUserTheme && userThemePack.value) || site.value.appearance.themePack || 'default')
const effectiveDarkOnly = computed(() => isDarkOnlyTheme(effectiveThemePack.value))
const effectiveMCBiome = computed(() => isMCBiomeTheme(effectiveThemePack.value))
const effectiveParticlesEnabled = computed(() => {
  if (!effectiveMCBiome.value) return false
  if (userParticlesPref.value === 'on') return true
  if (userParticlesPref.value === 'off') return false
  return !site.value.appearance.disableParticles
})
const showMcDisclaimer = computed(() => isMCFamilyTheme(effectiveThemePack.value) || Boolean(site.value.themeAssets?.count))
const effectiveFontPreset = computed(() => (site.value.appearance.allowUserTheme && userFontPreset.value && fontPresetNames[userFontPreset.value] && userFontPreset.value !== 'custom') ? userFontPreset.value : (site.value.appearance.fontPreset || 'system'))

function siteTheme() {
  setInterfaceAppearance(site.value.appearance.advanced)
  document.documentElement.dataset.accent = site.value.appearance.accent
  const surface = site.value.appearance.surfaceEffect || 'solid'
  document.documentElement.dataset.surface = surface
  const opacity = site.value.appearance.surfaceOpacity
  const blur = site.value.appearance.surfaceBlur
  if ((surface === 'translucent' || surface === 'glass') && opacity && opacity >= 35 && opacity <= 98) {
    document.documentElement.style.setProperty('--surface-opacity', `${opacity}%`)
  } else {
    document.documentElement.style.removeProperty('--surface-opacity')
  }
  if (surface === 'glass' && blur && blur >= 4 && blur <= 36) {
    document.documentElement.style.setProperty('--surface-blur', `${blur}px`)
  } else {
    document.documentElement.style.removeProperty('--surface-blur')
  }
  const pack = effectiveThemePack.value
  if (pack === 'default') {
    delete document.documentElement.dataset.themePack
  } else {
    document.documentElement.dataset.themePack = pack
  }
  applyThemeFont({
    preset: effectiveFontPreset.value,
    scope: site.value.appearance.fontScope || 'headings',
    customFontUrl: site.value.appearance.customFontUrl,
    customFontFamily: site.value.appearance.customFontFamily,
    uploadedFontUrl: site.value.themeFont?.url,
  })
  const favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (favicon) {
    favicon.href = site.value.appearance.logoImage || '/favicon.svg'
    favicon.type = site.value.appearance.logoImage ? 'image/png' : 'image/svg+xml'
  }
  const preferDark = effectiveDarkOnly.value
    ? true
    : (themePreference ? themePreference === 'dark' : site.value.appearance.defaultTheme === 'system' ? window.matchMedia('(prefers-color-scheme: dark)').matches : site.value.appearance.defaultTheme === 'dark')
  applyTheme(preferDark)
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

watch([materialTab], () => { texturePage.value = 1; selectedTextureId.value = ''; if (user.value && materialTab.value !== 'upload') void loadTextures() })
watch([page, materialTab, user], () => { if (user.value && page.value === 'resources' && materialTab.value === 'upload') void loadQuota() }, { immediate: true })
watch(visibleTextures, items => {
  if (!items.some(texture => texture.id === selectedTextureId.value)) selectedTextureId.value = ''
})

watch(selectedTextureId, () => { editingTextureId.value = '' })

function applyTheme(value: boolean) {
  const resolvedDark = effectiveDarkOnly.value ? true : value
  dark.value = resolvedDark
  applyInterfaceColors(resolvedDark)
  document.documentElement.dataset.theme = resolvedDark ? 'dark' : 'light'
  applyThemeTextures(effectiveThemePack.value, resolvedDark, site.value.themeAssets)
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', getComputedStyle(document.documentElement).getPropertyValue('--bg').trim())
}

function runThemeTransition(apply: () => void) {
  themeTransition?.skipTransition()
  if (document.startViewTransition && !window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    document.documentElement.dataset.themeSwitching = ''
    const transition = document.startViewTransition(apply)
    themeTransition = transition
    void transition.ready.catch(() => { /* A newer toggle may skip this transition. */ })
    const completed = () => {
      if (themeTransition !== transition) return
      themeTransition = undefined
      delete document.documentElement.dataset.themeSwitching
    }
    void transition.finished.then(completed, completed)
  } else apply()
}

function toggleTheme() {
  if (effectiveDarkOnly.value) return
  const nextDark = themePreference ? themePreference !== 'dark' : !dark.value
  runThemeTransition(() => applyTheme(nextDark))
  themePreference = nextDark ? 'dark' : 'light'
  try { localStorage.setItem('wishingskin-theme', themePreference) } catch { /* Theme still works without storage. */ }
}

function setUserThemePack(value: string) {
  const next = resolveUserThemePack(value)
  userThemePack.value = next
  try {
    if (next) localStorage.setItem('wishingskin-theme-pack', next)
    else localStorage.removeItem('wishingskin-theme-pack')
  } catch { /* Ignore storage failures. */ }
  runThemeTransition(() => siteTheme())
}

function setUserFontPreset(value: string) {
  const next = value && fontPresetNames[value] && value !== 'custom' ? value : ''
  userFontPreset.value = next
  try {
    if (next) localStorage.setItem('wishingskin-font-preset', next)
    else localStorage.removeItem('wishingskin-font-preset')
  } catch { /* Ignore storage failures. */ }
  siteTheme()
}

function toggleParticles() {
  const next = effectiveParticlesEnabled.value ? 'off' : 'on'
  userParticlesPref.value = next
  try { localStorage.setItem('wishingskin-particles', next) } catch { /* Ignore storage failures. */ }
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
  const currentPath = normalizePath(window.location.pathname)
  const destination = notFound.value ? currentPath + window.location.search : currentPath === '/login' && accessMode.value === 'mail'
    ? '/login?mode=mail'
    : pendingNext.value || (currentPath === '/' || currentPath === '/library' || !!libraryItemId(currentPath) ? currentPath : '/user')
  if (destination.startsWith('/user/oauth/authorize?')) { window.location.assign(destination); return }
  navigate(destination, true)
  pendingNext.value = null
}

function clearSession() {
  textureRequest?.abort()
  clearFeedback(); texturePicker.value = undefined; applyingTexture.value = undefined
  textureTotal.value = 0; wardrobeTotal.value = 0
  sessionVersion++
  verificationBusy.value = false
  verificationMessage.value = ''
  verificationError.value = ''
  loginMessage.value = ''
  totpCode.value = ''
  totpRequired.value = false
  oidcTOTP.value = false
  captchaRequired.value = false
  loginChallengeToken.value = ''
  user.value = null
  csrfToken.value = ''
  characters.value = []
  selectedId.value = null
  textures.value = []
  wardrobe.value = []
  userPlugins.value = []
  userPluginsLoading.value = false
  userPluginsError.value = ''
  activeTextureAction.value = null
  activeTextureActionDirty.value = false
  wardrobeBusy.value = false
  wardrobeError.value = ''
  texturesBusy.value = false
  selectedTextureId.value = ''
  materialTab.value = 'browse'
  materialKind.value = ''
  materialSearch.value = ''
  materialFallback.value = ''
  editingTextureId.value = ''
  editTextureInitial.value = ''
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
  uploadPrecheck.value = null
  uploadDragDepth = 0
  uploadDragActive.value = false
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
  adminCharacterLimit.value = site.value.characterLimit ?? 1
  adminNonEmailLogin.value = !!site.value.yggNonEmailLogin
  failedImages.value = []
  fallbackViewer.value = ''
  navigate('/', true)
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

async function loadQuota() {
  if (!user.value) return
  const version = sessionVersion
  try {
    const response = await api('/api/account/quota')
    if (version !== sessionVersion) return
    if (response.status === 401) { expireSession(); return }
    if (response.ok) accountQuota.value = await response.json() as AccountQuota
  } catch {
    // Non-critical quota indicator; keep upload usable even if temporarily unavailable.
  }
}

async function loadTextures() {
  textureRequest?.abort()
  const controller = new AbortController()
  textureRequest = controller
  const version = sessionVersion
  texturesBusy.value = true
  textureError.value = ''
  try {
    const params = new URLSearchParams({ q: materialSearch.value.trim(), kind: materialKind.value, page: String(texturePage.value), pageSize: '20' })
    const response = await api(`/api/textures?${params}`, { signal: controller.signal })
    if (version !== sessionVersion || controller.signal.aborted) return
    if (response.status === 401) { clearSession(); loginError.value = '登录已过期，请重新登录。'; return }
    if (!response.ok) throw new Error('textures')
    const data: { textures: TextureRecord[]; total: number } = await response.json()
    if (version !== sessionVersion || controller.signal.aborted) return
    textures.value = data.textures; textureTotal.value = data.total
    if (texturePage.value > 1 && !data.textures.length) { texturePage.value = Math.max(1, Math.ceil(data.total / 20)); void loadTextures() }
    failedImages.value = []
  } catch {
    if (version === sessionVersion && !controller.signal.aborted) textureError.value = '材质列表暂时无法加载。'
  } finally {
    if (version === sessionVersion && textureRequest === controller) texturesBusy.value = false
  }
}

function searchMaterials() {
  updateRoute(resourcePath('browse', materialKind.value), true)
  texturePage.value = 1
  selectedTextureId.value = ''
  void loadTextures()
}

async function loadWardrobe() {
  const version = sessionVersion
  wardrobeBusy.value = true
  wardrobeError.value = ''
  try {
    const response = await api('/api/wardrobe?page=1&pageSize=1')
    if (version !== sessionVersion) return
    if (response.status === 401) { expireSession(); return }
    if (!response.ok) throw new Error('wardrobe')
    const data: { textures: LibraryRecord[]; total: number } = await response.json()
    if (version === sessionVersion) { wardrobe.value = data.textures; wardrobeTotal.value = data.total }
  } catch {
    if (version === sessionVersion) wardrobeError.value = '衣柜暂时无法加载，请重试。'
  } finally {
    if (version === sessionVersion) wardrobeBusy.value = false
  }
}

function expireSession() { clearSession(); loginError.value = '登录已过期，请重新登录。' }

async function loadUserPlugins() {
  if (!user.value) return
  const version = sessionVersion
  userPluginsLoading.value = true
  userPluginsError.value = ''
  try {
    const response = await api('/api/user/plugins')
    if (version !== sessionVersion) return
    if (response.status === 401) { expireSession(); return }
    if (!response.ok) throw new Error('user-plugins')
    const data = await response.json().catch(() => ({})) as { plugins?: unknown }
    if (version === sessionVersion) userPlugins.value = normalizeUserPlugins(data.plugins)
  } catch {
    if (version === sessionVersion) userPluginsError.value = '用户插件列表暂时无法加载'
  } finally {
    if (version === sessionVersion) userPluginsLoading.value = false
  }
}

function siteUpdated() {
  void loadSite()
  void loadUserPlugins()
}

function onUserToolMutated(kind: 'textures' | 'characters') {
  if (kind === 'textures') {
    void loadTextures()
    void loadQuota()
  } else {
    void loadCharacters()
  }
}

async function wearCollected(_texture: LibraryRecord) { await loadCharacters() }
async function pickTexture(texture: TextureRecord, model?: string) { await bindTexture(texture.kind, texture.id, model); if (!actionError.value) texturePicker.value = undefined }

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
      const payload = await response.json().catch(() => ({})) as { error?: string; code?: string; pointsRulesChanged?: boolean }
      const friendlyError = payload.code === 'texture_account_capacity'
        ? '已达到当前账号的材质存储上限，请先清理不需要的旧材质后再上传。'
        : payload.code === 'texture_site_capacity'
          ? '站点材质存储空间已满，请联系管理员扩容或清理。'
          : (payload.error || '操作未完成，请重试。')
      if (version === sessionVersion) actionError.value = friendlyError
      if (payload.code === 'texture_account_capacity' || payload.code === 'texture_site_capacity') void loadQuota()
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
  const character = selected.value
  const name = renameCharacterName.value.trim()
  if (name === character.name || !await confirmAction({ title: '修改角色名', message: t('将“{name}”改名为“{nextName}”？', { name: character.name, nextName: name }) })) return
  if (selected.value?.id !== character.id) return
  const ok = await mutate(`/api/characters/${character.id}`, 'PATCH', JSON.stringify({ name }))
  if (!ok) return
  actionMessage.value = '角色名已更新。'
  await loadCharacters()
}

async function deleteCharacter() {
  const character = selected.value
  if (!character || !await confirmAction({ title: '删除角色', message: t('删除角色“{name}”？', { name: character.name }), danger: true })) return
  if (selected.value?.id !== character.id) return
  const ok = await mutate(`/api/characters/${character.id}`, 'DELETE', undefined, '')
  if (!ok) return
  selectedId.value = null
  actionMessage.value = '角色已删除。'
  await Promise.all([loadCharacters(), loadTextures()])
}

async function clearTexture(kind: 'skin' | 'cape') {
  const character = selected.value
  if (!character) return
  if (!await confirmAction({ title: '清除材质', message: t(kind === 'skin' ? '清除当前角色的皮肤？' : '清除当前角色的披风？') })) return
  if (selected.value?.id !== character.id) return
  await bindTexture(kind, '')
}

async function bindTexture(kind: 'skin' | 'cape', recordId: string, model?: string) {
  if (!selected.value) return
  const ok = await mutate(`/api/characters/${selected.value.id}/textures/${kind}`, 'PUT', JSON.stringify({ recordId, ...(kind === 'skin' && model ? { model } : {}) }))
  if (!ok) return
  fallbackViewer.value = ''
  actionMessage.value = recordId ? (kind === 'skin' ? '皮肤已应用。' : '披风已应用。') : (kind === 'skin' ? '皮肤已清除。' : '披风已清除。')
  await Promise.all([loadCharacters(), loadTextures()])
}

async function uploadTexture() {
  const version = sessionVersion
  actionPage.value = 'resources'
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
 form.set('category',uploadCategory.value); form.set('tags',uploadTags.value)
  form.set('isPublic', String(uploadPublic.value))
  form.set('expectedCost', String(site.value.points.enabled ? site.value.points.uploadCost : 0))
  const ok = await mutate('/api/textures', 'POST', form, '')
  if (!ok) return
  const uploaded: TextureRecord = await ok.json()
  if (version !== sessionVersion) return
  navigate(resourcePath('browse', uploaded.kind), true)
  actionMessage.value = '材质已上传到我的材质。'
  uploadName.value = ''
  uploadDescription.value = ''
  uploadFile.value = null
  uploadPublic.value = false
  accountQuota.value = null
  if (uploadInput.value) uploadInput.value.value = ''
  await Promise.all([loadTextures(), loadQuota()])
  if (version === sessionVersion) selectedTextureId.value = uploaded.id
}

async function saveCharacterLimit() {
  const response = await mutate('/api/admin/settings', 'PATCH', JSON.stringify({ characterLimit: adminCharacterLimit.value }))
  if (!response) return
  const data = await response.json().catch(() => ({})) as { characterLimit?: number; yggNonEmailLogin?: boolean }
  if (typeof data.characterLimit === 'number' && data.characterLimit >= 1) {
    site.value.characterLimit = data.characterLimit
    adminCharacterLimit.value = data.characterLimit
  }
  actionMessage.value = '站点角色数量上限已保存。'
  await Promise.all([loadSite(), loadCharacters()])
}

async function saveYggdrasilLogin() {
  const response = await mutate('/api/admin/settings', 'PATCH', JSON.stringify({ yggNonEmailLogin: adminNonEmailLogin.value }))
  if (!response) return
  const data = await response.json().catch(() => ({})) as { characterLimit?: number; yggNonEmailLogin?: boolean }
  if (typeof data.yggNonEmailLogin === 'boolean') {
    site.value.yggNonEmailLogin = data.yggNonEmailLogin
    adminNonEmailLogin.value = data.yggNonEmailLogin
  }
  actionMessage.value = '外置登录设置已保存。'
  await loadSite()
}

function editTexture() {
  if (!selectedTexture.value?.owned) return
  editingTextureId.value = selectedTexture.value.id
  editTextureName.value = selectedTexture.value.name
  editTextureCategory.value=selectedTexture.value.category||'';editTextureTags.value=selectedTexture.value.tags?.join(', ')||''
  editTextureDescription.value = selectedTexture.value.description
  editTexturePublic.value = selectedTexture.value.public
  editTextureInitial.value = JSON.stringify({
    name: editTextureName.value,
    description: editTextureDescription.value,
    category: editTextureCategory.value,
    tags: editTextureTags.value,
    public: editTexturePublic.value,
  })
}

async function saveTexture() {
  if (!editingTextureId.value || !editTextureName.value.trim()) return
  actionPage.value = 'resources'
  actionError.value = ''
  actionMessage.value = ''
  if (editDescriptionLength.value > 200) { actionError.value = '材质描述不能超过 200 个字符。'; return }
  const ok = await mutate(`/api/textures/${editingTextureId.value}`, 'PATCH', JSON.stringify({ name: editTextureName.value.trim(), description: editTextureDescription.value, isPublic: editTexturePublic.value, category: editTextureCategory.value, tags: editTextureTags.value.split(',') }))
  if (!ok) return
  editingTextureId.value = ''
  actionMessage.value = '材质资料已保存。'
  await Promise.all([loadTextures(), loadWardrobe()])
}

async function deleteTexture() {
  const texture = selectedTexture.value
  if (!texture?.owned || !await confirmAction({ title: '删除材质', message: t('删除材质“{name}”？', { name: texture.name }), danger: true, confirmLabel: '删除' })) return
  const ok = await mutate(`/api/textures/${texture.id}`, 'DELETE', undefined, '')
  if (!ok) return
  editingTextureId.value = ''
  actionMessage.value = '材质已删除。'
  await Promise.all([loadTextures(), loadWardrobe(), loadQuota()])
}

async function restoreSession() {
  initializing.value = true
  sessionError.value = ''
  try {
    const response = await api('/api/auth/me')
    if (response.ok) {
      acceptSession(await response.json() as Session)
      await Promise.all([loadCharacters(), loadTextures(), loadWardrobe(), loadUserPlugins()])
    } else if (response.status !== 401) {
      throw new Error('session')
    }
  } catch {
    sessionError.value = '暂时无法连接到服务。请检查连接后重试。'
  } finally {
    initializing.value = false
  }
}

async function sendVerificationMail() {
  if (!user.value || verificationBusy.value) return
  const version = sessionVersion
  verificationBusy.value = true; verificationMessage.value = ''; verificationError.value = ''
  try {
    const response = await api('/api/auth/email', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: user.value.email, purpose: 'verify' }) })
    if (version !== sessionVersion) return
    if (!response.ok) { const data = await response.json().catch(() => ({})); if (version === sessionVersion) verificationError.value = data.error || '暂时无法发送邮件'; return }
    verificationMessage.value = '验证邮件已提交，请检查账号邮箱。'
  } catch { if (version === sessionVersion) verificationError.value = '无法确认操作结果，请检查邮箱或稍后重试。' }
  finally { if (version === sessionVersion) verificationBusy.value = false }
}

async function login() {
  if (loginBusy.value) return
  loginBusy.value = true
  loginError.value = ''
  loginMessage.value = ''
  try {
    const response = await api(oidcTOTP.value ? '/api/auth/totp' : '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(oidcTOTP.value ? { code: totpCode.value } : { email: email.value.trim(), password: password.value, turnstileToken: loginChallengeToken.value, totpCode: totpCode.value }),
    })
    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      if (Object.hasOwn(data, 'totpRequired')) totpRequired.value = !!data.totpRequired || oidcTOTP.value
      if (Object.hasOwn(data, 'captchaRequired')) captchaRequired.value = !!data.captchaRequired
      if (!captchaRequired.value) loginChallengeToken.value = ''
      emailVerificationRequired.value = !!data.emailVerificationRequired
      loginError.value = data.emailVerificationRequired && site.value.manualEmailVerification
        ? '请等待管理员完成邮箱验证。'
        : data.error || (response.status === 429 ? '尝试次数过多，请稍后再试。' : '暂时无法登录，请稍后重试。')
      return
    }
    const result = await response.json() as Session & { next?: string }
    acceptSession(result)
    if (result.next && safeNext(result.next)) navigate(result.next)
    totpCode.value = ''; totpRequired.value = false; oidcTOTP.value = false
    captchaRequired.value = false
    emailVerificationRequired.value = false
    password.value = ''
    await Promise.all([loadCharacters(), loadTextures(), loadWardrobe(), loadUserPlugins()])
  } catch {
    loginError.value = '无法连接到服务，请检查连接后重试。'
  } finally {
    loginBusy.value = false
    loginChallengeToken.value = ''
    void loginChallenge.value?.reset()
  }
}

async function logout() {
  if (logoutBusy.value || !await confirmAction({ title: '退出登录', message: t('退出当前账号？') })) return
  logoutBusy.value = true
  logoutError.value = ''
  try {
    const response = await api('/api/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrfToken.value } })
    if (!response.ok && response.status !== 401) throw new Error('logout')
    clearSession(); toast('已退出登录。')
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
  if (!await confirmAction({ title: '修改密码', message: t('修改密码并重新登录？') })) return
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
const handlePopState = async () => {
  const dest = canonicalWorkspaceURL(window.location.pathname, window.location.search)
  if (shouldConfirmLeave(dest)) {
    window.history.pushState({}, '', currentURL)
    if (!await confirmLeave(dest)) return
  }
  void navigate(dest, true)
}
function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (hasUnsavedChanges()) event.preventDefault()
}

let topbarObserver: ResizeObserver | undefined
onMounted(() => {
  const topbar = document.querySelector<HTMLElement>('.topbar')
  if (topbar) { topbarObserver = new ResizeObserver(() => { document.querySelector<HTMLElement>('.app-shell')?.style.setProperty('--topbar-height', topbar.getBoundingClientRect().height + 'px') }); topbarObserver.observe(topbar) }
  window.addEventListener('hashchange', readMailLink)
  window.addEventListener('popstate', handlePopState)
  window.addEventListener('beforeunload', handleBeforeUnload)
  let preference: string | null = null
  try {
    preference = localStorage.getItem('wishingskin-theme')
    userThemePack.value = resolveUserThemePack(localStorage.getItem('wishingskin-theme-pack'))
    const savedFont = localStorage.getItem('wishingskin-font-preset')
    if (savedFont && fontPresetNames[savedFont] && savedFont !== 'custom') userFontPreset.value = savedFont
    const savedParticles = localStorage.getItem('wishingskin-particles')
    if (savedParticles === 'on' || savedParticles === 'off') userParticlesPref.value = savedParticles
  } catch { /* Use the system preference. */ }
  themePreference = preference === 'light' || preference === 'dark' ? preference : ''
  applyTheme(themePreference ? themePreference === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches)
  void restoreSession()
  void loadSite()
})
onBeforeUnmount(() => {
  topbarObserver?.disconnect()
  navigationAnimations.forEach(animation => animation.cancel())
  textureRequest?.abort()
  window.removeEventListener('hashchange', readMailLink)
  window.removeEventListener('popstate', handlePopState)
  window.removeEventListener('beforeunload', handleBeforeUnload)
})
</script>

<template>
  <FeedbackHost />
  <TexturePicker v-if="texturePicker && user && selected" :kind="texturePicker" :busy="actionBusy" @close="texturePicker = undefined" @select="pickTexture" @expired="expireSession" />
  <ApplyTextureDialog v-if="applyingTexture && user" :texture="applyingTexture" :csrf-token="csrfToken" @close="applyingTexture = undefined" @applied="loadCharacters" @expired="expireSession" />
  <AppModal v-if="activeTextureAction && user" :title="t(activeTextureAction.frontend.label)" wide :confirm-close="activeTextureActionDirty ? dirtyModalConfirm : false" @close="activeTextureAction = null; activeTextureActionDirty = false">
    <PluginFrontend
      :key="`${activeTextureAction.plugin.id}:${activeTextureAction.frontend.id}:${activeTextureAction.texture.id}`"
      :plugin-id="activeTextureAction.plugin.id"
      :frontend-id="activeTextureAction.frontend.id"
      scope="user"
      tall
      enabled
      :version="activeTextureAction.plugin.version"
      :csrf-token="csrfToken"
      :initial-context="{ texture: activeTextureAction.texture }"
      @expired="activeTextureAction = null; activeTextureActionDirty = false; expireSession()"
      @mutated="onUserToolMutated"
      @dirty-change="activeTextureActionDirty = $event"
    />
  </AppModal>
  <div class="app-shell">
    <header class="topbar">
      <a class="brand" href="/" :aria-label="t('{name} 首页', { name: site.name })" :title="site.name"><img v-if="site.appearance.logoImage" class="brand-logo" :src="site.appearance.logoImage" alt="" width="36" height="36" /><BrandMark v-else :size="36" /><span class="brand-name"><template v-if="site.name === 'WishingSkin'">Wishing<span class="brand-light">Skin</span></template><template v-else>{{ site.name }}</template></span></a>
      <div class="topbar-actions">
        <button class="topbar-link" :class="{ active: routePath === '/library' || !!libraryDetailId }" type="button" :aria-current="(routePath === '/library' || !!libraryDetailId) ? 'page' : undefined" @click="navigate('/library')"><InterfaceIcon name="library" />{{ t('公开库') }}</button>
        <NoticeCenter :notice="site.notice" :format="site.noticeFormat" :signed-in="!!user" :user-id="user?.id" :csrf-token="csrfToken" @navigate="navigate" @expired="expireSession" />
        <AccountMenu :user="user" :avatar-version="avatarVersion" :dark="dark" :dark-only="effectiveDarkOnly" :mc-biome="effectiveMCBiome" :particles-enabled="effectiveParticlesEnabled" :allow-user-theme="!!site.appearance.allowUserTheme" :user-theme-pack="userThemePack" :user-font-preset="userFontPreset" :logout-busy="logoutBusy" @navigate="navigate" @theme="toggleTheme" @toggle-particles="toggleParticles" @update:theme-pack="setUserThemePack" @update:font-preset="setUserFontPreset" @logout="logout" />
      </div>
    </header>

    <main v-if="initializing" class="initial-state"><LoadingSkeleton layout="rows" label="正在打开你的工作台…" /></main>

    <main v-else-if="notFound" class="not-found-layout"><NotFoundPage @navigate="navigate" /></main>

    <main v-else-if="accessMode" class="access-layout"><AccountAccess :mode="accessMode" :token="linkToken" :site="site" :email="user?.email || email" :dark="dark" @back="navigate('/login'); linkToken = ''" @completed="accessCompleted" /></main>

    <main v-else-if="!user && publicLibraryOpen" class="public-library-layout"><button class="secondary-button public-library-back" type="button" @click="navigate('/')">{{ t("返回主页") }}</button><TextureBrowser mode="library" csrf-token="" :detail-id="libraryDetailId" :wardrobe="[]" :wardrobe-busy="false" wardrobe-error="" @open-detail="id => navigate(libraryItemPath(id))" @login="navigate(libraryDetailId ? `/login?next=${encodeURIComponent(libraryItemPath(libraryDetailId))}` : '/login')" @upload="openUpload" /></main>
    <main v-else-if="publicHome" class="public-home-page"><HomepageContent :homepage="site.homepage" :name="site.name" :tagline="site.tagline" :notice="site.notice" :notice-html="site.noticeHtml" :theme-pack="effectiveThemePack" :dark="dark" :mc-biome="effectiveMCBiome" :particles-enabled="effectiveParticlesEnabled" :theme-assets="site.themeAssets" @navigate="homepageLink" /></main>
    <main v-else-if="!user" class="login-layout">
      <section class="welcome-panel" aria-labelledby="welcome-title">
        <div class="welcome-copy"><h1 id="welcome-title">{{ t("每个角色，") + ' ' }}<br>{{ t("都有专属外观。") }}</h1><p>{{ t("统一管理你的 Minecraft 角色、皮肤与披风，") }}<br class="desktop-break">{{ t("随时预览并通过外置登录接入游戏。") }}</p></div>
        <img v-if="site.loginImageUrl && !loginImageFailed" class="login-custom-image" :src="site.loginImageUrl" alt="" @error="loginImageFailed = true" />
      </section>
      <section class="login-panel" aria-labelledby="login-title">
        <div class="login-heading"><span class="section-symbol" aria-hidden="true"><InterfaceIcon name="login" /></span><h2 id="login-title">{{ t("登录工作台") }}</h2><p>{{ t("使用已有账号登录。") }}</p><button class="text-button" type="button" @click="navigate('/')">{{ t('返回主页') }}</button></div>
        <div v-if="sessionError" class="notice error" role="alert"><p>{{ t(sessionError) }}</p><button class="text-button" type="button" @click="restoreSession">{{ t("重新连接") }}</button></div>
        <form @submit.prevent="login">
          <template v-if="!oidcTOTP">
          <label for="email">{{ t("邮箱") }}</label><input id="email" v-model="email" name="email" type="email" autocomplete="username" placeholder="you@example.com" required :disabled="loginBusy" />
          <label for="password">{{ t("密码") }}</label><input id="password" v-model="password" name="password" type="password" autocomplete="current-password" :placeholder="t('输入你的密码')" required :disabled="loginBusy" />
          </template>
          <label v-if="totpRequired || oidcTOTP" for="totp-code">{{ t("验证码或恢复码") }}</label><input v-if="totpRequired || oidcTOTP" id="totp-code" v-model="totpCode" autocomplete="one-time-code" maxlength="32" required :disabled="loginBusy" />
          <TurnstileChallenge v-if="captchaRequired" ref="loginChallenge" :site-key="site.turnstileSiteKey" action="login" :dark="dark" @token="loginChallengeToken = $event" />


          <button v-if="emailVerificationRequired && !site.manualEmailVerification" class="text-button" type="button" @click="navigate('/login?mode=mail')">{{ t('重新发送验证邮件') }}</button>
          <button class="primary-button login-button" type="submit" :disabled="loginBusy || (captchaRequired && !loginChallengeToken)">{{ loginBusy ? t('正在登录…') : t('登录') }}<span aria-hidden="true"><InterfaceIcon name="arrow" /></span></button>
        </form>
        <template v-if="!oidcTOTP"><OIDCSignIn v-for="provider in (site.oidcProviders || (site.oidc?.enabled ? [{ id: '', name: site.oidc.name }] : []))" :key="provider.id" :name="provider.name" :provider="provider.id" :next-path="pendingNext || '/user'" /></template>
        <button class="secondary-button browse-library" type="button" @click="navigate('/library')">{{ t("浏览公开库") }}</button>
        <div class="access-links"><button v-if="site.registration !== 'closed'" class="text-button" type="button" @click="navigate('/login?mode=register')">{{ t('创建账号') }}</button><button class="text-button" type="button" @click="navigate('/login?mode=mail')">{{ t('验证邮箱 / 找回密码') }}</button></div>
        <div v-if="siteError" class="notice" role="alert">{{ t('暂时无法读取站点设置') }} <button class="text-button" type="button" @click="loadSite">{{ t('重试') }}</button></div>
        <div v-else-if="site.registration === 'closed'" class="registration-note"><span class="status-dot" aria-hidden="true"></span><p>{{ t('当前未开放自主注册') }}<br><span>{{ t("如需账号请联系站点管理员。") }}</span></p></div>
      </section>
      <OIDCConfirmation v-if="oidcConfirmation" :csrf-token="csrfToken" login-layout @completed="finishOIDCConfirmation" />
    </main>

    <div v-else class="workspace-layout">
      <aside class="sidebar">
        <div class="sidebar-title">{{ t("我的工作台") }}</div>
        <nav :aria-label="t('工作台导航')" :class="{ 'has-admin': user.isAdmin }">
          <button type="button" class="nav-button" :class="{ active: page === 'characters' }" :aria-current="page === 'characters' ? 'page' : undefined" @click="navigate('/user')"><InterfaceIcon name="characters" />{{ t("角色管理") }}<span class="nav-count">{{ characters.length }}</span></button>
          <button type="button" class="nav-button" :class="{ active: page === 'resources' }" :aria-current="page === 'resources' ? 'page' : undefined" @click="navigate('/user/resources')"><InterfaceIcon name="materials" />{{ t("我的材质") }}</button>
          <button type="button" class="nav-button" :class="{ active: page === 'wardrobe' }" :aria-current="page === 'wardrobe' ? 'page' : undefined" @click="navigate('/user/wardrobe')"><InterfaceIcon name="wardrobe" />{{ t("我的衣柜") }}<span class="nav-count">{{ wardrobeTotal }}</span></button>
          <button v-if="hasUserTools || page === 'tools'" type="button" class="nav-button" :class="{ active: page === 'tools' }" :aria-current="page === 'tools' ? 'page' : undefined" @click="navigate('/user/tools')"><InterfaceIcon name="tools" />{{ t("工具箱") }}</button>
          <button type="button" class="nav-button" :class="{ active: page === 'account' }" :aria-current="page === 'account' ? 'page' : undefined" @click="navigate('/user/account')"><InterfaceIcon name="account" />{{ t("账号") }}</button>
          <button v-if="user.isAdmin" type="button" class="nav-button" :class="{ active: page === 'administration' }" :aria-current="page === 'administration' ? 'page' : undefined" @click="navigate('/admin')"><InterfaceIcon name="administration" />{{ t('管理后台') }}</button>
        </nav>
      </aside>

      <main class="workspace-content">
        <UserToolsWorkspace v-if="page === 'tools'" :csrf-token="csrfToken" :plugins="userPlugins" :loading="userPluginsLoading" :error="userPluginsError" :selected-plugin-id="userToolView?.pluginId || ''" :selected-frontend-id="userToolView?.frontendId || ''" @navigate="navigate" @refresh="loadUserPlugins" @expired="expireSession" @mutated="onUserToolMutated" />
        <Administration v-if="page === 'administration' && user.isAdmin && adminView" :csrf-token="csrfToken" :user-id="String(user.id)" :active-tab="adminView.tab" :plugin-id="adminView.pluginId" @navigate="navigate" @expired="expireSession" @updated="siteUpdated" @user-updated="loadCharacters"><template #settings>
          <SettingsWorkspace :active-tab="adminView.settingsTab" @navigate="navigate" :site="site" :csrf-token="csrfToken" @updated="loadSite" @expired="expireSession">
          <section class="character-setting" :aria-label="t('角色数量设置')"><form class="inline-form" @submit.prevent="saveCharacterLimit"><label for="character-limit">{{ t("站点每账号角色上限") }}</label><div class="inline-controls"><input id="character-limit" v-model.number="adminCharacterLimit" type="number" min="1" step="1" required :disabled="actionBusy" /><button class="secondary-button" type="submit" :disabled="actionBusy">{{ t("保存设置") }}</button></div><small>{{ t("降低上限不影响已有角色，超出上限的账号暂不可继续新增。") }}</small></form></section>
          <section class="site-setting site-setting--divided" :aria-label="t('外置登录方式')">
            <form class="toggle-setting" @submit.prevent="saveYggdrasilLogin">
              <div class="toggle-setting__text">
                <label class="toggle-setting__title" for="ygg-name-login">{{ t('允许使用角色名登录') }}</label>
                <p id="ygg-name-login-desc" class="toggle-setting__desc">{{ t('开启后，玩家可在 authlib-injector 启动器中用角色名和密码登录，并自动选中该角色。') }}</p>
              </div>
              <input id="ygg-name-login" v-model="adminNonEmailLogin" class="switch" type="checkbox" role="switch" aria-describedby="ygg-name-login-desc" :disabled="actionBusy" />
              <button class="secondary-button toggle-setting__save" type="submit" :disabled="actionBusy || adminNonEmailLogin === !!site.yggNonEmailLogin">{{ t('保存设置') }}</button>
            </form>
          </section>

          <RegistrationSettings :site="site" :csrf-token="csrfToken" @updated="loadSite" @expired="expireSession" />
          </SettingsWorkspace></template></Administration>
        <TextureBrowser v-if="page === 'wardrobe' || page === 'library'" :key="`${user.id}:${page}`" :mode="page" :csrf-token="csrfToken" :detail-id="page === 'library' ? libraryDetailId : ''" :wardrobe="wardrobe" :wardrobe-busy="wardrobeBusy" :wardrobe-error="wardrobeError" :texture-actions="allTextureActions" @open-detail="id => { if (page === 'library') navigate(libraryItemPath(id)) }" @refresh="loadWardrobe" @changed="loadWardrobe" @wear="wearCollected" @upload="openUpload" @plugin-action="activeTextureActionDirty = false; activeTextureAction = $event" @expired="expireSession" />
        <template v-if="page === 'characters'">
          <div class="page-heading"><div><h1>{{ t("角色管理") }}</h1></div><button class="secondary-button" type="button" :disabled="charactersBusy || actionBusy" @click="loadCharacters">{{ charactersBusy ? t('正在刷新…') : t('刷新列表') }}</button></div>

          <LoadingSkeleton v-if="charactersBusy" class="panel" layout="characters" label="正在加载角色…" />
          <div v-else-if="charactersError" class="empty-state panel" role="alert"><span class="empty-symbol"><AppIcon name="error" /></span><h2>{{ t("加载失败") }}</h2><button class="secondary-button" type="button" @click="loadCharacters">{{ t("重新加载") }}</button></div>
          <div v-else class="character-workspace">
            <section class="character-list panel" aria-labelledby="character-list-title"><div class="panel-title"><h2 id="character-list-title">{{ t("角色列表") }}</h2><span>{{ t('{count} / {limit} 个', { count: characters.length, limit: characterLimit }) }}</span></div><div v-if="characters.length" class="character-options"><button v-for="character in characters" :key="character.id" type="button" class="character-option" :class="{ selected: character.id === selectedId }" :aria-pressed="character.id === selectedId" @click="selectedId = character.id; renameCharacterName = character.name"><ModelThumbnail class="character-model-thumbnail" :name="character.name" :model="character.model" :skin-hash="character.skinHash" :cape-hash="character.capeHash" /><span class="character-name"><strong>{{ character.name }}</strong><small>{{ modelLabel(character.model) }}</small></span><span class="selection-indicator" aria-hidden="true"></span></button></div><div v-else class="list-empty"><AppIcon name="characters" /><p>{{ t("暂无角色，先创建一个吧。") }}</p></div>
              <p v-if="site.points.enabled && characters.length < characterLimit" class="point-fee">{{ t('新建一个角色需 {cost} 积分；失败不扣费。', { cost: site.points.characterCost }) }}</p><form v-if="characters.length < characterLimit" class="inline-form create-character-form" @submit.prevent="createCharacter"><label for="new-character-name">{{ t("新建角色") }}</label><div class="inline-controls"><input id="new-character-name" v-model="newCharacterName" name="name" autocomplete="off" minlength="3" maxlength="16" pattern="[A-Za-z0-9_]{3,16}" required :placeholder="t('3–16 位英文、数字或下划线')" :disabled="actionBusy" /><button class="primary-button" type="submit" :disabled="actionBusy || !newCharacterName.trim()">{{ t("创建") }}</button></div></form>
              <p v-else class="limit-note">{{ characters.length > characterLimit ? t('现有角色已超出数量上限，原角色会保留，暂不可新增。') : t('已达到角色上限。') }}</p>
            </section>
            <div class="character-tools">
            <section v-if="selected" class="character-detail panel" aria-labelledby="character-title">
              <div class="detail-heading"><span class="detail-icon"><AppIcon name="characters" /></span><div><h2 id="character-title">{{ selected.name }}</h2><span>{{ modelLabel(selected.model) }}</span></div></div>
              <dl class="character-meta"><div><dt>{{ t("角色 UUID") }}</dt><dd class="character-uuid-row"><code>{{ selected.uuid }}</code><CopyButton :text="selected.uuid" :label="t('复制角色 UUID')" /></dd></div></dl>
              <CharacterViewer :key="viewerKey" :name="selected.name" :model="selected.model" :skin-hash="selected.skinHash" :cape-hash="selected.capeHash" @fallback="fallbackViewer = viewerKey" />
              <section class="management-panel" :aria-label="t('角色和材质操作')">
                <div class="management-heading"><div><h3>{{ t("角色设置") }}</h3><p>{{ t("修改角色名称、绑定皮肤或披风。") }}</p></div><button class="danger-button" type="button" :disabled="actionBusy" @click="deleteCharacter">{{ t("删除角色") }}</button></div>
                <form class="inline-form rename-form" @submit.prevent="renameCharacter"><label :for="`rename-${selected.id}`">{{ t("角色名") }}</label><div class="inline-controls"><input :id="`rename-${selected.id}`" v-model="renameCharacterName" name="name" autocomplete="off" minlength="3" maxlength="16" pattern="[A-Za-z0-9_]{3,16}" required :disabled="actionBusy" /><button class="secondary-button" type="submit" :disabled="actionBusy || renameCharacterName.trim() === selected.name">{{ t("保存名称") }}</button></div></form>
                <div class="binding-grid"><div v-for="kind in (['skin', 'cape'] as const)" :key="kind" class="character-texture-actions"><strong>{{ t(kind === 'skin' ? '皮肤' : '披风') }}</strong><button class="secondary-button" type="button" :disabled="actionBusy" @click="texturePicker = kind">{{ t(kind === 'skin' ? '选择皮肤' : '选择披风') }}</button><button class="text-button" type="button" :disabled="actionBusy || !(kind === 'skin' ? selected.skinHash : selected.capeHash)" @click="clearTexture(kind)">{{ t('清除') }}</button></div></div>
              <div class="material-link"><button class="text-button" type="button" @click="navigate('/user/resources/upload')">{{ t('上传材质') }}</button><button class="text-button" type="button" @click="navigate('/library')">{{ t('浏览公开库') }}</button></div>
              </section>
              <details :key="`sources:${viewerKey}`" class="texture-sources" :open="fallbackViewer === viewerKey">
                <summary>{{ t("PNG 材质与下载") }}</summary>
                <div class="texture-grid">
                <section v-for="texture in [{ label: t('皮肤'), hash: selected.skinHash, kind: 'skin' }, { label: t('披风'), hash: selected.capeHash, kind: 'cape' }]" :key="`${selected.id}-${texture.kind}`" class="texture-section"><div class="texture-heading"><h3>{{ texture.label }}</h3><a v-if="texture.hash" class="download-link" :href="textureUrl(texture.hash)" :download="`${selected.name}-${texture.kind}.png`">{{ t("下载 PNG") }}<AppIcon name="download" /></a></div><div class="texture-preview checkerboard" :class="{ 'has-texture': texture.hash && !failedImages.includes(texture.hash) }"><img v-if="texture.hash && !failedImages.includes(texture.hash)" :src="textureUrl(texture.hash)" :alt="t('角色“{name}”的 {kind} PNG 材质平面图', { name: selected.name, kind: texture.label })" @error="imageFailed(texture.hash)" /><div v-else class="texture-placeholder"><AppIcon :name="texture.hash ? 'error' : 'plus'" /><p>{{ texture.hash ? t('材质预览加载失败') : t('尚未设置{kind}', { kind: texture.label }) }}</p><small v-if="texture.hash">{{ t("可尝试下载或刷新列表") }}</small></div></div></section>
                </div>
              </details>
            </section>
            <section v-else class="empty-state panel"><span class="empty-character" aria-hidden="true"><InterfaceIcon name="empty-character" /></span><h2>{{ t("创建你的第一个角色") }}</h2><p>{{ t("创建后即可穿戴皮肤和披风，你也可以先去上传材质。") }}</p><button class="secondary-button" type="button" @click="navigate('/user/resources/upload')">{{ t("前往上传材质") }}</button></section>
            </div>
          </div>
        </template>
        <section v-show="page === 'resources'" class="materials-page" aria-labelledby="materials-title">
          <div class="page-heading"><div><h1 id="materials-title">{{ t("我的材质") }}</h1></div><button class="secondary-button" type="button" :disabled="texturesBusy || actionBusy" @click="loadTextures">{{ texturesBusy ? t('正在刷新…') : t('刷新列表') }}</button></div>

          <nav class="material-tabs" :aria-label="t('材质栏目')">
            <button type="button" :class="{ active: materialTab === 'browse' }" :aria-current="materialTab === 'browse' ? 'page' : undefined" @click="openMaterialTab('browse')">{{ t('浏览材质') }}</button>
            <button type="button" :class="{ active: materialTab === 'upload' }" :aria-current="materialTab === 'upload' ? 'page' : undefined" @click="openMaterialTab('upload')">{{ t('上传材质') }}<AppIcon name="plus" /></button>
          </nav>
          <div data-panel-region="resources">
          <ResourceFilters v-if="materialTab === 'browse'" v-model:query="materialSearch" v-model:kind="materialKind" label="搜索我的材质" placeholder="名称或描述" :busy="texturesBusy" @search="searchMaterials" />
          <div v-if="materialTab === 'browse'" class="materials-workspace browse-list-layout">
            <section class="material-library panel" :aria-label="t('我的材质')" :aria-busy="texturesBusy">
              <div class="panel-title"><h2>{{ t('我的材质') }}</h2><span>{{ t('{count} 份材质', { count: textureTotal }) }}</span></div>
              <LoadingSkeleton v-if="texturesBusy" layout="cards" label="正在加载材质…" :count="6" />
              <div v-else-if="textureError" class="empty-state" role="alert"><span class="empty-symbol"><AppIcon name="error" /></span><h2>{{ t("加载失败") }}</h2><button class="secondary-button" type="button" @click="loadTextures">{{ t("重新加载") }}</button></div>
              <div v-else-if="!visibleTextures.length" class="empty-state"><span class="empty-symbol"><AppIcon name="plus" /></span><h2>{{ t(materialKind === 'skin' ? '还没有皮肤' : materialKind === 'cape' ? '还没有披风' : '还没有材质') }}</h2><p>{{ t("上传 PNG 皮肤或披风，即可在此 3D 预览并应用到角色。") }}</p><button class="secondary-button" type="button" @click="openMaterialTab('upload')">{{ t('上传材质') }}</button></div>
              <div v-else class="material-cards"><button v-for="texture in visibleTextures" :key="texture.id" class="material-card" :class="{ selected: texture.id === selectedTextureId }" type="button" :aria-pressed="texture.id === selectedTextureId" @click="selectedTextureId = texture.id"><ModelThumbnail v-if="!texture.reviewState || texture.reviewState === 'approved'" class="material-thumbnail" :name="texture.name" :model="texture.kind === 'skin' ? texture.model : 'default'" :skin-hash="texture.kind === 'skin' ? texture.fileHash : ''" :cape-hash="texture.kind === 'cape' ? texture.fileHash : ''" /><span class="material-card-copy"><strong>{{ texture.name }}</strong><span>{{ texture.kind === 'skin' ? modelLabel(texture.model) : t('披风') }}<small>{{ texture.reviewState && texture.reviewState!=='approved' ? t(texture.reviewState==='pending'?'待审核':'已拒绝') : texture.public ? t('公开') : t('未公开') }}</small></span></span></button></div>
            <nav v-if="textureTotal > 20" class="library-pagination"><button type="button" class="secondary-button" :disabled="texturesBusy || texturePage <= 1" @click="texturePage--; loadTextures()">{{ t('上一页') }}</button><span>{{ t('第 {page} / {total} 页', { page: texturePage, total: Math.ceil(textureTotal / 20) }) }}</span><button type="button" class="secondary-button" :disabled="texturesBusy || texturePage * 20 >= textureTotal" @click="texturePage++; loadTextures()">{{ t('下一页') }}</button></nav></section>
            <AppModal v-if="selectedTexture" :title="selectedTexture.name" wide :busy="actionBusy" :confirm-close="editTextureDirty ? dirtyModalConfirm : false" @close="selectedTextureId = ''"><div class="material-preview">
              <div class="material-preview-heading"><h2 id="material-preview-title">{{ t("材质预览") }}</h2><span>{{ selectedTexture.public ? t('公开') : t('未公开') }}</span></div>
              <CharacterViewer v-if="page === 'resources' && (!selectedTexture.reviewState || selectedTexture.reviewState === 'approved')" :key="selectedTexture.id" :name="selectedTexture.name" :model="selectedTexture.kind === 'skin' ? selectedTexture.model : 'default'" :skin-hash="selectedTexture.kind === 'skin' ? selectedTexture.fileHash : ''" :cape-hash="selectedTexture.kind === 'cape' ? selectedTexture.fileHash : ''" @fallback="materialFallback = selectedTexture.id" />
              <div class="material-description"><h3>{{ selectedTexture.name }}</h3><p v-if="selectedTexture.reviewState && selectedTexture.reviewState!=='approved'" class="password-rule">{{ t(selectedTexture.reviewState==='pending'?'待审核':'已拒绝') }} {{ selectedTexture.reviewReason }}</p><small>{{ selectedTexture.category }} {{ selectedTexture.tags?.join(', ') }}</small><p>{{ selectedTexture.description || t('暂无描述。') }}</p></div>
              <div v-if="selectedTexture.owned" class="material-editing">
                <form v-if="editingTextureId === selectedTexture.id" class="texture-edit-form" @submit.prevent="saveTexture"><fieldset :disabled="actionBusy"><label class="binding-field"><span>{{ t("材质名称") }}</span><input v-model="editTextureName" maxlength="255" required /></label><label class="binding-field"><span>{{ t('分类') }}</span><select v-model="editTextureCategory"><option value="">{{ t('未分类') }}</option><option v-for="category in site.contentPolicy?.categories || []" :key="category">{{ category }}</option></select></label><label class="binding-field"><span>{{ t('标签') }}</span><input v-model="editTextureTags" maxlength="260" :placeholder="t('用逗号分隔')" /></label><label class="binding-field description-field"><span>{{ t("材质描述") }} <small>{{ editDescriptionLength }}/200</small></span><textarea v-model="editTextureDescription" rows="3"></textarea></label><label class="visibility-choice"><input v-model="editTexturePublic" type="checkbox" /><span><strong>{{ t("公开这份材质") }}</strong><small>{{ t("取消公开后不会出现在公开库，已收藏或穿戴的角色不受影响；持有直链的客户端仍可加载。") }}</small></span></label><div class="texture-edit-actions"><button class="primary-button" type="submit">{{ t("保存资料") }}</button><button class="secondary-button" type="button" @click="editingTextureId = ''">{{ t("取消") }}</button></div></fieldset></form>
                <div v-else class="texture-edit-actions"><button class="secondary-button" type="button" :disabled="actionBusy" @click="editTexture">{{ t("编辑资料") }}</button><button class="danger-button" type="button" :disabled="actionBusy" @click="deleteTexture">{{ t("删除材质") }}</button></div>
              </div>
              <button v-if="(!selectedTexture.reviewState || selectedTexture.reviewState==='approved') && (selectedTexture.owned || selectedTexture.public)" class="secondary-button material-wear" type="button" :disabled="actionBusy || wardrobeBusy || !!wardrobeError || wardrobe.some(item => item.id === selectedTexture?.id)" @click="collectOwnTexture">{{ wardrobe.some(item => item.id === selectedTexture?.id) ? t('已在衣柜') : t('收藏到衣柜') }}</button>
              <p v-if="selectedTexture.reviewState && selectedTexture.reviewState !== 'approved'" class="password-rule">{{ t('材质通过审核后可预览和下载。') }}</p><details v-else :key="selectedTexture.id" class="texture-sources" :open="materialFallback === selectedTexture.id"><summary>{{ t("PNG 材质与下载") }}</summary><div class="texture-preview checkerboard"><img v-if="!failedImages.includes(selectedTexture.fileHash)" :src="textureUrl(selectedTexture.fileHash)" :alt="t('“{name}”的 PNG 材质平面图', { name: selectedTexture.name })" @error="imageFailed(selectedTexture.fileHash)" /><p v-else class="texture-placeholder">{{ t("材质预览加载失败，可尝试下载或刷新列表。") }}</p></div><a class="download-link material-download" :href="textureUrl(selectedTexture.fileHash)" :download="`${selectedTexture.name}.png`">{{ t("下载 PNG") }}</a></details>
              <button class="secondary-button material-wear" type="button" @click="navigate('/user')">{{ t("前往角色管理穿戴") }}</button>
              <button type="button" class="primary-button material-wear" :disabled="actionBusy || (selectedTexture.reviewState && selectedTexture.reviewState !== 'approved')" @click="applyingTexture = selectedTexture">{{ t('应用到角色') }}</button>
              <div v-if="selectedTextureActions.length" class="texture-plugin-actions">
                <span class="texture-plugin-actions-label">{{ t('更多工具') }}</span>
                <div class="settings-actions">
                  <button v-for="item in selectedTextureActions" :key="`${item.plugin.id}:${item.frontend.id}`" type="button" class="secondary-button" @click="activeTextureAction = { plugin: item.plugin, frontend: item.frontend, texture: selectedTexture! }">{{ t(item.frontend.label) }}</button>
                </div>
              </div>
            </div></AppModal>
          </div>
          <div v-show="materialTab === 'upload'" class="upload-workspace">
            <section class="material-panel panel" aria-labelledby="upload-title">
              <form class="upload-form" :class="{ 'drag-active': uploadDragActive }" @dragenter.prevent="onUploadDragEnter" @dragover.prevent @dragleave.prevent="onUploadDragLeave" @drop.prevent="onUploadDrop" @submit.prevent="uploadTexture"><fieldset :disabled="actionBusy">
                <div class="management-heading"><div><h2 id="upload-title">{{ t("上传材质") }}</h2><p>{{ t("支持标准或纤细皮肤与披风，上传后可在列表中 3D 预览并应用；可将 PNG 拖入此处。") }}</p></div></div>
                <div class="upload-controls"><label class="binding-field"><span>{{ t("类型") }}</span><select v-model="uploadKind" :aria-label="t('类型')" @change="uploadModel = 'default'"><option value="skin">{{ t("皮肤") }}</option><option value="cape">{{ t("披风") }}</option></select></label><label v-if="uploadKind === 'skin'" class="binding-field"><span>{{ t("手臂模型") }}</span><select v-model="uploadModel" :aria-label="t('手臂模型')"><option value="default">{{ t("标准手臂") }}</option><option value="slim">{{ t("纤细手臂") }}</option></select></label><label class="binding-field"><span>{{ t("名称") }}</span><input v-model="uploadName" maxlength="255" :placeholder="uploadNamePlaceholder" /></label><label class="binding-field"><span>{{ t("PNG 文件") }}</span><input ref="uploadInput" type="file" accept="image/png,.png" @change="uploadFile = ($event.target as HTMLInputElement).files?.[0] || null" /></label></div>
                <p v-if="uploadPrecheck" class="upload-file-check" :class="{ 'form-error': !uploadPrecheck.ok }" role="status">{{ uploadPrecheck.text }}</p>
                <label class="binding-field"><span>{{ t('分类') }}</span><select v-model="uploadCategory"><option value="">{{ t('未分类') }}</option><option v-for="category in site.contentPolicy?.categories || []" :key="category">{{ category }}</option></select></label><label class="binding-field"><span>{{ t('标签') }}</span><input v-model="uploadTags" maxlength="260" :placeholder="t('用逗号分隔')" /></label><label class="binding-field description-field"><span>{{ t("描述") }} <small>{{ descriptionLength }}/200</small></span><textarea v-model="uploadDescription" rows="3" :placeholder="t('简单介绍这份材质（可选）')"></textarea></label>
                <label class="visibility-choice"><input v-model="uploadPublic" type="checkbox" /><span><strong>{{ t("公开这份材质") }}</strong><small>{{ t("默认仅自己可见；持有图片直链的客户端仍可加载。") }}</small></span></label>
                <p v-if="site.contentPolicy?.reviewUploads" class="password-rule">{{ t('本站已开启上传审核，审核通过后方可穿戴和下载。') }}</p><p v-if="site.points.enabled" class="point-fee">{{ t('每次上传需 {cost} 积分，包括相同图片的新记录；失败不扣费。', { cost: site.points.uploadCost }) }}</p><button class="primary-button" type="submit" :disabled="actionBusy || (!!uploadPrecheck && !uploadPrecheck.ok)">{{ actionBusy ? t('正在上传…') : t('上传到我的材质') }}</button>
              </fieldset></form>
            </section>
            <aside class="upload-guide panel" aria-labelledby="upload-guide-title">
              <h2 id="upload-guide-title">{{ t("上传要求") }}</h2>
              <div v-if="accountQuota" class="quota-meter">
                <div class="quota-meter-head">
                  <span>{{ t('账号材质空间') }}</span>
                  <strong>{{ accountQuota.accountLimitBytes > 0 ? `${formatBytes(accountQuota.accountUsedBytes)} / ${formatBytes(accountQuota.accountLimitBytes)}` : formatBytes(accountQuota.accountUsedBytes) }}</strong>
                </div>
                <div v-if="accountQuota.accountLimitBytes > 0" class="quota-bar" role="progressbar" :aria-valuenow="quotaPercent(accountQuota.accountUsedBytes, accountQuota.accountLimitBytes)" aria-valuemin="0" aria-valuemax="100" :aria-label="t('账号材质存储使用比例')">
                  <span class="quota-bar-fill" :class="{ warning: quotaPercent(accountQuota.accountUsedBytes, accountQuota.accountLimitBytes) >= 85 }" :style="{ width: `${quotaPercent(accountQuota.accountUsedBytes, accountQuota.accountLimitBytes)}%` }"></span>
                </div>
              </div>
              <dl><div><dt>{{ t("文件") }}</dt><dd>{{ uploadFileRuleText }}</dd></div><div><dt>{{ t("皮肤") }}</dt><dd>{{ uploadSkinRuleText }}</dd></div><div><dt>{{ t("披风") }}</dt><dd>{{ uploadCapeRuleText }}</dd></div></dl><p>{{ t("上传后可在材质详情中应用到角色。") }}</p>
            </aside>
          </div>
          </div>
        </section>
        <template v-if="page === 'account'">
          <div class="page-heading"><div><h1>{{ t("账号") }}</h1><p>{{ t("管理个人资料、登录安全与游戏接入。") }}</p></div></div>
          <nav class="account-tabs admin-navigation" :aria-label="t('账号栏目')"><div class="admin-core-navigation"><span class="admin-group-label">{{ t('账号工作区') }}</span><div><button v-for="item in (['dashboard', 'profile', 'points', 'integration', 'reports'] as const)" :key="item" type="button" class="secondary-button" :class="{ active: accountTab === item }" :aria-current="accountTab === item ? 'page' : undefined" @click="navigate(accountPath(item))">{{ t({ dashboard: '仪表盘', profile: '个人资料与安全', points: '积分', integration: '游戏接入', reports: '我的举报' }[item]) }}</button></div></div></nav>
          <div data-panel-region="account">
          <section v-if="accountTab === 'dashboard'" class="account-dashboard" :aria-label="t('账号仪表盘')">
            <div class="account-dashboard-grid"><article class="panel account-summary-card account-summary-primary"><div class="account-profile"><img class="user-avatar large" :src="'/api/avatars/' + user.id + '.png?v=' + avatarVersion" alt="" width="64" height="64" /><div><p class="eyebrow">{{ t('当前账号') }}</p><h2>{{ user.nickname || t('我的账号') }}</h2><p>{{ user.email }}</p></div></div><span class="account-status" :class="{ verified: user.emailVerified }">{{ user.emailVerified ? t('邮箱已验证') : t('邮箱待验证') }}</span></article><article class="panel account-summary-card"><h2>{{ t('角色') }}</h2><strong>{{ characters.length }} / {{ characterLimit }}</strong><p>{{ t('已创建角色') }}</p><button class="text-button" type="button" @click="navigate('/user')">{{ t('打开角色管理') }}<AppIcon name="arrow" /></button></article><article class="panel account-summary-card"><h2>{{ t('材质与衣柜') }}</h2><strong>{{ textures.length + wardrobe.length }}</strong><p>{{ t('材质与收藏总数') }}</p><button class="text-button" type="button" @click="navigate('/user/resources')">{{ t('打开我的材质') }}<AppIcon name="arrow" /></button></article></div>
            <div class="account-dashboard-columns"><section class="panel account-dashboard-section"><div class="panel-title"><h2>{{ t('快速操作') }}</h2><span>{{ t('常用入口') }}</span></div><div class="account-quick-actions"><button class="secondary-button" type="button" @click="navigate('/user/account/profile')">{{ t('修改个人资料') }}</button><button class="secondary-button" type="button" @click="navigate('/user/resources/upload')">{{ t('上传材质') }}</button><button class="secondary-button" type="button" @click="navigate('/user/wardrobe')">{{ t('打开我的衣柜') }}</button><button class="secondary-button" type="button" @click="navigate('/user/account/reports')">{{ t('查看我的举报') }}</button></div></section><section class="panel account-dashboard-section"><div class="panel-title"><h2>{{ t('账号状态') }}</h2><span>{{ site.name }}</span></div><dl class="account-fields"><div><dt>{{ t('昵称') }}</dt><dd>{{ user.nickname || t('未设置') }}</dd></div><div><dt>{{ t('登录邮箱') }}</dt><dd>{{ user.email }}</dd></div><div><dt>{{ t('账号类型') }}</dt><dd>{{ user.isAdmin ? t('管理员') : t('普通用户') }}</dd></div></dl></section></div>
          </section>
          <section v-else-if="accountTab === 'profile'" class="account-panel panel" aria-labelledby="account-title"><div class="account-block account-overview"><div class="account-section-heading"><div><h2>{{ t('个人资料') }}</h2><p>{{ t('头像、昵称和站内身份信息。') }}</p></div></div><div class="account-profile"><img class="user-avatar large" :src="'/api/avatars/' + user.id + '.png?v=' + avatarVersion" alt="" width="64" height="64" /><div><h2 id="account-title">{{ user.nickname || t('我的账号') }}</h2><p>{{ t('已登录 {name}', { name: site.name }) }}</p></div></div><dl class="account-fields"><div><dt>{{ t("昵称") }}</dt><dd>{{ user.nickname || t('未设置') }}</dd></div><div><dt>{{ t("邮箱") }}</dt><dd>{{ user.email }}</dd></div><div><dt>{{ t('邮箱状态') }}</dt><dd>{{ user.emailVerified ? t('已验证') : t('未验证') }}</dd></div></dl></div><div class="account-block account-security"><div class="account-section-heading"><div><h2>{{ t('登录与安全') }}</h2><p>{{ t('更新个人资料、邮箱验证和密码。') }}</p></div></div><ProfileSettings :csrf-token="csrfToken" :characters="characters" @updated="profileUpdated" @expired="expireSession" /><ConnectorAccount :csrf-token="csrfToken" @expired="clearSession" /><OAuthAccount :csrf-token="csrfToken" @expired="clearSession" /><OIDCAccount :csrf-token="csrfToken" @expired="expireSession" /><MinecraftAccount :csrf-token="csrfToken" @expired="expireSession" /><TOTPSettings :csrf-token="csrfToken" @expired="expireSession" /><section v-if="!user.emailVerified && !site.manualEmailVerification" class="account-password"><h3>{{ t('验证邮箱') }}</h3><button class="secondary-button" type="button" :disabled="verificationBusy || !site.mailEnabled" @click="sendVerificationMail">{{ verificationBusy ? t('正在发送验证邮件…') : t('发送验证邮件') }}</button></section><section class="account-password" aria-labelledby="password-title"><h3 id="password-title">{{ t("修改密码") }}</h3><p>{{ t("修改密码后，所有网页会话与游戏令牌都将失效，需要重新登录。") }}</p><form class="password-form" @submit.prevent="changePassword"><fieldset :disabled="passwordBusy"><input type="hidden" name="username" :value="user.email" autocomplete="username" /><label class="binding-field"><span>{{ t("当前密码") }}</span><input v-model="currentPassword" type="password" autocomplete="current-password" required /></label><label class="binding-field"><span>{{ t("新密码") }}</span><input v-model="newPassword" type="password" autocomplete="new-password" required aria-describedby="password-rule" /></label><p id="password-rule" class="password-rule">{{ t("8–128 个字符，须同时包含英文字母、数字和符号；空白不计作符号。") }}</p><label class="binding-field"><span>{{ t("确认新密码") }}</span><input v-model="confirmPassword" type="password" autocomplete="new-password" required /></label><button class="primary-button" type="submit" :disabled="passwordBusy || actionBusy">{{ passwordBusy ? t('正在修改…') : t('修改密码并重新登录') }}</button></fieldset></form></section><div class="account-logout"><div><h3>{{ t("退出当前账号") }}</h3><p>{{ t("退出后需要重新登录才能访问工作台。") }}</p></div><button class="secondary-button" type="button" :disabled="logoutBusy || passwordBusy || actionBusy || charactersBusy || texturesBusy || wardrobeBusy" @click="logout">{{ logoutBusy ? t('正在退出…') : t('退出登录') }}</button></div></div></section>
          <PointsPanel v-else-if="accountTab === 'points'" :csrf-token="csrfToken" @expired="expireSession" />
          <IntegrationPanel v-else-if="accountTab === 'integration'" :non-email-login="!!site.yggNonEmailLogin" />
          <ReportsView v-else-if="accountTab === 'reports'" :csrf-token="csrfToken" @expired="expireSession" />
          </div>
        </template>
        <SiteFooter :name="site.name" :tagline="site.tagline || t('每一个像素，都是你的模样。')" :footer="site.footer" :show-mc-disclaimer="showMcDisclaimer" />
      </main>
    </div>
    <SiteFooter v-if="!user || publicHome || notFound" class="public-site-footer" :name="site.name" :tagline="site.tagline" :footer="site.footer" :show-mc-disclaimer="showMcDisclaimer" />
  </div>
</template>
