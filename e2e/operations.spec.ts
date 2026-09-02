import { expect, test } from '@playwright/test';

const importRunID = '22000000-0000-4000-8000-000000000001';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/instruments/sectors', (route) => route.fulfill({ json: { items: [
    { code: 'industrials', name: 'Industrials', instrument_count: 1 },
    { code: 'unclassified', name: 'Unclassified', instrument_count: 0 },
  ] } }));
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: {
    id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
    role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
  } }));
  await page.addInitScript(() => {
    class FakeEventSource extends EventTarget {
      constructor() {
        super();
        queueMicrotask(() => this.dispatchEvent(new Event('open')));
      }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: FakeEventSource });
  });
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [{
    id: importRunID,
    kind: 'daily_update',
    provider: 'fixture',
    status: 'failed',
    started_at: '2026-09-01T20:00:00Z',
    finished_at: '2026-09-01T20:01:00Z',
    counts: { processed: 100, accepted: 0, rejected: 0, flagged: 0, revised: 2 },
    error_summary: 'Market-data provider request timed out.',
  }] } }));
  await page.route('**/api/v1/feature-runs*', (route) => route.fulfill({ json: { items: [{
    id: 'eeeeeeee-0014-4000-8000-000000000002',
    kind: 'incremental',
    status: 'partial',
    started_at: '2026-09-01T20:05:00Z',
    finished_at: '2026-09-01T20:05:04Z',
    instrument_count: 100,
    value_count: 7502,
    failed_count: 3,
    trigger_run_id: importRunID,
    definition_name: null,
    app_version: '0.10.1',
  }] } }));
});

for (const viewport of VIEWPORTS) {
  test(`operational state is readable at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/operations');

    // Reachable from the primary navigation rather than by knowing the URL.
    await expect(page.getByRole('link', { name: 'Operations' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Data pipeline' })).toBeVisible();

    // The import half, with its sanitized reason and no provider internals.
    await expect(page.getByTestId('run-status')).toContainText('failed', { ignoreCase: true });
    await expect(page.getByText('Market-data provider request timed out.')).toBeVisible();

    // A run that corrected sessions it had already stored says so. That is the case worth
    // noticing: every feature and every signal derived from those sessions moved underneath.
    await expect(page.getByText(/Corrected/)).toBeVisible();

    // The engine half, which had no interface at all before this feature.
    await expect(page.getByTestId('feature-run-list')).toBeVisible();
    await expect(page.getByTestId('feature-run-list').getByText('partial')).toBeVisible();
    // A partial run leaves earlier values standing; saying how many is the whole point.
    await expect(page.getByText(/3 instruments kept their earlier values/)).toBeVisible();

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('market data carries no operational report, only a link to it', async ({ page }) => {
  await page.route('**/api/v1/instruments?*', (route) =>
    route.fulfill({ json: { items: [], next_cursor: null } }));
  await page.goto('/markets');
  await expect(page.getByRole('heading', { name: 'Market data' })).toBeVisible();

  // The report's own controls must not be on this page any more.
  await expect(page.getByTestId('copy-retry')).toHaveCount(0);
  await expect(page.getByTestId('feature-run-list')).toHaveCount(0);

  // What remains leads to the screen that has them.
  const link = page.getByRole('link', { name: 'Operations' });
  await expect(link.first()).toBeVisible();
  await link.first().click();
  await expect(page.getByRole('heading', { name: 'Data pipeline' })).toBeVisible();
});

test('a deployment where the engine has never run says so', async ({ page }) => {
  await page.route('**/api/v1/feature-runs*', (route) => route.fulfill({ json: { items: [] } }));
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/operations');
  await expect(page.getByText(/feature engine has not run/i)).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

test('a run that corrected nothing does not imply it did', async ({ page }) => {
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [{
    id: importRunID,
    kind: 'daily_update',
    provider: 'fixture',
    status: 'succeeded',
    started_at: '2026-09-01T20:00:00Z',
    finished_at: '2026-09-01T20:01:00Z',
    counts: { processed: 100, accepted: 100, rejected: 0, flagged: 0, revised: 0 },
  }] } }));
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/operations');
  // Zero is shown the way the other zero counts are: an ordinary night must not be
  // indistinguishable from a screen that cannot report corrections at all.
  await expect(page.getByText(/Corrected/)).toBeVisible();
  await expect(page.getByText(/Corrected\s*[1-9]/)).toHaveCount(0);
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
