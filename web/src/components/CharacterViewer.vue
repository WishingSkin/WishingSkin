<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { SkinViewer } from 'skinview3d'
import { t } from '../i18n'
import { defaultPreviewSkin, disposePreview } from './previewModel'

const props = defineProps<{ name: string; model: string; skinHash: string; capeHash: string }>()
const emit = defineEmits<{ fallback: [] }>()
const stage = ref<HTMLDivElement>()
const canvas = ref<HTMLCanvasElement>()
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref('')
const attempt = ref(0)
const rotating = ref(false)
const reducedMotion = ref(false)
let viewer: SkinViewer | undefined
let controller: AbortController | undefined
let generation = 0
let mounted = false
let inView = true
let resizeObserver: ResizeObserver | undefined
let intersectionObserver: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined

function draw() {
  if (viewer && state.value === 'ready' && !document.hidden && inView) viewer.render()
}

function updateMotion() {
  if (!viewer) return
  viewer.autoRotate = rotating.value && !reducedMotion.value && !document.hidden && inView
  viewer.renderPaused = !viewer.autoRotate
  draw()
}

function stopRotation() {
  rotating.value = false
  updateMotion()
}

function resize() {
  if (!viewer || !stage.value) return
  viewer.pixelRatio = Math.min(window.devicePixelRatio || 1, 2)
  viewer.setSize(Math.max(1, stage.value.clientWidth), Math.max(1, stage.value.clientHeight))
  draw()
}

function releaseViewer() {
  if (!viewer) return
  const current = viewer
  viewer = undefined
  current.canvas.removeEventListener('webglcontextlost', contextLost)
  current.controls.removeEventListener('change', draw)
  current.autoRotate = false
  current.renderPaused = true
  disposePreview(current)
}

function fail(message: string) {
  state.value = 'error'
  error.value = message
  rotating.value = false
  controller?.abort()
  releaseViewer()
  emit('fallback')
}

function contextLost(event: Event) {
  event.preventDefault()
  generation++
  fail('3D 显示连接已中断。可以重试预览，或查看下方 PNG 材质。')
}

async function loadTexture(hash: string, label: string, signal: AbortSignal, bitmaps: ImageBitmap[]) {
  try {
    const response = await fetch(`/textures/${encodeURIComponent(hash)}`, { credentials: 'same-origin', signal })
    if (!response.ok) throw new Error('texture')
    const bitmap = await createImageBitmap(await response.blob())
    if (signal.aborted) {
      bitmap.close()
      throw new DOMException('Aborted', 'AbortError')
    }
    bitmaps.push(bitmap)
    return bitmap
  } catch {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    throw new Error(label === '皮肤' ? '皮肤材质无法加载。可以重试预览，或查看下方 PNG 材质。' : '披风材质无法加载。可以重试预览，或查看下方 PNG 材质。')
  }
}

async function initialize() {
  const run = ++generation
  controller?.abort()
  releaseViewer()
  rotating.value = false
  state.value = 'loading'
  error.value = ''
  attempt.value++
  await nextTick()
  if (!mounted || run !== generation || !canvas.value) return
  const request = new AbortController()
  controller = request
  const bitmaps: ImageBitmap[] = []
  let phase: 'dependency' | 'webgl' | 'texture' = 'dependency'
  try {
    if (props.model !== 'default' && props.model !== 'slim') {
      fail('此角色的模型类型暂不受支持。可查看下方 PNG 材质。')
      return
    }
    const { SkinViewer } = await import('skinview3d')
    if (!mounted || run !== generation) return
    phase = 'webgl'
    const current = new SkinViewer({ canvas: canvas.value, pixelRatio: Math.min(window.devicePixelRatio || 1, 2), renderPaused: true, zoom: 0.78 })
    viewer = current
    current.controls.enablePan = false
    current.controls.enableDamping = false
    current.controls.minDistance = 28
    current.controls.maxDistance = 110
    current.controls.addEventListener('change', draw)
    current.canvas.addEventListener('webglcontextlost', contextLost)
    resize()
    phase = 'texture'
    const [skin, cape] = await Promise.all([
      props.skinHash ? loadTexture(props.skinHash, '皮肤', request.signal, bitmaps) : Promise.resolve(defaultPreviewSkin(props.model)),
      props.capeHash ? loadTexture(props.capeHash, '披风', request.signal, bitmaps) : Promise.resolve(null),
    ])
    if (!mounted || run !== generation || request.signal.aborted) return
    current.loadSkin(skin, { model: props.model })
    if (!props.skinHash) current.playerObject.skin.setOuterLayerVisible(false)
    if (cape) current.loadCape(cape, { backEquipment: 'cape' })
    state.value = 'ready'
    await nextTick()
    if (!mounted || run !== generation) return
    setView('reset')
  } catch (cause) {
    if (!mounted || run !== generation || request.signal.aborted) return
    fail(phase === 'dependency' ? '3D 预览组件未能加载。请检查连接后重试，或查看下方 PNG 材质。'
      : phase === 'webgl' ? '当前浏览器无法开启 3D 预览。可以重试，或查看下方 PNG 材质。'
      : cause instanceof Error && cause.message.includes('材质无法加载') ? cause.message
      : '材质无法用于 3D 预览。请查看下方 PNG 文件，或重试预览。')
  } finally {
    for (const bitmap of bitmaps) bitmap.close()
  }
}

function setView(view: 'front' | 'back' | 'reset') {
  if (!viewer) return
  stopRotation()
  viewer.playerWrapper.rotation.y = 0
  viewer.zoom = 0.78
  viewer.resetCameraPose()
  if (view === 'back') viewer.camera.position.z *= -1
  if (view === 'reset') {
    const distance = viewer.camera.position.length()
    viewer.camera.position.set(distance * 0.36, distance * 0.12, distance * (props.capeHash && !props.skinHash ? -0.93 : 0.93)).setLength(distance)
  }
  viewer.controls.target.set(0, 0, 0)
  viewer.controls.update()
  draw()
}

function zoom(factor: number) {
  if (!viewer) return
  const distance = Math.min(viewer.controls.maxDistance, Math.max(viewer.controls.minDistance, viewer.camera.position.length() * factor))
  viewer.camera.position.setLength(distance)
  viewer.controls.update()
  draw()
}

function visibilityChanged() {
  if (document.hidden) stopRotation()
  else draw()
}

function motionChanged() {
  reducedMotion.value = motionQuery?.matches ?? false
  if (reducedMotion.value) stopRotation()
}

watch(rotating, updateMotion)
onMounted(() => {
  mounted = true
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  motionChanged()
  motionQuery.addEventListener('change', motionChanged)
  document.addEventListener('visibilitychange', visibilityChanged)
  resizeObserver = new ResizeObserver(resize)
  if (stage.value) {
    resizeObserver.observe(stage.value)
    intersectionObserver = new IntersectionObserver(entries => {
      inView = entries[0]?.isIntersecting ?? false
      if (!inView) stopRotation()
      else draw()
    })
    intersectionObserver.observe(stage.value)
  }
  void initialize()
})

onBeforeUnmount(() => {
  mounted = false
  generation++
  controller?.abort()
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
  motionQuery?.removeEventListener('change', motionChanged)
  document.removeEventListener('visibilitychange', visibilityChanged)
  releaseViewer()
})
</script>

<template>
  <section class="character-viewer" :aria-label="t('“{name}”的模型预览', { name })">
    <div class="appearance-heading"><h3>{{ t("角色外观") }}</h3><span>{{ t("3D 预览") }}</span></div>
    <div ref="stage" class="viewer-stage" :class="{ 'viewer-unavailable': state === 'error' }" :aria-busy="state === 'loading'">
      <canvas :key="attempt" ref="canvas" class="character-canvas" :class="{ 'canvas-ready': state === 'ready' }" role="img" :aria-label="t('“{name}”的三维角色外观，可使用下方按钮调整视角', { name })"></canvas>
      <div v-if="state === 'loading'" class="viewer-message" role="status"><span class="loading-dot"></span><p>{{ t("正在穿戴角色材质…") }}</p></div>
      <div v-else-if="state === 'error'" class="viewer-message viewer-error" role="alert"><span class="viewer-error-symbol" aria-hidden="true">◇</span><p>{{ t(error) }}</p><button class="secondary-button" type="button" @click="initialize">{{ t("重试 3D 预览") }}</button></div>
      <span v-if="state === 'ready'" class="viewer-interaction-hint">{{ t("拖动旋转 · 滚轮或双指缩放") }}</span>
    </div>
    <div class="viewer-controls" role="group" :aria-label="t('外观视角控制')">
      <div class="view-buttons"><button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="setView('front')">{{ t("正面") }}</button><button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="setView('back')">{{ t("背面") }}</button><button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="setView('reset')">{{ t("重置视角") }}</button><button class="secondary-button zoom-button" type="button" :aria-label="t('放大角色')" :disabled="state !== 'ready'" @click="zoom(0.85)">＋</button><button class="secondary-button zoom-button" type="button" :aria-label="t('缩小角色')" :disabled="state !== 'ready'" @click="zoom(1.15)">−</button></div>
      <button class="text-button rotation-button" type="button" :aria-pressed="rotating" :disabled="state !== 'ready' || reducedMotion" @click="rotating = !rotating">{{ rotating ? t('停止旋转') : t('自动旋转') }}</button>
    </div>
    <p v-if="state === 'ready' && !skinHash" class="viewer-note">{{ t(capeHash ? '未设置皮肤，使用 Steve 风格默认皮肤展示披风。' : '未设置皮肤，当前显示 Steve 风格默认皮肤。') }}</p>
    <p v-if="reducedMotion" class="viewer-note">{{ t("已启用减少动态效果，自动旋转已关闭。") }}</p>
  </section>
</template>

<style scoped>
.character-viewer{margin-top:24px}.appearance-heading{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}.appearance-heading h3{font-size:13px;font-weight:600}.appearance-heading>span{font-size:11px;color:var(--muted)}.viewer-stage{height:420px;position:relative;overflow:hidden;border:1px solid var(--border);border-radius:7px;background:radial-gradient(ellipse at 50% 85%,var(--accent-soft),transparent 62%),var(--subtle)}.viewer-stage::after{content:"";position:absolute;left:22%;right:22%;bottom:36px;height:1px;background:var(--border);pointer-events:none}.character-canvas{display:block;visibility:hidden;touch-action:none;cursor:grab;max-width:100%}.character-canvas:active{cursor:grabbing}.canvas-ready{visibility:visible}.viewer-message{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:15px;padding:30px;text-align:center;color:var(--muted);font-size:13px;line-height:1.9}.viewer-error{max-width:420px;margin:auto}.viewer-error-symbol{font-size:32px;color:var(--muted)}.viewer-message .secondary-button{margin-top:4px}.viewer-interaction-hint{position:absolute;bottom:14px;left:12px;right:12px;text-align:center;font-size:11px;color:var(--muted);pointer-events:none}.viewer-controls{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:14px;padding-top:13px}.view-buttons{display:flex;gap:7px;flex-wrap:wrap}.view-buttons .secondary-button{font-size:11px;min-height:33px;padding:7px 11px}.view-buttons .zoom-button{font-size:16px;width:33px;padding:4px}.rotation-button{font-size:11px;color:var(--accent);text-decoration:none;min-height:33px}.rotation-button[aria-pressed=true]{font-weight:600}.rotation-button:disabled{color:var(--muted);cursor:default}.viewer-note{font-size:11px;line-height:1.8;margin-top:12px;color:var(--muted)}
@media(max-width:1100px){.viewer-stage{height:370px}}
@media(max-width:560px){.viewer-stage{height:340px}.viewer-controls{gap:7px}.view-buttons{gap:5px}.view-buttons .secondary-button{padding:6px 9px}.view-buttons .zoom-button{width:30px}.viewer-interaction-hint{font-size:10px}.viewer-note{font-size:10px}.viewer-message{padding:22px;font-size:12px}}
</style>
