<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import { onMounted, onBeforeUnmount, ref, useId } from 'vue'
import { t } from '../i18n'
import { confirmAction, modalStack } from '../feedback'

export interface ModalCloseConfirm {
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}

const props = defineProps<{ title: string; busy?: boolean; wide?: boolean; alert?: boolean; confirmClose?: ModalCloseConfirm | false }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const titleId = useId()
let previousFocus: HTMLElement | null = null
let active = true
let closing = false
let confirming = false
let sawPointerDown = false
let pointerDownOutside = false

function outside(event: MouseEvent) {
  if (!dialog.value || event.target !== dialog.value) return false
  const rect = dialog.value.getBoundingClientRect()
  return event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom
}
function onPointerDown(event: PointerEvent) {
  sawPointerDown = true
  pointerDownOutside = outside(event)
}
function backdrop(event: MouseEvent) {
  const allow = !sawPointerDown || pointerDownOutside
  sawPointerDown = false
  pointerDownOutside = false
  if (!allow || !outside(event)) return
  void close()
}
async function close() {
  if (props.busy || closing || confirming) return
  if (props.confirmClose) {
    confirming = true
    let confirmed = false
    try {
      confirmed = await confirmAction({
        title: props.confirmClose.title,
        message: t(props.confirmClose.message),
        danger: props.confirmClose.danger ?? true,
        confirmLabel: props.confirmClose.confirmLabel ?? '放弃更改',
        cancelLabel: props.confirmClose.cancelLabel ?? '继续编辑',
      })
    } finally {
      confirming = false
    }
    if (!confirmed || !active) return
  }
  closing = true
  if (dialog.value && !window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    try { await dialog.value.animate([{ opacity: 1, transform: 'translateY(0) scale(1)' }, { opacity: 0, transform: 'translateY(8px) scale(.98)' }], { duration: 200, easing: 'ease-in', fill: 'forwards' }).finished } catch { /* Unmount cancels the visual transition. */ }
  }
  if (active) emit('close')
}
onMounted(() => {
  document.documentElement.style.overflow = 'hidden'
  previousFocus = document.activeElement as HTMLElement | null
  dialog.value?.showModal()
  modalStack.value = [...modalStack.value, titleId]
})
onBeforeUnmount(() => {
  active = false
  dialog.value?.close()
  modalStack.value = modalStack.value.filter(id => id !== titleId)
  if (!modalStack.value.length) document.documentElement.style.overflow = ''
  if (previousFocus?.isConnected) previousFocus.focus()
})
</script>
<template>
  <Teleport to="body">
    <Transition name="modal-motion" appear><dialog ref="dialog" :role="alert ? 'alertdialog' : 'dialog'" class="app-modal" :class="{ 'app-modal-wide': wide }" :aria-labelledby="titleId" :aria-busy="busy || undefined" @cancel.prevent="close" @pointerdown="onPointerDown" @click="backdrop">
      <header class="app-modal-heading"><h2 :id="titleId">{{ title }}</h2><button type="button" class="secondary-button modal-close" :disabled="busy" :aria-label="t('关闭')" @click="close"><AppIcon name="close" /></button></header>
      <div class="app-modal-body"><slot /></div>
    </dialog></Transition>
  </Teleport>
</template>
