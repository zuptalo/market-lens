import { expect, test } from '@playwright/test';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

const VOLVO = '33333333-3333-4333-8333-333333333333';

function intent(overrides: Record<string, unknown> = {}) {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    instrument_id: VOLVO,
    ticker: 'VOLV-B',
    name: 'Volvo B',
    currency: 'SEK',
    direction: 'buy',
    quantity: '120.000000000000',
    price: '281.400000000000',
    costs: '0.000000000000',
    status: 'considering',
    recorded_at: '2026-09-18T09:00:00Z',
    settled_at: null,
    consequence: {
      resulting_quantity: '520.000000000000',
      resulting_value: '146328.000000000000',
      resulting_share: '0.412300000000',
      denominator: '354900.000000000000',
      absence_reason: null,
      limits: [{
        kind: 'instrument_share', threshold: '0.250000000000', state: 'exceeded',
        measured: '0.412300000000', denominator: '354900.000000000000',
        absence_reason: null, contributions: [],
      }],
    },
    ...overrides,
  };
}

function report(intents: unknown[]) {
  return {
    intents,
    evaluated_independently: true,
    records_what_you_are_considering: true,
  };
}

test.beforeEach(async ({ page }) => {
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
  // One handler for everything, registered last so a test may add its own in front of it. Anything
  // not answered here would otherwise reach the dev proxy and hang.
  await page.route('**/api/v1/**', (route) => {
    const url = route.request().url();
    if (url.includes('/api/v1/account')) {
      return route.fulfill({ json: {
        id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com',
        display_name: 'Owner', role: 'owner', status: 'active',
        email_verified_at: '2026-08-30T08:00:00Z',
      } });
    }
    if (url.includes('/api/v1/order-intents')) {
      return route.fulfill({ json: report([intent()]) });
    }
    if (url.includes('/api/v1/portfolio')) {
      return route.fulfill({ json: {
        accounting_currency: 'SEK', holdings: [], realised: [],
        total: { value: '0', cost: '0', unrealised: '0', realised: '0',
          complete: true, incomplete_reason: null, return_absence: 'cash_is_not_tracked' },
        records_what_you_entered: true,
      } });
    }
    if (url.includes('/api/v1/instruments')) {
      return route.fulfill({ json: {
        items: [{
          id: VOLVO, isin: 'SE0000115446', ticker: 'VOLV-B', name: 'Volvo B',
          exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' }, currency: 'SEK', country: 'SE',
          sector: 'industrials', sector_name: 'Industrials', industry: 'Trucks',
          instrument_type: 'common_stock', status: 'active',
          purchasability_status: 'user_confirmed',
          latest_session: '2026-09-17', latest_close: '281.400000000000',
          change_absolute: '1.200000000000', change_percent: 0.0043,
          return_20: null, return_90: null, volatility: null, stored_sessions: 2500,
          freshness: { state: 'current', sessions_behind: 0 },
        }],
        next_cursor: null, total: 1,
      } });
    }
    return route.fulfill({ json: { items: [] } });
  });
});

for (const viewport of VIEWPORTS) {
  test(`a person sees what an intent would do at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/intents');

    await expect(page.getByRole('link', { name: 'Intents' }).first()).toBeVisible();
    await expect(page.getByRole('heading', { name: /what you are considering/i }).first())
      .toBeVisible();

    // The statement the whole screen is built around.
    const notice = page.getByTestId('no-advice-notice');
    await expect(notice).toBeVisible();
    await expect(notice).toContainText(/offers no advice/i);
    await expect(notice).toContainText(/nothing is sent anywhere/i);

    const list = page.getByTestId('intent-list');
    await expect(list).toContainText('VOLV-B');
    // What the position would become, what it would be worth, and the share with its denominator.
    await expect(list).toContainText('520');
    await expect(list).toContainText('146,328');
    await expect(list).toContainText('41.2%');
    await expect(list).toContainText('354,900');
    // The limit verdict as a word, never colour alone.
    await expect(list).toContainText('Over');
    await expect(list).toContainText('Most in any one company');

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('no surface tells the person whether to act', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/intents');
  await expect(page.getByTestId('intent-list')).toBeVisible();
  const body = (await page.locator('main').innerText()).toLowerCase();
  for (const forbidden of ['recommend', 'suggest', 'you should', 'suggested',
    'place an order', 'submit order', 'send to broker']) {
    expect(body, `the page says "${forbidden}"`).not.toContain(forbidden);
  }
});

test('writing one down sends no field a broker could act on', async ({ page }) => {
  let sent: Record<string, unknown> | null = null;
  await page.route('**/api/v1/order-intents', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    sent = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ status: 201, json: report([intent()]) });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/intents');

  // The field stays closed until the list has arrived, which is also the signal that it is safe
  // to open: a dropdown opened before then would show nothing and not recover.
  const which = page.getByLabel('Which instrument you are considering');
  await expect(which).toBeEnabled();
  await which.click();
  await page.getByRole('option').filter({ hasText: 'VOLV-B' }).click();
  // A number field commits its value on blur, so each entry is followed by a Tab.
  await page.getByLabel('How many').fill('120');
  await page.getByLabel('How many').press('Tab');
  await page.getByLabel('At what price, per share').fill('281.4');
  await page.getByLabel('At what price, per share').press('Tab');
  const write = page.getByRole('button', { name: 'Write this down' });
  await expect(write).toBeEnabled();
  await write.click();

  await expect.poll(() => sent).not.toBeNull();
  expect(Object.keys(sent!).sort()).toEqual(
    ['costs', 'direction', 'instrument_id', 'price', 'quantity'],
  );
  expect(sent).toMatchObject({ instrument_id: VOLVO, direction: 'buy', quantity: '120' });
});

test('marking one acted on records no trade', async ({ page }) => {
  let settled: Record<string, unknown> | null = null;
  const tradeWrites: string[] = [];
  await page.route('**/api/v1/portfolio/trades*', (route) => {
    if (route.request().method() !== 'GET') tradeWrites.push(route.request().method());
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/api/v1/order-intents/*', async (route) => {
    settled = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ json: report([intent({
      status: 'acted_on', settled_at: '2026-09-18T10:00:00Z', consequence: null })]) });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/intents');

  await page.getByRole('button', { name: /Record that you acted on/ }).click();

  await expect.poll(() => settled).toEqual({ status: 'acted_on' });
  await expect(page.getByTestId('intent-list')).toContainText('Acted on');
  // The product makes no claim about what was paid, so it writes no trade.
  expect(tradeWrites).toEqual([]);
  await expect(page.getByTestId('intents-notice')).toContainText(/does not record a trade/i);
});

test('a person considering nothing is told so and offered nothing', async ({ page }) => {
  await page.route('**/api/v1/order-intents', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ json: report([]) });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/intents');

  await expect(page.getByText(/not considering anything/i)).toBeVisible();
  // And the form proposes no quantity or price to accept by mistake.
  await expect(page.getByLabel('How many')).toHaveValue('');
  await expect(page.getByLabel('At what price, per share')).toHaveValue('');
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/intents');
  await expect(page.getByTestId('intent-list')).toBeVisible();
  await expect(page.getByTestId('no-advice-notice')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
