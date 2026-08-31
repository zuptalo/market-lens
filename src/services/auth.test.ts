import { describe, expect, it, vi } from 'vitest';
import { AuthClient, AuthRequestError } from './auth';
import { mailAccount, mailSecret } from '@/services/testSecrets';

const accountWire = {
  id: '10000000-0000-4000-8000-000000000001',
  email: 'owner@example.com',
  display_name: 'Owner',
  role: 'owner',
  status: 'active',
  email_verified_at: '2026-08-30T08:00:00Z',
};

describe('AuthClient', () => {
  it('uses expanded encrypted setup and generic sign-in contracts without recovery methods', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(response(201, { account: accountWire, csrf_token: 'csrf-setup-secret' }))
      .mockResolvedValueOnce(response(202, { message: 'If you have an account, you should receive an email with a six-digit passcode.' }));
    const client = new AuthClient(fetcher) as AuthClient & {
      startSignIn(email: string): Promise<{ message: string }>;
    };
    await client.completeOwnerSetup({
      capability: 'setup-capability-secret', email: 'owner@example.com', password: 'password-secret', displayName: 'Owner',
      eodhdApiKey: 'eodhd-key-secret', smtp: {
        host: 'smtp.example.test', port: 587, from: 'access@example.test', username: mailAccount, password: mailSecret,
      },
    } as never);
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/auth/owner/setup', expect.objectContaining({
      body: JSON.stringify({
        capability: 'setup-capability-secret', email: 'owner@example.com', password: 'password-secret', display_name: 'Owner',
        eodhd_api_key: 'eodhd-key-secret', smtp: {
          host: 'smtp.example.test', port: 587, from: 'access@example.test', username: mailAccount, password: mailSecret,
        },
      }),
    }));
    await expect(client.startSignIn('someone@example.com')).resolves.toEqual({
      message: 'If you have an account, you should receive an email with a six-digit passcode.',
    });
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/auth/sign-in/start', expect.objectContaining({
      body: JSON.stringify({ email: 'someone@example.com' }), credentials: 'same-origin', method: 'POST',
    }));
    expect('requestOwnerRecovery' in client).toBe(false);
    expect('completeOwnerRecovery' in client).toBe(false);
  });

  it('maps setup, login, account, and session responses without persisting secrets', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(response(200, { setup_required: true }))
      .mockResolvedValueOnce(response(201, { account: accountWire, csrf_token: 'csrf-setup-secret' }))
      .mockResolvedValueOnce(response(200, { account: accountWire, csrf_token: 'csrf-login-secret' }))
      .mockResolvedValueOnce(response(200, accountWire))
      .mockResolvedValueOnce(response(200, { items: [{
        id: '20000000-0000-4000-8000-000000000001', current: true, device_label: 'Firefox',
        created_at: '2026-08-30T08:00:00Z', last_seen_at: '2026-08-30T08:01:00Z',
        idle_expires_at: '2026-08-30T16:01:00Z', absolute_expires_at: '2026-09-29T08:00:00Z', revoked: false,
      }] }));
    const client = new AuthClient(fetcher);

    await expect(client.setupStatus()).resolves.toEqual({ setupRequired: true });
    await expect(client.completeOwnerSetup({
      capability: 'setup-capability-secret', email: 'owner@example.com', password: 'password-secret', displayName: 'Owner',
      eodhdApiKey: 'eodhd-key-secret', smtp: {
        host: 'smtp.example.test', port: 587, from: 'access@example.test', username: '', password: '',
      },
    }))
      .resolves.toMatchObject({ account: { displayName: 'Owner' }, csrfToken: 'csrf-setup-secret' });
    await expect(client.loginOwner({ email: 'owner@example.com', password: 'password-secret' }))
      .resolves.toMatchObject({ account: { role: 'owner' }, csrfToken: 'csrf-login-secret' });
    await expect(client.account()).resolves.toMatchObject({ email: 'owner@example.com' });
    await expect(client.sessions()).resolves.toEqual([expect.objectContaining({ current: true, deviceLabel: 'Firefox' })]);

    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/auth/owner/setup', expect.objectContaining({
      method: 'POST', credentials: 'same-origin',
      body: JSON.stringify({
        capability: 'setup-capability-secret', email: 'owner@example.com', password: 'password-secret', display_name: 'Owner',
        eodhd_api_key: 'eodhd-key-secret', smtp: {
          host: 'smtp.example.test', port: 587, from: 'access@example.test', username: '', password: '',
        },
      }),
    }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/account', expect.objectContaining({ credentials: 'same-origin' }));
  });

  it('sends CSRF only on authenticated mutations and returns safe classified errors', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(response(204))
      .mockResolvedValueOnce(response(204))
      .mockResolvedValueOnce(response(204))
      .mockResolvedValueOnce(response(401, { error: { code: 'authentication_required', message: 'session=raw-secret' } }));
    const client = new AuthClient(fetcher);

    await client.revokeSession('session/id', 'csrf-memory-only');
    await client.revokeAllSessions('csrf-memory-only');
    await client.logout('csrf-memory-only');

    for (let call = 0; call < 3; call += 1) {
      expect(fetcher.mock.calls[call][1]).toEqual(expect.objectContaining({
        credentials: 'same-origin', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf-memory-only' }),
      }));
    }
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/account/sessions/session%2Fid');

    await expect(client.account()).rejects.toEqual(expect.objectContaining<AuthRequestError>({
      name: 'AuthRequestError', status: 401, code: 'authentication_required', message: 'Authentication is required.',
      fieldErrors: {}, results: {},
    }));
  });
});

describe('AuthClient member administration', () => {
  it('reads member security metadata and unlocks with CSRF', async () => {
    const memberWire = {
      id: 'a0000000-0000-4000-8000-000000000001',
      email: 'member@example.com',
      display_name: 'Member One',
      status: 'active',
      login_state: 'administratively_locked',
      blocked_until: null,
      locked_at: '2026-08-30T10:00:00Z',
      active_session_count: 2,
      created_at: '2026-08-30T09:00:00Z',
    };
    const fetcher = vi.fn()
      .mockResolvedValueOnce(response(200, { members: [memberWire], next_cursor: '' }))
      .mockResolvedValueOnce(response(204));
    const client = new AuthClient(fetcher);

    await expect(client.members()).resolves.toEqual({
      members: [{
        id: 'a0000000-0000-4000-8000-000000000001', email: 'member@example.com', displayName: 'Member One',
        status: 'active', loginState: 'administratively_locked', blockedUntil: null,
        lockedAt: '2026-08-30T10:00:00Z', activeSessionCount: 2, createdAt: '2026-08-30T09:00:00Z',
      }],
      nextCursor: '',
    });
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/owner/members', expect.objectContaining({ credentials: 'same-origin' }));

    await client.unlockMember('a0000000-0000-4000-8000-000000000001', 'csrf-memory-only');
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/owner/members/a0000000-0000-4000-8000-000000000001/unlock',
      expect.objectContaining({
        method: 'POST', credentials: 'same-origin',
        headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf-memory-only' }),
      }));
  });

  it('rejects a member payload that does not match the contract', async () => {
    const fetcher = vi.fn().mockResolvedValue(response(200, { members: [{ id: 'a', login_state: 'sudo' }], next_cursor: '' }));
    await expect(new AuthClient(fetcher).members()).rejects.toMatchObject({ code: 'invalid_response' });
  });

  it('reports owner-only denial without leaking whether the member exists', async () => {
    const fetcher = vi.fn().mockResolvedValue(response(403, { error: { code: 'authorization_denied' } }));
    await expect(new AuthClient(fetcher).unlockMember('a0000000-0000-4000-8000-000000000001', 'csrf'))
      .rejects.toMatchObject({ status: 403, code: 'authorization_denied', message: 'Owner access is required.' });
  });
});

function response(status: number, body?: unknown): Pick<Response, 'ok' | 'status' | 'json'> {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}
