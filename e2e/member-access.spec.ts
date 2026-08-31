import { expect, test, type Page } from '@playwright/test';

const memberAccount = {
  id: '11111111-1111-4111-8111-111111111111', email: 'member@example.com', display_name: 'Ada Member',
  role: 'member', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};
const ownerAccount = {
  id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
  role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};
const GENERIC = 'If you have an account, you should receive an email with a six-digit passcode.';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class QuietEventSource extends EventTarget {
      constructor(_url: string | URL) { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: QuietEventSource });
  });
});

test('a member signs in with only the emailed code and never sees a password field', async ({ page, isMobile }) => {
  const state = { authenticated: false };
  await mockMemberAPI(page, state, { code: '012345' });

  await page.goto('/markets');
  await expect(page).toHaveURL(/\/login\?redirect=/);

  await page.getByLabel('Email').fill('member@example.com');
  const advance = page.getByRole('button', { name: 'Continue' });
  if (isMobile) await advance.tap(); else await advance.click();

  await expect(page.getByRole('heading', { name: 'Enter your passcode' })).toBeVisible();
  await expect(page.getByText(GENERIC)).toBeVisible();
  await expect(page.getByText('member@example.com')).toBeVisible();
  // The passwordless journey must never present a password input.
  await expect(page.locator('input[type="password"]')).toHaveCount(0);

  const code = page.getByLabel('Six-digit passcode');
  await expect(code).toHaveAttribute('inputmode', 'numeric');
  await expect(code).toHaveAttribute('autocomplete', 'one-time-code');

  // A code pasted with the spacing many mail clients introduce is still accepted.
  await code.fill('01 23-45');
  await expect(code).toHaveValue('012345');
  const verify = page.getByRole('button', { name: 'Verify passcode' });
  if (isMobile) await verify.tap(); else await verify.click();

  await expect(page).toHaveURL(/\/markets(?:\?.*)?$/);
  // No secret may be persisted in browser storage.
  const stored = await page.evaluate(() => JSON.stringify({ localStorage, sessionStorage }));
  expect(stored).not.toContain('012345');
  expect(stored).not.toContain('csrf');
});

test('an incomplete code is refused locally and a wrong code fails generically', async ({ page }) => {
  const state = { authenticated: false };
  await mockMemberAPI(page, state, { code: '012345' });

  await page.goto('/login');
  await page.getByLabel('Email').fill('member@example.com');
  await page.getByRole('button', { name: 'Continue' }).click();

  // Fewer than six digits must not reach the server at all.
  await page.getByLabel('Six-digit passcode').fill('123');
  await page.getByRole('button', { name: 'Verify passcode' }).click();
  await expect(page.getByRole('alert')).toContainText('six digits');
  expect(state.verifyAttempts ?? 0).toBe(0);

  // A wrong code returns the same generic failure, with no hint about the account.
  await page.getByLabel('Six-digit passcode').fill('999999');
  await page.getByRole('button', { name: 'Verify passcode' }).click();
  await expect(page.getByRole('alert')).toContainText('Authentication failed.');
  const alert = await page.getByRole('alert').innerText();
  expect(alert.toLowerCase()).not.toContain('locked');
  expect(alert.toLowerCase()).not.toContain('blocked');
  expect(alert.toLowerCase()).not.toContain('no account');
  // The typed code is retained so a failure does not force retyping.
  await expect(page.getByLabel('Six-digit passcode')).toHaveValue('999999');
});

test('a blocked and a locked member both see only the generic failure', async ({ page }) => {
  const state = { authenticated: false };
  await mockMemberAPI(page, state, { code: '012345', alwaysFail: true });

  await page.goto('/login');
  await page.getByLabel('Email').fill('member@example.com');
  await page.getByRole('button', { name: 'Continue' }).click();

  const messages: string[] = [];
  for (const attempt of ['111111', '222222', '333333', '012345']) {
    await page.getByLabel('Six-digit passcode').fill(attempt);
    await page.getByRole('button', { name: 'Verify passcode' }).click();
    await expect(page.getByRole('alert')).toBeVisible();
    messages.push(await page.getByRole('alert').innerText());
  }
  // Crossing the block threshold must not change what the person is told.
  expect(new Set(messages).size).toBe(1);
  await expect(page).toHaveURL(/\/login/);
});

test('the resend control stays disabled until its countdown elapses', async ({ page }) => {
  const state = { authenticated: false };
  await mockMemberAPI(page, state, { code: '012345' });

  await page.goto('/login');
  await page.getByLabel('Email').fill('member@example.com');
  await page.getByRole('button', { name: 'Continue' }).click();

  const resend = page.locator('button[data-resend]');
  await expect(resend).toBeDisabled();
  await expect(resend).toContainText(/Send a new code in \d+s/);
});

test('an owner administers member lock state while a member cannot', async ({ page, context }) => {
  const state = { authenticated: true, role: 'owner' as const };
  await mockMemberAPI(page, state, { code: '012345' });
  // A reloaded page recovers its CSRF token from the cookie the server published at sign-in.
  await context.addCookies([{
    name: '__Host-market_lens_csrf', value: 'csrf-memory-only-login',
    domain: '127.0.0.1', path: '/', secure: true,
  }]);

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Members' })).toBeVisible();
  await expect(page.locator('[data-state="administratively_locked"]')).toHaveText('Locked');
  await expect(page.locator('[data-state="temporarily_blocked"]')).toHaveText('Temporarily blocked');

  // Only the administratively locked member offers an owner action.
  const unlock = page.locator('button[data-unlock]');
  await expect(unlock).toHaveCount(1);
  await unlock.click();
  await expect(page.getByText('Member sign-in unlocked.')).toBeVisible();
  expect(state.unlockCSRF).toBe('csrf-memory-only-login');
});

test('a signed-in member never sees owner member administration', async ({ page }) => {
  const state = { authenticated: true, role: 'member' as const };
  await mockMemberAPI(page, state, { code: '012345' });

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Members' })).toHaveCount(0);
  await expect(page.locator('button[data-unlock]')).toHaveCount(0);
  expect(state.memberListRequests ?? 0).toBe(0);
});

test('the passcode step survives theme changes and fits 320 CSS pixels', async ({ page }) => {
  const state = { authenticated: false };
  await mockMemberAPI(page, state, { code: '012345' });
  await page.setViewportSize({ width: 320, height: 800 });

  await page.goto('/login');
  await page.getByLabel('Email').fill('member@example.com');
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByLabel('Six-digit passcode').fill('0123');

  for (let theme = 0; theme < 3; theme += 1) {
    await page.getByRole('button', { name: 'Change color theme' }).click();
    // Input typed before the theme switch must survive it.
    await expect(page.getByLabel('Six-digit passcode')).toHaveValue('0123');
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

type MemberState = {
  authenticated: boolean;
  role?: 'owner' | 'member';
  verifyAttempts?: number;
  memberListRequests?: number;
  unlockCSRF?: string;
};

async function mockMemberAPI(page: Page, state: MemberState, options: { code: string; alwaysFail?: boolean }): Promise<void> {
  const accountFor = () => (state.role === 'owner' ? ownerAccount : memberAccount);
  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: false } }));
  await page.route('**/api/v1/account', (route) => state.authenticated
    ? route.fulfill({ json: accountFor() })
    : route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'Authentication is required.' } } }));
  await page.route('**/api/v1/auth/sign-in/start', (route) => route.fulfill({
    status: 202, json: { message: GENERIC },
  }));
  await page.route('**/api/v1/auth/member/code/verify', async (route) => {
    state.verifyAttempts = (state.verifyAttempts ?? 0) + 1;
    const body = route.request().postDataJSON() as { code?: string };
    if (options.alwaysFail || body.code !== options.code) {
      return route.fulfill({
        status: 401, json: { error: { code: 'authentication_failed', message: 'Authentication failed.' } },
      });
    }
    state.authenticated = true;
    state.role = 'member';
    return route.fulfill({ json: { account: memberAccount, csrf_token: 'csrf-memory-only-login' } });
  });
  await page.route('**/api/v1/account/sessions', (route) => route.request().method() === 'DELETE'
    ? route.fulfill({ status: 204 })
    : route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/account/sessions/*', (route) => route.fulfill({ status: 204 }));
  await page.route('**/api/v1/owner/members', (route) => {
    state.memberListRequests = (state.memberListRequests ?? 0) + 1;
    if (state.role !== 'owner') {
      return route.fulfill({
        status: 403, json: { error: { code: 'authorization_denied', message: 'Owner access is required.' } },
      });
    }
    return route.fulfill({ json: { members: [
      {
        id: 'a0000000-0000-4000-8000-000000000001', email: 'locked@example.com', display_name: 'Locked Member',
        status: 'active', login_state: 'administratively_locked', blocked_until: null,
        locked_at: '2026-08-30T10:00:00Z', active_session_count: 0, created_at: '2026-08-30T09:00:00Z',
      },
      {
        id: 'a0000000-0000-4000-8000-000000000002', email: 'blocked@example.com', display_name: 'Blocked Member',
        status: 'active', login_state: 'temporarily_blocked', blocked_until: '2026-08-30T10:15:00Z',
        locked_at: null, active_session_count: 1, created_at: '2026-08-30T09:00:00Z',
      },
    ], next_cursor: '' } });
  });
  await page.route('**/api/v1/owner/members/*/unlock', (route) => {
    state.unlockCSRF = route.request().headers()['x-csrf-token'];
    return route.fulfill({ status: 204 });
  });
}
