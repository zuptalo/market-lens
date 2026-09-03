import { test, expect, type Page } from '@playwright/test';

/**
 * Switching tabs must not move the page under the reader.
 *
 * Every destination in the primary navigation had grown its own header treatment: three different
 * title sizes across five tabs, four different vertical starting points, and two views adding
 * their own padding on top of the shell's. Moving between them shifted the first heading by up to
 * twenty-four pixels vertically and sixteen horizontally — small enough that nobody could name it,
 * large enough that the interface felt loose.
 */

const TABS = [
  { path: '/', name: 'Overview' },
  { path: '/markets', name: 'Market data' },
  { path: '/signals', name: 'Signals' },
  { path: '/operations', name: 'Operations' },
  { path: '/account', name: 'Account' },
];

async function stub(page: Page): Promise<void> {
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
  await page.route('**/api/v1/**', (route) => {
    const url = route.request().url();
    if (url.includes('/account')) return route.fallback();
    if (url.includes('/signals')) {
      return route.fulfill({ json: {
        items: [], next_cursor: null, total: 0, session_date: '2026-09-02', scored: 0, unscored: 0,
        strategy: { name: 'momentum_trend', version: 1, title: 'Momentum and trend', caveat: 'Stated, not fitted.', superseded: false },
      } });
    }
    if (url.includes('/instruments')) return route.fulfill({ json: { items: [], next_cursor: null, total: 0 } });
    return route.fulfill({ json: { items: [], next_cursor: '', members: [] } });
  });
}

/** Where the page's own title sits, and how large it is. */
async function heading(page: Page) {
  return page.evaluate(() => {
    const h1 = document.querySelector('main h1');
    if (!h1) return null;
    const box = h1.getBoundingClientRect();
    return { top: Math.round(box.top), left: Math.round(box.left), fontSize: getComputedStyle(h1).fontSize };
  });
}

for (const width of [1440, 768, 390]) {
  test(`every tab starts in the same place at ${width}`, async ({ page }) => {
    await stub(page);
    await page.setViewportSize({ width, height: 900 });

    const measured: Record<string, Awaited<ReturnType<typeof heading>>> = {};
    for (const tab of TABS) {
      await page.goto(tab.path);
      await expect(page.locator('main h1')).toBeVisible();
      measured[tab.name] = await heading(page);
    }

    const reference = measured['Market data'];
    expect(reference).not.toBeNull();
    for (const tab of TABS) {
      expect(measured[tab.name], `${tab.name} has no page title`).not.toBeNull();
      expect(measured[tab.name]!.top, `${tab.name} starts at a different height`).toBe(reference!.top);
      expect(measured[tab.name]!.left, `${tab.name} starts at a different left edge`).toBe(reference!.left);
      expect(measured[tab.name]!.fontSize, `${tab.name} sizes its title differently`).toBe(reference!.fontSize);
    }
  });
}

/** How far the widest thing on the page reaches, against how far it is allowed to. */
async function contentReach(page: Page) {
  return page.evaluate(() => {
    const main = document.querySelector('main.app-content') as HTMLElement;
    const style = getComputedStyle(main);
    const limit = main.getBoundingClientRect().right - parseFloat(style.paddingRight);
    let widest = 0;
    let widestLabel = '';
    for (const el of Array.from(main.querySelectorAll('*'))) {
      const box = el.getBoundingClientRect();
      if (box.width === 0 || box.height === 0) continue;
      if (box.right > widest) {
        widest = box.right;
        widestLabel = `${el.tagName.toLowerCase()}.${(el as HTMLElement).className || ''}`.slice(0, 60);
      }
    }
    return { limit: Math.round(limit), widest: Math.round(widest), widestLabel };
  });
}

// Each tab used to stop at a different right edge — the account panels at 64rem, the ranking at
// 84rem, the overview card at 42rem, while market data and operations ran to the full width. The
// device column was the visible cost: identifiers wrapped onto two lines inside a table that had
// been given less room than the page had to give.
test('every tab uses the same content width', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 1600, height: 900 });
  for (const tab of TABS) {
    await page.goto(tab.path);
    await expect(page.locator('main h1')).toBeVisible();
    const reach = await contentReach(page);
    expect(reach.widest, `${tab.name} stops ${reach.limit - reach.widest}px short of the page width (widest: ${reach.widestLabel})`)
      .toBeGreaterThanOrEqual(reach.limit - 1);
  }
});

// A page taller than the window takes a scrollbar and a short one does not, which moves every
// tab's content sideways on the way in. Reserving the gutter costs nothing and stops it.
test('a scrollbar appearing does not move the content sideways', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/markets');
  await expect(page.locator('main h1')).toBeVisible();
  const gutter = await page.evaluate(() => getComputedStyle(document.documentElement).scrollbarGutter);
  expect(gutter).toBe('stable');
});
