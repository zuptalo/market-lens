import { createRouter, createWebHistory } from 'vue-router';
import DashboardView from '@/views/DashboardView.vue';
import MarketsView from '@/views/MarketsView.vue';

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/markets', name: 'markets', component: MarketsView },
  ],
});
