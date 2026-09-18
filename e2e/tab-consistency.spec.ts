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
  { path: '/intents', name: 'Intents' },
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

/**
 * Opens the drawer and waits for it to stop moving.
 *
 * It slides in, so a box measured the instant after the tap is a box mid-flight — which reads as
 * a link sitting outside the viewport when it is merely on its way in.
 */
async function openMenu(page: Page, menu: ReturnType<Page['getByRole']>): Promise<void> {
  await menu.click();
  const drawer = page.locator('.app-drawer');
  await expect(drawer).toBeVisible();
  await page.waitForFunction(() => {
    // The panel and the mask behind it animate separately, and the mask is what swallows a click
    // while it is still fading in. Waiting on the panel alone let a tap land on the overlay.
    const mask = document.querySelector('.p-drawer-mask');
    if (!mask) return true;
    return mask.getAnimations({ subtree: true })
      .every((animation) => animation.playState === 'finished');
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

/**
 * Every destination in the primary navigation is actually clickable, at every supported width.
 *
 * This exists because adding one has broken it twice. The header carried a fixed height with
 * flex-wrap, so once the links stopped fitting on one row the wrapped row was laid out and then
 * clipped: the link was on the page, focusable, and unclickable. It happened at 360px with four
 * links and again at 768px with seven, and the second time only CI caught it — Linux renders the
 * font fractionally wider than macOS, so the local suite passed.
 *
 * A link that is present but unreachable is worse than one that is absent, because nothing looks
 * wrong. Counting them is not enough; each one has to be hit.
 */
for (const width of [1440, 1024, 768, 390, 320]) {
  test(`every navigation destination can be clicked at ${width}`, async ({ page }) => {
    await stub(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/');

    // Below the tablet breakpoint the destinations live in a drawer, because nine of them wrapped
    // onto three rows and took a third of a phone screen. The question this test asks is unchanged
    // — can every destination actually be reached — but on a phone it takes one tap to get there.
    const menu = page.getByRole('button', { name: /open navigation menu/i });
    // Visibility, not presence: the CSS decides which treatment applies, so both are present.
    const behindAMenu = await menu.isVisible();
    if (behindAMenu) await openMenu(page, menu);

    const nav = page.locator('.primary-nav');
    const links = nav.getByRole('link');
    const count = await links.count();
    expect(count).toBeGreaterThan(5);

    for (let index = 0; index < count; index += 1) {
      const link = links.nth(index);
      const name = (await link.textContent())?.trim() ?? `link ${index}`;
      const href = await link.getAttribute('href');

      // Visible, hit-testable, and inside the viewport — not merely present in the DOM.
      await expect(link, `${name} is not visible at ${width}`).toBeVisible();
      const box = await link.boundingBox();
      expect(box, `${name} has no box at ${width}`).not.toBeNull();
      expect(box!.x, `${name} starts off the left edge at ${width}`).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width, `${name} runs past the right edge at ${width}`)
        .toBeLessThanOrEqual(width + 1);

      // The header must not be clipping the row it wrapped onto.
      const clipped = await link.evaluate((element) => {
        const header = element.closest('header');
        if (!header) return false;
        const linkBox = element.getBoundingClientRect();
        const headerBox = header.getBoundingClientRect();
        return linkBox.bottom > headerBox.bottom + 1;
      });
      expect(clipped, `${name} is clipped by the header at ${width}`).toBe(false);

      // The destination is what matters; a view is free to add query parameters of its own once
      // it arrives, as the markets listing does.
      await link.click();
      await expect(page).toHaveURL(new RegExp(`${href}(\\?|$)`));

      // Arriving closes the drawer, so the next destination needs it opened again. That it closes
      // at all is the point: a menu still covering the page you asked for is the usual bug here.
      if (behindAMenu) {
        await expect(menu, 'the menu did not close on arrival').toBeVisible();
        await openMenu(page, menu);
      }
    }
  });
}
