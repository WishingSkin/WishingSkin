import type { PointRules } from './pointTypes'
import type { FontPreset, FontScope, ThemePack } from './themePacks'

export interface PublicThemeAssets {
  version: string
  sourceName?: string
  importedAt?: string
  count: number
  keys: string[]
}

export interface PublicThemeFont {
  version: string
  fileName?: string
  size?: number
  uploadedAt?: string
  url?: string
}

export interface Appearance {
  logoImage: string
  accent: 'blue' | 'green' | 'violet' | 'rose' | 'orange' | 'teal' | 'gray'
  defaultTheme: 'light' | 'dark' | 'system'
  themePack?: ThemePack
  surfaceEffect?: 'solid' | 'translucent' | 'glass'
  surfaceOpacity?: number
  surfaceBlur?: number
  allowUserTheme?: boolean
  disableParticles?: boolean
  fontPreset?: FontPreset
  fontScope?: FontScope
  customFontUrl?: string
  advanced?: AdvancedAppearance
}

export interface AdvancedAppearance {
  light: Record<string, string>
  dark: Record<string, string>
  copy: Record<string, string>
  localizedCopy: Record<'zh-CN' | 'en', Record<string, string>>
  icons: Record<string, string>
}

export interface HomepageFeature { title: string; body: string; href: string; icon?: string; iconImage?: string }
export interface HomepageBlock { id: string; name?: string; image?: string; imageAlt?: string; kind: 'intro' | 'features' | 'notice' | 'cta' | 'showcase'; enabled: boolean; title: string; body: string; buttonLabel: string; buttonHref: string; features?: HomepageFeature[]; iconImage?: string }
export interface HomepageSettings { blocks: HomepageBlock[]; background?: { image?: string; imageUrl?: string; overlay: number } }

export interface SiteAccess {
  contentPolicy?: { reviewUploads: boolean; categories: string[] }
  loginImageUrl?: string
  oidcProviders?: { id: string; name: string }[]
  oidc?: { enabled: boolean; name: string }
  appearance: Appearance
  themeAssets?: PublicThemeAssets
  themeFont?: PublicThemeFont
  homepage: HomepageSettings
  noticeFormat: 'plain' | 'markdown' | ''
  noticeHtml: string
  points: PointRules
  name: string
  tagline: string
  footer: string
  notice: string
  registration: 'closed' | 'open' | 'invitation'
  randomUserIds?: boolean
  emailDomains: string[]
  blockedEmailDomains?: string[]
  manualEmailVerification?: boolean
  mailEnabled: boolean
  turnstileSiteKey: string
}

export type AccessMode = 'register' | 'mail' | 'verify' | 'reset'
