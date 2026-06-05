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

// usePushTapRouting wires the system push-tap into the in-app navigator
// (S125). When a notification carries `data.type` (set by the backend
// notifier — see backend/internal/application/notification/subscriber.go),
// we route the tap to the right deep-link screen. The hook is deliberately
// thin: every branch is a small `navigationRef.navigate(name, params)`
// call, the payload shape is the same one the backend serializes.
//
// Supported types (extend as more push types come online):
//   - "cohost.invited" -> CohostInvitation screen with the invitation id
//                         + listing/host/permission hints. The accept and
//                         decline live on that screen, not in the OS
//                         notification UI.
//
// The hook also handles the "cold-start" case via getLastNotificationResponseAsync:
// if the user tapped a push while the app was closed and that tap was what
// launched it, the listener won't fire — we need to read the launch payload
// once on mount instead.
//
// navigationRef MUST be a @react-navigation/native NavigationContainer ref.
// Passing null is safe — the effect waits until isReady() flips true.
export function usePushTapRouting(navigationRef) {
  useEffect(() => {
    const Notifications = loadNotifications();
    if (!Notifications || !navigationRef) return undefined;

    function route(response) {
      const data = response?.notification?.request?.content?.data;
      if (!data || typeof data !== 'object') return;
      // navigateWhenReady defers the call until the NavigationContainer is
      // mounted; otherwise a tap-launched cold start would try to navigate
      // before the navigator is up and silently no-op.
      const go = (name, params) => {
        if (navigationRef.isReady && navigationRef.isReady()) {
          navigationRef.navigate(name, params);
        } else {
          // Poll briefly; navigator readiness happens within the first frame.
          const t = setInterval(() => {
            if (navigationRef.isReady && navigationRef.isReady()) {
              clearInterval(t);
              navigationRef.navigate(name, params);
            }
          }, 50);
          // Cap the wait so a broken ref doesn't leak the interval forever.
          setTimeout(() => clearInterval(t), 5000);
        }
      };

      switch (data.type) {
        case 'cohost.invited':
          go('CohostInvitation', {
            invitationId: data.invitationId || data.id,
            propertyTitle: data.propertyTitle,
            hostName: data.hostName,
            permissions: Array.isArray(data.permissions) ? data.permissions : undefined,
          });
          break;
        default:
          // Unknown payload — leave the in-app surface alone. The system
          // tray notification is its own UX; not every push needs a
          // dedicated screen.
          break;
      }
    }

    // 1. Live taps while the app is foregrounded or backgrounded.
    let sub;
    try {
      sub = Notifications.addNotificationResponseReceivedListener(route);
    } catch (e) {
      // eslint-disable-next-line no-console
      console.warn('push: addNotificationResponseReceivedListener failed', e?.message);
    }

    // 2. Cold-start tap: the user opened the app from a notification, so
    // there's no live listener event — the launch payload is on the
    // last-response API instead. Run once on mount.
    (async () => {
      try {
        if (typeof Notifications.getLastNotificationResponseAsync === 'function') {
          const last = await Notifications.getLastNotificationResponseAsync();
          if (last) route(last);
        }
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn('push: getLastNotificationResponseAsync failed', e?.message);
      }
    })();

    return () => {
      if (sub && typeof sub.remove === 'function') sub.remove();
    };
  }, [navigationRef]);
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
