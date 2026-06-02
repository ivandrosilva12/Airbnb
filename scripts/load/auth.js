// Token helpers for the authenticated k6 scripts. We use Keycloak's OIDC
// direct-grant flow (already enabled on the realm — see
// infra/keycloak/realm-export.json) so each VU can mint a real access
// token without scripting a browser login.
//
// Token lifetime in the dev realm is 5 minutes; the scripts here are
// short-lived runs (2-3 minutes) so we don't bother with refresh — the
// first acquisition lasts the whole scenario. If you bump DURATION past
// ~4 minutes, add a refresh helper or shorten the cache window.
import http from 'k6/http';
import { fail } from 'k6';

// Keycloak endpoints. Default to the dev docker-compose layout; override
// via KEYCLOAK_URL when pointing at staging.
const KEYCLOAK_URL = __ENV.KEYCLOAK_URL || 'http://localhost:8080';
const REALM = __ENV.KEYCLOAK_REALM || 'airhost';
const CLIENT_ID = __ENV.KEYCLOAK_CLIENT || 'airhost-api';

const TOKEN_URL = `${KEYCLOAK_URL}/realms/${REALM}/protocol/openid-connect/token`;

// Seeded test users from infra/keycloak/realm-export.json. We expose
// short aliases so scripts can do `getToken('guest')` instead of repeating
// the password literal everywhere.
const USERS = {
  guest: { username: 'guest@airhost.dev', password: 'guest123' },
  host:  { username: 'host@airhost.dev',  password: 'host123'  },
  admin: { username: 'admin@airhost.dev', password: 'admin123' },
};

// Per-VU token cache. k6 instantiates this module once per VU, so __VU
// scoping isn't needed — module-level `let` is already isolated per VU.
let cached = {};

// getToken returns a Bearer-suitable access token for one of the seeded
// test users. Caches per-role so a scenario hammering /me doesn't re-
// authenticate on every iteration.
export function getToken(role) {
  if (cached[role]) return cached[role];
  const u = USERS[role];
  if (!u) fail(`getToken: unknown role ${role}; valid: guest|host|admin`);

  const res = http.post(TOKEN_URL, {
    grant_type: 'password',
    client_id: CLIENT_ID,
    username: u.username,
    password: u.password,
  }, {
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    tags: { name: 'auth-token' },
  });
  if (res.status !== 200) {
    fail(`getToken(${role}): Keycloak returned ${res.status} — is Keycloak up and the realm imported? Body: ${res.body}`);
  }
  const body = JSON.parse(res.body);
  cached[role] = body.access_token;
  return cached[role];
}

// authHeaders returns the Authorization header dictionary for the given
// role. Pass to http.get/post as `headers`. Caches the token transparently.
export function authHeaders(role) {
  return { Authorization: `Bearer ${getToken(role)}` };
}
