import type { ConnectionState } from './marketData';

export type AccountRole = 'owner' | 'member';
export type AccountStatus = 'active' | 'deactivated';
export type AuthStatus = 'unknown' | 'anonymous' | 'authenticated';

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
