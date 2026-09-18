import { expect, test } from '@playwright/test';
import { dismissShellControls, navigationLink } from './support/shell';

const VOLVO = '44000000-0000-4000-8000-000000000001';
const TRADE = '88000000-0022-4000-8000-000000000001';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

function portfolioBody(overrides: Record<string, unknown> = {}) {
  return {
    accounting_currency: 'SEK',
    holdings: [{
      instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo AB', currency: 'SEK',
      quantity: '100.000000000000', cost: '24619.000000000000',
      unrealised: '3781.000000000000',
      valuation: {
        value: '28400.000000000000', session: '2026-09-16',
        conversion_rate: null, absence_reason: null,
      },
      comparison: {
        series: 'OMXS30.INDX', from_session: '2026-03-16', to_session: '2026-09-16',
        holding_return: '0.153600000000', benchmark_return: '0.082000000000', absence_reason: null,
      },
    }],
    realised: [{
      instrument_id: '44000000-0000-4000-8000-000000000002', ticker: 'NOKIA', name: 'Nokia Oyj',
      quantity: '50.000000000000', proceeds: '6000.000000000000', cost: '5000.000000000000',
      realised: '1000.000000000000', cost_basis: 'fifo',
    }],
    total: {
      value: '28400.000000000000', cost: '24619.000000000000',
      unrealised: '3781.000000000000', realised: '1000.000000000000',
      complete: true, incomplete_reason: null, return_absence: 'cash_is_not_tracked',
    },
    records_what_you_entered: true,
    ...overrides,
  };
}

test.beforeEach(async ({ page }) => {
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
  await page.route('**/api/v1/instruments?*', (route) => route.fulfill({ json: {
    items: [{
      id: VOLVO, ticker: 'VOLV-B', name: 'Volvo AB', isin: 'SE0000115446',
      exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' }, sector: 'industrials',
      sector_name: 'Industrials', industry: null, country: 'SE', currency: 'SEK',
      status: 'active', latest_session: '2026-09-16', latest_close: '284.00',
      change_absolute: '1.00', change_percent: 0.0035, return_20: null, return_90: null,
      volatility: null, stored_sessions: 100, freshness: { state: 'current', sessions_behind: 0 },
    }],
    next_cursor: null, total: 1,
  } }));
  await page.route('**/api/v1/portfolio/trades*', (route) => {
    if (route.request().method() !== 'GET') return route.fulfill({ status: 201, json: {} });
    return route.fulfill({ json: {
      items: [{
        id: TRADE, instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo AB', direction: 'buy',
        quantity: '100.000000000000', price: '246.190000000000', currency: 'SEK',
        costs: '39.000000000000', trade_date: '2026-03-16', sequence: 1, status: 'current',
        supersedes: null, recorded_at: '2026-03-16T09:00:00Z', changed_at: null,
      }],
      next_cursor: null, total: 1,
    } });
  });
  await page.route('**/api/v1/portfolio', (route) => route.fulfill({ json: portfolioBody() }));
});

for (const viewport of VIEWPORTS) {
  test(`a portfolio states what it knows and what it does not at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/portfolio');

    await expect(await navigationLink(page, 'Portfolio')).toBeVisible();
    await dismissShellControls(page);
    await expect(page.getByRole('heading', { name: /what you own/i })).toBeVisible();

    // FR-018: the product records what a person entered, and offers no advice.
    await expect(page.getByTestId('no-advice-notice')).toBeVisible();
    await expect(page.getByTestId('no-advice-notice')).toContainText(/nothing here is advice/i);

    // FR-016a: the absent portfolio return is stated where somebody would look for it.
    await expect(page.getByTestId('return-absence')).toBeVisible();
    await expect(page.getByTestId('return-absence')).toContainText(/cannot tell you your overall return/i);

    const totals = page.getByTestId('portfolio-totals');
    await expect(totals).toContainText('28,400');
    await expect(totals).toContainText('24,619');
    // The basis is stated wherever a realised figure appears.
    await expect(totals).toContainText('first in, first out');

    const holdings = page.getByTestId('holding-table');
    await expect(holdings).toContainText('VOLV-B');
    // Up or down in words, never colour alone.
    await expect(holdings).toContainText('Up');
    // And the market beside it, so a gain is never read alone.
    await expect(holdings).toContainText('OMXS30.INDX');

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('an unvaluable holding is listed and explained rather than dropped', async ({ page }) => {
  await page.route('**/api/v1/portfolio', (route) => route.fulfill({
    json: portfolioBody({
      holdings: [{
        instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo AB', currency: 'SEK',
        quantity: '100.000000000000', cost: '24619.000000000000', unrealised: null,
        valuation: { value: null, session: null, conversion_rate: null, absence_reason: 'no_price' },
        comparison: {
          series: 'OMXS30.INDX', from_session: null, to_session: null,
          holding_return: null, benchmark_return: null, absence_reason: 'holding_not_valued',
        },
      }],
      total: {
        value: null, cost: '24619.000000000000', unrealised: null, realised: '0',
        complete: false,
        incomplete_reason: 'at least one holding could not be valued, so no total is stated',
        return_absence: 'cash_is_not_tracked',
      },
    }),
  }));
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/portfolio');

  // The holding stays in the list: omitting it would make the total look whole.
  await expect(page.getByTestId('holding-table')).toContainText('VOLV-B');
  await expect(page.getByTestId('holding-table')).toContainText('Not valued');
  await expect(page.getByTestId('incomplete-total')).toBeVisible();
  await expect(page.getByTestId('incomplete-total')).toContainText(/would make this total look complete/i);
});

test('a refusal says what to do about it', async ({ page }) => {
  await page.route('**/api/v1/portfolio/trades', (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    return route.fulfill({ status: 400, json: { error: {
      code: 'sale_exceeds_position',
      message: 'That is more than you hold. You hold 100.',
      held_quantity: '100.000000000000',
    } } });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/portfolio');

  await page.getByLabel('Instrument').click();
  await page.getByRole('option', { name: /VOLV-B/ }).click();
  // Tab out of each number the way a person moves through a form: the control commits its value on
  // blur, and Save stays disabled until every required field has one.
  await page.getByLabel('Shares').fill('150');
  await page.getByLabel('Shares').press('Tab');
  await page.getByLabel('Price per share').fill('250');
  await page.getByLabel('Price per share').press('Tab');

  const save = page.getByRole('button', { name: 'Save' });
  await expect(save).toBeEnabled();
  await save.click();

  const refusal = page.getByTestId('trade-refusal');
  await expect(refusal).toBeVisible();
  // Not "something went wrong" — the quantity actually held.
  await expect(refusal).toContainText('You hold 100');
});

test('the history keeps what was replaced', async ({ page }) => {
  await page.route('**/api/v1/portfolio/trades*', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ json: {
      items: [
        {
          id: TRADE, instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo AB', direction: 'buy',
          quantity: '120.000000000000', price: '246.190000000000', currency: 'SEK',
          costs: '39.000000000000', trade_date: '2026-03-16', sequence: 1, status: 'current',
          supersedes: 'older', recorded_at: '2026-03-16T09:00:00Z',
          changed_at: '2026-04-01T09:00:00Z',
        },
        {
          id: 'older', instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo AB', direction: 'buy',
          quantity: '100.000000000000', price: '246.190000000000', currency: 'SEK',
          costs: '39.000000000000', trade_date: '2026-03-16', sequence: 1, status: 'superseded',
          supersedes: null, recorded_at: '2026-03-16T09:00:00Z', changed_at: null,
        },
      ],
      next_cursor: null, total: 2,
    } });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/portfolio');

  const history = page.getByTestId('trade-history');
  await expect(history).toContainText('Counts');
  await expect(history).toContainText('Replaced');
  await expect(history).toContainText('kept for reference');
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/portfolio');
  await expect(page.getByTestId('holding-table')).toBeVisible();
  await expect(page.getByTestId('return-absence')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
