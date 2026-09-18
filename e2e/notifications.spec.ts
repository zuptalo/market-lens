import { expect, test } from '@playwright/test';
import { api } from './fixtures/api.mjs';

const VIEWPORTS = [
  { name: 'mobile', width: 360, height: 800 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1440, height: 900 },
];

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class FakeEventSource extends EventTarget {
      constructor() { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: FakeEventSource });
  });
  await page.route('**/api/v1/**', (route) => {
    const { pathname } = new URL(route.request().url());
    return route.fulfill({ json: api(pathname) as object });
  });
});

for (const viewport of VIEWPORTS) {
  test(`a person reads and changes what they are told, at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/account');

    const settings = page.getByTestId('notification-settings');
    await expect(settings).toBeVisible();

    // The statement the whole section is built around: a row of switches that are all off reads as
    // an oversight unless it is stated as a choice.
    await expect(settings).toContainText(/nothing is sent unless you asked for it/i);

    // Each kind, said the way a person would say it.
    await expect(settings).toContainText('A decision is waiting');
    await expect(settings).toContainText('A paper order settled');
    await expect(settings).toContainText('A strategy changed its view');

    // Quiet hours, and what they actually do — held, not dropped.
    await expect(settings).toContainText(/quiet hours/i);
    await expect(settings).toContainText(/held until it ends/i);

    // Devices, by the name the person gave them.
    await expect(settings).toContainText('iPhone, added 2026-09-01');

    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });
}

// A device list is not a list of endpoints: an endpoint is where somebody reads their mail.
test('the device list shows no endpoint and no keys', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/account');
  await expect(page.getByTestId('notification-settings')).toBeVisible();

  const section = await page.getByTestId('notification-settings').innerText();
  for (const forbidden of ['https://', 'p256dh', 'BCVxsr']) {
    expect(section, `the device list shows ${forbidden}`).not.toContain(forbidden);
  }
});

test('turning something on goes through the write path', async ({ page }) => {
  let sent: Record<string, unknown> | null = null;
  await page.route('**/api/v1/notifications/preferences', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    sent = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ json: api('/api/v1/notifications/preferences') as object });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/account');

  await page.getByLabel('A paper order settled by Email').click();
  await expect.poll(() => sent).not.toBeNull();
  expect(sent).toMatchObject({ kind: 'paper_fill', channel: 'email', enabled: true });
});

test('nothing on the screen suggests turning notifications on', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/account');
  await expect(page.getByTestId('notification-settings')).toBeVisible();

  const section = (await page.getByTestId('notification-settings').innerText()).toLowerCase();
  for (const forbidden of ['recommend', 'you should', 'stay informed', 'never miss', 'popular']) {
    expect(section, `the section says "${forbidden}"`).not.toContain(forbidden);
  }
});

// The SMTP section already existed. What it gained is proof a message actually arrives, which the
// settings check cannot give: a server can connect, refuse the sender, and look configured.
test('the mail settings offer a test send to yourself', async ({ page }) => {
  let called = false;
  await page.route('**/api/v1/notifications/test-email', async (route) => {
    called = true;
    await route.fulfill({ json: { sent: true } });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/account');

  const test = page.getByRole('button', { name: /send a test message to yourself/i });
  await expect(test).toBeVisible();
  await test.click();
  await expect.poll(() => called).toBe(true);
  await expect(page.locator('main')).toContainText(/sent\./i);
});

// The one page that works without a session, because an unsubscribe that needs a sign-in is one
// people do not use — they mark the mail as spam instead.
test('an unsubscribe link works without signing in', async ({ page }) => {
  let sent: Record<string, unknown> | null = null;
  await page.route('**/api/v1/**', async (route) => {
    const { pathname } = new URL(route.request().url());
    if (pathname === '/api/v1/notifications/unsubscribe') {
      sent = route.request().postDataJSON() as Record<string, unknown>;
      return route.fulfill({ json: { kind: 'signal_change', channel: 'email', stopped: true } });
    }
    if (pathname === '/api/v1/account') {
      return route.fulfill({ status: 401, json: { error: { code: 'authentication_required' } } });
    }
    return route.fulfill({ json: api(pathname) as object });
  });
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/unsubscribe?token=abc.def');

  await expect(page.getByTestId('unsubscribed')).toBeVisible();
  await expect(page.locator('main')).toContainText(/no longer tell you/i);
  // It says what it did, and that it did nothing else.
  await expect(page.locator('main')).toContainText(/nothing else changed/i);
  expect(sent).toMatchObject({ token: 'abc.def' });
});

test('a broken unsubscribe link says so rather than pretending', async ({ page }) => {
  await page.route('**/api/v1/notifications/unsubscribe', (route) =>
    route.fulfill({ status: 400, json: { error: { code: 'invalid_token', message: 'This link is not readable, or has expired.' } } }));
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/unsubscribe?token=nonsense');

  await expect(page.getByTestId('unsubscribe-failed')).toBeVisible();
  await expect(page.locator('main')).toContainText(/account settings/i);
});

test('nothing clips at the 320 pixel floor', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/account');
  await expect(page.getByTestId('notification-settings')).toBeVisible();
  expect(await page.evaluate(() =>
    document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});
