<script setup lang="ts">
import { computed, ref } from 'vue'
import type { AdvancedAppearance } from '../accountTypes'
import { colorFields, iconFields } from '../interfaceAppearance'
import { english } from '../i18n.en'
import { locale, t } from '../i18n'
const draft = defineModel<AdvancedAppearance>({ required: true })
const palette = ref<'light' | 'dark'>('light')
const copyLocale = ref<'zh-CN' | 'en'>(locale.value)
const copies = computed(() => draft.value.localizedCopy[copyLocale.value])
const search = ref('')
const limit = ref(30)
const sources = computed(() => [...new Set([...Object.keys(english), ...Object.keys(copies.value)])].filter(source => [source, english[source] || '', copies.value[source] || ''].some(text => text.toLowerCase().includes(search.value.toLowerCase()))).sort())
function original(source: string) { return copyLocale.value === 'en' ? english[source] || source : source }
function resetColor(key: string) { delete draft.value[palette.value][key] }
function resetCopy(key: string) { delete copies.value[key] }
</script>
<template>
  <details class="advanced-settings">
    <summary>{{ t('高级界面设置') }}</summary>
    <details class="advanced-group" open><summary>{{ t('界面颜色') }}</summary>
      <p class="password-rule">{{ t('自定义颜色覆盖所选主题包，清空后使用主题颜色。') }}</p>
      <div class="settings-tabs" role="group" :aria-label="t('颜色模式')"><button v-for="mode in (['light', 'dark'] as const)" :key="mode" class="secondary-button" :class="{ active: palette === mode }" type="button" :aria-pressed="palette === mode" @click="palette = mode">{{ t(mode === 'light' ? '浅色' : '深色') }}</button></div>
      <div class="appearance-color-grid"><label v-for="[key, label] in colorFields" :key="palette + key" class="binding-field"><span>{{ t(label) }}</span><div class="color-control"><input type="color" :value="draft[palette][key] || '#808080'" :aria-label="t(label)" @input="draft[palette][key] = ($event.target as HTMLInputElement).value" /><input v-model="draft[palette][key]" :aria-label="t(label) + ' HEX'" placeholder="#RRGGBB" pattern="#[0-9a-fA-F]{6}" maxlength="7" /><button class="text-button" type="button" :disabled="!draft[palette][key]" @click="resetColor(key)">{{ t('默认') }}</button></div></label></div>
    </details>
    <details class="advanced-group"><summary>{{ t('界面文案') }} <small>{{ Object.values(copies).filter(Boolean).length }}</small></summary>
      <label class="binding-field"><span>{{ t('文案语言') }}</span><select v-model="copyLocale" @change="limit = 30"><option value="zh-CN">简体中文</option><option value="en">English</option></select></label>
      <label class="binding-field"><span>{{ t('查找原文或自定义文案') }}</span><input v-model="search" type="search" @input="limit = 30" /></label>
      <p class="password-rule">{{ t('仅修改所选语言；留空使用该语言的默认文案。保留 {name} 等占位符。') }}</p>
      <div class="copy-editor"><label v-for="source in sources.slice(0, limit)" :key="copyLocale + source" class="binding-field"><span>{{ original(source) }}</span><div class="copy-control"><textarea v-model="copies[source]" rows="2" maxlength="1000" :placeholder="original(source)"></textarea><button class="text-button" type="button" :disabled="!copies[source]" @click="resetCopy(source)">{{ t('默认') }}</button></div></label></div>
      <button v-if="sources.length > limit" class="secondary-button" type="button" @click="limit += 30">{{ t('显示更多') }} ({{ sources.length - limit }})</button>
    </details>
    <details class="advanced-group"><summary>{{ t('特殊图标') }}</summary><p class="password-rule">{{ t('此处填写短文本符号。首页主标志和功能卡图片在下方首页区块中设置。') }}</p><div class="appearance-color-grid"><label v-for="[key, label] in iconFields" :key="key" class="binding-field"><span>{{ t(label) }}</span><input v-model="draft.icons[key]" maxlength="8" :placeholder="t('默认')" /></label></div></details>
  </details>
</template>
