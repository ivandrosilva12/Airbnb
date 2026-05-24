import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import * as AuthSession from 'expo-auth-session';
import * as WebBrowser from 'expo-web-browser';
import * as SecureStore from 'expo-secure-store';
import { KEYCLOAK_ISSUER, KEYCLOAK_CLIENT_ID } from '../config';

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

  // Restore persisted tokens on startup.
  useEffect(() => {
    SecureStore.getItemAsync(TOKEN_KEY)
      .then((raw) => raw && setTokens(JSON.parse(raw)))
      .finally(() => setReady(true));
  }, []);

  // Exchange the auth code for tokens once the browser flow completes.
  useEffect(() => {
    async function exchange() {
      if (response?.type !== 'success' || !discovery || !request) return;
      const result = await AuthSession.exchangeCodeAsync(
        {
          clientId: KEYCLOAK_CLIENT_ID,
          code: response.params.code,
          redirectUri,
          extraParams: { code_verifier: request.codeVerifier },
        },
        discovery,
      );
      const stored = {
        accessToken: result.accessToken,
        refreshToken: result.refreshToken,
        expiresAt: Date.now() + (result.expiresIn ?? 300) * 1000,
      };
      setTokens(stored);
      await SecureStore.setItemAsync(TOKEN_KEY, JSON.stringify(stored));
    }
    exchange();
  }, [response, discovery, request]);

  // clearSession drops the stored tokens so the app falls back to the sign-in
  // screen instead of retrying with credentials that will keep returning 401.
  async function clearSession() {
    setTokens(null);
    await SecureStore.deleteItemAsync(TOKEN_KEY);
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
      await SecureStore.setItemAsync(TOKEN_KEY, JSON.stringify(stored));
      return stored.accessToken;
    } catch {
      await clearSession();
      return null;
    }
  }

  async function logout() {
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
    }),
    [ready, tokens, request, discovery],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
