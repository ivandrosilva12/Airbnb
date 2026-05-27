import { Platform } from 'react-native';
import Constants from 'expo-constants';

// Configuration is supplied via app.json -> expo.extra. The native defaults use
// the Android emulator alias (10.0.2.2) to reach services on the host; in a web
// browser that alias is meaningless, so the web target reaches the host on
// localhost instead.
const extra = Constants.expoConfig?.extra ?? {};
const isWeb = Platform.OS === 'web';

export const API_BASE_URL = isWeb ? 'http://localhost:8081/api/v1' : (extra.apiBaseUrl ?? 'http://10.0.2.2:8081/api/v1');
export const KEYCLOAK_URL = isWeb ? 'http://localhost:8080' : (extra.keycloakUrl ?? 'http://10.0.2.2:8080');
export const KEYCLOAK_REALM = extra.keycloakRealm ?? 'airhost';
export const KEYCLOAK_CLIENT_ID = extra.keycloakClientId ?? 'airhost-web';
export const KEYCLOAK_ISSUER = `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}`;

// Public web app origin, used to build shareable links (e.g. a shared wishlist).
export const WEB_BASE_URL = extra.webBaseUrl ?? 'http://localhost:5173';
