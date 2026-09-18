import { expect, test } from '@playwright/test';
import { dismissShellControls, navigationLink } from './support/shell';

const RUN = '55000000-0000-4000-8000-000000000001';
const VOLVO = '44000000-0000-4000-8000-000000000001';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

const configuration = {
  name: 'momentum_trend_equal_weight',
  version: 1,
  title: 'Momentum and trend, equal weight, monthly',
  intent: 'Hold the ten highest-scoring instruments in equal weight, rebalanced monthly.',
  caveat: 'This is a simulation over past data, not a prediction and not advice.',
  strategy: { name: 'momentum_trend', version: 1 },
  universe: 'nordic-liquid-v1',
  from_session: null,
  to_session: null,
  starting_capital: '1000000',
  accounting_currency: 'EUR',
  sizing: { rule: 'equal_weight_top_n', holdings: 10 },
  rebalance: { schedule: 'monthly' },
  costs: {
    brokerage_bps: '10', brokerage_minimum: '5', slippage_bps: '5', currency_spread_bps: '10',
  },
};

const summary = {
  id: RUN,
  configuration,
  status: 'succeeded',
  from_session: '2016-08-31',
  to_session: '2026-09-15',
  started_at: '2026-09-16T04:00:00Z',
  finished_at: '2026-09-16T04:01:20Z',
  trade_count: 312,
  skipped_count: 11840,
  rebalance_count: 121,
  is_simulation: true,
};

function measures(overrides: Record<string, unknown> = {}) {
  return {
    from_session: '2016-08-31',
    to_session: '2026-09-15',
    total_return: '0.418200000000',
    annualised_return: '0.035600000000',
    volatility: '0.184300000000',
    maximum_drawdown: '-0.312400000000',
    trade_count: 312,
    total_costs: '18849.571698069468',
    absence_reason: null,
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
  await page.route('**/api/v1/backtests?*', (route) => route.fulfill({ json: { items: [summary] } }));
  await page.route(`**/api/v1/backtests/${RUN}`, (route) => route.fulfill({ json: {
    ...summary,
    measures: measures(),
    benchmarks: [
      {
        mic: 'XSTO', series: 'OMXS30.INDX', absence_reason: null,
        measures: measures({ total_return: '0.612000000000', trade_count: 0, total_costs: '0' }),
      },
      {
        mic: 'XCSE', series: 'OMXC25.INDX', absence_reason: 'series_starts_after_range',
        measures: measures({
          from_session: null, to_session: null, total_return: null, annualised_return: null,
          volatility: null, maximum_drawdown: null, trade_count: null, total_costs: null,
          absence_reason: 'series_starts_after_range',
        }),
      },
    ],
    skipped: [
      { reason: 'not_selected', count: 11800 },
      { reason: 'no_cash', count: 40 },
    ],
  } }));
  await page.route(`**/api/v1/backtests/${RUN}/trades*`, (route) => route.fulfill({ json: {
    items: [{
      id: '66000000-0000-4000-8000-000000000001',
      instrument_id: VOLVO,
      ticker: 'VOLV-B',
      name: 'Volvo AB',
      signal_session: '2026-09-01',
      execution_session: '2026-09-02',
      direction: 'buy',
      quantity: '100.000000000000',
      price: '42.500000000000',
      currency: 'SEK',
      conversion_rate: '9.456700000000',
      brokerage: '5.000000000000',
      slippage: '0.224700000000',
      currency_spread: '0.449500000000',
      cash_effect: '-455.000000000000',
      signal_id: `${VOLVO}/2026-09-01/00000000-0015-4000-8000-000000000001`,
    }],
    next_cursor: null,
    total: 1,
  } }));
  await page.route(`**/api/v1/backtests/${RUN}/equity`, (route) => route.fulfill({ json: {
    currency: 'EUR',
    items: [
      { session_date: '2016-08-31', cash: '1000000.000000000000', positions_value: '0.000000000000', total: '1000000.000000000000', absence_reason: null },
      { session_date: '2021-03-15', cash: '120000.000000000000', positions_value: '1380000.000000000000', total: '1500000.000000000000', absence_reason: null },
      { session_date: '2026-09-14', cash: '505855.000000000000', positions_value: '912345.000000000000', total: '1418200.000000000000', absence_reason: null },
      { session_date: '2026-09-15', cash: '505855.000000000000', positions_value: null, total: null, absence_reason: 'position_unvalued' },
    ],
  } }));
});

for (const viewport of VIEWPORTS) {
  test(`a result states what it is and what it is compared with at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/backtests');

    // Reachable from the primary navigation, not only by knowing the URL.
    await expect(await navigationLink(page, 'Backtests')).toBeVisible();
    await dismissShellControls(page);
    await expect(page.getByRole('heading', { name: /what a strategy would have done/i })).toBeVisible();

    // FR-021: the statement is on the screen, above the result rather than beneath it.
    await expect(page.getByTestId('simulation-notice')).toBeVisible();
    await expect(page.getByTestId('simulation-notice')).toContainText(/not a prediction/i);

    // The rules the result was produced under, in full.
    await expect(page.getByTestId('configuration-rules')).toContainText('momentum_trend v1');
    await expect(page.getByTestId('configuration-rules')).toContainText('nordic-liquid-v1');
    await expect(page.getByTestId('configuration-rules')).toContainText('1,000,000 EUR');

    // All six measures, including the two that flatter least.
    const table = page.getByTestId('measure-table');
    await expect(table).toContainText('Total return');
    await expect(table).toContainText('Maximum drawdown');
    await expect(table).toContainText('Cost of trading');
    await expect(table).toContainText('41.82%');
    await expect(table).toContainText('-31.24%');

    // The benchmark beside it, and the one that cannot be compared saying why.
    await expect(table).toContainText('OMXS30.INDX');
    await expect(page.getByText(/OMXC25\.INDX cannot be compared/i)).toBeVisible();

    // Every figure the curve conveys, as text, at every width.
    const figures = page.getByTestId('equity-figures');
    await expect(figures).toContainText('Started');
    await expect(figures).toContainText('Highest');
    await expect(figures).toContainText('could not be valued');

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('the curve is reduced to its figures on a small screen', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/backtests');
  // A chart squeezed into 360 pixels is a squiggle. The figures carry the whole result, so the
  // canvas simply does not appear rather than appearing uselessly.
  await expect(page.getByTestId('equity-figures')).toBeVisible();
  await expect(page.getByTestId('equity-chart')).toHaveCount(0);
});

test('a trade leads to the signal that caused it', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/backtests');

  const trades = page.getByTestId('trade-list');
  await expect(trades).toContainText('VOLV-B');
  await expect(trades).toContainText('Bought');
  // Both sessions are shown: a reader who cannot see both cannot tell whether the simulation
  // read the future.
  await expect(trades).toContainText('2026-09-02');
  await expect(trades).toContainText('2026-09-01');

  // SC-003: reachable by keyboard, and it names where it leads.
  const reason = trades.getByRole('link', { name: /see why the strategy scored VOLV-B/i });
  await expect(reason).toBeVisible();
  await reason.focus();
  await expect(reason).toBeFocused();
});

test('what the simulation considered and did not do is stated', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/backtests');
  const skipped = page.getByTestId('skipped-reasons');
  await expect(skipped).toContainText('not among the highest scored');
  await expect(skipped).toContainText('not enough cash left to buy a whole share');
});

test('a deployment with no backtest says so rather than showing an empty page', async ({ page }) => {
  await page.route('**/api/v1/backtests?*', (route) => route.fulfill({ json: { items: [] } }));
  await page.goto('/backtests');
  await expect(page.getByText(/no backtest has been run in this deployment/i)).toBeVisible();
  await expect(page.getByText(/nothing here can start one/i)).toBeVisible();
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/backtests');
  await expect(page.getByTestId('measure-table')).toBeVisible();
  await expect(page.getByTestId('equity-figures')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
