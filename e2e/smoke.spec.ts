import { expect, test } from '@playwright/test';

/**
 * The application boots and the shell renders.
 *
 * This used to assert the landing page's prose — a heading reading "Market Lens", the tagline, and
 * the words "Foundation stage". That coupled the smoke test to whichever view happened to be
 * mounted at `/`, so replacing the foundation-stage stub with a real Overview broke six tests that
 * were not about the Overview at all.
 *
 * What a smoke test should assert is what survives every view: the shell is there, it knows which
 * version it is, the navigation is reachable, and nothing overflows. Those hold whatever `/` shows.
 */

const rawVersion = process.env.APP_VERSION || 'dev';
const expectedVersion = /^\d+\.\d+\.\d+$/.test(rawVersion) ? `v${rawVersion}` : 'development';

test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: {
    id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
    role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
  } }));
  await page.route('**/api/v1/**', (route) => {
    const url = route.request().url();
    if (url.includes('/account')) return route.fallback();
    if (url.includes('/risk-limits')) {
      return route.fulfill({ json: { accounting_currency: 'SEK', limits: [], limits_are_your_own: true } });
    }
    if (url.includes('/portfolio')) {
      return route.fulfill({ json: {
        accounting_currency: 'SEK', holdings: [], realised: [],
        total: { value: '0', cost: '0', unrealised: '0', realised: '0', complete: true,
          incomplete_reason: null, return_absence: 'cash_is_not_tracked' },
        records_what_you_entered: true,
      } });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.addInitScript(() => {
    class QuietEventSource extends EventTarget {
      constructor(_url: string | URL) { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: QuietEventSource });
  });
});

test('the shell renders and knows which version it is', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Market Lens home' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible();
  await expect(page.getByText(expectedVersion, { exact: true })).toBeVisible();
  // Whatever the landing view is, it has a title.
  await expect(page.locator('main h1')).toBeVisible();
});

test('the shell fits a 320px viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/');

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
  await expect(page.getByRole('link', { name: 'Market Lens home' })).toBeVisible();
  await expect(page.getByText(expectedVersion, { exact: true })).toBeVisible();
});
