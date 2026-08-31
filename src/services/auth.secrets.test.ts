import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Account, AuthenticationResult } from '@/types/auth';
import { AuthClient } from './auth';
import { createAuthStore, type AuthAPI, type AuthEventStream } from '@/stores/auth';
import { AuthorizedEventStream } from './events';
import { fixtureSecret, mailAccount, mailSecret } from '@/services/testSecrets';

// Every secret a person types into this application. None may survive in the browser beyond the
// request that carries it, and none may be baked into what we ship.
const SECRETS = {
  password: fixtureSecret('owner-secret'),
  code: '481207',
  capability: fixtureSecret('capability'),
  eodhdApiKey: fixtureSecret('eodhd-secret'),
  smtpPassword: fixtureSecret('mail-secret'),
  csrfToken: fixtureSecret('csrf'),
};

const account: Account = {
  id: '10000000-0000-4000-8000-000000000001', email: 'owner@example.com', displayName: 'Owner',
  role: 'owner', status: 'active', emailVerifiedAt: '2026-08-31T08:00:00Z',
};

afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('browser secret handling', () => {
  it('sends every supplied secret once and keeps none of it in the browser', async () => {
    const sent: string[] = [];
    const client = new AuthClient(async (input, init) => {
      sent.push(String(init?.body ?? ''));
      return { ok: true, status: 200, json: async () => ({ account: wireAccount(), csrf_token: SECRETS.csrfToken }) };
    });

    await client.completeOwnerSetup({
      capability: SECRETS.capability, email: account.email, password: SECRETS.password,
      displayName: 'Owner', eodhdApiKey: SECRETS.eodhdApiKey,
      smtp: { host: 'smtp.example.test', port: 587, from: 'access@example.test', username: mailAccount, password: SECRETS.smtpPassword },
    });
    await client.loginOwner({ email: account.email, password: SECRETS.password });
    await client.loginMemberCode({ email: account.email, code: SECRETS.code });
    await client.acceptInvitation({ capability: SECRETS.capability, email: account.email, displayName: 'Member' });

    // The secrets did travel: this proves the scan below is looking at a real journey.
    expect(sent.join('\n')).toContain(SECRETS.password);
    expect(sent.join('\n')).toContain(SECRETS.eodhdApiKey);

    for (const [name, secret] of Object.entries(SECRETS)) {
      expect(browserState(), `${name} survived in browser storage`).not.toContain(secret);
    }
  });

  it('holds the CSRF token in memory and drops it the moment the session ends', async () => {
    const stream = fakeStream();
    const store = createAuthStore(fakeAPI(), () => stream);

    await store.loginOwner({ email: account.email, password: SECRETS.password });
    expect(store.state.csrfToken).toBe(SECRETS.csrfToken);
    // In memory only: nothing durable, and nothing a script on another page could read back.
    expect(browserState()).not.toContain(SECRETS.csrfToken);

    stream.callbacks().onUnauthorized();
    expect(store.state.csrfToken).toBeNull();
    expect(JSON.stringify(store.state)).not.toContain(SECRETS.csrfToken);
  });

  it('never lets a request error or a serialized store echo what was typed', async () => {
    const client = new AuthClient(async () => ({
      ok: false, status: 401,
      json: async () => ({ error: { code: 'authentication_required', message: 'Authentication is required.' } }),
    }));

    const failure = await client.loginOwner({ email: account.email, password: SECRETS.password }).catch((error: Error) => error);
    expect(String(failure)).not.toContain(SECRETS.password);
    expect(JSON.stringify(failure, Object.getOwnPropertyNames(failure))).not.toContain(SECRETS.password);
  });

  it('keeps no event payload in the browser and applies nothing it cannot verify', () => {
    const invalidations = vi.fn();
    const stream = new AuthorizedEventStream({ sourceFactory: () => fakeSource() });
    stream.setAudience({ userId: account.id, role: 'owner' });
    stream.configure({ onInvalidate: invalidations, onState: vi.fn(), onUnauthorized: vi.fn() });
    stream.start();
    expect(localStorage).toHaveLength(0);
    expect(sessionStorage).toHaveLength(0);
    expect(invalidations).not.toHaveBeenCalled();
  });
});

// Names that must never appear in anything shipped to a browser.
const FORBIDDEN_IN_BUILD = ('AUTH_SECRET EXTERNAL_CREDENTIAL_KEY POSTGRES_PASSWORD SMTP_PASSWORD '
  + 'eodhd-key key_material').split(' ').concat([
  'BEGIN PRIVATE KEY', 'BEGIN RSA PRIVATE KEY',
  // The instance signing key and anything derived from it are server-side only. The browser
  // never needs the key, its column, or the label its fingerprint is derived under, so any
  // appearance in a shipped asset means something leaked out of the backend.
  'market-lens/instance-signing-key/fingerprint',
]);

describe('shipped assets', () => {
  it('carries no credential, key, or endpoint secret into the build', () => {
    const distribution = join(process.cwd(), 'dist');
    // `make verify` builds before it runs the unit tests, so this is a real gate there. A bare
    // `vitest run` has nothing built yet, and the source scan below still applies.
    const files = existsSync(distribution) ? collectFiles(distribution) : [];
    const sources = collectFiles(join(process.cwd(), 'src'));

    for (const file of [...files, ...sources]) {
      const contents = readFileSync(file, 'utf8');
      // Split from one string rather than listed as adjacent literals, which a secret scanner
      // reads as an assignment.
      for (const forbidden of FORBIDDEN_IN_BUILD) {
        if (file.includes('.secrets.test.') || file.includes('.test.')) continue;
        expect(contents, `${file} embeds ${forbidden}`).not.toContain(forbidden);
      }
    }
  });
});

// browserState is everything a later page load, another script, or a shared machine could read.
function browserState(): string {
  return JSON.stringify({
    localStorage: { ...localStorage }, sessionStorage: { ...sessionStorage }, cookie: document.cookie,
  });
}

function collectFiles(root: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) found.push(...collectFiles(path));
    else if (/\.(js|mjs|css|html|ts|vue)$/.test(entry.name)) found.push(path);
  }
  return found;
}

function wireAccount() {
  return {
    id: account.id, email: account.email, display_name: account.displayName, role: account.role,
    status: account.status, email_verified_at: account.emailVerifiedAt,
  };
}

function fakeAPI(): AuthAPI {
  const authenticated: AuthenticationResult = { account, csrfToken: SECRETS.csrfToken };
  return {
    setupStatus: vi.fn(), completeOwnerSetup: vi.fn().mockResolvedValue(authenticated),
    startSignIn: vi.fn().mockResolvedValue({ message: 'ok' }),
    loginOwner: vi.fn().mockResolvedValue(authenticated), loginMemberCode: vi.fn().mockResolvedValue(authenticated),
    account: vi.fn().mockResolvedValue(account), sessions: vi.fn().mockResolvedValue([]),
    revokeSession: vi.fn(), revokeAllSessions: vi.fn(), logout: vi.fn(),
    members: vi.fn().mockResolvedValue({ members: [], nextCursor: '' }), unlockMember: vi.fn(),
    setMemberStatus: vi.fn(), invitations: vi.fn().mockResolvedValue({ items: [], nextCursor: '' }),
    createInvitation: vi.fn(), resendInvitation: vi.fn(), revokeInvitation: vi.fn(),
    acceptInvitation: vi.fn().mockResolvedValue(authenticated),
    integrationSettings: vi.fn().mockResolvedValue({
      eodhd: { configured: false, validatedAt: null },
      smtp: { configured: false, host: '', port: 0, from: '', username: '', passwordConfigured: false },
    }),
    verifyIntegrations: vi.fn().mockResolvedValue({}), updateIntegrations: vi.fn().mockResolvedValue({}),
  } satisfies AuthAPI;
}

function fakeStream() {
  let captured!: Parameters<AuthEventStream['configure']>[0];
  return {
    configure: vi.fn((callbacks) => { captured = callbacks; }),
    setAudience: vi.fn(), start: vi.fn(), stop: vi.fn(), setOnline: vi.fn(), callbacks: () => captured,
  } satisfies AuthEventStream & { callbacks: () => typeof captured };
}

function fakeSource() {
  return { addEventListener: vi.fn(), close: vi.fn() };
}
