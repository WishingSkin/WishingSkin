<script setup lang="ts">
import { t } from '../i18n'
const emit = defineEmits<{ navigate: [path: string] }>()
</script>
<template>
  <section class="not-found-page" aria-labelledby="not-found-title"><p class="not-found-code">404</p><h1 id="not-found-title">{{ t('页面不存在') }}</h1><p>{{ t('你访问的页面不存在或已移动，请检查链接或返回首页。') }}</p><a class="primary-button" href="/" @click.prevent="emit('navigate', '/')">{{ t('返回主页') }}</a></section>
</template>
