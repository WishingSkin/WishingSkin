<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import AppModal from './AppModal.vue'
import { confirmation, decideConfirmation, notifications, dismissToast, modalStack } from '../feedback'
import { t } from '../i18n'
</script>
<template>
  <Teleport to="body"><TransitionGroup v-if="!modalStack.length" name="toast-motion" tag="div" class="toast-stack" :aria-label="t('通知')"><div v-for="item in notifications" :key="item.id" class="toast" :class="`toast-${item.tone}`" :role="item.tone === 'error' ? 'alert' : 'status'"><span>{{ item.message }}</span><button type="button" :aria-label="t('关闭')" @click="dismissToast(item.id)"><AppIcon name="close" /></button></div></TransitionGroup></Teleport>
  <AppModal v-if="confirmation" alert :title="t(confirmation.title)" @close="decideConfirmation(false)"><p>{{ confirmation.message }}</p><div class="modal-actions"><button class="secondary-button" type="button" autofocus @click="decideConfirmation(false)">{{ t(confirmation.cancelLabel || '取消') }}</button><button :class="confirmation.danger ? 'danger-button' : 'primary-button'" type="button" @click="decideConfirmation(true)">{{ t(confirmation.confirmLabel || '确认') }}</button></div></AppModal>
</template>
