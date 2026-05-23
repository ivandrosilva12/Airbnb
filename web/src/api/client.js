import keycloak from '../keycloak';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8081/api/v1';

// request is a thin fetch wrapper that attaches the bearer token (refreshing it
// when close to expiry) and normalises error handling.
async function request(method, path, { body, formData, auth = false } = {}) {
  const headers = {};
  const opts = { method, headers };

  if (auth && keycloak.authenticated) {
    try {
      await keycloak.updateToken(30);
    } catch {
      keycloak.login();
      return;
    }
    headers.Authorization = `Bearer ${keycloak.token}`;
  }

  if (formData) {
    opts.body = formData;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }

  const res = await fetch(`${BASE_URL}${path}`, opts);
  if (res.status === 204) return null;

  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const message = (data && data.error) || res.statusText;
    throw new Error(message);
  }
  return data;
}

export const api = {
  // Listings (public)
  searchProperties: (params = {}) => {
    const qs = new URLSearchParams(params).toString();
    return request('GET', `/properties${qs ? `?${qs}` : ''}`);
  },
  getProperty: (id) => request('GET', `/properties/${id}`),
  getReviews: (id) => request('GET', `/properties/${id}/reviews`),
  getReviewSummary: (id) => request('GET', `/properties/${id}/reviews/summary`),

  // Profile
  me: () => request('GET', '/me', { auth: true }),
  becomeHost: () => request('POST', '/me/become-host', { auth: true }),

  // Bookings
  createBooking: (body) => request('POST', '/bookings', { body, auth: true }),
  myBookings: () => request('GET', '/bookings/me', { auth: true }),
  cancelBooking: (id) => request('POST', `/bookings/${id}/cancel`, { auth: true }),

  // Reviews
  createReview: (body) => request('POST', '/reviews', { body, auth: true }),

  // Host
  myProperties: () => request('GET', '/host/properties', { auth: true }),
  createProperty: (body) => request('POST', '/properties', { body, auth: true }),
  publishProperty: (id) => request('POST', `/properties/${id}/publish`, { auth: true }),
  deleteProperty: (id) => request('DELETE', `/properties/${id}`, { auth: true }),
  uploadPhoto: (id, file) => {
    const fd = new FormData();
    fd.append('photo', file);
    return request('POST', `/properties/${id}/photos`, { formData: fd, auth: true });
  },
  propertyBookings: (id) => request('GET', `/properties/${id}/bookings`, { auth: true }),
  confirmBooking: (id) => request('POST', `/bookings/${id}/confirm`, { auth: true }),
  completeBooking: (id) => request('POST', `/bookings/${id}/complete`, { auth: true }),

  // Availability (public)
  availability: (id, params = {}) => {
    const qs = new URLSearchParams(params).toString();
    return request('GET', `/properties/${id}/availability${qs ? `?${qs}` : ''}`);
  },

  // Messaging
  startConversation: (propertyId) => request('POST', '/conversations', { body: { propertyId }, auth: true }),
  listConversations: () => request('GET', '/conversations', { auth: true }),
  listMessages: (id) => request('GET', `/conversations/${id}/messages`, { auth: true }),
  sendMessage: (id, body) => request('POST', `/conversations/${id}/messages`, { body: { body }, auth: true }),

  // Favorites
  listFavorites: () => request('GET', '/favorites', { auth: true }),
  addFavorite: (propertyId) => request('POST', '/favorites', { body: { propertyId }, auth: true }),
  removeFavorite: (propertyId) => request('DELETE', `/favorites/${propertyId}`, { auth: true }),

  // Notifications
  listNotifications: () => request('GET', '/notifications', { auth: true }),
  markNotificationRead: (id) => request('POST', `/notifications/${id}/read`, { auth: true }),
  markAllNotificationsRead: () => request('POST', '/notifications/read-all', { auth: true }),
};
