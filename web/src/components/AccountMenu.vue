<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { locale, setLocale, t } from '../i18n'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ user: { id: number; nickname: string; email: string } | null; avatarVersion: number; dark: boolean; logoutBusy: boolean }>()
const emit = defineEmits<{ navigate: [path: string]; theme: []; logout: [] }>()
const trigger = ref<HTMLButtonElement>()
const popover = ref<HTMLElement>()
const opened = ref(false)
const position = ref({ top: '0px', left: '0px' })
function place() {
  if (!trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const width = Math.min(288, innerWidth - 24)
  position.value = { top: rect.bottom + 12 + 'px', left: Math.max(12, Math.min(rect.right - width, innerWidth - width - 12)) + 'px' }
}
function toggle(event: Event) { opened.value = (event as ToggleEvent).newState === 'open'; if (opened.value) place() }
function open() { place(); popover.value?.togglePopover() }
function navigate(path: string) { popover.value?.hidePopover(); emit('navigate', path) }
function logout() { popover.value?.hidePopover(); emit('logout') }
watch(() => props.user?.id, () => popover.value?.hidePopover())
onMounted(() => { window.addEventListener('resize', place); window.addEventListener('scroll', place, true) })
onBeforeUnmount(() => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true) })
</script>
<template>
  <button ref="trigger" class="account-menu-trigger icon-button" type="button" :aria-label="t('打开账号菜单')" :title="user?.nickname || t('账号菜单')" aria-haspopup="dialog" :aria-expanded="opened" @click="open"><img v-if="user" class="user-avatar" :src="'/api/avatars/' + user.id + '.png?v=' + avatarVersion" alt="" width="34" height="34" /><AppIcon v-else name="account" /></button>
  <Teleport to="body"><section ref="popover" class="account-menu" popover="auto" role="dialog" :aria-label="t('账号菜单')" :style="position" @toggle="toggle">
    <div v-if="user" class="account-menu-identity"><strong>{{ user.nickname || t('我的账号') }}</strong><span>{{ user.email }}</span></div>
    <button class="account-menu-action" type="button" autofocus @click="navigate(user ? '/user/account' : '/login')"><AppIcon :name="user ? 'account' : 'login'" />{{ t(user ? '我的账号' : '登录') }}<AppIcon name="arrow-right" /></button>
    <div class="account-menu-preferences">
      <label class="account-menu-language"><span><AppIcon name="language" />{{ t('界面语言') }}</span><select :value="locale" :aria-label="t('界面语言')" @change="setLocale(($event.target as HTMLSelectElement).value)"><option value="zh-CN" lang="zh-CN">简体中文</option><option value="en" lang="en">English</option></select></label>
      <button class="account-menu-action" type="button" @click="emit('theme')"><AppIcon :name="dark ? 'sun' : 'moon'" />{{ t(dark ? '切换浅色主题' : '切换深色主题') }}</button>
    </div>
    <button v-if="user" class="account-menu-action account-menu-logout" type="button" :disabled="logoutBusy" @click="logout"><AppIcon name="logout" />{{ t(logoutBusy ? '正在退出…' : '退出登录') }}</button>
  </section></Teleport>
</template>
