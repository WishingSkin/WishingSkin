import { getCurrentInstance, onBeforeUnmount, shallowRef, type Ref } from 'vue'
import { normalizePath } from './workspaceRoutes.ts'

export interface ConfirmPromptOptions {
  title: string
  message: string
  danger?: boolean
  confirmLabel?: string
  cancelLabel?: string
}

export type ConfirmPrompter = (options: ConfirmPromptOptions) => Promise<boolean>

interface GuardEntry {
  id: number
  dirty: Ref<boolean>
  keepWithin?: string
  tag?: string
}

export const dirtyModalConfirm = {
  title: '放弃未保存的更改？',
  message: '关闭后，填写的内容不会保存。',
  confirmLabel: '放弃更改',
  cancelLabel: '继续编辑',
  danger: true,
} as const

export const oneTimeSecretModalConfirm = {
  title: '确认关闭密钥',
  message: '密钥关闭后不会再次显示。确认已保存？',
  confirmLabel: '已保存，关闭',
  cancelLabel: '返回查看',
  danger: false,
} as const

const guards = shallowRef<GuardEntry[]>([])
let nextGuardId = 0

function destinationPath(destination: string) {
  const withoutHash = destination.split('#')[0] || '/'
  const withoutQuery = withoutHash.split('?')[0] || '/'
  return normalizePath(withoutQuery)
}

function staysWithin(destination: string, keepWithin?: string) {
  if (!keepWithin) return false
  const path = destinationPath(destination)
  if (keepWithin.endsWith('/')) {
    const base = normalizePath(keepWithin.slice(0, -1))
    return path === base || path.startsWith(keepWithin)
  }
  const base = normalizePath(keepWithin)
  return path === base || path.startsWith(base + '/')
}

export function useUnsavedGuard(dirty: Ref<boolean>, keepWithin?: string, tag?: string) {
  const id = ++nextGuardId
  guards.value = [...guards.value, { id, dirty, keepWithin, tag }]
  const unregister = () => {
    guards.value = guards.value.filter(entry => entry.id !== id)
  }
  if (getCurrentInstance()) {
    onBeforeUnmount(unregister)
  }
  return unregister
}

export function hasUnsavedChanges() {
  return guards.value.some(entry => entry.dirty.value)
}

export function hasUnsavedForTag(tag: string) {
  return guards.value.some(entry => entry.tag === tag && entry.dirty.value)
}

export function shouldConfirmLeave(destination: string) {
  return guards.value.some(entry => entry.dirty.value && !staysWithin(destination, entry.keepWithin))
}

export async function confirmLeave(destination: string, ask?: ConfirmPrompter) {
  if (!shouldConfirmLeave(destination)) return true
  if (ask) {
    return ask({
      title: '放弃未保存的更改？',
      message: '离开当前页面后，填写的内容不会保存。',
      danger: true,
      confirmLabel: '放弃更改',
      cancelLabel: '继续编辑',
    })
  }
  const [{ confirmAction }, { t }] = await Promise.all([import('./feedback'), import('./i18n')])
  return confirmAction({
    title: '放弃未保存的更改？',
    message: t('离开当前页面后，填写的内容不会保存。'),
    danger: true,
    confirmLabel: '放弃更改',
    cancelLabel: '继续编辑',
  })
}

export function clearUnsavedGuards() {
  guards.value = []
}
