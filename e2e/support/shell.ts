import { expect, type Locator, type Page } from '@playwright/test';

/**
 * Reaching the shell's own controls, at any width.
 *
 * Below the tablet breakpoint the destinations, the build number and the theme control live in a
 * drawer rather than in the bar: nine destinations wrapped onto three rows took a third of a phone
 * screen before the page began. Every spec that wants one of them asks for it here, so the
 * interaction is described in one place instead of in nine.
 */

function menuButton(page: Page): Locator {
  return page.getByRole('button', { name: /open navigation menu/i });
}

/**
 * True when this viewport keeps the shell's controls behind a tap.
 *
 * Visibility, not presence: which treatment applies is decided in CSS, so both the menu control
 * and the inline row are in the document at every width and exactly one of them is displayed.
 */
export async function controlsAreBehindAMenu(page: Page): Promise<boolean> {
  return menuButton(page).isVisible();
}

/**
 * Makes the shell's controls reachable, opening the drawer when the viewport calls for it.
 *
 * Waits for the drawer to stop moving: it slides in, and a box measured the instant after the tap
 * is a box mid-flight, which reads as a control sitting outside the viewport.
 */
export async function revealShellControls(page: Page): Promise<void> {
  if (!await controlsAreBehindAMenu(page)) return;
  const drawer = page.locator('.app-drawer');
  if (await drawer.isVisible().catch(() => false)) return;
  await menuButton(page).click();
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

/** Closes the drawer if it is open, so the page underneath can be read again. */
export async function dismissShellControls(page: Page): Promise<void> {
  const drawer = page.locator('.app-drawer');
  if (!await drawer.isVisible().catch(() => false)) return;
  await page.getByRole('button', { name: /close/i }).first().click();
  await expect(drawer).toBeHidden();
}

/** Advances the colour theme by one, wherever the control happens to live. */
export async function cycleTheme(page: Page): Promise<void> {
  await revealShellControls(page);
  await page.getByRole('button', { name: 'Change color theme' }).click();
  await dismissShellControls(page);
}

/** What the theme control currently says, wherever it happens to live. */
export async function themeLabel(page: Page): Promise<string> {
  await revealShellControls(page);
  const label = await page.getByRole('button', { name: 'Change color theme' }).textContent();
  await dismissShellControls(page);
  return label ?? '';
}

/**
 * The build number, wherever it happens to live.
 *
 * Scoped like the navigation, and for the same reason: the copy that is not showing is hidden with
 * CSS rather than removed, and a text lookup finds hidden text too.
 */
export async function shellVersion(page: Page): Promise<Locator> {
  const behindAMenu = await controlsAreBehindAMenu(page);
  await revealShellControls(page);
  return behindAMenu
    ? page.locator('.app-drawer .app-version')
    : page.locator('.app-actions--inline .app-version');
}

/**
 * The navigation link for a destination, revealed first if this viewport hides it.
 *
 * Scoped to whichever navigation is actually showing. Two things make an unscoped lookup wrong:
 * pages link to each other (the market-data screen links to operations), and while the drawer is
 * open its copy of the destinations sits alongside the bar's.
 */
export async function navigationLink(page: Page, name: string): Promise<Locator> {
  const behindAMenu = await controlsAreBehindAMenu(page);
  await revealShellControls(page);
  const nav = behindAMenu
    ? page.locator('.app-drawer .primary-nav')
    : page.locator('.primary-nav--inline');
  return nav.getByRole('link', { name, exact: true });
}
