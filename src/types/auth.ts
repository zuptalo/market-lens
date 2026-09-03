import type { ConnectionState } from './marketData';

export type AccountRole = 'owner' | 'member';
export type AccountStatus = 'active' | 'deactivated';
/**
 * `anonymous` means the server said the caller is not signed in. `unreachable` means it did not
 * say anything — the request failed before an answer came back. Collapsing the two is what sent
 * a signed-in person to the login page every time the deployment rolled a new pod.
 */
export type AuthStatus = 'unknown' | 'anonymous' | 'authenticated' | 'unreachable';

export interface Account {
  id: string;
  email: string;
  displayName: string;
  role: AccountRole;
  status: AccountStatus;
  emailVerifiedAt: string;
}

export interface AuthenticationResult {
  account: Account;
  csrfToken: string;
}

export interface OwnerSetupInput {
  capability: string;
  email: string;
  password: string;
  displayName: string;
  eodhdApiKey: string;
  smtp: SMTPConfiguration;
}

export interface SMTPConfiguration {
  host: string;
  port: number;
  from: string;
  username: string;
  password: string;
}

export interface OwnerLoginInput {
  email: string;
  password: string;
}

export interface MemberCodeInput {
  email: string;
  code: string;
}

export type SignInStep = 'email' | 'otp' | 'owner-password';

export interface Session {
  id: string;
  current: boolean;
  deviceLabel: string;
  createdAt: string;
  lastSeenAt: string;
  idleExpiresAt: string;
  absoluteExpiresAt: string;
  revoked: boolean;
}

export interface AuthState {
  status: AuthStatus;
  account: Account | null;
  csrfToken: string | null;
  connection: ConnectionState;
  error: string | null;
  signInStep: SignInStep;
  signInEmail: string;
  signInMessage: string | null;
}

export type MemberLoginState = 'available' | 'temporarily_blocked' | 'administratively_locked';

export interface Member {
  id: string;
  email: string;
  displayName: string;
  status: AccountStatus;
  loginState: MemberLoginState;
  blockedUntil: string | null;
  lockedAt: string | null;
  activeSessionCount: number;
  createdAt: string;
}

export interface MemberPage {
  members: Member[];
  nextCursor: string;
}

export type InvitationState = 'pending' | 'accepted' | 'revoked' | 'expired';
export type DeliveryState = 'pending' | 'sending' | 'sent' | 'failed' | 'abandoned';

export interface Invitation {
  id: string;
  email: string;
  state: InvitationState;
  expiresAt: string;
  acceptedAt: string | null;
  deliveryState: DeliveryState;
  deliveryError: string | null;
  resendCount: number;
  createdAt: string;
}

export interface InvitationPage {
  items: Invitation[];
  nextCursor: string;
}

export interface AcceptInvitationInput {
  capability: string;
  email: string;
  displayName: string;
}

/** The non-secret view of one installation's integrations. Secrets are write-only: the API
 * reports that they are set, never their values, so nothing here can be prefilled. */
export interface IntegrationSettingsView {
  eodhd: { configured: boolean; validatedAt: string | null };
  smtp: {
    configured: boolean;
    host: string;
    port: number;
    from: string;
    username: string;
    passwordConfigured: boolean;
  };
}

/** A submitted change. Each integration is optional so they can be changed independently, and
 * an omitted SMTP password keeps the stored one. */
export interface IntegrationUpdateInput {
  smtp?: { host: string; port: number; from: string; username: string; password?: string };
  eodhd?: { apiKey: string };
}

/** Each integration's own outcome from the last check or save. `not_checked` is a real
 * answer: a value of the wrong shape stops every network call. */
export type IntegrationResults = Partial<Record<'eodhd' | 'smtp', string>>;
