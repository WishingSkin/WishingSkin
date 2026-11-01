<script setup lang="ts">
import { feedbackRef, confirmAction, toast } from '../feedback'
import AppModal from './AppModal.vue'
import { onMounted, ref } from 'vue'
import { t } from '../i18n'
const props=defineProps<{ csrfToken: string }>()
const emit=defineEmits<{ expired: [] }>()
type Client={ id: string; name: string; redirectUris: string[]; public: boolean; enabled: boolean }
const clients=ref<Client[]>([]), busy=ref(false), error = feedbackRef('error'), issued=ref('')
const draft=ref<Client>({ id:'',name:'',redirectUris:[],public:false,enabled:true }), redirects=ref('')
function edit(client?: Client) { issued.value=''; draft.value=client ? { ...client } : { id:'',name:'',redirectUris:[],public:false,enabled:true }; redirects.value=draft.value.redirectUris.join('\n') }
async function load() { try { const r=await fetch('/api/admin/oauth/clients'); if(r.status===401){emit('expired');return}; const data=await r.json(); if(!r.ok)throw new Error(data.error); clients.value=data.clients } catch(e){error.value=(e as Error).message} }
async function save(){if(busy.value)return;if(draft.value.id&&!await confirmAction({title:'修改应用',message:t('保存应用设置并撤销现有授权？')}))return;busy.value=true;error.value='';issued.value='';try{const r=await fetch('/api/admin/oauth/clients'+(draft.value.id?'/'+draft.value.id:''),{method:draft.value.id?'PATCH':'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':props.csrfToken},body:JSON.stringify({...draft.value,redirectUris:redirects.value.split('\n').map(s=>s.trim()).filter(Boolean)})});if(r.status===401){emit('expired');return};const data=await r.json();if(!r.ok)throw new Error(data.error);toast('应用设置已保存。');issued.value=data.clientSecret||'';draft.value=data.client;await load()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
onMounted(load)
</script>
<template><section class="settings-section"><h3>{{ t('OAuth2 授权服务') }}</h3><div class="settings-actions"><button v-for="client in clients" :key="client.id" class="secondary-button" type="button" @click="edit(client)">{{ client.name }}</button><button class="secondary-button" type="button" @click="edit()">{{ t('添加应用') }}</button></div><form class="settings-form" @submit.prevent="save"><fieldset :disabled="busy"><label class="binding-field"><span>{{ t('应用名称') }}</span><input v-model="draft.name" maxlength="128" required /></label><label class="binding-field"><span>{{ t('回调地址，每行一个') }}</span><textarea v-model="redirects" rows="3" required /></label><label class="checkbox-label"><input v-model="draft.public" type="checkbox" :disabled="!!draft.id" />{{ t('公开客户端（无密钥）') }}</label><label class="checkbox-label"><input v-model="draft.enabled" type="checkbox" />{{ t('启用应用') }}</label><p v-if="draft.id">Client ID: <code>{{ draft.id }}</code></p><button class="primary-button">{{ t('保存应用') }}</button></fieldset></form><AppModal v-if="issued" :title="t('应用密钥')" @close="issued=''"><div><p>{{ t('Client Secret 仅显示一次，请妥善保存。') }}</p><code class="totp-secret">{{ issued }}</code><button type="button" class="text-button" @click="issued=''">{{ t('我已保存') }}</button></div></AppModal></section></template>
