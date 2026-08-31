import { createApp } from 'vue';
import PrimeVue from 'primevue/config';
import Aura from '@primeuix/themes/aura';
import App from './App.vue';
import router from './router';
import { bindAuthConnectivity } from '@/composables/useAuth';
import './styles/main.css';

bindAuthConnectivity();

createApp(App)
  .use(router)
  .use(PrimeVue, {
    theme: {
      preset: Aura,
      options: { darkModeSelector: '.market-lens-dark' },
    },
  })
  .mount('#app');
