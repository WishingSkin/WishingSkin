<script setup lang="ts">
defineProps<{ size?: number }>()
</script>
<template>
  <svg
    class="brand-mark-svg"
    :width="size || 30"
    :height="size || 30"
    viewBox="0 0 64 64"
    fill="none"
    aria-hidden="true"
  >
    <path class="bm-shell" d="M32 6L55 19.2V45.6L32 58.8L9 45.6V19.2L32 6Z" />
    <path class="bm-top" d="M32 9L52.2 20.6L32 32.2L11.8 20.6L32 9Z" />
    <path class="bm-left" d="M11.8 22.8L30.8 33.8V55.2L11.8 44.2V22.8Z" />
    <path class="bm-right" d="M52.2 22.8L33.2 33.8V55.2L52.2 44.2V22.8Z" />
    <path class="bm-sheen" d="M52.2 22.8L33.2 33.8V42.4L52.2 31.4V22.8Z" />
    <path class="bm-star" d="M32 13.4L34.7 18.6L41.6 20.6L34.7 22.6L32 27.8L29.3 22.6L22.4 20.6L29.3 18.6L32 13.4Z" />
    <rect class="bm-spark-a" x="52" y="7" width="4" height="4" rx="0.8" />
    <rect class="bm-spark-b" x="57" y="13" width="3" height="3" rx="0.6" />
  </svg>
</template>
