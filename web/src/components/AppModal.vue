<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, useId } from 'vue'
import { t } from '../i18n'
import { notifications, dismissToast, modalStack } from '../feedback'
const props = defineProps<{ title: string; busy?: boolean; wide?: boolean; alert?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const titleId = useId()
let previousFocus: HTMLElement | null = null
let active = true
let closing = false
function backdrop(event: MouseEvent) {
  if (!dialog.value || event.target !== dialog.value) return
  const rect = dialog.value.getBoundingClientRect()
  if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close()
}
async function close() {
  if (props.busy || closing) return
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
    <Transition name="modal-motion" appear><dialog ref="dialog" :role="alert ? 'alertdialog' : 'dialog'" class="app-modal" :class="{ 'app-modal-wide': wide }" :aria-labelledby="titleId" :aria-busy="busy || undefined" @cancel.prevent="close" @click="backdrop">
      <header class="app-modal-heading"><h2 :id="titleId">{{ title }}</h2><button type="button" class="secondary-button modal-close" :disabled="busy" :aria-label="t('关闭')" @click="close">×</button></header>
      <div class="app-modal-body"><slot /></div>
      <TransitionGroup v-if="modalStack.at(-1) === titleId" name="toast-motion" tag="div" class="toast-stack modal-toast-stack" :aria-label="t('通知')"><div v-for="item in notifications" :key="item.id" class="toast" :class="'toast-' + item.tone" :role="item.tone === 'error' ? 'alert' : 'status'"><span>{{ item.message }}</span><button type="button" :aria-label="t('关闭')" @click="dismissToast(item.id)">×</button></div></TransitionGroup>
    </dialog></Transition>
  </Teleport>
</template>
