// Web Push helpers (S96).
//
// `registerServiceWorker` mounts /sw.js so the browser delivers `push` events
// even when the SPA tab is closed. `subscribe` / `unsubscribe` round-trip the
// PushSubscription through the backend so the WebPushSender can later target
// this exact device by its endpoint URL + p256dh/auth keys.

import { api } from '../api/client';

// urlBase64ToUint8Array converts a VAPID public key (URL-safe base64) into the
// raw Uint8Array `pushManager.subscribe` requires for `applicationServerKey`.
// Browsers do not natively accept the base64 form, so every web push tutorial
// ships this helper — kept here so other code can stay free of the detail.
function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const normalized = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(normalized);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) out[i] = raw.charCodeAt(i);
  return out;
}

// pushSupported is true when the browser has the APIs we need. Safari < 16
// and most in-app webviews are false here, so the Settings UI can hide the
// toggle gracefully instead of throwing on click.
export function pushSupported() {
  return (
    typeof window !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  );
}

// registerServiceWorker mounts /sw.js once per page load and returns the
// `ServiceWorkerRegistration`. Idempotent — repeated calls reuse the existing
// registration via the browser cache.
export async function registerServiceWorker() {
  if (!pushSupported()) return null;
  try {
    const reg = await navigator.serviceWorker.register('/sw.js');
    return reg;
  } catch (err) {
    // Surface as console warning so devs see it, but don't blow up the SPA —
    // the rest of the app continues working without push.
    console.warn('sw register failed', err);
    return null;
  }
}

// currentSubscription returns the existing PushSubscription if the browser
// already has one for this origin, or null otherwise. Used by the Settings
// panel to decide whether to show "Enable" vs "Disable".
export async function currentSubscription() {
  if (!pushSupported()) return null;
  const reg = await navigator.serviceWorker.getRegistration('/sw.js');
  if (!reg) return null;
  return reg.pushManager.getSubscription();
}

// subscribe walks the full opt-in flow:
//   1. Ask the user for Notification permission (no-op when already granted).
//   2. Fetch the VAPID public key from the backend.
//   3. Call pushManager.subscribe with the key.
//   4. POST the resulting endpoint+keys to /me/push-tokens as platform=web.
// Throws when permission is denied, the backend has no key configured, or
// the subscribe call fails — the caller turns the error into a UI message.
export async function subscribe() {
  if (!pushSupported()) {
    throw new Error('Push notifications are not supported in this browser.');
  }
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') {
    throw new Error('Notification permission was not granted.');
  }
  const reg = (await navigator.serviceWorker.getRegistration('/sw.js')) || (await registerServiceWorker());
  if (!reg) throw new Error('Service worker registration failed.');

  const vapidKey = await api.getVapidPublicKey();
  if (!vapidKey) throw new Error('Web push is not configured on the server.');

  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(vapidKey.trim()),
  });

  const subJSON = sub.toJSON();
  await api.registerPushToken({
    platform: 'web',
    token: subJSON.endpoint,
    keys: subJSON.keys,
  });
  return sub;
}

// unsubscribe revokes the browser-side subscription and tells the backend to
// drop the matching push_tokens row. Safe to call when there is no
// subscription — returns true either way.
export async function unsubscribe() {
  if (!pushSupported()) return true;
  const reg = await navigator.serviceWorker.getRegistration('/sw.js');
  if (!reg) return true;
  const sub = await reg.pushManager.getSubscription();
  if (!sub) return true;
  const endpoint = sub.endpoint;
  try {
    await sub.unsubscribe();
  } catch {
    // Some Push Services drop the subscription before we get the ack;
    // continue so the backend cleanup still runs.
  }
  try {
    await api.unregisterPushToken({ platform: 'web', token: endpoint });
  } catch {
    // Backend may already have pruned the row (e.g. provider 410); ignore.
  }
  return true;
}
