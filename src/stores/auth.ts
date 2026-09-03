import { reactive } from 'vue';
import type { AcceptInvitationInput, Account, AccountStatus, AuthenticationResult, AuthState, Invitation, InvitationPage, Member, MemberCodeInput, MemberPage, OwnerLoginInput, OwnerSetupInput, IntegrationSettingsView, IntegrationUpdateInput, Session, IntegrationResults } from '@/types/auth';
import type { AuthEventCallbacks } from '@/services/events';
import { AuthClient, AuthRequestError } from '@/services/auth';
import { AuthorizedEventStream } from '@/services/events';

export interface AuthAPI {
  setupStatus(): Promise<{ setupRequired: boolean }>;
  completeOwnerSetup(input: OwnerSetupInput): Promise<AuthenticationResult>;
  startSignIn(email: string): Promise<{ message: string }>;
  loginOwner(input: OwnerLoginInput): Promise<AuthenticationResult>;
  loginMemberCode(input: MemberCodeInput): Promise<AuthenticationResult>;
  account(): Promise<Account>;
  sessions(): Promise<Session[]>;
  revokeSession(sessionID: string, csrfToken: string): Promise<void>;
  revokeAllSessions(csrfToken: string): Promise<void>;
  members(cursor?: string): Promise<MemberPage>;
  unlockMember(memberID: string, csrfToken: string): Promise<void>;
  setMemberStatus(memberID: string, status: AccountStatus, csrfToken: string): Promise<Member>;
  integrationSettings(): Promise<IntegrationSettingsView>;
  verifyIntegrations(update: IntegrationUpdateInput, csrfToken: string): Promise<IntegrationResults>;
  updateIntegrations(update: IntegrationUpdateInput, csrfToken: string): Promise<IntegrationResults>;
  invitations(cursor?: string): Promise<InvitationPage>;
  createInvitation(email: string, csrfToken: string): Promise<Invitation>;
  resendInvitation(invitationID: string, csrfToken: string): Promise<Invitation>;
  revokeInvitation(invitationID: string, csrfToken: string): Promise<void>;
  acceptInvitation(input: AcceptInvitationInput): Promise<AuthenticationResult>;
  logout(csrfToken: string): Promise<void>;
}

export interface AuthEventStream {
  configure(callbacks: AuthEventCallbacks): void;
  setAudience(audience: { userId: string; role: 'owner' | 'member' }): void;
  start(): void;
  stop(): void;
  setOnline(online: boolean): void;
}

// The CSRF token is published in a script-readable cookie so a reloaded page can keep making
// authorized mutations. It is not a bearer credential: the HttpOnly session cookie authenticates
// the caller, while this token only proves the request came from this application's own code.
const CSRF_COOKIE = '__Host-market_lens_csrf';

/**
 * Whether the server actually said the caller is not signed in.
 *
 * Only 401 and 403 are that statement. A network failure, a gateway error while the deployment
 * rolls a new pod, or an unreadable body are all "no answer" — and treating them as a sign-out
 * meant a release logged everyone out, left their real session valid and unrevoked, and added
 * one dead entry to their device list each time.
 */
function saidNotSignedIn(cause: unknown): boolean {
  return cause instanceof AuthRequestError && (cause.status === 401 || cause.status === 403);
}

function readCSRFCookie(): string | null {
  if (typeof document === 'undefined') return null;
  for (const entry of document.cookie.split(';')) {
    const [name, ...rest] = entry.trim().split('=');
    if (name === CSRF_COOKIE) {
      const value = rest.join('=');
      return value.length > 0 ? decodeURIComponent(value) : null;
    }
  }
  return null;
}

export function createAuthStore(api: AuthAPI, streamFactory: () => AuthEventStream) {
  const state = reactive<AuthState>({
    status: 'unknown', account: null, csrfToken: null, connection: 'offline', error: null,
    signInStep: 'email', signInEmail: '', signInMessage: null,
  });
  const stream = streamFactory();
  const seenEvents = new Set<string>();
  // Owner-scoped member events invalidate any open member roster without polling.
  const memberListeners = new Set<() => void>();
  stream.configure({
    onInvalidate: (event) => {
      if (seenEvents.has(event.id)) return;
      seenEvents.add(event.id);
      if (seenEvents.size > 200) seenEvents.delete(seenEvents.values().next().value ?? '');
      if (event.entityType === 'account' || event.entityType === 'session' || event.entityType === 'sessions' || event.entityType === 'credential') {
        void refreshAccount();
      }
      if (event.entityType === 'member' || event.entityType === 'invitation') {
        memberListeners.forEach((listener) => { listener(); });
      }
    },
    onState: (connection) => { state.connection = connection; },
    onUnauthorized: clearAuthentication,
  });

  function clearAuthentication(): void {
    stream.stop();
    state.status = 'anonymous';
    state.account = null;
    state.csrfToken = null;
    state.connection = 'offline';
    state.error = null;
  }

  function acceptAuthentication(result: AuthenticationResult): void {
    state.status = 'authenticated';
    state.account = result.account;
    state.csrfToken = result.csrfToken;
    state.error = null;
    stream.setAudience({ userId: result.account.id, role: result.account.role });
    stream.start();
  }

  // Mutations need the double-submit token; a restored page recovers it from its cookie.
  function requireCSRF(action: string): string {
    const token = state.csrfToken ?? readCSRFCookie();
    if (!token) throw new Error(`Sign in again to ${action}.`);
    state.csrfToken = token;
    return token;
  }

  async function refreshAccount(): Promise<void> {
    try {
      state.account = await api.account();
      state.status = 'authenticated';
    } catch (cause) {
      if (saidNotSignedIn(cause)) {
        clearAuthentication();
        return;
      }
      // The server did not answer. The person is still signed in as far as anyone knows, and
      // they are mid-task; the stream's own reconnection reports the outage.
      state.connection = 'reconnecting';
    }
  }

  return {
    state,
    async setupStatus(): Promise<{ setupRequired: boolean }> { return api.setupStatus(); },
    async restore(): Promise<void> {
      // 'unreachable' is retried: the server was rolling and may be back.
      if (state.status !== 'unknown' && state.status !== 'unreachable') return;
      try {
        state.account = await api.account();
        state.status = 'authenticated';
        state.csrfToken = readCSRFCookie();
        state.error = null;
        stream.setAudience({ userId: state.account.id, role: state.account.role });
        stream.start();
      } catch (cause) {
        if (!saidNotSignedIn(cause)) {
          state.status = 'unreachable';
          state.connection = 'reconnecting';
          return;
        }
        clearAuthentication();
      }
    },
    async loginOwner(input: OwnerLoginInput): Promise<void> { acceptAuthentication(await api.loginOwner(input)); },
    async loginMemberCode(input: MemberCodeInput): Promise<void> { acceptAuthentication(await api.loginMemberCode(input)); },
    async startSignIn(email: string): Promise<void> {
      const result = await api.startSignIn(email);
      state.signInEmail = email;
      state.signInMessage = result.message;
      state.signInStep = 'otp';
    },
    selectOwnerPassword(): void { state.signInStep = 'owner-password'; },
    resetSignIn(): void {
      state.signInStep = 'email';
      state.signInEmail = '';
      state.signInMessage = null;
    },
    async completeOwnerSetup(input: OwnerSetupInput): Promise<void> { acceptAuthentication(await api.completeOwnerSetup(input)); },
    async sessions(): Promise<Session[]> { return api.sessions(); },
    async revokeSession(sessionID: string): Promise<void> {
      if (!state.csrfToken) throw new Error('Sign in again to manage sessions.');
      await api.revokeSession(sessionID, state.csrfToken);
      await refreshAccount();
    },
    async revokeAllSessions(): Promise<void> {
      if (!state.csrfToken) throw new Error('Sign in again to manage sessions.');
      await api.revokeAllSessions(state.csrfToken);
      clearAuthentication();
    },
    async members(cursor = ''): Promise<MemberPage> { return api.members(cursor); },
    async unlockMember(memberID: string): Promise<void> {
      await api.unlockMember(memberID, requireCSRF('manage members'));
    },
    async setMemberStatus(memberID: string, status: AccountStatus): Promise<Member> {
      return api.setMemberStatus(memberID, status, requireCSRF('manage members'));
    },
    async integrationSettings(): Promise<IntegrationSettingsView> { return api.integrationSettings(); },
    async verifyIntegrations(update: IntegrationUpdateInput): Promise<IntegrationResults> {
      return api.verifyIntegrations(update, requireCSRF('change integrations'));
    },
    async updateIntegrations(update: IntegrationUpdateInput): Promise<IntegrationResults> {
      return api.updateIntegrations(update, requireCSRF('change integrations'));
    },
    async invitations(cursor = ''): Promise<InvitationPage> { return api.invitations(cursor); },
    async createInvitation(email: string): Promise<Invitation> {
      return api.createInvitation(email, requireCSRF('send invitations'));
    },
    async resendInvitation(invitationID: string): Promise<Invitation> {
      return api.resendInvitation(invitationID, requireCSRF('send invitations'));
    },
    async revokeInvitation(invitationID: string): Promise<void> {
      await api.revokeInvitation(invitationID, requireCSRF('manage invitations'));
    },
    async acceptInvitation(input: AcceptInvitationInput): Promise<void> {
      acceptAuthentication(await api.acceptInvitation(input));
    },
    async logout(): Promise<void> {
      if (!state.csrfToken) throw new Error('Sign in again to sign out.');
      await api.logout(state.csrfToken);
      clearAuthentication();
    },
    onMembersChanged(listener: () => void): () => void {
      memberListeners.add(listener);
      return () => { memberListeners.delete(listener); };
    },
    setOnline(online: boolean): void { stream.setOnline(online); },
  };
}

export type AuthStore = ReturnType<typeof createAuthStore>;
export const authStore = createAuthStore(new AuthClient(), () => new AuthorizedEventStream({}));
