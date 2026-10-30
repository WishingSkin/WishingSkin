import { ref, watch } from 'vue'
import { interfaceText } from './interfaceAppearance'
import { english } from './i18n.en'

type Locale = 'zh-CN' | 'en'
const storageKey = 'wishingskin-locale'
function storedLocale(): Locale {
  try { return localStorage.getItem(storageKey) === 'en' ? 'en' : 'zh-CN' } catch { return 'zh-CN' }
}
export const locale = ref<Locale>(storedLocale())

export function setLocale(value: string) {
  locale.value = value === 'en' ? 'en' : 'zh-CN'
}

export function t(source: string, params: Record<string, string | number> = {}) {
  const limit = source.match(/^当前角色上限为 (\d+) 个$/)
  if (limit) return t('当前角色上限为 {limit} 个', { limit: limit[1] })
  const key = Object.hasOwn(english, source) ? source : source + '。'
  const text = interfaceText(source, locale.value, locale.value === 'en' && Object.hasOwn(english, key) ? english[key]! : source)
  return text.replace(/\{(\w+)\}/g, (placeholder, name: string) => Object.hasOwn(params, name) ? String(params[name]) : placeholder)
}

watch(locale, value => {
  document.documentElement.lang = value
  document.cookie = `ws_locale=${value}; Path=/; SameSite=Lax`
  try { localStorage.setItem(storageKey, value) } catch { /* Switching still works without storage. */ }
}, { immediate: true, flush: 'sync' })
