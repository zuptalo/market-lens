import { expect, test, type Page } from '@playwright/test';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

interface Options {
  findings?: unknown[];
  limits?: unknown[];
  importStatus?: string;
  revised?: number;
  unvaluedHolding?: boolean;
  failFindings?: boolean;
}

async function stub(page: Page, options: Options = {}): Promise<void> {
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: {
    id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
    role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
  } }));
  await page.addInitScript(() => {
    class Quiet extends EventTarget {
      constructor() { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: Quiet });
  });

  await page.route('**/api/v1/market-data/quality-findings*', (route) => {
    if (options.failFindings) return route.fulfill({ status: 500, json: { error: 'unavailable' } });
    return route.fulfill({ json: { items: options.findings ?? [] } });
  });
  await page.route('**/api/v1/market-data/imports*', (route) => route.fulfill({ json: { items: [{
    id: 'aaaaaaaa-0024-4000-8000-000000000001', kind: 'daily_update', provider: 'eodhd',
    status: options.importStatus ?? 'succeeded',
    started_at: '2026-09-17T18:00:00Z', finished_at: '2026-09-17T18:04:00Z',
    counts: { processed: 4325, accepted: 4325, rejected: 0, flagged: 0, revised: options.revised ?? 0 },
  }] } }));
  await page.route('**/api/v1/feature-runs*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/strategy-runs*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/risk-limits', (route) => route.fulfill({ json: {
    accounting_currency: 'SEK', limits: options.limits ?? [], limits_are_your_own: true } }));
  await page.route('**/api/v1/portfolio', (route) => route.fulfill({ json: {
    accounting_currency: 'SEK',
    holdings: options.unvaluedHolding ? [{
      instrument_id: 'x', ticker: 'VOLV-B', name: 'Volvo AB', currency: 'SEK',
      quantity: '100', cost: '24619', unrealised: null,
      valuation: { value: null, session: null, conversion_rate: null, absence_reason: 'no_price' },
      comparison: {
        series: 'OMXS30.INDX', from_session: null, to_session: null,
        holding_return: null, benchmark_return: null, absence_reason: 'holding_not_valued',
      },
    }] : [],
    realised: [],
    total: {
      value: options.unvaluedHolding ? null : '0', cost: '0',
      unrealised: options.unvaluedHolding ? null : '0', realised: '0',
      complete: !options.unvaluedHolding,
      incomplete_reason: options.unvaluedHolding ? 'at least one holding could not be valued' : null,
      return_absence: 'cash_is_not_tracked',
    },
    records_what_you_entered: true,
  } }));
}

const finding = {
  id: 'dddddddd-0024-4000-8000-000000000001',
  instrument_id: '44000000-0000-4000-8000-000000000001',
  session_date: '2017-04-13', rule: 'provider_gap', severity: 'warning',
  detail: 'the source has no bar for this session', status: 'open',
  reexamined_at: '2026-09-16T18:00:00Z', awaiting_decision: true, accepted_at: null,
};

const exceededLimit = {
  kind: 'instrument_share', threshold: '0.250000000000', state: 'exceeded',
  measured: '0.412300000000', denominator: '284000.000000000000',
  absence_reason: null, contributions: [],
};

for (const viewport of VIEWPORTS) {
  test(`the Overview says what is waiting at ${viewport.name}`, async ({ page }) => {
    await stub(page, { findings: [finding], limits: [exceededLimit], revised: 7 });
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/');

    await expect(page.getByRole('heading', { name: /what needs you/i })).toBeVisible();

    const waiting = page.getByTestId('waiting');
    await expect(waiting).toContainText(/awaits your decision/i);
    await expect(waiting).toContainText(/limit you are outside/i);

    // What changed, with the statistic that means every derived value moved.
    const changed = page.getByTestId('changed');
    await expect(changed).toContainText('7');
    await expect(changed).toContainText(/corrected/i);

    // Counts and dates only — never a value or a percentage.
    const text = await page.locator('main').innerText();
    expect(text).not.toContain('%');
    for (const currency of ['SEK', 'EUR', 'DKK', 'NOK']) {
      expect(text, `the Overview states a value in ${currency}`).not.toContain(currency);
    }

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('a waiting item leads to the screen that owns it', async ({ page }) => {
  await stub(page, { limits: [exceededLimit] });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await page.getByTestId('waiting').getByRole('link', { name: /where you stand/i }).click();
  await expect(page).toHaveURL(/\/risk$/);
  // And the figures live there, not on the Overview.
  await expect(page.getByTestId('limit-table')).toBeVisible();
});

test('a quiet day says so in one sentence', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await expect(page.getByTestId('waiting-clear')).toContainText(/nothing needs you/i);
  // Nothing is padded to fill the page: the sections with nothing in them are absent.
  await expect(page.getByTestId('unknown')).toHaveCount(0);
});

test('a source that could not be read says so rather than reporting nothing', async ({ page }) => {
  await stub(page, { failFindings: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await expect(page.getByTestId('waiting-unreadable')).toContainText(/could not be read/i);
  // The all-clear must not appear while something is unknown — that is the failure this screen
  // exists to avoid, and it fails in the direction that makes somebody stop looking.
  await expect(page.getByTestId('waiting-clear')).toHaveCount(0);
});

test('what could not be determined is collected in one place', async ({ page }) => {
  await stub(page, { unvaluedHolding: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await expect(page.getByTestId('unknown')).toContainText(/could not be valued/i);
});

test('the Overview claims nothing about unshipped features', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const text = (await page.locator('main').innerText()).toLowerCase();
  expect(text).not.toContain('will be implemented');
  expect(text).not.toContain('foundation stage');
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await stub(page, { findings: [finding], limits: [exceededLimit] });
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/');
  await expect(page.getByTestId('waiting')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
