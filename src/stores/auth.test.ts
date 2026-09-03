import { describe, expect, it, vi } from 'vitest';
import type { Account, AuthenticationResult, Session } from '@/types/auth';
import { createAuthStore, type AuthAPI, type AuthEventStream } from './auth';
import { AuthRequestError } from '@/services/auth';

const account: Account = {
  id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', displayName: 'Owner',
  role: 'owner', status: 'active', emailVerifiedAt: '2026-08-30T08:00:00Z',
};

describe('auth store', () => {
  it('advances every email to OTP and exposes owner password only as an explicit client choice', async () => {
    const api = fakeAPI();
    const store = createAuthStore(api, () => fakeStream()) as ReturnType<typeof createAuthStore> & {
      startSignIn(email: string): Promise<void>;
      selectOwnerPassword(): void;
    };

    await store.startSignIn('someone@example.com');
    expect(api.startSignIn).toHaveBeenCalledWith('someone@example.com');
    expect(store.state).toMatchObject({ signInStep: 'otp', signInEmail: 'someone@example.com' });
    store.selectOwnerPassword();
    expect(store.state).toMatchObject({ signInStep: 'owner-password', signInEmail: 'someone@example.com' });
    expect('requestOwnerRecovery' in store).toBe(false);
    expect('completeOwnerRecovery' in store).toBe(false);
  });

  it('restores a cookie session, keeps CSRF only in memory, and clears auth on logout', async () => {
    const api = fakeAPI();
    const stream = fakeStream();
    const localSet = vi.spyOn(Storage.prototype, 'setItem');
    const sessionSet = vi.spyOn(sessionStorage, 'setItem');
    const store = createAuthStore(api, () => stream);

    await store.restore();
    expect(store.state.status).toBe('authenticated');
    expect(store.state.account).toEqual(account);
    expect(store.state.csrfToken).toBeNull();
    expect(stream.start).toHaveBeenCalledOnce();

    await store.loginOwner({ email: account.email, password: 'password-secret' });
    expect(store.state.csrfToken).toBe('csrf-memory-secret');
    expect(localSet).not.toHaveBeenCalled();
    expect(sessionSet).not.toHaveBeenCalled();

    await store.logout();
    expect(api.logout).toHaveBeenCalledWith('csrf-memory-secret');
    expect(store.state).toMatchObject({ status: 'anonymous', account: null, csrfToken: null });
    expect(stream.stop).toHaveBeenCalled();
  });

  it('deduplicates account invalidations, refreshes snapshots, and exposes reconnect/stale/offline state', async () => {
    const api = fakeAPI();
    const stream = fakeStream();
    const store = createAuthStore(api, () => stream);
    await store.loginOwner({ email: account.email, password: 'password-secret' });
    const callbacks = stream.callbacks();

    callbacks.onState('reconnecting');
    expect(store.state.connection).toBe('reconnecting');
    callbacks.onState('stale');
    expect(store.state.connection).toBe('stale');
    callbacks.onState('offline');
    expect(store.state.connection).toBe('offline');

    callbacks.onInvalidate({ id: '41', type: 'account.changed.v1', entityType: 'account', entityId: account.id });
    callbacks.onInvalidate({ id: '41', type: 'account.changed.v1', entityType: 'account', entityId: account.id });
    await Promise.resolve();
    expect(api.account).toHaveBeenCalledTimes(1);

    callbacks.onUnauthorized();
    expect(store.state).toMatchObject({ status: 'anonymous', account: null, csrfToken: null });
  });

  it('tells the stream which account it is watching so private events can be verified', async () => {
    const api = fakeAPI();
    const stream = fakeStream();
    const store = createAuthStore(api, () => stream);

    await store.loginOwner({ email: account.email, password: 'password-secret' });

    // Role alone cannot distinguish one member's private events from another's.
    expect(stream.setAudience).toHaveBeenCalledWith({ userId: account.id, role: 'owner' });
  });

  it('ends the session and stops the stream the moment authorization expires behind it', async () => {
    const api = fakeAPI();
    const stream = fakeStream();
    const store = createAuthStore(api, () => stream);
    await store.loginOwner({ email: account.email, password: 'password-secret' });

    stream.callbacks().onUnauthorized();

    expect(store.state).toMatchObject({ status: 'anonymous', account: null, csrfToken: null, connection: 'offline' });
    expect(stream.stop).toHaveBeenCalled();
    // A revoked session must not leave a mutation path open behind the cleared state.
    await expect(store.logout()).rejects.toThrow();
  });

  it('refreshes snapshots from events without ever scheduling a poll', async () => {
    const interval = vi.spyOn(globalThis, 'setInterval');
    const api = fakeAPI();
    const stream = fakeStream();
    const store = createAuthStore(api, () => stream);
    await store.loginOwner({ email: account.email, password: 'password-secret' });

    stream.callbacks().onInvalidate({ id: '91', type: 'session.revoked.v1', entityType: 'session', entityId: 'session-1' });
    await Promise.resolve();

    expect(api.account).toHaveBeenCalledTimes(1);
    expect(interval).not.toHaveBeenCalled();
    interval.mockRestore();
  });

  it('never invalidates an owner roster from a member session', async () => {
    const api = fakeAPI();
    const stream = fakeStream();
    const memberAccount = { ...account, id: '10000000-0000-4000-8000-000000000601', role: 'member' as const };
    api.loginMemberCode.mockResolvedValue({ account: memberAccount, csrfToken: 'csrf-memory-secret' });
    const store = createAuthStore(api, () => stream);
    await store.loginMemberCode({ email: memberAccount.email, code: '424242' });

    expect(stream.setAudience).toHaveBeenCalledWith({ userId: memberAccount.id, role: 'member' });
    const rosterRefreshed = vi.fn();
    store.onMembersChanged(rosterRefreshed);
    // The server never sends owner-scoped events to a member and the stream drops them, so a
    // member session has no path that refreshes administration data.
    stream.callbacks().onInvalidate({ id: '92', type: 'daily_bar.changed.v1', entityType: 'daily_bar', entityId: 'bar-1' });
    expect(rosterRefreshed).not.toHaveBeenCalled();
  });

  // An unreadable answer is not the server saying "not signed in" either, so the status is
  // 'unreachable' rather than 'anonymous'. The claim this test exists for is unchanged: whatever
  // went wrong, nothing the server said reaches the state a person can read.
  it('reports a failed restore without leaking server error details', async () => {
    const api = fakeAPI();
    api.account.mockRejectedValueOnce(new Error('cookie=raw-secret'));
    const store = createAuthStore(api, () => fakeStream());

    await store.restore();
    expect(store.state).toMatchObject({ status: 'unreachable', account: null, error: null });
    expect(JSON.stringify(store.state)).not.toContain('raw-secret');
  });
});

describe('restored sessions', () => {
  it('recovers the CSRF token from its cookie so a reloaded page can still mutate', async () => {
    document.cookie = '__Host-market_lens_csrf=csrf-from-cookie; path=/; secure';
    const api = fakeAPI();
    const store = createAuthStore(api, fakeStream);

    await store.restore();

    expect(store.state.status).toBe('authenticated');
    // Without this the person is signed in yet cannot sign out, revoke a device, or
    // administer members until they authenticate again.
    await store.unlockMember('a0000000-0000-4000-8000-000000000001');
    expect(api.unlockMember).toHaveBeenCalledWith('a0000000-0000-4000-8000-000000000001', 'csrf-from-cookie');
    await store.logout();
    expect(api.logout).toHaveBeenCalledWith('csrf-from-cookie');
  });

  it('stays read-only when no CSRF cookie is present', async () => {
    document.cookie = '__Host-market_lens_csrf=; path=/; secure; max-age=0';
    const api = fakeAPI();
    const store = createAuthStore(api, fakeStream);
    await store.restore();
    await expect(store.logout()).rejects.toThrow();
  });
});

function fakeAPI() {
  const authenticated: AuthenticationResult = { account, csrfToken: 'csrf-memory-secret' };
  const sessions: Session[] = [];
  return {
    setupStatus: vi.fn(), completeOwnerSetup: vi.fn().mockResolvedValue(authenticated),
    startSignIn: vi.fn().mockResolvedValue({ message: 'If you have an account, you should receive an email with a six-digit passcode.' }),
    loginOwner: vi.fn().mockResolvedValue(authenticated), loginMemberCode: vi.fn().mockResolvedValue(authenticated),
    account: vi.fn().mockResolvedValue(account),
    sessions: vi.fn().mockResolvedValue(sessions), revokeSession: vi.fn(), revokeAllSessions: vi.fn(), logout: vi.fn(),
    members: vi.fn().mockResolvedValue({ members: [], nextCursor: '' }), unlockMember: vi.fn(),
    setMemberStatus: vi.fn(), invitations: vi.fn().mockResolvedValue({ items: [], nextCursor: '' }),
    createInvitation: vi.fn(), resendInvitation: vi.fn(), revokeInvitation: vi.fn(),
    acceptInvitation: vi.fn().mockResolvedValue(authenticated),
    integrationSettings: vi.fn().mockResolvedValue({
      eodhd: { configured: true, validatedAt: null },
      smtp: { configured: true, host: 'smtp.example.test', port: 587, from: 'a@example.test', username: '', passwordConfigured: false },
    }),
    verifyIntegrations: vi.fn().mockResolvedValue({}), updateIntegrations: vi.fn().mockResolvedValue({}),
  } satisfies AuthAPI & Record<string, ReturnType<typeof vi.fn>>;
}

function fakeStream() {
  let captured!: Parameters<AuthEventStream['configure']>[0];
  return {
    configure: vi.fn((callbacks) => { captured = callbacks; }),
    setAudience: vi.fn(), start: vi.fn(), stop: vi.fn(), setOnline: vi.fn(),
    callbacks: () => captured,
  } satisfies AuthEventStream & { callbacks: () => typeof captured };
}

describe('auth store when the server is unreachable', () => {
  // The deployment rolls a new pod on every release. For the seconds the old pod is gone, any
  // request fails with a network or gateway error — and the store treated that exactly like a
  // 401, declared itself anonymous, and let the router send a signed-in person to the login
  // page. They signed in again, which created a second session while the first was still
  // perfectly valid, which is why the device list accumulated one entry per release.
  //
  // A server that cannot be reached has not said anything. Only a server that answers 401 or
  // 403 has said the caller is not signed in.
  it('does not sign a person out because it could not reach the server', async () => {
    const api = fakeAPI();
    api.account = vi.fn().mockRejectedValue(new AuthRequestError(0, 'network_error', 'unavailable'));
    const store = createAuthStore(api, () => fakeStream());

    await store.restore();

    expect(store.state.status).not.toBe('anonymous');
    expect(store.state.status).toBe('unreachable');
  });

  it('signs a person out only when the server actually says so', async () => {
    for (const status of [401, 403]) {
      const api = fakeAPI();
      api.account = vi.fn().mockRejectedValue(new AuthRequestError(status, 'unauthorized', 'no'));
      const store = createAuthStore(api, () => fakeStream());
      await store.restore();
      expect(store.state.status).toBe('anonymous');
    }
  });

  // A gateway error during a rollout is the same class of event as a dropped connection.
  it('treats a gateway error as unreachable rather than as a sign-out', async () => {
    for (const status of [500, 502, 503, 504]) {
      const api = fakeAPI();
      api.account = vi.fn().mockRejectedValue(new AuthRequestError(status, 'unavailable', 'no'));
      const store = createAuthStore(api, () => fakeStream());
      await store.restore();
      expect(store.state.status).toBe('unreachable');
    }
  });

  // The same rule on a refresh triggered by an event, where the person is already signed in and
  // looking at their data. Losing it to a blip is worse: they were mid-task.
  it('keeps an established session through a failed refresh', async () => {
    const api = fakeAPI();
    const stream = fakeStream();
    const store = createAuthStore(api, () => stream);
    await store.restore();
    expect(store.state.status).toBe('authenticated');

    // The refresh a stream event triggers, which is how it happens in production.
    api.account = vi.fn().mockRejectedValue(new AuthRequestError(0, 'network_error', 'unavailable'));
    stream.callbacks().onInvalidate({ id: 'e1', type: 'account.changed.v1', entityType: 'account', entityId: account.id });
    await Promise.resolve();
    await Promise.resolve();

    expect(store.state.status).toBe('authenticated');
    expect(store.state.account).not.toBeNull();
  });

  it('recovers on its own once the server answers again', async () => {
    const api = fakeAPI();
    api.account = vi.fn().mockRejectedValue(new AuthRequestError(0, 'network_error', 'unavailable'));
    const store = createAuthStore(api, () => fakeStream());
    await store.restore();
    expect(store.state.status).toBe('unreachable');

    api.account = vi.fn().mockResolvedValue(account);
    await store.restore();

    expect(store.state.status).toBe('authenticated');
  });
});
