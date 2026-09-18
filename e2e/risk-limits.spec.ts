import { expect, test } from '@playwright/test';
import { dismissShellControls, navigationLink } from './support/shell';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

function report(limits: unknown[]) {
  return { accounting_currency: 'SEK', limits, limits_are_your_own: true };
}

const breached = {
  kind: 'instrument_share',
  threshold: '0.250000000000',
  state: 'exceeded',
  measured: '0.412300000000',
  denominator: '284000.000000000000',
  absence_reason: null,
  contributions: [
    { label: 'VOLV-B', value: '117093.200000000000', share: '0.412300000000' },
    { label: 'NOKIA', value: '166906.800000000000', share: '0.587700000000' },
  ],
};

const unevaluable = {
  kind: 'sector_share',
  threshold: '0.500000000000',
  state: 'unevaluable',
  measured: null,
  denominator: null,
  absence_reason: 'portfolio_incomplete',
  contributions: [],
};

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
  await page.route('**/api/v1/risk-limits', (route) =>
    route.fulfill({ json: report([breached, unevaluable]) }));
});

for (const viewport of VIEWPORTS) {
  test(`a person sees where they stand against their own rules at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/risk');

    await expect(await navigationLink(page, 'Limits')).toBeVisible();
    await dismissShellControls(page);
    await expect(page.getByRole('heading', { name: /rules you set yourself/i })).toBeVisible();

    // FR-018: these are the person's own rules, and the product offers no advice.
    await expect(page.getByTestId('own-rules-notice')).toBeVisible();
    await expect(page.getByTestId('own-rules-notice')).toContainText(/suggests none/i);

    const table = page.getByTestId('limit-table');
    // The state as a word, never colour alone.
    await expect(table).toContainText('Over');
    await expect(table).toContainText('Not measured');
    // The arithmetic behind the figure.
    await expect(table).toContainText('41.2%');
    await expect(table).toContainText('25.0%');
    await expect(table).toContainText('284,000');
    await expect(table).toContainText('VOLV-B');
    // The gap, and nothing about what would close it.
    await expect(table).toContainText('16.2 points over');
    // And why the second could not be measured.
    await expect(table).toContainText(/could not be priced/i);

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

test('no surface says what would close a breach', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/risk');
  const body = (await page.locator('main').innerText()).toLowerCase();
  for (const forbidden of ['sell ', 'reduce', 'we suggest', 'recommend', 'you should']) {
    expect(body, `the page says "${forbidden}"`).not.toContain(forbidden);
  }
});

test('a person with no limits is told so and offered none', async ({ page }) => {
  await page.route('**/api/v1/risk-limits', (route) => route.fulfill({ json: report([]) }));
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/risk');

  await expect(page.getByText(/have not set any limits/i)).toBeVisible();
  await expect(page.getByText(/does not suggest any/i)).toBeVisible();
  // And the form offers no default threshold to accept by mistake.
  await expect(page.getByLabel('No more than')).toHaveValue('');
});

test('a limit is stated as a percentage and stored as a proportion', async ({ page }) => {
  let sent: Record<string, unknown> | null = null;
  await page.route('**/api/v1/risk-limits/instrument_share', async (route) => {
    sent = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ json: report([breached]) });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/risk');

  await page.getByLabel('No more than').fill('25');
  await page.getByLabel('No more than').press('Tab');
  const save = page.getByRole('button', { name: 'Set this limit' });
  await expect(save).toBeEnabled();
  await save.click();

  // People say "25 per cent"; a share is 0.25. The form does the translation so nobody has to.
  await expect.poll(() => sent).not.toBeNull();
  expect(sent).toEqual({ threshold: '0.25' });
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/risk');
  await expect(page.getByTestId('limit-table')).toBeVisible();
  await expect(page.getByTestId('own-rules-notice')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
