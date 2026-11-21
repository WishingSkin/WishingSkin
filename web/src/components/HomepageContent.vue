<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { HomepageSettings, PublicThemeAssets } from '../accountTypes'
import type { LibraryRecord } from '../textureTypes'
import HomepageIcon from './HomepageIcon.vue'
import BiomeParticles from './BiomeParticles.vue'
import ModelThumbnail from './ModelThumbnail.vue'
import { t } from '../i18n'

const props = defineProps<{
  homepage: HomepageSettings
  name: string
  tagline: string
  notice: string
  noticeHtml: string
  preview?: boolean
  themePack?: string
  dark?: boolean
  mcBiome?: boolean
  particlesEnabled?: boolean
  themeAssets?: PublicThemeAssets
}>()
const emit = defineEmits<{ navigate: [event: MouseEvent, href: string] }>()
const background = computed(() => props.homepage.background?.image || props.homepage.background?.imageUrl || '')
const useScene = computed(() => Boolean(props.mcBiome && !background.value && !props.preview))
const scrollProgress = ref(0)
const showcaseFeeds = ref<Record<string, LibraryRecord[]>>({})

const showcaseSources = computed(() => {
  const unique = new Set<string>()
  for (const block of props.homepage.blocks) {
    if (block.enabled && block.kind === 'showcase' && block.source) {
      unique.add(block.source)
    }
  }
  return Array.from(unique)
})

async function loadShowcaseFeeds() {
  for (const source of showcaseSources.value) {
    if (showcaseFeeds.value[source]) continue
    try {
      const response = await fetch(`/api/library?sort=${encodeURIComponent(source)}&page=1&pageSize=6`)
      if (response.ok) {
        const data = await response.json() as { textures: LibraryRecord[] }
        showcaseFeeds.value = { ...showcaseFeeds.value, [source]: data.textures || [] }
      }
    } catch {
      // Keep homepage rendering resilient if library feed temporarily fails.
    }
  }
}

watch(showcaseSources, () => { void loadShowcaseFeeds() }, { immediate: true })

const style = computed(() => ({
  '--homepage-image': background.value ? `url("${background.value}")` : 'none',
  '--homepage-overlay': `${props.homepage.background?.overlay ?? 65}%`,
  '--scene-scroll': String(scrollProgress.value),
}))

let raf = 0
function updateScroll() {
  if (raf || !useScene.value) return
  raf = window.requestAnimationFrame(() => {
    raf = 0
    const maxScroll = Math.max(1, document.documentElement.scrollHeight - window.innerHeight)
    scrollProgress.value = Math.min(1, Math.max(0, window.scrollY / maxScroll))
  })
}

onMounted(() => {
  if (!props.preview) {
    updateScroll()
    window.addEventListener('scroll', updateScroll, { passive: true })
    window.addEventListener('resize', updateScroll, { passive: true })
  }
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', updateScroll)
  window.removeEventListener('resize', updateScroll)
  if (raf) window.cancelAnimationFrame(raf)
})

function follow(event: MouseEvent, href: string) {
  if (props.preview) event.preventDefault()
  else emit('navigate', event, href)
}
</script>
<template>
  <div class="homepage-surface" :class="{ 'has-background': !!background, 'is-preview': preview, 'mc-scene': useScene, 'mc-scene-dark': useScene && !!dark }" :style="style">
    <div v-if="useScene" class="homepage-scene-backdrop" aria-hidden="true">
      <div class="homepage-scene-stage">
        <div class="homepage-scene-layer scene-surface"></div>
        <div class="homepage-scene-layer scene-underground"></div>
        <div class="homepage-scene-veil"></div>
        <BiomeParticles v-if="themePack" :pack="themePack" :dark="!!dark" :enabled="!!particlesEnabled" :assets="themeAssets" />
      </div>
    </div>
    <div class="homepage-content">
      <template v-for="block in homepage.blocks.filter(item => item.enabled)" :key="block.id">
        <section v-if="block.kind === 'intro'" class="homepage-intro">
          <div class="homepage-intro-mark" aria-hidden="true"><HomepageIcon :image="block.iconImage" kind="intro" /></div>
          <h1>{{ t(block.title || name) }}</h1><p class="homepage-lead">{{ t(block.body || tagline) || t('每一个像素，都是你的模样。') }}</p>
          <div class="homepage-actions"><a v-if="block.buttonLabel && block.buttonHref" class="primary-button" :href="block.buttonHref" @click="follow($event, block.buttonHref)">{{ t(block.buttonLabel) }}</a><a v-if="block.buttonHref !== '/library'" class="homepage-library-link" href="/library" @click="follow($event, '/library')">{{ t('探索公开库') }}<AppIcon name="arrow" /></a></div>
        </section>
        <section v-else-if="block.kind === 'features'" class="homepage-features"><header v-if="block.title || block.body"><h2>{{ t(block.title) }}</h2><p v-if="block.body">{{ t(block.body) }}</p></header><div class="homepage-features-grid"><article v-for="(feature, index) in block.features || []" :key="index" class="homepage-feature"><span class="homepage-feature-icon" aria-hidden="true"><HomepageIcon :image="feature.iconImage" :text="feature.icon" :kind="index % 3 === 0 ? 'characters' : index % 3 === 1 ? 'library' : 'game'" /></span><h3>{{ t(feature.title) }}</h3><p>{{ t(feature.body) }}</p><a v-if="feature.href" :href="feature.href" @click="follow($event, feature.href)"><span class="sr-only">{{ t(feature.title) }} </span>{{ t('查看') }}<AppIcon name="arrow" /></a></article></div></section>
        <section v-else-if="block.kind === 'notice' && notice" class="homepage-notice"><h2>{{ t(block.title || '最新公告') }}</h2><div class="notice-content" v-html="noticeHtml"></div></section>
        <section v-else-if="block.kind === 'showcase' && block.source" class="homepage-showcase homepage-showcase-feed">
          <div class="homepage-showcase-header">
            <div>
              <h2 v-if="block.title">{{ t(block.title) }}</h2>
              <p v-if="block.body">{{ t(block.body) }}</p>
            </div>
            <a class="secondary-button" :href="block.buttonHref || '/library'" @click="follow($event, block.buttonHref || '/library')">{{ t(block.buttonLabel || '前往公开库') }}</a>
          </div>
          <div v-if="showcaseFeeds[block.source]?.length" class="homepage-showcase-grid">
            <a
              v-for="item in showcaseFeeds[block.source]"
              :key="item.id"
              class="homepage-showcase-item"
              href="/library"
              @click="follow($event, '/library')"
            >
              <ModelThumbnail
                class="homepage-showcase-thumb"
                :name="item.name"
                :model="item.kind === 'skin' ? item.model : 'default'"
                :skin-hash="item.kind === 'skin' ? item.fileHash : ''"
                :cape-hash="item.kind === 'cape' ? item.fileHash : ''"
              />
              <span class="homepage-showcase-item-meta">
                <strong>{{ item.name }}</strong>
                <small><AppIcon name="heart" />{{ item.likeCount ?? 0 }} / {{ t('{count} 收藏', { count: item.collectionCount }) }}</small>
              </span>
            </a>
          </div>
          <p v-else class="homepage-showcase-empty">{{ t('公开库暂时还没有可展示的材质。') }}</p>
        </section>
        <section v-else-if="block.kind === 'showcase'" class="homepage-showcase" :class="{ 'has-image': !!block.image }"><img v-if="block.image" :src="block.image" :alt="block.imageAlt || block.title" loading="lazy" /><div><h2 v-if="block.title">{{ t(block.title) }}</h2><p v-if="block.body">{{ t(block.body) }}</p><a v-if="block.buttonHref" class="secondary-button" :href="block.buttonHref" @click="follow($event, block.buttonHref)">{{ t(block.buttonLabel || '查看') }}</a></div></section>
        <section v-else-if="block.kind === 'cta'" class="homepage-cta"><div><h2>{{ t(block.title) }}</h2><p v-if="block.body">{{ t(block.body) }}</p></div><a v-if="block.buttonHref" class="secondary-button" :href="block.buttonHref" @click="follow($event, block.buttonHref)">{{ t(block.buttonLabel || '继续') }}</a></section>
      </template>
      <p v-if="!homepage.blocks.some(block => block.enabled)" class="homepage-lead">{{ tagline || t('每一个像素，都是你的模样。') }}</p>
    </div>
  </div>
</template>
