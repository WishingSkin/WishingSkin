<script setup lang="ts">
import { computed } from 'vue'
import type { PublicThemeAssets } from '../accountTypes'
import { themePackById } from '../themePacks'
import { resolveTextureUrl } from '../themeTextures'

const props = defineProps<{
  pack: string
  dark: boolean
  enabled: boolean
  assets?: PublicThemeAssets | null
}>()

const profile = computed(() => {
  if (!props.enabled) return null
  const meta = themePackById[props.pack]
  if (!meta?.particles) return null
  return props.dark ? meta.particles.dark || null : meta.particles.light || null
})

// Edge-framed low-density layout so particles stay atmospheric and clear of center Hero copy.
const tracks = [
  { left: '4%', top: '18%', dur: '14s', delay: '-2.2s', drift: '36px' },
  { left: '12%', top: '62%', dur: '16.5s', delay: '-7.5s', drift: '44px' },
  { left: '84%', top: '26%', dur: '15s', delay: '-4.1s', drift: '-34px' },
  { left: '92%', top: '68%', dur: '17.2s', delay: '-10.3s', drift: '-42px' },
  { left: '20%', top: '36%', dur: '15.8s', delay: '-1.4s', drift: '38px' },
  { left: '78%', top: '54%', dur: '16s', delay: '-8.6s', drift: '-38px' },
  { left: '8%', top: '78%', dur: '18s', delay: '-5.0s', drift: '32px' },
]

const items = computed(() => {
  const current = profile.value
  if (!current || !current.keys.length) return []
  const count = Math.min(Math.max(4, Math.round(current.count * 0.72)), tracks.length)
  return Array.from({ length: count }, (_, idx) => {
    const key = current.keys[idx % current.keys.length]
    const track = tracks[idx]
    const scale = idx % 3 === 0 ? 1.15 : idx % 3 === 1 ? 0.92 : 1
    return {
      key: `${props.pack}:${props.dark}:${idx}:${key}`,
      src: resolveTextureUrl(key, props.assets),
      motion: current.motion,
      size: Math.round(current.sizePx * scale) + 'px',
      left: track.left,
      top: track.top,
      dur: track.dur,
      delay: track.delay,
      drift: track.drift,
    }
  })
})
</script>
<template>
  <div v-if="items.length" class="biome-particles" :class="'motion-' + items[0].motion" aria-hidden="true">
    <img
      v-for="item in items"
      :key="item.key"
      class="biome-particle"
      :src="item.src"
      alt=""
      decoding="async"
      :style="{
        width: item.size,
        height: item.size,
        left: item.left,
        '--p-top': item.top,
        '--p-dur': item.dur,
        '--p-delay': item.delay,
        '--p-drift': item.drift,
      }"
    />
  </div>
</template>
