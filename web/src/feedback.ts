import { customRef, ref } from 'vue'
import { t } from './i18n'

type Tone = 'success' | 'error' | 'info'
export const notifications = ref<{ id: number; message: string; tone: Tone }[]>([])
export const modalStack = ref<string[]>([])
let sequence = 0
export function dismissToast(id: number) { notifications.value = notifications.value.filter(item => item.id !== id) }
export function toast(message: string, tone: Tone = 'success') {
  const translated = t(message)
  if (notifications.value.some(item => item.message === translated && item.tone === tone)) return
  const id = ++sequence
  notifications.value = [...notifications.value.slice(-3), { id, message: translated, tone }]
  setTimeout(() => dismissToast(id), tone === 'error' ? 10000 : 6000)
}
export function feedbackRef(tone: Tone) {
  let value = ''
  return customRef<string>((track, trigger) => ({
    get() { track(); return value },
    set(message) {
      value = message
      trigger()
      if (message) toast(message, tone)
    },
  }))
}
interface Confirmation { title: string; message: string; danger?: boolean; confirmLabel?: string }
export const confirmation = ref<Confirmation | null>(null)
let resolveConfirmation: ((value: boolean) => void) | undefined
export function decideConfirmation(value: boolean) {
  confirmation.value = null
  const resolve = resolveConfirmation
  resolveConfirmation = undefined
  resolve?.(value)
}
export function confirmAction(options: Confirmation) {
  if (resolveConfirmation) decideConfirmation(false)
  confirmation.value = options
  return new Promise<boolean>(resolve => { resolveConfirmation = resolve })
}

export function clearFeedback() { notifications.value = []; decideConfirmation(false) }
