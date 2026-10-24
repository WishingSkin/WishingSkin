<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
import InvitationPlugin from './InvitationPlugin.vue'
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ updated: []; expired: [] }>()
const selected = ref(false)
const enabled = ref<boolean | null>(null)
async function load() {
  try {
    const response = await fetch('/api/admin/plugins/invitations', { credentials: 'same-origin' })
    if (response.status === 401) { emit('expired'); return }
    if (response.ok) enabled.value = (await response.json()).enabled
  } catch { enabled.value = null }
}
function updated() { void load(); emit('updated') }
onMounted(load)
</script>
<template>
  <section class="plugin-workspace" :aria-label="t('官方插件区域')">
    <header class="plugin-workspace-heading"><div><h2>{{ t('官方插件') }}</h2><p>{{ t('随程序发行的官方模块，可独立启停与配置。') }}</p></div><span class="plugin-origin">WishingSkin</span></header>
    <div class="plugin-catalog"><article class="plugin-card panel" :class="{ selected }"><div class="plugin-card-heading"><span class="plugin-symbol" aria-hidden="true">＋</span><div><h3>{{ t('邀请码') }}</h3><span class="plugin-origin">{{ t('官方模块') }}</span></div><span v-if="enabled !== null" class="plugin-status">{{ enabled ? t('已启用') : t('已停用') }}</span></div><p>{{ t('为邮箱验证注册增加可选或必填的邀请码，支持批量生成和导出。') }}</p><button class="secondary-button" type="button" :aria-expanded="selected" aria-controls="invitation-plugin-settings" @click="selected = !selected">{{ selected ? t('收起设置') : t('配置插件') }}</button></article></div>
    <InvitationPlugin v-if="selected" id="invitation-plugin-settings" class="plugin-settings panel" :csrf-token="props.csrfToken" @expired="emit('expired')" @updated="updated" />
  </section>
</template>
