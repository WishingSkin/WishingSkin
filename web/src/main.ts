import { createApp } from 'vue'
import App from './App.vue'
import { toast } from './feedback'
import './tokens.css'
import './style.css'
import './themes.css'

document.addEventListener('invalid', event => {
  const field = event.target
  if (!(field instanceof HTMLInputElement || field instanceof HTMLSelectElement || field instanceof HTMLTextAreaElement)) return
  event.preventDefault()
  const first = field.form?.querySelector('input:invalid, select:invalid, textarea:invalid')
  if (first && first !== field) return
  toast(field.validationMessage, 'error')
  field.focus()
}, true)

createApp(App).mount('#app')
