import type { AcceptInvitationInput, Account, AccountStatus, AuthenticationResult, Invitation, InvitationPage, Member, MemberCodeInput, MemberPage, OwnerLoginInput, OwnerSetupInput, Session } from '@/types/auth';

export type AuthFetcher = (input: string, init?: RequestInit) => Promise<Pick<Response, 'ok' | 'status' | 'json'>>;

export class AuthRequestError extends Error {
  constructor(public readonly status: number, public readonly code: string, message: string) {
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
    try {
      const body = await response.json() as { error?: { code?: unknown } };
      if (typeof body.error?.code === 'string') code = body.error.code;
    } catch {
      // Response bodies are untrusted; only allowlisted local messages reach the UI.
    }
    throw new AuthRequestError(response.status, code, safeMessage(code));
  }
}

function jsonMutation(body: unknown): RequestInit {
  return { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

function csrfMutation(method: 'POST' | 'DELETE', csrfToken: string): RequestInit {
  return { method, headers: { 'X-CSRF-Token': csrfToken } };
}

function csrfJSONMutation(method: 'POST' | 'PATCH', csrfToken: string, body: unknown): RequestInit {
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
  };
  return messages[code] ?? 'Authentication request failed.';
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
