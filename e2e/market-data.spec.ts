import { expect, test } from '@playwright/test';

const runID = '22000000-0000-4000-8000-000000000001';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    const sources: EventTarget[] = [];
    const urls: string[] = [];
    class FakeEventSource extends EventTarget {
      constructor(url: string | URL) {
        super();
        urls.push(String(url));
        sources.push(this);
        queueMicrotask(() => this.dispatchEvent(new Event('open')));
      }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: FakeEventSource });
    Object.assign(window, {
      __marketDataEventURLs: urls,
      __emitMarketDataEvent: (id: string, type: string, entityType: string, entityID: string) => {
        const source = sources.at(-1);
        source?.dispatchEvent(new MessageEvent('message', {
          data: JSON.stringify({ version: 1, scope: 'shared', entity_type: entityType, entity_id: entityID }),
          lastEventId: id,
        }));
        source?.dispatchEvent(new MessageEvent(type, {
          data: JSON.stringify({ version: 1, scope: 'shared', entity_type: entityType, entity_id: entityID }),
          lastEventId: id,
        }));
      },
      __failMarketDataEvent: () => sources.at(-1)?.dispatchEvent(new Event('error')),
    });
  });
});

test('shows partial import, refreshes live without polling, reconnects, and preserves state', async ({ page, isMobile }) => {
  let status = 'partial';
  let requests = 0;
  await page.route('**/api/v1/market-data/imports?*', async (route) => {
    requests += 1;
    await route.fulfill({ json: { items: [{
      id: runID,
      kind: 'backfill',
      provider: 'fixture',
      status,
      started_at: '2026-08-29T08:00:00Z',
      finished_at: '2026-08-29T08:01:00Z',
      counts: status === 'partial'
        ? { processed: 3, accepted: 2, rejected: 1, flagged: 1 }
        : { processed: 3, accepted: 3, rejected: 0, flagged: 0 },
      error_summary: status === 'partial' ? 'One instrument failed safely.' : null,
    }] } });
  });

  await page.goto('/markets');
  await expect(page.getByRole('heading', { name: 'Market data' })).toBeVisible();
  await expect(page.getByTestId('connection-state')).toContainText('connected', { ignoreCase: true });
  await expect(page.getByTestId('run-status')).toContainText('partial', { ignoreCase: true });
  await expect(page.getByText('Rejected 1')).toBeVisible();
  await expect(page.getByText('One instrument failed safely.')).toBeVisible();

  const copy = page.getByTestId('copy-retry');
  await copy.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByText('Retry command copied')).toBeVisible();
  if (isMobile) await copy.tap();

  await page.getByRole('button', { name: 'Change color theme' }).click();
  const themeLabel = await page.getByRole('button', { name: 'Change color theme' }).textContent();
  status = 'succeeded';
  await page.evaluate(({ runID }) => {
    (window as unknown as { __emitMarketDataEvent: (...args: string[]) => void })
      .__emitMarketDataEvent('41', 'import_run.changed.v1', 'import_run', runID);
  }, { runID });
  await expect(page.getByTestId('run-status')).toContainText('succeeded', { timeout: 10_000, ignoreCase: true });

  await page.evaluate(() => {
    (window as unknown as { __failMarketDataEvent: () => void }).__failMarketDataEvent();
  });
  await expect(page.getByTestId('connection-state')).toContainText('reconnecting', { ignoreCase: true });
  await expect.poll(async () => page.evaluate(() =>
    (window as unknown as { __marketDataEventURLs: string[] }).__marketDataEventURLs.at(-1),
  )).toContain('last_event_id=41');
  await expect(page.getByRole('button', { name: 'Change color theme' })).toHaveText(themeLabel ?? '');

  const afterEventRequests = requests;
  await page.waitForTimeout(2_000);
  expect(requests).toBe(afterEventRequests);
});

test('shows failed import accessibly in every theme and does not overflow at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [{
    id: runID,
    kind: 'daily_update',
    provider: 'fixture',
    status: 'failed',
    started_at: '2026-08-29T08:00:00Z',
    finished_at: '2026-08-29T08:01:00Z',
    counts: { processed: 0, accepted: 0, rejected: 0, flagged: 0 },
    error_summary: 'Market-data provider request timed out.',
  }] } }));
  await page.goto('/markets');
  for (let index = 0; index < 3; index += 1) {
    await expect(page.getByTestId('run-status')).toContainText('failed', { ignoreCase: true });
    await expect(page.getByText('Market-data provider request timed out.')).toBeVisible();
    await page.getByRole('button', { name: 'Change color theme' }).click();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  await expect(page.getByTestId('copy-retry')).toBeVisible();
});

test('searches, inspects, and returns with instrument state across responsive input modes', async ({ page, isMobile }) => {
  const instrumentID = '33000000-0000-4000-8000-000000000001';
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/instruments?*', (route) => route.fulfill({ json: { items: [{
    id: instrumentID, isin: 'SE0000000100', ticker: 'ALFA', name: 'Alpha AB',
    exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm', timezone: 'Europe/Stockholm' },
    currency: 'SEK', country: 'SE', instrument_type: 'common_stock', active: true,
    purchasability_status: 'unverified',
  }], next_cursor: null } }));
  await page.route(`**/api/v1/instruments/${instrumentID}`, (route) => route.fulfill({ json: {
    id: instrumentID, isin: 'SE0000000100', ticker: 'ALFA', name: 'Alpha AB',
    exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm', timezone: 'Europe/Stockholm' },
    currency: 'SEK', country: 'SE', instrument_type: 'common_stock', active: true,
    purchasability_status: 'unverified',
    latest_bar: { session_date: '2026-08-28', open: '100.125', high: '102.5', low: '99.75', close: '101.25', adjusted_close: null, volume: 1234, currency: 'SEK', provider: 'fixture', observed_at: '2026-08-29T18:30:00Z' },
    history: { first_session: '2026-08-27', last_session: '2026-08-28', bar_count: 2 },
    quality_summary: { open_warnings: 1, open_errors: 0 },
  } }));
  await page.route(`**/api/v1/instruments/${instrumentID}/prices?*`, (route) => route.fulfill({ json: { items: [], next_cursor: null } }));

  await page.goto('/markets');
  const search = page.getByRole('searchbox', { name: 'Search instruments' });
  await search.fill('ALFA');
  await page.getByLabel('Exchange').selectOption('XSTO');
  await expect(page).toHaveURL(/q=ALFA/);
  const result = page.getByRole('link', { name: /Alpha AB.*ALFA.*XSTO/i });
  if (isMobile) await result.tap(); else await result.press('Enter');

  await expect(page).toHaveURL(new RegExp(`/markets/${instrumentID}`));
  await expect(page.getByRole('heading', { name: 'Alpha AB' })).toBeVisible();
  await expect(page.getByText('Latest known daily value')).toBeVisible();
  await expect(page.getByText('Session 2026-08-28')).toBeVisible();
  await expect(page.getByText('101.25 SEK')).toBeVisible();
  await expect(page.getByText(/1 open warning/i)).toBeVisible();

  for (let theme = 0; theme < 3; theme += 1) {
    await expect(page.getByRole('heading', { name: 'Alpha AB' })).toBeVisible();
    await page.getByRole('button', { name: 'Change color theme' }).click();
  }
  const viewport = page.viewportSize();
  if (viewport && viewport.width === 768) {
    await page.setViewportSize({ width: 1024, height: 768 });
    await expect(page.getByRole('heading', { name: 'Alpha AB' })).toBeVisible();
  }

  await page.getByRole('link', { name: 'Back to instruments' }).click();
  await expect(search).toHaveValue('ALFA');
  await expect(page.getByLabel('Exchange')).toHaveValue('XSTO');
  await page.setViewportSize({ width: 320, height: 800 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
