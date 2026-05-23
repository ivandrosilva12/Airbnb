import { createContext, useContext, useEffect, useState } from 'react';
import keycloak from '../keycloak';
import { api } from '../api/client';

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
  const [ready, setReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [profile, setProfile] = useState(null);

  useEffect(() => {
    let cancelled = false;
    keycloak
      .init({ onLoad: 'check-sso', pkceMethod: 'S256' })
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
        setReady(true);
      })
      .catch(() => setReady(true));
    return () => {
      cancelled = true;
    };
  }, []);

  const value = {
    ready,
    authenticated,
    profile,
    isHost: profile?.role === 'host' || profile?.role === 'admin',
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
