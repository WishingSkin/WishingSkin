<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import { onBeforeUnmount, ref } from 'vue'
import { toast } from '../feedback'
import { t } from '../i18n'

const props = defineProps<{
  text: string
  label?: string
  variant?: 'icon' | 'text'
  disabled?: boolean
}>()

const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  if (props.disabled || !props.text) return
  try {
    await navigator.clipboard.writeText(props.text)
    copied.value = true
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => { copied.value = false }, 1500)
  } catch {
    toast('无法自动复制，请选择并复制文本。', 'error')
  }
}

onBeforeUnmount(() => { if (timer) clearTimeout(timer) })
</script>

<template>
  <button
    type="button"
    class="secondary-button copy-button"
    :class="{ 'copy-button--icon': (variant || 'icon') === 'icon', copied }"
    :disabled="disabled || !text"
    :aria-label="label || t('复制')"
    :title="label || t('复制')"
    @click="copy"
  >
    <AppIcon :name="copied ? 'check' : 'copy'" />
    <span v-if="variant === 'text'">{{ copied ? t('已复制') : (label || t('复制')) }}</span>
    <span class="sr-only" aria-live="polite">{{ copied ? t('已复制') : '' }}</span>
  </button>
</template>
