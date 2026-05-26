import { createContext, useContext, useEffect, useState } from 'react';
import keycloak, { initKeycloak } from '../keycloak';
import { api } from '../api/client';

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
  const [ready, setReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [profile, setProfile] = useState(null);

  useEffect(() => {
    let cancelled = false;
    // Don't let a slow/unreachable identity provider hold the whole app on a
    // loading screen: reveal the (public) UI after a short cap, and let the auth
    // state fill in whenever init resolves.
    const revealTimer = setTimeout(() => {
      if (!cancelled) setReady(true);
    }, 2500);
    initKeycloak()
      .then(async (auth) => {
        if (cancelled) return;
        setAuthenticated(auth);
        if (auth) {
          try {
            setProfile(await api.me());
          } catch {
            /* profile will be provisioned on first authenticated call */
          }
        }
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) {
          clearTimeout(revealTimer);
          setReady(true);
        }
      });
    return () => {
      cancelled = true;
      clearTimeout(revealTimer);
    };
  }, []);

  const value = {
    ready,
    authenticated,
    profile,
    isHost: profile?.role === 'host' || profile?.role === 'admin',
    isAdmin: profile?.role === 'admin',
    login: () => keycloak.login(),
    register: () => keycloak.register(),
    logout: () => keycloak.logout({ redirectUri: window.location.origin }),
    refreshProfile: async () => setProfile(await api.me()),
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
