import { createRouter, createWebHistory } from 'vue-router';
import DashboardView from '@/views/DashboardView.vue';
import MarketsView from '@/views/MarketsView.vue';
import InstrumentMarketDataView from '@/views/InstrumentMarketDataView.vue';

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/markets', name: 'markets', component: MarketsView },
    { path: '/markets/:instrumentId', name: 'instrument-market-data', component: InstrumentMarketDataView },
  ],
});
