import type { PointRules } from './pointTypes'
export interface Appearance { logoImage: string; accent: 'blue' | 'green' | 'violet'; defaultTheme: 'light' | 'dark' | 'system' }
export interface HomepageFeature { title: string; body: string; href: string }
export interface HomepageBlock { id: string; kind: 'intro' | 'features' | 'notice' | 'cta'; enabled: boolean; title: string; body: string; buttonLabel: string; buttonHref: string; features?: HomepageFeature[] }
export interface HomepageSettings { blocks: HomepageBlock[] }
export interface SiteAccess {
  appearance: Appearance
  homepage: HomepageSettings
  noticeFormat: 'plain' | 'markdown' | ''
  noticeHtml: string
  points: PointRules
  name: string
  tagline: string
  footer: string
  notice: string
  registration: 'closed' | 'open' | 'invitation'
  emailDomains: string[]
  mailEnabled: boolean
  turnstileSiteKey: string
}
export type AccessMode = 'register' | 'mail' | 'verify' | 'reset'
