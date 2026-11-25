<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import AppModal from './AppModal.vue'
import { nextTick, onMounted, ref, watch } from 'vue'
import { confirmation, decideConfirmation, notifications, dismissToast, modalStack } from '../feedback'
import { t } from '../i18n'

const toastLayer = ref<HTMLDivElement | null>(null)

async function syncTopLayer() {
  await nextTick()
  const el = toastLayer.value as (HTMLDivElement & { showPopover?: () => void; hidePopover?: () => void }) | null
  if (!el || typeof el.showPopover !== 'function' || typeof el.hidePopover !== 'function') return
  try {
    if (notifications.value.length === 0) {
      if (el.matches(':popover-open')) el.hidePopover()
      return
    }
    if (el.matches(':popover-open')) el.hidePopover()
    el.showPopover()
  } catch {
    // Fallback for test environments without Popover Top Layer API.
  }
}

watch([() => notifications.value.length, () => modalStack.value.length], () => {
  void syncTopLayer()
})

onMounted(() => {
  void syncTopLayer()
})
</script>
<template>
  <Teleport to="body">
    <div ref="toastLayer" popover="manual" class="toast-layer">
      <TransitionGroup name="toast-motion" tag="div" class="toast-stack" :aria-label="t('通知')">
        <div v-for="item in notifications" :key="item.id" class="toast" :class="`toast-${item.tone}`" :role="item.tone === 'error' ? 'alert' : 'status'">
          <span>{{ item.message }}</span>
          <button type="button" :aria-label="t('关闭')" @click="dismissToast(item.id)"><AppIcon name="close" /></button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
  <AppModal v-if="confirmation" alert :title="t(confirmation.title)" @close="decideConfirmation(false)"><p>{{ confirmation.message }}</p><div class="modal-actions"><button class="secondary-button" type="button" autofocus @click="decideConfirmation(false)">{{ t(confirmation.cancelLabel || '取消') }}</button><button :class="confirmation.danger ? 'danger-button' : 'primary-button'" type="button" @click="decideConfirmation(true)">{{ t(confirmation.confirmLabel || '确认') }}</button></div></AppModal>
</template>
