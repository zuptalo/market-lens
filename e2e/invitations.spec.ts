import { expect, test, type Page } from '@playwright/test';

const ownerAccount = {
  id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', display_name: 'Owner',
  role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};
const memberAccount = {
  id: '11111111-1111-4111-8111-111111111111', email: 'invitee@example.com', display_name: 'Ada Invitee',
  role: 'member', status: 'active', email_verified_at: '2026-08-30T08:00:00Z',
};

type Invitation = {
  id: string; email: string; state: string; expires_at: string; accepted_at: string | null;
  delivery_state: string; delivery_error: string | null; resend_count: number; created_at: string;
};

function pending(id: string, email: string, overrides: Partial<Invitation> = {}): Invitation {
  return {
    id, email, state: 'pending', expires_at: '2026-09-06T10:00:00Z', accepted_at: null,
    delivery_state: 'sent', delivery_error: null, resend_count: 0, created_at: '2026-08-30T10:00:00Z',
    ...overrides,
  };
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class QuietEventSource extends EventTarget {
      constructor(_url: string | URL) { super(); queueMicrotask(() => this.dispatchEvent(new Event('open'))); }
      close(): void {}
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: QuietEventSource });
  });
});

test('an owner invites, resends, and revokes without ever seeing a capability', async ({ page, context, isMobile }) => {
  const state = createOwnerState();
  await mockInvitationAPI(page, state);
  await context.addCookies([{
    name: '__Host-market_lens_csrf', value: 'csrf-memory-only-login',
    domain: '127.0.0.1', path: '/', secure: true,
  }]);

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Invitations' })).toBeVisible();
  await expect(page.getByText('No invitations yet')).toBeVisible();

  await page.getByLabel('Invite by email').fill('invitee@example.com');
  const send = page.getByRole('button', { name: 'Send invitation' });
  if (isMobile) await send.tap(); else await send.click();

  await expect(page.getByText('Invitation sent.')).toBeVisible();
  await expect(page.getByText('invitee@example.com')).toBeVisible();
  expect(state.created).toBe('invitee@example.com');
  // A capability is delivered only by email and must never appear in the interface.
  expect(await page.locator('body').innerText()).not.toContain('capability');

  await page.locator('button[data-resend]').click();
  await expect(page.getByText('Invitation resent.')).toBeVisible();
  expect(state.resent).toBe('70000000-0000-4000-8000-000000000001');

  await page.locator('button[data-revoke]').click();
  await expect(page.getByText('Invitation revoked.')).toBeVisible();
  expect(state.revoked).toBe('70000000-0000-4000-8000-000000000001');
  // Every mutation must carry the double-submit token.
  expect(state.csrfTokens).not.toContain(undefined);
});

test('a failed delivery is shown as safe resendable state', async ({ page, context }) => {
  const state = createOwnerState();
  state.invitations = [pending('70000000-0000-4000-8000-000000000001', 'invitee@example.com', {
    delivery_state: 'failed', delivery_error: 'temporary_failure',
  })];
  await mockInvitationAPI(page, state);
  await context.addCookies([{
    name: '__Host-market_lens_csrf', value: 'csrf-memory-only-login',
    domain: '127.0.0.1', path: '/', secure: true,
  }]);

  await page.goto('/account');
  await expect(page.getByText('Not delivered')).toBeVisible();
  // The provider's own error vocabulary must not reach the owner.
  const body = await page.locator('body').innerText();
  expect(body).not.toContain('temporary_failure');
  expect(body).not.toContain('smtp');
  // The invitation is still actionable.
  await expect(page.locator('button[data-resend]')).toBeEnabled();
});

test('an invited person joins with no password and the capability leaves the URL', async ({ page, isMobile }) => {
  const state = createOwnerState();
  await mockInvitationAPI(page, state);

  await page.goto('/invite#invitation-capability-secret');
  await expect(page.getByRole('heading', { name: 'Accept your invitation' })).toBeVisible();
  // Nothing in the passwordless journey may ask for a password.
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  expect((await page.locator('body').innerText()).toLowerCase()).not.toContain('password');
  // The capability must not linger in history or in a copied link.
  expect(page.url()).not.toContain('invitation-capability-secret');

  await page.getByLabel('Display name').fill('Ada Invitee');
  await page.getByLabel('Email').fill('invitee@example.com');
  const join = page.getByRole('button', { name: 'Join Market Lens' });
  if (isMobile) await join.tap(); else await join.click();

  await expect(page).toHaveURL('/');
  expect(state.acceptedCapability).toBe('invitation-capability-secret');
  const stored = await page.evaluate(() => JSON.stringify({ localStorage, sessionStorage }));
  expect(stored).not.toContain('invitation-capability-secret');
});

test('an unusable invitation reports one generic outcome', async ({ page }) => {
  const state = createOwnerState();
  state.acceptStatus = 400;
  await mockInvitationAPI(page, state);

  await page.goto('/invite#expired-capability');
  await page.getByLabel('Display name').fill('Ada Invitee');
  await page.getByLabel('Email').fill('invitee@example.com');
  await page.getByRole('button', { name: 'Join Market Lens' }).click();

  await expect(page.getByRole('alert')).toContainText('invalid or unavailable');
  await expect(page).toHaveURL(/\/invite/);
  // Typed details survive the failure so nothing has to be retyped.
  await expect(page.getByLabel('Display name')).toHaveValue('Ada Invitee');
});

test('an owner deactivates and reactivates a member', async ({ page, context }) => {
  const state = createOwnerState();
  state.members = [{
    id: 'a0000000-0000-4000-8000-000000000001', email: 'member@example.com', display_name: 'Ada Member',
    status: 'active', login_state: 'available', blocked_until: null, locked_at: null,
    active_session_count: 2, created_at: '2026-08-30T09:00:00Z',
  }];
  await mockInvitationAPI(page, state);
  await context.addCookies([{
    name: '__Host-market_lens_csrf', value: 'csrf-memory-only-login',
    domain: '127.0.0.1', path: '/', secure: true,
  }]);

  await page.goto('/account');
  await page.locator('button[data-deactivate]').click();
  await expect(page.getByText('Member deactivated.')).toBeVisible();
  expect(state.statusChange).toEqual({ id: 'a0000000-0000-4000-8000-000000000001', status: 'deactivated' });

  await page.locator('button[data-reactivate]').click();
  await expect(page.getByText('Member reactivated.')).toBeVisible();
  expect(state.statusChange).toEqual({ id: 'a0000000-0000-4000-8000-000000000001', status: 'active' });
});

test('owner administration fits 320 pixels and survives theme changes', async ({ page, context }) => {
  const state = createOwnerState();
  state.invitations = [pending('70000000-0000-4000-8000-000000000001', 'a-very-long-invited-address@example.com')];
  await mockInvitationAPI(page, state);
  await context.addCookies([{
    name: '__Host-market_lens_csrf', value: 'csrf-memory-only-login',
    domain: '127.0.0.1', path: '/', secure: true,
  }]);
  await page.setViewportSize({ width: 320, height: 800 });

  await page.goto('/account');
  await expect(page.getByRole('heading', { name: 'Invitations' })).toBeVisible();
  await page.getByLabel('Invite by email').fill('typed@example.com');
  for (let theme = 0; theme < 3; theme += 1) {
    await page.getByRole('button', { name: 'Change color theme' }).click();
    await expect(page.getByLabel('Invite by email')).toHaveValue('typed@example.com');
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

type OwnerState = {
  invitations: Invitation[];
  members: Record<string, unknown>[];
  created?: string;
  resent?: string;
  revoked?: string;
  acceptedCapability?: string;
  acceptStatus: number;
  statusChange?: { id: string; status: string };
  csrfTokens: (string | undefined)[];
};

function createOwnerState(): OwnerState {
  return { invitations: [], members: [], acceptStatus: 201, csrfTokens: [] };
}

async function mockInvitationAPI(page: Page, state: OwnerState): Promise<void> {
  await page.route('**/api/v1/setup/status', (route) => route.fulfill({ json: { setup_required: false } }));
  await page.route('**/api/v1/account', (route) => route.fulfill({ json: ownerAccount }));
  await page.route('**/api/v1/account/sessions', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/owner/members', (route) => route.fulfill({
    json: { members: state.members, next_cursor: '' },
  }));
  await page.route('**/api/v1/owner/members/*/status', async (route) => {
    state.csrfTokens.push(route.request().headers()['x-csrf-token']);
    const id = route.request().url().split('/members/')[1].split('/')[0];
    const body = route.request().postDataJSON() as { status: string };
    state.statusChange = { id, status: body.status };
    state.members = state.members.map((member) => member.id === id ? { ...member, status: body.status } : member);
    return route.fulfill({ json: state.members.find((member) => member.id === id) });
  });
  await page.route('**/api/v1/owner/invitations', async (route) => {
    if (route.request().method() === 'POST') {
      state.csrfTokens.push(route.request().headers()['x-csrf-token']);
      const body = route.request().postDataJSON() as { email: string };
      state.created = body.email;
      const created = pending('70000000-0000-4000-8000-000000000001', body.email);
      state.invitations = [created];
      return route.fulfill({ status: 201, json: created });
    }
    return route.fulfill({ json: { items: state.invitations, next_cursor: '' } });
  });
  await page.route('**/api/v1/owner/invitations/*/resend', (route) => {
    state.csrfTokens.push(route.request().headers()['x-csrf-token']);
    state.resent = route.request().url().split('/invitations/')[1].split('/')[0];
    state.invitations = state.invitations.map((invitation) => ({ ...invitation, resend_count: invitation.resend_count + 1 }));
    return route.fulfill({ json: state.invitations[0] });
  });
  await page.route('**/api/v1/owner/invitations/*', (route) => {
    if (route.request().method() !== 'DELETE') return route.fallback();
    state.csrfTokens.push(route.request().headers()['x-csrf-token']);
    state.revoked = route.request().url().split('/invitations/')[1];
    state.invitations = [];
    return route.fulfill({ status: 204 });
  });
  await page.route('**/api/v1/auth/invitations/accept', (route) => {
    const body = route.request().postDataJSON() as { capability: string };
    if (state.acceptStatus !== 201) {
      return route.fulfill({
        status: state.acceptStatus,
        json: { error: { code: 'invalid_capability', message: 'The invitation is invalid or unavailable.' } },
      });
    }
    state.acceptedCapability = body.capability;
    return route.fulfill({ status: 201, json: { account: memberAccount, csrf_token: 'csrf-memory-only-invite' } });
  });
}
