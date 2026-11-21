<script setup lang="ts">
import LoadingSkeleton from './LoadingSkeleton.vue'
import AppIcon from './AppIcon.vue'
import { toast } from '../feedback'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { SkinViewer } from 'skinview3d'
import { t } from '../i18n'
import { defaultPreviewSkin, disposePreview } from './previewModel'

const props = defineProps<{ name: string; model: string; skinHash: string; capeHash: string }>()
const emit = defineEmits<{ fallback: [] }>()
const stage = ref<HTMLDivElement>()
const canvas = ref<HTMLCanvasElement>()
const state = ref<'loading' | 'ready' | 'error'>('loading')
const attempt = ref(0)
const rotating = ref(false)
const pose = ref<'none' | 'idle' | 'walk' | 'run'>('none')
const outerLayer = ref(true)
const backEquipment = ref<'cape' | 'elytra'>('cape')
const reducedMotion = ref(false)
let viewer: SkinViewer | undefined
let controller: AbortController | undefined
let generation = 0
let mounted = false
let inView = true
let resizeObserver: ResizeObserver | undefined
let intersectionObserver: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined
let IdleAnimationClass: typeof import('skinview3d').IdleAnimation | undefined
let WalkingAnimationClass: typeof import('skinview3d').WalkingAnimation | undefined
let RunningAnimationClass: typeof import('skinview3d').RunningAnimation | undefined

function draw() {
  if (viewer && state.value === 'ready' && !document.hidden && inView) viewer.render()
}

function updateMotion() {
  if (!viewer) return
  viewer.autoRotate = rotating.value && !reducedMotion.value && !document.hidden && inView
  const activePose = !reducedMotion.value && !document.hidden && inView ? pose.value : 'none'
  if (activePose === 'idle' && IdleAnimationClass) {
    if (!(viewer.animation instanceof IdleAnimationClass)) viewer.animation = new IdleAnimationClass()
  } else if (activePose === 'walk' && WalkingAnimationClass) {
    if (!(viewer.animation instanceof WalkingAnimationClass)) viewer.animation = new WalkingAnimationClass()
  } else if (activePose === 'run' && RunningAnimationClass) {
    if (!(viewer.animation instanceof RunningAnimationClass)) viewer.animation = new RunningAnimationClass()
  } else {
    viewer.animation = null
    viewer.playerObject.resetJoints()
  }
  viewer.renderPaused = !(viewer.autoRotate || activePose !== 'none')
  draw()
}

function applyAppearanceOptions() {
  if (!viewer || state.value !== 'ready') return
  viewer.playerObject.skin.setOuterLayerVisible(outerLayer.value)
  if (props.capeHash) {
    viewer.playerObject.backEquipment = backEquipment.value
  }
  draw()
}

function exportSnapshot() {
  if (!viewer || !canvas.value || state.value !== 'ready') return
  const prevRatio = viewer.pixelRatio
  try {
    viewer.pixelRatio = Math.max(window.devicePixelRatio || 1, 2)
    viewer.render()
    const dataUrl = canvas.value.toDataURL('image/png')
    const link = document.createElement('a')
    const safeName = (props.name || 'character').replace(/[\\/:*?"<>|]+/g, '_').trim() || 'character'
    link.href = dataUrl
    link.download = `${safeName}-3d.png`
    link.click()
    toast('已导出 3D 立绘 PNG。')
  } catch {
    toast('导出立绘失败，请重试。', 'error')
  } finally {
    viewer.pixelRatio = prevRatio
    draw()
  }
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
	current.animation = null
  current.renderPaused = true
  disposePreview(current)
}

function fail(message: string) {
  state.value = 'error'
  toast(message, 'error')
  rotating.value = false
  pose.value = 'none'
  controller?.abort()
  releaseViewer()
  emit('fallback')
}

function contextLost(event: Event) {
  event.preventDefault()
  generation++
  fail('3D 显示连接已中断。可以重试预览，或查看下方 PNG 材质。')
}

async function loadTexture(url: string, label: string, signal: AbortSignal, bitmaps: ImageBitmap[]) {
  try {
    const response = await fetch(url, { credentials: 'same-origin', signal })
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
    const { SkinViewer, IdleAnimation, WalkingAnimation, RunningAnimation } = await import('skinview3d')
    IdleAnimationClass = IdleAnimation
    WalkingAnimationClass = WalkingAnimation
    RunningAnimationClass = RunningAnimation
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
      loadTexture(props.skinHash ? `/textures/${encodeURIComponent(props.skinHash)}` : defaultPreviewSkin(props.model), '皮肤', request.signal, bitmaps),
      props.capeHash ? loadTexture(`/textures/${encodeURIComponent(props.capeHash)}`, '披风', request.signal, bitmaps) : Promise.resolve(null),
    ])
    if (!mounted || run !== generation || request.signal.aborted) return
    current.loadSkin(skin, { model: props.model })
    current.playerObject.skin.setOuterLayerVisible(outerLayer.value)
    if (cape) current.loadCape(cape, { backEquipment: backEquipment.value })
    state.value = 'ready'
    await nextTick()
    if (!mounted || run !== generation) return
    setView('reset')
  } catch (cause) {
    if (!mounted || run !== generation || request.signal.aborted) return
    fail(phase === 'dependency' ? '3D 预览组件加载失败，请检查网络后重试，或直接查看下方 PNG 原图。'
      : phase === 'webgl' ? '当前浏览器暂不支持 WebGL 3D 预览，请查看下方 PNG 原图。'
      : cause instanceof Error && cause.message.includes('材质无法加载') ? cause.message
      : '该材质暂无法进行 3D 预览，请重试或查看下方 PNG 原图。')
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
	else updateMotion()
}

function motionChanged() {
  reducedMotion.value = motionQuery?.matches ?? false
  if (reducedMotion.value) stopRotation()
	else updateMotion()
}

watch(rotating, updateMotion)
watch(pose, updateMotion)
watch([outerLayer, backEquipment], applyAppearanceOptions)
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
      else updateMotion()
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
      <LoadingSkeleton v-if="state === 'loading'" class="viewer-message" layout="characters" label="正在穿戴角色材质…" :count="1" />
      <div v-else-if="state === 'error'" class="viewer-message viewer-error" role="alert"><AppIcon class="viewer-error-symbol" name="error" /><p>{{ t('模型预览不可用') }}</p><button class="secondary-button" type="button" @click="initialize">{{ t("重试 3D 预览") }}</button></div>
      <span v-if="state === 'ready'" class="viewer-interaction-hint">{{ t("拖动旋转，滚轮或双指缩放") }}</span>
    </div>
    <div class="viewer-controls" role="group" :aria-label="t('外观视角控制')">
      <div class="view-buttons">
        <button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="setView('front')">{{ t("正面") }}</button>
        <button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="setView('back')">{{ t("背面") }}</button>
        <button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="setView('reset')">{{ t("重置视角") }}</button>
        <button class="secondary-button zoom-button" type="button" :aria-label="t('放大角色')" :disabled="state !== 'ready'" @click="zoom(0.85)"><AppIcon name="plus" /></button>
        <button class="secondary-button zoom-button" type="button" :aria-label="t('缩小角色')" :disabled="state !== 'ready'" @click="zoom(1.15)"><AppIcon name="minus" /></button>
        <button class="secondary-button" type="button" :disabled="state !== 'ready'" @click="exportSnapshot">{{ t("导出立绘") }}</button>
      </div>
      <div class="viewer-motion-buttons">
        <button class="secondary-button rotation-button" type="button" :aria-pressed="outerLayer" :disabled="state !== 'ready'" @click="outerLayer = !outerLayer">{{ t('外层饰品') }}</button>
        <button v-if="capeHash" class="secondary-button rotation-button" type="button" :aria-pressed="backEquipment === 'elytra'" :disabled="state !== 'ready'" @click="backEquipment = backEquipment === 'cape' ? 'elytra' : 'cape'">{{ backEquipment === 'elytra' ? t('切换披风') : t('切换鞘翅') }}</button>
        <button class="secondary-button rotation-button" type="button" :aria-pressed="rotating" :disabled="state !== 'ready' || reducedMotion" @click="rotating = !rotating">{{ rotating ? t('停止旋转') : t('自动旋转') }}</button>
        <label class="viewer-pose-select">
          <span class="sr-only">{{ t('动作') }}</span>
          <select v-model="pose" :disabled="state !== 'ready' || reducedMotion" :aria-label="t('角色动作')">
            <option value="none">{{ t('静止') }}</option>
            <option value="idle">{{ t('待机') }}</option>
            <option value="walk">{{ t('行走') }}</option>
            <option value="run">{{ t('奔跑') }}</option>
          </select>
        </label>
      </div>
    </div>
    <p v-if="state === 'ready' && !skinHash" class="viewer-note">{{ t('默认皮肤：{name}', { name: model === 'slim' ? 'Alex' : 'Steve' }) }}</p>
    <p v-if="reducedMotion" class="viewer-note">{{ t("已启用减少动态效果。") }}</p>
  </section>
</template>

<style scoped>
.character-viewer{margin-top:24px}.appearance-heading{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}.appearance-heading h3{font-size:var(--font-body);font-weight:600}.appearance-heading>span{font-size:var(--font-small);color:var(--muted)}.viewer-stage{height:420px;position:relative;overflow:hidden;border:1px solid var(--border);border-radius:var(--radius-card);background:radial-gradient(ellipse at 50% 85%,var(--accent-soft),transparent 62%),var(--subtle)}.viewer-stage::after{content:"";position:absolute;left:22%;right:22%;bottom:36px;height:1px;background:var(--border);pointer-events:none}.character-canvas{display:block;visibility:hidden;touch-action:none;cursor:grab;max-width:100%}.character-canvas:active{cursor:grabbing}.canvas-ready{visibility:visible}.viewer-message{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:15px;padding:30px;text-align:center;color:var(--muted);font-size:var(--font-body);line-height:1.9}.viewer-error{max-width:420px;margin:auto}.viewer-error-symbol{font-size:var(--font-title);color:var(--muted)}.viewer-message .secondary-button{margin-top:4px}.viewer-interaction-hint{position:absolute;bottom:14px;left:12px;right:12px;text-align:center;font-size:var(--font-small);color:var(--muted);pointer-events:none}.viewer-motion-buttons{display:flex;gap:8px;flex-wrap:wrap;align-items:center}.viewer-pose-select select{min-height:33px;padding:6px 10px;border:1px solid var(--border);border-radius:var(--radius-control);background:var(--panel);color:var(--ink);font:inherit;font-size:var(--font-small)}.viewer-pose-select select:disabled{color:var(--muted);opacity:.65}.viewer-controls{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:14px;padding-top:13px}.view-buttons{display:flex;gap:7px;flex-wrap:wrap}.view-buttons .secondary-button{font-size:var(--font-small);min-height:33px;padding:7px 11px}.view-buttons .zoom-button{font-size:var(--font-body);width:33px;padding:4px}.rotation-button{font-size:var(--font-small);min-height:33px;padding:7px 11px}.rotation-button[aria-pressed=true]{border-color:var(--accent);background:var(--accent-soft);color:var(--accent)}.rotation-button:disabled{color:var(--muted);cursor:default}.viewer-note{font-size:var(--font-small);line-height:1.8;margin-top:12px;color:var(--muted)}
@media(max-width:1100px){.viewer-stage{height:370px}}
@media(max-width:560px){.viewer-stage{height:340px}.viewer-controls{gap:7px}.view-buttons{gap:5px}.view-buttons .secondary-button{padding:6px 9px}.view-buttons .zoom-button{width:30px}.viewer-interaction-hint{font-size:var(--font-small)}.viewer-note{font-size:var(--font-small)}.viewer-message{padding:22px;font-size:var(--font-small)}}
</style>
