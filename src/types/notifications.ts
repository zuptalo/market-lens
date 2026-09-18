/**
 * Consented email and Web Push alerts (feature 027).
 *
 * Nothing here is on by default. Every kind is off for every person until they ask for it, per
 * channel — a product that mails somebody unasked has decided on their behalf.
 */

export type NotificationKind =
  | 'decision_waiting'
  | 'paper_fill'
  | 'pipeline_failure'
  | 'signal_change';

export type NotificationChannel = 'email' | 'web_push';

export interface NotificationPreference {
  kind: NotificationKind;
  channel: NotificationChannel;
  enabled: boolean;
}

/**
 * The window during which nothing arrives. Local wall-clock times plus a zone, because a person
 * means "while I am asleep" and daylight saving moves that against UTC twice a year. A window whose
 * end is before its start crosses midnight.
 */
export interface QuietHours {
  startsAt: string;
  endsAt: string;
  timezone: string;
}

export interface NotificationSettings {
  /** Only the kinds this person is offered. One is the owner's alone. */
  preferences: NotificationPreference[];
  quietHours: QuietHours | null;
  /** Always true. Nothing is sent to anybody who did not ask for it. */
  nothingIsOnByDefault: boolean;
}

/** One device. No endpoint and no keys: a page has no use for either. */
export interface PushSubscriptionSummary {
  id: string;
  label: string;
  /** Lets the browser that owns this device recognise its own row. Every other one is opaque. */
  endpointDigest: string;
  createdAt: string;
  lastUsedAt: string | null;
}

/** What was sent, when, and what failed — without the payload. */
export interface NotificationRecord {
  kind: NotificationKind;
  channel: NotificationChannel;
  state: 'pending' | 'sending' | 'sent' | 'failed' | 'abandoned';
  count: number;
  attempts: number;
  lastError: string | null;
  createdAt: string;
  sentAt: string | null;
}
