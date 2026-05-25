import Keycloak from 'keycloak-js';

// Single Keycloak instance shared across the app.
const keycloak = new Keycloak({
  url: import.meta.env.VITE_KEYCLOAK_URL || 'http://keycloak:8080',
  realm: import.meta.env.VITE_KEYCLOAK_REALM || 'airhost',
  clientId: import.meta.env.VITE_KEYCLOAK_CLIENT_ID || 'airhost-web',
});

// keycloak-js throws ("can only be initialized once") if init() is called more
// than once on the same instance. React 18 StrictMode invokes effects twice in
// development, so memoise the init call and hand the same promise back on every
// invocation instead of re-initialising.
let initPromise = null;
export function initKeycloak() {
  if (!initPromise) {
    initPromise = keycloak.init({ onLoad: 'check-sso', pkceMethod: 'S256' });
  }
  return initPromise;
}

export default keycloak;
