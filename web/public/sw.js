// AirHost web push service worker (S96).
//
// Browsers only deliver `push` events when the page is closed AND a service
// worker has been registered for the origin. This file is intentionally tiny
// — it does not cache assets, intercept fetches, or take over the document.
// Its only job is to surface server-sent notifications and route clicks to
// the right deep-link inside the SPA.
//
// The payload shape mirrors what the backend's WebPushSender emits in
// `infrastructure/push/webpush.go`:
//   { "title": "...", "body": "...", "data": { "url": "/x", ... } }

self.addEventListener('install', () => {
  // Activate the new worker immediately so a redeploy doesn't strand users
  // on a stale version.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  // Take control of every open client without requiring a hard reload.
  event.waitUntil(self.clients.claim());
});

self.addEventListener('push', (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    // Push providers occasionally deliver an empty / non-JSON body (e.g.
    // a synthetic "wake-up" push). Fall back to a generic notification so
    // the user still gets *something* rather than a silent failure.
    payload = { title: 'AirHost', body: 'You have a new notification.' };
  }
  const title = payload.title || 'AirHost';
  const url = payload.data && payload.data.url ? payload.data.url : '/';
  const options = {
    body: payload.body || '',
    icon: '/favicon.ico',
    badge: '/favicon.ico',
    data: { url },
    // Tag stale notifications of the same kind so a new one replaces the
    // previous (e.g. multiple "new message" pings don't stack up).
    tag: payload.data && payload.data.type ? payload.data.type : undefined,
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    (async () => {
      const clientList = await self.clients.matchAll({
        type: 'window',
        includeUncontrolled: true,
      });
      for (const client of clientList) {
        // Reuse an existing AirHost tab when possible — opening another one
        // is jarring and burns memory.
        try {
          const u = new URL(client.url);
          if (u.origin === self.location.origin) {
            await client.focus();
            if ('navigate' in client) {
              return client.navigate(url);
            }
            return undefined;
          }
        } catch {
          // ignore unparseable client URLs
        }
      }
      return self.clients.openWindow(url);
    })(),
  );
});
