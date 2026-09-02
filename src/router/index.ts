import { watch } from 'vue';
import { createRouter, createWebHistory, type RouterHistory } from 'vue-router';
import DashboardView from '@/views/DashboardView.vue';
import MarketsView from '@/views/MarketsView.vue';
import InstrumentMarketDataView from '@/views/InstrumentMarketDataView.vue';
import OperationsView from '@/views/OperationsView.vue';
import SignalsView from '@/views/SignalsView.vue';
import LoginView from '@/views/LoginView.vue';
import OwnerSetupView from '@/views/OwnerSetupView.vue';
import AcceptInvitationView from '@/views/AcceptInvitationView.vue';
import AccountSettingsView from '@/views/AccountSettingsView.vue';
import { authStore } from '@/stores/auth';

export interface RouteAuthStore {
  state: { status: 'unknown' | 'anonymous' | 'authenticated' };
  restore(): Promise<void>;
}

export function createMarketLensRouter(history: RouterHistory, auth: RouteAuthStore = authStore) {
  const router = createRouter({
    history,
    routes: [
      { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
      { path: '/setup', name: 'owner-setup', component: OwnerSetupView, meta: { public: true } },
      { path: '/invite', name: 'accept-invitation', component: AcceptInvitationView, meta: { public: true } },
      { path: '/', name: 'dashboard', component: DashboardView },
      { path: '/markets', name: 'markets', component: MarketsView },
      { path: '/markets/:instrumentId', name: 'instrument-market-data', component: InstrumentMarketDataView },
      { path: '/signals', name: 'signals', component: SignalsView },
      { path: '/operations', name: 'operations', component: OperationsView },
      { path: '/account', name: 'account-settings', component: AccountSettingsView },
    ],
  });
  router.beforeEach(async (to) => {
    if (to.meta.public === true) return true;
    await auth.restore();
    if (auth.state.status === 'authenticated') return true;
    return { path: '/login', query: { redirect: to.fullPath } };
  });
  // Authorization can end between navigations: a revoked session or a deactivated account closes
  // the event stream, and the page a person is already looking at has to leave with it rather
  // than sit there showing private data it can no longer refresh.
  watch(() => auth.state.status, (status) => {
    if (status !== 'anonymous') return;
    const current = router.currentRoute.value;
    if (current.meta.public === true) return;
    void router.replace({ path: '/login', query: { redirect: current.fullPath } });
  });
  return router;
}

export default createMarketLensRouter(createWebHistory());
