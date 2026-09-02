import { expect, test } from '@playwright/test';

const ALFA = 'dddddddd-0015-4000-8000-000000000001';
const BETA = 'dddddddd-0015-4000-8000-000000000002';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

const strategy = {
  name: 'momentum_trend',
  version: 1,
  title: 'Momentum and trend',
  caveat: 'Its weights are stated rather than fitted, and it has not been tested against outcomes.',
  superseded: false,
};

function signal(overrides: Record<string, unknown> = {}) {
  return {
    instrument_id: ALFA,
    session_date: '2026-06-30',
    strategy,
    score: '0.412500000000',
    action: 'WATCH',
    confidence: '0.875000000000',
    absence_reason: null,
    divisor: '1.000000000000',
    computed_at: '2026-07-01T04:00:00Z',
    contributions: [
      {
        factor: 'momentum_90', feature: 'return_90', feature_value: '0.081234000000',
        feature_session: '2026-06-30', factor_score: '0.400000000000',
        weight: '0.250000000000', contribution: '0.100000000000', unavailable_reason: null,
      },
      {
        factor: 'volatility_penalty', feature: 'volatility_20', feature_value: '0.480000000000',
        feature_session: '2026-06-30', factor_score: '-0.466666666667',
        weight: '0.100000000000', contribution: '-0.046666666667', unavailable_reason: null,
      },
    ],
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
  await page.route('**/api/v1/signals*', (route) => route.fulfill({ json: {
    items: [
      { ...signal(), ticker: 'ALFA', name: 'Alfa AB', rank: 1 },
      {
        ...signal({
          instrument_id: BETA, score: null, action: null, confidence: null, divisor: null,
          absence_reason: 'insufficient_history', contributions: [],
        }),
        ticker: 'BETA', name: 'Beta AB', rank: null,
      },
    ],
    next_cursor: null,
    total: 2,
    strategy,
    session_date: '2026-06-30',
    scored: 1,
    unscored: 1,
  } }));
});

for (const viewport of VIEWPORTS) {
  test(`the ranking states its version and its reasons at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/signals');

    // Reachable from the primary navigation, not only by knowing the URL.
    await expect(page.getByRole('link', { name: 'Signals' }).first()).toBeVisible();
    await expect(page.getByRole('heading', { name: /universe in a strategy/i })).toBeVisible();

    // Which version, and as of when. A ranking that does not say is not reproducible by a reader.
    await expect(page.getByText(/momentum_trend v1/)).toBeVisible();
    await expect(page.getByText('2026-06-30').first()).toBeVisible();

    await expect(page.getByTestId('signal-ranking')).toBeVisible();
    await expect(page.getByTestId('signal-ranking').getByRole('link', { name: 'ALFA' })).toBeVisible();
    await expect(page.getByTestId('signal-ranking').getByText('WATCH')).toBeVisible();

    // The instrument with no view is separated and explained, never ranked below a SELL.
    await expect(page.getByTestId('unscored-instruments').getByRole('link', { name: 'BETA' })).toBeVisible();
    await expect(page.getByTestId('unscored-instruments')).toContainText(/too little stored history/i);

    // SC-006: the statement is on the screen showing the signals.
    await expect(page.getByText(/not advice/i).first()).toBeVisible();

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('a row leads to the instrument, where the reasons are', async ({ page }) => {
  const sessions = ['2026-06-26', '2026-06-29', '2026-06-30'];
  await page.route(`**/api/v1/instruments/${ALFA}/history*`, (route) => route.fulfill({ json: {
    instrument: {
      id: ALFA, isin: 'SE0000001501', ticker: 'ALFA', name: 'Alfa AB',
      exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
      currency: 'SEK', country: 'SE', sector: 'industrials', sector_name: 'Industrials',
      status: 'unverified', latest_session: '2026-06-30', latest_close: '109.50',
      change_absolute: '1.00', change_percent: 0.0092, return_20: '0.041200000000',
      return_90: '0.081234000000', volatility: '0.187500000000', stored_sessions: 320,
      freshness: { state: 'current', sessions_behind: 0 },
    },
    coverage: { first_session: sessions[0], last_session: '2026-06-30', stored_sessions: 320 },
    requested_from: sessions[0],
    requested_to: '2026-06-30',
    bars: sessions.map((session_date, index) => ({
      session_date,
      open: (100 + index).toFixed(2),
      high: (101.5 + index).toFixed(2),
      low: (98.5 + index).toFixed(2),
      close: (100.5 + index).toFixed(2),
      adjusted_close: null,
      volume: 1000 + index,
    })),
    missing_sessions: [],
    series_basis: 'raw',
    provider: 'fixture',
    observed_at: '2026-06-30T17:30:00Z',
    actions: [],
    findings: [],
  } }));
  await page.route(`**/api/v1/instruments/${ALFA}/signal*`, (route) => route.fulfill({ json: signal() }));

  await page.goto('/signals');
  await page.getByTestId('signal-ranking').getByRole('link', { name: 'ALFA' }).click();

  // SC-003: from a rank to the reasons behind it, without knowing a URL.
  await expect(page.getByRole('heading', { name: 'Strategy view' })).toBeVisible();
  await expect(page.getByTestId('contribution-list')).toBeVisible();
  await expect(page.getByTestId('contribution-list')).toContainText('momentum_90');
  // SC-010: direction and magnitude as text, not as a colour or a bar.
  await expect(page.getByTestId('contribution-list')).toContainText(/raises the score by 0\.100/);
  await expect(page.getByTestId('contribution-list')).toContainText(/lowers the score by 0\.047/);
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/signals');
  await expect(page.getByTestId('signal-ranking')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

test('a deployment where no strategy has run says so', async ({ page }) => {
  await page.route('**/api/v1/signals*', (route) => route.fulfill({ json: {
    items: [], next_cursor: null, total: 0, strategy, session_date: '', scored: 0, unscored: 0,
  } }));
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/signals');
  await expect(page.getByText(/no strategy has recorded a view/i)).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
