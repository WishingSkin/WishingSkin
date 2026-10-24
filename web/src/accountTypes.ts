import type { PointRules } from './pointTypes'
export interface Appearance { logoImage: string; accent: 'blue' | 'green' | 'violet'; defaultTheme: 'light' | 'dark' | 'system' }
export interface SiteAccess {
  appearance: Appearance
  noticeFormat: 'plain' | 'markdown' | ''
  noticeHtml: string
  points: PointRules
  name: string
  tagline: string
  notice: string
  registration: 'closed' | 'open' | 'invitation'
  mailEnabled: boolean
  turnstileSiteKey: string
}
export type AccessMode = 'register' | 'mail' | 'verify' | 'reset'
