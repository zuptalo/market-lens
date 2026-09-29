import { expect, test, type Page } from '@playwright/test';

/**
 * Feature 030: while prices come from the fallback provider, every screen says so.
 *
 * The banner is the standing signal — there are no reminders — so it has to be readable on a
 * phone, a tablet and a desktop, appear and clear without a reload, and never push the page
 * sideways.
 */

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

const ACTIVE = {
  active: true, provider: 'yahoo', since: '2026-09-29', instruments: 100,
  pending_reconciliation: false, changed_at: '2026-09-29T18:05:00Z',
};
const QUIET = {
  active: false, provider: 'yahoo', since: null, instruments: 0,
  pending_reconciliation: false, changed_at: null,
};

async function signedInWithFallback(page: Page, answers: object[]): Promise<void> {
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: {
    id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
    role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
  } }));
  let call = 0;
  await page.route('**/api/v1/market-data/fallback', (route) => {
    const answer = answers[Math.min(call, answers.length - 1)];
    call += 1;
    return route.fulfill({ json: answer });
  });
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/market-data/quality-findings*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/feature-runs*', (route) => route.fulfill({ json: { items: [] } }));
  // An event source the test can speak through, standing in for the server's stream.
  await page.addInitScript(() => {
    const sources: EventTarget[] = [];
    class FakeEventSource extends EventTarget {
      constructor() {
        super();
        sources.push(this);
        queueMicrotask(() => this.dispatchEvent(new Event('open')));
      }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: FakeEventSource });
    Object.defineProperty(window, '__announce', { configurable: true, value: (type: string, data: object) => {
      for (const source of sources) {
        source.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data), lastEventId: String(Date.now()) }));
      }
    } });
  });
}

async function sideways(page: Page): Promise<{ scrollWidth: number; clientWidth: number }> {
  return page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
}

for (const viewport of VIEWPORTS) {
  test(`the fallback banner is readable at ${viewport.name}`, async ({ page }) => {
    await signedInWithFallback(page, [ACTIVE]);
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/operations');

    const banner = page.getByRole('status').filter({ hasText: 'fallback provider' });
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('100 instruments');
    await expect(banner).toContainText('Paper orders wait');
    // Above the page, not over it: the banner is in the flow, and the heading is still reachable.
    await expect(page.getByRole('heading', { name: 'Data pipeline' })).toBeVisible();
    const measured = await sideways(page);
    expect(measured.scrollWidth).toBeLessThanOrEqual(measured.clientWidth);
  });
}

test('the banner does not push a 320px screen sideways', async ({ page }) => {
  await signedInWithFallback(page, [ACTIVE]);
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/operations');
  await expect(page.getByRole('status').filter({ hasText: 'fallback provider' })).toBeVisible();
  await page.waitForTimeout(250);
  const measured = await sideways(page);
  expect(measured.scrollWidth).toBeLessThanOrEqual(measured.clientWidth);
});

test('an ordinary day shows no banner', async ({ page }) => {
  await signedInWithFallback(page, [QUIET]);
  await page.goto('/operations');
  await expect(page.getByRole('heading', { name: 'Data pipeline' })).toBeVisible();
  await expect(page.getByText('fallback provider')).toHaveCount(0);
});

test('the banner appears and clears without a reload', async ({ page }) => {
  await signedInWithFallback(page, [QUIET, ACTIVE, QUIET]);
  await page.goto('/operations');
  await expect(page.getByRole('heading', { name: 'Data pipeline' })).toBeVisible();
  await expect(page.getByText('fallback provider')).toHaveCount(0);

  const announce = (active: boolean) => page.evaluate((isActive) => {
    (window as unknown as { __announce: (type: string, data: object) => void }).__announce(
      'market_data_fallback.changed.v1',
      // The server's envelope: a shared event, which every signed-in session receives.
      { version: 1, scope: 'shared', entity_type: 'market_data_fallback', entity_id: 'instance',
        payload: { active: isActive }, occurred_at: '2026-09-29T18:05:00Z' },
    );
  }, active);

  await announce(true);
  await expect(page.getByRole('status').filter({ hasText: 'fallback provider' })).toBeVisible();
  await announce(false);
  await expect(page.getByText('fallback provider')).toHaveCount(0);
});

test('a failed snapshot shows no banner and breaks nothing', async ({ page }) => {
  await signedInWithFallback(page, [QUIET]);
  await page.route('**/api/v1/market-data/fallback', (route) => route.fulfill({ status: 500, json: { error: 'x' } }));
  await page.goto('/operations');
  await expect(page.getByRole('heading', { name: 'Data pipeline' })).toBeVisible();
  await expect(page.getByText('fallback provider')).toHaveCount(0);
});
