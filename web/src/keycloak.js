import Keycloak from 'keycloak-js';

// Single Keycloak instance shared across the app.
const keycloak = new Keycloak({
  url: import.meta.env.VITE_KEYCLOAK_URL || 'http://keycloak:8080',
  realm: import.meta.env.VITE_KEYCLOAK_REALM || 'airhost',
  clientId: import.meta.env.VITE_KEYCLOAK_CLIENT_ID || 'airhost-web',
});

export default keycloak;
