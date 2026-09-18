import { expect, test } from '@playwright/test';
import { api, ABB } from './fixtures/api.mjs';
import { dismissShellControls, navigationLink } from './support/shell';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class FakeEventSource extends EventTarget {
      constructor() { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: FakeEventSource });
  });
  // One handler for everything, registered first so a test may add its own in front of it.
  await page.route('**/api/v1/**', (route) => {
    const { pathname } = new URL(route.request().url());
    return route.fulfill({ json: api(pathname) as object });
  });
});

for (const viewport of VIEWPORTS) {
  test(`a person reads what their decisions would have done at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/paper');
    await expect(page.getByRole('heading', { name: /what your decisions would have done/i }))
      .toBeVisible();

    await expect(await navigationLink(page, 'Paper')).toBeVisible();
    await dismissShellControls(page);

    // The statement the whole screen is built around.
    const notice = page.getByTestId('paper-notice');
    await expect(notice).toContainText(/simulation/i);
    await expect(notice).toContainText(/nothing here was traded/i);
    await expect(notice).toContainText(/proposes none/i);

    // What it is worth, where it started, and the one return figure in the product.
    const summary = page.getByTestId('simulation-notice');
    await expect(summary).toBeVisible();
    await expect(page.locator('main')).toContainText('727,440');
    await expect(page.locator('main')).toContainText('1,000,000');
    await expect(page.locator('main')).toContainText('4.3%');

    // The orders, with the price expected beside the price actually paid.
    const orders = page.getByTestId('paper-order-list');
    await expect(orders).toContainText('600.00');
    await expect(orders).toContainText('600.30');
    await expect(orders).toContainText('Filled');
    await expect(orders).toContainText('Could not fill');
    await expect(orders).toContainText(/not enough cash/i);
    await expect(orders).toContainText('Waiting');

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('no surface tells the person what to do with the result', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/paper');
  await expect(page.getByTestId('paper-order-list')).toBeVisible();
  const body = (await page.locator('main').innerText()).toLowerCase();
  for (const forbidden of ['we recommend', 'we suggest', 'you should', 'suggested',
    'place an order', 'proven', 'guaranteed']) {
    expect(body, `the page says "${forbidden}"`).not.toContain(forbidden);
  }
});

test('the account states the costs it charged rather than hiding them', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/paper');
  const main = page.locator('main');
  await expect(main).toContainText(/brokerage/i);
  await expect(main).toContainText(/slippage/i);
  await expect(main).toContainText(/currency spread/i);
});

test('promoting sends only the intent, and nothing a broker could act on', async ({ page }) => {
  let sent: Record<string, unknown> | null = null;
  await page.route('**/api/v1/paper-account/orders', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    sent = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ status: 201, json: api('/api/v1/paper-account') as object });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/paper');

  const which = page.getByLabel('Which intent to promote into the paper account');
  await expect(which).toBeEnabled();
  await which.click();
  await page.getByRole('option').first().click();
  const promote = page.getByRole('button', { name: 'Promote it' });
  await expect(promote).toBeEnabled();
  await promote.click();

  await expect.poll(() => sent).not.toBeNull();
  expect(Object.keys(sent!)).toEqual(['intent_id']);
});

test('a pending order can be withdrawn and a filled one cannot', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/paper');
  await expect(page.getByTestId('paper-order-list')).toBeVisible();

  // Three orders: filled, unfillable, pending. Only the pending one offers to be withdrawn.
  const withdraw = page.getByRole('button', { name: /^Withdraw the promoted order/ });
  await expect(withdraw).toHaveCount(1);
  await expect(withdraw).toContainText('Withdraw');
});

test('a person with no account is offered one, and told the terms are final', async ({ page }) => {
  await page.route('**/api/v1/paper-account', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ status: 404, json: { error: { code: 'no_account', message: 'none' } } });
  });
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/paper');

  await expect(page.getByRole('heading', { name: 'Open a paper account' })).toBeVisible();
  await expect(page.locator('main')).toContainText(/cannot be changed/i);
  // And nothing proposes a starting balance to accept by mistake.
  await expect(page.getByLabel('It starts with')).toHaveValue('');
  await expect(page.getByTestId('paper-order-list')).toHaveCount(0);
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/paper');
  await expect(page.getByTestId('paper-order-list')).toBeVisible();
  await expect(page.getByTestId('paper-notice')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

// FR-020: no screen combines a paper figure with a real holding. The read path is the proof.
test('the paper screen reads no real portfolio', async ({ page }) => {
  const reads: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    const { pathname } = new URL(route.request().url());
    reads.push(pathname);
    return route.fulfill({ json: api(pathname) as object });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/paper');
  await expect(page.getByTestId('paper-order-list')).toBeVisible();

  expect(reads.filter((path) => path.startsWith('/api/v1/portfolio'))).toEqual([]);
  expect(reads.some((path) => path === '/api/v1/paper-account')).toBe(true);
  expect(ABB.length).toBeGreaterThan(0);
});
