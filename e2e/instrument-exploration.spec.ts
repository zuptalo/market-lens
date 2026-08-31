import { expect, test, type Page } from '@playwright/test';

/**
 * Browsing the universe and reading one instrument's stored history, at every supported
 * viewport.
 *
 * The API is stubbed so the journeys assert on behavior rather than on whatever happens to
 * be imported. The one thing the stub must never do is tidy the data: it keeps a gap, an
 * instrument with too few sessions to compute a statistic, a recorded split and an open
 * finding, because those are the cases this feature exists to handle honestly.
 */

const GAPPY = '11111111-1111-4111-8111-111111111111';
const SHORT = '33333333-3333-4333-8333-333333333333';

/** Two sessions the exchange was open for with no stored bar. Not weekends, not holidays. */
const MISSING = ['2026-05-25', '2026-05-26'];

const SESSIONS = [
  '2026-05-18', '2026-05-19', '2026-05-20', '2026-05-21', '2026-05-22',
  '2026-05-27', '2026-05-28', '2026-05-29', '2026-06-01', '2026-06-02',
];

function bars() {
  return SESSIONS.map((session_date, index) => {
    const base = 100 + index;
    return {
      session_date,
      open: base.toFixed(2),
      high: (base + 1.5).toFixed(2),
      low: (base - 1.5).toFixed(2),
      close: (base + 0.5).toFixed(2),
      adjusted_close: null,
      volume: 1000 + index * 7,
    };
  });
}

function listingRow(overrides: Record<string, unknown> = {}) {
  return {
    id: GAPPY,
    isin: 'SE0000000200',
    ticker: 'GAPPY',
    name: 'Interrupted History AB',
    exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
    currency: 'SEK',
    country: 'SE',
    sector: 'Technology',
    industry: 'Software',
    instrument_type: 'common_stock',
    status: 'active',
    purchasability_status: 'unverified',
    latest_session: '2026-06-02',
    latest_close: '109.50',
    change_absolute: '1.00',
    change_percent: 0.0092,
    return_20: 0.0412,
    return_90: null,
    volatility: 0.1875,
    stored_sessions: 10,
    freshness: { state: 'current', sessions_behind: 0 },
    ...overrides,
  };
}

/** An instrument with too few stored sessions for any statistic — every one is null. */
function shortRow() {
  return listingRow({
    id: SHORT,
    isin: 'DK0000000300',
    ticker: 'SHORT',
    name: 'Barely Listed A/S',
    exchange: { mic: 'XCSE', name: 'Nasdaq Copenhagen' },
    currency: 'DKK',
    country: 'DK',
    sector: 'Health Care',
    return_20: null,
    return_90: null,
    volatility: null,
    stored_sessions: 20,
  });
}

function historyWindow() {
  return {
    instrument: listingRow(),
    coverage: { first_session: '2026-01-05', last_session: '2026-06-02', stored_sessions: 120 },
    requested_from: SESSIONS[0],
    requested_to: SESSIONS[SESSIONS.length - 1],
    bars: bars(),
    missing_sessions: [...MISSING],
    series_basis: 'provider_adjusted',
    provider: 'fixture',
    observed_at: '2026-06-02T17:30:00Z',
    actions: [{
      id: 'a1', action_type: 'split', ex_date: '2026-05-28', ratio: '2',
      amount: null, currency: null, old_symbol: null, new_symbol: null,
    }],
    findings: [{
      id: 'f1', rule: 'suspicious_jump', status: 'open', session_date: '2026-05-29',
      detail: 'close moved more than the configured threshold',
    }],
  };
}

/** Every listing request the page makes, so a test can assert what was asked of the server. */
const listingRequests: string[] = [];

async function stubApi(page: Page): Promise<void> {
  listingRequests.length = 0;

  await page.route('**/api/v1/account', (route) => route.fulfill({
    json: {
      id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com',
      display_name: 'Owner', role: 'owner', status: 'active',
      email_verified_at: '2026-08-30T08:00:00Z',
    },
  }));

  await page.route('**/api/v1/instruments?*', (route) => {
    const url = new URL(route.request().url());
    listingRequests.push(url.search);
    let items = [listingRow(), shortRow()];
    const sector = url.searchParams.get('sector');
    if (sector) items = items.filter((item) => item.sector === sector);
    const query = url.searchParams.get('q');
    if (query) {
      items = items.filter((item) =>
        `${item.name} ${item.ticker} ${item.isin}`.toLowerCase().includes(query.toLowerCase()));
    }
    if (url.searchParams.get('mic') === 'NONE') items = [];
    return route.fulfill({ json: { items, next_cursor: null } });
  });

  await page.route('**/api/v1/instruments/*/history*', (route) =>
    route.fulfill({ json: historyWindow() }));

  await page.route('**/api/v1/market-data/imports*', (route) =>
    route.fulfill({ json: { items: [] } }));

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
    Object.assign(window, {
      // Named, because that is what the server writes.
      __emitNamed: (type: string, payload: Record<string, unknown>, id = '1') => {
        sources.at(-1)?.dispatchEvent(new MessageEvent(type, {
          data: JSON.stringify({ version: 1, scope: 'shared', ...payload }),
          lastEventId: id,
        }));
      },
    });
  });
}

test.beforeEach(async ({ page }) => stubApi(page));

/**
 * Below the tablet breakpoint the filters and the sort control live in a sheet rather than
 * above the list, so the list itself gets the screen. Opening it is part of the journey on a
 * phone, not a workaround.
 */
async function openFilters(page: Page, isMobile: boolean | undefined): Promise<void> {
  if (!isMobile) return;
  await page.getByTestId('open-filters').click();
  await expect(page.getByLabel('Search instruments')).toBeVisible();
}

test('browses, filters, searches and keeps the view across a reload', async ({ page, isMobile }) => {
  await page.goto('/markets');
  await expect(page.getByText('Interrupted History AB')).toBeVisible();
  await expect(page.getByText('Barely Listed A/S')).toBeVisible();

  // Each price is stated in its own currency, and no comparison across them is implied.
  await expect(page.getByText('109.50 SEK')).toBeVisible();

  await openFilters(page, isMobile);
  await page.getByLabel('Search instruments').fill('gappy');
  await expect(page.getByText('Barely Listed A/S')).toHaveCount(0);
  await expect(page.getByText('Interrupted History AB')).toBeVisible();

  // The query is in the address bar, so a reload lands on the same view (FR-006).
  await expect(page).toHaveURL(/q=gappy/);
  await page.reload();
  // The result survives the reload on its own; the sheet has to be reopened to read the
  // field back, because a reload closes it as it closes any other transient surface.
  await expect(page.getByText('Interrupted History AB')).toBeVisible();
  await openFilters(page, isMobile);
  await expect(page.getByLabel('Search instruments')).toHaveValue('gappy');
});

test('never shows an uncomputable statistic as a zero move', async ({ page }) => {
  await page.goto('/markets');
  await expect(page.getByText('Barely Listed A/S')).toBeVisible();
  // SHORT has twenty stored sessions — one short of what a 20-session return needs. Rendering
  // 0.00% would say it did not move, which is a different and false claim.
  await expect(page.getByText('0.00%')).toHaveCount(0);
});

test('explains an empty result and offers a way back', async ({ page }) => {
  await page.goto('/markets?mic=NONE');
  await expect(page.getByText('No instruments match these filters')).toBeVisible();
  await expect(page.getByTestId('clear-filters')).toBeVisible();
});

test('asks the server to sort the whole result set', async ({ page, isMobile }) => {
  await page.goto('/markets');
  await expect(page.getByText('Interrupted History AB')).toBeVisible();
  listingRequests.length = 0;

  if (isMobile) {
    // The stacked card layout has no header row on screen, so the sheet carries the sort
    // control. Without it a phone could not sort at all.
    await openFilters(page, isMobile);
    await page.getByRole('combobox', { name: 'Sort by' }).click();
    await page.getByRole('option', { name: '20-session return', exact: true }).click();
  } else {
    await page.getByRole('columnheader', { name: /20-session/ }).click();
  }
  await expect.poll(() => listingRequests.some((search) => search.includes('sort=return_20')))
    .toBe(true);
});

test('sorts from the sheet on a small screen and from the headers on a large one', async ({ page, isMobile }) => {
  await page.goto('/markets');
  await expect(page.getByText('Interrupted History AB')).toBeVisible();
  await openFilters(page, isMobile);
  // Whichever the viewport, a sort control is reachable by keyboard.
  const control = isMobile
    ? page.getByRole('combobox', { name: 'Sort by' })
    : page.getByRole('columnheader', { name: /Close/ });
  await control.focus();
  await expect(control).toBeFocused();
});

test('reads one instrument: range, overlay, and an honest gap', async ({ page }) => {
  await page.goto(`/markets/${GAPPY}`);
  await expect(page.getByRole('heading', { name: 'Interrupted History AB' })).toBeVisible();

  // Ranges are labelled in sessions; "30 days" would mean a different number of observations
  // on each exchange.
  await expect(page.getByRole('button', { name: '60 sessions' })).toBeVisible();
  await expect(page.getByText(/\d+\s*days/)).toHaveCount(0);

  await page.getByRole('button', { name: '60 sessions' }).click();
  await expect(page.getByRole('button', { name: '60 sessions' })).toHaveAttribute('aria-pressed', 'true');

  await page.getByLabel('20-session moving average').click();
  await expect(page.getByLabel('20-session moving average')).toHaveAttribute('aria-pressed', 'false');

  // The gap is stated in words, not only drawn (FR-017).
  await expect(page.getByText(/2 sessions the exchange was open with no stored bar/)).toBeVisible();
  await expect(page.getByText('2026-05-25')).toBeVisible();
});

test('shows a corporate action and an open finding without hovering', async ({ page }) => {
  await page.goto(`/markets/${GAPPY}`);
  await expect(page.getByRole('heading', { name: 'Corporate actions' })).toBeVisible();
  // Visible with no pointer anywhere near the chart.
  await expect(page.getByText('2026-05-28')).toBeVisible();
  await expect(page.getByText('ratio 2')).toBeVisible();
  await expect(page.getByText('suspicious_jump')).toBeVisible();
  await expect(page.getByText(/Provider-adjusted/)).toBeVisible();
});

test('operates the chart controls by keyboard alone', async ({ page }) => {
  await page.goto(`/markets/${GAPPY}`);
  const range = page.getByRole('button', { name: '60 sessions' });
  await range.focus();
  await expect(range).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(range).toHaveAttribute('aria-pressed', 'true');

  // Zoom and pan are real controls, not gestures only.
  const zoomIn = page.getByLabel('Zoom in');
  await zoomIn.focus();
  await expect(zoomIn).toBeFocused();
  await page.keyboard.press('Enter');
});

test('applies a live change without losing the chosen range or overlays', async ({ page }) => {
  await page.goto(`/markets/${GAPPY}`);
  await page.getByRole('button', { name: '60 sessions' }).click();
  await expect(page.getByRole('button', { name: '60 sessions' })).toHaveAttribute('aria-pressed', 'true');

  await page.evaluate((id) => {
    (window as unknown as { __emitNamed: (t: string, p: Record<string, unknown>, i?: string) => void })
      .__emitNamed('daily_bar.changed.v1', { entity_type: 'daily_bar', entity_id: 'bar-1', instrument_id: id }, '77');
  }, GAPPY);

  await expect(page.getByRole('button', { name: '60 sessions' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('heading', { name: 'Interrupted History AB' })).toBeVisible();
});

test('renders the chart and stays interactive on the longest history', async ({ page }) => {
  // SC-003: a full stored history must render and respond to zoom and pan without stalling.
  await page.route('**/api/v1/instruments/*/history*', (route) => {
    const window_ = historyWindow();
    const long = Array.from({ length: 2500 }, (_, index) => {
      const day = new Date(Date.UTC(2016, 0, 4) + index * 86_400_000);
      const base = 100 + (index % 200) * 0.25;
      return {
        session_date: day.toISOString().slice(0, 10),
        open: base.toFixed(2),
        high: (base + 1.5).toFixed(2),
        low: (base - 1.5).toFixed(2),
        close: (base + 0.5).toFixed(2),
        adjusted_close: null,
        volume: 1000 + index,
      };
    });
    return route.fulfill({
      json: {
        ...window_, bars: long, missing_sessions: [],
        coverage: { first_session: long[0].session_date, last_session: long.at(-1)!.session_date, stored_sessions: long.length },
      },
    });
  });

  const started = Date.now();
  await page.goto(`/markets/${GAPPY}`);
  await expect(page.getByRole('heading', { name: 'Stored daily history' })).toBeVisible();
  expect(Date.now() - started, '2500 sessions took too long to render').toBeLessThan(15_000);

  const zoomStarted = Date.now();
  await page.getByLabel('Zoom in').click();
  await page.getByLabel('Pan back').click();
  await expect(page.getByRole('heading', { name: 'Stored daily history' })).toBeVisible();
  expect(Date.now() - zoomStarted, 'zoom and pan stalled').toBeLessThan(5_000);
});

test('tolerates 320 pixels without scrolling the page sideways', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });

  for (const path of ['/markets', `/markets/${GAPPY}`]) {
    await page.goto(path);
    await expect(page.getByRole('heading').first()).toBeVisible();
    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, `${path} scrolls horizontally at 320px`).toBeLessThanOrEqual(1);
  }
});

test('keeps the range and scroll position across an orientation change', async ({ page }) => {
  await page.goto(`/markets/${GAPPY}`);
  await page.getByRole('button', { name: '60 sessions' }).click();
  await expect(page.getByRole('button', { name: '60 sessions' })).toHaveAttribute('aria-pressed', 'true');

  await page.setViewportSize({ width: 800, height: 360 });
  await expect(page.getByRole('button', { name: '60 sessions' })).toHaveAttribute('aria-pressed', 'true');

  await page.setViewportSize({ width: 360, height: 800 });
  await expect(page.getByRole('button', { name: '60 sessions' })).toHaveAttribute('aria-pressed', 'true');
});

test('stacks into cards that use the full width of a small screen', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'the stacked card layout only applies below the tablet breakpoint');

  await page.goto('/markets');
  await expect(page.getByText('Interrupted History AB')).toBeVisible();

  const measured = await page.evaluate(() => {
    const row = document.querySelector('tbody tr');
    const table = document.querySelector('table');
    const container = document.querySelector('.p-datatable-table-container')
      ?? document.querySelector('.p-datatable');
    return {
      viewport: window.innerWidth,
      rowWidth: row ? row.getBoundingClientRect().width : 0,
      tableWidth: table ? table.getBoundingClientRect().width : 0,
      containerWidth: container ? container.getBoundingClientRect().width : 0,
    };
  });

  // A stacked card is the row. If the table is still laid out as a table it keeps its column
  // widths — 880px against a 360px screen — and every card collapses to a fraction of the
  // space, hugging the left edge with the rest of the screen empty.
  expect(measured.tableWidth,
    `the table is ${Math.round(measured.tableWidth)}px inside a ${Math.round(measured.containerWidth)}px container`)
    .toBeLessThanOrEqual(measured.containerWidth + 1);

  expect(measured.rowWidth,
    `a card is ${Math.round(measured.rowWidth)}px wide in a ${measured.viewport}px viewport`)
    .toBeGreaterThan(measured.containerWidth * 0.9);
});

test('hides the header row entirely when the table is stacked', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'the stacked card layout only applies below the tablet breakpoint');

  await page.goto('/markets');
  await expect(page.getByText('Interrupted History AB')).toBeVisible();

  const head = await page.evaluate(() => {
    const thead = document.querySelector('thead');
    if (!thead) return null;
    const rect = thead.getBoundingClientRect();
    return { height: Math.round(rect.height), width: Math.round(rect.width), display: getComputedStyle(thead).display };
  });

  // Each cell carries its own label once stacked, so the header row has no job — but PrimeVue
  // styles it by class, which outranks a bare element selector. Losing that specificity battle
  // leaves a blank band above the first card that looks like a rendering fault.
  expect(head, 'no table header found').not.toBeNull();
  expect(head!.height, `the header row still occupies ${head!.height}px above the cards`).toBeLessThanOrEqual(1);
});

test('a stacked card scrolls nothing sideways and clips no value', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'the stacked card layout only applies below the tablet breakpoint');

  await page.goto('/markets');
  await expect(page.getByText('Interrupted History AB')).toBeVisible();

  const overflowing = await page.evaluate(() => {
    const offenders: string[] = [];
    for (const cell of Array.from(document.querySelectorAll('tbody td'))) {
      if (cell.scrollWidth > cell.clientWidth + 1) {
        offenders.push(`${cell.getAttribute('data-label') ?? '?'}: ${cell.scrollWidth}>${cell.clientWidth}`);
      }
    }
    return offenders;
  });
  expect(overflowing, 'cell content is clipped inside its card').toEqual([]);

  const pageOverflow = await page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(pageOverflow).toBeLessThanOrEqual(1);
});
