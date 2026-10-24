<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
interface Configuration { authlibInjector: string; cslRoot: string; skinTemplate: string; capeTemplate: string; customSkinLoader: object }
const config = ref<Configuration | null>(null)
const busy = ref(false)
const error = ref('')
const copyMessage = ref('')
async function load() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const response = await fetch('/api/integration')
    if (!response.ok) { error.value = '暂时无法读取接入配置'; return }
    config.value = await response.json()
  } catch { error.value = '暂时无法读取接入配置' }
  finally { busy.value = false }
}
async function copy(value: string) {
  try { await navigator.clipboard.writeText(value); copyMessage.value = '已复制。' }
  catch { copyMessage.value = '无法自动复制，请选择并复制文本。' }
}
function download() {
  if (!config.value) return
  const url = URL.createObjectURL(new Blob([JSON.stringify(config.value.customSkinLoader, null, 2) + '\n'], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = url; link.download = 'CustomSkinLoader.json'; link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
onMounted(load)
</script>

<template>
  <section class="integration-panel panel" :aria-label="t('游戏接入')">
    <h2>{{ t('游戏接入') }}</h2>
    <p>{{ t('外置登录用于认证；CustomSkinLoader 与传统地址用于加载外观。按客户端说明选择一种方式。') }}</p>
    <p v-if="busy" role="status">{{ t('正在加载…') }}</p>
    <div v-else-if="error" class="form-error" role="alert">{{ t(error) }} <button class="text-button" type="button" @click="load">{{ t('重试') }}</button></div>
    <template v-else-if="config">
      <div v-for="item in [{ label: '外置登录地址', value: config.authlibInjector }, { label: 'CustomSkinLoader 根地址', value: config.cslRoot }, { label: '传统皮肤地址模板', value: config.skinTemplate }, { label: '传统披风地址模板', value: config.capeTemplate }]" :key="item.label" class="integration-address">
        <label class="binding-field"><span>{{ t(item.label) }}</span><input :value="item.value" readonly spellcheck="false" /></label>
        <button class="secondary-button" type="button" :aria-label="t('复制{label}', { label: t(item.label) })" @click="copy(item.value)">{{ t('复制') }}</button>
      </div>
      <p>{{ t('传统模板中的 {USERNAME} 替换为角色名。') }}</p>
      <button class="secondary-button" type="button" @click="download">{{ t('下载 CustomSkinLoader 配置') }}</button>
      <p>{{ t('配置优先使用本站外观，再查询 Mojang。合并到现有配置前请先保留副本。') }}</p>
      <p v-if="copyMessage" role="status">{{ t(copyMessage) }}</p>
    </template>
  </section>
</template>
