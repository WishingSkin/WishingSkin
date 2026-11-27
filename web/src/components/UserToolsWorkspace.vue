<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { t } from '../i18n'
import type { UserPluginEntry } from '../pluginModel'
import { userToolPath } from '../workspaceRoutes'
import AppIcon from './AppIcon.vue'
import InterfaceIcon from './InterfaceIcon.vue'
import LoadingSkeleton from './LoadingSkeleton.vue'
import NotFoundPage from './NotFoundPage.vue'
import PluginFrontend from './PluginFrontend.vue'
import PluginIcon from './PluginIcon.vue'

const props = defineProps<{
  csrfToken: string
  plugins: UserPluginEntry[]
  loading: boolean
  error: string
  selectedPluginId: string
  selectedFrontendId: string
}>()

const emit = defineEmits<{
  navigate: [path: string]
  refresh: []
  expired: []
  mutated: [kind: 'textures' | 'characters']
}>()

const tools = computed(() => props.plugins.flatMap(plugin =>
  plugin.frontends
    .filter(frontend => frontend.slot === 'user.tools')
    .map(frontend => ({ plugin, frontend })),
))

const availableTools = computed(() => tools.value.map(item => ({
  pluginId: item.plugin.id,
  frontendId: item.frontend.id,
  label: item.frontend.label,
})))

const activeTool = computed(() => {
  if (!props.selectedPluginId || !props.selectedFrontendId) return null
  return tools.value.find(item => item.plugin.id === props.selectedPluginId && item.frontend.id === props.selectedFrontendId) || null
})

const pendingHandoff = shallowRef<{ pluginId: string; frontendId: string; context: Record<string, unknown> } | null>(null)

const activeInitialContext = computed(() => {
  if (!activeTool.value || !pendingHandoff.value) return undefined
  if (
    pendingHandoff.value.pluginId !== activeTool.value.plugin.id ||
    pendingHandoff.value.frontendId !== activeTool.value.frontend.id
  ) {
    return undefined
  }
  return pendingHandoff.value.context
})

watch(() => [props.selectedPluginId, props.selectedFrontendId], ([pid, fid]) => {
  if (
    pendingHandoff.value &&
    (pendingHandoff.value.pluginId !== pid || pendingHandoff.value.frontendId !== fid)
  ) {
    pendingHandoff.value = null
  }
})

function handleOpenTool(req: { pluginId: string; frontendId: string; context?: Record<string, unknown> }) {
  const exists = tools.value.some(
    item => item.plugin.id === req.pluginId && item.frontend.id === req.frontendId,
  )
  if (!exists) return
  pendingHandoff.value = req.context
    ? { pluginId: req.pluginId, frontendId: req.frontendId, context: req.context }
    : null
  emit('navigate', userToolPath(req.pluginId, req.frontendId))
}

const missingSelection = computed(() =>
  !props.loading && !props.error && Boolean(props.selectedPluginId || props.selectedFrontendId) && !activeTool.value,
)
</script>

<template>
  <section class="tools-workspace" :aria-label="t('工具箱')">
    <NotFoundPage v-if="missingSelection" @navigate="emit('navigate', $event)" />
    <template v-else-if="activeTool">
      <div class="page-heading tool-workspace-header">
        <div>
          <button type="button" class="text-button" @click="emit('navigate', '/user/tools')">{{ t('返回工具箱') }}</button>
          <h1>{{ t(activeTool.frontend.label) }}</h1>
          <p v-if="activeTool.frontend.description">{{ t(activeTool.frontend.description) }}</p>
        </div>
        <span v-if="activeTool.plugin.signedOfficial" class="notice-current-badge">{{ t('官方扩展') }}</span>
      </div>
      <div data-panel-region="tools">
        <PluginFrontend
          :key="`${activeTool.plugin.id}:${activeTool.frontend.id}`"
          :plugin-id="activeTool.plugin.id"
          :frontend-id="activeTool.frontend.id"
          scope="user"
          tall
          enabled
          :version="activeTool.plugin.version"
          :csrf-token="props.csrfToken"
          :available-tools="availableTools"
          :initial-context="activeInitialContext"
          @expired="emit('expired')"
          @mutated="emit('mutated', $event)"
          @open-tool="handleOpenTool"
        />
      </div>
    </template>
    <template v-else>
      <div class="page-heading">
        <div>
          <h1>{{ t('工具箱') }}</h1>
          <p>{{ t('使用已启用扩展插件提供的皮肤编辑、渲染卡片与账号同步工具。') }}</p>
        </div>
        <button class="secondary-button" type="button" :disabled="loading" @click="emit('refresh')">{{ loading ? t('正在刷新…') : t('刷新列表') }}</button>
      </div>
      <div data-panel-region="tools">
        <LoadingSkeleton v-if="loading" layout="cards" label="正在加载工具…" :count="3" />
        <div v-else-if="error" class="empty-state panel" role="alert">
          <span class="empty-symbol"><AppIcon name="error" /></span>
          <h2>{{ t('加载失败') }}</h2>
          <p>{{ t(error) }}</p>
          <button class="secondary-button" type="button" @click="emit('refresh')">{{ t('重试') }}</button>
        </div>
        <div v-else-if="!tools.length" class="empty-state panel">
          <span class="empty-symbol"><InterfaceIcon name="tools" /></span>
          <h2>{{ t('暂无可用工具') }}</h2>
          <p>{{ t('管理员启用提供用户端工具的插件后，将在这里显示。') }}</p>
        </div>
        <div v-else class="tool-card-grid">
          <article v-for="item in tools" :key="`${item.plugin.id}:${item.frontend.id}`" class="tool-card panel">
            <div class="tool-card-heading">
              <span class="plugin-symbol"><PluginIcon :id="item.plugin.id" /></span>
              <div>
                <h2>{{ t(item.frontend.label) }}</h2>
                <span class="plugin-origin">{{ t(item.plugin.name || item.plugin.id) }} / v{{ item.plugin.version }}</span>
              </div>
              <span v-if="item.plugin.signedOfficial" class="notice-current-badge">{{ t('官方扩展') }}</span>
            </div>
            <p v-if="item.frontend.description || item.plugin.description">{{ t(item.frontend.description || item.plugin.description || '') }}</p>
            <div class="settings-actions">
              <button type="button" class="primary-button" @click="emit('navigate', userToolPath(item.plugin.id, item.frontend.id))">{{ t('打开工具') }}</button>
            </div>
          </article>
        </div>
      </div>
    </template>
  </section>
</template>
