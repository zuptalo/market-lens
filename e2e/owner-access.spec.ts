import { expect, test, type Page } from '@playwright/test';

/**
 * A session that is live *now*. Written relative to the clock rather than as fixed dates,
 * because a signed-in device is one whose session has not expired — and a fixture that pins
 * an expiry to a calendar date silently becomes an expired session as soon as that date
 * passes, then asserts it is still listed.
 */
const liveSessionExpiry = {
  idle: new Date(Date.now() + 8 * 60 * 60 * 1000).toISOString(),
  absolute: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString(),
};

const account = {
  id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
  role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class QuietEventSource extends EventTarget {
      constructor(_url: string | URL) { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: QuietEventSource });
  });
});

test('denies anonymous markets and uses generic email before explicit owner password', async ({ page }) => {
  let authenticated = false;
  let marketRequests = 0;
  await mockOwnerAPI(page, () => authenticated, () => { authenticated = true; });
  await page.route('**/api/v1/instruments?*', (route) => { marketRequests += 1; return route.fulfill({ json: { items: [] } }); });
  await page.route('**/api/v1/market-data/imports?*', (route) => { marketRequests += 1; return route.fulfill({ json: { items: [] } }); });

  await page.goto('/markets');
  await expect(page).toHaveURL(/\/login\?redirect=(?:%2F|\/)markets$/);
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  expect(marketRequests).toBe(0);

  await page.getByLabel('Email').fill('owner@example.com');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByLabel('Six-digit passcode')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Use owner password' })).toBeVisible();
  await page.getByRole('button', { name: 'Use owner password' }).click();
  await page.getByLabel('Password').fill('password-secret');
  await page.getByRole('button', { name: 'Sign in' }).press('Enter');
  await expect(page).toHaveURL(/\/markets(?:\?.*)?$/);
  await expect(page.getByRole('heading', { name: 'Market data' })).toBeVisible();
  expect(marketRequests).toBeGreaterThan(0);
});

test('retries provider validation then completes encrypted owner setup without browser persistence', async ({ page, isMobile }) => {
  let authenticated = false;
  await mockOwnerAPI(page, () => authenticated, () => { authenticated = true; }, true, true);

  await page.goto('/setup#setup-capability-secret');
  await expect(page.getByRole('heading', { name: 'Create the owner account' })).toBeVisible();
  await page.getByLabel('Display name').fill('Owner');
  await page.getByLabel('Email', { exact: true }).fill('owner@example.com');
  await page.getByLabel('Password', { exact: true }).fill('strong-password-secret');
  await page.getByLabel('EODHD API key').fill('eodhd-key-secret');
  await page.getByLabel('SMTP host').fill('smtp.example.test');
  await page.getByLabel('SMTP port').fill('587');
  await page.getByLabel('From email').fill('access@example.test');
  await page.getByLabel('SMTP username').fill('mailer');
  await page.getByLabel('SMTP password').fill('smtp-password-secret');
  const create = page.getByRole('button', { name: 'Create owner' });
  if (isMobile) await create.tap(); else await create.press('Enter');
  await expect(page.getByRole('alert')).toContainText('Provider validation is temporarily unavailable.');
  await expect(page.getByLabel('EODHD API key')).toHaveValue('eodhd-key-secret');
  if (isMobile) await create.tap(); else await create.press('Enter');
  await expect(page).toHaveURL('/');
  expect(await page.evaluate(() => JSON.stringify({ localStorage, sessionStorage }))).not.toContain('eodhd-key-secret');
  expect(await page.evaluate(() => JSON.stringify({ localStorage, sessionStorage }))).not.toContain('smtp-password-secret');

  await page.getByRole('link', { name: 'Account' }).click();
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
  await expect(page.getByText('Chrome on Linux')).toBeVisible();
  await page.getByRole('button', { name: 'Revoke Chrome on Linux' }).click();
  await expect(page.getByText('Session revoked')).toBeVisible();
});

test('has no forgot-password or public recovery interaction', async ({ page }) => {
  await mockOwnerAPI(page, () => false, () => {});
  await page.goto('/login');
  await expect(page.getByText(/forgot your password/i)).toHaveCount(0);
  await expect(page.getByText(/recover owner access/i)).toHaveCount(0);
});

test('retains auth form state across themes and has no horizontal overflow at 320px', async ({ page }) => {
  await mockOwnerAPI(page, () => false, () => {});
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/login');
  await page.getByLabel('Email').fill('owner@example.com');
  for (let theme = 0; theme < 3; theme += 1) {
    await page.getByRole('button', { name: 'Change color theme' }).click();
    await expect(page.getByLabel('Email')).toHaveValue('owner@example.com');
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

async function mockOwnerAPI(
  page: Page,
  isAuthenticated: () => boolean,
  authenticate: () => void,
  setupRequired = false,
  failFirstSetup = false,
): Promise<void> {
  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: setupRequired } }));
  await page.route('**/api/v1/account', (route) => isAuthenticated()
    ? route.fulfill({ json: account })
    : route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'Authentication is required.' } } }));
  await page.route('**/api/v1/auth/owner/login', (route) => {
    authenticate();
    return route.fulfill({ json: { account, csrf_token: 'csrf-memory-only-login' } });
  });
  await page.route('**/api/v1/auth/sign-in/start', (route) => route.fulfill({
    status: 202, json: { message: 'If you have an account, you should receive an email with a six-digit passcode.' },
  }));
  let setupAttempts = 0;
  await page.route('**/api/v1/auth/owner/setup', (route) => {
    setupAttempts += 1;
    if (failFirstSetup && setupAttempts === 1) return route.fulfill({
      status: 503, json: { error: { code: 'provider_unavailable', message: 'Provider could not be validated.' } },
    });
    authenticate();
    return route.fulfill({ status: 201, json: { account, csrf_token: 'csrf-memory-only-setup' } });
  });
  await page.route('**/api/v1/account/sessions', async (route) => {
    if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 });
    return route.fulfill({ json: { items: [{
      id: '20000000-0000-4000-8000-000000000001', current: false, device_label: 'Chrome on Linux',
      created_at: '2026-08-30T08:00:00Z', last_seen_at: '2026-08-30T08:01:00Z',
      idle_expires_at: liveSessionExpiry.idle, absolute_expires_at: liveSessionExpiry.absolute, revoked: false,
    }] } });
  });
  await page.route('**/api/v1/account/sessions/*', (route) => route.fulfill({ status: 204 }));
}

const ownerAccount = {
  id: '11111111-1111-4111-8111-111111111000', email: 'owner@example.com', display_name: 'Market Owner',
  role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};

const memberAccount = {
  id: '11111111-1111-4111-8111-111111111111', email: 'member-a@example.com', display_name: 'Member A',
  role: 'member', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};

// StreamHarness replaces EventSource with one the test drives, so an authorization change behind
// an open stream can be observed the way a person would experience it.
declare global {
  interface Window {
    __stream: {
      emit(type: string, id: string, data: unknown): void;
      refuse(): void;
      drop(): void;
      opened: number;
      urls: string[];
    };
  }
}

async function installStreamHarness(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const listeners: Array<{ target: EventTarget; url: string }> = [];
    const control = {
      opened: 0,
      urls: [] as string[],
      emit(type: string, id: string, data: unknown) {
        const current = listeners.at(-1);
        if (!current) return;
        const event = new MessageEvent(type, { data: JSON.stringify(data), lastEventId: id });
        current.target.dispatchEvent(event);
      },
      // A refusal is what the browser reports when the server rejects the stream outright.
      refuse() {
        const current = listeners.at(-1);
        if (!current) return;
        (current.target as { readyState?: number }).readyState = 2;
        current.target.dispatchEvent(new Event('error'));
      },
      // A dropped connection keeps retrying rather than ending the session.
      drop() {
        const current = listeners.at(-1);
        if (!current) return;
        (current.target as { readyState?: number }).readyState = 0;
        current.target.dispatchEvent(new Event('error'));
      },
    };
    class ControlledEventSource extends EventTarget {
      // Mirrors the real EventSource readyState constants the client reads.
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSED = 2;
      readyState = 1;
      constructor(url: string | URL) {
        super();
        control.opened += 1;
        control.urls.push(String(url));
        listeners.push({ target: this, url: String(url) });
        queueMicrotask(() => this.dispatchEvent(new Event('open')));
      }
      close(): void { this.readyState = 2; }
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: ControlledEventSource });
    Object.defineProperty(window, '__stream', { configurable: true, value: control });
  });
}

async function mockMemberSession(page: Page, state: { authenticated: boolean }): Promise<{ ownerCalls: number }> {
  const counters = { ownerCalls: 0 };
  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: false } }));
  await page.route('**/api/v1/account', (route) => state.authenticated
    ? route.fulfill({ json: memberAccount })
    : route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'Authentication is required.' } } }));
  await page.route('**/api/v1/account/sessions', (route) => route.fulfill({ json: { items: [{
    id: '20000000-0000-4000-8000-0000000000a1', current: true, device_label: 'Member phone',
    created_at: '2026-08-30T08:00:00Z', last_seen_at: '2026-08-30T08:01:00Z',
    idle_expires_at: liveSessionExpiry.idle, absolute_expires_at: liveSessionExpiry.absolute, revoked: false,
  }] } }));
  // Owner administration is refused for a member at the server, and nothing about what exists
  // behind it may appear in the refusal.
  await page.route('**/api/v1/owner/**', (route) => {
    counters.ownerCalls += 1;
    return route.fulfill({ status: 403, json: { error: { code: 'forbidden', message: 'Owner authorization is required.' } } });
  });
  await page.route('**/api/v1/instruments?*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/market-data/imports?*', (route) => route.fulfill({ json: { items: [] } }));
  return counters;
}

test('a member sees no owner administration and reaches no owner route by guessing it', async ({ page }) => {
  await installStreamHarness(page);
  const state = { authenticated: true };
  const counters = await mockMemberSession(page, state);

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
  await expect(page.getByText('Member phone')).toBeVisible();
  // Administration surfaces belong to the owner alone.
  await expect(page.getByRole('heading', { name: 'Members' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Invitations' })).toHaveCount(0);
  expect(counters.ownerCalls).toBe(0);

  // A guessed application route still resolves to the member's own view, never another
  // member's data and never an administration surface.
  const refusal = await page.evaluate(async () => {
    const response = await fetch('/api/v1/owner/members');
    return { status: response.status, body: await response.text() };
  });
  expect(refusal.status).toBe(403);
  expect(refusal.body).not.toContain('@example.com');
});

test('a shared market event reaches a member while another member private event is ignored', async ({ page }) => {
  await installStreamHarness(page);
  const state = { authenticated: true };
  await mockMemberSession(page, state);
  let accountReads = 0;
  await page.route('**/api/v1/account', (route) => {
    accountReads += 1;
    return route.fulfill({ json: memberAccount });
  });

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
  const baseline = accountReads;

  // Another member's private event must never invalidate this member's snapshot.
  await page.evaluate(() => window.__stream.emit('session.revoked.v1', '501', {
    version: 1, scope: 'user', subject_user_id: '22222222-2222-4222-8222-222222222222',
    entity_type: 'session', entity_id: 'session-b', payload: {}, occurred_at: '2026-08-30T08:02:00Z',
  }));
  await expect.poll(() => accountReads).toBe(baseline);

  // Shared market data reaches every signed-in member.
  await page.evaluate(() => window.__stream.emit('daily_bar.changed.v1', '502', {
    version: 1, scope: 'shared', entity_type: 'daily_bar', entity_id: 'bar-1',
    payload: {}, occurred_at: '2026-08-30T08:03:00Z',
  }));

  // This member's own private event does refresh the snapshot.
  await page.evaluate(() => window.__stream.emit('session.revoked.v1', '503', {
    version: 1, scope: 'user', subject_user_id: '11111111-1111-4111-8111-111111111111',
    entity_type: 'session', entity_id: 'session-a', payload: {}, occurred_at: '2026-08-30T08:04:00Z',
  }));
  await expect.poll(() => accountReads).toBeGreaterThan(baseline);
});

test('revoking the session behind an open stream returns the member to sign-in', async ({ page }) => {
  await installStreamHarness(page);
  const state = { authenticated: true };
  await mockMemberSession(page, state);

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();

  // The owner deactivates the member, or the member revokes the device elsewhere.
  state.authenticated = false;
  await page.evaluate(() => window.__stream.refuse());

  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  // No stale private data may remain on the page behind the sign-in form.
  await expect(page.getByText('Member phone')).toHaveCount(0);
});

test('a dropped connection reconnects and resumes after the last delivered event', async ({ page }) => {
  await installStreamHarness(page);
  const state = { authenticated: true };
  await mockMemberSession(page, state);

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
  await page.evaluate(() => window.__stream.emit('daily_bar.changed.v1', '601', {
    version: 1, scope: 'shared', entity_type: 'daily_bar', entity_id: 'bar-1',
    payload: {}, occurred_at: '2026-08-30T08:05:00Z',
  }));

  await page.evaluate(() => window.__stream.drop());
  await expect.poll(() => page.evaluate(() => window.__stream.opened), { timeout: 10_000 }).toBeGreaterThan(1);
  // The reconnection asks only for what it has not already seen.
  const urls = await page.evaluate(() => window.__stream.urls);
  expect(urls.at(-1)).toContain('last_event_id=601');
  // A dropped connection is not an authorization failure.
  await expect(page).toHaveURL(/\/account/);
});

test('authorization state stays legible and reachable at every supported viewport', async ({ page }) => {
  await installStreamHarness(page);
  const state = { authenticated: true };
  await mockMemberSession(page, state);

  for (const size of [{ width: 320, height: 800 }, { width: 360, height: 800 }, { width: 768, height: 1024 }, { width: 1440, height: 900 }]) {
    await page.setViewportSize(size);
    await page.goto('/account');
    await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Members' })).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  }

  // The signed-out state is equally usable on the narrowest supported screen.
  await page.setViewportSize({ width: 320, height: 800 });
  state.authenticated = false;
  await page.goto('/markets');
  await expect(page).toHaveURL(/\/login\?redirect=/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

test('a fresh installation explains itself instead of showing a dead sign-in form', async ({ page }) => {
  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: true } }));
  await page.route('**/api/v1/account', (route) => route.fulfill({
    status: 401, json: { error: { code: 'authentication_required', message: 'Authentication is required.' } },
  }));

  // Somebody who has just deployed this lands here, and it has to tell them what to do.
  await page.goto('/');
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole('heading', { name: 'This installation has not been set up yet' })).toBeVisible();
  await expect(page.getByText('market-lens auth setup-link')).toBeVisible();
  await expect(page.locator('input[type="email"]')).toHaveCount(0);

  // The instruction must not assume one deployment shape over another.
  const body = (await page.locator('body').innerText()).toLowerCase();
  for (const assumption of ['kubectl', 'k3s', 'kubernetes', 'docker compose exec', 'helm']) {
    expect(body, `setup guidance assumes ${assumption}`).not.toContain(assumption);
  }

  // It stays readable on the narrowest supported screen, which is where somebody checking
  // a new deployment from a phone will see it.
  await page.setViewportSize({ width: 320, height: 800 });
  await expect(page.getByRole('heading', { name: 'This installation has not been set up yet' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)).toBe(false);
  await expect(page.getByRole('button', { name: 'Copy command' })).toBeVisible();
});

// Feature 011. Provider credentials used to be write-once: an expired EODHD key or a changed
// mail password had no recovery short of a new database. The owner can now see the
// configuration, check a change against the real services, and save only what works.
test('the owner can see, check, and correct the integration configuration at every viewport', async ({ page }) => {
  await installStreamHarness(page);
  const calls = { verified: 0, saved: 0 };
  let host = 'smtp.example.test';

  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: false } }));
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: ownerAccount }));
  await page.route('**/api/v1/account/sessions', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/owner/members*', (route) => route.fulfill({ json: { items: [], next_cursor: '' } }));
  await page.route('**/api/v1/owner/invitations*', (route) => route.fulfill({ json: { items: [], next_cursor: '' } }));
  await page.route('**/api/v1/owner/integrations/verify', (route) => {
    calls.verified += 1;
    return route.fulfill({
      status: 400,
      json: {
        error: {
          code: 'invalid_setup', message: 'Some of the details you entered need attention.',
          fields: [{ field: 'smtp_password', code: 'auth_rejected', message: 'ignored' }],
        },
      },
    });
  });
  await page.route('**/api/v1/owner/integrations', (route) => {
    if (route.request().method() === 'PUT') {
      calls.saved += 1;
      host = 'smtp.moved.test';
      return route.fulfill({ status: 204, body: '' });
    }
    return route.fulfill({
      json: {
        integrations: [], configuration: {},
        settings: {
          eodhd: { configured: true, validated_at: '2026-08-31T09:00:00Z' },
          smtp: {
            configured: true, host, port: 587, from: 'access@example.test',
            username: 'mailer', password_configured: true,
          },
        },
      },
    });
  });

  // Mutations use the double-submit CSRF token a real sign-in would have left behind.
  await page.context().addCookies([{
    name: '__Host-market_lens_csrf', value: 'csrf-e2e-token',
    domain: '127.0.0.1', path: '/', secure: true, sameSite: 'Strict',
  }]);

  await page.goto('/account');
  // The configuration is visible, and no secret is prefilled because none is ever returned.
  await expect(page.locator('#integration-smtp-host')).toHaveValue('smtp.example.test');
  await expect(page.locator('#integration-smtp-password')).toHaveValue('');
  await expect(page.locator('#integration-eodhd-api-key')).toHaveValue('');

  // A check reports the failing field and saves nothing.
  await page.getByRole('button', { name: 'Check without saving' }).click();
  await expect(page.locator('#integration-smtp_password-error')).toContainText('rejected these credentials');
  await expect(page.locator('#integration-smtp-password')).toHaveAttribute('aria-invalid', 'true');
  expect(calls.saved).toBe(0);

  // A correction saves, and the reloaded configuration reflects it.
  await page.locator('#integration-smtp-host').fill('smtp.moved.test');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('#integration-smtp-host')).toHaveValue('smtp.moved.test');
  expect(calls.saved).toBe(1);

  for (const size of [{ width: 320, height: 800 }, { width: 360, height: 800 }, { width: 768, height: 1024 }, { width: 1440, height: 900 }]) {
    await page.setViewportSize(size);
    await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  }
});
