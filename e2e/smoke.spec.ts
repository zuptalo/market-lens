import { expect, test } from '@playwright/test';

const rawVersion = process.env.APP_VERSION || 'dev';
const expectedVersion = /^\d+\.\d+\.\d+$/.test(rawVersion) ? `v${rawVersion}` : 'development';

test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: {
    id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
    role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
  } }));
  await page.addInitScript(() => {
    class QuietEventSource extends EventTarget {
      constructor(_url: string | URL) { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: QuietEventSource });
  });
});

test('shows the Market Lens foundation shell', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Market Lens' })).toBeVisible();
  await expect(page.getByText('Stock research and strategy experimentation platform')).toBeVisible();
  await expect(page.getByText('Foundation stage')).toBeVisible();
  await expect(page.getByText(expectedVersion, { exact: true })).toBeVisible();
});

test('keeps the foundation shell within a 320px viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/');

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
  await expect(page.getByRole('heading', { name: 'Market Lens' })).toBeVisible();
  await expect(page.getByText(expectedVersion, { exact: true })).toBeVisible();
});
