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

  // A release rolls a new pod, and for a few seconds nothing answers. Sending somebody to the
  // login page then is a lie — they are signed in, the server just cannot be reached — and it
  // cost them their place and created a second session when they signed in again.
  it('keeps somebody where they are when the server cannot be reached', async () => {
    const auth = routeAuth('unreachable');
    const router = createMarketLensRouter(createMemoryHistory(), auth);

    await router.push('/markets?country=SE');
    await router.isReady();

    expect(router.currentRoute.value.path).toBe('/markets');
    expect(router.currentRoute.value.query.country).toBe('SE');
  });

  it('still leaves a signed-out person at the login page', async () => {
    const auth = routeAuth('anonymous');
    const router = createMarketLensRouter(createMemoryHistory(), auth);
    await router.push('/operations');
    await router.isReady();
    expect(router.currentRoute.value.path).toBe('/login');
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
