/*
 * The service worker, whose only job is to receive a push and show it.
 *
 * It deliberately knows nothing. The payload carries a kind, a count and a path within the app —
 * no figures, no instrument names, nothing about what anybody owns — because the message rests on a
 * push service's server until this collects it, and the threat there is accumulation rather than
 * interception.
 *
 * It does not fetch, does not cache, and does not intercept requests. A service worker that caches
 * responses would be one more place a person's private data lives, on a device this product cannot
 * clear.
 */

self.addEventListener('install', () => {
  // Take over immediately rather than waiting for every tab to close. A notification that only
  // starts working after a full browser restart is one nobody believes is working.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('push', (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    // An unreadable payload still deserves a notification: something happened, and the app can say
    // what once it is opened. Showing nothing would be the one outcome the person cannot act on.
    payload = {};
  }

  const title = payload.title || 'Market Lens';
  const body = payload.body || 'Open Market Lens to see what changed.';
  const path = typeof payload.path === 'string' && payload.path.startsWith('/') ? payload.path : '/';

  event.waitUntil(self.registration.showNotification(title, {
    body,
    icon: '/favicon.svg',
    badge: '/favicon.svg',
    // Collapse several of the same kind rather than stacking them: four separate taps saying the
    // same thing is worse than one.
    tag: payload.kind || 'market-lens',
    renotify: false,
    data: { path },
  }));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const path = (event.notification.data && event.notification.data.path) || '/';
  const target = new URL(path, self.location.origin).href;

  // Focus a tab that is already open rather than opening a fourth one.
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of windows) {
      if (client.url.startsWith(self.location.origin) && 'focus' in client) {
        await client.navigate(target).catch(() => {});
        return client.focus();
      }
    }
    return self.clients.openWindow(target);
  })());
});
