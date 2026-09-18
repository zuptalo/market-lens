import { expect, test, type Page } from '@playwright/test';
import { ROUTES, api } from './fixtures/api.mjs';

/**
 * A phone must never have to scroll sideways to read a page.
 *
 * This is stricter than it sounds, and it is the guard the product was missing. A page wider than
 * the window does not merely look untidy: mobile Safari responds by shrinking the whole document
 * to fit, so the header stops reaching the edges, every type size drops, and the reader is left
 * pinching. It looks like a broken layout rather than an overflowing table, which is why it went
 * unnoticed — the existing checks only looked at the four screens their own feature owned.
 *
 * Contained scrolling is still allowed and still right: a table of twelve columns scrolls inside
 * its own box. What is forbidden is the page itself scrolling, which is the distinction between a
 * component that handles being narrow and one that does not.
 */

const WIDTHS = [320, 390];

async function stub(page: Page): Promise<void> {
  await page.addInitScript(() => {
    class Quiet extends EventTarget {
      constructor() { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: Quiet });
  });
  await page.route('**/api/v1/**', (route) => {
    const { pathname } = new URL(route.request().url());
    return route.fulfill({ json: api(pathname) as object });
  });
}

/** What reaches past the right edge, named, so a failure says which element to look at. */
async function sideways(page: Page) {
  return page.evaluate(() => {
    const root = document.documentElement;
    const limit = root.clientWidth + 1;
    const offenders: string[] = [];
    for (const element of Array.from(document.querySelectorAll('*'))) {
      const box = element.getBoundingClientRect();
      if (box.width === 0 || box.height === 0 || box.right <= limit) continue;
      const childOverflows = Array.from(element.children).some((child) => {
        const childBox = child.getBoundingClientRect();
        return childBox.width > 0 && childBox.height > 0 && childBox.right > limit;
      });
      // Only the innermost offender is reported: every ancestor is merely carrying it.
      if (!childOverflows) {
        const name = `${element.tagName.toLowerCase()}.${String((element as HTMLElement).className || '').split(' ')[0]}`;
        offenders.push(`${name} reaches ${Math.round(box.right)}`);
      }
    }
    return { clientWidth: root.clientWidth, scrollWidth: root.scrollWidth, offenders: offenders.slice(0, 6) };
  });
}

for (const width of WIDTHS) {
  for (const route of ROUTES) {
    test(`${route.name} does not scroll sideways at ${width}`, async ({ page }) => {
      await stub(page);
      await page.setViewportSize({ width, height: 800 });
      await page.goto(route.path);
      await expect(page.locator('main h1').first()).toBeVisible();
      // Charts and tables settle a frame late; the measurement has to be of the settled page.
      await page.waitForTimeout(250);

      const measured = await sideways(page);
      expect(
        measured.scrollWidth,
        `${route.name} is ${measured.scrollWidth - measured.clientWidth}px too wide at ${width}. `
        + `Innermost offenders: ${measured.offenders.join('; ') || 'none reported'}`,
      ).toBeLessThanOrEqual(measured.clientWidth);
    });
  }
}

/**
 * A table on a phone is a stack of labelled blocks, not a sideways-scrolling grid.
 *
 * Contained scrolling keeps the *page* honest, which is why it passes the check above — but six
 * columns of figures in a 360-pixel box is not readable, and the controls in the last column sit
 * off-screen until you drag. The project already owns the stacked treatment; eleven tables simply
 * never reached it, because they asked for it with `responsive-layout="stack"`, a PrimeVue 3 prop
 * that PrimeVue 4 does not have and silently ignores.
 */
for (const route of ROUTES) {
  test(`${route.name} stacks its tables rather than scrolling them at 390`, async ({ page }) => {
    await stub(page);
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto(route.path);
    await expect(page.locator('main h1').first()).toBeVisible();
    await page.waitForTimeout(250);

    const cramped = await page.evaluate(() => Array.from(
      document.querySelectorAll('.p-datatable-table-container, .p-datatable-wrapper'),
    )
      .filter((box) => box.scrollWidth > box.clientWidth + 1)
      .map((box) => {
        const table = box.closest('.p-datatable');
        const label = table?.getAttribute('data-testid') ?? table?.className ?? 'table';
        return `${label} needs ${box.scrollWidth - box.clientWidth}px more`;
      }));

    expect(cramped, `these scroll sideways instead of stacking: ${cramped.join('; ')}`).toEqual([]);
  });
}

/**
 * The header is navigation, not content. Nine destinations wrapped onto three rows took a third of
 * a phone screen before the page began, which is the cost of treating a desktop header as though
 * it were responsive merely because it wraps.
 */
test('the header leaves the page most of the screen at 390', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/');
  await expect(page.locator('main h1').first()).toBeVisible();

  const header = await page.locator('.app-header').boundingBox();
  expect(header, 'no header').not.toBeNull();
  expect(header!.height, 'the header takes more than a fifth of the screen').toBeLessThanOrEqual(160);
});

/**
 * Every destination stays reachable on a phone, behind one deliberate tap rather than three rows
 * of wrapped links. Hiding them would be the easy way to make the header short and the wrong one:
 * a destination nobody can reach is worse than a tall header.
 */
test('every destination is one tap away on a phone', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/');
  await expect(page.locator('main h1').first()).toBeVisible();

  const open = page.getByRole('button', { name: /open navigation menu/i });
  await expect(open, 'no way to open the navigation on a phone').toBeVisible();

  // A target somebody can hit with a thumb. 44 CSS pixels is the long-standing floor.
  const box = await open.boundingBox();
  expect(Math.min(box!.width, box!.height), 'the menu control is too small to tap')
    .toBeGreaterThanOrEqual(44);

  await open.click();
  const destinations = page.getByRole('navigation', { name: /primary/i }).getByRole('link');
  await expect(destinations.first()).toBeVisible();
  expect(await destinations.count(), 'not every destination is in the menu').toBeGreaterThanOrEqual(9);
  for (const name of ['Overview', 'Market data', 'Signals', 'Portfolio', 'Limits', 'Intents',
    'Backtests', 'Operations', 'Account']) {
    await expect(page.getByRole('link', { name, exact: true })).toBeVisible();
  }
});

// The phone treatment must not follow a laptop home. There is room for the links at desktop width,
// and putting them behind a tap there would cost a click for nothing.
test('a wide screen shows the destinations without a menu', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await expect(page.locator('main h1').first()).toBeVisible();

  // Present in the document at every width, because the choice is made in CSS — but never shown
  // where there is room for the destinations themselves.
  await expect(page.getByRole('button', { name: /open navigation menu/i })).toBeHidden();
  await expect(page.getByRole('link', { name: 'Backtests', exact: true })).toBeVisible();
});

// Navigation that scrolls off the top is navigation you have to scroll back for. On a long page
// this is the difference between one tap and a flick plus a tap.
test('the header stays put while the page scrolls', async ({ page }) => {
  await stub(page);
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/markets');
  await expect(page.locator('main h1').first()).toBeVisible();

  await page.evaluate(() => window.scrollTo(0, 600));
  await page.waitForTimeout(100);
  const box = await page.locator('.app-header').boundingBox();
  expect(box!.y, 'the header scrolled away').toBeLessThanOrEqual(1);
});
