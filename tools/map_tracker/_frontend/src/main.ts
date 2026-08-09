import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { i18n } from './i18n'
import router from './router'
import 'element-plus/theme-chalk/dark/css-vars.css'
import './style.css'

createApp(App).use(createPinia()).use(i18n).use(router).mount('#app')
