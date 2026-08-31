import type { AcceptInvitationInput, IntegrationResults, IntegrationSettingsView, IntegrationUpdateInput, Account, AccountStatus, AuthenticationResult, Invitation, InvitationPage, Member, MemberCodeInput, MemberPage, OwnerLoginInput, OwnerSetupInput, Session } from '@/types/auth';

export type AuthFetcher = (input: string, init?: RequestInit) => Promise<Pick<Response, 'ok' | 'status' | 'json'>>;

export class AuthRequestError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    // Keyed by the wire field name. The text is always ours: the server sends a field and a
    // code, and every string the user reads is chosen locally, so an untrusted response body
    // can never put words on the screen.
    public readonly fieldErrors: Record<string, string> = {},
    // Each integration's own outcome, when the failing request reported one.
    public readonly results: IntegrationResults = {},
  ) {
    super(message);
    this.name = 'AuthRequestError';
  }
}

export class AuthClient {
  constructor(private readonly fetcher: AuthFetcher = (input, init) => fetch(input, init)) {}

  async setupStatus(): Promise<{ setupRequired: boolean }> {
    const body = await this.json('/api/v1/setup/status') as { setup_required?: unknown };
    if (typeof body.setup_required !== 'boolean') throw invalidResponse();
    return { setupRequired: body.setup_required };
  }

  async completeOwnerSetup(input: OwnerSetupInput): Promise<AuthenticationResult> {
    return authenticationFromWire(await this.json('/api/v1/auth/owner/setup', jsonMutation({
      capability: input.capability, email: input.email, password: input.password, display_name: input.displayName,
      eodhd_api_key: input.eodhdApiKey,
      smtp: input.smtp,
    })));
  }

  async startSignIn(email: string): Promise<{ message: string }> {
    const body = await this.json('/api/v1/auth/sign-in/start', jsonMutation({ email })) as { message?: unknown };
    if (typeof body.message !== 'string' || body.message.length === 0) throw invalidResponse();
    return { message: body.message };
  }

  async loginOwner(input: OwnerLoginInput): Promise<AuthenticationResult> {
    return authenticationFromWire(await this.json('/api/v1/auth/owner/login', jsonMutation(input)));
  }

  async loginMemberCode(input: MemberCodeInput): Promise<AuthenticationResult> {
    return authenticationFromWire(await this.json('/api/v1/auth/member/code/verify', jsonMutation(input)));
  }

  async account(): Promise<Account> {
    return accountFromWire(await this.json('/api/v1/account'));
  }

  async sessions(): Promise<Session[]> {
    const body = await this.json('/api/v1/account/sessions') as { items?: unknown };
    if (!Array.isArray(body.items)) throw invalidResponse();
    return body.items.map(sessionFromWire);
  }

  async revokeSession(sessionID: string, csrfToken: string): Promise<void> {
    await this.empty(`/api/v1/account/sessions/${encodeURIComponent(sessionID)}`, csrfMutation('DELETE', csrfToken));
  }

  async revokeAllSessions(csrfToken: string): Promise<void> {
    await this.empty('/api/v1/account/sessions', csrfMutation('DELETE', csrfToken));
  }

  async members(cursor = ''): Promise<MemberPage> {
    const path = cursor ? `/api/v1/owner/members?cursor=${encodeURIComponent(cursor)}` : '/api/v1/owner/members';
    const body = await this.json(path) as { members?: unknown; next_cursor?: unknown };
    if (!Array.isArray(body.members) || typeof body.next_cursor !== 'string') throw invalidResponse();
    return { members: body.members.map(memberFromWire), nextCursor: body.next_cursor };
  }

  async unlockMember(memberID: string, csrfToken: string): Promise<void> {
    await this.empty(`/api/v1/owner/members/${encodeURIComponent(memberID)}/unlock`, csrfMutation('POST', csrfToken));
  }

  /** The non-secret configuration the owner may edit. Secrets are never returned. */
  async integrationSettings(): Promise<IntegrationSettingsView> {
    const body = await this.json('/api/v1/owner/integrations') as { settings?: unknown };
    return integrationSettingsFromWire(body.settings);
  }

  /** Checks a configuration against the real services and stores nothing. */
  async verifyIntegrations(update: IntegrationUpdateInput, csrfToken: string): Promise<IntegrationResults> {
    return integrationResultsFromWire(await this.json('/api/v1/owner/integrations/verify',
      csrfJSONMutation('POST', csrfToken, integrationUpdateToWire(update))));
  }

  /** Stores a configuration, but only after it has been proven to work. */
  async updateIntegrations(update: IntegrationUpdateInput, csrfToken: string): Promise<IntegrationResults> {
    return integrationResultsFromWire(await this.json('/api/v1/owner/integrations',
      csrfJSONMutation('PUT', csrfToken, integrationUpdateToWire(update))));
  }

  async invitations(cursor = ''): Promise<InvitationPage> {
    const path = cursor ? `/api/v1/owner/invitations?cursor=${encodeURIComponent(cursor)}` : '/api/v1/owner/invitations';
    const body = await this.json(path) as { items?: unknown; next_cursor?: unknown };
    if (!Array.isArray(body.items) || typeof body.next_cursor !== 'string') throw invalidResponse();
    return { items: body.items.map(invitationFromWire), nextCursor: body.next_cursor };
  }

  async createInvitation(email: string, csrfToken: string): Promise<Invitation> {
    return invitationFromWire(await this.json('/api/v1/owner/invitations',
      csrfJSONMutation('POST', csrfToken, { email })));
  }

  async resendInvitation(invitationID: string, csrfToken: string): Promise<Invitation> {
    return invitationFromWire(await this.json(
      `/api/v1/owner/invitations/${encodeURIComponent(invitationID)}/resend`, csrfMutation('POST', csrfToken)));
  }

  async revokeInvitation(invitationID: string, csrfToken: string): Promise<void> {
    await this.empty(`/api/v1/owner/invitations/${encodeURIComponent(invitationID)}`, csrfMutation('DELETE', csrfToken));
  }

  async acceptInvitation(input: AcceptInvitationInput): Promise<AuthenticationResult> {
    return authenticationFromWire(await this.json('/api/v1/auth/invitations/accept', jsonMutation({
      capability: input.capability, email: input.email, display_name: input.displayName,
    })));
  }

  async setMemberStatus(memberID: string, status: AccountStatus, csrfToken: string): Promise<Member> {
    return memberFromWire(await this.json(
      `/api/v1/owner/members/${encodeURIComponent(memberID)}/status`,
      csrfJSONMutation('PATCH', csrfToken, { status })));
  }

  async logout(csrfToken: string): Promise<void> {
    await this.empty('/api/v1/auth/logout', csrfMutation('POST', csrfToken));
  }

  private async json(path: string, init: RequestInit = {}): Promise<unknown> {
    const response = await this.request(path, init);
    try {
      return await response.json();
    } catch {
      throw invalidResponse();
    }
  }

  private async empty(path: string, init: RequestInit): Promise<void> {
    await this.request(path, init);
  }

  private async request(path: string, init: RequestInit): Promise<Pick<Response, 'ok' | 'status' | 'json'>> {
    let response: Pick<Response, 'ok' | 'status' | 'json'>;
    try {
      response = await this.fetcher(path, { credentials: 'same-origin', ...init });
    } catch {
      throw new AuthRequestError(0, 'network_error', 'Authentication service is unavailable.');
    }
    if (response.ok) return response;
    let code = 'request_failed';
    let fieldErrors: Record<string, string> = {};
    let results: IntegrationResults = {};
    try {
      const body = await response.json() as {
        error?: { code?: unknown; fields?: unknown; results?: unknown };
      };
      if (typeof body.error?.code === 'string') code = body.error.code;
      fieldErrors = safeFieldMessages(body.error?.fields);
      results = safeIntegrationResults(body.error?.results);
    } catch {
      // Response bodies are untrusted; only allowlisted local messages reach the UI.
    }
    throw new AuthRequestError(response.status, code, safeMessage(code), fieldErrors, results);
  }
}

function jsonMutation(body: unknown): RequestInit {
  return { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

function csrfMutation(method: 'POST' | 'DELETE', csrfToken: string): RequestInit {
  return { method, headers: { 'X-CSRF-Token': csrfToken } };
}

function csrfJSONMutation(method: 'POST' | 'PATCH' | 'PUT', csrfToken: string, body: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(body),
  };
}

function safeMessage(code: string): string {
  const messages: Record<string, string> = {
    authentication_required: 'Authentication is required.', authentication_failed: 'Authentication failed.',
    csrf_rejected: 'Request verification failed.', invalid_capability: 'The request is invalid or unavailable.',
    setup_closed: 'Owner setup is unavailable.', invalid_request: 'The request is invalid.',
    not_found: 'The requested item was not found.', rate_limited: 'Please wait before trying again.',
    authorization_denied: 'Owner access is required.', conflict: 'That change conflicts with the current state.',
    temporarily_unavailable: 'Authentication is temporarily unavailable.',
    provider_unavailable: 'Provider validation is temporarily unavailable.',
    invalid_setup: 'Some of the details you entered need attention.',
    dependency_unreachable: 'Setup could not reach a service it needs to check.',
  };
  return messages[code] ?? 'Authentication request failed.';
}

// SETUP_FIELD_MESSAGES holds every word the setup form can show against an input. The server
// sends only a field name and a code; the prose lives here, so a response body - or a mail
// server banner quoted into one - can never reach the screen.
const SETUP_FIELD_MESSAGES: Record<string, string> = {
  'email:invalid_format': 'Enter a valid email address, such as you@example.com.',
  'display_name:invalid_format': 'Enter a display name of 1 to 120 characters, with no leading or trailing spaces.',
  'password:too_short': 'Password must be at least 12 characters.',
  'password:too_long': 'Password must be at most 1024 characters.',
  'eodhd_api_key:invalid_format': 'Enter your EODHD API key.',
  'eodhd_api_key:rejected': 'EODHD rejected this API key. Check it in your EODHD account and paste it again.',
  'eodhd_api_key:unreachable': 'EODHD could not be reached, so the key was not checked. Try again shortly.',
  'smtp_host:invalid_format': 'Enter the mail server host name, such as smtp.example.com.',
  'smtp_host:unreachable': 'Could not reach the mail server. Check the host and port, and that it accepts connections from here.',
  'smtp_host:tls_failed': 'The connection to the mail server could not be encrypted. Check the host and port.',
  'smtp_port:out_of_range': 'SMTP port must be between 1 and 65535. It is usually 587.',
  'smtp_from:invalid_format': 'Enter the plain address that mail is sent from, such as market-lens@example.com.',
  'smtp_from:sender_rejected': 'The mail server refused this sender address. It usually has to be an address that server may send as.',
  'smtp_username:invalid_format': 'The SMTP username is too long or contains control characters.',
  'smtp_username:required': 'Enter the SMTP username that goes with this password, or clear the password to connect without authentication.',
  'smtp_username:auth_rejected': 'The mail server rejected these credentials. Check the username and password.',
  'smtp_password:invalid_format': 'The SMTP password is too long or contains control characters.',
  'smtp_password:required': 'Enter the SMTP password that goes with this username, or clear the username to connect without authentication.',
  'smtp_password:auth_rejected': 'The mail server rejected these credentials. Check the username and password.',
};

const SETUP_FIELDS = new Set([
  'email', 'display_name', 'password', 'eodhd_api_key',
  'smtp_host', 'smtp_port', 'smtp_from', 'smtp_username', 'smtp_password',
]);

function safeFieldMessages(fields: unknown): Record<string, string> {
  if (!Array.isArray(fields)) return {};
  const messages: Record<string, string> = {};
  for (const entry of fields) {
    if (typeof entry !== 'object' || entry === null) continue;
    const { field, code } = entry as { field?: unknown; code?: unknown };
    if (typeof field !== 'string' || typeof code !== 'string') continue;
    if (!SETUP_FIELDS.has(field)) continue;
    messages[field] = SETUP_FIELD_MESSAGES[`${field}:${code}`] ?? 'Check this value and try again.';
  }
  return messages;
}

function invalidResponse(): AuthRequestError {
  return new AuthRequestError(0, 'invalid_response', 'Authentication service returned an invalid response.');
}

function authenticationFromWire(value: unknown): AuthenticationResult {
  const wire = value as { account?: unknown; csrf_token?: unknown };
  if (typeof wire?.csrf_token !== 'string' || wire.csrf_token.length === 0) throw invalidResponse();
  return { account: accountFromWire(wire.account), csrfToken: wire.csrf_token };
}

function accountFromWire(value: unknown): Account {
  const wire = value as Record<string, unknown>;
  if (!wire || typeof wire.id !== 'string' || typeof wire.email !== 'string' || typeof wire.display_name !== 'string' ||
      (wire.role !== 'owner' && wire.role !== 'member') || (wire.status !== 'active' && wire.status !== 'deactivated') ||
      typeof wire.email_verified_at !== 'string') throw invalidResponse();
  return { id: wire.id, email: wire.email, displayName: wire.display_name, role: wire.role,
    status: wire.status, emailVerifiedAt: wire.email_verified_at };
}

function memberFromWire(value: unknown): Member {
  const wire = value as Record<string, unknown>;
  const states = ['available', 'temporarily_blocked', 'administratively_locked'];
  if (!wire || typeof wire.id !== 'string' || typeof wire.email !== 'string' || typeof wire.display_name !== 'string' ||
      (wire.status !== 'active' && wire.status !== 'deactivated') ||
      typeof wire.login_state !== 'string' || !states.includes(wire.login_state) ||
      (wire.blocked_until !== null && typeof wire.blocked_until !== 'string') ||
      (wire.locked_at !== null && typeof wire.locked_at !== 'string') ||
      typeof wire.active_session_count !== 'number' || typeof wire.created_at !== 'string') throw invalidResponse();
  return {
    id: wire.id, email: wire.email, displayName: wire.display_name, status: wire.status,
    loginState: wire.login_state as Member['loginState'],
    blockedUntil: wire.blocked_until as string | null, lockedAt: wire.locked_at as string | null,
    activeSessionCount: wire.active_session_count, createdAt: wire.created_at,
  };
}

function invitationFromWire(value: unknown): Invitation {
  const wire = value as Record<string, unknown>;
  const states = ['pending', 'accepted', 'revoked', 'expired'];
  const deliveryStates = ['pending', 'sending', 'sent', 'failed', 'abandoned'];
  if (!wire || typeof wire.id !== 'string' || typeof wire.email !== 'string' ||
      typeof wire.state !== 'string' || !states.includes(wire.state) ||
      typeof wire.expires_at !== 'string' ||
      (wire.accepted_at !== null && typeof wire.accepted_at !== 'string') ||
      typeof wire.delivery_state !== 'string' || !deliveryStates.includes(wire.delivery_state) ||
      (wire.delivery_error !== null && typeof wire.delivery_error !== 'string') ||
      typeof wire.resend_count !== 'number' || typeof wire.created_at !== 'string') throw invalidResponse();
  return {
    id: wire.id, email: wire.email, state: wire.state as Invitation['state'],
    expiresAt: wire.expires_at, acceptedAt: wire.accepted_at as string | null,
    deliveryState: wire.delivery_state as Invitation['deliveryState'],
    deliveryError: wire.delivery_error as string | null,
    resendCount: wire.resend_count, createdAt: wire.created_at,
  };
}

function sessionFromWire(value: unknown): Session {
  const wire = value as Record<string, unknown>;
  if (!wire || typeof wire.id !== 'string' || typeof wire.current !== 'boolean' || typeof wire.device_label !== 'string' ||
      typeof wire.created_at !== 'string' || typeof wire.last_seen_at !== 'string' || typeof wire.idle_expires_at !== 'string' ||
      typeof wire.absolute_expires_at !== 'string' || typeof wire.revoked !== 'boolean') throw invalidResponse();
  return { id: wire.id, current: wire.current, deviceLabel: wire.device_label, createdAt: wire.created_at,
    lastSeenAt: wire.last_seen_at, idleExpiresAt: wire.idle_expires_at,
    absoluteExpiresAt: wire.absolute_expires_at, revoked: wire.revoked };
}

// An omitted password is the difference between "keep the stored one" and "remove
// authentication", so it is only sent when it was actually typed.
function integrationUpdateToWire(update: IntegrationUpdateInput): Record<string, unknown> {
  const wire: Record<string, unknown> = {};
  if (update.smtp) {
    wire.smtp = {
      host: update.smtp.host, port: update.smtp.port, from: update.smtp.from,
      username: update.smtp.username,
      ...(update.smtp.password === undefined ? {} : { password: update.smtp.password }),
    };
  }
  if (update.eodhd) wire.eodhd = { api_key: update.eodhd.apiKey };
  return wire;
}

function integrationSettingsFromWire(value: unknown): IntegrationSettingsView {
  if (typeof value !== 'object' || value === null) throw invalidResponse();
  const wire = value as { eodhd?: Record<string, unknown>; smtp?: Record<string, unknown> };
  const eodhd = wire.eodhd ?? {};
  const smtp = wire.smtp ?? {};
  if (typeof eodhd.configured !== 'boolean' || typeof smtp.configured !== 'boolean') throw invalidResponse();
  return {
    eodhd: {
      configured: eodhd.configured,
      validatedAt: typeof eodhd.validated_at === 'string' ? eodhd.validated_at : null,
    },
    smtp: {
      configured: smtp.configured,
      host: typeof smtp.host === 'string' ? smtp.host : '',
      port: typeof smtp.port === 'number' ? smtp.port : 0,
      from: typeof smtp.from === 'string' ? smtp.from : '',
      username: typeof smtp.username === 'string' ? smtp.username : '',
      passwordConfigured: smtp.password_configured === true,
    },
  };
}

// Outcomes are an allowlist too: an unrecognised value is dropped rather than shown, so a
// response can never put an unknown state on the screen.
const INTEGRATION_OUTCOMES = new Set(['verified', 'failed', 'not_checked']);

function safeIntegrationResults(value: unknown): IntegrationResults {
  if (typeof value !== 'object' || value === null) return {};
  const results: IntegrationResults = {};
  for (const section of ['eodhd', 'smtp'] as const) {
    const outcome = (value as Record<string, unknown>)[section];
    if (typeof outcome === 'string' && INTEGRATION_OUTCOMES.has(outcome)) results[section] = outcome;
  }
  return results;
}

function integrationResultsFromWire(body: unknown): IntegrationResults {
  if (typeof body !== 'object' || body === null) return {};
  return safeIntegrationResults((body as { results?: unknown }).results);
}
