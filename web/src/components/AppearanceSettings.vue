<script setup lang="ts">
import { feedbackRef, confirmAction } from '../feedback'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { Appearance, PublicThemeAssets, PublicThemeFont } from '../accountTypes'
import { t } from '../i18n'
import { imageData } from './imageInput'
import { cloneAdvanced } from '../interfaceAppearance'
import { settingsSnapshot } from '../settingsDraft'
import { defaultThemeMeta, fontPresets, fontScopes, themePackById, themePackGroups } from '../themePacks'
import { resolveTextureUrl } from '../themeTextures'
import BrandMark from './BrandMark.vue'

const props = defineProps<{
  appearance: Appearance
  themeAssets?: PublicThemeAssets
  themeFont?: PublicThemeFont
  csrfToken: string
  managed?: boolean
}>()
const emit = defineEmits<{ updated: []; expired: [] }>()

function cloneDraft() {
  return {
    ...props.appearance,
    themePack: props.appearance.themePack || 'default',
    surfaceEffect: props.appearance.surfaceEffect || 'solid',
    surfaceOpacity: props.appearance.surfaceOpacity || 0,
    surfaceBlur: props.appearance.surfaceBlur || 0,
    allowUserTheme: Boolean(props.appearance.allowUserTheme),
    disableParticles: Boolean(props.appearance.disableParticles),
    fontPreset: props.appearance.fontPreset || 'system',
    fontScope: props.appearance.fontScope || 'headings',
    customFontUrl: props.appearance.customFontUrl || '',
    customFontFamily: props.appearance.customFontFamily || '',
    advanced: cloneAdvanced(props.appearance.advanced),
  }
}

const draft = ref(cloneDraft())
const saved = ref(settingsSnapshot(draft.value))
const dirty = computed(() => settingsSnapshot(draft.value) !== saved.value)
watch(() => props.appearance, () => {
  if (!dirty.value) {
    draft.value = cloneDraft()
    saved.value = settingsSnapshot(draft.value)
  }
})

const busy = ref(false)
const imageBusy = ref(false)
const assetsBusy = ref(false)
const fontBusy = ref(false)
const assetInput = ref<HTMLInputElement | null>(null)
const fontInput = ref<HTMLInputElement | null>(null)
const error = feedbackRef('error')
const message = feedbackRef('success')

const currentPackMeta = computed(() => themePackById[draft.value.themePack] || defaultThemeMeta)
const previewTileKeys = computed(() => {
  const keys = currentPackMeta.value.textures?.previewKeys
  if (keys?.length) return keys
  return ['plains_grass_side', 'deepslate_diamond_ore', 'ice_crystal', 'moss_block', 'chiseled_sandstone', 'cherry_leaves', 'crimson_nylium_side', 'sculk']
})

async function pick(event: Event) {
  imageBusy.value = true; error.value = ''; message.value = ''
  try { const value = await imageData(event); if (value) draft.value.logoImage = value }
  catch (e) { error.value = (e as Error).message }
  finally { imageBusy.value = false }
}

async function uploadAssets(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || assetsBusy.value) return
  assetsBusy.value = true; error.value = ''; message.value = ''
  try {
    const form = new FormData()
    form.set('file', file)
    const response = await fetch('/api/admin/theme-assets', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
      body: form,
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({}))
    if (!response.ok) { error.value = data.error || '导入 Minecraft 材质资源失败'; return }
    message.value = t('已导入 {count} 张 Minecraft 方块贴图。', { count: data.count ?? 0 })
    emit('updated')
  } catch {
    error.value = '上传过程中发生网络错误，请检查连接后重试。'
  } finally {
    assetsBusy.value = false
    if (assetInput.value) assetInput.value.value = ''
  }
}

async function clearAssets() {
  if (assetsBusy.value) return
  if (!await confirmAction({ title: '清除已导入贴图', message: t('清除已导入的 Minecraft 方块贴图并恢复内置像素纹理？'), danger: true })) return
  assetsBusy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/theme-assets', {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { error.value = '暂时无法清除已导入贴图'; return }
    message.value = '已恢复内置像素纹理。'
    emit('updated')
  } catch {
    error.value = '无法确认清除结果，请稍后重试。'
  } finally {
    assetsBusy.value = false
  }
}

async function uploadFont(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || fontBusy.value) return
  fontBusy.value = true; error.value = ''; message.value = ''
  try {
    const form = new FormData()
    form.set('file', file)
    const response = await fetch('/api/admin/theme-font', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
      body: form,
    })
    if (response.status === 401) { emit('expired'); return }
    const data = await response.json().catch(() => ({}))
    if (!response.ok) { error.value = data.error || '上传自定义字体失败'; return }
    message.value = '自定义字体已上传。'
    emit('updated')
  } catch {
    error.value = '字体上传失败，请检查网络后重试。'
  } finally {
    fontBusy.value = false
    if (fontInput.value) fontInput.value.value = ''
  }
}

async function clearFont() {
  if (fontBusy.value) return
  if (!await confirmAction({ title: '移除已上传字体', message: t('移除已上传的自定义 WOFF2 字体文件？'), danger: true })) return
  fontBusy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/theme-font', {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': props.csrfToken },
    })
    if (response.status === 401) { emit('expired'); return }
    if (!response.ok) { error.value = '暂时无法移除自定义字体'; return }
    message.value = '已移除上传的自定义字体。'
    emit('updated')
  } catch {
    error.value = '无法确认操作结果，请稍后重试。'
  } finally {
    fontBusy.value = false
  }
}

async function save() {
  if (busy.value || imageBusy.value || assetsBusy.value || fontBusy.value) return false
  busy.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/admin/appearance', {
      method: 'PATCH',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': props.csrfToken },
      body: JSON.stringify(draft.value),
    })
    if (response.status === 401) { emit('expired'); return false }
    const data = await response.json()
    if (!response.ok) { error.value = data.error || '暂时无法保存站点设置'; return false }
    draft.value = {
      ...data,
      themePack: data.themePack || 'default',
      surfaceEffect: data.surfaceEffect || 'solid',
      surfaceOpacity: data.surfaceOpacity || 0,
      surfaceBlur: data.surfaceBlur || 0,
      allowUserTheme: Boolean(data.allowUserTheme),
      disableParticles: Boolean(data.disableParticles),
      fontPreset: data.fontPreset || 'system',
      fontScope: data.fontScope || 'headings',
      customFontUrl: data.customFontUrl || '',
      customFontFamily: data.customFontFamily || '',
      advanced: cloneAdvanced(data.advanced),
    }
    saved.value = settingsSnapshot(draft.value)
    if (!props.managed) { message.value = '个性化设置已保存。'; emit('updated') }
    return true
  } catch {
    error.value = '无法确认操作结果，请重新检查设置。'
    return false
  } finally {
    busy.value = false
  }
}

const defaultOpacity = computed(() => draft.value.surfaceEffect === 'glass' ? 72 : 86)
const activeOpacity = computed(() => draft.value.surfaceOpacity || defaultOpacity.value)
const activeBlur = computed(() => draft.value.surfaceBlur || 16)

function applySurfacePreview(effect?: string, opacity?: number, blur?: number) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const surface = effect || 'solid'
  root.dataset.surface = surface
  if ((surface === 'translucent' || surface === 'glass') && opacity && opacity >= 35 && opacity <= 98) {
    root.style.setProperty('--surface-opacity', `${opacity}%`)
  } else {
    root.style.removeProperty('--surface-opacity')
  }
  if (surface === 'glass' && blur && blur >= 4 && blur <= 36) {
    root.style.setProperty('--surface-blur', `${blur}px`)
  } else {
    root.style.removeProperty('--surface-blur')
  }
}

watch(
  [() => draft.value.surfaceEffect, () => draft.value.surfaceOpacity, () => draft.value.surfaceBlur],
  ([effect, opacity, blur]) => {
    if (dirty.value) applySurfacePreview(effect, opacity, blur)
    else applySurfacePreview(props.appearance.surfaceEffect, props.appearance.surfaceOpacity, props.appearance.surfaceBlur)
  },
)

onBeforeUnmount(() => {
  applySurfacePreview(props.appearance.surfaceEffect, props.appearance.surfaceOpacity, props.appearance.surfaceBlur)
})

defineExpose({ draft, dirty, busy: computed(() => busy.value || imageBusy.value || assetsBusy.value || fontBusy.value), save })
</script>
<template>
  <section class="settings-section">
    <h3>{{ t('个性化') }}</h3>
    <form class="settings-form" style="max-width:none" @submit.prevent="!managed && save()">
      <fieldset :disabled="busy || imageBusy">
        <div class="image-setting">
          <img v-if="draft.logoImage" :src="draft.logoImage" :alt="t('Logo 预览')" width="96" height="96" />
          <span v-else class="image-placeholder large"><BrandMark :size="52" /></span>
          <div>
            <label class="binding-field"><span>{{ t('站点 Logo') }}</span><input type="file" accept="image/png,image/jpeg" @change="pick" /></label>
            <button class="text-button" type="button" :disabled="!draft.logoImage" @click="draft.logoImage = ''">{{ t('恢复默认标识') }}</button>
          </div>
        </div>
        <p class="password-rule">{{ t('PNG 或 JPEG，最大 512 KiB、2048×2048；保存为最长 512 px 的正方形 PNG。') }}</p>

        <div class="theme-pack-picker" role="radiogroup" :aria-label="t('主题包')">
          <div class="theme-pack-group">
            <h4>{{ t('默认') }}</h4>
            <div class="theme-pack-grid">
              <button
                type="button"
                class="theme-pack-card"
                :class="{ selected: draft.themePack === 'default' }"
                role="radio"
                :aria-checked="draft.themePack === 'default'"
                @click="draft.themePack = 'default'"
              >
                <span class="theme-pack-swatches" aria-hidden="true">
                  <span class="theme-pack-swatch-half" :style="{ background: defaultThemeMeta.swatches[0] }"><span class="theme-pack-swatch-dot" :style="{ background: defaultThemeMeta.swatches[1] }"></span></span>
                  <span class="theme-pack-swatch-half" :style="{ background: defaultThemeMeta.swatches[2] }"><span class="theme-pack-swatch-dot" :style="{ background: defaultThemeMeta.swatches[3] }"></span></span>
                </span>
                <span class="theme-pack-head"><strong>{{ t(defaultThemeMeta.label) }}</strong></span>
                <span class="theme-pack-scenes">{{ t(defaultThemeMeta.lightScene) }}</span>
              </button>
            </div>
          </div>

          <div v-for="group in themePackGroups" :key="group.label" class="theme-pack-group">
            <h4>{{ t(group.label) }}</h4>
            <div class="theme-pack-grid">
              <button
                v-for="option in group.options"
                :key="option.value"
                type="button"
                class="theme-pack-card"
                :class="{ selected: draft.themePack === option.value }"
                role="radio"
                :aria-checked="draft.themePack === option.value"
                @click="draft.themePack = option.value"
              >
                <span class="theme-pack-swatches" aria-hidden="true">
                  <span class="theme-pack-swatch-half" :style="{ background: option.swatches[0] }"><span class="theme-pack-swatch-dot" :style="{ background: option.swatches[1] }"></span></span>
                  <span class="theme-pack-swatch-half" :style="{ background: option.swatches[2] }"><span class="theme-pack-swatch-dot" :style="{ background: option.swatches[3] }"></span></span>
                </span>
                <span class="theme-pack-head">
                  <strong>{{ t(option.label) }}</strong>
                  <span v-if="option.textures" class="theme-pack-tiles" aria-hidden="true">
                    <img
                      v-if="!option.darkOnly"
                      class="theme-pack-tile"
                      :src="resolveTextureUrl(option.textures.lightTrim, themeAssets)"
                      alt=""
                      width="20"
                      height="20"
                    />
                    <img
                      class="theme-pack-tile"
                      :src="resolveTextureUrl(option.textures.darkTrim, themeAssets)"
                      alt=""
                      width="20"
                      height="20"
                    />
                  </span>
                  <span v-else-if="option.darkOnly" class="theme-pack-badge">{{ t('仅深色') }}</span>
                </span>
                <span v-if="option.darkOnly && option.textures" class="theme-pack-badge theme-pack-badge-inline">{{ t('仅深色') }}</span>
                <span class="theme-pack-scenes">{{ option.darkOnly ? t(option.darkScene) : `${t(option.lightScene)} / ${t(option.darkScene)}` }}</span>
              </button>
            </div>
          </div>
        </div>

        <div style="max-width:650px">
          <label v-if="draft.themePack === 'default'" class="binding-field">
            <span>{{ t('主题配色') }}</span>
            <select v-model="draft.accent">
              <option value="blue">{{ t('蓝色') }}</option>
              <option value="green">{{ t('绿色') }}</option>
              <option value="violet">{{ t('紫色') }}</option>
              <option value="rose">{{ t('玫瑰') }}</option>
              <option value="orange">{{ t('橙色') }}</option>
              <option value="teal">{{ t('青色') }}</option>
              <option value="gray">{{ t('石墨') }}</option>
            </select>
          </label>
          <label class="binding-field">
            <span>{{ t('站点默认主题') }}</span>
            <select v-model="draft.defaultTheme" :disabled="currentPackMeta.darkOnly">
              <option value="system">{{ t('跟随系统') }}</option>
              <option value="light">{{ t('浅色') }}</option>
              <option value="dark">{{ t('深色') }}</option>
            </select>
          </label>
          <p v-if="currentPackMeta.darkOnly" class="password-rule">{{ t('深暗之域仅提供深色模式，访客端会自动隐藏明暗切换按钮。') }}</p>
          <label class="binding-field">
            <span>{{ t('面板效果') }}</span>
            <select v-model="draft.surfaceEffect">
              <option value="solid">{{ t('实色') }}</option>
              <option value="translucent">{{ t('半透明') }}</option>
              <option value="glass">{{ t('毛玻璃') }}</option>
            </select>
          </label>
          <div v-if="draft.surfaceEffect === 'translucent' || draft.surfaceEffect === 'glass'" class="surface-tuning">
            <label class="surface-slider-field">
              <span class="surface-slider-head">
                <span>{{ t('面板不透明度') }}</span>
                <strong>{{ activeOpacity }}%</strong>
              </span>
              <input
                type="range"
                min="35"
                max="98"
                step="1"
                :value="activeOpacity"
                @input="draft.surfaceOpacity = Number(($event.target as HTMLInputElement).value)"
              />
            </label>
            <label v-if="draft.surfaceEffect === 'glass'" class="surface-slider-field">
              <span class="surface-slider-head">
                <span>{{ t('毛玻璃模糊度') }}</span>
                <strong>{{ activeBlur }} px</strong>
              </span>
              <input
                type="range"
                min="4"
                max="36"
                step="1"
                :value="activeBlur"
                @input="draft.surfaceBlur = Number(($event.target as HTMLInputElement).value)"
              />
            </label>
            <button
              v-if="draft.surfaceOpacity || draft.surfaceBlur"
              class="text-button"
              type="button"
              @click="draft.surfaceOpacity = 0; draft.surfaceBlur = 0"
            >{{ t('恢复默认透明与模糊参数') }}</button>
          </div>

          <label class="settings-check">
            <input v-model="draft.allowUserTheme" type="checkbox" />
            <span>{{ t('允许用户在账号菜单中自行切换主题包与字体偏好') }}</span>
          </label>
          <label class="settings-check">
            <input :checked="!draft.disableParticles" type="checkbox" @change="draft.disableParticles = !($event.target as HTMLInputElement).checked" />
            <span>{{ t('启用 MC 主题原版像素氛围粒子动效') }}</span>
          </label>

          <h4>{{ t('界面字体') }}</h4>
          <label class="binding-field">
            <span>{{ t('字体预设') }}</span>
            <select v-model="draft.fontPreset">
              <option v-for="preset in fontPresets" :key="preset.value" :value="preset.value">{{ t(preset.label) }}</option>
            </select>
          </label>
          <label v-if="draft.fontPreset !== 'system'" class="binding-field">
            <span>{{ t('字体应用范围') }}</span>
            <select v-model="draft.fontScope">
              <option v-for="scope in fontScopes" :key="scope.value" :value="scope.value">{{ t(scope.label) }}</option>
            </select>
          </label>

          <div v-if="draft.fontPreset === 'custom'" class="theme-resource-box">
            <h4>{{ t('自定义字体来源') }}</h4>
            <p>{{ t('可上传不超过 2 MiB 的 WOFF2 字体文件，或填写外部 HTTPS / 站内字体地址（填写地址时优先使用地址）。') }}</p>
            <label class="binding-field">
              <span>{{ t('自定义字体地址（可选）') }}</span>
              <input v-model="draft.customFontUrl" maxlength="512" :placeholder="t('https://example.com/font.css 或 /fonts/custom.woff2')" />
            </label>
            <label v-if="draft.customFontUrl.trim()" class="binding-field">
              <span>{{ t('CSS 字体族名称（可选）') }}</span>
              <input v-model="draft.customFontFamily" maxlength="120" :placeholder="t('使用无 ?family= 参数的样式表时填写，如 Press Start 2P')" />
            </label>
            <p v-if="themeFont?.version">{{ t('已上传字体：{name}', { name: themeFont.fileName || 'custom.woff2' }) }}</p>
            <div class="theme-resource-actions">
              <label class="secondary-button" :aria-disabled="fontBusy">
                {{ fontBusy ? t('正在上传…') : t('上传 WOFF2 字体') }}
                <input ref="fontInput" type="file" accept=".woff2,font/woff2" class="sr-only" :disabled="fontBusy" @change="uploadFont" />
              </label>
              <button v-if="themeFont?.version" class="danger-button" type="button" :disabled="fontBusy" @click="clearFont">{{ t('移除已上传字体') }}</button>
            </div>
          </div>

          <div class="theme-resource-box">
            <h4>{{ t('Minecraft 方块材质资源') }}</h4>
            <p v-if="themeAssets?.count">
              {{ t('当前已导入 {source}（共 {count} 张方块贴图）。原始文件不会保留在服务器。', { source: themeAssets.sourceName || 'Minecraft pack', count: themeAssets.count }) }}
            </p>
            <p v-else>
              {{ t('当前使用内置像素纹理。上传 Minecraft 客户端 client.jar 或资源包 .zip 后，将自动提取原版方块与粒子贴图并按群系染色，原始文件不会留在服务器。') }}
            </p>
            <div class="theme-tile-strip" :aria-label="t('方块贴图预览')">
              <img
                v-for="key in previewTileKeys"
                :key="`${themeAssets?.version || 'builtin'}:${key}`"
                class="theme-tile-sample"
                :src="resolveTextureUrl(key, themeAssets)"
                :alt="key"
                :title="key"
                width="36"
                height="36"
              />
            </div>
            <div class="theme-resource-actions">
              <label class="secondary-button" :aria-disabled="assetsBusy">
                {{ assetsBusy ? t('正在提取贴图…') : t('导入 client.jar / 资源包 .zip') }}
                <input ref="assetInput" type="file" accept=".jar,.zip,application/java-archive,application/zip" class="sr-only" :disabled="assetsBusy" @change="uploadAssets" />
              </label>
              <button v-if="themeAssets?.count" class="danger-button" type="button" :disabled="assetsBusy" @click="clearAssets">{{ t('恢复内置像素纹理') }}</button>
            </div>
          </div>

          <button v-if="!managed" class="primary-button" type="submit">{{ busy ? t('正在保存…') : t('保存个性化设置') }}</button>
        </div>
      </fieldset>
    </form>
  </section>
</template>
