import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import * as AuthSession from 'expo-auth-session';
import * as WebBrowser from 'expo-web-browser';
import * as Storage from './storage';
import { KEYCLOAK_ISSUER, KEYCLOAK_CLIENT_ID } from '../config';
import { createApi } from '../api/client';
import { unregisterPushTokenForCurrentDevice } from '../notifications/pushBootstrap';

WebBrowser.maybeCompleteAuthSession();

const TOKEN_KEY = 'airhost.tokens';
const AuthContext = createContext(null);

const redirectUri = AuthSession.makeRedirectUri({ scheme: 'airhost' });

export function AuthProvider({ children }) {
  const discovery = AuthSession.useAutoDiscovery(KEYCLOAK_ISSUER);
  const [tokens, setTokens] = useState(null);
  const [ready, setReady] = useState(false);

  const [request, response, promptAsync] = AuthSession.useAuthRequest(
    {
      clientId: KEYCLOAK_CLIENT_ID,
      redirectUri,
      scopes: ['openid', 'profile', 'email'],
      usePKCE: true,
    },
    discovery,
  );

  // A second request carrying kc_action=CONFIGURE_TOTP triggers Keycloak's
  // application-initiated action to enrol two-factor (TOTP). It returns a code
  // exactly like a normal login, which we exchange to refresh the session.
  const [actionRequest, actionResponse, promptActionAsync] = AuthSession.useAuthRequest(
    {
      clientId: KEYCLOAK_CLIENT_ID,
      redirectUri,
      scopes: ['openid', 'profile', 'email'],
      usePKCE: true,
      extraParams: { kc_action: 'CONFIGURE_TOTP' },
    },
    discovery,
  );

  // Restore persisted tokens on startup.
  useEffect(() => {
    Storage.getItem(TOKEN_KEY)
      .then((raw) => raw && setTokens(JSON.parse(raw)))
      .finally(() => setReady(true));
  }, []);

  // exchange swaps an authorization code (+ its PKCE verifier) for tokens and
  // persists them. Shared by the login flow and the 2FA-enrolment flow.
  const exchange = useCallback(
    async (code, codeVerifier) => {
      if (!discovery) return;
      const result = await AuthSession.exchangeCodeAsync(
        {
          clientId: KEYCLOAK_CLIENT_ID,
          code,
          redirectUri,
          extraParams: { code_verifier: codeVerifier },
        },
        discovery,
      );
      const stored = {
        accessToken: result.accessToken,
        refreshToken: result.refreshToken,
        expiresAt: Date.now() + (result.expiresIn ?? 300) * 1000,
      };
      setTokens(stored);
      await Storage.setItem(TOKEN_KEY, JSON.stringify(stored));
    },
    [discovery],
  );

  // Exchange the auth code once the login browser flow completes.
  useEffect(() => {
    if (response?.type === 'success' && discovery && request) {
      exchange(response.params.code, request.codeVerifier);
    }
  }, [response, discovery, request, exchange]);

  // Same for the 2FA-enrolment flow.
  useEffect(() => {
    if (actionResponse?.type === 'success' && discovery && actionRequest) {
      exchange(actionResponse.params.code, actionRequest.codeVerifier);
    }
  }, [actionResponse, discovery, actionRequest, exchange]);

  // clearSession drops the stored tokens so the app falls back to the sign-in
  // screen instead of retrying with credentials that will keep returning 401.
  async function clearSession() {
    setTokens(null);
    await Storage.deleteItem(TOKEN_KEY);
  }

  // Returns a valid access token, refreshing if it is close to expiry. When the
  // refresh token is rejected (expired/revoked) the session is cleared and null
  // is returned, so callers stop sending a dead token.
  async function getAccessToken() {
    if (!tokens) return null;
    if (Date.now() < tokens.expiresAt - 30_000) return tokens.accessToken;
    if (!discovery) return tokens.accessToken; // discovery not ready yet; transient
    if (!tokens.refreshToken) {
      await clearSession();
      return null;
    }
    try {
      const refreshed = await AuthSession.refreshAsync(
        { clientId: KEYCLOAK_CLIENT_ID, refreshToken: tokens.refreshToken },
        discovery,
      );
      const stored = {
        accessToken: refreshed.accessToken,
        refreshToken: refreshed.refreshToken ?? tokens.refreshToken,
        expiresAt: Date.now() + (refreshed.expiresIn ?? 300) * 1000,
      };
      setTokens(stored);
      await Storage.setItem(TOKEN_KEY, JSON.stringify(stored));
      return stored.accessToken;
    } catch {
      await clearSession();
      return null;
    }
  }

  async function logout() {
    // Unregister this device's push token BEFORE clearing auth state, so
    // the api call still carries a valid bearer. If it fails (network,
    // backend hiccup, no token cached), logout must still proceed — a
    // stale token is a smaller problem than a user trapped in the app.
    try {
      const api = createApi(getAccessToken);
      await unregisterPushTokenForCurrentDevice(api);
    } catch {
      // unregisterPushTokenForCurrentDevice already swallows its own
      // errors; this catch guards against a synchronous throw from
      // createApi itself (it shouldn't, but logout is non-negotiable).
    }
    await clearSession();
  }

  const value = useMemo(
    () => ({
      ready,
      authenticated: !!tokens,
      login: () => promptAsync(),
      logout,
      getAccessToken,
      canLogin: !!request,
      setupTwoFactor: () => promptActionAsync(),
      canSetupTwoFactor: !!actionRequest,
    }),
    [ready, tokens, request, actionRequest, discovery],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
