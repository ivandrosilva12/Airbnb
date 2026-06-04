// pushBootstrap registers the device with the AirHost backend so it can
// receive native pushes for booking updates and new messages. It is
// idempotent: re-registering the same token is a backend upsert, so we can
// call it on every authenticated app start.
//
// The expo-notifications module is required dynamically because Expo Go from
// SDK 53 onward no longer ships native push, and a developer running the app
// inside a simulator or without the package installed should not crash. When
// the module is unavailable we log a hint and noop.

import { useEffect } from 'react';
import { Platform } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

let warned = false;

// cachedRegistration stores the most recently registered (platform, token)
// pair so logout can explicitly tear it down BEFORE the auth state is
// cleared — at which point the api client would no longer have a usable
// bearer token. The hook keeps this in sync with whatever it told the
// backend; unregisterPushTokenForCurrentDevice consumes and clears it.
let cachedRegistration = null;

function loadNotifications() {
  try {
    // Dynamic require so a missing dep gracefully degrades to a noop in dev.
    // eslint-disable-next-line global-require
    return require('expo-notifications');
  } catch (e) {
    if (!warned) {
      // eslint-disable-next-line no-console
      console.warn('expo-notifications not installed; push registration disabled', e?.message);
      warned = true;
    }
    return null;
  }
}

function loadDevice() {
  try {
    // eslint-disable-next-line global-require
    return require('expo-device');
  } catch {
    return null;
  }
}

// registerForPushTokenAsync asks the OS for permission (no-op if already
// granted) and returns the platform-specific push token, or null when push is
// unavailable (simulator, denied permission, missing module, etc.).
export async function registerForPushTokenAsync() {
  const Notifications = loadNotifications();
  if (!Notifications) return null;
  const Device = loadDevice();
  if (Device && Device.isDevice === false) {
    // Simulators do not get push tokens.
    return null;
  }
  try {
    const settings = await Notifications.getPermissionsAsync();
    let status = settings.status;
    if (status !== 'granted') {
      const req = await Notifications.requestPermissionsAsync();
      status = req.status;
    }
    if (status !== 'granted') return null;
    if (Platform.OS === 'android') {
      try {
        await Notifications.setNotificationChannelAsync('default', {
          name: 'default',
          importance: 4, // MAX
        });
      } catch {
        // Older expo-notifications builds expose importance differently; safe to ignore.
      }
    }
    // Use Expo's push service token by default; for FCM/APNs straight to the
    // provider, replace with getDevicePushTokenAsync() once the project's
    // Firebase/APNs credentials are wired in. The backend treats either as an
    // opaque per-platform token.
    const result = await Notifications.getExpoPushTokenAsync();
    return result?.data || null;
  } catch (e) {
    // eslint-disable-next-line no-console
    console.warn('push: token registration failed', e?.message);
    return null;
  }
}

// useRegisterPushToken hooks into the auth state so that on every login the
// device is (re-)registered with the backend, and on logout the previously
// registered token is removed.
export function useRegisterPushToken() {
  const { authenticated } = useAuth();
  const api = useApi();

  useEffect(() => {
    if (!authenticated) return undefined;
    let cancelled = false;
    let registered = null;
    (async () => {
      const token = await registerForPushTokenAsync();
      if (cancelled || !token) return;
      const platform = Platform.OS === 'ios' ? 'ios' : Platform.OS === 'android' ? 'android' : 'web';
      try {
        await api.registerPushToken(platform, token);
        registered = { platform, token };
        cachedRegistration = { platform, token };
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn('push: registerPushToken failed', e?.message);
      }
    })();
    return () => {
      cancelled = true;
      if (registered) {
        // Best-effort cleanup if the auth state flips without going through
        // the explicit logout path (e.g. refresh-token rejected). The same
        // shape as unregisterPushTokenForCurrentDevice — kept inline because
        // the api client closes over the now-stale token getter.
        api.unregisterPushToken(registered.platform, registered.token).catch(() => {});
        cachedRegistration = null;
      }
    };
  }, [authenticated, api]);
}

// unregisterPushTokenForCurrentDevice is called by the logout flow BEFORE
// the auth state is cleared, so the api client passed in still carries a
// valid bearer token. It is best-effort: a failure to unregister must not
// block the user from signing out. The cached registration is cleared
// regardless so a later sign-in starts from a clean slate.
export async function unregisterPushTokenForCurrentDevice(api) {
  const reg = cachedRegistration;
  cachedRegistration = null;
  if (!reg || !api || typeof api.unregisterPushToken !== 'function') return;
  try {
    await api.unregisterPushToken(reg.platform, reg.token);
  } catch (e) {
    // eslint-disable-next-line no-console
    console.warn('push: unregisterPushToken on logout failed', e?.message);
  }
}
