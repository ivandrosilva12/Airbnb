import { API_BASE_URL } from '../config';

// createApi builds an API client bound to an access-token provider, so screens
// stay free of auth plumbing. The same endpoints as the web client are exposed.
export function createApi(getAccessToken) {
  async function request(method, path, { body, auth = false } = {}) {
    const headers = {};
    const opts = { method, headers };

    if (auth) {
      const token = await getAccessToken();
      if (token) headers.Authorization = `Bearer ${token}`;
    }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }

    const res = await fetch(`${API_BASE_URL}${path}`, opts);
    if (res.status === 204) return null;
    const text = await res.text();
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) throw new Error((data && data.error) || res.statusText);
    return data;
  }

  return {
    searchProperties: (params = {}) => {
      const qs = new URLSearchParams(params).toString();
      return request('GET', `/properties${qs ? `?${qs}` : ''}`);
    },
    getProperty: (id) => request('GET', `/properties/${id}`),
    me: () => request('GET', '/me', { auth: true }),

    // Identity verification (KYC)
    getVerification: () => request('GET', '/me/verification', { auth: true }),
    submitVerification: (body) => request('POST', '/me/verification', { body, auth: true }),

    createBooking: (body) => request('POST', '/bookings', { body, auth: true }),
    myBookings: () => request('GET', '/bookings/me', { auth: true }),
    cancelBooking: (id) => request('POST', `/bookings/${id}/cancel`, { auth: true }),

    // Post-stay reviews
    pendingReviews: () => request('GET', '/me/reviews/pending', { auth: true }),
    createReview: (body) => request('POST', '/reviews', { body, auth: true }),

    // Payments (guest-facing reads)
    listPayments: () => request('GET', '/payments/me', { auth: true }),

    // Report a listing for moderation
    reportListing: (propertyId, body) => request('POST', `/properties/${propertyId}/reports`, { body, auth: true }),

    // Host
    hostMetrics: () => request('GET', '/host/metrics', { auth: true }),
    hostEarnings: () => request('GET', '/host/earnings', { auth: true }),
    myProperties: () => request('GET', '/host/properties', { auth: true }),
    propertyBookings: (id) => request('GET', `/properties/${id}/bookings`, { auth: true }),
    confirmBooking: (id) => request('POST', `/bookings/${id}/confirm`, { auth: true }),
    completeBooking: (id) => request('POST', `/bookings/${id}/complete`, { auth: true }),

    // Favorites (wishlist)
    listFavorites: () => request('GET', '/favorites', { auth: true }),
    addFavorite: (propertyId) => request('POST', '/favorites', { body: { propertyId }, auth: true }),
    removeFavorite: (propertyId) => request('DELETE', `/favorites/${propertyId}`, { auth: true }),

    // In-app notifications
    listNotifications: () => request('GET', '/notifications', { auth: true }),
    markNotificationRead: (id) => request('POST', `/notifications/${id}/read`, { auth: true }),
    markAllNotificationsRead: () => request('POST', '/notifications/read-all', { auth: true }),

    // Messaging
    listConversations: () => request('GET', '/conversations', { auth: true }),
    startConversation: (propertyId) => request('POST', '/conversations', { body: { propertyId }, auth: true }),
    listMessages: (id) => request('GET', `/conversations/${id}/messages`, { auth: true }),
    sendMessage: (id, body) => request('POST', `/conversations/${id}/messages`, { body: { body }, auth: true }),
    markConversationRead: (id) => request('POST', `/conversations/${id}/read`, { auth: true }),
  };
}
