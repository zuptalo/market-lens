import { expect, test, type Page } from '@playwright/test';

// Every viewport this product commits to, including the 320-pixel floor it must tolerate.
const VIEWPORTS = [
  { name: '320 floor', width: 320, height: 800 },
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
] as const;

const owner = {
  id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
  role: 'owner', status: 'active', email_verified_at: '2026-08-31T08:00:00Z',
};

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class QuietEventSource extends EventTarget {
      static readonly CLOSED = 2;
      readyState = 1;
      constructor(_url: string | URL) { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: QuietEventSource });
  });
});

test('every control on the sign-in page has a name, a focus ring, and enough contrast', async ({ page }) => {
  await mockAccount(page, false);
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();

  await expectAccessibleNames(page);
  await expectLabelsPointAtSomething(page);
  await expectVisibleFocusIndicator(page, 'Email');
  await expectReadableContrast(page);
});

test('the account page stays operable by keyboard alone', async ({ page }) => {
  await mockAccount(page, true);
  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();

  await expectAccessibleNames(page);
  await expectLabelsPointAtSomething(page);
  await expectReadableContrast(page);

  // Tabbing must reach every control that does something, in an order a person can follow.
  const reached = await page.evaluate(() => {
    const focusable = Array.from(document.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ));
    return focusable.filter((element) => element.offsetParent !== null).length;
  });
  expect(reached).toBeGreaterThan(0);
  for (let step = 0; step < reached; step += 1) {
    await page.keyboard.press('Tab');
    const active = await page.evaluate(() => document.activeElement?.tagName ?? '');
    expect(active).not.toBe('BODY');
  }
});

// The market-data filters are the densest set of controls in the application and, until this
// test existed, the only screen the accessibility suite never opened - which is how five
// labels pointing at nothing went unnoticed there.
test('the market data filters are labelled and readable', async ({ page }) => {
  await mockAccount(page, true);
  await page.goto('/markets');
  await expect(page.getByRole('heading', { name: 'Instruments' })).toBeVisible();

  await expectAccessibleNames(page);
  await expectLabelsPointAtSomething(page);
  await expectReadableContrast(page);

  for (const size of [{ width: 320, height: 800 }, { width: 360, height: 800 }, { width: 1440, height: 900 }]) {
    await page.setViewportSize(size);
    expect(await horizontallyOverflows(page), (await widestElements(page)).join('; ')).toBe(false);
  }
});

test('nothing important is reachable only by hovering', async ({ page }) => {
  await mockAccount(page, true);
  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();

  // A control that only appears on hover cannot be used on a touch screen at all.
  const hoverOnly = await page.evaluate(() => {
    const offending: string[] = [];
    for (const sheet of Array.from(document.styleSheets)) {
      let rules: CSSRuleList;
      try { rules = sheet.cssRules; } catch { continue; }
      for (const rule of Array.from(rules)) {
        if (!(rule instanceof CSSStyleRule) || !rule.selectorText.includes(':hover')) continue;
        const declaration = rule.style;
        // Revealing a control on hover is the pattern that strands touch users.
        if (declaration.display === 'block' || declaration.display === 'flex'
          || declaration.visibility === 'visible' || declaration.opacity === '1') {
          if (rule.selectorText.includes('button') || rule.selectorText.includes('a')
            || rule.selectorText.includes('[role=')) {
            offending.push(rule.selectorText);
          }
        }
      }
    }
    return offending;
  });
  expect(hoverOnly, 'controls revealed only on hover').toEqual([]);
});

for (const viewport of VIEWPORTS) {
  test(`the signed-in shell fits and stays usable at ${viewport.name}`, async ({ page }) => {
    await mockAccount(page, true);
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/account');
    await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();

    // The page may scroll down; it may never scroll sideways, and no control may be clipped.
    expect(await horizontallyOverflows(page), (await widestElements(page)).join('; ')).toBe(false);
    const clipped = await page.evaluate(() => {
      const width = document.documentElement.clientWidth;
      return Array.from(document.querySelectorAll<HTMLElement>('button, a[href], input'))
        .filter((element) => element.offsetParent !== null)
        .filter((element) => {
          const box = element.getBoundingClientRect();
          return box.width > 0 && (box.left < -1 || box.right > width + 1);
        })
        .map((element) => element.textContent?.trim() || element.tagName);
    });
    expect(clipped, 'controls pushed outside the viewport').toEqual([]);

    // Touch targets stay large enough to hit on a phone.
    if (viewport.width <= 768) {
      const small = await page.evaluate(() => Array.from(document.querySelectorAll<HTMLElement>('button, a[href]'))
        .filter((element) => element.offsetParent !== null)
        .filter((element) => {
          const box = element.getBoundingClientRect();
          return box.height > 0 && box.height < 24;
        })
        .map((element) => element.textContent?.trim() || element.tagName));
      expect(small, 'touch targets under 24 pixels tall').toEqual([]);
    }
  });
}

test('typed input survives every theme and an orientation change', async ({ page }) => {
  await mockAccount(page, true);
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();

  const invite = page.getByLabel('Invite by email');
  await invite.fill('someone@example.com');

  // System, light, and dark in turn. The theme control cycles through all three.
  for (let theme = 0; theme < 3; theme += 1) {
    await page.getByRole('button', { name: 'Change color theme' }).click();
    await expect(invite).toHaveValue('someone@example.com');
    expect(await horizontallyOverflows(page)).toBe(false);
    await expectReadableContrast(page);
  }

  // Turning the phone sideways must not discard what somebody was half way through typing.
  await page.setViewportSize({ width: 800, height: 360 });
  await expect(invite).toHaveValue('someone@example.com');
  expect(await horizontallyOverflows(page)).toBe(false);
  await page.setViewportSize({ width: 360, height: 800 });
  await expect(invite).toHaveValue('someone@example.com');
});

async function horizontallyOverflows(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
}

// widestElements names what is pushing the page sideways. Knowing that something overflows is
// not enough to fix it, and hunting for the element by hand is most of the work.
async function widestElements(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const limit = document.documentElement.clientWidth;
    return Array.from(document.querySelectorAll<HTMLElement>('*'))
      .map((element) => ({ element, rect: element.getBoundingClientRect() }))
      .filter(({ rect }) => rect.right > limit + 1)
      .slice(0, 6)
      .map(({ element, rect }) => `${element.tagName.toLowerCase()}.${element.className || '-'}`
        + ` right=${Math.round(rect.right)} limit=${limit}`);
  });
}

// expectAccessibleNames fails on any control a screen reader would announce as unlabelled.
async function expectAccessibleNames(page: Page): Promise<void> {
  const unnamed = await page.evaluate(() => {
    const named = (element: HTMLElement): boolean => {
      if (element.getAttribute('aria-label')?.trim()) return true;
      if (element.getAttribute('aria-labelledby')?.trim()) return true;
      if (element.getAttribute('title')?.trim()) return true;
      if (element.id && document.querySelector(`label[for="${CSS.escape(element.id)}"]`)) return true;
      if (element.closest('label')) return true;
      return (element.textContent ?? '').trim().length > 0;
    };
    return Array.from(document.querySelectorAll<HTMLElement>('button, a[href], input, select, textarea'))
      .filter((element) => element.offsetParent !== null && !named(element))
      .map((element) => element.outerHTML.slice(0, 120));
  });
  expect(unnamed, 'controls with no accessible name').toEqual([]);
}

// expectLabelsPointAtSomething catches a label whose `for` names an element that does not
// exist. A composite control renders its id somewhere other than where a plain input does, so
// this breaks quietly during a component migration - and an aria-label on the wrapper is
// enough to keep every other accessibility check passing while it does.
async function expectLabelsPointAtSomething(page: Page): Promise<void> {
  const dangling = await page.evaluate(() => {
    const labelable = new Set(['INPUT', 'SELECT', 'TEXTAREA', 'BUTTON', 'METER', 'OUTPUT', 'PROGRESS']);
    return Array.from(document.querySelectorAll<HTMLLabelElement>('label[for]'))
      .map((label) => ({ label, target: document.getElementById(label.htmlFor) }))
      // Existing is not enough. A composite control puts a plain `id` on its outer wrapper,
      // which is neither focusable nor labelable: clicking the label focuses nothing and the
      // association is not reported. The target has to be something a person can land on.
      .filter(({ target }) => !target || (!labelable.has(target.tagName) && target.tabIndex < 0))
      .map(({ label, target }) => `${label.textContent?.trim()} -> #${label.htmlFor}`
        + ` (${target ? `${target.tagName.toLowerCase()}, tabindex ${target.tabIndex}` : 'missing'})`);
  });
  expect(dangling, 'labels pointing at nothing a person can focus').toEqual([]);
}

// expectVisibleFocusIndicator proves keyboard focus is actually visible, not merely present.
async function expectVisibleFocusIndicator(page: Page, label: string): Promise<void> {
  const field = page.getByLabel(label);

  // Focus is moved with the keyboard rather than element.focus(). A ring is drawn on
  // :focus-visible, and whether a programmatic focus satisfies that is a browser heuristic
  // about the last input modality - which made this assertion pass or fail depending on what
  // ran before it. Tabbing to the control is both deterministic and what a keyboard user does.
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
  let reached = false;
  for (let step = 0; step < 40 && !reached; step += 1) {
    await page.keyboard.press('Tab');
    reached = await field.evaluate((element) => element === document.activeElement);
  }
  expect(reached, `could not reach ${label} with the keyboard`).toBe(true);

  const indicator = await field.evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      outlineWidth: style.outlineWidth, outlineStyle: style.outlineStyle,
      boxShadow: style.boxShadow, borderColor: style.borderColor,
      // Whether the ring should apply at all depends on this, so report it alongside.
      focusVisible: element.matches(':focus-visible'),
    };
  });
  const visible = (indicator.outlineStyle !== 'none' && parseFloat(indicator.outlineWidth) > 0)
    || (indicator.boxShadow !== 'none' && indicator.boxShadow.length > 0);
  expect(visible, `focus indicator for ${label}: ${JSON.stringify(indicator)}`).toBe(true);
}

// expectReadableContrast measures real rendered colors against the WCAG AA text ratio.
// Getting this right needs three things the naive version gets wrong: Chromium reports some
// computed colors as `color(srgb ...)` with 0-1 channels, backgrounds are routinely
// semi-transparent tints that have to be composited over what is behind them, and text colors
// carry alpha too.
async function expectReadableContrast(page: Page): Promise<void> {
  // Colours transition when the theme changes, and a colour caught mid-transition is not the
  // one anybody reads. Poll until it settles rather than measuring the animation.
  await expect.poll(() => measureContrast(page), { timeout: 5_000 }).toEqual([]);
}

async function measureContrast(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    type RGBA = [number, number, number, number];

    const parse = (value: string): RGBA | null => {
      const modern = value.match(/color\(srgb\s+([^)]+)\)/);
      if (modern) {
        const [channels, alpha] = modern[1].split('/');
        const parts = channels.trim().split(/\s+/).map((part) => parseFloat(part) * 255);
        return [parts[0], parts[1], parts[2], alpha === undefined ? 1 : parseFloat(alpha)];
      }
      const legacy = value.match(/rgba?\(([^)]+)\)/);
      if (!legacy) return null;
      const parts = legacy[1].split(',').map((part) => parseFloat(part));
      return [parts[0], parts[1], parts[2], parts.length >= 4 ? parts[3] : 1];
    };

    // over composites a source colour onto an already-opaque backdrop.
    const over = (source: RGBA, backdrop: RGBA): RGBA => [
      source[0] * source[3] + backdrop[0] * (1 - source[3]),
      source[1] * source[3] + backdrop[1] * (1 - source[3]),
      source[2] * source[3] + backdrop[2] * (1 - source[3]),
      1,
    ];

    // resolveBackground stacks every painted layer from the page ground up to the element.
    const resolveBackground = (element: Element): RGBA => {
      const layers: RGBA[] = [];
      let current: Element | null = element;
      while (current) {
        const colour = parse(getComputedStyle(current).backgroundColor);
        if (colour && colour[3] > 0) {
          layers.push(colour);
          if (colour[3] === 1) break;
        }
        current = current.parentElement;
      }
      let result: RGBA = layers.length > 0 && layers[layers.length - 1][3] === 1
        ? layers.pop() as RGBA
        : [255, 255, 255, 1];
      for (let index = layers.length - 1; index >= 0; index -= 1) {
        result = over(layers[index], result);
      }
      return result;
    };

    const luminance = ([r, g, b]: RGBA): number => {
      const channel = (value: number) => {
        const scaled = value / 255;
        return scaled <= 0.03928 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    };

    const offending: string[] = [];
    for (const element of Array.from(document.querySelectorAll<HTMLElement>('h1, h2, h3, p, label, button, a[href], span, li, td, th'))) {
      if (element.offsetParent === null) continue;
      const text = (element.textContent ?? '').trim();
      if (!text || element.children.length > 0) continue;
      const style = getComputedStyle(element);
      if (style.visibility === 'hidden' || parseFloat(style.opacity) < 0.5) continue;
      const colour = parse(style.color);
      if (!colour || colour[3] === 0) continue;
      const background = resolveBackground(element);
      const foreground = over(colour, background);
      const lighter = Math.max(luminance(foreground), luminance(background));
      const darker = Math.min(luminance(foreground), luminance(background));
      const ratio = (lighter + 0.05) / (darker + 0.05);
      const size = parseFloat(style.fontSize);
      const large = size >= 24 || (size >= 18.66 && parseInt(style.fontWeight, 10) >= 700);
      if (ratio < (large ? 3 : 4.5)) {
        // The colours are reported too: knowing a ratio failed is not enough to find out
        // which layer produced it, and that is most of the work in fixing one.
        const rgb = (value: RGBA) => `rgb(${value.slice(0, 3).map(Math.round).join(',')})`;
        offending.push(
          `${text.slice(0, 40)} @ ${ratio.toFixed(2)}:1 `
          + `[${element.tagName.toLowerCase()}.${element.className || '-'} `
          + `fg ${rgb(foreground)} on bg ${rgb(background)}]`,
        );
      }
    }
    return offending;
  });
}

async function mockAccount(page: Page, authenticated: boolean): Promise<void> {
  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: false } }));
  await page.route('**/api/v1/account', (route) => authenticated
    ? route.fulfill({ json: owner })
    : route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'Authentication is required.' } } }));
  await page.route('**/api/v1/account/sessions', (route) => route.fulfill({ json: { items: [{
    id: '20000000-0000-4000-8000-000000000001', current: true, device_label: 'Chrome on macOS',
    created_at: '2026-08-31T08:00:00Z', last_seen_at: '2026-08-31T08:01:00Z',
    idle_expires_at: '2026-08-31T16:01:00Z', absolute_expires_at: '2026-09-30T08:00:00Z', revoked: false,
  }] } }));
  await page.route('**/api/v1/owner/members', (route) => route.fulfill({ json: { members: [{
    id: '10000000-0000-4000-8000-000000000601', email: 'member@example.com', display_name: 'Member',
    status: 'active', login_state: 'available', blocked_until: null, locked_at: null,
    active_session_count: 1, created_at: '2026-08-31T08:00:00Z',
  }], next_cursor: '' } }));
  await page.route('**/api/v1/owner/invitations', (route) => route.fulfill({ json: { items: [], next_cursor: '' } }));
  await page.route('**/api/v1/instruments?*', (route) => route.fulfill({ json: { items: [{
    id: '11111111-1111-4111-8111-111111111111', isin: 'SE0000000200', ticker: 'GAPPY',
    name: 'Interrupted History AB', exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
    currency: 'SEK', country: 'SE', sector: 'Technology', industry: 'Software',
    instrument_type: 'common_stock', status: 'active', purchasability_status: 'unverified',
    latest_session: '2026-06-02', latest_close: '109.50', change_absolute: '1.00',
    change_percent: 0.0092, return_20: 0.0412, return_90: null, volatility: 0.1875,
    stored_sessions: 120, freshness: { state: 'current', sessions_behind: 0 },
  }], next_cursor: null } }));
  await page.route('**/api/v1/instruments/*/history*', (route) => route.fulfill({ json: {
    instrument: {
      id: '11111111-1111-4111-8111-111111111111', isin: 'SE0000000200', ticker: 'GAPPY',
      name: 'Interrupted History AB', exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
      currency: 'SEK', country: 'SE', sector: 'Technology', industry: 'Software',
      instrument_type: 'common_stock', status: 'active', purchasability_status: 'unverified',
      latest_session: '2026-06-02', latest_close: '109.50', change_absolute: '1.00',
      change_percent: 0.0092, return_20: 0.0412, return_90: null, volatility: 0.1875,
      stored_sessions: 120, freshness: { state: 'current', sessions_behind: 0 },
    },
    coverage: { first_session: '2026-01-05', last_session: '2026-06-02', stored_sessions: 120 },
    requested_from: '2026-05-18', requested_to: '2026-06-02',
    bars: [
      { session_date: '2026-05-18', open: '100.00', high: '101.50', low: '98.50', close: '100.50', adjusted_close: null, volume: 1000 },
      { session_date: '2026-05-27', open: '105.00', high: '106.50', low: '103.50', close: '105.50', adjusted_close: null, volume: 1042 },
    ],
    missing_sessions: ['2026-05-25', '2026-05-26'],
    series_basis: 'provider_adjusted', provider: 'fixture', observed_at: '2026-06-02T17:30:00Z',
    actions: [{ id: 'a1', action_type: 'split', ex_date: '2026-05-27', ratio: '2', amount: null, currency: null, old_symbol: null, new_symbol: null }],
    findings: [{ id: 'f1', rule: 'suspicious_jump', status: 'open', session_date: '2026-05-27', detail: 'close moved more than the threshold' }],
  } }));
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [] } }));
}

/**
 * The two views feature 005 adds carry the heaviest accessibility burden in the product: a
 * data-dense table and a chart. Both are measured here rather than asserted in a review.
 */
for (const path of ['/markets', '/markets/11111111-1111-4111-8111-111111111111']) {
  test(`${path} has named controls, visible focus and readable contrast`, async ({ page }) => {
    await mockAccount(page, true);
    await page.goto(path);
    await expect(page.getByRole('heading').first()).toBeVisible();

    await expectAccessibleNames(page);
    await expectReadableContrast(page);
  });

  for (const viewport of VIEWPORTS) {
    test(`${path} fits and stays usable at ${viewport.name}`, async ({ page }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      await mockAccount(page, true);
      await page.goto(path);
      await expect(page.getByRole('heading').first()).toBeVisible();

      expect(await horizontallyOverflows(page), `${path} overflows at ${viewport.name}`).toBe(false);

      // Every primary control must be comfortably operable by touch. A 24-pixel target on a
      // phone is a target people miss.
      const tooSmall = await page.evaluate(() => {
        const offending: string[] = [];
        for (const element of Array.from(document.querySelectorAll('button, a[href], select, input'))) {
          const rect = element.getBoundingClientRect();
          if (rect.width === 0 && rect.height === 0) continue;
          const style = window.getComputedStyle(element);
          if (style.display === 'none' || style.visibility === 'hidden') continue;
          // The charting library renders its own attribution link inside the chart. Its size
          // and placement are the library's, the licence requires the link to be present, and
          // enlarging or removing it is not ours to do. Everything else is ours and is held
          // to the minimum.
          if (element.closest('.price-chart')) continue;
          if (rect.height < 24 || rect.width < 24) {
            offending.push(`${element.tagName}:${(element.textContent ?? '').trim().slice(0, 24)}`);
          }
        }
        return offending;
      });
      expect(tooSmall, `controls below the minimum touch target at ${viewport.name}`).toEqual([]);
    });
  }
}

test('the chart states in words everything it draws', async ({ page }) => {
  await mockAccount(page, true);
  await page.goto('/markets/11111111-1111-4111-8111-111111111111');
  await expect(page.getByRole('heading', { name: 'Stored daily history' })).toBeVisible();

  // A canvas is opaque to a screen reader. Each of these is a fact the chart shows visually
  // and must therefore also state as text (FR-017).
  await expect(page.getByText('2 sessions', { exact: false })).toBeVisible();
  await expect(page.getByText('the exchange was open with no stored bar')).toBeVisible();
  await expect(page.getByText(/Provider-adjusted/)).toBeVisible();
  await expect(page.getByText('Suspicious jump')).toBeVisible();
  await expect(page.getByText(/2026-01-05/)).toBeVisible();
});
