import Constants from 'expo-constants';

// Configuration is supplied via app.json -> expo.extra. The defaults use the
// Android emulator alias (10.0.2.2) to reach services on the host machine.
const extra = Constants.expoConfig?.extra ?? {};

export const API_BASE_URL = extra.apiBaseUrl ?? 'http://10.0.2.2:8081/api/v1';
export const KEYCLOAK_URL = extra.keycloakUrl ?? 'http://10.0.2.2:8080';
export const KEYCLOAK_REALM = extra.keycloakRealm ?? 'airhost';
export const KEYCLOAK_CLIENT_ID = extra.keycloakClientId ?? 'airhost-web';
export const KEYCLOAK_ISSUER = `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}`;
