import { describe, expect, it, vi } from 'vitest';
import { createMemoryHistory } from 'vue-router';
import { createMarketLensRouter, type RouteAuthStore } from './index';

describe('authentication route guard', () => {
  it('redirects anonymous users away from market routes and preserves the intended path', async () => {
    const auth = routeAuth('anonymous');
    const router = createMarketLensRouter(createMemoryHistory(), auth);

    await router.push('/markets?country=SE');
    await router.isReady();

    expect(auth.restore).toHaveBeenCalledOnce();
    expect(router.currentRoute.value.path).toBe('/login');
    expect(router.currentRoute.value.query.redirect).toBe('/markets?country=SE');
  });

  it('allows authenticated market access and keeps login public', async () => {
    const auth = routeAuth('authenticated');
    const router = createMarketLensRouter(createMemoryHistory(), auth);

    await router.push('/markets');
    expect(router.currentRoute.value.name).toBe('markets');
    await router.push('/login');
    expect(router.currentRoute.value.name).toBe('login');
  });
});

function routeAuth(status: RouteAuthStore['state']['status']): RouteAuthStore & { restore: ReturnType<typeof vi.fn> } {
  return { state: { status }, restore: vi.fn(async function (this: RouteAuthStore) {
    if (this.state.status === 'unknown') this.state.status = 'anonymous';
  }) };
}
