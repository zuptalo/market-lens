import { createApp } from 'vue';
import PrimeVue from 'primevue/config';
import App from './App.vue';
import router from './router';
import { bindAuthConnectivity } from '@/composables/useAuth';
import { marketLensTheme, themeOptions } from '@/styles/theme';
import './styles/main.css';

bindAuthConnectivity();

createApp(App)
  .use(router)
  .use(PrimeVue, { theme: { preset: marketLensTheme, options: themeOptions } })
  .mount('#app');
