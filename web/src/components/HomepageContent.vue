<script setup lang="ts">
import { computed } from 'vue'
import type { HomepageSettings } from '../accountTypes'
import { t } from '../i18n'
const props = defineProps<{ homepage: HomepageSettings; name: string; tagline: string; notice: string; noticeHtml: string; preview?: boolean }>()
const emit = defineEmits<{ navigate: [event: MouseEvent, href: string] }>()
const background = computed(() => props.homepage.background?.image || props.homepage.background?.imageUrl || '')
const style = computed(() => ({ '--homepage-image': background.value ? `url("${background.value}")` : 'none', '--homepage-overlay': `${props.homepage.background?.overlay ?? 65}%` }))
function follow(event: MouseEvent, href: string) { if (props.preview) event.preventDefault(); else emit('navigate', event, href) }
</script>
<template>
  <div class="homepage-surface" :class="{ 'has-background': !!background, 'is-preview': preview }" :style="style">
    <div class="homepage-content">
      <template v-for="block in homepage.blocks.filter(item => item.enabled)" :key="block.id">
        <section v-if="block.kind === 'intro'" class="homepage-intro">
          <div class="homepage-intro-mark" aria-hidden="true"><svg viewBox="0 0 40 40" fill="none"><path d="m20 3 15 9v17l-15 9-15-9V12zM5 12l15 9 15-9M20 21v17M12 8l16 9v16M28 8l-16 9v16" stroke="currentColor" stroke-width="1.4"/></svg></div>
          <h1>{{ t(block.title || name) }}</h1><p class="homepage-lead">{{ t(block.body || tagline) || t('每一个像素，都是你的模样。') }}</p>
          <div class="homepage-actions"><a v-if="block.buttonLabel && block.buttonHref" class="primary-button" :href="block.buttonHref" @click="follow($event, block.buttonHref)">{{ t(block.buttonLabel) }}</a><a v-if="block.buttonHref !== '/library'" class="homepage-library-link" href="/library" @click="follow($event, '/library')">{{ t('探索公开库') }}<span aria-hidden="true">↗</span></a></div>
        </section>
        <section v-else-if="block.kind === 'features'" class="homepage-features"><header v-if="block.title || block.body"><h2>{{ t(block.title) }}</h2><p v-if="block.body">{{ t(block.body) }}</p></header><div class="homepage-features-grid"><article v-for="(feature, index) in block.features || []" :key="index" class="homepage-feature"><span class="homepage-feature-icon" aria-hidden="true">{{ feature.icon || ['◇','▧','↗'][index % 3] }}</span><h3>{{ t(feature.title) }}</h3><p>{{ t(feature.body) }}</p><a v-if="feature.href" :href="feature.href" @click="follow($event, feature.href)"><span class="sr-only">{{ t(feature.title) }} </span>{{ t('查看') }}<span aria-hidden="true">↗</span></a></article></div></section>
        <section v-else-if="block.kind === 'notice' && notice" class="homepage-notice"><h2>{{ t(block.title || '最新公告') }}</h2><div class="notice-content" v-html="noticeHtml"></div></section>
        <section v-else-if="block.kind === 'cta'" class="homepage-cta"><div><h2>{{ t(block.title) }}</h2><p v-if="block.body">{{ t(block.body) }}</p></div><a v-if="block.buttonHref" class="secondary-button" :href="block.buttonHref" @click="follow($event, block.buttonHref)">{{ t(block.buttonLabel || '继续') }}</a></section>
      </template>
      <p v-if="!homepage.blocks.some(block => block.enabled)" class="homepage-lead">{{ tagline || t('每一个像素，都是你的模样。') }}</p>
    </div>
  </div>
</template>
