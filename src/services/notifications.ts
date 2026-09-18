import type {
  NotificationChannel,
  NotificationKind,
  NotificationRecord,
  NotificationSettings,
  PushSubscriptionSummary,
} from '@/types/notifications';

/**
 * What a person asked to be told about, and where.
 *
 * Nothing here turns anything on by itself. The one function that looks like it might —
 * subscribeThisDevice — asks the browser for permission first and does nothing at all if the answer
 * is no, because a product that finds a way around a refused permission has learned the wrong
 * lesson from being refused.
 */

type Fetcher = typeof fetch;

function writeHeaders(csrfToken: string): HeadersInit {
  return { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken };
}

interface SettingsWire {
  preferences: { kind: NotificationKind; channel: NotificationChannel; enabled: boolean }[];
  quiet_hours: { starts_at: string; ends_at: string; timezone: string } | null;
  nothing_is_on_by_default: boolean;
}

function toSettings(wire: SettingsWire): NotificationSettings {
  return {
    preferences: (wire.preferences ?? []).map((preference) => ({ ...preference })),
    quietHours: wire.quiet_hours
      ? {
        startsAt: wire.quiet_hours.starts_at,
        endsAt: wire.quiet_hours.ends_at,
        timezone: wire.quiet_hours.timezone,
      }
      : null,
    nothingIsOnByDefault: wire.nothing_is_on_by_default !== false,
  };
}

export async function fetchNotificationSettings(
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<NotificationSettings> {
  const response = await fetcher('/api/v1/notifications/preferences', { signal });
  if (!response.ok) throw new Error('Unable to load your notification settings.');
  return toSettings(await response.json() as SettingsWire);
}

export async function setNotificationPreference(
  kind: NotificationKind,
  channel: NotificationChannel,
  enabled: boolean,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<NotificationSettings> {
  const response = await fetcher('/api/v1/notifications/preferences', {
    method: 'PUT', headers: writeHeaders(csrfToken),
    body: JSON.stringify({ kind, channel, enabled }),
  });
  if (!response.ok) throw new Error('That could not be changed.');
  return toSettings(await response.json() as SettingsWire);
}

/** Passing null clears the window. Half a window is not a thing. */
export async function setQuietHours(
  hours: { startsAt: string; endsAt: string; timezone: string } | null,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<NotificationSettings> {
  const response = await fetcher('/api/v1/notifications/quiet-hours', {
    method: 'PUT',
    headers: writeHeaders(csrfToken),
    body: JSON.stringify(hours
      ? { starts_at: hours.startsAt, ends_at: hours.endsAt, timezone: hours.timezone }
      : { starts_at: null, ends_at: null, timezone: null }),
  });
  if (!response.ok) {
    const failure = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(failure?.error?.message ?? 'Those quiet hours could not be saved.');
  }
  return toSettings(await response.json() as SettingsWire);
}

export async function fetchSubscriptions(
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<PushSubscriptionSummary[]> {
  const response = await fetcher('/api/v1/notifications/subscriptions', { signal });
  if (!response.ok) throw new Error('Unable to load your devices.');
  const body = await response.json() as {
    subscriptions?: { id: string; label: string; endpoint_digest?: string; created_at: string; last_used_at: string | null }[];
  };
  return (body.subscriptions ?? []).map((subscription) => ({
    id: subscription.id,
    label: subscription.label,
    endpointDigest: subscription.endpoint_digest ?? '',
    createdAt: subscription.created_at,
    lastUsedAt: subscription.last_used_at ?? null,
  }));
}

export async function revokeSubscription(
  id: string,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<PushSubscriptionSummary[]> {
  const response = await fetcher(`/api/v1/notifications/subscriptions/${encodeURIComponent(id)}`, {
    method: 'DELETE', headers: writeHeaders(csrfToken),
  });
  if (!response.ok) throw new Error('That device could not be removed.');
  const body = await response.json() as {
    subscriptions?: { id: string; label: string; endpoint_digest?: string; created_at: string; last_used_at: string | null }[];
  };
  return (body.subscriptions ?? []).map((subscription) => ({
    id: subscription.id,
    label: subscription.label,
    endpointDigest: subscription.endpoint_digest ?? '',
    createdAt: subscription.created_at,
    lastUsedAt: subscription.last_used_at ?? null,
  }));
}

export async function fetchNotificationHistory(
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<NotificationRecord[]> {
  const response = await fetcher('/api/v1/notifications/history', { signal });
  if (!response.ok) throw new Error('Unable to load what was sent.');
  const body = await response.json() as {
    notifications?: {
      kind: NotificationKind; channel: NotificationChannel; state: string; count: number;
      attempts: number; last_error: string | null; created_at: string; sent_at: string | null;
    }[];
  };
  return (body.notifications ?? []).map((record) => ({
    kind: record.kind,
    channel: record.channel,
    state: record.state as NotificationRecord['state'],
    count: record.count,
    attempts: record.attempts,
    lastError: record.last_error ?? null,
    createdAt: record.created_at,
    sentAt: record.sent_at ?? null,
  }));
}

/** Whether this browser can receive push at all. Safari on iOS only can once installed. */
export function pushIsAvailable(): boolean {
  return typeof window !== 'undefined'
    && typeof navigator !== 'undefined'
    && Boolean(navigator.serviceWorker)
    && typeof (window as { PushManager?: unknown }).PushManager !== 'undefined'
    && typeof (window as { Notification?: unknown }).Notification !== 'undefined';
}

/**
 * What this device can and does receive — a different question from what the account prefers.
 *
 * Conflating the two is what left a phone showing "push on" while it had never been asked for
 * permission and would never receive anything. A preference belongs to a person; a subscription
 * belongs to a device, and only the device can say whether it has one.
 */
export interface ThisDevice {
  /** False on iOS Safari until the app is installed, and on anything without a service worker. */
  available: boolean;
  /** What the browser will answer without prompting. `denied` cannot be re-asked from a page. */
  permission: NotificationPermission | 'unsupported';
  /** Whether this browser currently holds a push subscription. */
  subscribed: boolean;
  /** Matches one row in the device list, so a person can find the device in their hand. */
  digest: string | null;
}

/** The digest the server publishes for each device, computed here over this device's endpoint. */
async function endpointDigest(endpoint: string): Promise<string | null> {
  const subtle = globalThis.crypto?.subtle;
  if (!subtle) return null;
  const bytes = new TextEncoder().encode(`market-lens/push-endpoint\u0000${endpoint}`);
  const hashed = await subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(hashed)]
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('')
    .slice(0, 16);
}

export async function inspectThisDevice(): Promise<ThisDevice> {
  if (!pushIsAvailable()) {
    return { available: false, permission: 'unsupported', subscribed: false, digest: null };
  }
  const permission = Notification.permission;
  try {
    const registration = await navigator.serviceWorker.getRegistration('/');
    const subscription = await registration?.pushManager.getSubscription();
    if (!subscription) {
      return { available: true, permission, subscribed: false, digest: null };
    }
    return {
      available: true,
      permission,
      subscribed: true,
      digest: await endpointDigest(subscription.endpoint),
    };
  } catch {
    // A browser that will not answer is treated as not subscribed, which is the safe way to be
    // wrong: it offers to subscribe rather than claiming a device is covered when it is not.
    return { available: true, permission, subscribed: false, digest: null };
  }
}

function urlBase64ToUint8Array(value: string): Uint8Array<ArrayBuffer> {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
  // An explicitly-sized buffer, because pushManager.subscribe will not take a view over a
  // SharedArrayBuffer and Uint8Array.from does not promise which it produces.
  const bytes = new Uint8Array(new ArrayBuffer(raw.length));
  for (let index = 0; index < raw.length; index += 1) bytes[index] = raw.charCodeAt(index);
  return bytes;
}

function toBase64Url(buffer: ArrayBuffer | null): string {
  if (!buffer) return '';
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/**
 * Subscribes this device, after asking the browser.
 *
 * Permission is asked for first and a refusal ends it. There is no fallback path, no second prompt,
 * and nothing is recorded — a product that works around a refused permission has learned the wrong
 * lesson from being refused.
 */
export async function subscribeThisDevice(
  label: string,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<PushSubscriptionSummary[]> {
  if (!pushIsAvailable()) {
    throw new Error('This browser cannot receive push notifications.');
  }
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') {
    throw new Error('This browser was not given permission to show notifications.');
  }

  const keyResponse = await fetcher('/api/v1/notifications/push-key');
  if (!keyResponse.ok) throw new Error('Unable to reach this installation to subscribe.');
  const { public_key: publicKey } = await keyResponse.json() as { public_key: string };

  const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
  await navigator.serviceWorker.ready;
  const subscription = await registration.pushManager.subscribe({
    // Required by every browser: a push that is not shown to the person may not be sent at all.
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(publicKey),
  });

  const response = await fetcher('/api/v1/notifications/subscriptions', {
    method: 'POST',
    headers: writeHeaders(csrfToken),
    body: JSON.stringify({
      endpoint: subscription.endpoint,
      p256dh: toBase64Url(subscription.getKey('p256dh')),
      auth: toBase64Url(subscription.getKey('auth')),
      label,
    }),
  });
  if (!response.ok) {
    const failure = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(failure?.error?.message ?? 'This device could not be subscribed.');
  }
  const body = await response.json() as {
    subscriptions?: { id: string; label: string; endpoint_digest?: string; created_at: string; last_used_at: string | null }[];
  };
  return (body.subscriptions ?? []).map((item) => ({
    id: item.id, label: item.label, endpointDigest: item.endpoint_digest ?? '',
    createdAt: item.created_at, lastUsedAt: item.last_used_at ?? null,
  }));
}

/** Turns off one kind on one channel from a link in an email, with no session. */
export async function unsubscribeWithToken(
  token: string,
  fetcher: Fetcher = fetch,
): Promise<{ kind: NotificationKind; channel: NotificationChannel }> {
  const response = await fetcher('/api/v1/notifications/unsubscribe', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  });
  if (!response.ok) {
    const failure = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(failure?.error?.message
      ?? 'This link is not readable, or has expired. You can turn notifications off under Account settings.');
  }
  const body = await response.json() as { kind: NotificationKind; channel: NotificationChannel };
  return { kind: body.kind, channel: body.channel };
}

/**
 * Proves a message actually arrives, to the caller's own address.
 *
 * The settings check beside it proves the server accepts a connection; this proves a message
 * arrives, which is a different failure. A server can connect, refuse the sender, and look
 * perfectly configured until the first alert quietly does not turn up.
 */
export async function sendTestEmail(csrfToken: string, fetcher: Fetcher = fetch): Promise<void> {
  const response = await fetcher('/api/v1/notifications/test-email', {
    method: 'POST', headers: writeHeaders(csrfToken),
  });
  if (!response.ok) {
    const failure = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(failure?.error?.message ?? 'The mail server did not accept the message.');
  }
}
