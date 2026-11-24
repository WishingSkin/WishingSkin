<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import { feedbackRef, toast } from '../feedback'
import { computed, onMounted, ref } from 'vue'
import { authlibInjectorDragUrl } from '../formatBytes'
import { t } from '../i18n'
const props = defineProps<{ nonEmailLogin?: boolean }>()
interface Configuration { authlibInjector: string; cslRoot: string; skinTemplate: string; capeTemplate: string; customSkinLoader: object }
const config = ref<Configuration | null>(null)
const dragAuthlibUrl = computed(() => config.value ? authlibInjectorDragUrl(config.value.authlibInjector) : '')
const busy = ref(false)
const error = feedbackRef('error')
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
  try { await navigator.clipboard.writeText(value); toast('已复制。') }
  catch { toast('无法自动复制，请选择并复制文本。', 'error') }
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
    <p>{{ t('使用 authlib-injector 进行游戏外置登录，或通过 CustomSkinLoader / 传统链接加载皮肤和披风。') }}</p>
    <LoadingSkeleton v-if="busy" label="正在加载…" :count="2" />
    <div v-else-if="error"><button class="text-button" type="button" @click="load">{{ t('重试') }}</button></div>
    <template v-else-if="config">
      <div v-if="dragAuthlibUrl" class="integration-drag-box">
        <a
          class="primary-button authlib-drag-link"
          :href="dragAuthlibUrl"
          draggable="true"
          @click.prevent="copy(config.authlibInjector)"
        >{{ t('拖拽到启动器快捷接入') }}</a>
        <p>{{ t('将上方按钮直接拖入 HMCL、PCL2 或 BakaXL 等启动器即可自动配置外置登录；点击则复制地址。') }}</p>
      </div>
      <div v-for="item in [{ label: '外置登录地址', value: config.authlibInjector }, { label: 'CustomSkinLoader 根地址', value: config.cslRoot }, { label: '传统皮肤地址模板', value: config.skinTemplate }, { label: '传统披风地址模板', value: config.capeTemplate }]" :key="item.label" class="integration-address">
        <label class="binding-field"><span>{{ t(item.label) }}</span><input :value="item.value" readonly spellcheck="false" /></label>
        <button class="secondary-button" type="button" :aria-label="t('复制{label}', { label: t(item.label) })" @click="copy(item.value)">{{ t('复制') }}</button>
      </div>
      <p>{{ t('使用传统链接模板时，请将 {USERNAME} 替换为游戏角色名。') }}</p>
      <p class="integration-login-mode">{{ props.nonEmailLogin ? t('当前站点已开启非邮箱登录：在启动器外置登录界面既可填账号邮箱，也可直接填游戏角色名。') : t('当前站点需使用账号邮箱与密码在启动器内登录。') }}</p>
      <button class="secondary-button" type="button" @click="download">{{ t('下载 CustomSkinLoader 配置') }}</button>
      <p>{{ t('生成的配置会优先从本站加载外观，未命中时再回退至 Mojang。合并前建议备份原配置。') }}</p>
    </template>
  </section>
</template>
