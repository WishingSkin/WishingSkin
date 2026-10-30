<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
const props=defineProps<{ csrfToken: string }>()
const emit=defineEmits<{ expired: [] }>()
const grants=ref<{id:string;name:string;scope:string}[]>([]),error=ref(''),busy=ref('')
async function load(){try{const r=await fetch('/api/account/oauth');if(r.status===401){emit('expired');return};const d=await r.json();if(!r.ok)throw new Error(d.error);grants.value=d.grants}catch(e){error.value=(e as Error).message}}
async function revoke(id:string){busy.value=id;try{const r=await fetch('/api/account/oauth/'+id,{method:'DELETE',headers:{'X-CSRF-Token':props.csrfToken}});if(!r.ok)throw new Error();await load()}catch{error.value='暂时无法撤销授权'}finally{busy.value=''}}
onMounted(load)
</script>
<template><section class="account-password"><h3>{{ t('已授权的应用') }}</h3><p v-if="!grants.length">{{ t('暂无应用授权') }}</p><div v-for="grant in grants" :key="grant.id" class="settings-actions"><span>{{ grant.name }} / {{ grant.scope }}</span><button class="secondary-button" type="button" :disabled="!!busy" @click="revoke(grant.id)">{{ t('撤销授权') }}</button></div><p v-if="error" class="form-error" role="alert">{{ t(error) }}</p></section></template>
