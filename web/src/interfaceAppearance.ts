import { shallowRef } from 'vue'
import type { AdvancedAppearance } from './accountTypes'

export function cloneAdvanced(value?: Partial<AdvancedAppearance>): AdvancedAppearance {
  return { light: { ...value?.light }, dark: { ...value?.dark }, copy: {}, localizedCopy: { 'zh-CN': { ...value?.copy, ...value?.localizedCopy?.['zh-CN'] }, en: { ...value?.copy, ...value?.localizedCopy?.en } }, icons: { ...value?.icons } }
}
export const interfaceAppearance = shallowRef<AdvancedAppearance>(cloneAdvanced())
export const colorFields = [
  ['bg', '页面背景'], ['panel', '面板背景'], ['subtle', '次级背景'], ['border', '边框'],
  ['ink', '正文'], ['heading', '标题'], ['muted', '辅助文字'], ['nav', '导航文字'],
  ['accent', '主题色'], ['accent-hover', '主题悬停色'], ['accent-soft', '主题浅背景'], ['button-text', '按钮文字'],
  ['login-title', '登录页标题'], ['login-body', '登录页说明'], ['footer', '页脚文字'], ['error', '错误文字'], ['error-bg', '错误背景'],
] as const
export const iconFields = [['login', '登录页标记'], ['characters', '角色管理'], ['materials', '我的材质'], ['wardrobe', '我的衣柜'], ['account', '账号'], ['administration', '管理后台'], ['empty-character', '空角色状态'], ['library', '公开库'], ['arrow', '登录按钮箭头']] as const

export function setInterfaceAppearance(value?: Partial<AdvancedAppearance>) {
  interfaceAppearance.value = cloneAdvanced(value)
}

export function interfaceText(source: string, language: 'zh-CN' | 'en', fallback: string) {
  return interfaceAppearance.value.localizedCopy[language][source] || fallback
}

export function applyInterfaceColors(dark: boolean) {
  const palette = interfaceAppearance.value[dark ? 'dark' : 'light']
  for (const [key] of colorFields) {
    const value = palette[key]
    if (value && /^#[\da-f]{6}$/i.test(value)) document.documentElement.style.setProperty(`--${key}`, value)
    else document.documentElement.style.removeProperty(`--${key}`)
  }
}
